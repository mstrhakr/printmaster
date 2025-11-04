// PrintMaster Server - Central management hub for PrintMaster agents
// Aggregates data from multiple agents, provides reporting, alerting, and web UI
package main

import (
	"context"
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

func setupRoutes() {
	// Health check
	http.HandleFunc("/health", handleHealth)

	// Version info
	http.HandleFunc("/api/version", handleVersion)

	// Agent API (v1)
	http.HandleFunc("/api/v1/agents/register", handleAgentRegister)
	http.HandleFunc("/api/v1/agents/heartbeat", handleAgentHeartbeat)
	http.HandleFunc("/api/v1/devices/batch", handleDevicesBatch)
	http.HandleFunc("/api/v1/metrics/batch", handleMetricsBatch)

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
		serverLogger.Warn("Invalid JSON in agent register", "error", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	serverLogger.Info("Agent registering", "agent_id", req.AgentID, "version", req.AgentVersion, "host", req.Hostname)

	// Check protocol version compatibility
	if req.ProtocolVersion != ProtocolVersion {
		serverLogger.Warn("Protocol version mismatch", "agent", req.ProtocolVersion, "server", ProtocolVersion)
		http.Error(w, fmt.Sprintf("Protocol mismatch: server supports v%s, agent uses v%s",
			ProtocolVersion, req.ProtocolVersion), http.StatusBadRequest)
		return
	}

	// Save agent to database
	agent := &storage.Agent{
		AgentID:         req.AgentID,
		Hostname:        req.Hostname,
		IP:              req.IP,
		Platform:        req.Platform,
		Version:         req.AgentVersion,
		ProtocolVersion: req.ProtocolVersion,
		RegisteredAt:    time.Now(),
		LastSeen:        time.Now(),
		Status:          "active",
	}

	ctx := context.Background()
	if err := serverStore.RegisterAgent(ctx, agent); err != nil {
		serverLogger.Error("Failed to register agent", "agent_id", req.AgentID, "error", err)
		http.Error(w, "Failed to register agent", http.StatusInternalServerError)
		return
	}

	serverLogger.Info("Agent registered successfully", "agent_id", req.AgentID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"agent_id": req.AgentID,
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

	// Update agent last_seen
	ctx := context.Background()
	if err := serverStore.UpdateAgentHeartbeat(ctx, req.AgentID, req.Status); err != nil {
		serverLogger.Warn("Failed to update heartbeat", "agent_id", req.AgentID, "error", err)
		// Don't fail the request, just log it
	}

	serverLogger.Debug("Heartbeat received", "agent_id", req.AgentID, "status", req.Status)

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
		serverLogger.Warn("Invalid JSON in devices batch", "error", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	serverLogger.Info("Devices batch received", "agent_id", req.AgentID, "count", len(req.Devices))

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
			serverLogger.Warn("Device missing serial, skipping", "ip", device.IP)
			continue
		}

		if err := serverStore.UpsertDevice(ctx, device); err != nil {
			serverLogger.Error("Failed to store device", "serial", device.Serial, "error", err)
			continue
		}
		stored++
	}

	serverLogger.Info("Devices stored", "agent_id", req.AgentID, "stored", stored, "total", len(req.Devices))

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
		serverLogger.Warn("Invalid JSON in metrics batch", "error", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	serverLogger.Info("Metrics batch received", "agent_id", req.AgentID, "count", len(req.Metrics))

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
			serverLogger.Error("Failed to store metrics", "serial", metric.Serial, "error", err)
			continue
		}
		stored++
	}

	serverLogger.Info("Metrics stored", "agent_id", req.AgentID, "stored", stored, "total", len(req.Metrics))

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
