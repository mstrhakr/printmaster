package main

import (
	"context"
	"runtime"
	"sync"
	"time"

	"printmaster/agent/agent"
	"printmaster/agent/autoupdate"
	"printmaster/common/logger"
	"printmaster/common/updatepolicy"
	wscommon "printmaster/common/ws"
)

// autoUpdateConfigProvider wraps agentConfig to provide the interface.
type autoUpdateConfigProvider struct {
	mu  sync.RWMutex
	cfg *AgentConfig
}

func (p *autoUpdateConfigProvider) GetAutoUpdateMode() updatepolicy.AgentOverrideMode {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.cfg == nil {
		return updatepolicy.AgentOverrideInherit
	}
	return p.cfg.AutoUpdate.GetAutoUpdateMode()
}

func (p *autoUpdateConfigProvider) GetLocalPolicy() updatepolicy.PolicySpec {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.cfg == nil {
		return defaultLocalPolicySpec()
	}
	return p.cfg.AutoUpdate.GetLocalPolicy()
}

func (p *autoUpdateConfigProvider) SetConfig(cfg *AgentConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cfg = cfg
}

// fleetPolicyProvider stores fleet policy received from server.
type fleetPolicyProvider struct {
	mu     sync.RWMutex
	policy *updatepolicy.FleetUpdatePolicy
}

func (p *fleetPolicyProvider) GetFleetPolicy() *updatepolicy.FleetUpdatePolicy {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.policy
}

func (p *fleetPolicyProvider) SetFleetPolicy(policy *updatepolicy.FleetUpdatePolicy) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.policy = policy
}

// sendUpdateProgress sends an update progress message via local SSE and to the server via WebSocket.
func sendUpdateProgress(status autoupdate.Status, targetVersion string, progress int, message string, err error) {
	data := map[string]interface{}{
		"status":   string(status),
		"progress": progress,
		"message":  message,
	}
	if targetVersion != "" {
		data["target_version"] = targetVersion
	}
	if err != nil {
		data["error"] = err.Error()
	}

	// Broadcast to local SSE clients (agent UI)
	if sseHub != nil {
		sseHub.Broadcast(SSEEvent{
			Type: "update_progress",
			Data: data,
		})
	}

	// Also send to server via WebSocket if connected
	uploadWorkerMu.RLock()
	worker := uploadWorker
	uploadWorkerMu.RUnlock()

	if worker == nil {
		return
	}

	wsClient := worker.WSClient()
	if wsClient == nil || !wsClient.IsConnected() {
		return
	}

	msg := wscommon.Message{
		Type:      wscommon.MessageTypeUpdateProgress,
		Data:      data,
		Timestamp: time.Now(),
	}

	if sendErr := wsClient.SendMessage(msg); sendErr != nil {
		// Log but don't fail - progress reporting is best-effort
		_ = sendErr
	}
}

// makeProgressCallback creates a progress callback that sends updates via WebSocket.
func makeProgressCallback() autoupdate.ProgressCallback {
	return func(status autoupdate.Status, targetVersion string, progress int, message string, err error) {
		sendUpdateProgress(status, targetVersion, progress, message, err)
	}
}

// startAutoUpdateManager initializes and starts the auto-update background worker.
func startAutoUpdateManager(
	ctx context.Context,
	serverClient *agent.ServerClient,
	configProvider *autoUpdateConfigProvider,
	fleetProvider *fleetPolicyProvider,
	dataDir string,
	currentVersion string,
	isService bool,
	log *logger.Logger,
) (*autoupdate.Manager, error) {
	if serverClient == nil {
		return nil, nil // No server connection, no auto-update
	}

	clientAdapter := autoupdate.NewClientAdapter(serverClient)
	telemetryAdapter := autoupdate.NewTelemetryAdapter(serverClient)
	policyAdapter := autoupdate.NewPolicyAdapter(configProvider, fleetProvider)

	opts := autoupdate.Options{
		Enabled:          true,
		CurrentVersion:   currentVersion,
		Platform:         runtime.GOOS,
		Arch:             runtime.GOARCH,
		Channel:          "stable", // TODO: make configurable
		DataDir:          dataDir,
		IsService:        isService,
		ServerClient:     clientAdapter,
		PolicyProvider:   policyAdapter,
		TelemetrySink:    telemetryAdapter,
		ProgressCallback: makeProgressCallback(),
		Log:              log,
		// Use defaults for intervals and thresholds
	}

	manager, err := autoupdate.NewManager(opts)
	if err != nil {
		return nil, err
	}

	// Check for post-update validation (runs if we just restarted after an update)
	wasUpdated, fromVersion, validationErr := manager.ValidatePostUpdate()
	if wasUpdated {
		if validationErr != nil {
			// Update validation failed - log error and notify
			log.Error("Post-update validation FAILED",
				"error", validationErr,
				"current_version", currentVersion,
				"from_version", fromVersion)
			sendUpdateProgress(autoupdate.StatusFailed, currentVersion, -1,
				"Update validation failed: "+validationErr.Error(), validationErr)
		} else {
			// Update successful!
			log.Info("Post-update validation PASSED",
				"new_version", currentVersion,
				"previous_version", fromVersion)
			sendUpdateProgress(autoupdate.StatusSucceeded, currentVersion, 100,
				"Update completed successfully from "+fromVersion+" to "+currentVersion, nil)
		}
	}

	manager.Start(ctx)

	log.Info("Auto-update manager started",
		"version", currentVersion,
		"platform", runtime.GOOS,
		"arch", runtime.GOARCH,
	)

	return manager, nil
}

// initAutoUpdateWorker is called after the upload worker is ready to set up auto-updates.
// It runs in a goroutine to avoid blocking startup.
func initAutoUpdateWorker(
	ctx context.Context,
	agentCfg *AgentConfig,
	dataDir string,
	isService bool,
	log *logger.Logger,
) {
	// Wait a bit for upload worker to be fully ready
	select {
	case <-ctx.Done():
		return
	case <-time.After(5 * time.Second):
	}

	uploadWorkerMu.RLock()
	worker := uploadWorker
	uploadWorkerMu.RUnlock()

	if worker == nil {
		log.Info("Auto-update disabled: no upload worker available (agent not connected to server)")
		return
	}

	serverClient := worker.Client()
	if serverClient == nil {
		log.Info("Auto-update disabled: no server client available")
		return
	}

	configProvider := &autoUpdateConfigProvider{cfg: agentCfg}
	fleetProvider := &fleetPolicyProvider{} // Will be populated when fleet policy arrives

	manager, err := startAutoUpdateManager(
		ctx,
		serverClient,
		configProvider,
		fleetProvider,
		dataDir,
		Version,
		isService,
		log,
	)
	if err != nil {
		log.Error("Failed to start auto-update manager", "error", err)
		return
	}

	if manager != nil {
		autoUpdateManagerMu.Lock()
		autoUpdateManager = manager
		autoUpdateManagerMu.Unlock()

		// Register command handler for server-triggered updates
		agent.SetCommandHandler(func(command string, data map[string]interface{}) {
			handleServerCommand(ctx, command, data, log)
		})
		log.Info("Auto-update ready: WebSocket command handler registered")
	}
}

// handleServerCommand processes commands received from the server via WebSocket
func handleServerCommand(ctx context.Context, command string, data map[string]interface{}, log *logger.Logger) {
	log.Info("Processing server command", "command", command)

	switch command {
	case "check_update":
		autoUpdateManagerMu.RLock()
		manager := autoUpdateManager
		autoUpdateManagerMu.RUnlock()

		if manager == nil {
			log.Warn("Received check_update command but auto-update manager not available")
			sendUpdateProgress(autoupdate.StatusFailed, "", -1, "Auto-update manager not available", nil)
			return
		}

		// Trigger immediate update check
		go func() {
			log.Info("Triggering immediate update check per server request")
			if err := manager.CheckNow(ctx); err != nil {
				log.Error("Update check failed", "error", err)
				sendUpdateProgress(autoupdate.StatusFailed, "", -1, err.Error(), err)
			} else {
				log.Info("Update check completed")
			}
		}()

	case "cancel_update":
		autoUpdateManagerMu.RLock()
		manager := autoUpdateManager
		autoUpdateManagerMu.RUnlock()

		if manager == nil {
			log.Warn("Received cancel_update command but auto-update manager not available")
			return
		}

		if manager.Cancel() {
			log.Info("Update cancellation requested")
			sendUpdateProgress(autoupdate.StatusCancelled, "", -1, "Update cancelled by user", nil)
		} else {
			log.Warn("Unable to cancel update - may be in non-cancellable phase")
			sendUpdateProgress(autoupdate.StatusFailed, "", -1, "Cannot cancel: update is in a non-cancellable phase", nil)
		}

	case "force_update":
		autoUpdateManagerMu.RLock()
		manager := autoUpdateManager
		autoUpdateManagerMu.RUnlock()

		if manager == nil {
			log.Warn("Received force_update command but auto-update manager not available")
			sendUpdateProgress(autoupdate.StatusFailed, "", -1, "Auto-update manager not available", nil)
			return
		}

		reason, _ := data["reason"].(string)

		go func() {
			log.Info("Triggering forced reinstall per server request", "reason", reason)
			if err := manager.ForceInstallLatest(ctx, reason); err != nil {
				log.Error("Forced reinstall failed", "error", err)
				sendUpdateProgress(autoupdate.StatusFailed, "", -1, err.Error(), err)
			} else {
				log.Info("Forced reinstall completed")
			}
		}()

	default:
		log.Warn("Unknown server command", "command", command)
	}
}
