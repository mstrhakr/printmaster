// PrintMaster Server - Central management hub for PrintMaster agents
// Aggregates data from multiple agents, provides reporting, alerting, and web UI
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"printmaster/server/logger"
	"printmaster/server/storage"
	"runtime"
	"strings"
	"time"
)

// Version information (set at build time via -ldflags)
var (
	Version         = "dev"     // Semantic version (e.g., "0.1.0")
	BuildTime       = "unknown" // Build timestamp
	GitCommit       = "unknown" // Git commit hash
	BuildType       = "dev"     // "dev" or "release"
	ProtocolVersion = "1"       // Agent-Server protocol version
)

var (
	serverLogger *logger.Logger
	serverStore  storage.Store
)

func main() {
	// Command line flags
	port := flag.Int("port", 9090, "HTTP port for server API")
	httpsPort := flag.Int("https-port", 9443, "HTTPS port for server UI")
	dbPath := flag.String("db", "", "Database path (default: platform-specific)")
	logLevel := flag.String("log-level", "info", "Log level (error, warn, info, debug, trace)")
	flag.Parse()

	log.Printf("PrintMaster Server %s (protocol v%s)", Version, ProtocolVersion)
	log.Printf("Build: %s, Commit: %s, Type: %s", BuildTime, GitCommit, BuildType)
	log.Printf("Go: %s, OS: %s, Arch: %s", runtime.Version(), runtime.GOOS, runtime.GOARCH)

	// Initialize logger
	logDir := filepath.Join(filepath.Dir(storage.GetDefaultDBPath()), "logs")
	serverLogger = logger.New(logger.ParseLevel(*logLevel), logDir, 1000)
	serverLogger.Info("Server starting", "version", Version, "protocol", ProtocolVersion)

	// Initialize database
	if *dbPath == "" {
		*dbPath = storage.GetDefaultDBPath()
	}
	log.Printf("Database: %s", *dbPath)
	serverLogger.Info("Initializing database", "path", *dbPath)

	var err error
	serverStore, err = storage.NewSQLiteStore(*dbPath)
	if err != nil {
		serverLogger.Error("Failed to initialize database", "error", err)
		log.Fatal(err)
	}
	defer serverStore.Close()

	serverLogger.Info("Database initialized successfully")

	// Setup HTTP routes
	setupRoutes()

	// Start HTTP server
	httpAddr := fmt.Sprintf(":%d", *port)
	httpsAddr := fmt.Sprintf(":%d", *httpsPort)

	log.Printf("Starting HTTP server on %s", httpAddr)
	log.Printf("Starting HTTPS server on %s (TODO)", httpsAddr)
	log.Printf("Server ready to accept agent connections")

	if err := http.ListenAndServe(httpAddr, nil); err != nil {
		log.Fatal(err)
	}
}

// generateToken creates a secure random token for agent authentication
func generateToken() (string, error) {
	b := make([]byte, 32) // 256 bits of entropy
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// requireAuth is middleware that validates Bearer token authentication
func requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract Bearer token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			http.Error(w, "Invalid Authorization header format", http.StatusUnauthorized)
			return
		}

		token := parts[1]

		// Validate token against database
		ctx := context.Background()
		agent, err := serverStore.GetAgentByToken(ctx, token)
		if err != nil {
			if serverLogger != nil {
				serverLogger.Warn("Invalid authentication attempt", "token", token[:8]+"...", "error", err)
			}
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		// Store agent info in request context for handlers to use
		ctx = context.WithValue(r.Context(), "agent", agent)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// logAuditEntry is a helper to log agent operations to the audit log
func logAuditEntry(ctx context.Context, agentID, action, details, ipAddress string) {
	entry := &storage.AuditEntry{
		Timestamp: time.Now(),
		AgentID:   agentID,
		Action:    action,
		Details:   details,
		IPAddress: ipAddress,
	}

	if err := serverStore.SaveAuditEntry(ctx, entry); err != nil {
		if serverLogger != nil {
			serverLogger.Error("Failed to save audit entry", "agent_id", agentID, "action", action, "error", err)
		}
	}
}

// extractClientIP gets the client IP address from the request
func extractClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (if behind proxy)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// Fall back to RemoteAddr
	ip := r.RemoteAddr
	// Strip port if present
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

func setupRoutes() {
	// Health check (no auth required)
	http.HandleFunc("/health", handleHealth)

	// Version info (no auth required)
	http.HandleFunc("/api/version", handleVersion)

	// Agent API (v1)
	http.HandleFunc("/api/v1/agents/register", handleAgentRegister) // No auth - this generates token
	http.HandleFunc("/api/v1/agents/heartbeat", requireAuth(handleAgentHeartbeat))
	http.HandleFunc("/api/v1/devices/batch", requireAuth(handleDevicesBatch))
	http.HandleFunc("/api/v1/metrics/batch", requireAuth(handleMetricsBatch))

	// Web UI endpoints (future)
	http.HandleFunc("/", handleWebUI)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
	})
}

func handleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"version":          Version,
		"build_time":       BuildTime,
		"git_commit":       GitCommit,
		"build_type":       BuildType,
		"protocol_version": ProtocolVersion,
		"go_version":       runtime.Version(),
		"os":               runtime.GOOS,
		"arch":             runtime.GOARCH,
	})
}

// Agent registration - first contact from a new agent
func handleAgentRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		AgentID         string `json:"agent_id"`
		AgentVersion    string `json:"agent_version"`
		ProtocolVersion string `json:"protocol_version"`
		Hostname        string `json:"hostname"`
		IP              string `json:"ip"`
		Platform        string `json:"platform"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if serverLogger != nil {
			serverLogger.Warn("Invalid JSON in agent register", "error", err)
		}
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if serverLogger != nil {
		serverLogger.Info("Agent registering", "agent_id", req.AgentID, "version", req.AgentVersion, "host", req.Hostname)
	}

	// Check protocol version compatibility
	if req.ProtocolVersion != ProtocolVersion {
		if serverLogger != nil {
			serverLogger.Warn("Protocol version mismatch", "agent", req.ProtocolVersion, "server", ProtocolVersion)
		}
		http.Error(w, fmt.Sprintf("Protocol mismatch: server supports v%s, agent uses v%s",
			ProtocolVersion, req.ProtocolVersion), http.StatusBadRequest)
		return
	}

	// Generate secure token for this agent
	token, err := generateToken()
	if err != nil {
		if serverLogger != nil {
			serverLogger.Error("Failed to generate token", "agent_id", req.AgentID, "error", err)
		}
		http.Error(w, "Failed to generate authentication token", http.StatusInternalServerError)
		return
	}

	// Save agent to database with token
	agent := &storage.Agent{
		AgentID:         req.AgentID,
		Hostname:        req.Hostname,
		IP:              req.IP,
		Platform:        req.Platform,
		Version:         req.AgentVersion,
		ProtocolVersion: req.ProtocolVersion,
		Token:           token,
		RegisteredAt:    time.Now(),
		LastSeen:        time.Now(),
		Status:          "active",
	}

	ctx := context.Background()
	if err := serverStore.RegisterAgent(ctx, agent); err != nil {
		if serverLogger != nil {
			serverLogger.Error("Failed to register agent", "agent_id", req.AgentID, "error", err)
		}
		http.Error(w, "Failed to register agent", http.StatusInternalServerError)
		return
	}

	// Log audit entry for registration
	clientIP := extractClientIP(r)
	logAuditEntry(ctx, req.AgentID, "register", fmt.Sprintf("Agent registered: %s v%s on %s",
		req.Hostname, req.AgentVersion, req.Platform), clientIP)

	if serverLogger != nil {
		serverLogger.Info("Agent registered successfully", "agent_id", req.AgentID, "token", token[:8]+"...")
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"agent_id": req.AgentID,
		"token":    token,
		"message":  "Agent registered successfully",
	})
}

// Agent heartbeat - periodic ping to show agent is alive
func handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		AgentID   string    `json:"agent_id"`
		Timestamp time.Time `json:"timestamp"`
		Status    string    `json:"status"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Get authenticated agent from context
	agent := r.Context().Value("agent").(*storage.Agent)

	// Update agent last_seen
	ctx := context.Background()
	if err := serverStore.UpdateAgentHeartbeat(ctx, agent.AgentID, req.Status); err != nil {
		if serverLogger != nil {
			serverLogger.Warn("Failed to update heartbeat", "agent_id", agent.AgentID, "error", err)
		}
		// Don't fail the request, just log it
	}

	// Log audit entry for heartbeat (only occasionally to reduce log volume)
	// Could add logic here to only log every Nth heartbeat
	clientIP := extractClientIP(r)
	logAuditEntry(ctx, agent.AgentID, "heartbeat", fmt.Sprintf("Status: %s", req.Status), clientIP)

	if serverLogger != nil {
		serverLogger.Debug("Heartbeat received", "agent_id", agent.AgentID, "status", req.Status)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

// Devices batch upload - agent sends discovered devices
func handleDevicesBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		AgentID   string                   `json:"agent_id"`
		Timestamp time.Time                `json:"timestamp"`
		Devices   []map[string]interface{} `json:"devices"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if serverLogger != nil {
			serverLogger.Warn("Invalid JSON in devices batch", "error", err)
		}
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if serverLogger != nil {
		serverLogger.Info("Devices batch received", "agent_id", req.AgentID, "count", len(req.Devices))
	}

	// Store each device
	ctx := context.Background()
	stored := 0
	for _, deviceMap := range req.Devices {
		// Convert map to Device struct (simplified - in production, use proper unmarshaling)
		device := &storage.Device{
			AgentID:   req.AgentID,
			LastSeen:  req.Timestamp,
			FirstSeen: req.Timestamp,
			CreatedAt: req.Timestamp,
		}

		// Extract fields from map
		if v, ok := deviceMap["serial"].(string); ok {
			device.Serial = v
		}
		if v, ok := deviceMap["ip"].(string); ok {
			device.IP = v
		}
		if v, ok := deviceMap["manufacturer"].(string); ok {
			device.Manufacturer = v
		}
		if v, ok := deviceMap["model"].(string); ok {
			device.Model = v
		}
		if v, ok := deviceMap["hostname"].(string); ok {
			device.Hostname = v
		}
		if v, ok := deviceMap["firmware"].(string); ok {
			device.Firmware = v
		}
		if v, ok := deviceMap["mac_address"].(string); ok {
			device.MACAddress = v
		}
		device.RawData = deviceMap

		if device.Serial == "" {
			if serverLogger != nil {
				serverLogger.Warn("Device missing serial, skipping", "ip", device.IP)
			}
			continue
		}

		if err := serverStore.UpsertDevice(ctx, device); err != nil {
			if serverLogger != nil {
				serverLogger.Error("Failed to store device", "serial", device.Serial, "error", err)
			}
			continue
		}
		stored++
	}

	// Get authenticated agent from context
	agent := r.Context().Value("agent").(*storage.Agent)

	if serverLogger != nil {
		serverLogger.Info("Devices stored", "agent_id", agent.AgentID, "stored", stored, "total", len(req.Devices))
	}

	// Log audit entry for device upload
	clientIP := extractClientIP(r)
	logAuditEntry(ctx, agent.AgentID, "upload_devices",
		fmt.Sprintf("Uploaded %d devices (%d stored)", len(req.Devices), stored), clientIP)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"received": len(req.Devices),
		"stored":   stored,
	})
}

// Metrics batch upload - agent sends device metrics
func handleMetricsBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		AgentID   string                   `json:"agent_id"`
		Timestamp time.Time                `json:"timestamp"`
		Metrics   []map[string]interface{} `json:"metrics"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if serverLogger != nil {
			serverLogger.Warn("Invalid JSON in metrics batch", "error", err)
		}
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if serverLogger != nil {
		serverLogger.Info("Metrics batch received", "agent_id", req.AgentID, "count", len(req.Metrics))
	}

	// Store each metric snapshot
	ctx := context.Background()
	stored := 0
	for _, metricMap := range req.Metrics {
		metric := &storage.MetricsSnapshot{
			AgentID:   req.AgentID,
			Timestamp: req.Timestamp,
		}

		// Extract fields
		if v, ok := metricMap["serial"].(string); ok {
			metric.Serial = v
		}
		if v, ok := metricMap["page_count"].(float64); ok {
			metric.PageCount = int(v)
		}
		if v, ok := metricMap["color_pages"].(float64); ok {
			metric.ColorPages = int(v)
		}
		if v, ok := metricMap["mono_pages"].(float64); ok {
			metric.MonoPages = int(v)
		}
		if v, ok := metricMap["scan_count"].(float64); ok {
			metric.ScanCount = int(v)
		}
		if v, ok := metricMap["toner_levels"].(map[string]interface{}); ok {
			metric.TonerLevels = v
		}

		if metric.Serial == "" {
			continue
		}

		if err := serverStore.SaveMetrics(ctx, metric); err != nil {
			if serverLogger != nil {
				serverLogger.Error("Failed to store metrics", "serial", metric.Serial, "error", err)
			}
			continue
		}
		stored++
	}

	// Get authenticated agent from context
	agent := r.Context().Value("agent").(*storage.Agent)

	if serverLogger != nil {
		serverLogger.Info("Metrics stored", "agent_id", agent.AgentID, "stored", stored, "total", len(req.Metrics))
	}

	// Log audit entry for metrics upload
	clientIP := extractClientIP(r)
	logAuditEntry(ctx, agent.AgentID, "upload_metrics",
		fmt.Sprintf("Uploaded %d metric snapshots (%d stored)", len(req.Metrics), stored), clientIP)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"received": len(req.Metrics),
		"stored":   stored,
	})
}

// Web UI placeholder
func handleWebUI(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html>
<html>
<head>
	<title>PrintMaster Server</title>
	<style>
		body { font-family: system-ui; max-width: 1200px; margin: 50px auto; padding: 20px; }
		h1 { color: #333; }
		.info { background: #f0f0f0; padding: 20px; border-radius: 8px; }
		.info p { margin: 10px 0; }
		code { background: #e0e0e0; padding: 2px 6px; border-radius: 3px; }
	</style>
</head>
<body>
	<h1>🖨️ PrintMaster Server</h1>
	<div class="info">
		<p><strong>Version:</strong> ` + Version + `</p>
		<p><strong>Protocol:</strong> v` + ProtocolVersion + `</p>
		<p><strong>Status:</strong> Running</p>
		<p><strong>Build:</strong> ` + BuildTime + ` (` + GitCommit + `)</p>
	</div>
	<h2>API Endpoints</h2>
	<ul>
		<li><code>GET /health</code> - Health check</li>
		<li><code>GET /api/version</code> - Version info</li>
		<li><code>POST /api/v1/agents/register</code> - Register agent</li>
		<li><code>POST /api/v1/agents/heartbeat</code> - Agent heartbeat</li>
		<li><code>POST /api/v1/devices/batch</code> - Upload devices</li>
		<li><code>POST /api/v1/metrics/batch</code> - Upload metrics</li>
	</ul>
	<h2>Next Steps</h2>
	<p>🚧 Web UI under development</p>
	<p>Configure agents to point to: <code>http://this-server:9090</code></p>
</body>
</html>`
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// getDefaultDBPath returns platform-specific database path
func getDefaultDBPath() string {
	switch runtime.GOOS {
	case "windows":
		return `C:\ProgramData\PrintMaster\server.db`
	case "darwin":
		home, _ := os.UserHomeDir()
		return home + "/Library/Application Support/PrintMaster/server.db"
	default: // linux, etc.
		home, _ := os.UserHomeDir()
		return home + "/.local/share/printmaster/server.db"
	}
}
