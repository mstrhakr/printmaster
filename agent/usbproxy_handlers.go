//go:build windows
// +build windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"printmaster/agent/usbproxy"
	"printmaster/agent/usbproxy/metrics"
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

// GetUSBTransportForSerial returns an http.RoundTripper for a USB printer
// This allows the main proxy handler to use USB transport with standard reverse proxy
func GetUSBTransportForSerial(serial string) (http.RoundTripper, error) {
	usbProxyManagerMu.RLock()
	manager := usbProxyManager
	usbProxyManagerMu.RUnlock()

	if manager == nil {
		return nil, fmt.Errorf("USB proxy not available")
	}
	return manager.GetTransportForSerial(serial)
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
	fmt.Println("============================================")
	fmt.Println("[USB-HANDLERS] RegisterUSBProxyHandlers CALLED")
	fmt.Println("============================================")

	// GET /api/usb-printers - list USB printers with IPP-USB capability
	http.HandleFunc("/api/usb-printers", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[DEBUG] Handler /api/usb-printers called: path=%s", r.URL.Path)
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

	// GET /api/usb-printers/{serial}/metrics - collect metrics from USB printer via web scraping
	http.HandleFunc("/api/usb-printers/metrics/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(os.Stderr, "\n[USB-METRICS] >>> HANDLER ENTRY: %s\n", r.URL.Path)
		fmt.Println("[USB-METRICS] >>> HANDLER ENTRY (stdout):", r.URL.Path)

		// Recover from panics
		defer func() {
			if err := recover(); err != nil {
				log.Printf("[ERROR] USB metrics handler panic: %v", err)
				http.Error(w, fmt.Sprintf("Internal error: %v", err), http.StatusInternalServerError)
			}
		}()

		log.Printf("[DEBUG] USB metrics request: %s", r.URL.Path)

		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}

		// Extract serial from path: /api/usb-printers/metrics/{serial}
		path := strings.TrimPrefix(r.URL.Path, "/api/usb-printers/metrics/")
		serial := strings.TrimSuffix(path, "/")
		if serial == "" {
			http.Error(w, "serial required", http.StatusBadRequest)
			return
		}

		log.Printf("[DEBUG] USB metrics: looking up printer serial=%s", serial)

		usbProxyManagerMu.RLock()
		manager := usbProxyManager
		usbProxyManagerMu.RUnlock()

		if manager == nil {
			http.Error(w, "USB proxy not available", http.StatusServiceUnavailable)
			return
		}

		// Get printer info
		printer, found := manager.GetPrinterBySerial(serial)
		if !found {
			http.Error(w, "USB printer not found: "+serial, http.StatusNotFound)
			return
		}

		log.Printf("[DEBUG] USB metrics: found printer manufacturer=%s product=%s vendorID=%04X",
			printer.Manufacturer, printer.Product, printer.VendorID)

		// Get transport for the printer
		transport, err := manager.GetTransportForSerial(serial)
		if err != nil {
			http.Error(w, "Failed to get transport: "+err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("[DEBUG] USB metrics: got transport, creating collector")

		// Create metrics collector
		collector := metrics.NewCollector(transport, printer.Manufacturer, printer.Product, printer.VendorID)

		log.Printf("[DEBUG] USB metrics: collector created, vendor=%s", collector.Vendor.Name())

		// Collect metrics with timeout - USB is slow, allow 90 seconds for multiple endpoint probes
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()

		log.Printf("[DEBUG] USB metrics: starting collection (this may take 30+ seconds via USB)")

		metricsData, err := collector.Collect(ctx)
		if err != nil {
			log.Printf("[ERROR] USB metrics: collection failed: %v", err)
			http.Error(w, "Failed to collect metrics: "+err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("[DEBUG] USB metrics: collection complete, total_pages=%d source=%s",
			metricsData.TotalPages, metricsData.Source)

		// Fill in identity from printer info if not scraped
		if metricsData.Serial == "" {
			metricsData.Serial = printer.SerialNumber
		}
		if metricsData.Manufacturer == "" {
			metricsData.Manufacturer = printer.Manufacturer
		}
		if metricsData.Model == "" {
			metricsData.Model = printer.Product
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(metricsData)
	})

	// GET /api/usb-printers/{serial}/probe - probe all endpoints and return results
	http.HandleFunc("/api/usb-printers/probe/", func(w http.ResponseWriter, r *http.Request) {
		// Recover from panics
		defer func() {
			if err := recover(); err != nil {
				log.Printf("[ERROR] USB probe handler panic: %v", err)
				http.Error(w, fmt.Sprintf("Internal error: %v", err), http.StatusInternalServerError)
			}
		}()

		log.Printf("[DEBUG] USB probe request: %s", r.URL.Path)

		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}

		// Extract serial from path
		path := strings.TrimPrefix(r.URL.Path, "/api/usb-printers/probe/")
		serial := strings.TrimSuffix(path, "/")
		if serial == "" {
			http.Error(w, "serial required", http.StatusBadRequest)
			return
		}

		log.Printf("[DEBUG] USB probe: looking up printer serial=%s", serial)

		usbProxyManagerMu.RLock()
		manager := usbProxyManager
		usbProxyManagerMu.RUnlock()

		if manager == nil {
			http.Error(w, "USB proxy not available", http.StatusServiceUnavailable)
			return
		}

		// Get printer info
		printer, found := manager.GetPrinterBySerial(serial)
		if !found {
			http.Error(w, "USB printer not found: "+serial, http.StatusNotFound)
			return
		}

		log.Printf("[DEBUG] USB probe: found printer manufacturer=%s product=%s vendorID=%04X",
			printer.Manufacturer, printer.Product, printer.VendorID)

		// Get transport
		transport, err := manager.GetTransportForSerial(serial)
		if err != nil {
			http.Error(w, "Failed to get transport: "+err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("[DEBUG] USB probe: got transport")

		// Detect vendor and get endpoints
		vendor := metrics.DetectVendor(printer.Manufacturer, printer.Product, printer.VendorID)
		endpoints := vendor.Endpoints()

		log.Printf("[DEBUG] USB probe: detected vendor=%s, endpoints=%d", vendor.Name(), len(endpoints))

		// Probe each endpoint
		type probeResult struct {
			Path        string `json:"path"`
			Description string `json:"description"`
			Status      int    `json:"status"`
			ContentType string `json:"content_type"`
			Size        int    `json:"size"`
			Error       string `json:"error,omitempty"`
			Preview     string `json:"preview,omitempty"`
		}

		results := make([]probeResult, 0, len(endpoints))
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()

		for i, ep := range endpoints {
			log.Printf("[DEBUG] USB probe: probing endpoint %d/%d path=%s", i+1, len(endpoints), ep.Path)

			result := probeResult{
				Path:        ep.Path,
				Description: ep.Description,
			}

			// Create request with short timeout per endpoint
			epCtx, epCancel := context.WithTimeout(ctx, 10*time.Second)
			req, err := http.NewRequestWithContext(epCtx, "GET", "http://usb-printer"+ep.Path, nil)
			if err != nil {
				epCancel()
				result.Error = err.Error()
				results = append(results, result)
				continue
			}

			resp, err := transport.RoundTrip(req)
			epCancel()
			if err != nil {
				log.Printf("[DEBUG] USB probe: endpoint %s failed: %v", ep.Path, err)
				result.Error = err.Error()
				results = append(results, result)
				continue
			}

			result.Status = resp.StatusCode
			result.ContentType = resp.Header.Get("Content-Type")

			// Read body preview
			body := make([]byte, 4096)
			n, _ := resp.Body.Read(body)
			resp.Body.Close()

			result.Size = n
			log.Printf("[DEBUG] USB probe: endpoint %s status=%d size=%d", ep.Path, result.Status, result.Size)

			if n > 0 && result.Status == http.StatusOK {
				// Show preview (truncated, safe characters only)
				preview := string(body[:n])
				if len(preview) > 500 {
					preview = preview[:500] + "..."
				}
				result.Preview = preview
			}

			results = append(results, result)
		}

		log.Printf("[DEBUG] USB probe: complete, returning %d results", len(results))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"serial":       serial,
			"manufacturer": printer.Manufacturer,
			"product":      printer.Product,
			"vendor":       vendor.Name(),
			"endpoints":    results,
		})
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
