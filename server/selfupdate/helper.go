package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"printmaster/common/config"
	"printmaster/server/storage"
	"runtime"
	"strings"
	"time"
)

// RunApplyHelper executes inside the detached helper binary to orchestrate
// stopping the service, swapping binaries, restarting, and rolling back on error.
func RunApplyHelper(instructionPath string) error {
	inst, err := loadInstruction(instructionPath)
	if err != nil {
		return err
	}
	logger, closeFn, err := newHelperLogger(inst.LogPath)
	if err != nil {
		return err
	}
	defer closeFn()

	logger.Printf("starting apply helper for run %d", inst.RunID)

	if err := os.Remove(instructionPath); err != nil && !os.IsNotExist(err) {
		logger.Printf("warning: unable to remove instruction file: %v", err)
	}

	// Parse database config from JSON
	var dbConfig struct {
		Driver   string `json:"driver"`
		Path     string `json:"path,omitempty"`
		Host     string `json:"host,omitempty"`
		Port     int    `json:"port,omitempty"`
		Name     string `json:"name,omitempty"`
		User     string `json:"user,omitempty"`
		Password string `json:"password,omitempty"`
	}
	if err := json.Unmarshal([]byte(inst.DatabaseConfig), &dbConfig); err != nil {
		logger.Printf("failed to parse database config: %v", err)
		return err
	}

	// Open database using appropriate driver
	var store storage.Store
	if dbConfig.Driver == "postgres" {
		cfg := &config.DatabaseConfig{
			Driver:   "postgres",
			Host:     dbConfig.Host,
			Port:     dbConfig.Port,
			Name:     dbConfig.Name,
			User:     dbConfig.User,
			Password: dbConfig.Password,
		}
		store, err = storage.NewPostgresStore(cfg)
	} else {
		// SQLite
		store, err = storage.NewSQLiteStore(dbConfig.Path)
	}
	if err != nil {
		logger.Printf("failed to open database: %v", err)
		return err
	}
	defer store.Close()

	ctx := context.Background()
	run, err := store.GetSelfUpdateRun(ctx, inst.RunID)
	if err != nil {
		logger.Printf("failed to load run: %v", err)
		return err
	}
	if run == nil {
		return fmt.Errorf("self-update run %d not found", inst.RunID)
	}

	worker := &applyWorker{
		inst:   inst,
		run:    run,
		store:  store,
		logger: logger,
	}

	return worker.execute(ctx)
}

func loadInstruction(path string) (*ApplyInstruction, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read instruction: %w", err)
	}
	var inst ApplyInstruction
	if err := json.Unmarshal(data, &inst); err != nil {
		return nil, fmt.Errorf("decode instruction: %w", err)
	}
	return &inst, nil
}

type applyWorker struct {
	inst   *ApplyInstruction
	run    *storage.SelfUpdateRun
	store  storage.Store
	logger *log.Logger
}

func (w *applyWorker) execute(ctx context.Context) error {
	start := time.Now().UTC()
	w.logger.Printf("apply helper started at %s", start.Format(time.RFC3339))
	if err := w.stopService(ctx); err != nil {
		w.logger.Printf("failed to stop service: %v", err)
		return w.completeFailure(ctx, "stop-service", err, nil)
	}
	if err := w.swapBinary(); err != nil {
		w.logger.Printf("failed to swap binary: %v", err)
		rbErr := w.rollback()
		return w.completeFailure(ctx, "swap-binary", err, rbErr)
	}
	if err := w.startService(ctx); err != nil {
		w.logger.Printf("failed to start service: %v", err)
		rbErr := w.rollback()
		return w.completeFailure(ctx, "start-service", err, rbErr)
	}
	w.logger.Printf("service restarted successfully")
	return w.completeSuccess(ctx)
}

func (w *applyWorker) stopService(ctx context.Context) error {
	return controlService(ctx, w.inst.Platform, w.inst.ServiceName, serviceActionStop, w.logger)
}

func (w *applyWorker) startService(ctx context.Context) error {
	return controlService(ctx, w.inst.Platform, w.inst.ServiceName, serviceActionStart, w.logger)
}

func (w *applyWorker) swapBinary() error {
	if err := copyFile(w.inst.StagePath, w.inst.BinaryPath); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(w.inst.BinaryPath, 0o755); err != nil {
			w.logger.Printf("warning: failed to chmod binary: %v", err)
		}
	}
	return nil
}

func (w *applyWorker) rollback() error {
	if strings.TrimSpace(w.inst.BackupPath) == "" {
		return fmt.Errorf("backup path missing")
	}
	if err := copyFile(w.inst.BackupPath, w.inst.BinaryPath); err != nil {
		return fmt.Errorf("restore backup: %w", err)
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(w.inst.BinaryPath, 0o755)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := controlService(ctx, w.inst.Platform, w.inst.ServiceName, serviceActionStart, w.logger); err != nil {
		return fmt.Errorf("restart original service: %w", err)
	}
	return nil
}

func (w *applyWorker) completeSuccess(ctx context.Context) error {
	w.run.Status = storage.SelfUpdateStatusSucceeded
	w.run.CompletedAt = time.Now().UTC()
	if w.run.Metadata == nil {
		w.run.Metadata = map[string]any{}
	}
	w.run.Metadata = mergeMetadata(w.run.Metadata, map[string]any{
		"helper_log_path": w.inst.LogPath,
		"helper_binary":   w.inst.HelperBinaryPath,
		"rolled_back":     false,
	})
	w.run.CurrentVersion = w.inst.TargetVersion
	w.run.ErrorCode = ""
	w.run.ErrorMessage = ""
	return w.store.UpdateSelfUpdateRun(ctx, w.run)
}

func (w *applyWorker) completeFailure(ctx context.Context, code string, applyErr error, rollbackErr error) error {
	w.run.Status = storage.SelfUpdateStatusFailed
	w.run.CompletedAt = time.Now().UTC()
	w.run.ErrorCode = code
	if applyErr != nil {
		w.run.ErrorMessage = applyErr.Error()
	}
	meta := map[string]any{
		"helper_log_path": w.inst.LogPath,
		"helper_binary":   w.inst.HelperBinaryPath,
		"rolled_back":     rollbackErr == nil,
	}
	if rollbackErr != nil {
		meta["rollback_error"] = rollbackErr.Error()
	}
	w.run.Metadata = mergeMetadata(w.run.Metadata, meta)
	if err := w.store.UpdateSelfUpdateRun(ctx, w.run); err != nil {
		return err
	}
	if applyErr != nil {
		return applyErr
	}
	return fmt.Errorf("self-update failed")
}

func newHelperLogger(path string) (*log.Logger, func(), error) {
	if path == "" {
		return log.Default(), func() {}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, err
	}
	logger := log.New(f, "selfupdate-helper ", log.LstdFlags|log.LUTC)
	return logger, func() { _ = f.Close() }, nil
}
