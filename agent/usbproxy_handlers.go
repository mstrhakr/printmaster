//go:build windows
// +build windows

package main

import (
	"encoding/json"
	"net/http"
	"sync"

	"printmaster/agent/usbproxy"
)

var (
	usbProxyManager   *usbproxy.Manager
	usbProxyManagerMu sync.RWMutex
)

// InitUSBProxy initializes the USB proxy manager
func InitUSBProxy(logger Logger) error {
	if !usbproxy.IsSupported() {
		if logger != nil {
			logger.Info("USB proxy not supported on this platform")
		}
		return nil
	}

	config := usbproxy.DefaultConfig()
	config.Logger = &usbProxyLoggerAdapter{logger: logger}

	manager, err := usbproxy.NewManager(config)
	if err != nil {
		return err
	}

	if err := manager.Start(); err != nil {
		return err
	}

	usbProxyManagerMu.Lock()
	usbProxyManager = manager
	usbProxyManagerMu.Unlock()

	if logger != nil {
		logger.Info("USB proxy manager started")
	}

	return nil
}

// StopUSBProxy stops the USB proxy manager
func StopUSBProxy() {
	usbProxyManagerMu.Lock()
	defer usbProxyManagerMu.Unlock()

	if usbProxyManager != nil {
		usbProxyManager.Stop()
		usbProxyManager = nil
	}
}

// GetUSBProxyManager returns the USB proxy manager (may be nil)
func GetUSBProxyManager() *usbproxy.Manager {
	usbProxyManagerMu.RLock()
	defer usbProxyManagerMu.RUnlock()
	return usbProxyManager
}

// CanUSBProxySerial checks if a serial corresponds to a USB printer we can proxy
// This is called by the main proxy handler to determine routing
func CanUSBProxySerial(serial string) bool {
	usbProxyManagerMu.RLock()
	manager := usbProxyManager
	usbProxyManagerMu.RUnlock()

	if manager == nil {
		return false
	}
	return manager.CanProxySerial(serial)
}

// HandleUSBProxy handles proxy requests for USB printers
// Returns true if the request was handled, false if it should fall through to network proxy
func HandleUSBProxy(w http.ResponseWriter, r *http.Request, serial string) bool {
	usbProxyManagerMu.RLock()
	manager := usbProxyManager
	usbProxyManagerMu.RUnlock()

	if manager == nil {
		return false
	}
	return manager.ProxyRequestBySerial(w, r, serial)
}

// RegisterUSBProxyHandlers registers USB proxy API endpoints
func RegisterUSBProxyHandlers() {
	// GET /api/usb-printers - list USB printers with IPP-USB capability
	http.HandleFunc("/api/usb-printers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}

		usbProxyManagerMu.RLock()
		manager := usbProxyManager
		usbProxyManagerMu.RUnlock()

		if manager == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"supported": false,
				"printers":  []interface{}{},
			})
			return
		}

		printers := manager.GetPrinterInfos()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"supported": true,
			"running":   manager.IsRunning(),
			"printers":  printers,
		})
	})

	// POST /api/usb-printers/scan - trigger a USB device scan
	http.HandleFunc("/api/usb-printers/scan", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}

		usbProxyManagerMu.RLock()
		manager := usbProxyManager
		usbProxyManagerMu.RUnlock()

		if manager == nil {
			http.Error(w, "USB proxy not available", http.StatusServiceUnavailable)
			return
		}

		if err := manager.Scan(); err != nil {
			http.Error(w, "scan failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		printers := manager.GetPrinterInfos()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":  true,
			"printers": printers,
		})
	})

	// GET /api/usb-printers/status - check USB proxy status
	http.HandleFunc("/api/usb-printers/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}

		supported := usbproxy.IsSupported()

		usbProxyManagerMu.RLock()
		manager := usbProxyManager
		usbProxyManagerMu.RUnlock()

		status := map[string]interface{}{
			"supported": supported,
			"running":   false,
		}

		if manager != nil {
			status["running"] = manager.IsRunning()
			printers := manager.GetPrinters()
			status["printer_count"] = len(printers)

			// Count active sessions
			activeCount := 0
			for _, p := range manager.GetPrinterInfos() {
				if p.HasActiveProxy {
					activeCount++
				}
			}
			status["active_sessions"] = activeCount
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	})
}

// usbProxyLoggerAdapter adapts the main Logger to usbproxy.Logger
type usbProxyLoggerAdapter struct {
	logger Logger
}

func (a *usbProxyLoggerAdapter) Error(msg string, context ...interface{}) {
	if a.logger != nil {
		a.logger.Error(msg, context...)
	}
}

func (a *usbProxyLoggerAdapter) Warn(msg string, context ...interface{}) {
	if a.logger != nil {
		a.logger.Warn(msg, context...)
	}
}

func (a *usbProxyLoggerAdapter) Info(msg string, context ...interface{}) {
	if a.logger != nil {
		a.logger.Info(msg, context...)
	}
}

func (a *usbProxyLoggerAdapter) Debug(msg string, context ...interface{}) {
	if a.logger != nil {
		a.logger.Debug(msg, context...)
	}
}
