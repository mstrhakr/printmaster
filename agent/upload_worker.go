package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"printmaster/agent/agent"
	"printmaster/agent/storage"
)

// Logger interface for upload worker operations
type Logger interface {
	Error(msg string, context ...interface{})
	Warn(msg string, context ...interface{})
	Info(msg string, context ...interface{})
	Debug(msg string, context ...interface{})
}

// UploadWorker handles periodic uploads of device and metrics data to the server
type UploadWorker struct {
	client *agent.ServerClient
	store  storage.DeviceStore
	logger Logger

	// WebSocket client (optional, falls back to HTTP if unavailable)
	wsClient     *agent.WSClient
	useWebSocket bool
	wsClientMu   sync.RWMutex

	// Configuration
	heartbeatInterval time.Duration
	uploadInterval    time.Duration
	retryAttempts     int
	retryBackoff      time.Duration

	// State tracking
	mu                sync.RWMutex
	lastHeartbeat     time.Time
	lastDeviceUpload  time.Time
	lastMetricsUpload time.Time

	// Lifecycle
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// UploadWorkerConfig contains configuration for the upload worker
type UploadWorkerConfig struct {
	HeartbeatInterval time.Duration
	UploadInterval    time.Duration
	RetryAttempts     int
	RetryBackoff      time.Duration
	UseWebSocket      bool // Enable WebSocket for heartbeats
}

// NewUploadWorker creates a new upload worker instance
func NewUploadWorker(client *agent.ServerClient, store storage.DeviceStore, logger Logger, config UploadWorkerConfig) *UploadWorker {
	// Apply defaults
	if config.HeartbeatInterval == 0 {
		config.HeartbeatInterval = 60 * time.Second
	}
	if config.UploadInterval == 0 {
		config.UploadInterval = 300 * time.Second
	}
	if config.RetryAttempts == 0 {
		config.RetryAttempts = 3
	}
	if config.RetryBackoff == 0 {
		config.RetryBackoff = 2 * time.Second
	}

	w := &UploadWorker{
		client:            client,
		store:             store,
		logger:            logger,
		heartbeatInterval: config.HeartbeatInterval,
		uploadInterval:    config.UploadInterval,
		retryAttempts:     config.RetryAttempts,
		retryBackoff:      config.RetryBackoff,
		useWebSocket:      config.UseWebSocket,
		stopCh:            make(chan struct{}),
	}

	return w
}

// Start begins the upload worker goroutines
func (w *UploadWorker) Start(ctx context.Context, version string) error {
	// Ensure agent is registered first
	if err := w.ensureRegistered(ctx, version); err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}

	// Initialize WebSocket client if enabled
	if w.useWebSocket {
		serverURL := w.client.GetServerURL()
		token := w.client.GetToken()

		w.wsClientMu.Lock()
		w.wsClient = agent.NewWSClient(serverURL, token, w.client.IsInsecureSkipVerify())
		w.wsClientMu.Unlock()

		// Start WebSocket client (non-blocking, handles reconnection internally)
		if err := w.wsClient.Start(); err != nil {
			w.logger.Warn("WebSocket client failed to start (falling back to HTTP)", "error", err)
			w.wsClientMu.Lock()
			w.wsClient = nil
			w.wsClientMu.Unlock()
		} else {
			w.logger.Info("WebSocket client started for live heartbeat")
		}
	}

	// Start heartbeat goroutine
	w.wg.Add(1)
	go w.heartbeatLoop()

	// Start upload goroutine
	w.wg.Add(1)
	go w.uploadLoop()

	w.logger.Info("Upload worker started",
		"heartbeat_interval", w.heartbeatInterval,
		"upload_interval", w.uploadInterval,
		"websocket_enabled", w.useWebSocket)

	return nil
}

// Stop gracefully shuts down the upload worker
func (w *UploadWorker) Stop() {
	w.logger.Info("Stopping upload worker...")
	close(w.stopCh)

	// Stop WebSocket client if running
	w.wsClientMu.Lock()
	if w.wsClient != nil {
		w.wsClient.Stop()
		w.wsClient = nil
	}
	w.wsClientMu.Unlock()

	w.wg.Wait()
	w.logger.Info("Upload worker stopped")
}

// ensureRegistered checks if agent has a token, registers if not
func (w *UploadWorker) ensureRegistered(ctx context.Context, version string) error {
	token := w.client.GetToken()

	if token != "" {
		// Already have token, validate it with a heartbeat
		hbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		if err := w.client.Heartbeat(hbCtx); err == nil {
			w.logger.Info("Using existing authentication token")
			return nil // Token is valid
		}

		// Token invalid, need to re-register
		w.logger.Warn("Existing token invalid, re-registering")
		w.client.SetToken("")
	}

	// Register and get new token
	w.logger.Info("Registering agent with server")
	regCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	token, err := w.client.Register(regCtx, version)
	if err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}

	w.logger.Info("Agent registered successfully", "token", token[:8]+"...")
	return nil
}

// heartbeatLoop sends periodic heartbeats to the server
func (w *UploadWorker) heartbeatLoop() {
	defer w.wg.Done()

	ticker := time.NewTicker(w.heartbeatInterval)
	defer ticker.Stop()

	// Send initial heartbeat immediately
	w.sendHeartbeat()

	for {
		select {
		case <-ticker.C:
			w.sendHeartbeat()
		case <-w.stopCh:
			return
		}
	}
}

// sendHeartbeat sends a single heartbeat with retry logic
func (w *UploadWorker) sendHeartbeat() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Try WebSocket first if available
	w.wsClientMu.RLock()
	wsClient := w.wsClient
	w.wsClientMu.RUnlock()

	if wsClient != nil && wsClient.IsConnected() {
		// Get device count to include in heartbeat
		visibleOnly := true
		devices, err := w.store.List(ctx, storage.DeviceFilter{Visible: &visibleOnly})
		deviceCount := 0
		if err == nil {
			deviceCount = len(devices)
		}

		// Send heartbeat over WebSocket
		heartbeatData := map[string]interface{}{
			"device_count": deviceCount,
		}

		if err := wsClient.SendHeartbeat(heartbeatData); err != nil {
			w.logger.Warn("WebSocket heartbeat failed, falling back to HTTP", "error", err)
			// Fall through to HTTP heartbeat
		} else {
			w.mu.Lock()
			w.lastHeartbeat = time.Now()
			w.mu.Unlock()
			w.logger.Debug("Heartbeat sent via WebSocket")
			return
		}
	}

	// Fall back to HTTP heartbeat
	err := w.retryWithBackoff(func() error {
		return w.client.Heartbeat(ctx)
	})

	if err != nil {
		w.logger.Warn("HTTP heartbeat failed after retries", "error", err)
	} else {
		w.mu.Lock()
		w.lastHeartbeat = time.Now()
		w.mu.Unlock()
		w.logger.Debug("Heartbeat sent via HTTP")
	}
}

// uploadLoop handles periodic uploads of devices and metrics
func (w *UploadWorker) uploadLoop() {
	defer w.wg.Done()

	ticker := time.NewTicker(w.uploadInterval)
	defer ticker.Stop()

	// Upload immediately on start (don't wait for first interval)
	w.doUpload()

	for {
		select {
		case <-ticker.C:
			w.doUpload()
		case <-w.stopCh:
			return
		}
	}
}

// doUpload performs a complete upload cycle (devices + metrics)
func (w *UploadWorker) doUpload() {
	w.logger.Debug("Starting upload cycle")

	// Upload devices first
	if err := w.uploadDevices(); err != nil {
		w.logger.Error("Device upload failed", "error", err)
		// Continue to metrics even if devices failed (partial success OK)
	}

	// Then upload metrics
	if err := w.uploadMetrics(); err != nil {
		w.logger.Error("Metrics upload failed", "error", err)
	}

	w.logger.Debug("Upload cycle complete")
}

// uploadDevices reads devices from store and uploads them
func (w *UploadWorker) uploadDevices() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Get all visible devices from store
	visibleTrue := true
	filter := storage.DeviceFilter{
		Visible: &visibleTrue,
	}

	devices, err := w.store.List(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to list devices: %w", err)
	}

	if len(devices) == 0 {
		w.logger.Debug("No devices to upload")
		return nil
	}

	// Convert devices to upload format
	deviceMaps := make([]interface{}, 0, len(devices))
	for _, dev := range devices {
		deviceMap := map[string]interface{}{
			"serial":           dev.Serial,
			"ip":               dev.IP,
			"manufacturer":     dev.Manufacturer,
			"model":            dev.Model,
			"hostname":         dev.Hostname,
			"firmware":         dev.Firmware,
			"mac_address":      dev.MACAddress,
			"subnet_mask":      dev.SubnetMask,
			"gateway":          dev.Gateway,
			"consumables":      dev.Consumables,
			"status_messages":  dev.StatusMessages,
			"last_seen":        dev.LastSeen,
			"first_seen":       dev.FirstSeen,
			"discovery_method": dev.DiscoveryMethod,
			"asset_number":     dev.AssetNumber,
			"location":         dev.Location,
			"description":      dev.Description,
			"web_ui_url":       dev.WebUIURL,
			"raw_data":         dev.RawData,
		}
		deviceMaps = append(deviceMaps, deviceMap)
	}

	// Upload with retry
	err = w.retryWithBackoff(func() error {
		return w.client.UploadDevices(ctx, deviceMaps)
	})

	if err != nil {
		return fmt.Errorf("failed to upload devices: %w", err)
	}

	w.mu.Lock()
	w.lastDeviceUpload = time.Now()
	w.mu.Unlock()

	w.logger.Info("Devices uploaded successfully", "count", len(devices))
	return nil
}

// uploadMetrics reads latest metrics from store and uploads them
func (w *UploadWorker) uploadMetrics() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Get all visible devices to fetch their latest metrics
	visibleTrue := true
	filter := storage.DeviceFilter{
		Visible: &visibleTrue,
	}

	devices, err := w.store.List(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to list devices for metrics: %w", err)
	}

	if len(devices) == 0 {
		w.logger.Debug("No devices for metrics upload")
		return nil
	}

	// Get latest metrics for each device
	var metricMaps []interface{}

	for _, dev := range devices {
		metrics, err := w.store.GetLatestMetrics(ctx, dev.Serial)
		if err != nil || metrics == nil {
			// No metrics for this device yet, skip
			continue
		}

		metricMap := map[string]interface{}{
			"serial":       metrics.Serial,
			"timestamp":    metrics.Timestamp,
			"page_count":   metrics.PageCount,
			"color_pages":  metrics.ColorPages,
			"mono_pages":   metrics.MonoPages,
			"scan_count":   metrics.ScanCount,
			"toner_levels": metrics.TonerLevels,
		}
		metricMaps = append(metricMaps, metricMap)
	}

	if len(metricMaps) == 0 {
		w.logger.Debug("No metrics to upload")
		return nil
	}

	// Upload with retry
	err = w.retryWithBackoff(func() error {
		return w.client.UploadMetrics(ctx, metricMaps)
	})

	if err != nil {
		return fmt.Errorf("failed to upload metrics: %w", err)
	}

	w.mu.Lock()
	w.lastMetricsUpload = time.Now()
	w.mu.Unlock()

	w.logger.Info("Metrics uploaded successfully", "count", len(metricMaps))
	return nil
}

// retryWithBackoff retries a function with exponential backoff
func (w *UploadWorker) retryWithBackoff(fn func() error) error {
	var lastErr error

	for attempt := 0; attempt < w.retryAttempts; attempt++ {
		err := fn()
		if err == nil {
			return nil // Success
		}

		lastErr = err

		// Don't retry on the last attempt
		if attempt == w.retryAttempts-1 {
			break
		}

		// Exponential backoff: 2s, 4s, 8s, etc.
		backoff := w.retryBackoff * time.Duration(1<<attempt)
		w.logger.Debug("Retry after backoff",
			"attempt", attempt+1,
			"backoff", backoff,
			"error", err)

		select {
		case <-time.After(backoff):
			// Continue to next attempt
		case <-w.stopCh:
			return fmt.Errorf("stopped during retry")
		}
	}

	return fmt.Errorf("failed after %d attempts: %w", w.retryAttempts, lastErr)
}

// GetStats returns current upload worker statistics
func (w *UploadWorker) GetStats() map[string]interface{} {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return map[string]interface{}{
		"last_heartbeat":      w.lastHeartbeat,
		"last_device_upload":  w.lastDeviceUpload,
		"last_metrics_upload": w.lastMetricsUpload,
		"heartbeat_interval":  w.heartbeatInterval.String(),
		"upload_interval":     w.uploadInterval.String(),
	}
}
