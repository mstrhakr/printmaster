package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"printmaster/agent/agent"
	"printmaster/agent/storage"
	"printmaster/common/logger"
)

const (
	identityRefreshWorkers          = 5
	identityRefreshVersionConfigKey = "identity_refresh_version"
)

type identityRefreshResult struct {
	Attempted int
	Refreshed int
	Skipped   int
	Failed    int
}

// refreshKnownDeviceIdentities refreshes identity metadata only for known
// devices. A response is persisted only when it reports the same serial as
// the stored device, preventing a reused IP from overwriting another record.
func refreshKnownDeviceIdentities(
	ctx context.Context,
	devices []*storage.Device,
	refresh func(context.Context, string, int) (*agent.PrinterInfo, error),
	persist func(context.Context, agent.PrinterInfo) error,
) identityRefreshResult {
	result := identityRefreshResult{}
	jobs := make(chan *storage.Device)
	var mu sync.Mutex
	var workers sync.WaitGroup

	for range identityRefreshWorkers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for device := range jobs {
				if ctx.Err() != nil {
					return
				}

				pi, err := refresh(ctx, device.IP, 10)
				if err != nil {
					mu.Lock()
					result.Failed++
					mu.Unlock()
					continue
				}
				if pi == nil || pi.Manufacturer == "" || !strings.EqualFold(strings.TrimSpace(pi.Serial), strings.TrimSpace(device.Serial)) {
					mu.Lock()
					result.Skipped++
					mu.Unlock()
					continue
				}

				pi.Serial = device.Serial
				pi.DiscoveryMethods = append(pi.DiscoveryMethods, "post-update-identity-refresh")
				if err := persist(ctx, *pi); err != nil {
					mu.Lock()
					result.Failed++
					mu.Unlock()
					continue
				}

				mu.Lock()
				result.Refreshed++
				mu.Unlock()
			}
		}()
	}

	for _, device := range devices {
		if device == nil || device.IP == "" || device.Serial == "" {
			mu.Lock()
			result.Skipped++
			mu.Unlock()
			continue
		}
		mu.Lock()
		result.Attempted++
		mu.Unlock()
		select {
		case jobs <- device:
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return result
		}
	}
	close(jobs)
	workers.Wait()
	return result
}

func currentIdentityRefreshVersion() string {
	if strings.TrimSpace(GitCommit) == "" {
		return Version
	}
	return Version + "+" + GitCommit
}

func shouldRefreshIdentitiesForVersion(configStore storage.AgentConfigStore, version string) (bool, error) {
	if configStore == nil {
		return false, fmt.Errorf("agent config store is unavailable")
	}

	var lastRefreshedVersion string
	if err := configStore.GetConfigValue(identityRefreshVersionConfigKey, &lastRefreshedVersion); err != nil {
		return false, err
	}
	return lastRefreshedVersion != version, nil
}

func refreshKnownDeviceIdentitiesFromStore(ctx context.Context, store storage.DeviceStore, log *logger.Logger) (identityRefreshResult, error) {
	if store == nil {
		return identityRefreshResult{}, fmt.Errorf("device store is unavailable")
	}

	devices, err := store.List(ctx, storage.DeviceFilter{})
	if err != nil {
		return identityRefreshResult{}, err
	}

	adapter := &deviceStorageAdapter{store: store}
	result := refreshKnownDeviceIdentities(ctx, devices, LiveDiscoveryDetect, adapter.StoreDiscoveredDevice)
	log.Info("Identity refresh completed",
		"attempted", result.Attempted,
		"refreshed", result.Refreshed,
		"skipped", result.Skipped,
		"failed", result.Failed)
	return result, nil
}

// startIdentityRefreshForVersionChange refreshes known devices when the
// running build differs from the build that last completed a refresh. The
// marker is written only after all queried devices are processed without
// query or persistence failures, so an interrupted refresh retries later.
func startIdentityRefreshForVersionChange(ctx context.Context, configStore storage.AgentConfigStore, store storage.DeviceStore, log *logger.Logger) {
	version := currentIdentityRefreshVersion()
	shouldRefresh, err := shouldRefreshIdentitiesForVersion(configStore, version)
	if err != nil {
		log.Warn("Identity refresh version check failed", "error", err)
		return
	}
	if !shouldRefresh {
		return
	}

	log.Info("Identity refresh scheduled for new agent version", "version", version)
	go func() {
		result, err := refreshKnownDeviceIdentitiesFromStore(ctx, store, log)
		if err != nil {
			log.Warn("Identity refresh failed", "error", err)
			return
		}
		if result.Failed > 0 {
			log.Warn("Identity refresh incomplete; it will retry on the next startup", "failed", result.Failed)
			return
		}
		if err := configStore.SetConfigValue(identityRefreshVersionConfigKey, version); err != nil {
			log.Warn("Failed to record identity refresh version", "error", err, "version", version)
		}
	}()
}
