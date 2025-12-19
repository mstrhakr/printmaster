package main

import (
	"context"
	"encoding/json"
	"net/http"
	"runtime"
	"strconv"
	"sync"
	"time"

	"printmaster/agent/spooler"
	"printmaster/agent/storage"
)

// SpoolerWorker manages the print spooler watcher and integrates with storage
type SpoolerWorker struct {
	watcher *spooler.Watcher
	store   storage.LocalPrinterStore
	logger  Logger

	mu      sync.RWMutex
	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// SpoolerWorkerConfig holds configuration for the spooler worker
type SpoolerWorkerConfig struct {
	PollInterval           time.Duration
	IncludeNetworkPrinters bool
	IncludeVirtualPrinters bool
	AutoTrackUSB           bool
	AutoTrackLocal         bool
}

// DefaultSpoolerWorkerConfig returns sensible defaults
func DefaultSpoolerWorkerConfig() SpoolerWorkerConfig {
	return SpoolerWorkerConfig{
		PollInterval:           5 * time.Second,
		IncludeNetworkPrinters: false,
		IncludeVirtualPrinters: false,
		AutoTrackUSB:           true,
		AutoTrackLocal:         false,
	}
}

// NewSpoolerWorker creates a new spooler worker
func NewSpoolerWorker(store storage.LocalPrinterStore, config SpoolerWorkerConfig, logger Logger) *SpoolerWorker {
	watcherConfig := spooler.WatcherConfig{
		PollInterval:           config.PollInterval,
		IncludeNetworkPrinters: config.IncludeNetworkPrinters,
		IncludeVirtualPrinters: config.IncludeVirtualPrinters,
		AutoTrackUSB:           config.AutoTrackUSB,
		AutoTrackLocal:         config.AutoTrackLocal,
	}

	return &SpoolerWorker{
		watcher: spooler.NewWatcher(watcherConfig, logger),
		store:   store,
		logger:  logger,
	}
}

// Start begins monitoring the print spooler
func (w *SpoolerWorker) Start() error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}

	// Check if spooler is supported on this platform
	if !spooler.IsSupported() {
		w.mu.Unlock()
		w.logger.Info("Spooler watching not supported on this platform", "os", runtime.GOOS)
		return nil
	}

	w.running = true
	w.stopCh = make(chan struct{})
	w.mu.Unlock()

	// Start the underlying watcher
	if err := w.watcher.Start(); err != nil {
		w.mu.Lock()
		w.running = false
		w.mu.Unlock()
		return err
	}

	// Start the event processor goroutine
	w.wg.Add(1)
	go w.processEvents()

	// Start the sync goroutine (syncs watcher state to storage)
	w.wg.Add(1)
	go w.syncLoop()

	w.logger.Info("Spooler worker started")
	return nil
}

// Stop halts the spooler worker
func (w *SpoolerWorker) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	w.running = false
	close(w.stopCh)
	w.mu.Unlock()

	w.watcher.Stop()
	w.wg.Wait()
	w.logger.Info("Spooler worker stopped")
}

// IsRunning returns whether the worker is running
func (w *SpoolerWorker) IsRunning() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.running
}

// GetPrinters returns all discovered local printers
func (w *SpoolerWorker) GetPrinters() []*spooler.LocalPrinter {
	return w.watcher.GetPrinters()
}

// GetPrinter returns a specific printer by name
func (w *SpoolerWorker) GetPrinter(name string) (*spooler.LocalPrinter, bool) {
	return w.watcher.GetPrinter(name)
}

// SetBaseline sets the baseline page count for a printer
func (w *SpoolerWorker) SetBaseline(printerName string, baseline int64) error {
	// Update in watcher
	if !w.watcher.SetBaseline(printerName, baseline) {
		return storage.ErrNotFound
	}

	// Persist to storage
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return w.store.SetLocalPrinterBaseline(ctx, printerName, baseline)
}

// SetTracking enables/disables tracking for a printer
func (w *SpoolerWorker) SetTracking(printerName string, enabled bool) error {
	// Update in watcher
	if !w.watcher.SetTracking(printerName, enabled) {
		return storage.ErrNotFound
	}

	// Persist to storage
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return w.store.SetLocalPrinterTracking(ctx, printerName, enabled)
}

// processEvents handles job events from the watcher
func (w *SpoolerWorker) processEvents() {
	defer w.wg.Done()

	events := w.watcher.Events()
	if events == nil {
		return
	}

	for {
		select {
		case <-w.stopCh:
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			w.handleJobEvent(event)
		}
	}
}

// handleJobEvent processes a single job event
func (w *SpoolerWorker) handleJobEvent(event spooler.JobEvent) {
	switch event.Type {
	case spooler.JobEventCompleted:
		// Record the completed job
		if event.Job != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			job := &storage.LocalPrintJob{
				PrinterName:  event.PrinterName,
				JobID:        event.Job.JobID,
				DocumentName: event.Job.DocumentName,
				UserName:     event.Job.UserName,
				MachineName:  event.Job.MachineName,
				TotalPages:   event.Job.TotalPages,
				PagesPrinted: event.Job.PagesPrinted,
				IsColor:      event.Job.IsColor,
				SizeBytes:    event.Job.Size,
				SubmittedAt:  event.Job.Submitted,
				CompletedAt:  event.Timestamp,
				Status:       "completed",
			}

			if err := w.store.AddLocalPrintJob(ctx, job); err != nil {
				w.logger.Warn("Failed to record print job",
					"printer", event.PrinterName,
					"error", err)
			}

			// Update page count in storage
			pages := int64(event.PagesAdded)
			if pages <= 0 && event.Job.TotalPages > 0 {
				pages = int64(event.Job.TotalPages)
			}
			if pages > 0 {
				colorPages := int64(0)
				monoPages := pages
				if event.Job.IsColor {
					colorPages = pages
					monoPages = 0
				}

				if err := w.store.UpdateLocalPrinterPages(ctx, event.PrinterName, pages, colorPages, monoPages); err != nil {
					if err != storage.ErrNotFound {
						w.logger.Warn("Failed to update page count",
							"printer", event.PrinterName,
							"error", err)
					}
				}
			}

			w.logger.Debug("Print job completed",
				"printer", event.PrinterName,
				"document", event.Job.DocumentName,
				"pages", event.PagesAdded)

			// Broadcast SSE event if available
			if sseHub != nil {
				sseHub.Broadcast(SSEEvent{
					Type: "print_job_completed",
					Data: map[string]interface{}{
						"printer_name":  event.PrinterName,
						"document_name": event.Job.DocumentName,
						"pages":         event.PagesAdded,
						"timestamp":     event.Timestamp,
					},
				})
			}
		}
	}
}

// syncLoop periodically syncs watcher state to storage
func (w *SpoolerWorker) syncLoop() {
	defer w.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Initial sync
	w.syncPrinters()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.syncPrinters()
		}
	}
}

// syncPrinters persists current printer state to storage
func (w *SpoolerWorker) syncPrinters() {
	printers := w.watcher.GetPrinters()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, p := range printers {
		// Load existing data from storage to preserve user-set fields
		existing, err := w.store.GetLocalPrinter(ctx, p.Name)

		storagePrinter := &storage.LocalPrinter{
			Name:            p.Name,
			PortName:        p.PortName,
			DriverName:      p.DriverName,
			PrinterType:     string(p.Type),
			IsDefault:       p.IsDefault,
			IsShared:        p.IsShared,
			Manufacturer:    p.Manufacturer,
			Model:           p.Model,
			SerialNumber:    p.SerialNumber,
			Status:          p.Status,
			FirstSeen:       p.FirstSeen,
			LastSeen:        p.LastSeen,
			TotalPages:      p.TotalPages,
			TotalColorPages: p.TotalColorPages,
			TotalMonoPages:  p.TotalMonoPages,
			BaselinePages:   p.BaselinePages,
			LastPageUpdate:  p.LastPageUpdate,
			TrackingEnabled: p.TrackingEnabled,
		}

		// Preserve user-set fields from existing record
		if err == nil && existing != nil {
			if existing.AssetNumber != "" {
				storagePrinter.AssetNumber = existing.AssetNumber
			}
			if existing.Location != "" {
				storagePrinter.Location = existing.Location
			}
			if existing.Description != "" {
				storagePrinter.Description = existing.Description
			}
			// Preserve tracking state if user has set it
			if existing.TrackingEnabled != p.TrackingEnabled {
				storagePrinter.TrackingEnabled = existing.TrackingEnabled
			}
			// Preserve baseline if set
			if existing.BaselinePages > 0 && p.BaselinePages == 0 {
				storagePrinter.BaselinePages = existing.BaselinePages
			}
		}

		if err := w.store.UpsertLocalPrinter(ctx, storagePrinter); err != nil {
			w.logger.Warn("Failed to sync printer to storage",
				"printer", p.Name,
				"error", err)
		}
	}
}

// Global spooler worker instance
var (
	spoolerWorker   *SpoolerWorker
	spoolerWorkerMu sync.RWMutex
)

// RegisterSpoolerHandlers registers HTTP handlers for local printer management
func RegisterSpoolerHandlers(store storage.LocalPrinterStore) {
	// List local printers
	// GET /api/local-printers
	// Query params: type=usb|local|network|virtual, tracking=true|false
	http.HandleFunc("/api/local-printers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}

		// Check if spooler is supported
		if !spooler.IsSupported() {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"supported": false,
				"message":   "Local printer monitoring is only supported on Windows",
				"printers":  []interface{}{},
			})
			return
		}

		filter := storage.LocalPrinterFilter{}

		if typeParam := r.URL.Query().Get("type"); typeParam != "" {
			filter.PrinterType = &typeParam
		}
		if trackingParam := r.URL.Query().Get("tracking"); trackingParam != "" {
			tracking := trackingParam == "true"
			filter.TrackingEnabled = &tracking
		}
		if nameParam := r.URL.Query().Get("name"); nameParam != "" {
			filter.Name = nameParam
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		printers, err := store.ListLocalPrinters(ctx, filter)
		if err != nil {
			http.Error(w, "failed to list printers: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Also get live status from watcher if available
		spoolerWorkerMu.RLock()
		worker := spoolerWorker
		spoolerWorkerMu.RUnlock()

		liveStatus := make(map[string]*spooler.LocalPrinter)
		if worker != nil && worker.IsRunning() {
			for _, p := range worker.GetPrinters() {
				liveStatus[p.Name] = p
			}
		}

		// Merge live status with stored data
		response := make([]map[string]interface{}, 0, len(printers))
		for _, p := range printers {
			item := map[string]interface{}{
				"name":              p.Name,
				"port_name":         p.PortName,
				"driver_name":       p.DriverName,
				"printer_type":      p.PrinterType,
				"is_default":        p.IsDefault,
				"is_shared":         p.IsShared,
				"manufacturer":      p.Manufacturer,
				"model":             p.Model,
				"serial_number":     p.SerialNumber,
				"status":            p.Status,
				"first_seen":        p.FirstSeen,
				"last_seen":         p.LastSeen,
				"total_pages":       p.TotalPageCount(),
				"tracked_pages":     p.TotalPages,
				"baseline_pages":    p.BaselinePages,
				"total_color_pages": p.TotalColorPages,
				"total_mono_pages":  p.TotalMonoPages,
				"last_page_update":  p.LastPageUpdate,
				"tracking_enabled":  p.TrackingEnabled,
				"asset_number":      p.AssetNumber,
				"location":          p.Location,
				"description":       p.Description,
			}

			// Override with live status if available
			if live, ok := liveStatus[p.Name]; ok {
				item["status"] = live.Status
				item["job_count"] = live.JobCount
				item["printing_job"] = live.PrintingJob
			}

			response = append(response, item)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"supported": true,
			"running":   worker != nil && worker.IsRunning(),
			"printers":  response,
		})
	})

	// Get single local printer
	// GET /api/local-printers/{name}
	http.HandleFunc("/api/local-printers/get", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}

		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "name parameter required", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		printer, err := store.GetLocalPrinter(ctx, name)
		if err != nil {
			if err == storage.ErrNotFound {
				http.Error(w, "printer not found", http.StatusNotFound)
				return
			}
			http.Error(w, "failed to get printer: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(printer)
	})

	// Update local printer settings
	// POST /api/local-printers/update
	// Body: { "name": "printer name", "tracking_enabled": true, "baseline_pages": 1000, ... }
	http.HandleFunc("/api/local-printers/update", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Name            string  `json:"name"`
			TrackingEnabled *bool   `json:"tracking_enabled,omitempty"`
			BaselinePages   *int64  `json:"baseline_pages,omitempty"`
			Manufacturer    *string `json:"manufacturer,omitempty"`
			Model           *string `json:"model,omitempty"`
			SerialNumber    *string `json:"serial_number,omitempty"`
			AssetNumber     *string `json:"asset_number,omitempty"`
			Location        *string `json:"location,omitempty"`
			Description     *string `json:"description,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
			return
		}

		if req.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		// Handle tracking change
		if req.TrackingEnabled != nil {
			spoolerWorkerMu.RLock()
			worker := spoolerWorker
			spoolerWorkerMu.RUnlock()

			if worker != nil {
				worker.SetTracking(req.Name, *req.TrackingEnabled)
			} else {
				store.SetLocalPrinterTracking(ctx, req.Name, *req.TrackingEnabled)
			}
		}

		// Handle baseline change
		if req.BaselinePages != nil {
			spoolerWorkerMu.RLock()
			worker := spoolerWorker
			spoolerWorkerMu.RUnlock()

			if worker != nil {
				worker.SetBaseline(req.Name, *req.BaselinePages)
			} else {
				store.SetLocalPrinterBaseline(ctx, req.Name, *req.BaselinePages)
			}
		}

		// Handle other field updates
		updates := make(map[string]interface{})
		if req.Manufacturer != nil {
			updates["manufacturer"] = *req.Manufacturer
		}
		if req.Model != nil {
			updates["model"] = *req.Model
		}
		if req.SerialNumber != nil {
			updates["serial_number"] = *req.SerialNumber
		}
		if req.AssetNumber != nil {
			updates["asset_number"] = *req.AssetNumber
		}
		if req.Location != nil {
			updates["location"] = *req.Location
		}
		if req.Description != nil {
			updates["description"] = *req.Description
		}

		if len(updates) > 0 {
			if err := store.UpdateLocalPrinterInfo(ctx, req.Name, updates); err != nil {
				if err == storage.ErrNotFound {
					http.Error(w, "printer not found", http.StatusNotFound)
					return
				}
				http.Error(w, "failed to update printer: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
	})

	// Get print job history for a printer
	// GET /api/local-printers/jobs?name=PRINTER&limit=100
	http.HandleFunc("/api/local-printers/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}

		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "name parameter required", http.StatusBadRequest)
			return
		}

		limit := 100
		if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
				limit = l
			}
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		jobs, err := store.GetLocalPrintJobs(ctx, name, limit)
		if err != nil {
			http.Error(w, "failed to get jobs: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"printer_name": name,
			"jobs":         jobs,
		})
	})

	// Get statistics for a printer
	// GET /api/local-printers/stats?name=PRINTER&days=30
	http.HandleFunc("/api/local-printers/stats", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}

		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "name parameter required", http.StatusBadRequest)
			return
		}

		days := 30
		if daysStr := r.URL.Query().Get("days"); daysStr != "" {
			if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
				days = d
			}
		}

		since := time.Now().AddDate(0, 0, -days)

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		stats, err := store.GetLocalPrinterStats(ctx, name, since)
		if err != nil {
			http.Error(w, "failed to get stats: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	})

	// Spooler watcher control
	// POST /api/local-printers/watcher/start
	http.HandleFunc("/api/local-printers/watcher/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}

		if !spooler.IsSupported() {
			http.Error(w, "spooler watching not supported on this platform", http.StatusNotImplemented)
			return
		}

		spoolerWorkerMu.Lock()
		defer spoolerWorkerMu.Unlock()

		if spoolerWorker != nil && spoolerWorker.IsRunning() {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"message": "watcher already running",
			})
			return
		}

		if spoolerWorker == nil {
			spoolerWorker = NewSpoolerWorker(store, DefaultSpoolerWorkerConfig(), appLogger)
		}

		if err := spoolerWorker.Start(); err != nil {
			http.Error(w, "failed to start watcher: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "watcher started",
		})
	})

	// POST /api/local-printers/watcher/stop
	http.HandleFunc("/api/local-printers/watcher/stop", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}

		spoolerWorkerMu.Lock()
		defer spoolerWorkerMu.Unlock()

		if spoolerWorker == nil || !spoolerWorker.IsRunning() {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"message": "watcher not running",
			})
			return
		}

		spoolerWorker.Stop()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "watcher stopped",
		})
	})

	// GET /api/local-printers/watcher/status
	http.HandleFunc("/api/local-printers/watcher/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}

		spoolerWorkerMu.RLock()
		worker := spoolerWorker
		spoolerWorkerMu.RUnlock()

		status := map[string]interface{}{
			"supported": spooler.IsSupported(),
			"running":   false,
			"platform":  runtime.GOOS,
		}

		if worker != nil {
			status["running"] = worker.IsRunning()
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	})
}

// StartSpoolerWorker starts the spooler worker (called from main)
func StartSpoolerWorker(store storage.LocalPrinterStore, config SpoolerWorkerConfig, logger Logger) error {
	if !spooler.IsSupported() {
		if logger != nil {
			logger.Info("Spooler watching not supported on this platform", "os", runtime.GOOS)
		}
		return nil
	}

	spoolerWorkerMu.Lock()
	defer spoolerWorkerMu.Unlock()

	if spoolerWorker != nil {
		return nil
	}

	spoolerWorker = NewSpoolerWorker(store, config, logger)
	return spoolerWorker.Start()
}

// StopSpoolerWorker stops the spooler worker (called from main during shutdown)
func StopSpoolerWorker() {
	spoolerWorkerMu.Lock()
	defer spoolerWorkerMu.Unlock()

	if spoolerWorker != nil {
		spoolerWorker.Stop()
		spoolerWorker = nil
	}
}
