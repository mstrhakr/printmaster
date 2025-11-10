// Printer/Copier Fleet Management Agent in Go
// Cross-platform agent for SNMP printer discovery and reporting
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"embed"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"html/template"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"printmaster/agent/agent"
	"printmaster/agent/proxy"
	"printmaster/agent/scanner"
	"printmaster/agent/storage"
	"printmaster/common/config"
	"printmaster/common/logger"
	commonutil "printmaster/common/util"
	sharedweb "printmaster/common/web"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/kardianos/service"
)

// Version information (set at build time via -ldflags)
var (
	Version   = "dev"     // Semantic version (e.g., "1.0.0")
	BuildTime = "unknown" // Build timestamp
	GitCommit = "unknown" // Git commit hash
	BuildType = "dev"     // "dev" or "release"
)

//go:embed web
var webFS embed.FS

// loggingResponseWriter captures status code and byte count for diagnostics
type loggingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.status = code
	lrw.ResponseWriter.WriteHeader(code)
}

func (lrw *loggingResponseWriter) Write(b []byte) (int, error) {
	n, err := lrw.ResponseWriter.Write(b)
	lrw.bytes += n
	return n, err
}

// Flush proxies Flush to the underlying writer when supported
func (lrw *loggingResponseWriter) Flush() {
	if f, ok := lrw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// ReadFrom ensures io.Copy can use an optimized path while still counting bytes
func (lrw *loggingResponseWriter) ReadFrom(r io.Reader) (int64, error) {
	// Use io.Copy which will call lrw.Write, preserving the byte counter
	return io.Copy(lrw, r)
}

// basicAuth returns base64 of user:pass per RFC7617
func basicAuth(userpass string) string {
	return base64.StdEncoding.EncodeToString([]byte(userpass))
}

// Global session cache for form-based logins
var proxySessionCache = proxy.NewSessionCache()

// Context key for storing whether the original request was HTTPS
type contextKey string

const isHTTPSContextKey contextKey = "isHTTPS"

// Static resource cache to avoid hitting slow printers repeatedly
type staticResourceCache struct {
	sync.RWMutex
	items map[string]cachedResource
}

type cachedResource struct {
	data        []byte
	contentType string
	headers     http.Header
	expiry      time.Time
}

func newStaticResourceCache() *staticResourceCache {
	return &staticResourceCache{items: make(map[string]cachedResource)}
}

func (c *staticResourceCache) Get(key string) ([]byte, string, http.Header, bool) {
	c.RLock()
	defer c.RUnlock()
	item, ok := c.items[key]
	if !ok || time.Now().After(item.expiry) {
		return nil, "", nil, false
	}
	return item.data, item.contentType, item.headers, true
}

func (c *staticResourceCache) Set(key string, data []byte, contentType string, headers http.Header, ttl time.Duration) {
	c.Lock()
	defer c.Unlock()
	c.items[key] = cachedResource{
		data:        data,
		contentType: contentType,
		headers:     headers,
		expiry:      time.Now().Add(ttl),
	}
}

var staticCache = newStaticResourceCache()

var (
	// deviceStore is the global device storage interface
	deviceStore storage.DeviceStore
	// agentConfigStore handles agent configuration (IP ranges, settings, etc.)
	agentConfigStore storage.AgentConfigStore

	// Global structured logger
	appLogger *logger.Logger

	// Global scanner configuration
	scannerConfig struct {
		sync.RWMutex
		SNMPTimeoutMs       int
		SNMPRetries         int
		DiscoverConcurrency int
	}
)

// runGarbageCollection runs periodic cleanup of old scan history and hidden devices
func runGarbageCollection(ctx context.Context, store storage.DeviceStore, config *agent.RetentionConfig) {
	ticker := time.NewTicker(24 * time.Hour) // Run daily
	defer ticker.Stop()

	// Check if context is already cancelled before running
	select {
	case <-ctx.Done():
		return
	default:
		// Run immediately on startup
		doGarbageCollection(store, config)
	}

	for {
		select {
		case <-ticker.C:
			doGarbageCollection(store, config)
		case <-ctx.Done():
			return
		}
	}
}

// doGarbageCollection performs the actual cleanup work
func doGarbageCollection(store storage.DeviceStore, config *agent.RetentionConfig) {
	ctx := context.Background()

	// Calculate cutoff timestamps
	scanHistoryCutoff := time.Now().AddDate(0, 0, -config.ScanHistoryDays).Unix()
	hiddenDevicesCutoff := time.Now().AddDate(0, 0, -config.HiddenDevicesDays).Unix()

	// Delete old scan history
	if scansDeleted, err := store.DeleteOldScans(ctx, scanHistoryCutoff); err != nil {
		appLogger.Error("Garbage collection: Failed to delete old scans", "error", err, "cutoff_days", config.ScanHistoryDays)
	} else if scansDeleted > 0 {
		appLogger.Info("Garbage collection: Deleted old scan history", "count", scansDeleted, "age_days", config.ScanHistoryDays)
	}

	// Delete old hidden devices
	if devicesDeleted, err := store.DeleteOldHiddenDevices(ctx, hiddenDevicesCutoff); err != nil {
		appLogger.Error("Garbage collection: Failed to delete old hidden devices", "error", err, "cutoff_days", config.HiddenDevicesDays)
	} else if devicesDeleted > 0 {
		appLogger.Info("Garbage collection: Deleted old hidden devices", "count", devicesDeleted, "age_days", config.HiddenDevicesDays)
	}
}

// runMetricsDownsampler runs periodic downsampling of metrics data
// This implements Netdata-style tiered storage: raw → hourly → daily → monthly
func runMetricsDownsampler(ctx context.Context, store storage.DeviceStore) {
	// Run every 6 hours (4 times per day)
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()

	// Run immediately on startup (with a small delay to let the app initialize)
	select {
	case <-time.After(30 * time.Second):
		doMetricsDownsampling(store)
	case <-ctx.Done():
		return
	}

	for {
		select {
		case <-ticker.C:
			doMetricsDownsampling(store)
		case <-ctx.Done():
			return
		}
	}
}

// doMetricsDownsampling performs the actual downsampling work
func doMetricsDownsampling(store storage.DeviceStore) {
	ctx := context.Background()

	appLogger.Info("Metrics downsampling: Starting tiered aggregation")

	// Perform full downsampling: raw→hourly, hourly→daily, daily→monthly, cleanup
	if err := store.PerformFullDownsampling(ctx); err != nil {
		appLogger.Error("Metrics downsampling: Failed", "error", err)
	} else {
		appLogger.Info("Metrics downsampling: Completed successfully")
	}
}

// ensureTLSCertificates generates or loads TLS certificates for HTTPS
// If customCertPath and customKeyPath are provided, uses those instead
func ensureTLSCertificates(customCertPath, customKeyPath string) (certFile, keyFile string, err error) {
	// If custom cert paths provided, validate and use them
	if customCertPath != "" && customKeyPath != "" {
		if _, err := os.Stat(customCertPath); err == nil {
			if _, err := os.Stat(customKeyPath); err == nil {
				appLogger.Info("Using custom TLS certificates", "cert", customCertPath, "key", customKeyPath)
				return customCertPath, customKeyPath, nil
			}
		}
		appLogger.Warn("Custom TLS certificate paths invalid, falling back to auto-generated", "cert", customCertPath, "key", customKeyPath)
	}

	// Get data directory
	dataDir, err := storage.GetDataDir("PrintMaster")
	if err != nil {
		return "", "", fmt.Errorf("failed to get data directory: %w", err)
	}

	certFile = filepath.Join(dataDir, "server.crt")
	keyFile = filepath.Join(dataDir, "server.key")

	// Check if certificates already exist
	if _, err := os.Stat(certFile); err == nil {
		if _, err := os.Stat(keyFile); err == nil {
			// Both files exist
			return certFile, keyFile, nil
		}
	}

	// Generate new self-signed certificate
	appLogger.Info("Generating self-signed TLS certificate")

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate private key: %w", err)
	}

	// Create certificate template
	notBefore := time.Now()
	notAfter := notBefore.Add(365 * 24 * time.Hour * 10) // 10 years

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", fmt.Errorf("failed to generate serial number: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"PrintMaster"},
			CommonName:   "PrintMaster Agent",
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}

	// Create self-signed certificate
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return "", "", fmt.Errorf("failed to create certificate: %w", err)
	}

	// Write certificate file
	certOut, err := os.Create(certFile)
	if err != nil {
		return "", "", fmt.Errorf("failed to create cert file: %w", err)
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		certOut.Close()
		return "", "", fmt.Errorf("failed to write cert: %w", err)
	}
	certOut.Close()

	// Write private key file
	keyOut, err := os.Create(keyFile)
	if err != nil {
		return "", "", fmt.Errorf("failed to create key file: %w", err)
	}
	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		keyOut.Close()
		return "", "", fmt.Errorf("failed to marshal private key: %w", err)
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}); err != nil {
		keyOut.Close()
		return "", "", fmt.Errorf("failed to write key: %w", err)
	}
	keyOut.Close()

	appLogger.Info("Generated self-signed TLS certificate", "cert", certFile, "key", keyFile)
	return certFile, keyFile, nil
}

// deviceStorageAdapter implements agent.DeviceStorage interface
type deviceStorageAdapter struct {
	store storage.DeviceStore
}

func (a *deviceStorageAdapter) StoreDiscoveredDevice(ctx context.Context, pi agent.PrinterInfo) error {
	// Convert PrinterInfo to Device
	device := storage.PrinterInfoToDevice(pi, false)
	device.Visible = true

	// Upsert device to database
	if err := a.store.Upsert(ctx, device); err != nil {
		return fmt.Errorf("failed to upsert device: %w", err)
	}

	// Add scan history snapshot (device state changes for audit trail)
	snapshot := storage.PrinterInfoToScanSnapshot(pi)
	if err := a.store.AddScanHistory(ctx, snapshot); err != nil {
		return fmt.Errorf("failed to add scan history: %w", err)
	}

	// Save metrics snapshot (page counts, toner levels for time-series analysis)
	metrics := storage.PrinterInfoToMetricsSnapshot(pi)
	if err := a.store.SaveMetricsSnapshot(ctx, metrics); err != nil {
		return fmt.Errorf("failed to save metrics snapshot: %w", err)
	}

	// Broadcast device update via SSE
	if sseHub != nil {
		isNew := device.FirstSeen.Equal(device.LastSeen) || time.Since(device.FirstSeen) < time.Second
		eventType := "device_updated"
		if isNew {
			eventType = "device_discovered"
		}
		sseHub.Broadcast(SSEEvent{
			Type: eventType,
			Data: map[string]interface{}{
				"serial": device.Serial,
				"ip":     device.IP,
				"make":   device.Manufacturer,
				"model":  device.Model,
			},
		})
	}

	return nil
}

// SSE (Server-Sent Events) Hub for real-time UI updates
type SSEEvent struct {
	Type string                 `json:"type"`
	Data map[string]interface{} `json:"data"`
}

type SSEClient struct {
	id     string
	events chan SSEEvent
}

type SSEHub struct {
	clients    map[string]*SSEClient
	broadcast  chan SSEEvent
	register   chan *SSEClient
	unregister chan *SSEClient
	shutdown   chan struct{}
	mu         sync.RWMutex
}

func NewSSEHub() *SSEHub {
	hub := &SSEHub{
		clients:    make(map[string]*SSEClient),
		broadcast:  make(chan SSEEvent, 100),
		register:   make(chan *SSEClient),
		unregister: make(chan *SSEClient),
		shutdown:   make(chan struct{}),
	}
	go hub.run()
	return hub
}

func (h *SSEHub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.id] = client
			h.mu.Unlock()
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.id]; ok {
				delete(h.clients, client.id)
				close(client.events)
			}
			h.mu.Unlock()
		case event := <-h.broadcast:
			h.mu.RLock()
			for _, client := range h.clients {
				select {
				case client.events <- event:
				default:
					// Client's buffer is full, skip
				}
			}
			h.mu.RUnlock()
		case <-h.shutdown:
			// Close all client connections
			h.mu.Lock()
			for _, client := range h.clients {
				close(client.events)
			}
			h.clients = make(map[string]*SSEClient)
			h.mu.Unlock()
			return
		}
	}
}

func (h *SSEHub) Stop() {
	close(h.shutdown)
}

func (h *SSEHub) Broadcast(event SSEEvent) {
	select {
	case h.broadcast <- event:
	default:
		// Broadcast buffer full, skip event
	}
}

func (h *SSEHub) NewClient() *SSEClient {
	client := &SSEClient{
		id:     fmt.Sprintf("client_%d", time.Now().UnixNano()),
		events: make(chan SSEEvent, 10),
	}
	h.register <- client
	return client
}

func (h *SSEHub) RemoveClient(client *SSEClient) {
	h.unregister <- client
}

var sseHub *SSEHub

// tryLearnOIDForValue performs an SNMP walk to find an OID that returns the specified value
// Returns the OID if found, empty string otherwise
func tryLearnOIDForValue(ctx context.Context, ip string, vendorHint string, fieldName string, targetValue interface{}) string {
	if ip == "" {
		return ""
	}

	// Convert target value to string for comparison
	targetStr := fmt.Sprintf("%v", targetValue)
	if targetStr == "" {
		return ""
	}

	// Perform a targeted SNMP walk on common MIB roots
	appLogger.Info("Attempting to learn OID for locked field", "ip", ip, "field", fieldName, "target_value", targetStr)

	result, err := scanner.QueryDevice(ctx, ip, scanner.QueryFull, vendorHint, 10)
	if err != nil {
		appLogger.Warn("Failed to query device for OID learning", "ip", ip, "error", err)
		return ""
	}

	if result == nil || len(result.PDUs) == 0 {
		return ""
	}

	// Search through PDUs for matching value
	for _, pdu := range result.PDUs {
		var pduValueStr string

		// Convert PDU value to string based on type
		switch pdu.Type {
		case gosnmp.OctetString:
			if bytes, ok := pdu.Value.([]byte); ok {
				pduValueStr = string(bytes)
			} else {
				pduValueStr = fmt.Sprintf("%v", pdu.Value)
			}
		case gosnmp.Integer, gosnmp.Counter32, gosnmp.Gauge32, gosnmp.Counter64:
			pduValueStr = fmt.Sprintf("%v", pdu.Value)
		default:
			pduValueStr = fmt.Sprintf("%v", pdu.Value)
		}

		// Check for exact match or numeric match
		if pduValueStr == targetStr {
			appLogger.Info("Found matching OID for field", "ip", ip, "field", fieldName, "oid", pdu.Name, "value", pduValueStr)
			return pdu.Name
		}

		// For numeric fields, try parsing and comparing as integers
		if strings.Contains(strings.ToLower(fieldName), "page") || strings.Contains(strings.ToLower(fieldName), "count") {
			targetInt, targetErr := strconv.ParseInt(targetStr, 10, 64)
			pduInt, pduErr := strconv.ParseInt(pduValueStr, 10, 64)
			if targetErr == nil && pduErr == nil && targetInt == pduInt {
				appLogger.Info("Found matching OID for numeric field", "ip", ip, "field", fieldName, "oid", pdu.Name, "value", pduInt)
				return pdu.Name
			}
		}
	}

	appLogger.Info("No matching OID found for field", "ip", ip, "field", fieldName, "target_value", targetStr, "pdus_checked", len(result.PDUs))
	return ""
}

func main() {
	// Parse command-line flags for service management
	configPath := flag.String("config", "config.toml", "Configuration file path")
	generateConfig := flag.Bool("generate-config", false, "Generate default config file and exit")
	serviceCmd := flag.String("service", "", "Service control: install, uninstall, start, stop, run")
	showVersion := flag.Bool("version", false, "Show version information and exit")
	quiet := flag.Bool("quiet", false, "Suppress informational output (errors/warnings still shown)")
	flag.BoolVar(quiet, "q", false, "Shorthand for --quiet")
	silent := flag.Bool("silent", false, "Suppress ALL output (complete silence)")
	flag.BoolVar(silent, "s", false, "Shorthand for --silent")
	flag.Parse()

	// Set quiet/silent mode globally for util functions
	if *silent {
		commonutil.SetSilentMode(true)
	} else {
		commonutil.SetQuietMode(*quiet)
	}

	// Show version if requested
	if *showVersion {
		fmt.Printf("PrintMaster Agent %s\n", Version)
		fmt.Printf("Build Time: %s\n", BuildTime)
		fmt.Printf("Git Commit: %s\n", GitCommit)
		fmt.Printf("Build Type: %s\n", BuildType)
		fmt.Printf("Go Version: %s\n", runtime.Version())
		fmt.Printf("OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		return
	}

	// Generate default config if requested
	if *generateConfig {
		if err := WriteDefaultAgentConfig(*configPath); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to generate config: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Generated default configuration at %s\n", *configPath)
		return
	}

	// Handle service commands
	if *serviceCmd != "" {
		handleServiceCommand(*serviceCmd)
		return
	}

	// Check if running as service and start appropriately
	if !service.Interactive() {
		// Running as service, use service wrapper
		runAsService()
		return
	}

	// Running interactively, start normally (no context means run forever)
	runInteractive(context.Background(), *configPath)
}

// handleServiceCommand processes service install/uninstall/start/stop commands
func handleServiceCommand(cmd string) {
	svcConfig := getServiceConfig()
	prg := &program{}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create service: %v\n", err)
		os.Exit(1)
	}

	switch cmd {
	case "install":
		// Show banner
		commonutil.ShowBanner(Version, GitCommit, BuildTime, "Fleet Management Agent")

		// Check if service already exists and handle gracefully
		status, _ := s.Status()
		if status != service.StatusUnknown {
			commonutil.ShowWarning("Service already exists, removing first...")

			// Stop if running
			if status == service.StatusRunning {
				commonutil.ShowInfo("Stopping existing service...")
				_ = s.Stop()
				time.Sleep(2 * time.Second)
				commonutil.ShowSuccess("Service stopped")
			}

			// Uninstall existing
			commonutil.ShowInfo("Removing existing service...")
			if err := s.Uninstall(); err != nil {
				// Ignore "marked for deletion" errors - we can still install over it
				if !strings.Contains(err.Error(), "marked for deletion") {
					commonutil.ShowError(fmt.Sprintf("Failed to remove existing service: %v", err))
					commonutil.ShowCompletionScreen(false, "Installation Failed")
					os.Exit(1)
				}
				commonutil.ShowWarning("Service marked for deletion, will install anyway")
			} else {
				commonutil.ShowSuccess("Existing service removed")
			}
			time.Sleep(500 * time.Millisecond)
		}

		// Create service directories first
		commonutil.ShowInfo("Setting up directories...")
		time.Sleep(300 * time.Millisecond)
		if err := setupServiceDirectories(); err != nil {
			commonutil.ShowError(fmt.Sprintf("Failed to setup service directories: %v", err))
			commonutil.ShowCompletionScreen(false, "Installation Failed")
			os.Exit(1)
		}
		commonutil.ShowSuccess("Directories ready")

		commonutil.ShowInfo("Installing service...")
		time.Sleep(500 * time.Millisecond)
		err = s.Install()
		if err != nil {
			// If service already exists, that's actually okay for install
			if strings.Contains(err.Error(), "already exists") {
				commonutil.ShowWarning("Service already exists (this is normal)")
			} else {
				commonutil.ShowError(fmt.Sprintf("Failed to install service: %v", err))
				commonutil.ShowCompletionScreen(false, "Installation Failed")
				os.Exit(1)
			}
		}
		commonutil.ShowSuccess("Service installed")

		commonutil.ShowCompletionScreen(true, "Service Installed!")
		fmt.Println()
		commonutil.ShowInfo("Use '--service start' to start the service")

	case "uninstall":
		err = s.Uninstall()
		if err != nil {
			commonutil.ShowError(fmt.Sprintf("Failed to uninstall service: %v", err))
			os.Exit(1)
		}
		commonutil.ShowInfo("PrintMaster Agent service uninstalled successfully")

	case "start":
		// Show banner
		commonutil.ShowBanner(Version, GitCommit, BuildTime, "Fleet Management Agent")

		commonutil.ShowInfo("Starting service...")
		err = s.Start()
		if err != nil {
			commonutil.ShowError(fmt.Sprintf("Failed to start service: %v", err))
			commonutil.ShowCompletionScreen(false, "Start Failed")
			os.Exit(1)
		}
		commonutil.ShowSuccess("Service started")

		commonutil.ShowCompletionScreen(true, "Service Started!")

	case "stop":
		// Show banner
		commonutil.ShowBanner(Version, GitCommit, BuildTime, "Fleet Management Agent")

		commonutil.ShowInfo("Stopping service...")
		done := make(chan bool)
		go commonutil.AnimateProgress(0, "Stopping service (may take up to 30 seconds)", done)
		err = s.Stop()
		done <- true

		if err != nil {
			commonutil.ShowError(fmt.Sprintf("Failed to stop service: %v", err))
			commonutil.ShowCompletionScreen(false, "Stop Failed")
			os.Exit(1)
		}
		commonutil.ShowSuccess("Service stopped")

		commonutil.ShowCompletionScreen(true, "Service Stopped!")

	case "status":
		// Show banner
		commonutil.ShowBanner(Version, GitCommit, BuildTime, "Fleet Management Agent")

		// Get service status
		status, statusErr := s.Status()

		fmt.Println()
		commonutil.ShowInfo("Service Status Information")
		fmt.Println()

		// Service state
		var statusText, statusColor string
		switch status { //nolint:exhaustive
		case service.StatusRunning:
			statusText = "RUNNING"
			statusColor = commonutil.ColorGreen
		case service.StatusStopped:
			statusText = "STOPPED"
			statusColor = commonutil.ColorYellow
		case service.StatusUnknown:
			statusText = "NOT INSTALLED"
			statusColor = commonutil.ColorRed
		default:
			statusText = "UNKNOWN"
			statusColor = commonutil.ColorDim
		}

		if statusErr != nil {
			fmt.Printf("  %sService State:%s %s%s%s (%v)\n",
				commonutil.ColorDim, commonutil.ColorReset,
				statusColor, statusText, commonutil.ColorReset,
				statusErr)
		} else {
			fmt.Printf("  %sService State:%s %s%s%s\n",
				commonutil.ColorDim, commonutil.ColorReset,
				statusColor, commonutil.ColorBold+statusText, commonutil.ColorReset)
		}

		// Service configuration
		cfg := getServiceConfig()
		fmt.Printf("  %sService Name:%s  %s\n", commonutil.ColorDim, commonutil.ColorReset, cfg.Name)
		fmt.Printf("  %sDisplay Name:%s  %s\n", commonutil.ColorDim, commonutil.ColorReset, cfg.DisplayName)
		fmt.Printf("  %sDescription:%s   %s\n", commonutil.ColorDim, commonutil.ColorReset, cfg.Description)
		fmt.Printf("  %sData Directory:%s %s\n", commonutil.ColorDim, commonutil.ColorReset, cfg.WorkingDirectory)

		// Try to get more details on Windows
		if runtime.GOOS == "windows" && status == service.StatusRunning {
			fmt.Println()
			commonutil.ShowInfo("Checking service details...")

			// Use sc.exe to query service for more info
			cmd := exec.Command("sc", "query", cfg.Name)
			output, err := cmd.Output()
			if err == nil {
				lines := strings.Split(string(output), "\n")
				for _, line := range lines {
					line = strings.TrimSpace(line)
					if strings.Contains(line, "PID") {
						fmt.Printf("  %s%s%s\n", commonutil.ColorDim, line, commonutil.ColorReset)
					}
				}
			}

			// Try to get uptime via wmic
			cmd = exec.Command("wmic", "service", "where", fmt.Sprintf("name='%s'", cfg.Name), "get", "ProcessId,Started", "/value")
			output, err = cmd.Output()
			if err == nil {
				fmt.Printf("  %s%s%s\n", commonutil.ColorDim, strings.TrimSpace(string(output)), commonutil.ColorReset)
			}
		}

		fmt.Println()

		// Show helpful next steps based on status
		switch status {
		case service.StatusRunning:
			commonutil.ShowInfo("Service is running normally")
			fmt.Println()
			fmt.Printf("  %sWeb UI:%s http://localhost:8080 or https://localhost:8443\n", commonutil.ColorDim, commonutil.ColorReset)
		case service.StatusStopped:
			commonutil.ShowWarning("Service is installed but not running - Use '--service start' to start the service")
		default:
			commonutil.ShowWarning("Service is not installed - Use '--service install' to install the service")
		}

		fmt.Println()
		commonutil.PromptToContinue()

	case "restart":
		// Show banner
		commonutil.ShowBanner(Version, GitCommit, BuildTime, "Fleet Management Agent")

		commonutil.ShowInfo("Stopping service...")
		if err := s.Stop(); err != nil {
			commonutil.ShowError(fmt.Sprintf("Failed to stop service: %v", err))
			commonutil.ShowCompletionScreen(false, "Restart Failed")
			os.Exit(1)
		}
		commonutil.ShowSuccess("Service stopped")

		time.Sleep(1 * time.Second)

		commonutil.ShowInfo("Starting service...")
		if err := s.Start(); err != nil {
			commonutil.ShowError(fmt.Sprintf("Failed to start service: %v", err))
			commonutil.ShowCompletionScreen(false, "Restart Failed")
			os.Exit(1)
		}
		commonutil.ShowSuccess("Service started")

		commonutil.ShowCompletionScreen(true, "Service Restarted!")

	case "update":
		// Show banner
		commonutil.ShowBanner(Version, GitCommit, BuildTime, "Fleet Management Agent")

		// Stop service if running
		commonutil.ShowInfo("Stopping service...")
		done := make(chan bool)
		go commonutil.AnimateProgress(0, "Stopping service (may take up to 30 seconds)", done)

		stopErr := s.Stop()
		if stopErr != nil {
			done <- true
			commonutil.ShowWarning("Service not running or already stopped")
		} else {
			// Wait for service to fully stop (max 30 seconds)
			for i := 0; i < 30; i++ {
				time.Sleep(1 * time.Second)

				// Check service status (Windows-specific check)
				if runtime.GOOS == "windows" {
					status, _ := s.Status()
					if status == service.StatusStopped {
						break
					}
				}
			}
			done <- true
			commonutil.ShowSuccess("Service stopped")
		}

		// Uninstall existing service
		commonutil.ShowInfo("Uninstalling old service...")
		time.Sleep(500 * time.Millisecond)
		if err := s.Uninstall(); err != nil {
			commonutil.ShowWarning("Service not installed or already removed")
		} else {
			commonutil.ShowSuccess("Service uninstalled")
		}

		// Setup directories
		commonutil.ShowInfo("Setting up directories...")
		time.Sleep(300 * time.Millisecond)
		if err := setupServiceDirectories(); err != nil {
			commonutil.ShowError(fmt.Sprintf("Failed to setup service directories: %v", err))
			commonutil.ShowCompletionScreen(false, "Update Failed")
			os.Exit(1)
		}
		commonutil.ShowSuccess("Directories ready")

		// Reinstall service
		commonutil.ShowInfo("Installing updated service...")
		time.Sleep(500 * time.Millisecond)
		err = s.Install()
		if err != nil {
			commonutil.ShowError(fmt.Sprintf("Failed to install service: %v", err))
			commonutil.ShowCompletionScreen(false, "Update Failed")
			os.Exit(1)
		}
		commonutil.ShowSuccess("Service installed")

		// Start service
		commonutil.ShowInfo("Starting service...")
		time.Sleep(500 * time.Millisecond)
		err = s.Start()
		if err != nil {
			commonutil.ShowError(fmt.Sprintf("Failed to start service: %v", err))
			commonutil.ShowCompletionScreen(false, "Update Failed")
			os.Exit(1)
		}
		commonutil.ShowSuccess("Service started")

		// Show completion screen
		commonutil.ShowCompletionScreen(true, "Service Updated Successfully!")

	case "run":
		// Run as service (called by service manager)
		err = s.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Service run failed: %v\n", err)
			os.Exit(1)
		}

	case "help", "":
		// Show help for service commands
		fmt.Println("PrintMaster Agent - Service Management")
		fmt.Println()
		fmt.Println("Usage:")
		fmt.Println("  printmaster-agent --service <command>")
		fmt.Println()
		fmt.Println("Commands:")
		fmt.Println("  install    Install PrintMaster Agent as a system service")
		fmt.Println("  uninstall  Remove the PrintMaster Agent service")
		fmt.Println("  start      Start the PrintMaster Agent service")
		fmt.Println("  stop       Stop the PrintMaster Agent service")
		fmt.Println("  restart    Restart the PrintMaster Agent service")
		fmt.Println("  status     Show service status and information")
		fmt.Println("  update     Full reinstall cycle (stop, remove, install, start)")
		fmt.Println("  run        Run as service (used by service manager)")
		fmt.Println("  help       Show this help message")
		fmt.Println()
		fmt.Println("Service Details:")
		fmt.Println("  Name:         PrintMasterAgent")
		fmt.Println("  Display Name: PrintMaster Agent")
		fmt.Println("  Description:  Printer and copier fleet management agent")
		fmt.Println()
		fmt.Println("Platform-Specific Paths:")
		switch runtime.GOOS {
		case "windows":
			fmt.Println("  Data Directory: C:\\ProgramData\\PrintMaster\\")
			fmt.Println("  Log Directory:  C:\\ProgramData\\PrintMaster\\logs\\")
		case "darwin":
			fmt.Println("  Data Directory: /Library/Application Support/PrintMaster/")
			fmt.Println("  Log Directory:  /var/log/printmaster/")
		default: // Linux
			fmt.Println("  Data Directory: /var/lib/printmaster/")
			fmt.Println("  Log Directory:  /var/log/printmaster/")
			fmt.Println("  Config:         /etc/printmaster/")
		}
		fmt.Println()
		fmt.Println("Examples:")
		if runtime.GOOS == "windows" {
			fmt.Println("  # Install and start (requires Administrator)")
			fmt.Println("  .\\printmaster-agent.exe --service install")
			fmt.Println("  .\\printmaster-agent.exe --service start")
			fmt.Println()
			fmt.Println("  # Update running service")
			fmt.Println("  .\\printmaster-agent.exe --service update")
			fmt.Println()
			fmt.Println("  # Check service status")
			fmt.Println("  Get-Service PrintMasterAgent")
		} else {
			fmt.Println("  # Install and start (requires root)")
			fmt.Println("  sudo ./printmaster-agent --service install")
			fmt.Println("  sudo systemctl start PrintMasterAgent")
		}
		fmt.Println()

	default:
		fmt.Fprintf(os.Stderr, "Unknown service command: %s\n", cmd)
		fmt.Println()
		fmt.Println("Valid commands: install, uninstall, start, stop, restart, status, update, run, help")
		fmt.Println("Run 'printmaster-agent --service help' for more information")
		os.Exit(1)
	}
}

// runAsService starts the agent under service manager control
func runAsService() {
	svcConfig := getServiceConfig()
	prg := &program{}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		os.Exit(1)
	}

	err = s.Run()
	if err != nil {
		os.Exit(1)
	}
}

// runInteractive starts the agent in foreground mode (normal operation)
func runInteractive(ctx context.Context, configFlag string) {
	// Initialize SSE hub for real-time UI updates
	sseHub = NewSSEHub()

	// Initialize structured logger (DEBUG level for proxy diagnostics, 1000 entries in buffer)
	// Determine log directory based on whether we're running as a service
	var logDir string
	if !service.Interactive() {
		// Running as service - use platform-specific system directory
		logPath := getServiceLogPath()
		logDir = filepath.Dir(logPath)
	} else {
		logDir = "logs"
	}

	if err := os.MkdirAll(logDir, 0755); err == nil {
		appLogger = logger.New(logger.DEBUG, logDir, 1000)
		appLogger.SetRotationPolicy(logger.RotationPolicy{
			Enabled:    true,
			MaxSizeMB:  10,
			MaxAgeDays: 7,
			MaxFiles:   5,
		})
		// Set up SSE broadcasting for log entries
		appLogger.SetOnLogCallback(func(entry logger.LogEntry) {
			if sseHub != nil {
				// Broadcast log entry via SSE
				sseHub.Broadcast(SSEEvent{
					Type: "log_entry",
					Data: map[string]interface{}{
						"timestamp": entry.Timestamp.Format(time.RFC3339),
						"level":     logger.LevelToString(entry.Level),
						"message":   entry.Message,
						"context":   entry.Context,
					},
				})
			}
		})
		defer appLogger.Close()
	}

	appLogger.Info("Printer Fleet Agent starting",
		"startup_scan", "disabled",
		"version", Version,
		"build_time", BuildTime,
		"git_commit", GitCommit,
		"build_type", BuildType)

	// Provide the app logger to the agent package so internal logs are structured
	agent.SetLogger(appLogger)

	// Load TOML configuration
	// Try to find config.toml in multiple locations
	// Service mode: ProgramData/agent > ProgramData (legacy)
	// Interactive mode: executable dir > current dir
	var agentConfig *AgentConfig

	isService := !service.Interactive()
	var configPaths []string

	if isService {
		// Running as service - check ProgramData locations only
		programData := os.Getenv("ProgramData")
		if programData == "" {
			programData = "C:\\ProgramData"
		}
		configPaths = []string{
			filepath.Join(programData, "PrintMaster", "agent", "config.toml"),
			filepath.Join(programData, "PrintMaster", "config.toml"), // Legacy location
		}
	} else {
		// Running interactively - check local locations only
		configPaths = []string{
			filepath.Join(filepath.Dir(os.Args[0]), "config.toml"),
			"config.toml",
		}
	}

	// Resolve config path using shared helper which checks AGENT_CONFIG/AGENT_CONFIG_PATH,
	// generic CONFIG/CONFIG_PATH, then the provided flag value.
	configLoaded := false
	resolved := config.ResolveConfigPath("AGENT", configFlag)
	if resolved != "" {
		if _, statErr := os.Stat(resolved); statErr == nil {
			if cfg, err := LoadAgentConfig(resolved); err == nil {
				agentConfig = cfg
				appLogger.Info("Loaded configuration", "path", resolved)
				configLoaded = true
			} else {
				appLogger.Warn("Config path set but failed to parse", "path", resolved, "error", err)
			}
		} else {
			appLogger.Warn("Config path set but file not found", "path", resolved)
		}
	}

	// If not loaded via env/flag, fall back to default search paths
	for _, cfgPath := range configPaths {
		if configLoaded {
			break
		}
		if cfg, err := LoadAgentConfig(cfgPath); err == nil {
			agentConfig = cfg
			appLogger.Info("Loaded configuration", "path", cfgPath)
			configLoaded = true
			break
		}
	}

	if !configLoaded {
		appLogger.Warn("No config.toml found, using defaults")
		agentConfig = DefaultAgentConfig()
	}

	// Always apply environment overrides for database path (supports AGENT_DB_PATH and DB_PATH)
	// even when using default configuration (no config file present).
	config.ApplyDatabaseEnvOverrides(&agentConfig.Database, "AGENT")
	if agentConfig.Database.Path != "" {
		// If env var points to a directory, append default filename (devices.db)
		dbPath := agentConfig.Database.Path
		if strings.HasSuffix(dbPath, string(os.PathSeparator)) || strings.HasSuffix(dbPath, "/") {
			dbPath = filepath.Join(dbPath, "devices.db")
		} else {
			if fi, err := os.Stat(dbPath); err == nil && fi.IsDir() {
				dbPath = filepath.Join(dbPath, "devices.db")
			}
		}

		parent := filepath.Dir(dbPath)
		if err := os.MkdirAll(parent, 0755); err != nil {
			appLogger.Warn("Could not create DB parent directory, falling back", "parent", parent, "error", err)
			agentConfig.Database.Path = ""
		} else {
			// Probe write access
			f, err := os.OpenFile(dbPath, os.O_RDWR|os.O_CREATE, 0644)
			if err != nil {
				appLogger.Warn("Cannot write to DB path, falling back", "path", dbPath, "error", err)
				agentConfig.Database.Path = ""
			} else {
				f.Close()
				agentConfig.Database.Path = dbPath
				appLogger.Info("Database path overridden by environment", "path", agentConfig.Database.Path)
			}
		}
	}

	// Apply logging level from config
	if level := logger.LevelFromString(agentConfig.Logging.Level); level >= 0 {
		appLogger.SetLevel(level)
		appLogger.Info("Log level set from config", "level", agentConfig.Logging.Level)
	}

	// Initialize device storage
	// Use config-specified path or detect proper data directory for service
	var dbPath string
	var err error
	if agentConfig != nil && agentConfig.Database.Path != "" {
		dbPath = agentConfig.Database.Path
		appLogger.Info("Using configured database path", "path", dbPath)
	} else {
		// Detect if running as service and use appropriate directory
		dataDir, dirErr := config.GetDataDirectory("agent", isService)
		if dirErr != nil {
			appLogger.Warn("Could not get data directory, using in-memory storage", "error", dirErr)
			dbPath = ":memory:"
		} else {
			dbPath = filepath.Join(dataDir, "devices.db")
			appLogger.Info("Using device database", "path", dbPath)
		}
	}

	// Set logger for storage package
	storage.SetLogger(appLogger)

	// Initialize agent config storage first (needed for rotation tracking)
	agentDBPath := filepath.Join(filepath.Dir(dbPath), "agent.db")
	if dbPath == ":memory:" {
		agentDBPath = ":memory:"
	}
	agentConfigStore, err = storage.NewAgentConfigStore(agentDBPath)
	if err != nil {
		appLogger.Error("Failed to initialize agent config storage", "error", err, "path", agentDBPath)
		os.Exit(1)
	}
	defer agentConfigStore.Close()
	appLogger.Info("Agent config database initialized", "path", agentDBPath)

	// Clean up old database backups (keep 10 most recent)
	if err := storage.CleanupOldBackups(dbPath, 10); err != nil {
		appLogger.Warn("Failed to cleanup old database backups", "error", err)
	}

	// Initialize device storage with config store for rotation tracking
	deviceStore, err = storage.NewSQLiteStoreWithConfig(dbPath, agentConfigStore)
	if err != nil {
		appLogger.Error("Failed to initialize device storage", "error", err, "path", dbPath)
		os.Exit(1)
	}
	defer deviceStore.Close()

	// Load and restore trace tags from config
	var savedTraceTags map[string]bool
	if err := agentConfigStore.GetConfigValue("trace_tags", &savedTraceTags); err == nil && len(savedTraceTags) > 0 {
		appLogger.SetTraceTags(savedTraceTags)
		appLogger.Info("Restored trace tags from config", "count", len(savedTraceTags))
	}

	// Secret key for encrypting local credentials
	dataDir := filepath.Dir(dbPath)
	secretPath := filepath.Join(dataDir, "agent_secret.key")
	secretKey, skErr := commonutil.LoadOrCreateKey(secretPath)
	if skErr != nil {
		appLogger.Warn("Could not prepare local secret key", "error", skErr, "path", secretPath)
	} else {
		appLogger.Debug("Secret key loaded", "path", secretPath)
	}

	// Helpers for WebUI credential storage
	type credRecord struct {
		Username  string `json:"username"`
		Password  string `json:"password_enc"` // encrypted base64
		AuthType  string `json:"auth_type"`    // "basic" | "form"
		AutoLogin bool   `json:"auto_login"`
	}

	getCreds := func(serial string) (*credRecord, error) {
		if agentConfigStore == nil {
			return nil, fmt.Errorf("no config store")
		}
		var all map[string]credRecord
		if err := agentConfigStore.GetConfigValue("webui_credentials", &all); err != nil {
			all = map[string]credRecord{}
		}
		c, ok := all[serial]
		if !ok {
			return nil, fmt.Errorf("not found")
		}
		return &c, nil
	}

	saveCreds := func(serial string, c credRecord) error {
		if agentConfigStore == nil {
			return fmt.Errorf("no config store")
		}
		var all map[string]credRecord
		if err := agentConfigStore.GetConfigValue("webui_credentials", &all); err != nil || all == nil {
			all = map[string]credRecord{}
		}
		all[serial] = c
		return agentConfigStore.SetConfigValue("webui_credentials", all)
	}

	// Create storage adapter that implements agent.DeviceStorage interface
	storageAdapter := &deviceStorageAdapter{store: deviceStore}
	agent.SetDeviceStorage(storageAdapter)
	appLogger.Info("Device storage connected", "mode", "auto_persist")

	// Start garbage collection goroutine
	retentionConfig := agent.GetRetentionConfig()
	go runGarbageCollection(ctx, deviceStore, retentionConfig)

	// Start metrics downsampler goroutine (runs every 6 hours)
	go runMetricsDownsampler(ctx, deviceStore)

	// Auto-discovery management (periodic scanning + optional live discovery methods)
	// Controlled by discovery setting: auto_discover_enabled (bool) - master switch
	// Individual live discovery methods can be enabled/disabled independently
	var (
		autoDiscoverMu       sync.Mutex
		autoDiscoverCancel   context.CancelFunc
		autoDiscoverRunning  bool
		autoDiscoverInterval = 15 * time.Minute // Configurable via settings

		liveMDNSMu      sync.Mutex
		liveMDNSCancel  context.CancelFunc
		liveMDNSRunning bool
		liveMDNSSeen    = map[string]time.Time{}

		liveWSDiscoveryMu      sync.Mutex
		liveWSDiscoveryCancel  context.CancelFunc
		liveWSDiscoveryRunning bool
		liveWSDiscoverySeen    = map[string]time.Time{}

		liveSSDPMu      sync.Mutex
		liveSSDPCancel  context.CancelFunc
		liveSSDPRunning bool
		liveSSDPSeen    = map[string]time.Time{}

		metricsRescanMu       sync.Mutex
		metricsRescanCancel   context.CancelFunc
		metricsRescanRunning  bool
		metricsRescanInterval = 60 * time.Minute // Configurable via settings

		snmpTrapMu      sync.Mutex
		snmpTrapCancel  context.CancelFunc
		snmpTrapRunning bool
		snmpTrapSeen    = map[string]time.Time{}

		llmnrMu      sync.Mutex
		llmnrCancel  context.CancelFunc
		llmnrRunning bool
		llmnrSeen    = map[string]time.Time{}
	)

	// Periodic discovery worker
	startAutoDiscover := func() {
		autoDiscoverMu.Lock()
		defer autoDiscoverMu.Unlock()
		if autoDiscoverRunning {
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		autoDiscoverCancel = cancel
		autoDiscoverRunning = true
		appLogger.Info("Auto Discover: starting periodic scanner", "interval", autoDiscoverInterval.String())

		go func() {
			ticker := time.NewTicker(autoDiscoverInterval)
			defer ticker.Stop()

			// Run immediately on start
			runPeriodicScan := func() {
				appLogger.Debug("Auto Discover: running periodic scan")

				// Load discovery settings
				var discoverySettings = map[string]interface{}{
					"subnet_scan":   true,
					"manual_ranges": true,
					"arp_enabled":   true,
					"icmp_enabled":  true,
					"tcp_enabled":   true,
					"snmp_enabled":  true,
					"mdns_enabled":  false,
				}
				if agentConfigStore != nil {
					var stored map[string]interface{}
					if err := agentConfigStore.GetConfigValue("discovery_settings", &stored); err == nil && stored != nil {
						for k, v := range stored {
							discoverySettings[k] = v
						}
					}
				}

				// Get saved ranges
				var ranges []string
				if agentConfigStore != nil {
					savedRanges, _ := agentConfigStore.GetRangesList()
					ranges = savedRanges
				}

				// Build DiscoveryConfig from settings
				discoveryCfg := &agent.DiscoveryConfig{
					ARPEnabled:  discoverySettings["arp_enabled"] == true,
					ICMPEnabled: discoverySettings["icmp_enabled"] == true,
					TCPEnabled:  discoverySettings["tcp_enabled"] == true,
					SNMPEnabled: discoverySettings["snmp_enabled"] == true,
					MDNSEnabled: discoverySettings["mdns_enabled"] == true,
				}

				// Use new scanner for periodic discovery (full mode)
				_, err := Discover(ctx, ranges, "full", discoveryCfg, deviceStore, 50, 10)
				if err != nil && ctx.Err() == nil {
					appLogger.Error("Auto Discover scan error", "error", err, "ranges", len(ranges))
				}
			}
			runPeriodicScan()

			for {
				select {
				case <-ctx.Done():
					autoDiscoverMu.Lock()
					autoDiscoverRunning = false
					autoDiscoverCancel = nil
					autoDiscoverMu.Unlock()
					appLogger.Info("Auto Discover: stopped")
					return
				case <-ticker.C:
					runPeriodicScan()
				}
			}
		}()
	}

	stopAutoDiscover := func() {
		autoDiscoverMu.Lock()
		defer autoDiscoverMu.Unlock()
		if autoDiscoverCancel != nil {
			appLogger.Info("Auto Discover: stopping periodic scanner")
			autoDiscoverCancel()
			autoDiscoverCancel = nil
		}
	}

	// getSNMPTimeoutSeconds returns the configured SNMP timeout in seconds
	getSNMPTimeoutSeconds := func() int {
		scannerConfig.RLock()
		defer scannerConfig.RUnlock()
		timeoutSec := scannerConfig.SNMPTimeoutMs / 1000
		if timeoutSec < 1 {
			timeoutSec = 2 // Minimum 2 seconds
		}
		return timeoutSec
	}

	// handleLiveDiscovery processes a single IP from live discovery (mDNS, SSDP, WS-Discovery)
	// Uses the new scanner to detect and store the device
	handleLiveDiscovery := func(ip string, discoveryMethod string) {
		ctx := context.Background()

		// Check if we already know this IP from a saved device
		// If so, do a quick refresh instead of full detection
		if deviceStore != nil {
			visibleTrue := true
			devices, err := deviceStore.List(ctx, storage.DeviceFilter{
				Visible: &visibleTrue,
			})
			if err == nil {
				for _, device := range devices {
					if device.IP == ip {
						// Known device - liveness confirmed, do quick refresh
						appLogger.Debug(discoveryMethod+": known device liveness confirmed, refreshing",
							"ip", ip, "serial", device.Serial)

						// Perform quick SNMP query to get updated metrics
						pi, err := LiveDiscoveryDetect(ctx, ip, getSNMPTimeoutSeconds())
						if err != nil {
							appLogger.Debug(discoveryMethod+": refresh failed, updating last_seen only",
								"ip", ip, "serial", device.Serial, "error", err)
							// Just update last seen time even if SNMP fails
							device.LastSeen = time.Now()
							deviceStore.Update(ctx, device)
							return
						}

						// Update device with fresh data
						device.LastSeen = time.Now()
						if pi.Serial != "" && pi.Serial == device.Serial {
							// Serials match, update other fields if not locked
							if device.LockedFields == nil {
								device.LockedFields = []storage.FieldLock{}
							}
							isLocked := func(field string) bool {
								for _, lf := range device.LockedFields {
									if strings.EqualFold(lf.Field, field) {
										return true
									}
								}
								return false
							}

							if !isLocked("manufacturer") && pi.Manufacturer != "" {
								device.Manufacturer = pi.Manufacturer
							}
							if !isLocked("model") && pi.Model != "" {
								device.Model = pi.Model
							}
							if !isLocked("hostname") && pi.Hostname != "" {
								device.Hostname = pi.Hostname
							}

							deviceStore.Update(ctx, device)

							// Broadcast SSE update
							sseHub.Broadcast(SSEEvent{
								Type: "device_updated",
								Data: map[string]interface{}{
									"serial":       device.Serial,
									"ip":           ip,
									"manufacturer": device.Manufacturer,
									"model":        device.Model,
									"last_seen":    device.LastSeen.Format(time.RFC3339),
									"method":       discoveryMethod,
								},
							})
						}
						return
					}
				}
			}
		}

		// Not a known device - do full detection
		// Use new scanner for live discovery detection
		pi, err := LiveDiscoveryDetect(ctx, ip, getSNMPTimeoutSeconds())
		if err != nil {
			appLogger.WarnRateLimited(discoveryMethod+"_detect_"+ip, 5*time.Minute,
				discoveryMethod+" detection failed", "ip", ip, "error", err)
			// Don't store device without serial - it will just create errors
			return
		}

		// If lightweight query didn't get a serial, try a full deep scan
		// We already have proof of life from live discovery, so it's worth the extra query
		if pi.Serial == "" {
			appLogger.Debug(discoveryMethod+": no serial from quick scan, trying deep scan",
				"ip", ip, "manufacturer", pi.Manufacturer, "model", pi.Model)

			deepPi, deepErr := LiveDiscoveryDeepScan(ctx, ip, 30)
			if deepErr != nil {
				appLogger.Debug(discoveryMethod+": deep scan failed",
					"ip", ip, "error", deepErr)
				return
			}

			// Use deep scan result if it has a serial
			if deepPi != nil && deepPi.Serial != "" {
				pi = deepPi
				appLogger.Info(discoveryMethod+": deep scan found device",
					"ip", ip, "serial", pi.Serial, "manufacturer", pi.Manufacturer, "model", pi.Model)
			} else {
				appLogger.Debug(discoveryMethod+": deep scan completed but no serial found",
					"ip", ip)
				return
			}
		}

		// Add discovery method
		pi.DiscoveryMethods = append(pi.DiscoveryMethods, discoveryMethod)

		// Check if this is a known device
		if pi.Serial != "" {
			existing, err := deviceStore.Get(ctx, pi.Serial)
			if err == nil && existing != nil {
				// Known device - broadcast SSE update immediately
				existing.LastSeen = time.Now()
				existing.IP = ip
				if updateErr := deviceStore.Update(ctx, existing); updateErr == nil {
					sseHub.Broadcast(SSEEvent{
						Type: "device_updated",
						Data: map[string]interface{}{
							"serial":       pi.Serial,
							"ip":           ip,
							"manufacturer": pi.Manufacturer,
							"model":        pi.Model,
							"last_seen":    existing.LastSeen.Format(time.RFC3339),
							"method":       discoveryMethod,
						},
					})
					appLogger.Debug(discoveryMethod+": known device updated",
						"ip", ip, "serial", pi.Serial)
				}
			} else {
				// New device - broadcast discovery event
				sseHub.Broadcast(SSEEvent{
					Type: "device_discovered",
					Data: map[string]interface{}{
						"ip":           ip,
						"serial":       pi.Serial,
						"manufacturer": pi.Manufacturer,
						"model":        pi.Model,
						"method":       discoveryMethod,
					},
				})
				appLogger.Debug(discoveryMethod+": new device discovered",
					"ip", ip, "serial", pi.Serial)
			}
		}

		// Store/update the device
		agent.UpsertDiscoveredPrinter(*pi)
	}

	// Live mDNS discovery worker (only works when auto discover is enabled)
	startLiveMDNS := func() {
		liveMDNSMu.Lock()
		defer liveMDNSMu.Unlock()
		if liveMDNSRunning {
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		liveMDNSCancel = cancel
		liveMDNSRunning = true
		appLogger.Info("Live mDNS discovery: starting background browser")

		go func() {
			h := func(ip string) bool {
				ip = strings.TrimSpace(ip)
				if ip == "" {
					return false
				}
				liveMDNSMu.Lock()
				last, ok := liveMDNSSeen[ip]
				if ok && time.Since(last) < 10*time.Minute {
					liveMDNSMu.Unlock()
					return false
				}
				liveMDNSSeen[ip] = time.Now()
				liveMDNSMu.Unlock()
				agent.AppendScanEvent("LIVE MDNS: discovered " + ip)

				// Call LiveDiscoveryDetect directly
				go handleLiveDiscovery(ip, "mdns")
				return true
			}
			agent.StartMDNSBrowser(ctx, h)
			liveMDNSMu.Lock()
			liveMDNSRunning = false
			liveMDNSCancel = nil
			liveMDNSMu.Unlock()
			appLogger.Info("Live mDNS discovery: stopped")
		}()
	}

	stopLiveMDNS := func() {
		liveMDNSMu.Lock()
		defer liveMDNSMu.Unlock()
		if liveMDNSCancel != nil {
			appLogger.Info("Live mDNS discovery: stopping background browser")
			liveMDNSCancel()
			liveMDNSCancel = nil
		}
	}

	// Live WS-Discovery worker (Windows network printer discovery)
	startLiveWSDiscovery := func() {
		liveWSDiscoveryMu.Lock()
		defer liveWSDiscoveryMu.Unlock()
		if liveWSDiscoveryRunning {
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		liveWSDiscoveryCancel = cancel
		liveWSDiscoveryRunning = true
		appLogger.Info("Live WS-Discovery: starting background listener")

		go func() {
			h := func(ip string) bool {
				ip = strings.TrimSpace(ip)
				if ip == "" {
					return false
				}
				liveWSDiscoveryMu.Lock()
				last, ok := liveWSDiscoverySeen[ip]
				if ok && time.Since(last) < 10*time.Minute {
					liveWSDiscoveryMu.Unlock()
					return false
				}
				liveWSDiscoverySeen[ip] = time.Now()
				liveWSDiscoveryMu.Unlock()
				agent.AppendScanEvent("LIVE WS-DISCOVERY: discovered " + ip)

				// Call LiveDiscoveryDetect directly
				go handleLiveDiscovery(ip, "wsdiscovery")
				return true
			}
			agent.StartWSDiscoveryBrowser(ctx, h)
			liveWSDiscoveryMu.Lock()
			liveWSDiscoveryRunning = false
			liveWSDiscoveryCancel = nil
			liveWSDiscoveryMu.Unlock()
			appLogger.Info("Live WS-Discovery: stopped")
		}()
	}

	stopLiveWSDiscovery := func() {
		liveWSDiscoveryMu.Lock()
		defer liveWSDiscoveryMu.Unlock()
		if liveWSDiscoveryCancel != nil {
			appLogger.Info("Live WS-Discovery: stopping background listener")
			liveWSDiscoveryCancel()
			liveWSDiscoveryCancel = nil
		}
	}

	// Live SSDP/UPnP discovery worker
	startLiveSSDP := func() {
		liveSSDPMu.Lock()
		defer liveSSDPMu.Unlock()
		if liveSSDPRunning {
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		liveSSDPCancel = cancel
		liveSSDPRunning = true
		appLogger.Info("Live SSDP: starting background listener")

		go func() {
			h := func(ip string) bool {
				ip = strings.TrimSpace(ip)
				if ip == "" {
					return false
				}
				liveSSDPMu.Lock()
				last, ok := liveSSDPSeen[ip]
				if ok && time.Since(last) < 10*time.Minute {
					liveSSDPMu.Unlock()
					return false
				}
				liveSSDPSeen[ip] = time.Now()
				liveSSDPMu.Unlock()
				agent.AppendScanEvent("LIVE SSDP: discovered " + ip)

				// Call LiveDiscoveryDetect directly
				go handleLiveDiscovery(ip, "ssdp")
				return true
			}
			agent.StartSSDPBrowser(ctx, h)
			liveSSDPMu.Lock()
			liveSSDPRunning = false
			liveSSDPCancel = nil
			liveSSDPMu.Unlock()
			appLogger.Info("Live SSDP: stopped")
		}()
	}

	stopLiveSSDP := func() {
		liveSSDPMu.Lock()
		defer liveSSDPMu.Unlock()
		if liveSSDPCancel != nil {
			appLogger.Info("Live SSDP: stopping background listener")
			liveSSDPCancel()
			liveSSDPCancel = nil
		}
	}

	// SNMP Trap Listener: Event-driven discovery via trap notifications
	startSNMPTrap := func() {
		snmpTrapMu.Lock()
		defer snmpTrapMu.Unlock()

		if snmpTrapRunning {
			appLogger.Debug("SNMP Trap listener already running")
			return
		}

		ctx, cancel := context.WithCancel(context.Background())
		snmpTrapCancel = cancel
		snmpTrapRunning = true

		appLogger.Info("SNMP Trap: starting listener", "port", 162, "requires_admin", true)

		go func() {
			h := func(ip string) bool {
				// Async SNMP enrichment + metrics collection
				go func(ip string) {
					// Use new scanner for trap handling
					ctx := context.Background()
					pi, err := LiveDiscoveryDetect(ctx, ip, getSNMPTimeoutSeconds())
					if err != nil {
						appLogger.WarnRateLimited("trap_enrich_"+ip, 5*time.Minute, "SNMP Trap: enrichment failed", "ip", ip, "error", err)
						return
					}

					serial := pi.Serial
					if serial == "" {
						appLogger.Debug("SNMP Trap: no serial found for device", "ip", ip)
						return
					}

					// Check if device exists in DB
					existing, err := deviceStore.Get(ctx, serial)
					if err == nil && existing != nil {
						// Known device - update LastSeen
						existing.LastSeen = time.Now()
						existing.IP = ip
						if updateErr := deviceStore.Update(ctx, existing); updateErr == nil {
							appLogger.Debug("SNMP Trap: known device updated", "ip", ip, "serial", serial)
						}
					} else {
						// New device
						appLogger.Debug("SNMP Trap: new device discovered", "ip", ip, "serial", serial)
					}

					// Store/update the device
					agent.UpsertDiscoveredPrinter(*pi)
					appLogger.Info("SNMP Trap: discovered device", "ip", ip, "serial", serial)

					// If metrics monitoring is enabled and device is saved, collect metrics immediately
					metricsRescanMu.Lock()
					metricsEnabled := metricsRescanRunning
					metricsRescanMu.Unlock()

					if metricsEnabled && deviceStore != nil && serial != "" {
						// Check if device is saved
						ctx := context.Background()
						device, err := deviceStore.Get(ctx, serial)
						if err == nil && device != nil && device.IsSaved {
							// Extract learned OIDs from device for efficient metrics collection
							pi := storage.DeviceToPrinterInfo(device)
							learnedOIDs := &pi.LearnedOIDs

							// Collect metrics for this device using learned OIDs if available
							agentSnapshot, err := CollectMetricsWithOIDs(ctx, ip, serial, device.Manufacturer, 10, learnedOIDs)
							if err != nil {
								appLogger.WarnRateLimited("trap_metrics_"+serial, 5*time.Minute, "SNMP Trap: metrics collection failed", "serial", serial, "error", err)
							} else {
								// Convert to storage format
								storageSnapshot := &storage.MetricsSnapshot{}
								storageSnapshot.Serial = agentSnapshot.Serial
								storageSnapshot.Timestamp = time.Now()
								storageSnapshot.PageCount = agentSnapshot.PageCount
								storageSnapshot.ColorPages = agentSnapshot.ColorPages
								storageSnapshot.MonoPages = agentSnapshot.MonoPages
								storageSnapshot.ScanCount = agentSnapshot.ScanCount
								storageSnapshot.TonerLevels = agentSnapshot.TonerLevels
								storageSnapshot.FaxPages = agentSnapshot.FaxPages
								storageSnapshot.CopyPages = agentSnapshot.CopyPages
								storageSnapshot.OtherPages = agentSnapshot.OtherPages
								storageSnapshot.CopyMonoPages = agentSnapshot.CopyMonoPages
								storageSnapshot.CopyFlatbedScans = agentSnapshot.CopyFlatbedScans
								storageSnapshot.CopyADFScans = agentSnapshot.CopyADFScans
								storageSnapshot.FaxFlatbedScans = agentSnapshot.FaxFlatbedScans
								storageSnapshot.FaxADFScans = agentSnapshot.FaxADFScans
								storageSnapshot.ScanToHostFlatbed = agentSnapshot.ScanToHostFlatbed
								storageSnapshot.ScanToHostADF = agentSnapshot.ScanToHostADF
								storageSnapshot.DuplexSheets = agentSnapshot.DuplexSheets
								storageSnapshot.JamEvents = agentSnapshot.JamEvents
								storageSnapshot.ScannerJamEvents = agentSnapshot.ScannerJamEvents

								// Save to database (error already logged in storage layer)
								if err := deviceStore.SaveMetricsSnapshot(ctx, storageSnapshot); err == nil {
									appLogger.Debug("SNMP Trap: collected metrics", "serial", serial)
								}
							}
						}
					}
				}(ip)
				return true
			} // Call browser with 10-minute throttle window
			agent.StartSNMPTrapBrowser(ctx, h, snmpTrapSeen, 10*time.Minute)

			snmpTrapMu.Lock()
			snmpTrapRunning = false
			snmpTrapCancel = nil
			snmpTrapMu.Unlock()
			appLogger.Info("SNMP Trap: stopped")
		}()
	}

	stopSNMPTrap := func() {
		snmpTrapMu.Lock()
		defer snmpTrapMu.Unlock()
		if snmpTrapCancel != nil {
			appLogger.Info("SNMP Trap: stopping listener")
			snmpTrapCancel()
			snmpTrapCancel = nil
		}
	}

	// LLMNR: Windows hostname resolution for printer discovery
	startLLMNR := func() {
		llmnrMu.Lock()
		defer llmnrMu.Unlock()

		if llmnrRunning {
			appLogger.Debug("LLMNR listener already running")
			return
		}

		ctx, cancel := context.WithCancel(context.Background())
		llmnrCancel = cancel
		llmnrRunning = true

		appLogger.Info("LLMNR: starting listener")

		go func() {
			h := func(job scanner.ScanJob) bool {
				ip := job.IP
				hostname := ""
				if job.Meta != nil {
					if meta, ok := job.Meta.(map[string]interface{}); ok {
						if hn, ok := meta["hostname"].(string); ok {
							hostname = hn
						}
					}
				}

				// Async SNMP enrichment
				go func(ip, hostname string) {
					ctx := context.Background()
					pi, err := LiveDiscoveryDetect(ctx, ip, getSNMPTimeoutSeconds())
					if err != nil {
						appLogger.WarnRateLimited("llmnr_enrich_"+ip, 5*time.Minute, "LLMNR: enrichment failed", "ip", ip, "hostname", hostname, "error", err)
					} else {
						agent.UpsertDiscoveredPrinter(*pi)
						appLogger.Info("LLMNR: discovered device", "ip", ip, "hostname", hostname, "serial", pi.Serial)
					}
				}(ip, hostname)
				return true
			}

			// Call browser with 10-minute throttle window
			agent.StartLLMNRBrowser(ctx, h, llmnrSeen, 10*time.Minute)

			llmnrMu.Lock()
			llmnrRunning = false
			llmnrCancel = nil
			llmnrMu.Unlock()
			appLogger.Info("LLMNR: stopped")
		}()
	}

	stopLLMNR := func() {
		llmnrMu.Lock()
		defer llmnrMu.Unlock()
		if llmnrCancel != nil {
			appLogger.Info("LLMNR: stopping listener")
			llmnrCancel()
			llmnrCancel = nil
		}
	}

	// Declare collectMetricsForSavedDevices first so it can be used in startMetricsRescan
	var collectMetricsForSavedDevices func()

	// Metrics Rescan: Periodically collect metrics from saved devices
	startMetricsRescan := func(intervalMinutes int) {
		metricsRescanMu.Lock()
		defer metricsRescanMu.Unlock()

		if metricsRescanRunning {
			appLogger.Debug("Metrics rescan already running")
			return
		}

		if intervalMinutes < 5 {
			intervalMinutes = 5 // minimum 5 minutes
		}
		if intervalMinutes > 1440 {
			intervalMinutes = 1440 // maximum 24 hours
		}

		metricsRescanInterval = time.Duration(intervalMinutes) * time.Minute
		ctx, cancel := context.WithCancel(context.Background())
		metricsRescanCancel = cancel
		metricsRescanRunning = true

		appLogger.Info("Metrics rescan: starting", "interval_minutes", intervalMinutes)

		go func() {
			defer func() {
				metricsRescanMu.Lock()
				metricsRescanRunning = false
				metricsRescanCancel = nil
				metricsRescanMu.Unlock()
			}()

			// Run immediately on start
			collectMetricsForSavedDevices()

			ticker := time.NewTicker(metricsRescanInterval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					appLogger.Info("Metrics rescan: stopped")
					return
				case <-ticker.C:
					collectMetricsForSavedDevices()
				}
			}
		}()
	}

	stopMetricsRescan := func() {
		metricsRescanMu.Lock()
		defer metricsRescanMu.Unlock()
		if metricsRescanCancel != nil {
			appLogger.Info("Metrics rescan: stopping")
			metricsRescanCancel()
			metricsRescanCancel = nil
		}
	}

	// Define the collection function
	// Collect metrics from ALL devices (saved + discovered) for tiered storage
	collectMetricsForSavedDevices = func() {
		appLogger.Debug("Metrics rescan: collecting snapshots from all devices")
		ctx := context.Background()

		// Get all devices (no IsSaved filter - collect from discovered devices too)
		devices, err := deviceStore.List(ctx, storage.DeviceFilter{})
		if err != nil {
			appLogger.Error("Metrics rescan: failed to list devices", "error", err)
			return
		}

		count := 0
		for _, device := range devices {
			// Extract learned OIDs from device for efficient metrics collection
			pi := storage.DeviceToPrinterInfo(device)
			learnedOIDs := &pi.LearnedOIDs

			// Collect metrics snapshot using learned OIDs if available
			agentSnapshot, err := CollectMetricsWithOIDs(ctx, device.IP, device.Serial, device.Manufacturer, 10, learnedOIDs)
			if err != nil {
				appLogger.WarnRateLimited("metrics_collect_"+device.Serial, 5*time.Minute, "Metrics rescan: collection failed", "serial", device.Serial, "ip", device.IP, "error", err)
				continue
			}

			// Convert to storage type
			storageSnapshot := &storage.MetricsSnapshot{}
			storageSnapshot.Serial = agentSnapshot.Serial
			storageSnapshot.PageCount = agentSnapshot.PageCount
			storageSnapshot.ColorPages = agentSnapshot.ColorPages
			storageSnapshot.MonoPages = agentSnapshot.MonoPages
			storageSnapshot.ScanCount = agentSnapshot.ScanCount
			storageSnapshot.TonerLevels = agentSnapshot.TonerLevels
			storageSnapshot.FaxPages = agentSnapshot.FaxPages
			storageSnapshot.CopyPages = agentSnapshot.CopyPages
			storageSnapshot.OtherPages = agentSnapshot.OtherPages
			storageSnapshot.CopyMonoPages = agentSnapshot.CopyMonoPages
			storageSnapshot.CopyFlatbedScans = agentSnapshot.CopyFlatbedScans
			storageSnapshot.CopyADFScans = agentSnapshot.CopyADFScans
			storageSnapshot.FaxFlatbedScans = agentSnapshot.FaxFlatbedScans
			storageSnapshot.FaxADFScans = agentSnapshot.FaxADFScans
			storageSnapshot.ScanToHostFlatbed = agentSnapshot.ScanToHostFlatbed
			storageSnapshot.ScanToHostADF = agentSnapshot.ScanToHostADF
			storageSnapshot.DuplexSheets = agentSnapshot.DuplexSheets
			storageSnapshot.JamEvents = agentSnapshot.JamEvents
			storageSnapshot.ScannerJamEvents = agentSnapshot.ScannerJamEvents

			// Save to database (error already logged in storage layer)
			if err := deviceStore.SaveMetricsSnapshot(ctx, storageSnapshot); err != nil {
				continue
			}

			count++
		}

		appLogger.Info("Metrics rescan: completed", "device_count", count)
	}

	// Helpers to apply runtime effects for settings (closures to access local start/stop functions)
	applyDiscoveryEffects := func(req map[string]interface{}) {
		if req == nil {
			return
		}
		autoDiscoverEnabled := false
		if v, ok := req["auto_discover_enabled"]; ok {
			if vb, ok2 := v.(bool); ok2 {
				autoDiscoverEnabled = vb
				if vb {
					startAutoDiscover()
					appLogger.Info("Auto Discover enabled via settings")
				} else {
					stopAutoDiscover()
					stopLiveMDNS()
					stopLiveWSDiscovery()
					stopLiveSSDP()
					stopSNMPTrap()
					stopLLMNR()
					appLogger.Info("Auto Discover disabled via settings")
				}
			}
		}

		// Master IP scanning toggle (controls subnet/manual IP scanning)
		if v, ok := req["ip_scanning_enabled"]; ok {
			if vb, ok2 := v.(bool); ok2 {
				if !vb {
					// Stop any periodic per-IP scanning
					stopAutoDiscover()
					appLogger.Info("IP scanning disabled via settings: periodic and manual per-IP scans will be blocked")
				} else {
					// If enabling, only start auto-discover if auto_discover_enabled is true in the provided map
					if ad, ok := req["auto_discover_enabled"]; ok {
						if adb, ok2 := ad.(bool); ok2 && adb {
							startAutoDiscover()
							appLogger.Info("IP scanning enabled via settings: starting periodic scans")
						}
					} else {
						// No auto_discover change provided; do not automatically start periodic scans here
						appLogger.Info("IP scanning enabled via settings")
					}
				}
			}
		}
		if v, ok := req["auto_discover_live_mdns"]; ok {
			if vb, ok2 := v.(bool); ok2 {
				if vb && autoDiscoverEnabled {
					startLiveMDNS()
					appLogger.Info("Live mDNS discovery enabled via settings")
				} else {
					stopLiveMDNS()
					appLogger.Info("Live mDNS discovery disabled via settings")
				}
			}
		}
		if v, ok := req["auto_discover_live_wsd"]; ok {
			if vb, ok2 := v.(bool); ok2 {
				if vb && autoDiscoverEnabled {
					startLiveWSDiscovery()
					appLogger.Info("Live WS-Discovery enabled via settings")
				} else {
					stopLiveWSDiscovery()
					appLogger.Info("Live WS-Discovery disabled via settings")
				}
			}
		}
		if v, ok := req["auto_discover_live_ssdp"]; ok {
			if vb, ok2 := v.(bool); ok2 {
				if vb && autoDiscoverEnabled {
					startLiveSSDP()
					appLogger.Info("Live SSDP discovery enabled via settings")
				} else {
					stopLiveSSDP()
					appLogger.Info("Live SSDP discovery disabled via settings")
				}
			}
		}
		if v, ok := req["auto_discover_live_snmptrap"]; ok {
			if vb, ok2 := v.(bool); ok2 {
				if vb && autoDiscoverEnabled {
					startSNMPTrap()
					appLogger.Info("SNMP Trap listener enabled via settings")
				} else {
					stopSNMPTrap()
					appLogger.Info("SNMP Trap listener disabled via settings")
				}
			}
		}
		if v, ok := req["auto_discover_live_llmnr"]; ok {
			if vb, ok2 := v.(bool); ok2 {
				if vb && autoDiscoverEnabled {
					startLLMNR()
					appLogger.Info("LLMNR listener enabled via settings")
				} else {
					stopLLMNR()
					appLogger.Info("LLMNR listener disabled via settings")
				}
			}
		}
		if v, ok := req["metrics_rescan_enabled"]; ok {
			if vb, ok2 := v.(bool); ok2 {
				if vb {
					interval := 60
					if iv, ok := req["metrics_rescan_interval_minutes"]; ok {
						if ivf, ok2 := iv.(float64); ok2 {
							interval = int(ivf)
						}
					}
					startMetricsRescan(interval)
					appLogger.Info("Metrics monitoring enabled", "interval_minutes", interval)
				} else {
					stopMetricsRescan()
					appLogger.Info("Metrics monitoring disabled")
				}
			}
		}
	}

	// Load saved ranges from database
	rangesText, err := agentConfigStore.GetRanges()
	if err != nil {
		appLogger.Error("Failed to load ranges from database", "error", err.Error())
	} else if rangesText != "" {
		appLogger.Info("Loaded saved ranges (preview)")
		// show a short preview
		lines := strings.Split(rangesText, "\n")
		previewLines := len(lines)
		if previewLines > 5 {
			previewLines = 5
		}
		for i := 0; i < previewLines; i++ {
			appLogger.Debug("Range preview", "line", strings.TrimSpace(lines[i]))
		}
		// Validate the ranges
		res, err := agent.ParseRangeText(rangesText, 4096)
		if err != nil {
			appLogger.Error("Failed to parse saved ranges", "error", err.Error())
		} else if len(res.Errors) > 0 {
			for _, pe := range res.Errors {
				appLogger.Warn("Saved range parse error", "line", pe.Line, "error", pe.Msg)
			}
		} else {
			appLogger.Info("Validated saved addresses", "count", len(res.IPs))
		}
	}

	// Apply configuration from TOML
	if agentConfig != nil {
		// Apply asset ID regex from config
		if agentConfig.AssetIDRegex != "" {
			agent.SetAssetIDRegex(agentConfig.AssetIDRegex)
			appLogger.Info("AssetIDRegex configured from TOML", "pattern", agentConfig.AssetIDRegex)
		} else {
			// reasonable default: five digit numeric asset tags
			agent.SetAssetIDRegex(`\b\d{5}\b`)
			appLogger.Info("Using default AssetIDRegex", "pattern", "five-digit")
		}

		// Apply SNMP community
		if agentConfig.SNMP.Community != "" {
			_ = os.Setenv("SNMP_COMMUNITY", agentConfig.SNMP.Community)
			appLogger.Info("SNMP community configured from TOML")
		}

		// Apply SNMP timeout and retries settings
		scannerConfig.Lock()
		scannerConfig.SNMPTimeoutMs = agentConfig.SNMP.TimeoutMs
		scannerConfig.SNMPRetries = agentConfig.SNMP.Retries
		scannerConfig.DiscoverConcurrency = agentConfig.Concurrency
		appLogger.Info("Scanner config applied from TOML",
			"timeout_ms", scannerConfig.SNMPTimeoutMs,
			"retries", scannerConfig.SNMPRetries,
			"concurrency", scannerConfig.DiscoverConcurrency)
		scannerConfig.Unlock()
	}

	// Load server configuration from TOML and start upload worker
	var uploadWorker *UploadWorker
	if agentConfig != nil && agentConfig.Server.Enabled {
		// Get or generate stable agent ID
		dataDir, err := config.GetDataDirectory("agent", isService)
		if err != nil {
			appLogger.Error("Failed to get data directory", "error", err)
			return
		}

		// Load or generate agent UUID (stable identifier)
		agentID := agentConfig.Server.AgentID
		if agentID == "" {
			agentID, err = LoadOrGenerateAgentID(dataDir)
			if err != nil {
				appLogger.Error("Failed to generate agent ID", "error", err)
				return
			}
			appLogger.Info("Generated new agent ID", "agent_id", agentID)
			// Note: We don't save back to config file - agent_id is persisted separately
		}

		// Use Name field for display purposes, default to hostname if not set
		agentName := agentConfig.Server.Name
		if agentName == "" {
			hostname, _ := os.Hostname()
			agentName = hostname
		}

		appLogger.Info("Server integration enabled",
			"url", agentConfig.Server.URL,
			"agent_id", agentID,
			"agent_name", agentName,
			"ca_path", agentConfig.Server.CAPath,
			"upload_interval", agentConfig.Server.UploadInterval,
			"heartbeat_interval", agentConfig.Server.HeartbeatInterval)

		// Load authentication token from file
		token := LoadServerToken(dataDir)
		if token == "" {
			appLogger.Debug("No saved server token found")
		}

		// Create and start upload worker for server communication
		go func() {
			serverClient := agent.NewServerClientWithName(
				agentConfig.Server.URL,
				agentID,   // Use stable UUID
				agentName, // User-friendly name
				token,
				agentConfig.Server.CAPath,
				agentConfig.Server.InsecureSkipVerify,
			)

			workerConfig := UploadWorkerConfig{
				HeartbeatInterval: time.Duration(agentConfig.Server.HeartbeatInterval) * time.Second,
				UploadInterval:    time.Duration(agentConfig.Server.UploadInterval) * time.Second,
				RetryAttempts:     3,
				RetryBackoff:      2 * time.Second,
				UseWebSocket:      true, // Enable WebSocket for live heartbeat
			}

			uploadWorker = NewUploadWorker(serverClient, deviceStore, appLogger, workerConfig)

			// Start worker (will register if needed)
			if err := uploadWorker.Start(ctx, Version); err != nil {
				appLogger.Error("Failed to start upload worker", "error", err)
				return
			}

			// Save token after successful registration
			if newToken := serverClient.GetToken(); newToken != "" && newToken != token {
				if err := SaveServerToken(dataDir, newToken); err != nil {
					appLogger.Error("Failed to save server token", "error", err)
				} else {
					appLogger.Info("Server token saved")
				}
			}
		}()
	}

	// Load discovery settings from database (user-configurable via web UI)
	{
		var discoverySettings map[string]interface{}
		if agentConfigStore != nil {
			_ = agentConfigStore.GetConfigValue("discovery_settings", &discoverySettings)
		}
		if discoverySettings != nil {
			applyDiscoveryEffects(discoverySettings)
		}
	}

	// Ensure key handlers are registered (register sandbox explicitly so it's
	// always present regardless of init ordering in other files). Use a
	// Start web UI
	// Serve the UI only for the exact root path and GET method. This prevents
	// the UI HTML from being returned as a fallback for other endpoints (e.g.
	// POST /sandbox_simulate) when a handler is missing or not registered.
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "only GET allowed", http.StatusMethodNotAllowed)
			return
		}
		// Serve the HTML from embedded filesystem
		tmpl, err := template.ParseFS(webFS, "web/index.html")
		if err != nil {
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		tmpl.Execute(w, nil)

	})

	// Serve static assets (CSS, JS) from embedded filesystem
	http.HandleFunc("/static/", func(w http.ResponseWriter, r *http.Request) {
		// Strip /static/ prefix to get the filename
		fileName := strings.TrimPrefix(r.URL.Path, "/static/")

		// Serve shared assets from common/web package
		if fileName == "shared.css" {
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
			w.Write([]byte(sharedweb.SharedCSS))
			return
		}
		if fileName == "shared.js" {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			w.Write([]byte(sharedweb.SharedJS))
			return
		}
		if fileName == "metrics.js" {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			w.Write([]byte(sharedweb.MetricsJS))
			return
		}
		if fileName == "cards.js" {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			w.Write([]byte(sharedweb.CardsJS))
			return
		}

		// Serve other files from embedded filesystem
		filePath := "web/" + fileName
		content, err := webFS.ReadFile(filePath)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		// Set appropriate content type
		if strings.HasSuffix(filePath, ".css") {
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		} else if strings.HasSuffix(filePath, ".js") {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		}

		w.Write(content)
	})

	// Helper function to create bool pointer
	boolPtr := func(b bool) *bool {
		return &b
	}

	// SSE endpoint for real-time UI updates
	http.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		// Create client and register with hub
		client := sseHub.NewClient()
		defer sseHub.RemoveClient(client)

		// Send initial connection event
		fmt.Fprintf(w, "event: connected\ndata: {\"message\":\"Connected to event stream\"}\n\n")
		flusher.Flush()

		// Stream events to client
		for {
			select {
			case event := <-client.events:
				// Marshal event data
				data, err := json.Marshal(event.Data)
				if err != nil {
					continue
				}

				// Send SSE formatted event
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, string(data))
				flusher.Flush()

			case <-r.Context().Done():
				// Client disconnected
				return
			}
		}
	})

	// cancel currently running scan (if any)
	// Discovery endpoint - scans saved IP ranges and/or local subnet using discovery pipeline
	// Respects discovery_settings from database (manual_ranges, subnet_scan, method toggles)
	http.HandleFunc("/discover", func(w http.ResponseWriter, r *http.Request) {
		conc := 50
		timeoutSeconds := 5

		// Check for mode parameter (quick vs full)
		mode := r.URL.Query().Get("mode")
		if mode == "" {
			mode = "full" // default to full scan
		}

		// Load discovery settings
		var discoverySettings = map[string]interface{}{
			"subnet_scan":   true,
			"manual_ranges": true,
			"arp_enabled":   true,
			"icmp_enabled":  true,
			"tcp_enabled":   true,
			"snmp_enabled":  true,
			"mdns_enabled":  false,
		}
		if agentConfigStore != nil {
			var stored map[string]interface{}
			if err := agentConfigStore.GetConfigValue("discovery_settings", &stored); err == nil && stored != nil {
				for k, v := range stored {
					discoverySettings[k] = v
				}
			}
		}

		// If IP scanning master toggle is explicitly disabled, skip discovery
		if discoverySettings["ip_scanning_enabled"] == false {
			agent.Info("Discovery skipped: IP scanning disabled in settings")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "discovery skipped: IP scanning is disabled in settings")
			return
		}

		// Get saved ranges from database (if manual ranges enabled)
		var ranges []string
		manualRangesEnabled := discoverySettings["manual_ranges"] == true
		if manualRangesEnabled && agentConfigStore != nil {
			savedRanges, err := agentConfigStore.GetRangesList()
			if err == nil {
				ranges = savedRanges
			}
		}

		// Check if local subnet scanning is enabled
		scanLocalSubnet := discoverySettings["subnet_scan"] == true

		// Determine what to scan
		if len(ranges) > 0 {
			agent.Info(fmt.Sprintf("Starting Discover with %d saved addresses", len(ranges)))
		} else if scanLocalSubnet {
			agent.Info("Starting Auto Discover (local subnet)")
			// Empty ranges will trigger auto subnet detection
		} else {
			agent.Info("Discovery skipped: no saved ranges and subnet scan disabled")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "discovery skipped: enable subnet scanning or configure ranges in Settings")
			return
		}

		// Build DiscoveryConfig from settings
		discoveryCfg := &agent.DiscoveryConfig{
			ARPEnabled:  discoverySettings["arp_enabled"] == true,
			ICMPEnabled: discoverySettings["icmp_enabled"] == true,
			TCPEnabled:  discoverySettings["tcp_enabled"] == true,
			SNMPEnabled: discoverySettings["snmp_enabled"] == true,
			MDNSEnabled: discoverySettings["mdns_enabled"] == true,
		}

		// Build saved device IP map for bypass when detection is disabled
		savedDeviceIPs := make(map[string]bool)
		if deviceStore != nil {
			ctx := context.Background()
			saved := true
			savedDevices, err := deviceStore.List(ctx, storage.DeviceFilter{IsSaved: &saved})
			if err == nil {
				for _, dev := range savedDevices {
					savedDeviceIPs[dev.IP] = true
				}
				if len(savedDeviceIPs) > 0 {
					appLogger.Info("Discovery will bypass detection for saved devices", "count", len(savedDeviceIPs))
				}
			}
		}

		// Use new scanner for all discovery
		ctx := context.Background()
		printers, err := Discover(ctx, ranges, mode, discoveryCfg, deviceStore, conc, timeoutSeconds)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(printers)
	})

	// Synchronous discovery endpoint (quick Phase A scan) backed by discover.go
	http.HandleFunc("/discover_now", handleDiscover)

	// Removed /saved_ranges, /ranges, and /clear_ranges in favor of unified /settings

	// GET /devices/discovered - List discovered devices with optional filters
	// Query params:
	//   - minutes: only show devices discovered in last X minutes (default: no filter)
	//   - include_known: include already saved/known devices (default: false)
	http.HandleFunc("/devices/discovered", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Parse query parameters
		minutesStr := r.URL.Query().Get("minutes")
		includeKnown := r.URL.Query().Get("include_known") == "true"

		// Query discovered devices from database
		ctx := context.Background()

		// Build filter
		filter := storage.DeviceFilter{
			Visible: boolPtr(true), // Only visible devices
		}

		// Filter by save status unless include_known is true
		if !includeKnown {
			filter.IsSaved = boolPtr(false) // Only unsaved (new) devices
		}

		// Filter by time if minutes parameter provided
		if minutesStr != "" {
			if minutes, err := strconv.Atoi(minutesStr); err == nil && minutes > 0 {
				cutoff := time.Now().Add(-time.Duration(minutes) * time.Minute)
				filter.LastSeenAfter = &cutoff
			}
		}

		devices, err := deviceStore.List(ctx, filter)
		if err != nil {
			appLogger.Error("Error listing discovered devices", "error", err.Error())
			json.NewEncoder(w).Encode([]agent.PrinterInfo{})
			return
		}

		// Convert devices to PrinterInfo and enrich with latest metrics
		printers := make([]agent.PrinterInfo, len(devices))
		for i, dev := range devices {
			printers[i] = storage.DeviceToPrinterInfo(dev)

			// Fetch latest metrics for this device
			if dev.Serial != "" {
				if snapshot, err := deviceStore.GetLatestMetrics(ctx, dev.Serial); err == nil && snapshot != nil {
					printers[i].PageCount = snapshot.PageCount
					// Convert TonerLevels from map[string]interface{} to map[string]int
					if snapshot.TonerLevels != nil {
						toner := make(map[string]int)
						for k, v := range snapshot.TonerLevels {
							if level, ok := v.(float64); ok {
								toner[k] = int(level)
							} else if level, ok := v.(int); ok {
								toner[k] = level
							}
						}
						printers[i].TonerLevels = toner
					}
				}
			}
		}

		json.NewEncoder(w).Encode(printers)
	})

	// POST /devices/clear_discovered - Hide discovered devices (soft delete)
	http.HandleFunc("/devices/clear_discovered", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}

		// Hide discovered devices in database
		ctx := context.Background()
		count, err := deviceStore.HideDiscovered(ctx)
		if err != nil {
			appLogger.Error("Error hiding discovered devices", "error", err.Error())
			http.Error(w, "failed to hide discovered", http.StatusInternalServerError)
			return
		}
		appLogger.Info("Hidden discovered devices", "count", count)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "hidden %d devices", count)
	})

	// POST /database/clear - Backup current database and start fresh
	http.HandleFunc("/database/clear", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}

		appLogger.Info("Database clear requested - backing up and resetting")

		// Get the SQLiteStore to call backupAndReset
		if sqliteStore, ok := deviceStore.(*storage.SQLiteStore); ok {
			if err := sqliteStore.BackupAndReset(); err != nil {
				appLogger.Error("Failed to backup and reset database", "error", err)
				http.Error(w, fmt.Sprintf("failed to reset database: %v", err), http.StatusInternalServerError)
				return
			}

			appLogger.Info("Database backed up and reset successfully")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"message": "Database backed up and reset successfully",
				"reload":  true, // Signal UI to reload
			})
		} else {
			http.Error(w, "database type does not support reset", http.StatusBadRequest)
		}
	})

	// Use /devices/get?serial=X for device details by serial
	// Use /devices/list with filters for querying by IP

	// Use /devices/metrics/history for metrics data

	http.HandleFunc("/logs", func(w http.ResponseWriter, r *http.Request) {
		// Optional query params: level=ERROR|WARN|INFO|DEBUG|TRACE, tail=N
		q := r.URL.Query()
		levelStr := strings.ToUpper(strings.TrimSpace(q.Get("level")))
		tailStr := strings.TrimSpace(q.Get("tail"))

		// Map for level parsing local to this handler
		levelMap := map[string]int{
			"ERROR": 0,
			"WARN":  1,
			"INFO":  2,
			"DEBUG": 3,
			"TRACE": 4,
		}
		minLevel, haveLevel := levelMap[levelStr]

		// Parse tail count
		tail := 0
		if tailStr != "" {
			if n, err := strconv.Atoi(tailStr); err == nil && n > 0 {
				tail = n
			}
		}

		// Get buffered entries from app logger
		entries := appLogger.GetBuffer()

		// Filter by level if requested (include entries with level <= minLevel)
		if haveLevel {
			filtered := make([]logger.LogEntry, 0, len(entries))
			for _, e := range entries {
				if int(e.Level) <= minLevel {
					filtered = append(filtered, e)
				}
			}
			entries = filtered
		}

		// Tail if requested
		if tail > 0 && len(entries) > tail {
			entries = entries[len(entries)-tail:]
		}

		// Write as plain text compatible with prior behavior
		w.Header().Set("Content-Type", "text/plain")
		var b strings.Builder
		for i, e := range entries {
			// Format similar to logger package
			ts := e.Timestamp.Format("2006-01-02T15:04:05-07:00")
			// Best-effort level name mapping
			levelName := "INFO"
			switch e.Level {
			case 0:
				levelName = "ERROR"
			case 1:
				levelName = "WARN"
			case 2:
				levelName = "INFO"
			case 3:
				levelName = "DEBUG"
			case 4:
				levelName = "TRACE"
			}
			b.WriteString(fmt.Sprintf("%s [%s] %s", ts, levelName, e.Message))
			if len(e.Context) > 0 {
				for k, v := range e.Context {
					b.WriteString(fmt.Sprintf(" %s=%v", k, v))
				}
			}
			if i < len(entries)-1 {
				b.WriteString("\n")
			}
		}
		fmt.Fprint(w, b.String())
	})

	// Download a zip archive of the entire logs directory
	http.HandleFunc("/logs/archive", func(w http.ResponseWriter, r *http.Request) {
		logDir := filepath.Join(".", "logs")
		if st, err := os.Stat(logDir); err != nil || !st.IsDir() {
			http.Error(w, "logs directory not found", http.StatusNotFound)
			return
		}
		fname := fmt.Sprintf("logs_%s.zip", time.Now().Format("20060102_150405"))
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fname))

		zw := zip.NewWriter(w)
		defer zw.Close()

		// Walk logs directory and add files
		_ = filepath.Walk(logDir, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // skip problematic entries
			}
			if info.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(logDir, p)
			if err != nil {
				rel = info.Name()
			}
			// Normalize to forward slashes for zip entries
			zipName := strings.ReplaceAll(rel, "\\", "/")
			f, err := os.Open(p)
			if err != nil {
				return nil
			}
			defer f.Close()
			wtr, err := zw.Create(zipName)
			if err != nil {
				return nil
			}
			_, _ = io.Copy(wtr, f)
			return nil
		})
	})

	// Clear logs by rotating the current log file and clearing the buffer
	http.HandleFunc("/logs/clear", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Force rotation to archive current log and start fresh
		appLogger.ForceRotate()
		// Clear the in-memory buffer
		appLogger.ClearBuffer()
		appLogger.Info("Logs cleared and rotated by user request")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"success": true, "message": "Logs cleared and rotated"}`)
	})

	// Endpoint to return unknown manufacturer log entries (if present)
	http.HandleFunc("/unknown_manufacturers", func(w http.ResponseWriter, r *http.Request) {
		logDir := filepath.Join(".", "logs")
		fpath := filepath.Join(logDir, "unknown_mfg.log")
		data, err := os.ReadFile(fpath)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode([]string{})
			return
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(lines)
	})

	// Endpoint to fetch parse debug for an IP (returns in-memory snapshot or persisted JSON)
	http.HandleFunc("/parse_debug", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		ip := q.Get("ip")
		if ip == "" {
			http.Error(w, "ip parameter required", http.StatusBadRequest)
			return
		}
		// try in-memory snapshot first
		if d, ok := agent.GetParseDebug(ip); ok {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(d)
			return
		}
		// fallback to persisted file
		logDir := filepath.Join(".", "logs")
		fpath := filepath.Join(logDir, fmt.Sprintf("parse_debug_%s.json", strings.ReplaceAll(ip, ".", "_")))
		data, err := os.ReadFile(fpath)
		if err != nil {
			http.Error(w, "no diagnostics found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	})

	// Endpoint to return current scan metrics snapshot
	// TODO(deprecate): Remove /scan_metrics endpoint - superseded by metrics API
	// Still used by UI metrics display, needs replacement before removal
	http.HandleFunc("/scan_metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(agent.GetMetricsSnapshot())
	})

	// Serve the on-disk logfile for easier inspection
	http.HandleFunc("/logfile", func(w http.ResponseWriter, r *http.Request) {
		fpath := filepath.Join(".", "logs", "agent.log")
		data, err := os.ReadFile(fpath)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, "logfile not found")
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write(data)
	})

	// Check for database rotation event (GET) or clear the warning (POST)
	http.HandleFunc("/database/rotation_warning", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.Method {
		case "GET":
			// Check if rotation flag is set
			var rotationInfo map[string]interface{}
			err := agentConfigStore.GetConfigValue("database_rotation", &rotationInfo)
			if err != nil || rotationInfo == nil {
				// No rotation event
				json.NewEncoder(w).Encode(map[string]interface{}{
					"rotated":     false,
					"rotated_at":  nil,
					"backup_path": nil,
				})
				return
			}

			// Rotation event found
			json.NewEncoder(w).Encode(map[string]interface{}{
				"rotated":     true,
				"rotated_at":  rotationInfo["rotated_at"],
				"backup_path": rotationInfo["backup_path"],
			})
		case "POST":
			// Clear the rotation warning flag
			if err := agentConfigStore.SetConfigValue("database_rotation", nil); err != nil {
				appLogger.Error("Failed to clear rotation warning", "error", err)
				http.Error(w, "Failed to clear warning", http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"message": "Rotation warning cleared",
			})
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	appLogger.Info("Web UI running", "url", "http://localhost:8080")

	// Refresh device profile by serial (or IP). POST JSON { "serial": "...", "ip": "optional ip" }
	http.HandleFunc("/devices/refresh", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Serial string `json:"serial"`
			IP     string `json:"ip"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		req.Serial = strings.TrimSpace(req.Serial)
		if req.Serial == "" && req.IP == "" {
			http.Error(w, "serial or ip required", http.StatusBadRequest)
			return
		}
		// if serial provided but no IP, try load existing device to get IP
		targetIP := strings.TrimSpace(req.IP)
		if targetIP == "" && req.Serial != "" {
			devPath := filepath.Join(".", "logs", "devices", req.Serial+".json")
			if b, err := os.ReadFile(devPath); err == nil {
				var doc map[string]interface{}
				if json.Unmarshal(b, &doc) == nil {
					if pi, ok := doc["printer_info"].(map[string]interface{}); ok {
						if ipval, ok2 := pi["ip"].(string); ok2 {
							targetIP = strings.TrimSpace(ipval)
						}
						if targetIP == "" {
							if ipval2, ok3 := pi["IP"].(string); ok3 {
								targetIP = strings.TrimSpace(ipval2)
							}
						}
					}
				}
			}
		}
		if targetIP == "" {
			http.Error(w, "unable to determine target ip for refresh", http.StatusBadRequest)
			return
		}
		ctx := context.Background()
		pi, err := LiveDiscoveryDetect(ctx, targetIP, getSNMPTimeoutSeconds())
		if err != nil {
			http.Error(w, "refresh failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		agent.UpsertDiscoveredPrinter(*pi)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "serial": pi.Serial})
	})

	// Update device fields (now supports many fields; respects locked fields at the UI level)
	http.HandleFunc("/devices/update", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Serial       string    `json:"serial"`
			Manufacturer *string   `json:"manufacturer,omitempty"`
			Model        *string   `json:"model,omitempty"`
			Hostname     *string   `json:"hostname,omitempty"`
			Firmware     *string   `json:"firmware,omitempty"`
			IP           *string   `json:"ip,omitempty"`
			SubnetMask   *string   `json:"subnet_mask,omitempty"`
			Gateway      *string   `json:"gateway,omitempty"`
			DNSServers   *[]string `json:"dns_servers,omitempty"`
			DHCPServer   *string   `json:"dhcp_server,omitempty"`
			AssetNumber  *string   `json:"asset_number,omitempty"`
			Location     *string   `json:"location,omitempty"`
			Description  *string   `json:"description,omitempty"`
			WebUIURL     *string   `json:"web_ui_url,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		req.Serial = strings.TrimSpace(req.Serial)
		if req.Serial == "" {
			http.Error(w, "serial required", http.StatusBadRequest)
			return
		}

		// Get existing device
		ctx := context.Background()
		device, err := deviceStore.Get(ctx, req.Serial)
		if err != nil {
			http.Error(w, "device not found: "+err.Error(), http.StatusNotFound)
			return
		}

		// Helper to check if field is locked
		isFieldLocked := func(fieldName string) bool {
			if device.LockedFields == nil {
				return false
			}
			for _, lf := range device.LockedFields {
				if strings.EqualFold(lf.Field, fieldName) {
					return true
				}
			}
			return false
		}

		// Update only provided fields (skip locked fields)
		if req.Manufacturer != nil && !isFieldLocked("manufacturer") {
			device.Manufacturer = *req.Manufacturer
		}
		if req.Model != nil && !isFieldLocked("model") {
			device.Model = *req.Model
		}
		if req.Hostname != nil && !isFieldLocked("hostname") {
			device.Hostname = *req.Hostname
		}
		if req.Firmware != nil && !isFieldLocked("firmware") {
			device.Firmware = *req.Firmware
		}
		if req.IP != nil && !isFieldLocked("ip") {
			device.IP = *req.IP
		}
		if req.SubnetMask != nil && !isFieldLocked("subnet_mask") {
			device.SubnetMask = *req.SubnetMask
		}
		if req.Gateway != nil && !isFieldLocked("gateway") {
			device.Gateway = *req.Gateway
		}
		if req.DNSServers != nil && !isFieldLocked("dns_servers") {
			device.DNSServers = *req.DNSServers
		}
		if req.DHCPServer != nil && !isFieldLocked("dhcp_server") {
			device.DHCPServer = *req.DHCPServer
		}
		if req.AssetNumber != nil && !isFieldLocked("asset_number") {
			device.AssetNumber = *req.AssetNumber
		}
		if req.Location != nil && !isFieldLocked("location") {
			device.Location = *req.Location
		}
		if req.Description != nil && !isFieldLocked("description") {
			device.Description = *req.Description
		}
		if req.WebUIURL != nil && !isFieldLocked("web_ui_url") {
			device.WebUIURL = *req.WebUIURL
		}

		// Save updated device
		if err := deviceStore.Update(ctx, device); err != nil {
			http.Error(w, "update failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "serial": device.Serial})
	})

	// Preview device updates: perform a live walk+parse but DO NOT write to DB; returns proposed fields
	http.HandleFunc("/devices/preview", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct{ Serial, IP string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		req.Serial = strings.TrimSpace(req.Serial)
		// If IP not provided, try to load from DB
		if strings.TrimSpace(req.IP) == "" && req.Serial != "" {
			if dev, err := deviceStore.Get(context.Background(), req.Serial); err == nil {
				req.IP = dev.IP
			}
		}
		if strings.TrimSpace(req.IP) == "" {
			http.Error(w, "ip required", http.StatusBadRequest)
			return
		}

		// Build SNMP client and perform a full diagnostic walk (no stop keywords)
		cfg, err := agent.GetSNMPConfig()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		client, err := agent.NewSNMPClient(cfg, req.IP, 5)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer client.Close()

		cols := agent.FullDiagnosticWalk(client, nil, []string{"1.3.6.1.2.1", "1.3.6.1.2.1.43", "1.3.6.1.4.1"}, 10000)
		pi, _ := agent.ParsePDUs(req.IP, cols, nil, func(string) {})

		// Return only the fields relevant for device details
		proposed := map[string]interface{}{
			"ip":           pi.IP,
			"manufacturer": pi.Manufacturer,
			"model":        pi.Model,
			"hostname":     pi.Hostname,
			"firmware":     pi.Firmware,
			"subnet_mask":  pi.SubnetMask,
			"gateway":      pi.Gateway,
			"dns_servers":  pi.DNSServers,
			"dhcp_server":  pi.DHCPServer,
			"asset_number": pi.AssetID,
			"location":     pi.Location,
			"description":  pi.Description,
			"web_ui_url":   pi.WebUIURL,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"proposed": proposed})
	})

	// Toggle a field lock on a device
	http.HandleFunc("/devices/lock", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Serial       string      `json:"serial"`
			Field        string      `json:"field"`
			Lock         bool        `json:"lock"`
			CurrentValue interface{} `json:"current_value,omitempty"` // Value to search for when locking
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if req.Serial == "" || req.Field == "" {
			http.Error(w, "serial and field required", http.StatusBadRequest)
			return
		}
		ctx := context.Background()
		device, err := deviceStore.Get(ctx, req.Serial)
		if err != nil {
			http.Error(w, "device not found: "+err.Error(), http.StatusNotFound)
			return
		}

		// Ensure LockedFields slice exists
		if device.LockedFields == nil {
			device.LockedFields = []storage.FieldLock{}
		}
		// helper to check presence
		has := -1
		for i, lf := range device.LockedFields {
			if strings.EqualFold(lf.Field, req.Field) {
				has = i
				break
			}
		}

		// When locking a field, try to learn the OID for the current value
		var foundOID string
		if req.Lock && has == -1 && req.CurrentValue != nil {
			// Perform SNMP walk to find OID matching the locked value
			foundOID = tryLearnOIDForValue(ctx, device.IP, device.Manufacturer, req.Field, req.CurrentValue)
			if foundOID != "" {
				appLogger.Info("FIELD_LOCK_OID_LEARNED",
					"ip", device.IP,
					"manufacturer", device.Manufacturer,
					"model", device.Model,
					"serial", device.Serial,
					"field", req.Field,
					"value", req.CurrentValue,
					"found_oid", foundOID)

				// Store the learned OID in device RawData
				pi := storage.DeviceToPrinterInfo(device)
				switch strings.ToLower(req.Field) {
				case "page_count", "total_pages":
					pi.LearnedOIDs.PageCountOID = foundOID
				case "mono_pages", "mono_impressions":
					pi.LearnedOIDs.MonoPagesOID = foundOID
				case "color_pages", "color_impressions":
					pi.LearnedOIDs.ColorPagesOID = foundOID
				case "serial":
					pi.LearnedOIDs.SerialOID = foundOID
				case "model":
					pi.LearnedOIDs.ModelOID = foundOID
				default:
					// Store in vendor-specific OIDs
					if pi.LearnedOIDs.VendorSpecificOIDs == nil {
						pi.LearnedOIDs.VendorSpecificOIDs = make(map[string]string)
					}
					pi.LearnedOIDs.VendorSpecificOIDs[req.Field] = foundOID
				}
				// Update device with learned OIDs
				if device.RawData == nil {
					device.RawData = make(map[string]interface{})
				}
				device.RawData["learned_oids"] = pi.LearnedOIDs
			}
		}

		if req.Lock {
			if has == -1 {
				device.LockedFields = append(device.LockedFields, storage.FieldLock{Field: req.Field, LockedAt: time.Now(), Reason: "user_locked"})
			}
		} else {
			if has >= 0 {
				device.LockedFields = append(device.LockedFields[:has], device.LockedFields[has+1:]...)
			}
		}
		if err := deviceStore.Update(ctx, device); err != nil {
			http.Error(w, "failed to update locks: "+err.Error(), http.StatusInternalServerError)
			return
		}

		response := map[string]interface{}{
			"status": "ok",
		}
		if foundOID != "" {
			response["learned_oid"] = foundOID
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	// Endpoint: Save Web UI credentials (moved out of proxy response modifier)
	http.HandleFunc("/device/webui-credentials", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method { //nolint:exhaustive
		case http.MethodGet:
			serial := r.URL.Query().Get("serial")
			if serial == "" {
				http.Error(w, "serial required", http.StatusBadRequest)
				return
			}
			cr, err := getCreds(serial)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{"exists": false})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"exists": true, "username": cr.Username, "auth_type": cr.AuthType, "auto_login": cr.AutoLogin})
		case http.MethodPost:
			var req struct {
				Serial    string `json:"serial"`
				Username  string `json:"username"`
				Password  string `json:"password"`
				AuthType  string `json:"auth_type"`
				AutoLogin bool   `json:"auto_login"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			if req.Serial == "" {
				http.Error(w, "serial required", http.StatusBadRequest)
				return
			}
			enc := ""
			if req.Password != "" && len(secretKey) == 32 {
				if v, err := commonutil.EncryptToB64(secretKey, req.Password); err == nil {
					enc = v
				}
			}
			cr := credRecord{Username: req.Username, Password: enc, AuthType: strings.ToLower(req.AuthType), AutoLogin: req.AutoLogin}
			if err := saveCreds(req.Serial, cr); err != nil {
				http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Proxy printer web UI - /proxy/<serial>/<path...>
	http.HandleFunc("/proxy/", func(w http.ResponseWriter, r *http.Request) {
		// Determine if request is over HTTPS
		isHTTPS := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"

		// Set a timeout for the entire proxy request and store HTTPS status
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		ctx = context.WithValue(ctx, isHTTPSContextKey, isHTTPS)
		r = r.WithContext(ctx)

		// Extract serial from path: /proxy/SERIAL123/remaining/path
		pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/proxy/"), "/")
		if len(pathParts) == 0 || pathParts[0] == "" {
			http.Error(w, "serial required in path", http.StatusBadRequest)
			return
		}
		serial := pathParts[0]

		// Check if this looks like a resource path without a valid serial (e.g., /proxy/js/... or /proxy/css/...)
		// This happens when relative URLs like "../js/file.js" escape the serial directory
		// Common resource directories that shouldn't be treated as serials
		resourceDirs := []string{"js", "css", "images", "strings", "lib", "fonts", "assets", "static", "startwlm", "wlmeng"}
		for _, dir := range resourceDirs {
			if serial == dir {
				appLogger.Debug("Proxy: detected resource path without serial", "path", r.URL.Path, "referer", r.Header.Get("Referer"))
				// Try to extract serial from Referer header
				if referer := r.Header.Get("Referer"); referer != "" {
					if refURL, err := url.Parse(referer); err == nil && strings.HasPrefix(refURL.Path, "/proxy/") {
						refParts := strings.Split(strings.TrimPrefix(refURL.Path, "/proxy/"), "/")
						if len(refParts) > 0 && refParts[0] != "" {
							// Check if the referer's serial is valid
							refSerial := refParts[0]
							isValidSerial := true
							for _, resDir := range resourceDirs {
								if refSerial == resDir {
									isValidSerial = false
									break
								}
							}
							if isValidSerial {
								// Redirect to the correct path with serial
								correctPath := "/proxy/" + refSerial + strings.TrimPrefix(r.URL.Path, "/proxy")
								appLogger.Debug("Proxy: redirecting resource to correct serial path", "from", r.URL.Path, "to", correctPath)
								http.Redirect(w, r, correctPath, http.StatusFound)
								return
							}
						}
					}
				}
				http.Error(w, "Invalid proxy path - serial number required. Resource paths must include device serial.", http.StatusBadRequest)
				return
			}
		}

		// Look up device (use the timeout context from above)
		device, err := deviceStore.Get(ctx, serial)
		if err != nil {
			appLogger.Warn("Proxy: device lookup failed", "serial", serial, "error", err.Error(), "path", r.URL.Path)
			http.Error(w, "device not found: "+err.Error(), http.StatusNotFound)
			return
		}
		appLogger.Debug("Proxy: device found", "serial", serial, "ip", device.IP, "manufacturer", device.Manufacturer)

		// Determine target URL (prefer web_ui_url, fallback to http://<ip>)
		targetURL := device.WebUIURL
		if targetURL == "" {
			targetURL = "http://" + device.IP
		}

		target, err := url.Parse(targetURL)
		if err != nil {
			http.Error(w, "invalid target URL", http.StatusInternalServerError)
			return
		}

		// Build target path early for fast static resource detection
		targetPath := "/"
		if len(pathParts) > 1 {
			targetPath = "/" + strings.Join(pathParts[1:], "/")
		}
		targetPath = strings.ReplaceAll(targetPath, "//", "/")

		// Fast path for static resources - skip all auth logic for performance
		// These resources don't need authentication and checking on every request is slow
		staticExtensions := []string{".js", ".css", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".woff", ".woff2", ".ttf", ".eot"}
		isStaticResource := false
		lowerPath := strings.ToLower(targetPath)
		for _, ext := range staticExtensions {
			if strings.HasSuffix(lowerPath, ext) {
				isStaticResource = true
				break
			}
		}

		// Check cache for static resources first
		if isStaticResource {
			cacheKey := serial + ":" + targetPath
			if data, contentType, headers, ok := staticCache.Get(cacheKey); ok {
				appLogger.Debug("Proxy: serving from cache", "serial", serial, "path", targetPath, "size", len(data))
				// Copy cached headers
				for key, values := range headers {
					for _, value := range values {
						w.Header().Add(key, value)
					}
				}
				if contentType != "" {
					w.Header().Set("Content-Type", contentType)
				}
				w.Header().Set("Cache-Control", "public, max-age=3600") // Browser can cache for 1 hour
				w.WriteHeader(http.StatusOK)
				w.Write(data)
				return
			}
		}

		// Pre-authenticate for main page requests when auto-login is enabled
		// This ensures cookies are available before the page loads (important for Kyocera)
		if !isStaticResource && (targetPath == "/" || targetPath == "") {
			cr, err := getCreds(serial)
			if err == nil && cr != nil && cr.AuthType == "form" && cr.AutoLogin {
				// Check if we have a valid session cached
				cachedJar := proxySessionCache.Get(serial)
				hasValidSession := false
				if cachedJar != nil {
					// Verify the cached jar is valid
					func() {
						defer func() {
							if r := recover(); r != nil {
								appLogger.Debug("Proxy: cached session invalid on pre-auth check", "serial", serial)
								proxySessionCache.Clear(serial)
								cachedJar = nil
							}
						}()
						if targetParsed, err := url.Parse(targetURL); err == nil {
							cookies := cachedJar.Cookies(targetParsed)
							hasValidSession = len(cookies) > 0
							appLogger.Debug("Proxy: pre-auth session check", "serial", serial, "has_session", hasValidSession, "cookie_count", len(cookies))
						}
					}()
				}

				// If no valid session, perform login now before serving the page
				if !hasValidSession && len(secretKey) == 32 && cr.Password != "" {
					appLogger.Info("Proxy: pre-authenticating for main page request", "serial", serial, "manufacturer", device.Manufacturer)
					if adapter := proxy.GetAdapterForManufacturer(device.Manufacturer); adapter != nil {
						if pw, err := commonutil.DecryptFromB64(secretKey, cr.Password); err == nil {
							if jar, err := adapter.Login(targetURL, cr.Username, pw, appLogger); err == nil {
								proxySessionCache.Set(serial, jar)
								appLogger.Info("Proxy: pre-auth successful, cookies ready", "serial", serial, "manufacturer", device.Manufacturer)

								// Send cookies to browser and redirect to same URL to reload with auth
								if targetParsed, err := url.Parse(targetURL); err == nil {
									cookies := jar.Cookies(targetParsed)
									// Get HTTPS status from context
									isHTTPS := false
									if v := r.Context().Value(isHTTPSContextKey); v != nil {
										isHTTPS = v.(bool)
									}
									for _, cookie := range cookies {
										// Rewrite cookie path for proxy
										if cookie.Path == "" || cookie.Path == "/" {
											cookie.Path = "/proxy/" + serial + "/"
										} else if !strings.HasPrefix(cookie.Path, "/proxy/"+serial) {
											cookie.Path = "/proxy/" + serial + cookie.Path
										}
										cookie.Domain = ""
										// Set Secure flag based on current connection type
										// If agent is accessed via HTTPS, keep Secure=true; if HTTP, clear it
										cookie.Secure = isHTTPS
										if cookie.SameSite == 0 {
											cookie.SameSite = http.SameSiteLaxMode
										}
										http.SetCookie(w, cookie)
										appLogger.Debug("Proxy: pre-auth set cookie", "serial", serial, "name", cookie.Name, "path", cookie.Path, "secure", cookie.Secure)
									}
								}

								// Redirect to same URL to reload with cookies
								w.Header().Set("Location", r.URL.Path)
								w.WriteHeader(http.StatusFound)
								return
							} else {
								appLogger.Warn("Proxy: pre-auth login failed", "serial", serial, "error", err.Error())
							}
						}
					}
				}
			}
		}

		// Create reverse proxy
		rproxy := httputil.NewSingleHostReverseProxy(target)

		// Handle form-based login if configured (skip for static resources)
		var sessionJar http.CookieJar
		var cr *credRecord
		if !isStaticResource {
			var err error
			cr, err = getCreds(serial)
			appLogger.Debug("Proxy: checking credentials", "serial", serial, "has_creds", cr != nil, "get_error", err)
			if err == nil && cr != nil {
				appLogger.Debug("Proxy: credentials found", "serial", serial, "auth_type", cr.AuthType, "auto_login", cr.AutoLogin, "username", cr.Username)
			}
			if err == nil && cr != nil && cr.AuthType == "form" && cr.AutoLogin {
				appLogger.Info("Proxy: form auth configured for device", "serial", serial, "manufacturer", device.Manufacturer)
				// Check session cache first
				sessionJar = proxySessionCache.Get(serial)
				// Verify the jar is actually usable - sometimes cache returns non-nil but jar is invalid
				//lint:ignore SA4023 sessionJar is an interface and can be nil
				if sessionJar != nil {
					// Test if jar is actually usable by trying to get cookies
					func() {
						defer func() {
							if r := recover(); r != nil {
								appLogger.Warn("Proxy: cached jar is invalid, clearing", "serial", serial, "error", fmt.Sprintf("%v", r))
								sessionJar = nil
								proxySessionCache.Clear(serial)
							}
						}()
						if targetParsed, err := url.Parse(targetURL); err == nil {
							_ = sessionJar.Cookies(targetParsed)
							appLogger.Debug("Proxy: session cache check - jar is valid", "serial", serial)
						}
					}()
				}
				appLogger.Debug("Proxy: session cache check", "serial", serial, "cached", sessionJar != nil)
				if sessionJar == nil && len(secretKey) == 32 && cr.Password != "" {
					appLogger.Debug("Proxy: attempting fresh login", "serial", serial, "manufacturer", device.Manufacturer)
					// Attempt vendor-specific login
					if adapter := proxy.GetAdapterForManufacturer(device.Manufacturer); adapter != nil {
						appLogger.Debug("Proxy: attempting vendor login", "manufacturer", device.Manufacturer, "serial", serial, "adapter", adapter.Name())
						if pw, err := commonutil.DecryptFromB64(secretKey, cr.Password); err == nil {
							if jar, err := adapter.Login(targetURL, cr.Username, pw, appLogger); err == nil {
								sessionJar = jar
								proxySessionCache.Set(serial, jar)
								// Log cookies that were received
								if targetParsed, err := url.Parse(targetURL); err == nil {
									cookies := jar.Cookies(targetParsed)
									appLogger.Info("Proxy: logged into device", "manufacturer", device.Manufacturer, "serial", serial, "adapter", adapter.Name(), "cookies_received", len(cookies))
									for i, c := range cookies {
										appLogger.Debug("Proxy: received cookie", "index", i, "name", c.Name, "value_length", len(c.Value), "path", c.Path, "domain", c.Domain)
									}
								} else {
									appLogger.Info("Proxy: logged into device", "manufacturer", device.Manufacturer, "serial", serial, "adapter", adapter.Name())
								}
							} else {
								appLogger.WarnRateLimited("proxy_login_"+serial, 5*time.Minute, "Proxy: login failed", "serial", serial, "error", err.Error())
							}
						} else {
							appLogger.Warn("Proxy: password decryption failed", "serial", serial, "error", err.Error())
						}
					} else {
						appLogger.Debug("Proxy: no adapter found for manufacturer", "manufacturer", device.Manufacturer, "serial", serial)
					}
				}

				// If we have valid session cookies and user is accessing a login/password page,
				// redirect them to the home page instead (autologin bypass)
				if sessionJar != nil {
					// Build target path first to check it
					targetPath := "/"
					if len(pathParts) > 1 {
						targetPath = "/" + strings.Join(pathParts[1:], "/")
					}
					targetPath = strings.ReplaceAll(targetPath, "//", "/")

					loginPaths := []string{
						"/PRESENTATION/ADVANCED/PASSWORD",
						"/login",
						"/auth",
					}
					for _, loginPath := range loginPaths {
						if strings.HasPrefix(strings.ToUpper(targetPath), strings.ToUpper(loginPath)) {
							appLogger.Info("Proxy: redirecting authenticated user from login page to home", "serial", serial, "original_path", targetPath)

							// Send Set-Cookie headers to browser so it stores the session cookies
							if targetParsed, err := url.Parse(targetURL); err == nil {
								cookies := sessionJar.Cookies(targetParsed)
								proxyPrefix := "/proxy/" + serial
								for _, cookie := range cookies {
									// Clone the cookie and rewrite path for proxy
									browserCookie := &http.Cookie{
										Name:     cookie.Name,
										Value:    cookie.Value,
										Path:     proxyPrefix + "/",
										Domain:   "",
										MaxAge:   cookie.MaxAge,
										Secure:   false, // We're on localhost
										HttpOnly: cookie.HttpOnly,
										SameSite: http.SameSiteLaxMode,
									}
									http.SetCookie(w, browserCookie)
									appLogger.Debug("Proxy: sending Set-Cookie to browser", "name", cookie.Name, "path", browserCookie.Path)
								}
							}

							// Send HTML that redirects the top-level frame (not just iframe)
							// This ensures the entire page reloads with cookies, not just the iframe
							w.Header().Set("Content-Type", "text/html; charset=utf-8")
							w.WriteHeader(http.StatusOK)
							redirectHTML := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>Logged In - Redirecting...</title>
<script>
// Redirect the top-level window to ensure full page reload with cookies
window.top.location.href = '/proxy/%s/';
</script>
</head>
<body>
<p>Login successful. Redirecting...</p>
<noscript>
<p>Please enable JavaScript or <a href="/proxy/%s/">click here</a> to continue.</p>
</noscript>
</body>
</html>`, serial, serial)
							fmt.Fprint(w, redirectHTML)
							return
						}
					}
				}
			}
		} // End if !isStaticResource

		// Rewrite request path to remove /proxy/<serial> prefix BEFORE setting Director
		originalPath := r.URL.Path
		// Reuse targetPath if already computed, otherwise calculate it
		if targetPath == "/" && len(pathParts) > 1 {
			targetPath = "/" + strings.Join(pathParts[1:], "/")
		}
		// Clean up double slashes
		targetPath = strings.ReplaceAll(targetPath, "//", "/")

		// Proxy prefix for this device's serial, used for URL rewriting and header adjustments
		proxyPrefix := "/proxy/" + serial

		// Capture sessionJar for safe closure access (avoid races)
		capturedJar := sessionJar

		// Attach Basic Auth header or session cookies
		rproxy.Director = func(req *http.Request) {
			// Base director behavior to set URL/Host/Path
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.URL.Path = targetPath
			req.Host = target.Host
			// Force identity encoding so upstream doesn't gzip; simplifies any content rewriting
			// and avoids mismatched Content-Encoding headers when we replace bodies.
			req.Header.Del("Accept-Encoding")

			// Rewrite Referer and Origin headers to the upstream origin so vendor UIs that enforce
			// CSRF/host checks don't reject proxied form posts or XHR requests.
			if ref := req.Header.Get("Referer"); ref != "" {
				if u, err := url.Parse(ref); err == nil {
					if strings.HasPrefix(u.Path, proxyPrefix) {
						upPath := strings.TrimPrefix(u.Path, proxyPrefix)
						if upPath == "" {
							upPath = "/"
						}
						newRef := target.Scheme + "://" + target.Host + upPath
						if u.RawQuery != "" {
							newRef += "?" + u.RawQuery
						}
						if u.Fragment != "" {
							newRef += "#" + u.Fragment
						}
						appLogger.TraceTag("proxy_director", "Rewriting Referer header", "original", ref, "rewritten", newRef)
						req.Header.Set("Referer", newRef)
					}
				}
			}

			if origOrigin := req.Header.Get("Origin"); origOrigin != "" {
				newOrigin := target.Scheme + "://" + target.Host
				appLogger.TraceTag("proxy_director", "Rewriting Origin header", "original", origOrigin, "rewritten", newOrigin)
				req.Header.Set("Origin", newOrigin)
			}

			// Add Authorization for Basic auth
			if cr, err := getCreds(serial); err == nil && cr != nil && cr.AuthType == "basic" && cr.AutoLogin {
				if len(secretKey) == 32 && cr.Password != "" {
					if pw, err := commonutil.DecryptFromB64(secretKey, cr.Password); err == nil {
						userpass := cr.Username + ":" + pw
						req.Header.Set("Authorization", "Basic "+basicAuth(userpass))
					}
				}
			}

			// Attach cookies for form auth
			// Double-check jar is valid to prevent race conditions
			if capturedJar != nil && target != nil {
				// Safely get cookies with nil check
				func() {
					defer func() {
						if r := recover(); r != nil {
							appLogger.Warn("Proxy: panic getting cookies", "error", fmt.Sprintf("%v", r))
						}
					}()
					if cookies := capturedJar.Cookies(target); len(cookies) > 0 {
						appLogger.Debug("Proxy Director: attaching cookies", "path", req.URL.Path, "cookie_count", len(cookies))
						for _, c := range cookies {
							appLogger.Debug("Proxy Director: adding cookie", "name", c.Name, "value_length", len(c.Value))
							req.AddCookie(c)
						}
					} else {
						appLogger.Debug("Proxy Director: no cookies to attach", "path", req.URL.Path, "has_jar", capturedJar != nil)
					}
				}()
			} else {
				appLogger.Debug("Proxy Director: skipping cookies", "has_jar", capturedJar != nil, "has_target", target != nil, "path", req.URL.Path)
			}
		}

		// Configure transport to handle HTTPS with self-signed certs
		rproxy.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // Accept self-signed certs from printers
			},
			MaxIdleConns:          10,
			IdleConnTimeout:       60 * time.Second,
			DisableCompression:    false,
			DisableKeepAlives:     false,
			ResponseHeaderTimeout: 30 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   15 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		}

		appLogger.TraceTag("proxy_request", "Proxy request", "method", r.Method, "path", originalPath, "prefix", proxyPrefix, "target", target.String(), "target_path", targetPath)

		// Modify response to rewrite URLs in content and headers
		rproxy.ModifyResponse = func(resp *http.Response) error {
			// Rewrite Set-Cookie headers to include the proxy path
			// This ensures the browser stores cookies and includes them in iframe requests
			if cookies := resp.Cookies(); len(cookies) > 0 {
				resp.Header.Del("Set-Cookie")
				// Get HTTPS status from context
				isHTTPS := false
				if resp.Request != nil && resp.Request.Context() != nil {
					if v := resp.Request.Context().Value(isHTTPSContextKey); v != nil {
						isHTTPS = v.(bool)
					}
				}
				for _, cookie := range cookies {
					// Rewrite cookie path to be relative to proxy prefix
					if cookie.Path == "" || cookie.Path == "/" {
						cookie.Path = proxyPrefix + "/"
					} else if !strings.HasPrefix(cookie.Path, proxyPrefix) {
						cookie.Path = proxyPrefix + cookie.Path
					}
					// Clear domain since we're proxying to a different host
					cookie.Domain = ""
					// Set Secure flag based on connection type
					// If agent is accessed via HTTPS, keep Secure=true; if HTTP, clear it
					cookie.Secure = isHTTPS
					// Set SameSite to Lax to allow iframe requests
					if cookie.SameSite == 0 {
						cookie.SameSite = http.SameSiteLaxMode
					}
					resp.Header.Add("Set-Cookie", cookie.String())
					appLogger.Debug("Proxy: rewriting Set-Cookie for browser", "name", cookie.Name, "path", cookie.Path, "secure", cookie.Secure)
				}
			}

			// Rewrite Location header for redirects to stay within proxy path
			if loc := resp.Header.Get("Location"); loc != "" {
				if locURL, err := url.Parse(loc); err == nil {
					// Rewrite relative or same-host absolute URLs
					if locURL.Host == "" || locURL.Host == target.Host {
						newPath := locURL.Path
						if newPath == "" {
							newPath = "/"
						}
						newLoc := proxyPrefix + newPath
						if locURL.RawQuery != "" {
							newLoc += "?" + locURL.RawQuery
						}
						if locURL.Fragment != "" {
							newLoc += "#" + locURL.Fragment
						}
						resp.Header.Set("Location", newLoc)
					}
				}
			}

			// Strip headers that prevent iframe embedding
			resp.Header.Del("X-Frame-Options")
			resp.Header.Del("Content-Security-Policy")

			// Rewrite HTML/CSS/JS content to fix relative URLs
			contentType := resp.Header.Get("Content-Type")
			shouldRewrite := strings.Contains(contentType, "text/html") ||
				strings.Contains(contentType, "text/css") ||
				strings.Contains(contentType, "application/javascript") ||
				strings.Contains(contentType, "text/javascript") ||
				strings.Contains(contentType, "application/x-javascript")

			if shouldRewrite {
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					return err
				}
				resp.Body.Close()

				content := string(body)
				appLogger.TraceTag("proxy_body_rewrite", "Rewriting response body", "content_type", contentType, "original_size", len(body), "path", targetPath)
				isHTML := strings.Contains(contentType, "text/html")
				isCSS := strings.Contains(contentType, "text/css")

				// Rewrite common URL patterns in HTML/CSS/JS
				// Fix absolute paths: href="/path" -> href="/proxy/SERIAL/path"
				content = strings.ReplaceAll(content, `href="/"`, `href="`+proxyPrefix+`/"`)
				content = strings.ReplaceAll(content, `href='/'`, `href='`+proxyPrefix+`/'`)
				content = strings.ReplaceAll(content, `src="/"`, `src="`+proxyPrefix+`/"`)
				content = strings.ReplaceAll(content, `src='/'`, `src='`+proxyPrefix+`/'`)
				content = strings.ReplaceAll(content, `action="/"`, `action="`+proxyPrefix+`/"`)
				content = strings.ReplaceAll(content, `action='/'`, `action='`+proxyPrefix+`/'`)

				// Rewrite absolute-path attributes to stay under /proxy/<serial>
				content = strings.ReplaceAll(content, `href="/`, `href="`+proxyPrefix+`/`)
				content = strings.ReplaceAll(content, `href='/`, `href='`+proxyPrefix+`/`)
				content = strings.ReplaceAll(content, `src="/`, `src="`+proxyPrefix+`/`)
				content = strings.ReplaceAll(content, `src='/`, `src='`+proxyPrefix+`/`)
				content = strings.ReplaceAll(content, `action="/`, `action="`+proxyPrefix+`/`)
				content = strings.ReplaceAll(content, `action='/`, `action='`+proxyPrefix+`/`)
				content = strings.ReplaceAll(content, `data-src="/`, `data-src="`+proxyPrefix+`/`)
				content = strings.ReplaceAll(content, `data-src='/`, `data-src='`+proxyPrefix+`/`)
				content = strings.ReplaceAll(content, `data-href="/`, `data-href="`+proxyPrefix+`/`)
				content = strings.ReplaceAll(content, `data-href='/`, `data-href='`+proxyPrefix+`/`)

				// Fix CSS url() references: url(/path) and url("/path") and url('/path')
				if isCSS || isHTML {
					// CSS url() references
					content = strings.ReplaceAll(content, `url(/`, `url(`+proxyPrefix+`/`)
					content = strings.ReplaceAll(content, `url("/`, `url("`+proxyPrefix+`/`)
					content = strings.ReplaceAll(content, `url('/`, `url('`+proxyPrefix+`/`)
				}

				// Fix JavaScript location redirects
				content = strings.ReplaceAll(content, `location.href="/"`, `location.href="`+proxyPrefix+`/"`)
				content = strings.ReplaceAll(content, `location.href='/'`, `location.href='`+proxyPrefix+`/'`)
				content = strings.ReplaceAll(content, `window.location="/"`, `window.location="`+proxyPrefix+`/"`)
				content = strings.ReplaceAll(content, `window.location='/'`, `window.location='`+proxyPrefix+`/'`)

				// Add base tag to HTML to help resolve relative URLs
				// IMPORTANT: base must reflect the directory of the UPSTREAM request path
				// (not our incoming /proxy/<serial>/... path) to avoid duplicating the proxy prefix.
				if isHTML && !strings.Contains(strings.ToLower(content), "<base") {
					// Use the upstream request path from the reverse proxy response
					upstreamPath := "/"
					if resp != nil && resp.Request != nil && resp.Request.URL != nil {
						upstreamPath = resp.Request.URL.Path
					}
					dir := path.Dir(upstreamPath)
					if !strings.HasSuffix(dir, "/") {
						dir += "/"
					}
					baseHref := proxyPrefix + dir
					baseTag := "<base href=\"" + baseHref + "\">"
					contentLower := strings.ToLower(content)
					if idx := strings.Index(contentLower, "<head>"); idx != -1 {
						content = content[:idx+6] + baseTag + content[idx+6:]
					} else if idx := strings.Index(contentLower, "<head "); idx != -1 {
						// Find end of <head ...> tag
						if endIdx := strings.Index(content[idx:], ">"); endIdx != -1 {
							insertPos := idx + endIdx + 1
							content = content[:insertPos] + baseTag + content[insertPos:]
						}
					}
				}

				newBody := []byte(content)

				// Detect if we got a login page despite having cached session
				// This means the session was invalidated (user logged out)
				if isHTML && capturedJar != nil {
					contentLower := strings.ToLower(content)
					// Check for common login page indicators
					hasLoginForm := strings.Contains(contentLower, "type=\"password\"") ||
						strings.Contains(contentLower, "type='password'")
					hasLoginKeywords := strings.Contains(contentLower, "login") ||
						strings.Contains(contentLower, "password") ||
						strings.Contains(contentLower, "username") ||
						strings.Contains(contentLower, "sign in")

					// If this looks like a login page, clear the cached session
					if hasLoginForm && hasLoginKeywords {
						appLogger.Info("Proxy: detected login page - clearing cached session (likely logged out)", "serial", serial)
						proxySessionCache.Clear(serial)
					}
				}

				resp.Body = io.NopCloser(bytes.NewReader(newBody))
				// We've rewritten the body; ensure Content-Encoding is cleared and length matches
				resp.Header.Del("Content-Encoding")
				resp.ContentLength = int64(len(newBody))
				resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(newBody)))
			}

			// Cache static resources for performance (printers are very slow)
			if isStaticResource && resp.StatusCode == http.StatusOK {
				// Read the body to cache it
				body, err := io.ReadAll(resp.Body)
				if err == nil {
					resp.Body.Close()
					// Cache for 15 minutes
					cacheKey := serial + ":" + targetPath
					staticCache.Set(cacheKey, body, resp.Header.Get("Content-Type"), resp.Header.Clone(), 15*time.Minute)
					appLogger.Debug("Proxy: cached static resource", "serial", serial, "path", targetPath, "size", len(body))
					// Restore the body for the response
					resp.Body = io.NopCloser(bytes.NewReader(body))
					resp.ContentLength = int64(len(body))
				}
			}

			return nil
		}

		// Add error handler for proxy failures
		rproxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			appLogger.WarnRateLimited("proxy_error_"+serial, 1*time.Minute, "Proxy error", "serial", serial, "error", err.Error())
			if err == context.DeadlineExceeded || r.Context().Err() == context.DeadlineExceeded {
				http.Error(w, "Printer did not respond within 45 seconds. The device may be busy, turned off, or its web interface may be disabled.", http.StatusGatewayTimeout)
			} else {
				http.Error(w, fmt.Sprintf("Proxy connection failed: %v", err), http.StatusBadGateway)
			}
		}

		// Serve the proxied request with response diagnostics
		lrw := &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()

		// Ensure we always log completion/timeout via defer
		defer func() {
			dur := time.Since(start)
			if dur > 30*time.Second {
				appLogger.Warn("Proxy slow/timeout", "serial", serial, "status", lrw.status, "bytes", lrw.bytes, "duration_ms", dur.Milliseconds(), "path", originalPath)
			} else if lrw.status >= 500 {
				appLogger.WarnRateLimited("proxy_upstream_"+serial, 1*time.Minute, "Proxy upstream error", "serial", serial, "status", lrw.status, "bytes", lrw.bytes, "duration_ms", dur.Milliseconds(), "path", originalPath)
			} else {
				appLogger.TraceTag("proxy_response", "Proxy completed", "serial", serial, "status", lrw.status, "bytes", lrw.bytes, "duration_ms", dur.Milliseconds(), "path", originalPath)
			}
		}()

		rproxy.ServeHTTP(lrw, r)
	})

	// List merged device profiles (using storage interface)
	http.HandleFunc("/devices/list", func(w http.ResponseWriter, r *http.Request) {
		// List only saved devices (is_saved=true)
		saved := true
		devices, err := deviceStore.List(context.Background(), storage.DeviceFilter{IsSaved: &saved})
		if err != nil {
			http.Error(w, "failed to list devices: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Format for compatibility with existing frontend
		out := []map[string]interface{}{}
		for _, device := range devices {
			// Convert to PrinterInfo for compatibility
			pi := storage.DeviceToPrinterInfo(device)
			out = append(out, map[string]interface{}{
				"serial":       device.Serial,
				"path":         device.Serial + ".json", // For compatibility
				"printer_info": pi,
				"info":         pi, // Alias for compatibility
				"asset_number": device.AssetNumber,
				"location":     device.Location,
				"web_ui_url":   device.WebUIURL,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	// Get a merged device profile by serial. /devices/get?serial=SERIAL
	http.HandleFunc("/devices/get", func(w http.ResponseWriter, r *http.Request) {
		serial := r.URL.Query().Get("serial")
		if serial == "" {
			http.Error(w, "serial required", http.StatusBadRequest)
			return
		}

		// Try database first
		ctx := context.Background()
		device, err := deviceStore.Get(ctx, serial)
		if err == nil {
			// Convert fields directly for response

			// Fetch latest metrics for this device
			var pageCount int
			var tonerLevels map[string]interface{}
			if snapshot, err := deviceStore.GetLatestMetrics(ctx, device.Serial); err == nil && snapshot != nil {
				pageCount = snapshot.PageCount
				tonerLevels = snapshot.TonerLevels
			}

			// Create response with all device fields + printer_info for compatibility
			response := map[string]interface{}{
				"serial":          device.Serial,
				"ip":              device.IP,
				"manufacturer":    device.Manufacturer,
				"model":           device.Model,
				"hostname":        device.Hostname,
				"firmware":        device.Firmware,
				"mac_address":     device.MACAddress,
				"subnet_mask":     device.SubnetMask,
				"gateway":         device.Gateway,
				"dns_servers":     device.DNSServers,
				"dhcp_server":     device.DHCPServer,
				"page_count":      pageCount,
				"toner_levels":    tonerLevels,
				"consumables":     device.Consumables,
				"status_messages": device.StatusMessages,
				"asset_number":    device.AssetNumber,
				"location":        device.Location,
				"web_ui_url":      device.WebUIURL,
				"last_seen":       device.LastSeen,
				"created_at":      device.CreatedAt,
				"first_seen":      device.FirstSeen,
				"is_saved":        device.IsSaved,

				// Include RawData if present for extended fields
				"raw_data": device.RawData,
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}
		// Not found in database
		http.Error(w, "not found", http.StatusNotFound)
	})

	// Save a device by marking it as saved. POST { serial: "SERIAL" }
	http.HandleFunc("/devices/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Serial string `json:"serial"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if req.Serial == "" {
			http.Error(w, "serial required", http.StatusBadRequest)
			return
		}

		ctx := context.Background()
		if err := deviceStore.MarkSaved(ctx, req.Serial); err != nil {
			http.Error(w, "failed to save device: "+err.Error(), http.StatusInternalServerError)
			return
		}

		appLogger.Info("Device marked as saved", "serial", req.Serial)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "saved",
			"serial": req.Serial,
		})
	})

	// Save all discovered devices (marks all visible unsaved devices as saved)
	http.HandleFunc("/devices/save/all", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}

		ctx := context.Background()
		count, err := deviceStore.MarkAllSaved(ctx)
		if err != nil {
			http.Error(w, "failed to save all devices: "+err.Error(), http.StatusInternalServerError)
			return
		}

		appLogger.Info("Marked devices as saved", "count", count)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "saved",
			"count":  count,
		})
	})

	// Delete a device profile by serial. POST { serial: "SERIAL" }
	http.HandleFunc("/devices/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Serial string `json:"serial"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if req.Serial == "" {
			http.Error(w, "serial required", http.StatusBadRequest)
			return
		}

		// Try database first
		ctx := context.Background()

		err := deviceStore.Delete(ctx, req.Serial)
		if err == nil {
			appLogger.Info("Deleted device from database", "serial", req.Serial)

			// Note: Device will naturally be re-discovered during next scan if still on network
			// No need to immediately re-scan as this defeats the purpose of deletion

			w.WriteHeader(http.StatusOK)
			return
		}
		if err != storage.ErrNotFound {
			appLogger.Error("Database delete error", "error", err.Error())
			// Continue to file delete as fallback
		}

		// Fallback: delete JSON file
		p := filepath.Join(".", "logs", "devices", req.Serial+".json")
		if err := os.Remove(p); err != nil {
			if os.IsNotExist(err) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			http.Error(w, "delete failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	// Version endpoint
	http.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"version":    Version,
			"build_time": BuildTime,
			"git_commit": GitCommit,
			"build_type": BuildType,
			"go_version": runtime.Version(),
			"os":         runtime.GOOS,
			"arch":       runtime.GOARCH,
		})
	})

	// Metrics history endpoints
	http.HandleFunc("/api/devices/metrics/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}

		serial := r.URL.Query().Get("serial")
		if serial == "" {
			http.Error(w, "serial parameter required", http.StatusBadRequest)
			return
		}

		ctx := context.Background()
		snapshot, err := deviceStore.GetLatestMetrics(ctx, serial)
		if err != nil {
			if err == storage.ErrNotFound {
				http.Error(w, "no metrics found", http.StatusNotFound)
			} else {
				http.Error(w, "failed to get metrics: "+err.Error(), http.StatusInternalServerError)
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(snapshot)
	})

	http.HandleFunc("/api/devices/metrics/history", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}

		serial := r.URL.Query().Get("serial")
		if serial == "" {
			http.Error(w, "serial parameter required", http.StatusBadRequest)
			return
		}

		// Support both period-based and custom date range queries
		var since, until time.Time
		now := time.Now()

		// Check for custom date range first
		sinceStr := r.URL.Query().Get("since")
		untilStr := r.URL.Query().Get("until")

		if sinceStr != "" && untilStr != "" {
			// Custom date range
			var err error
			since, err = time.Parse(time.RFC3339, sinceStr)
			if err != nil {
				http.Error(w, "invalid since parameter (use RFC3339 format)", http.StatusBadRequest)
				return
			}
			until, err = time.Parse(time.RFC3339, untilStr)
			if err != nil {
				http.Error(w, "invalid until parameter (use RFC3339 format)", http.StatusBadRequest)
				return
			}
		} else {
			// Period-based range
			period := r.URL.Query().Get("period")
			if period == "" {
				period = "week" // default
			}

			until = now
			switch period {
			case "day":
				since = now.Add(-24 * time.Hour)
			case "week":
				since = now.Add(-7 * 24 * time.Hour)
			case "month":
				since = now.Add(-30 * 24 * time.Hour)
			case "year":
				since = now.Add(-365 * 24 * time.Hour)
			default:
				since = now.Add(-7 * 24 * time.Hour) // default to week
			}
		}

		ctx := context.Background()
		// Use tiered metrics retrieval so the store returns the best-resolution
		// data for the requested time range (raw/hourly/daily/monthly).
		snapshots, err := deviceStore.GetTieredMetricsHistory(ctx, serial, since, until)
		if err != nil {
			// Log the error server-side to aid debugging (will appear in agent logs)
			agent.Error(fmt.Sprintf("Failed to get metrics history: serial=%s error=%v", serial, err))
			http.Error(w, "failed to get metrics history: "+err.Error(), http.StatusInternalServerError)
			return
		}

		if agent.DebugEnabled {
			agent.Debug(fmt.Sprintf("GET /api/devices/metrics/history - serial=%s, since=%s, until=%s, found=%d snapshots",
				serial, since.Format(time.RFC3339), until.Format(time.RFC3339), len(snapshots)))
			if len(snapshots) > 0 {
				first := snapshots[0]
				last := snapshots[len(snapshots)-1]
				agent.Debug(fmt.Sprintf("  First: timestamp=%s, page_count=%d", first.Timestamp.Format(time.RFC3339), first.PageCount))
				agent.Debug(fmt.Sprintf("  Last: timestamp=%s, page_count=%d", last.Timestamp.Format(time.RFC3339), last.PageCount))
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(snapshots)
	})

	// POST /api/devices/metrics/delete - delete a single metrics row by id (tier optional)
	http.HandleFunc("/api/devices/metrics/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			ID   int64  `json:"id"`
			Tier string `json:"tier,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}

		if req.ID == 0 {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}

		ctx := context.Background()
		if deviceStore == nil {
			http.Error(w, "storage unavailable", http.StatusInternalServerError)
			return
		}

		if err := deviceStore.DeleteMetricByID(ctx, req.Tier, req.ID); err != nil {
			agent.Error(fmt.Sprintf("Failed to delete metrics row: id=%d tier=%s error=%v", req.ID, req.Tier, err))
			http.Error(w, "failed to delete metrics row: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})

	// POST /devices/metrics/collect - Manually collect metrics for a device
	http.HandleFunc("/devices/metrics/collect", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Serial string `json:"serial"`
			IP     string `json:"ip"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}

		if req.Serial == "" {
			http.Error(w, "serial required", http.StatusBadRequest)
			return
		}

		if req.IP == "" {
			// Try to get IP from database
			ctx := context.Background()
			device, err := deviceStore.Get(ctx, req.Serial)
			if err == nil {
				req.IP = device.IP
			}
			if req.IP == "" {
				http.Error(w, "ip required", http.StatusBadRequest)
				return
			}
		}

		// Collect metrics snapshot using new scanner
		metricsCtx := context.Background()
		vendorHint := ""
		// Get vendor hint from database if possible
		if deviceStore != nil {
			device, getErr := deviceStore.Get(metricsCtx, req.Serial)
			if getErr == nil && device != nil {
				vendorHint = device.Manufacturer
			}
		}

		// Use new scanner for metrics collection
		appLogger.Info("Collecting metrics", "serial", req.Serial, "ip", req.IP, "vendor_hint", vendorHint)
		agentSnapshot, err := CollectMetrics(metricsCtx, req.IP, req.Serial, vendorHint, 10)
		if err != nil {
			appLogger.Warn("Metrics collection failed", "serial", req.Serial, "ip", req.IP, "error", err.Error())
			if agent.DebugEnabled {
				agent.Debug(fmt.Sprintf("POST /devices/metrics/collect - FAILED for %s (%s): %s", req.Serial, req.IP, err.Error()))
			}
			http.Error(w, "failed to collect metrics: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Convert to storage type
		storageSnapshot := &storage.MetricsSnapshot{}
		storageSnapshot.Serial = agentSnapshot.Serial
		storageSnapshot.PageCount = agentSnapshot.PageCount
		storageSnapshot.ColorPages = agentSnapshot.ColorPages
		storageSnapshot.MonoPages = agentSnapshot.MonoPages
		storageSnapshot.ScanCount = agentSnapshot.ScanCount
		storageSnapshot.TonerLevels = agentSnapshot.TonerLevels
		storageSnapshot.FaxPages = agentSnapshot.FaxPages
		storageSnapshot.CopyPages = agentSnapshot.CopyPages
		storageSnapshot.OtherPages = agentSnapshot.OtherPages
		storageSnapshot.CopyMonoPages = agentSnapshot.CopyMonoPages
		storageSnapshot.CopyFlatbedScans = agentSnapshot.CopyFlatbedScans
		storageSnapshot.CopyADFScans = agentSnapshot.CopyADFScans
		storageSnapshot.FaxFlatbedScans = agentSnapshot.FaxFlatbedScans
		storageSnapshot.FaxADFScans = agentSnapshot.FaxADFScans
		storageSnapshot.ScanToHostFlatbed = agentSnapshot.ScanToHostFlatbed
		storageSnapshot.ScanToHostADF = agentSnapshot.ScanToHostADF
		storageSnapshot.DuplexSheets = agentSnapshot.DuplexSheets
		storageSnapshot.JamEvents = agentSnapshot.JamEvents
		storageSnapshot.ScannerJamEvents = agentSnapshot.ScannerJamEvents

		// Save to database
		ctx := context.Background()
		if err := deviceStore.SaveMetricsSnapshot(ctx, storageSnapshot); err != nil {
			http.Error(w, "failed to save metrics: "+err.Error(), http.StatusInternalServerError)
			return
		}

		appLogger.Info("Metrics collected successfully",
			"serial", req.Serial,
			"ip", req.IP,
			"page_count", agentSnapshot.PageCount,
			"color_pages", agentSnapshot.ColorPages,
			"mono_pages", agentSnapshot.MonoPages,
			"scan_count", agentSnapshot.ScanCount,
			"fax_pages", agentSnapshot.FaxPages,
			"copy_pages", agentSnapshot.CopyPages,
			"duplex_sheets", agentSnapshot.DuplexSheets,
			"jam_events", agentSnapshot.JamEvents)

		if agent.DebugEnabled {
			agent.Debug(fmt.Sprintf("POST /devices/metrics/collect - SUCCESS for %s (%s): PageCount=%d, ColorPages=%d, MonoPages=%d, ScanCount=%d",
				req.Serial, req.IP, agentSnapshot.PageCount, agentSnapshot.ColorPages, agentSnapshot.MonoPages, agentSnapshot.ScanCount))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":      "ok",
			"serial":      req.Serial,
			"page_count":  agentSnapshot.PageCount,
			"color_pages": agentSnapshot.ColorPages,
			"mono_pages":  agentSnapshot.MonoPages,
			"scan_count":  agentSnapshot.ScanCount,
		})
	})

	// vendor add handler moved to mib_suggestions_api.go to centralize candidate APIs

	// Trace tags endpoint for granular trace logging control
	http.HandleFunc("/dev_settings/trace_tags", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			tags := appLogger.GetTraceTags()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"tags": tags})
			return
		}

		if r.Method == http.MethodPost {
			var req struct {
				Tags map[string]bool `json:"tags"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}

			appLogger.SetTraceTags(req.Tags)

			// Persist to config store for restarts
			if agentConfigStore != nil {
				_ = agentConfigStore.SetConfigValue("trace_tags", req.Tags)
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			return
		}

		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})

	// Unified settings endpoint to get/save all settings at once
	http.HandleFunc("/settings", func(w http.ResponseWriter, r *http.Request) {
		if agentConfigStore == nil {
			http.Error(w, "config store unavailable", http.StatusInternalServerError)
			return
		}

		if r.Method == http.MethodGet {
			// Compose discovery
			discovery := map[string]interface{}{
				// IP Sources: what to scan
				"subnet_scan":   true,  // Scan local subnet by default
				"manual_ranges": false, // Advanced feature, off by default

				// Active Probes: how to find devices
				"arp_enabled":  true,  // Free, instant
				"icmp_enabled": true,  // Reliable liveness
				"tcp_enabled":  true,  // Works when ICMP blocked
				"mdns_enabled": false, // Optional, covered by live mDNS

				// Device Identification: what info to collect
				"snmp_enabled": true, // Essential for printer info

				// Passive Discovery: continuous listening (when Auto Discover enabled)
				"auto_discover_enabled":       false, // Opt-in behavior
				"auto_discover_live_mdns":     true,  // Recommended: macOS/Linux + modern printers
				"auto_discover_live_wsd":      true,  // Recommended: Windows native + HP/Canon/Epson
				"auto_discover_live_ssdp":     false, // Optional: broad but lower printer value
				"auto_discover_live_snmptrap": false, // Advanced: requires privileges
				"auto_discover_live_llmnr":    false, // Optional: limited printer support

				// Metrics Monitoring: historical tracking
				"metrics_rescan_enabled":          false, // Opt-in monitoring
				"metrics_rescan_interval_minutes": 60,    // Hourly when enabled
			}
			{
				var stored map[string]interface{}
				_ = agentConfigStore.GetConfigValue("discovery_settings", &stored)
				for k, v := range stored {
					discovery[k] = v
				}
			}

			// Compose developer
			developer := map[string]interface{}{
				"asset_id_regex":       "",
				"snmp_community":       "",
				"log_level":            "info",
				"dump_parse_debug":     false,
				"snmp_timeout_ms":      2000,
				"snmp_retries":         1,
				"discover_concurrency": 50,
			}
			{
				var stored map[string]interface{}
				_ = agentConfigStore.GetConfigValue("dev_settings", &stored)
				for k, v := range stored {
					developer[k] = v
				}
			}

			// Include saved IP ranges text in discovery for unified settings UI
			if agentConfigStore != nil {
				if txt, err := agentConfigStore.GetRanges(); err == nil {
					discovery["ranges_text"] = txt
				}
			}

			// Include detected subnet for display
			if ipnets, err := agent.GetLocalSubnets(); err == nil && len(ipnets) > 0 {
				discovery["detected_subnet"] = ipnets[0].String()
			} else {
				discovery["detected_subnet"] = ""
			}

			// Compose security settings
			security := map[string]interface{}{
				"credentials_enabled": true, // Default: enabled
			}
			{
				var stored map[string]interface{}
				_ = agentConfigStore.GetConfigValue("security_settings", &stored)
				for k, v := range stored {
					security[k] = v
				}
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"discovery": discovery,
				"developer": developer,
				"security":  security,
			})
			return
		}

		if r.Method == http.MethodPost {
			var req struct {
				Discovery map[string]interface{} `json:"discovery"`
				Developer map[string]interface{} `json:"developer"`
				Security  map[string]interface{} `json:"security"`
				Reset     bool                   `json:"reset"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}

			if req.Reset {
				// Reset both to defaults
				_ = agentConfigStore.SetConfigValue("discovery_settings", map[string]interface{}{})
				_ = agentConfigStore.SetConfigValue("dev_settings", map[string]interface{}{})
				_ = agentConfigStore.SetConfigValue("security_settings", map[string]interface{}{})
				// Stop background features
				stopAutoDiscover()
				stopLiveMDNS()
				stopLiveWSDiscovery()
				stopLiveSSDP()
				stopSNMPTrap()
				stopLLMNR()
				stopMetricsRescan()
				agent.SetDebugEnabled(false)
				agent.SetDumpParseDebug(false)
				w.WriteHeader(http.StatusOK)
				return
			}

			// Save discovery settings and apply effects (including ranges_text validation/save)
			if req.Discovery != nil {
				// Handle optional ranges_text field: validate and persist via AgentConfigStore
				if val, ok := req.Discovery["ranges_text"]; ok {
					var txt string
					if s, ok2 := val.(string); ok2 {
						txt = s
					}
					// Validate ranges using existing parser (same as /save_ranges)
					maxAddrs := 4096
					res, err := agent.ParseRangeText(txt, maxAddrs)
					if err != nil {
						http.Error(w, "validation error: "+err.Error(), http.StatusBadRequest)
						return
					}
					if len(res.Errors) > 0 {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusBadRequest)
						_ = json.NewEncoder(w).Encode(res)
						return
					}
					// Save ranges text as source of truth
					if agentConfigStore != nil {
						if err := agentConfigStore.SetRanges(txt); err != nil {
							http.Error(w, "failed to save ranges: "+err.Error(), http.StatusInternalServerError)
							return
						}
					}
					// Do not persist ranges_text inside discovery_settings JSON to avoid duplication
					delete(req.Discovery, "ranges_text")
				}

				if err := agentConfigStore.SetConfigValue("discovery_settings", req.Discovery); err != nil {
					http.Error(w, "failed to save discovery settings: "+err.Error(), http.StatusInternalServerError)
					return
				}
				// Apply side-effects using updated discovery settings
				applyDiscoveryEffects(req.Discovery)
			}

			// Save developer settings
			if req.Developer != nil {
				if err := agentConfigStore.SetConfigValue("dev_settings", req.Developer); err != nil {
					http.Error(w, "failed to save developer settings: "+err.Error(), http.StatusInternalServerError)
					return
				}
			}

			// Save security settings
			if req.Security != nil {
				if err := agentConfigStore.SetConfigValue("security_settings", req.Security); err != nil {
					http.Error(w, "failed to save security settings: "+err.Error(), http.StatusInternalServerError)
					return
				}
			}

			w.WriteHeader(http.StatusOK)
			return
		}

		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})

	// Legacy subnet scan endpoint (deprecated, use /settings/discovery)
	http.HandleFunc("/settings/subnet_scan", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.Header().Set("Content-Type", "application/json")
			enabled := true // default to true
			if agentConfigStore != nil {
				var setting struct {
					Enabled bool `json:"enabled"`
				}
				setting.Enabled = true // default
				_ = agentConfigStore.GetConfigValue("subnet_scan_enabled", &setting)
				enabled = setting.Enabled
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"enabled": enabled})
			return
		}
		if r.Method == "POST" {
			var req struct {
				Enabled bool `json:"enabled"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			if agentConfigStore != nil {
				if err := agentConfigStore.SetConfigValue("subnet_scan_enabled", map[string]bool{"enabled": req.Enabled}); err != nil {
					http.Error(w, "failed to save setting: "+err.Error(), http.StatusInternalServerError)
					return
				}
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})

	// API endpoint to regenerate TLS certificates
	http.HandleFunc("/api/regenerate-certs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Get data directory
		dataDir, err := storage.GetDataDir("PrintMaster")
		if err != nil {
			http.Error(w, "failed to get data directory: "+err.Error(), http.StatusInternalServerError)
			return
		}

		certFile := filepath.Join(dataDir, "server.crt")
		keyFile := filepath.Join(dataDir, "server.key")

		// Delete existing certificates
		os.Remove(certFile)
		os.Remove(keyFile)

		// Generate new certificates
		newCertFile, newKeyFile, err := ensureTLSCertificates("", "")
		if err != nil {
			http.Error(w, "failed to generate certificates: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Certificates regenerated successfully. Restart agent to use new certificates.",
			"cert":    newCertFile,
			"key":     newKeyFile,
		})
	})

	// Get HTTP/HTTPS settings
	enableHTTP := true
	enableHTTPS := true
	httpPort := "8080"
	httpsPort := "8443"
	redirectHTTPToHTTPS := false
	customCertPath := ""
	customKeyPath := ""

	// Try to load settings from config
	if agentConfigStore != nil {
		var securitySettings map[string]interface{}
		if err := agentConfigStore.GetConfigValue("security_settings", &securitySettings); err == nil {
			if val, ok := securitySettings["enable_http"].(bool); ok {
				enableHTTP = val
			}
			if val, ok := securitySettings["enable_https"].(bool); ok {
				enableHTTPS = val
			}
			if val, ok := securitySettings["http_port"].(string); ok && val != "" {
				httpPort = val
			}
			if val, ok := securitySettings["https_port"].(string); ok && val != "" {
				httpsPort = val
			}
			if val, ok := securitySettings["redirect_http_to_https"].(bool); ok {
				redirectHTTPToHTTPS = val
			}
			if val, ok := securitySettings["custom_cert_path"].(string); ok {
				customCertPath = val
			}
			if val, ok := securitySettings["custom_key_path"].(string); ok {
				customKeyPath = val
			}
		}
	}

	// Load or generate TLS certificates for HTTPS
	certFile, keyFile, err := ensureTLSCertificates(customCertPath, customKeyPath)
	if err != nil {
		appLogger.Error("Failed to setup TLS certificates", "error", err.Error())
		certFile = ""
		keyFile = ""
	}

	// Default to HTTPS if certificates are available
	if certFile == "" || keyFile == "" {
		enableHTTPS = false
		appLogger.Warn("HTTPS disabled: TLS certificates not available")
	}

	// Ensure at least one server is enabled
	if !enableHTTP && !enableHTTPS {
		enableHTTP = true
		appLogger.Warn("Both HTTP and HTTPS disabled in settings, enabling HTTP as fallback")
	}

	// Create server instances for graceful shutdown
	var httpServer *http.Server
	var httpsServer *http.Server
	var wg sync.WaitGroup

	// Start HTTP server
	if enableHTTP {
		// Create HTTP server with optional redirect to HTTPS
		var httpHandler http.Handler
		if redirectHTTPToHTTPS && enableHTTPS {
			// Redirect handler using 302 (temporary redirect)
			httpHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Build HTTPS URL
				host := r.Host
				// Replace port if it's the HTTP port
				if strings.Contains(host, ":"+httpPort) {
					host = strings.Replace(host, ":"+httpPort, ":"+httpsPort, 1)
				} else if !strings.Contains(host, ":") {
					// No port specified, add HTTPS port
					host = host + ":" + httpsPort
				}

				httpsURL := "https://" + host + r.RequestURI
				// Use 302 (Found) for temporary redirect, not 301 (permanent)
				http.Redirect(w, r, httpsURL, http.StatusFound)
			})
			appLogger.Info("HTTP server will redirect to HTTPS", "httpPort", httpPort, "httpsPort", httpsPort)
		} else {
			// Use default handler (http.DefaultServeMux with all registered routes)
			httpHandler = nil
		}

		httpServer = &http.Server{
			Addr:              ":" + httpPort,
			Handler:           httpHandler,
			ReadTimeout:       30 * time.Second,
			ReadHeaderTimeout: 10 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       120 * time.Second,
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			appLogger.Info("Starting HTTP server", "port", httpPort)
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				appLogger.Error("HTTP server failed", "error", err.Error())
			}
		}()
	}

	// Start HTTPS server
	if enableHTTPS && certFile != "" && keyFile != "" {
		httpsServer = &http.Server{
			Addr:              ":" + httpsPort,
			ReadTimeout:       30 * time.Second,
			ReadHeaderTimeout: 10 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       120 * time.Second,
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			appLogger.Info("Starting HTTPS server", "port", httpsPort)
			if err := httpsServer.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
				appLogger.Error("HTTPS server failed", "error", err.Error())
			}
		}()
	}

	// Wait for shutdown signal
	<-ctx.Done()
	appLogger.Info("Shutdown signal received, stopping servers...")

	// Stop background services first (quick operations)
	if uploadWorker != nil {
		uploadWorker.Stop()
	}
	if sseHub != nil {
		sseHub.Stop()
	}

	// Graceful shutdown with 20 second timeout (well before service 30s timeout)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer shutdownCancel()

	if httpServer != nil {
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			appLogger.Error("HTTP server shutdown error", "error", err.Error())
		} else {
			appLogger.Info("HTTP server stopped gracefully")
		}
	}

	if httpsServer != nil {
		if err := httpsServer.Shutdown(shutdownCtx); err != nil {
			appLogger.Error("HTTPS server shutdown error", "error", err.Error())
		} else {
			appLogger.Info("HTTPS server stopped gracefully")
		}
	}

	// Wait for servers to finish
	wg.Wait()
	appLogger.Info("All servers stopped")
}
