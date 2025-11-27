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

// startAutoUpdateManager initializes and starts the auto-update background worker.
func startAutoUpdateManager(
	ctx context.Context,
	serverClient *agent.ServerClient,
	configProvider *autoUpdateConfigProvider,
	fleetProvider *fleetPolicyProvider,
	dataDir string,
	currentVersion string,
	log *logger.Logger,
) (*autoupdate.Manager, error) {
	if serverClient == nil {
		return nil, nil // No server connection, no auto-update
	}

	clientAdapter := autoupdate.NewClientAdapter(serverClient)
	telemetryAdapter := autoupdate.NewTelemetryAdapter(serverClient)
	policyAdapter := autoupdate.NewPolicyAdapter(configProvider, fleetProvider)

	opts := autoupdate.Options{
		Enabled:        true,
		CurrentVersion: currentVersion,
		Platform:       runtime.GOOS,
		Arch:           runtime.GOARCH,
		Channel:        "stable", // TODO: make configurable
		DataDir:        dataDir,
		ServerClient:   clientAdapter,
		PolicyProvider: policyAdapter,
		TelemetrySink:  telemetryAdapter,
		Log:            log,
		// Use defaults for intervals and thresholds
	}

	manager, err := autoupdate.NewManager(opts)
	if err != nil {
		return nil, err
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
		log.Debug("No upload worker available, auto-update disabled")
		return
	}

	serverClient := worker.Client()
	if serverClient == nil {
		log.Debug("No server client available, auto-update disabled")
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
		log.Debug("Registered WebSocket command handler for server commands")
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
			return
		}

		// Trigger immediate update check
		go func() {
			log.Info("Triggering immediate update check per server request")
			if err := manager.CheckNow(ctx); err != nil {
				log.Error("Update check failed", "error", err)
			} else {
				log.Info("Update check completed")
			}
		}()

	default:
		log.Warn("Unknown server command", "command", command)
	}
}
