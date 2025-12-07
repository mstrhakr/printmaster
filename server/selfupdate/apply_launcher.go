package selfupdate

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"printmaster/common/logger"
	"printmaster/server/storage"
)

// ApplyInstruction represents the payload consumed by the helper to apply an update.
type ApplyInstruction struct {
	RunID            int64     `json:"run_id"`
	StagePath        string    `json:"stage_path"`
	BackupPath       string    `json:"backup_path"`
	BinaryPath       string    `json:"binary_path"`
	TargetVersion    string    `json:"target_version"`
	CurrentVersion   string    `json:"current_version"`
	ServiceName      string    `json:"service_name"`
	Platform         string    `json:"platform"`
	Arch             string    `json:"arch"`
	Channel          string    `json:"channel"`
	Component        string    `json:"component"`
	DatabaseConfig   string    `json:"database_config"` // JSON-encoded database config
	StateDir         string    `json:"state_dir"`
	HelperBinaryPath string    `json:"helper_binary_path"`
	LogPath          string    `json:"log_path"`
	CreatedAt        time.Time `json:"created_at"`
}

// ApplyLauncher launches the helper that performs the actual restart and rollback workflow.
type ApplyLauncher interface {
	Launch(run *storage.SelfUpdateRun, inst *ApplyInstruction) (map[string]any, error)
}

type helperLauncher struct {
	stateDir    string
	binaryPath  string
	serviceName string
	log         *logger.Logger
}

func newHelperLauncher(stateDir, binaryPath, serviceName string, log *logger.Logger) *helperLauncher {
	return &helperLauncher{
		stateDir:    stateDir,
		binaryPath:  binaryPath,
		serviceName: serviceName,
		log:         log,
	}
}

func (h *helperLauncher) Launch(run *storage.SelfUpdateRun, inst *ApplyInstruction) (map[string]any, error) {
	if run == nil || inst == nil {
		return nil, fmt.Errorf("run and instruction required")
	}
	if strings.TrimSpace(inst.StagePath) == "" {
		return nil, fmt.Errorf("stage path missing")
	}
	if strings.TrimSpace(inst.BackupPath) == "" {
		return nil, fmt.Errorf("backup path missing")
	}
	helpersDir := filepath.Join(h.stateDir, helperDirName)
	if err := os.MkdirAll(helpersDir, 0o755); err != nil {
		return nil, fmt.Errorf("create helpers dir: %w", err)
	}
	helperName := fmt.Sprintf("run-%d%s", run.ID, filepath.Ext(h.binaryPath))
	helperPath := filepath.Join(helpersDir, helperName)
	if err := copyFile(h.binaryPath, helperPath); err != nil {
		return nil, fmt.Errorf("copy helper binary: %w", err)
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(helperPath, 0o755)
	}
	logDir := filepath.Join(h.stateDir, logDirName)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	logPath := filepath.Join(logDir, fmt.Sprintf("run-%d.log", run.ID))
	applyDir := filepath.Join(h.stateDir, applyDirName)
	if err := os.MkdirAll(applyDir, 0o755); err != nil {
		return nil, fmt.Errorf("create apply dir: %w", err)
	}
	inst.HelperBinaryPath = helperPath
	inst.LogPath = logPath
	instructionPath := filepath.Join(applyDir, fmt.Sprintf("run-%d.json", run.ID))
	data, err := json.MarshalIndent(inst, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal instruction: %w", err)
	}
	if err := os.WriteFile(instructionPath, data, 0o600); err != nil {
		return nil, fmt.Errorf("write instruction: %w", err)
	}
	cmd := exec.Command(helperPath, "--selfupdate-apply", instructionPath)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start helper: %w", err)
	}
	meta := map[string]any{
		"apply_instruction_path": instructionPath,
		"helper_binary":          helperPath,
		"helper_pid":             cmd.Process.Pid,
		"helper_log_path":        logPath,
		"helper_started_at":      time.Now().UTC(),
	}
	return meta, nil
}
