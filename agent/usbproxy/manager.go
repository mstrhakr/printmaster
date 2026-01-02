package usbproxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Manager manages USB printer web UI proxies
type Manager struct {
	mu         sync.RWMutex
	config     Config
	logger     Logger
	enumerator USBDeviceEnumerator

	// printers tracks all discovered USB printers with IPP-USB capability
	printers map[string]*USBPrinter // keyed by device path

	// sessions tracks active proxy sessions
	sessions map[string]*ProxySession // keyed by device path

	// running state
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running bool
}

// NewManager creates a new USB proxy manager
func NewManager(config Config) (*Manager, error) {
	if !IsSupported() {
		return nil, fmt.Errorf("USB proxy not supported on this platform")
	}

	if config.Logger == nil {
		config.Logger = nullLogger{}
	}

	enumerator, err := NewEnumerator(config.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create USB enumerator: %w", err)
	}

	return &Manager{
		config:     config,
		logger:     config.Logger,
		enumerator: enumerator,
		printers:   make(map[string]*USBPrinter),
		sessions:   make(map[string]*ProxySession),
	}, nil
}

// Start begins the USB proxy manager
func (m *Manager) Start() error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return nil
	}

	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.running = true
	m.mu.Unlock()

	// Initial scan
	if err := m.Scan(); err != nil {
		m.logger.Warn("Initial USB scan failed", "error", err)
	}

	// Start background scanner
	m.wg.Add(1)
	go m.scanLoop()

	// Start session cleanup
	m.wg.Add(1)
	go m.cleanupLoop()

	m.logger.Info("USB proxy manager started")
	return nil
}

// Stop halts the USB proxy manager
func (m *Manager) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	m.cancel()
	m.mu.Unlock()

	m.wg.Wait()

	// Close all sessions
	m.mu.Lock()
	for _, session := range m.sessions {
		if session.Transport != nil {
			session.Transport.Close()
		}
	}
	m.sessions = make(map[string]*ProxySession)
	m.mu.Unlock()

	m.logger.Info("USB proxy manager stopped")
}

// IsRunning returns whether the manager is running
func (m *Manager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

// Scan enumerates USB printers and updates the printer list
func (m *Manager) Scan() error {
	printers, err := m.enumerator.Enumerate()
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	seen := make(map[string]bool)

	for _, p := range printers {
		seen[p.DevicePath] = true

		if existing, ok := m.printers[p.DevicePath]; ok {
			// Update existing printer
			existing.LastSeen = now
			existing.Status = USBPrinterStatusAvailable
			// Update any changed fields
			if p.SpoolerPortName != "" {
				existing.SpoolerPortName = p.SpoolerPortName
			}
		} else {
			// New printer
			p.FirstSeen = now
			p.LastSeen = now
			m.printers[p.DevicePath] = p
			m.logger.Info("Discovered USB printer",
				"manufacturer", p.Manufacturer,
				"product", p.Product,
				"serial", p.SerialNumber,
				"path", p.DevicePath)
		}
	}

	// Mark unseen printers as unavailable
	for path, p := range m.printers {
		if !seen[path] {
			p.Status = USBPrinterStatusUnavailable
		}
	}

	return nil
}

// GetPrinters returns all discovered USB printers
func (m *Manager) GetPrinters() []*USBPrinter {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*USBPrinter, 0, len(m.printers))
	for _, p := range m.printers {
		copy := *p
		result = append(result, &copy)
	}
	return result
}

// GetPrinter returns a specific printer by device path
func (m *Manager) GetPrinter(devicePath string) (*USBPrinter, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	p, ok := m.printers[devicePath]
	if !ok {
		return nil, false
	}
	copy := *p
	return &copy, true
}

// GetPrinterBySerial returns a printer by serial number
func (m *Manager) GetPrinterBySerial(serial string) (*USBPrinter, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, p := range m.printers {
		if strings.EqualFold(p.SerialNumber, serial) {
			copy := *p
			return &copy, true
		}
	}
	return nil, false
}

// GetPrinterBySpoolerPort returns a printer by Windows spooler port name
func (m *Manager) GetPrinterBySpoolerPort(portName string) (*USBPrinter, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, p := range m.printers {
		if strings.EqualFold(p.SpoolerPortName, portName) {
			copy := *p
			return &copy, true
		}
	}
	return nil, false
}

// GetOrCreateSession gets an existing session or creates a new one for a printer
func (m *Manager) GetOrCreateSession(devicePath string) (*ProxySession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for existing session
	if session, ok := m.sessions[devicePath]; ok {
		if session.Transport.IsOpen() {
			session.UpdateLastUsed()
			return session, nil
		}
		// Session exists but transport closed - remove it
		delete(m.sessions, devicePath)
	}

	// Get printer info
	printer, ok := m.printers[devicePath]
	if !ok {
		return nil, fmt.Errorf("printer not found: %s", devicePath)
	}

	if printer.Status == USBPrinterStatusUnavailable {
		return nil, fmt.Errorf("printer unavailable: %s", devicePath)
	}

	// Create transport
	transport, err := m.enumerator.CreateTransport(printer)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport: %w", err)
	}

	// Open transport
	if err := transport.Open(); err != nil {
		return nil, fmt.Errorf("failed to open transport: %w", err)
	}

	// Create session
	now := time.Now()
	session := &ProxySession{
		Printer:   printer,
		Transport: transport,
		StartedAt: now,
		LastUsed:  now,
	}

	// Create HTTP handler for this session
	session.Handler = m.createProxyHandler(session)

	m.sessions[devicePath] = session
	printer.Status = USBPrinterStatusConnected

	m.logger.Info("Created USB proxy session",
		"device", devicePath,
		"product", printer.Product)

	return session, nil
}

// CloseSession closes a specific proxy session
func (m *Manager) CloseSession(devicePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[devicePath]
	if !ok {
		return nil
	}

	if session.Transport != nil {
		session.Transport.Close()
	}

	if printer, ok := m.printers[devicePath]; ok {
		printer.Status = USBPrinterStatusAvailable
	}

	delete(m.sessions, devicePath)

	m.logger.Info("Closed USB proxy session", "device", devicePath)
	return nil
}

// GetSession returns an existing session if one exists
func (m *Manager) GetSession(devicePath string) (*ProxySession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, ok := m.sessions[devicePath]
	if ok && session.Transport.IsOpen() {
		return session, true
	}
	return nil, false
}

// GetTransportForSerial returns an http.RoundTripper for a USB printer by serial
// This allows using USB transport with httputil.ReverseProxy for URL rewriting
func (m *Manager) GetTransportForSerial(serial string) (http.RoundTripper, error) {
	printer, found := m.GetPrinterForProxy(serial)
	if !found {
		return nil, fmt.Errorf("USB printer not found: %s", serial)
	}

	session, err := m.GetOrCreateSession(printer.DevicePath)
	if err != nil {
		return nil, err
	}

	return session.Transport, nil
}

// ProxyRequest handles a proxy request for a USB printer
func (m *Manager) ProxyRequest(w http.ResponseWriter, r *http.Request, devicePath string) {
	session, err := m.GetOrCreateSession(devicePath)
	if err != nil {
		m.logger.Error("Failed to get session", "device", devicePath, "error", err)
		http.Error(w, fmt.Sprintf("Failed to connect to USB printer: %v", err), http.StatusBadGateway)
		return
	}

	session.Handler.ServeHTTP(w, r)
}

// createProxyHandler creates an HTTP handler for a proxy session
func (m *Manager) createProxyHandler(session *ProxySession) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session.UpdateLastUsed()

		// Strip the /proxy/<serial> prefix from the path
		// The incoming request has path like /proxy/SERIAL/path/to/resource
		// We need to send just /path/to/resource to the printer
		originalPath := r.URL.Path
		strippedPath := originalPath

		// Find and strip the /proxy/<serial> prefix
		if strings.HasPrefix(originalPath, "/proxy/") {
			parts := strings.SplitN(strings.TrimPrefix(originalPath, "/proxy/"), "/", 2)
			if len(parts) >= 2 {
				strippedPath = "/" + parts[1]
			} else {
				strippedPath = "/"
			}
		}

		// Clone the request and update the path
		proxyReq := r.Clone(r.Context())
		proxyReq.URL.Path = strippedPath
		proxyReq.RequestURI = "" // Must clear for client requests

		m.logger.Debug("USB proxy request",
			"original_path", originalPath,
			"stripped_path", strippedPath,
			"method", r.Method)

		// Send request via USB transport
		resp, err := session.Transport.RoundTrip(proxyReq)
		if err != nil {
			m.logger.Error("USB transport error",
				"device", session.Printer.DevicePath,
				"error", err,
				"path", strippedPath)
			http.Error(w, fmt.Sprintf("USB transport error: %v", err), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		// Copy response headers
		copyResponseHeaders(w.Header(), resp.Header)

		// Rewrite Location headers for redirects to include proxy prefix
		if loc := resp.Header.Get("Location"); loc != "" {
			if strings.HasPrefix(loc, "/") {
				// Add back the proxy prefix for redirects
				serial := session.Printer.SerialNumber
				if serial == "" {
					serial = session.Printer.SpoolerPortName
				}
				w.Header().Set("Location", "/proxy/"+serial+loc)
			}
		}

		// Write status code
		w.WriteHeader(resp.StatusCode)

		// Copy body
		io.Copy(w, resp.Body)
	})
}

// scanLoop periodically scans for USB printers
func (m *Manager) scanLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.config.ScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			if err := m.Scan(); err != nil {
				m.logger.Warn("USB scan failed", "error", err)
			}
		}
	}
}

// cleanupLoop closes idle sessions
func (m *Manager) cleanupLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.cleanupIdleSessions()
		}
	}
}

// cleanupIdleSessions closes sessions that have been idle too long
func (m *Manager) cleanupIdleSessions() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for path, session := range m.sessions {
		session.mu.Lock()
		idleTime := now.Sub(session.LastUsed)
		session.mu.Unlock()

		if idleTime > m.config.IdleTimeout {
			m.logger.Info("Closing idle USB session",
				"device", path,
				"idle_time", idleTime)

			if session.Transport != nil {
				session.Transport.Close()
			}

			if printer, ok := m.printers[path]; ok {
				printer.Status = USBPrinterStatusAvailable
			}

			delete(m.sessions, path)
		}
	}
}

// copyResponseHeaders copies response headers, filtering hop-by-hop
func copyResponseHeaders(dst, src http.Header) {
	hopByHop := map[string]bool{
		"Connection":          true,
		"Keep-Alive":          true,
		"Proxy-Authenticate":  true,
		"Proxy-Authorization": true,
		"Proxy-Connection":    true,
		"Te":                  true,
		"Trailer":             true,
		"Transfer-Encoding":   true,
		"Upgrade":             true,
	}

	for key, values := range src {
		if hopByHop[key] {
			continue
		}
		for _, v := range values {
			dst.Add(key, v)
		}
	}
}

// USBPrinterInfo is a summary of USB printer info for the API
type USBPrinterInfo struct {
	DevicePath      string           `json:"device_path"`
	VendorID        string           `json:"vendor_id"`
	ProductID       string           `json:"product_id"`
	Manufacturer    string           `json:"manufacturer"`
	Product         string           `json:"product"`
	SerialNumber    string           `json:"serial_number,omitempty"`
	SpoolerPortName string           `json:"spooler_port_name,omitempty"`
	Status          USBPrinterStatus `json:"status"`
	HasActiveProxy  bool             `json:"has_active_proxy"`
}

// GetPrinterInfos returns printer info suitable for API responses
func (m *Manager) GetPrinterInfos() []USBPrinterInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]USBPrinterInfo, 0, len(m.printers))
	for path, p := range m.printers {
		_, hasSession := m.sessions[path]
		info := USBPrinterInfo{
			DevicePath:      p.DevicePath,
			VendorID:        fmt.Sprintf("%04X", p.VendorID),
			ProductID:       fmt.Sprintf("%04X", p.ProductID),
			Manufacturer:    p.Manufacturer,
			Product:         p.Product,
			SerialNumber:    p.SerialNumber,
			SpoolerPortName: p.SpoolerPortName,
			Status:          p.Status,
			HasActiveProxy:  hasSession,
		}
		result = append(result, info)
	}
	return result
}

// CanProxySerial checks if a serial number corresponds to a USB printer we can proxy
// This is used by the main proxy handler to determine routing
func (m *Manager) CanProxySerial(serial string) bool {
	if serial == "" {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, p := range m.printers {
		if p.Status != USBPrinterStatusUnavailable &&
			(strings.EqualFold(p.SerialNumber, serial) ||
				strings.EqualFold(p.SpoolerPortName, serial)) {
			return true
		}
	}
	return false
}

// GetPrinterForProxy finds a USB printer by serial or port name for proxying
func (m *Manager) GetPrinterForProxy(identifier string) (*USBPrinter, bool) {
	if identifier == "" {
		return nil, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Try exact serial match first
	for _, p := range m.printers {
		if strings.EqualFold(p.SerialNumber, identifier) && p.Status != USBPrinterStatusUnavailable {
			copy := *p
			return &copy, true
		}
	}

	// Try spooler port name
	for _, p := range m.printers {
		if strings.EqualFold(p.SpoolerPortName, identifier) && p.Status != USBPrinterStatusUnavailable {
			copy := *p
			return &copy, true
		}
	}

	return nil, false
}

// ProxyRequestBySerial handles a proxy request by serial/identifier lookup
// This is the main integration point with the agent's proxy handler
func (m *Manager) ProxyRequestBySerial(w http.ResponseWriter, r *http.Request, identifier string) bool {
	printer, found := m.GetPrinterForProxy(identifier)
	if !found {
		return false // Not a USB printer we can handle
	}

	m.ProxyRequest(w, r, printer.DevicePath)
	return true
}
