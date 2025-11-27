package selfupdate

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type serviceAction string

const (
	serviceActionStop  serviceAction = "stop"
	serviceActionStart serviceAction = "start"
)

type serviceState string

const (
	stateRunning     serviceState = "running"
	stateStopped     serviceState = "stopped"
	stateStopPending serviceState = "stop_pending"
)

func controlService(ctx context.Context, platform, name string, action serviceAction, logger *log.Logger) error {
	if platform == "" {
		platform = runtime.GOOS
	}
	switch platform {
	case "windows":
		return controlWindowsService(ctx, name, action, logger)
	default:
		return controlSystemdService(ctx, name, action, logger)
	}
}

func controlWindowsService(ctx context.Context, name string, action serviceAction, logger *log.Logger) error {
	cmd := exec.CommandContext(ctx, "sc", string(action), name)
	if output, err := cmd.CombinedOutput(); err != nil {
		if action == serviceActionStop && bytes.Contains(bytes.ToLower(output), []byte("service has not been started")) {
			logger.Printf("service %s already stopped", name)
		} else {
			return fmt.Errorf("sc %s failed: %w (%s)", action, err, strings.TrimSpace(string(output)))
		}
	}
	desired := stateRunning
	if action == serviceActionStop {
		desired = stateStopped
	}
	return waitForWindowsState(ctx, name, desired)
}

func waitForWindowsState(ctx context.Context, name string, desired serviceState) error {
	deadline := time.Now().Add(60 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		state, err := queryWindowsState(name)
		if err == nil && state == desired {
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return err
			}
			return fmt.Errorf("service %s did not reach state %s", name, desired)
		}
		time.Sleep(2 * time.Second)
	}
}

func queryWindowsState(name string) (serviceState, error) {
	cmd := exec.Command("sc", "query", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("query service: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "STATE") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				state := strings.ToLower(parts[2])
				switch state {
				case "running":
					return stateRunning, nil
				case "stopped":
					return stateStopped, nil
				case "stop_pending":
					return stateStopPending, nil
				}
			}
		}
	}
	return "", fmt.Errorf("unable to parse service state for %s", name)
}

func controlSystemdService(ctx context.Context, name string, action serviceAction, logger *log.Logger) error {
	command := exec.CommandContext(ctx, "systemctl", string(action), name)
	if output, err := command.CombinedOutput(); err != nil {
		if action == serviceActionStop && bytes.Contains(bytes.ToLower(output), []byte("not loaded")) {
			logger.Printf("service %s already stopped", name)
		} else {
			return fmt.Errorf("systemctl %s failed: %w (%s)", action, err, strings.TrimSpace(string(output)))
		}
	}
	desired := "active"
	if action == serviceActionStop {
		desired = "inactive"
	}
	return waitForSystemdState(ctx, name, desired)
}

func waitForSystemdState(ctx context.Context, name, desired string) error {
	deadline := time.Now().Add(60 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		state, err := querySystemdState(name)
		if err == nil && state == desired {
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return err
			}
			return fmt.Errorf("service %s did not reach %s", name, desired)
		}
		time.Sleep(2 * time.Second)
	}
}

func querySystemdState(name string) (string, error) {
	cmd := exec.Command("systemctl", "is-active", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "inactive" && strings.Contains(err.Error(), "exit status 3") {
			return trimmed, nil
		}
		return "", fmt.Errorf("systemctl is-active failed: %w (%s)", err, trimmed)
	}
	return strings.TrimSpace(string(output)), nil
}
