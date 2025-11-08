package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/kardianos/service"
)

// program implements service.Interface
type program struct {
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	svcLogger service.Logger
}

func (p *program) Start(s service.Service) error {
	// Get service logger
	p.svcLogger, _ = s.Logger(nil)
	if p.svcLogger != nil {
		p.svcLogger.Info("PrintMaster Server service starting")
	}

	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.done = make(chan struct{})

	go p.run()
	return nil
}

func (p *program) run() {
	defer close(p.done)

	if p.svcLogger != nil {
		p.svcLogger.Info("PrintMaster Server service running")
	}

	// Call runServer with context for graceful shutdown
	runServer(p.ctx)

	if p.svcLogger != nil {
		p.svcLogger.Info("PrintMaster Server service stopping")
	}
}

func (p *program) Stop(s service.Service) error {
	// Service is stopping, cancel context and wait for shutdown
	if p.svcLogger != nil {
		p.svcLogger.Info("PrintMaster Server service stop requested")
	}

	if p.cancel != nil {
		p.cancel()
	}

	// Wait for run() to finish with timeout
	timeout := time.After(30 * time.Second)
	select {
	case <-p.done:
		if p.svcLogger != nil {
			p.svcLogger.Info("PrintMaster Server service stopped gracefully")
		}
	case <-timeout:
		if p.svcLogger != nil {
			p.svcLogger.Warning("PrintMaster Server service stopped with timeout")
		}
	}

	return nil
}

// getServiceConfig returns the service configuration for the current platform
func getServiceConfig() *service.Config {
	// Determine service data directory based on platform
	var workingDir string
	switch runtime.GOOS {
	case "windows":
		// Windows: C:\ProgramData\PrintMaster\server
		workingDir = filepath.Join(os.Getenv("ProgramData"), "PrintMaster", "server")
	case "darwin":
		// macOS: /Library/Application Support/PrintMaster/server
		workingDir = "/Library/Application Support/PrintMaster/server"
	default:
		// Linux: /var/lib/printmaster/server
		workingDir = "/var/lib/printmaster/server"
	}

	return &service.Config{
		Name:             "PrintMasterServer",
		DisplayName:      "PrintMaster Server",
		Description:      "PrintMaster central management server. Aggregates data from agents, provides reporting, alerting, and web UI.",
		WorkingDirectory: workingDir,
		Arguments:        []string{"--service", "run"},
		Option: service.KeyValue{
			// Windows service options
			"StartType":              "automatic",
			"DelayedAutoStart":       true,
			"Dependencies":           "",
			"OnFailure":              "restart",
			"OnFailureDelayDuration": "5s",
			"OnFailureResetPeriod":   30,

			// Linux systemd options
			"Restart":           "on-failure",
			"RestartSec":        5,
			"SuccessExitStatus": "0 SIGTERM",
			"KillMode":          "mixed",
			"KillSignal":        "SIGTERM",
			"SendSIGKILL":       true,

			// macOS launchd options
			"RunAtLoad":     true,
			"KeepAlive":     true,
			"SessionCreate": false,
		},
	}
}

// setupServiceDirectories creates necessary directories for service operation
func setupServiceDirectories() error {
	var dirs []string
	var configPath string

	switch runtime.GOOS {
	case "windows":
		baseDir := filepath.Join(os.Getenv("ProgramData"), "PrintMaster")
		serverDir := filepath.Join(baseDir, "server")
		dirs = []string{
			baseDir,
			serverDir,
			filepath.Join(serverDir, "logs"),
		}
		configPath = filepath.Join(serverDir, "config.toml")
	case "darwin":
		baseDir := "/Library/Application Support/PrintMaster"
		serverDir := filepath.Join(baseDir, "server")
		dirs = []string{
			baseDir,
			serverDir,
			filepath.Join(serverDir, "logs"),
			"/var/log/printmaster/server",
		}
		configPath = filepath.Join(serverDir, "config.toml")
	default: // Linux
		dirs = []string{
			"/var/lib/printmaster",
			"/var/lib/printmaster/server",
			"/var/log/printmaster",
			"/var/log/printmaster/server",
			"/etc/printmaster",
		}
		configPath = "/etc/printmaster/server.toml"
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Generate default config.toml if it doesn't exist
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := WriteDefaultConfig(configPath); err != nil {
			// Check if error is because file already exists (race condition)
			if strings.Contains(err.Error(), "already exists") {
				fmt.Printf("Configuration already exists at: %s\n", configPath)
			} else {
				return fmt.Errorf("failed to generate default config at %s: %w", configPath, err)
			}
		} else {
			fmt.Printf("Generated default configuration at: %s\n", configPath)
		}
	} else {
		fmt.Printf("Configuration already exists at: %s\n", configPath)
	}

	return nil
}
