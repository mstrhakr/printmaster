package main

import (
	"context"
	"strings"
	"sync"

	"printmaster/agent/agent"
	"printmaster/agent/storage"
	"printmaster/common/logger"
)

const identityRefreshWorkers = 5

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
			result.Skipped++
			continue
		}
		result.Attempted++
		jobs <- device
	}
	close(jobs)
	workers.Wait()
	return result
}

// runPostUpdateIdentityRefresh applies the identity-maintenance workflow after
// a verified agent update. Other maintenance triggers can reuse it directly.
func runPostUpdateIdentityRefresh(ctx context.Context, store storage.DeviceStore, log *logger.Logger) {
	if store == nil {
		return
	}

	devices, err := store.List(ctx, storage.DeviceFilter{})
	if err != nil {
		log.Warn("Post-update identity refresh: failed to list devices", "error", err)
		return
	}

	adapter := &deviceStorageAdapter{store: store}
	result := refreshKnownDeviceIdentities(ctx, devices, LiveDiscoveryDetect, adapter.StoreDiscoveredDevice)
	log.Info("Post-update identity refresh completed",
		"attempted", result.Attempted,
		"refreshed", result.Refreshed,
		"skipped", result.Skipped,
		"failed", result.Failed)
}
