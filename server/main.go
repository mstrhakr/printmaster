// PrintMaster Server - Central management hub for PrintMaster agents
// Aggregates data from multiple agents, provides reporting, alerting, and web UI
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"printmaster/common/config"
	"printmaster/common/logger"
	commonutil "printmaster/common/util"
	sharedweb "printmaster/common/web"
	wscommon "printmaster/common/ws"
	"printmaster/server/storage"
	tenancy "printmaster/server/tenancy"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kardianos/service"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const (
	agentContextKey     contextKey = "agent"
	userContextKey      contextKey = "user"
	principalContextKey contextKey = "principal"
)

// Principal represents the authenticated user along with cached authorization helpers.
type Principal struct {
	User      *storage.User
	Role      storage.Role
	TenantIDs []string
}

func newPrincipal(u *storage.User) *Principal {
	if u == nil {
		return nil
	}
	ids := append([]string{}, u.TenantIDs...)
	if len(ids) == 0 && strings.TrimSpace(u.TenantID) != "" {
		ids = []string{strings.TrimSpace(u.TenantID)}
	}
	ids = storage.SortTenantIDs(ids)
	return &Principal{
		User:      u,
		Role:      storage.NormalizeRole(string(u.Role)),
		TenantIDs: ids,
	}
}

func (p *Principal) IsAdmin() bool {
	return p != nil && p.Role == storage.RoleAdmin
}

func (p *Principal) HasRole(min storage.Role) bool {
	return rolePriority(p.Role) >= rolePriority(min)
}

func (p *Principal) AllowedTenantIDs() []string {
	if p == nil || p.IsAdmin() {
		return nil
	}
	return append([]string{}, p.TenantIDs...)
}

func (p *Principal) CanAccessTenant(tenantID string) bool {
	if p == nil {
		return false
	}
	if tenantID == "" {
		return p.IsAdmin()
	}
	if p.IsAdmin() {
		return true
	}
	for _, id := range p.TenantIDs {
		if id == tenantID {
			return true
		}
	}
	return false
}

func rolePriority(role storage.Role) int {
	switch role {
	case storage.RoleAdmin:
		return 3
	case storage.RoleOperator:
		return 2
	case storage.RoleViewer:
		return 1
	default:
		return 0
	}
}

func tenantScope(principal *Principal) (map[string]struct{}, bool) {
	if principal == nil {
		return nil, false
	}
	if principal.IsAdmin() {
		return nil, true
	}
	allowed := principal.AllowedTenantIDs()
	if len(allowed) == 0 {
		return nil, false
	}
	set := make(map[string]struct{}, len(allowed))
	for _, id := range allowed {
		set[id] = struct{}{}
	}
	return set, true
}

func tenantAllowed(scope map[string]struct{}, tenantID string) bool {
	if scope == nil {
		return true
	}
	_, ok := scope[tenantID]
	return ok
}

//go:embed web
var webFS embed.FS

// Version information (set at build time via -ldflags)
var (
	Version         = "dev"     // Semantic version (e.g., "0.1.0")
	BuildTime       = "unknown" // Build timestamp
	GitCommit       = "unknown" // Git commit hash
	BuildType       = "dev"     // "dev" or "release"
	ProtocolVersion = "1"       // Agent-Server protocol version
)

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

var (
	serverLogger       *logger.Logger
	serverStore        storage.Store
	authRateLimiter    *AuthRateLimiter // Rate limiter for failed auth attempts
	configLoadErrors   []string         // Track config loading errors for display in UI
	usingDefaultConfig bool             // Flag to indicate if using defaults vs loaded config
	loadedConfigPath   string           // Path of the config file that was successfully loaded
	sseHub             *SSEHub          // SSE hub for real-time UI updates
	wsHub              *wscommon.Hub    // In-process hub for websocket-capable UI clients
	serverConfig       *Config          // Loaded server configuration (accessible to handlers)
)

// Ensure SSE hub exists by default so handlers can broadcast without nil checks.
func init() {
	// If tests or other packages haven't initialized the hub yet, create it now.
	if sseHub == nil {
		sseHub = NewSSEHub()
	}
}

func main() {
	// Command line flags
	configPath := flag.String("config", "config.toml", "Configuration file path")
	generateConfig := flag.Bool("generate-config", false, "Generate default config file and exit")
	showVersion := flag.Bool("version", false, "Show version information and exit")
	quiet := flag.Bool("quiet", false, "Suppress informational output (errors/warnings still shown)")
	flag.BoolVar(quiet, "q", false, "Shorthand for --quiet")
	silent := flag.Bool("silent", false, "Suppress ALL output (complete silence)")
	flag.BoolVar(silent, "s", false, "Shorthand for --silent")

	// Service management flags
	svcCommand := flag.String("service", "", "Service command: install, uninstall, start, stop, restart, run")
	flag.Parse()

	// Set quiet/silent mode globally for util functions
	if *silent {
		commonutil.SetSilentMode(true)
	} else {
		commonutil.SetQuietMode(*quiet)
	}

	// Show version if requested
	if *showVersion {
		fmt.Printf("PrintMaster Server %s\n", Version)
		fmt.Printf("Protocol Version: %s\n", ProtocolVersion)
		fmt.Printf("Build Time: %s\n", BuildTime)
		fmt.Printf("Git Commit: %s\n", GitCommit)
		fmt.Printf("Build Type: %s\n", BuildType)
		fmt.Printf("Go Version: %s\n", runtime.Version())
		fmt.Printf("OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		return
	}

	// Generate default config if requested
	if *generateConfig {
		if err := WriteDefaultConfig(*configPath); err != nil {
			logFatal("Failed to generate config", "error", err)
		}
		logInfo("Generated default configuration", "path", *configPath)
		return
	}

	// Handle service commands
	if *svcCommand != "" {
		handleServiceCommand(*svcCommand)
		return
	}

	// Check if running as service (non-interactive)
	if !service.Interactive() {
		// Running as service - use service runner
		prg := &program{}
		s, err := service.New(prg, getServiceConfig())
		if err != nil {
			logFatal("Failed to create service", "error", err)
		}
		if err = s.Run(); err != nil {
			logFatal("Service execution failed", "error", err)
		}
		return
	}

	// Running interactively
	runServer(context.Background(), *configPath)
}

// runServer starts the server with the given context
func runServer(ctx context.Context, configFlag string) {
	// Load configuration from multiple locations
	// Priority when running as service: ProgramData/server > ProgramData (legacy)
	// Priority when interactive: executable directory > current directory
	var cfg *Config
	var configLoaded bool

	isService := !service.Interactive()
	var configPaths []string

	// Resolve config path using shared helper (checks SERVER_CONFIG/SERVER_CONFIG_PATH,
	// generic CONFIG/CONFIG_PATH, then the provided flag value)
	resolved := config.ResolveConfigPath("SERVER", configFlag)
	if resolved != "" {
		if _, statErr := os.Stat(resolved); statErr == nil {
			if loadedCfg, err := LoadConfig(resolved); err == nil {
				cfg = loadedCfg
				loadedConfigPath = resolved
				configLoaded = true
				logInfo("Loaded configuration", "path", resolved)
			} else {
				errMsg := fmt.Sprintf("Config file exists but failed to load: %s - Error: %v", resolved, err)
				configLoadErrors = append(configLoadErrors, errMsg)
				logWarn("Config file load failed", "path", resolved, "error", err)
			}
		} else {
			logWarn("Config path set but file not found", "path", resolved)
		}
	}

	if isService {
		// Running as service - check ProgramData locations only
		programData := os.Getenv("PROGRAMDATA")
		if programData == "" {
			programData = "C:\\ProgramData"
		}
		configPaths = []string{
			filepath.Join(programData, "PrintMaster", "server", "config.toml"),
			filepath.Join(programData, "PrintMaster", "config.toml"), // Legacy location
		}
	} else {
		// Running interactively - check local locations only
		configPaths = []string{
			filepath.Join(filepath.Dir(os.Args[0]), "config.toml"),
			"config.toml",
		}
	}

	configLoaded = false
	for _, configPath := range configPaths {
		if _, statErr := os.Stat(configPath); statErr == nil {
			// Config file exists, try to load it
			if loadedCfg, err := LoadConfig(configPath); err == nil {
				cfg = loadedCfg
				loadedConfigPath = configPath
				configLoaded = true
				logInfo("Loaded configuration", "path", configPath)
				break
			} else {
				// Config file exists but failed to parse
				errMsg := fmt.Sprintf("Config file exists but failed to load: %s - Error: %v", configPath, err)
				configLoadErrors = append(configLoadErrors, errMsg)
				logWarn("Config file load failed", "path", configPath, "error", err)
			}
		}
	}

	if !configLoaded {
		if len(configLoadErrors) > 0 {
			logError("Configuration files found but failed to parse; using defaults", "errors", strings.Join(configLoadErrors, "; "))
		} else {
			logWarn("No config.toml found; using defaults")
		}
		cfg = DefaultConfig()
		loadedConfigPath = "defaults"
		usingDefaultConfig = true
	} else {
		usingDefaultConfig = false
	}

	// Always apply environment overrides for database path (supports SERVER_DB_PATH and DB_PATH)
	// even when using default configuration (no config file present).
	config.ApplyDatabaseEnvOverrides(&cfg.Database, "SERVER")
	if cfg.Database.Path != "" {
		// If the env var points to a directory, append the default filename
		// so users can set either a directory or a full file path.
		dbPath := cfg.Database.Path
		// Normalize and detect directory-like values
		if strings.HasSuffix(dbPath, string(os.PathSeparator)) || strings.HasSuffix(dbPath, "/") {
			dbPath = filepath.Join(dbPath, "server.db")
		} else {
			if fi, err := os.Stat(dbPath); err == nil && fi.IsDir() {
				dbPath = filepath.Join(dbPath, "server.db")
			}
		}

		// Ensure parent directory exists
		parent := filepath.Dir(dbPath)
		if err := os.MkdirAll(parent, 0755); err != nil {
			logWarn("Could not create DB parent directory; falling back to default", "path", parent, "error", err)
			// clear to allow fallback logic to run
			cfg.Database.Path = ""
		} else {
			// Try to open or create the DB file to ensure we have write access
			f, err := os.OpenFile(dbPath, os.O_RDWR|os.O_CREATE, 0644)
			if err != nil {
				logWarn("Cannot write to DB path; falling back to default", "path", dbPath, "error", err)
				cfg.Database.Path = ""
			} else {
				f.Close()
				cfg.Database.Path = dbPath
				logInfo("Database path overridden by environment", "path", cfg.Database.Path)
			}
		}
	}

	logInfo("PrintMaster Server starting", "version", Version, "protocol_version", ProtocolVersion)
	logInfo("Build metadata", "build_time", BuildTime, "git_commit", GitCommit, "build_type", BuildType)
	logDebug("Runtime", "go", runtime.Version(), "os", runtime.GOOS, "arch", runtime.GOARCH)

	// Initialize logger
	if cfg.Database.Path == "" {
		// Use platform default, but on Windows if we're running interactively
		// prefer the user AppData location when ProgramData is not writable
		defaultPath := storage.GetDefaultDBPath()
		cfg.Database.Path = defaultPath
		if runtime.GOOS == "windows" && !isService {
			// If defaultPath looks like ProgramData and we can't create the parent dir,
			// fall back to the interactive data directory (AppData).
			pd := os.Getenv("PROGRAMDATA")
			if pd == "" {
				pd = "C:\\ProgramData"
			}
			if strings.HasPrefix(strings.ToLower(defaultPath), strings.ToLower(pd)) {
				parent := filepath.Dir(defaultPath)
				if err := os.MkdirAll(parent, 0755); err != nil {
					// Can't create ProgramData path — fall back to per-user data dir
					logInfo("Falling back to user data directory because ProgramData is not writable", "programdata", pd, "error", err)
					if userDir, derr := config.GetDataDirectory("server", false); derr == nil {
						// Ensure directory exists
						if err := os.MkdirAll(userDir, 0755); err == nil {
							cfg.Database.Path = filepath.Join(userDir, "server.db")
						} else {
							// If we still can't create, keep default and hope for the best
							logWarn("Failed to create user data directory; using default DB path", "user_dir", userDir, "error", err)
						}
					}
				}
			}
		}
	}

	// Determine log directory based on whether we're running as a service
	logDir, err := config.GetLogDirectory("server", isService)
	if err != nil {
		logFatal("Failed to get log directory", "error", err)
	}

	serverLogger = logger.NewWithComponent(logger.LevelFromString(cfg.Logging.Level), logDir, "server", 1000)
	logInfo("Server starting", "version", Version, "protocol", ProtocolVersion, "config", loadedConfigPath)

	// Save loaded config globally for handlers
	serverConfig = cfg

	// Initialize database
	logInfo("Using database", "path", cfg.Database.Path)
	logInfo("Initializing database", "path", cfg.Database.Path)

	// Inject structured logger into storage package so DB initialization logs are structured
	storage.SetLogger(serverLogger)
	serverStore, err = storage.NewSQLiteStore(cfg.Database.Path)
	if err != nil {
		logFatal("Failed to initialize database", "error", err)
	}
	defer serverStore.Close()

	logInfo("Database initialized successfully")

	// Bootstrap initial admin user. Default to ADMIN_USER=admin and ADMIN_PASSWORD=printmaster
	adminUser := os.Getenv("ADMIN_USER")
	if adminUser == "" {
		adminUser = "admin"
	}
	adminPass := os.Getenv("ADMIN_PASSWORD")
	if adminPass == "" {
		adminPass = "printmaster"
	}

	bctx := context.Background()
	if _, err := serverStore.GetUserByUsername(bctx, adminUser); err != nil {
		// create admin user
		u := &storage.User{Username: adminUser, Role: storage.RoleAdmin}
		if err := serverStore.CreateUser(bctx, u, adminPass); err != nil {
			logWarn("Failed to create initial admin user", "user", adminUser, "error", err)
		} else {
			logInfo("Initial admin user created (default)", "user", adminUser)
		}
	}

	// Initialize SSE hub for real-time UI updates if not already created (tests may have pre-initialized)
	if sseHub == nil {
		sseHub = NewSSEHub()
	}
	// Initialize wsHub and bridge SSE -> WS
	if wsHub == nil {
		wsHub = wscommon.NewHub()
		// Create a bridge SSE client to forward events into wsHub
		go func() {
			client := sseHub.NewClient()
			id := "sse-bridge"
			ch := make(chan wscommon.Message, 20)
			wsHub.Register(id, ch)
			defer func() {
				wsHub.Unregister(id)
				sseHub.RemoveClient(client)
				close(ch)
			}()

			for ev := range client.events {
				m := wscommon.Message{Type: ev.Type, Data: ev.Data}
				wsHub.Broadcast(m)
			}
		}()
	}
	logInfo("SSE hub initialized")

	// Initialize authentication rate limiter if enabled
	if cfg.Security.RateLimitEnabled {
		maxAttempts := cfg.Security.RateLimitMaxAttempts
		blockDuration := time.Duration(cfg.Security.RateLimitBlockMinutes) * time.Minute
		attemptsWindow := time.Duration(cfg.Security.RateLimitWindowMinutes) * time.Minute

		authRateLimiter = NewAuthRateLimiter(maxAttempts, blockDuration, attemptsWindow)
		logInfo("Authentication rate limiter initialized",
			"enabled", true,
			"max_attempts", maxAttempts,
			"block_duration", cfg.Security.RateLimitBlockMinutes,
			"window_minutes", cfg.Security.RateLimitWindowMinutes)
	} else {
		logInfo("Authentication rate limiter disabled")
	}

	// Setup HTTP routes
	setupRoutes(cfg)

	// Get TLS configuration
	tlsConfig := cfg.ToTLSConfig()

	// Start server based on deployment mode with graceful shutdown context
	if tlsConfig.BehindProxy {
		// nginx mode: HTTP only on localhost
		startReverseProxyMode(ctx, tlsConfig)
	} else {
		// Standalone mode: HTTPS only
		startStandaloneMode(ctx, tlsConfig)
	}
}

// handleServiceCommand handles service management commands
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
		commonutil.ShowBanner(Version, GitCommit, BuildTime, "Central Management Server")

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
		commonutil.ShowBanner(Version, GitCommit, BuildTime, "Central Management Server")
		commonutil.ShowInfo("Uninstalling service...")
		err = s.Uninstall()
		if err != nil {
			commonutil.ShowError(fmt.Sprintf("Failed to uninstall service: %v", err))
			commonutil.ShowCompletionScreen(false, "Uninstall Failed")
			os.Exit(1)
		}
		commonutil.ShowSuccess("Service uninstalled")
		commonutil.ShowCompletionScreen(true, "Service Uninstalled!")

	case "start":
		commonutil.ShowBanner(Version, GitCommit, BuildTime, "Central Management Server")
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
		commonutil.ShowBanner(Version, GitCommit, BuildTime, "Central Management Server")
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
		commonutil.ShowBanner(Version, GitCommit, BuildTime, "Central Management Server")

		status, statusErr := s.Status()

		fmt.Println()
		commonutil.ShowInfo("Service Status Information")
		fmt.Println()

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

		cfg := getServiceConfig()
		fmt.Printf("  %sService Name:%s  %s\n", commonutil.ColorDim, commonutil.ColorReset, cfg.Name)
		fmt.Printf("  %sDisplay Name:%s  %s\n", commonutil.ColorDim, commonutil.ColorReset, cfg.DisplayName)
		fmt.Printf("  %sDescription:%s   %s\n", commonutil.ColorDim, commonutil.ColorReset, cfg.Description)
		fmt.Printf("  %sData Directory:%s %s\n", commonutil.ColorDim, commonutil.ColorReset, cfg.WorkingDirectory)

		fmt.Println()

		switch status {
		case service.StatusRunning:
			commonutil.ShowInfo("Server is running normally")
			fmt.Println()
			fmt.Printf("  %sHTTPS URL:%s https://localhost:9443\n", commonutil.ColorDim, commonutil.ColorReset)
		case service.StatusStopped:
			commonutil.ShowWarning("Service is installed but not running - Use '--service start' to start the service")
		default:
			commonutil.ShowWarning("Service is not installed - Use '--service install' to install the service")
		}

		fmt.Println()
		commonutil.PromptToContinue()

	case "restart":
		commonutil.ShowBanner(Version, GitCommit, BuildTime, "Central Management Server")
		commonutil.ShowInfo("Restarting service...")
		err = s.Restart()
		if err != nil {
			commonutil.ShowError(fmt.Sprintf("Failed to restart service: %v", err))
			commonutil.ShowCompletionScreen(false, "Restart Failed")
			os.Exit(1)
		}
		commonutil.ShowSuccess("Service restarted")
		commonutil.ShowCompletionScreen(true, "Service Restarted!")

	case "update":
		// Show banner
		commonutil.ShowBanner(Version, GitCommit, BuildTime, "Central Management Server")

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
		// This is called by the service manager when starting the service
		if err := s.Run(); err != nil {
			logFatal("Service run failed", "error", err)
		}

	default:
		logFatal("Invalid service command", "command", cmd)
	}
}

// startReverseProxyMode starts the server in reverse proxy mode (behind nginx)
// Supports both HTTP and HTTPS based on configuration
func startReverseProxyMode(ctx context.Context, tlsConfig *TLSConfig) {
	// Use configured bind address, default to all interfaces if not set
	bindAddr := tlsConfig.BindAddress
	if bindAddr == "" {
		bindAddr = "0.0.0.0"
	}

	// Add reverse proxy middleware
	handler := loggingMiddleware(reverseProxyMiddleware(http.DefaultServeMux))

	// Determine if we're using HTTPS for end-to-end encryption
	if tlsConfig.ProxyUseHTTPS {
		// HTTPS mode: end-to-end encryption with reverse proxy
		addr := fmt.Sprintf("%s:%d", bindAddr, tlsConfig.HTTPSPort)

		// Get TLS configuration
		tlsCfg, err := tlsConfig.GetTLSConfig()
		if err != nil {
			logFatal("Failed to setup TLS for reverse proxy mode", "error", err)
		}

		logInfo("Starting in reverse proxy mode with HTTPS (end-to-end encryption)",
			"bind", addr,
			"tls_mode", tlsConfig.Mode,
			"trust_proxy", true)

		logInfo("HTTPS server listening (reverse proxy mode)", "addr", addr, "tls_mode", tlsConfig.Mode)
		logInfo("Reverse proxy terminates outer TLS, server uses inner TLS")
		logInfo("Server ready to accept agent connections")

		// Create HTTPS server
		httpsServer := &http.Server{
			Addr:      addr,
			TLSConfig: tlsCfg,
			Handler:   handler,
			ErrorLog:  log.New(logBridgeWriter{level: logger.ERROR}, "[HTTPS] ", 0),
			ConnState: func(conn net.Conn, state http.ConnState) {
				if state == http.StateNew {
					logDebug("New connection", "remote_addr", conn.RemoteAddr().String())
				}
			},
		}

		// Start server in goroutine
		go func() {
			if err := httpsServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
				logFatal("HTTPS server failed", "error", err)
			}
		}()

		logInfo("HTTPS server started", "addr", addr)

		// Wait for shutdown signal
		<-ctx.Done()
		logInfo("Shutdown signal received, stopping HTTPS server...")

		// Graceful shutdown with 30 second timeout
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := httpsServer.Shutdown(shutdownCtx); err != nil {
			logError("HTTPS server shutdown error", "error", err)
		} else {
			logInfo("HTTPS server stopped gracefully")
		}
	} else {
		// HTTP mode: reverse proxy handles all TLS
		addr := fmt.Sprintf("%s:%d", bindAddr, tlsConfig.HTTPPort)

		logInfo("Starting in reverse proxy mode with HTTP (HTTPS terminated by proxy)",
			"bind", addr,
			"trust_proxy", true)

		logInfo("HTTP server listening (reverse proxy mode)", "addr", addr)
		logInfo("HTTPS termination handled by nginx/reverse proxy")
		logInfo("Server ready to accept agent connections")

		// Create HTTP server
		httpServer := &http.Server{
			Addr:     addr,
			Handler:  handler,
			ErrorLog: log.New(logBridgeWriter{level: logger.ERROR}, "[HTTP] ", 0),
		}

		// Start server in goroutine
		go func() {
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logFatal("HTTP server failed", "error", err)
			}
		}()

		logInfo("HTTP server started", "addr", addr)

		// Wait for shutdown signal
		<-ctx.Done()
		logInfo("Shutdown signal received, stopping HTTP server...")

		// Graceful shutdown with 30 second timeout
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logError("HTTP server shutdown error", "error", err)
		} else {
			logInfo("HTTP server stopped gracefully")
		}
	}
}

// startStandaloneMode starts the server in standalone HTTPS-only mode
func startStandaloneMode(ctx context.Context, tlsConfig *TLSConfig) {
	// Get TLS configuration
	tlsCfg, err := tlsConfig.GetTLSConfig()
	if err != nil {
		logFatal("Failed to setup TLS", "error", err, "mode", tlsConfig.Mode)
	}

	// Use configured bind address, default to all interfaces if not set
	bindAddr := tlsConfig.BindAddress
	if bindAddr == "" {
		bindAddr = "0.0.0.0"
	}
	httpsAddr := fmt.Sprintf("%s:%d", bindAddr, tlsConfig.HTTPSPort)

	logInfo("Starting in standalone HTTPS mode",
		"port", tlsConfig.HTTPSPort,
		"tls_mode", tlsConfig.Mode,
		"bind_address", httpsAddr)

	logDebug("TLS configuration loaded",
		"min_version", "TLS 1.2",
		"has_certificates", len(tlsCfg.Certificates) > 0,
		"cert_count", len(tlsCfg.Certificates))

	logInfo("HTTPS server listening", "addr", httpsAddr)
	logInfo("TLS mode", "mode", tlsConfig.Mode)

	if tlsConfig.Mode == TLSModeLetsEncrypt {
		logInfo("Let's Encrypt configuration", "domain", tlsConfig.LetsEncryptDomain, "email", tlsConfig.LetsEncryptEmail)

		// Start HTTP server for ACME challenges
		go startACMEChallengeServer(tlsConfig)
	}

	logInfo("Server ready to accept agent connections (HTTPS only)")

	// Create HTTPS server with security headers
	httpsServer := &http.Server{
		Addr:      httpsAddr,
		TLSConfig: tlsCfg,
		Handler:   loggingMiddleware(securityHeadersMiddleware(http.DefaultServeMux)),
		ErrorLog:  log.New(logBridgeWriter{level: logger.ERROR}, "[HTTPS] ", 0),
		ConnState: func(conn net.Conn, state http.ConnState) {
			if state == http.StateNew {
				logDebug("New connection", "remote_addr", conn.RemoteAddr().String())
			}
		},
	}

	logInfo("HTTPS server starting", "addr", httpsAddr)
	logDebug("Calling ListenAndServeTLS", "cert_empty", "", "key_empty", "")

	// Start server in goroutine
	go func() {
		if err := httpsServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			logFatal("HTTPS server failed", "error", err, "addr", httpsAddr)
		}
	}()

	logInfo("HTTPS server started successfully", "addr", httpsAddr)

	// Wait for shutdown signal
	<-ctx.Done()
	logInfo("Shutdown signal received, stopping HTTPS server...")

	// Graceful shutdown with 30 second timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := httpsServer.Shutdown(shutdownCtx); err != nil {
		logError("HTTPS server shutdown error", "error", err)
	} else {
		logInfo("HTTPS server stopped gracefully")
	}
}

// startACMEChallengeServer starts HTTP server for Let's Encrypt ACME challenges only
func startACMEChallengeServer(tlsConfig *TLSConfig) {
	mux := http.NewServeMux()

	// Get ACME handler
	acmeManager, err := tlsConfig.GetACMEHTTPHandler()
	if err != nil {
		logError("Failed to setup ACME handler", "error", err)
		return
	}

	// Handle ACME challenges
	mux.Handle("/.well-known/acme-challenge/", acmeManager.HTTPHandler(nil))

	// Reject all other requests
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "HTTPS required - This port only serves ACME challenges", http.StatusBadRequest)
	})

	logInfo("Starting ACME HTTP-01 challenge server", "port", 80)
	logInfo("ACME challenge server listening", "addr", ":80")

	if err := http.ListenAndServe(":80", mux); err != nil {
		logError("ACME challenge server failed", "error", err)
	}
}

// loggingMiddleware logs all incoming HTTP requests
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Log the incoming request at debug level
		logDebug("Incoming request",
			"method", r.Method,
			"path", r.URL.Path,
			"remote_addr", r.RemoteAddr,
			"host", r.Host,
			"proto", r.Proto,
			"tls", r.TLS != nil,
		)

		next.ServeHTTP(w, r)
	})
}

// reverseProxyMiddleware adds headers for reverse proxy mode
func reverseProxyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Trust X-Forwarded-Proto from nginx
		if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
			// Store for downstream handlers
			r.Header.Set("X-Detected-Proto", proto)
		}

		// Security headers (nginx might add these too, duplicates are OK)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Don't set HSTS here - let nginx handle it
		next.ServeHTTP(w, r)
	})
}

// securityHeadersMiddleware adds security headers for standalone HTTPS mode
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Full security headers for standalone mode
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self' ws: wss:; object-src 'none'; frame-ancestors 'none'")

		next.ServeHTTP(w, r)
	})
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
		// Extract client IP address
		clientIP := extractIPFromAddr(r.RemoteAddr)

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
		tokenPrefix := token
		if len(token) > 8 {
			tokenPrefix = token[:8]
		}

		// Check if this IP+token is currently blocked
		if authRateLimiter != nil {
			if isBlocked, blockedUntil := authRateLimiter.IsBlocked(clientIP, tokenPrefix); isBlocked {
				logWarn("Blocked authentication attempt",
					"ip", clientIP,
					"token", tokenPrefix+"...",
					"blocked_until", blockedUntil.Format(time.RFC3339),
					"user_agent", r.Header.Get("User-Agent"))
				http.Error(w, "Too many failed attempts. Try again later.", http.StatusTooManyRequests)
				return
			}
		}

		// Validate token against database
		ctx := context.Background()
		agent, err := serverStore.GetAgentByToken(ctx, token)
		if err != nil {
			// Record failed attempt and check if we should log
			var isBlocked, shouldLog bool
			var attemptCount int
			if authRateLimiter != nil {
				isBlocked, shouldLog, attemptCount = authRateLimiter.RecordFailure(clientIP, tokenPrefix)
			} else {
				isBlocked, shouldLog = false, true // Always log if rate limiter not initialized
			}

			if shouldLog {
				fields := []interface{}{
					"ip", clientIP,
					"token", tokenPrefix + "...",
					"error", err.Error(),
					"attempt_count", attemptCount,
					"user_agent", r.Header.Get("User-Agent"),
				}

				if isBlocked {
					fields = append(fields, "status", "BLOCKED")
					logError("Authentication failed - IP blocked", fields...)

					// Log to audit trail when blocking occurs
					logAuditEntry(ctx, "UNKNOWN", "auth_blocked",
						fmt.Sprintf("IP blocked after %d failed attempts with token %s... Error: %s",
							attemptCount, tokenPrefix, err.Error()),
						clientIP)
				} else if attemptCount >= 3 {
					logWarn("Repeated authentication failures", fields...)
				} else {
					logWarn("Invalid authentication attempt", fields...)
				}
			}

			if isBlocked {
				http.Error(w, "Too many failed attempts. Try again later.", http.StatusTooManyRequests)
			} else {
				http.Error(w, "Invalid token", http.StatusUnauthorized)
			}
			return
		}

		// Success - clear any failure records for this IP+token
		if authRateLimiter != nil {
			authRateLimiter.RecordSuccess(clientIP, tokenPrefix)
		}

		// Store agent info in request context for handlers to use
		ctx = context.WithValue(r.Context(), agentContextKey, agent)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

var (
	errSessionMissing = errors.New("session token missing")
	errSessionInvalid = errors.New("session invalid")
	errSessionUser    = errors.New("session user invalid")
)

func sessionTokenFromRequest(r *http.Request) string {
	if ah := r.Header.Get("Authorization"); ah != "" {
		parts := strings.SplitN(ah, " ", 2)
		if len(parts) == 2 && parts[0] == "Bearer" {
			return parts[1]
		}
	}
	if c, err := r.Cookie("pm_session"); err == nil {
		return c.Value
	}
	return ""
}

func loadUserForSessionToken(token string) (*storage.User, error) {
	if token == "" {
		return nil, errSessionMissing
	}
	ctx := context.Background()
	ses, err := serverStore.GetSessionByToken(ctx, token)
	if err != nil {
		return nil, errSessionInvalid
	}
	user, err := serverStore.GetUserByID(ctx, ses.UserID)
	if err != nil {
		return nil, errSessionUser
	}
	return user, nil
}

// requireWebAuth validates a session token from cookie or Authorization header
func requireWebAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := sessionTokenFromRequest(r)
		if token == "" {
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}

		user, err := loadUserForSessionToken(token)
		if err != nil {
			switch err {
			case errSessionInvalid:
				http.Error(w, "invalid session", http.StatusUnauthorized)
			case errSessionUser:
				http.Error(w, "invalid session user", http.StatusUnauthorized)
			default:
				http.Error(w, "unauthenticated", http.StatusUnauthorized)
			}
			return
		}

		ctx2 := contextWithPrincipal(r.Context(), user)
		next.ServeHTTP(w, r.WithContext(ctx2))
	}
}

func ensureInteractiveSession(w http.ResponseWriter, r *http.Request) (*storage.User, bool) {
	user, err := loadUserForSessionToken(sessionTokenFromRequest(r))
	if err != nil {
		if err != errSessionMissing {
			clearSessionCookie(w, r)
		}
		redirectToLogin(w, r)
		return nil, false
	}
	return user, true
}

func redirectToLogin(w http.ResponseWriter, r *http.Request) {
	target := "/login"
	if r.URL != nil {
		requestURI := r.URL.RequestURI()
		if requestURI != "" && r.URL.Path != "/login" {
			target = fmt.Sprintf("/login?redirect=%s", url.QueryEscape(requestURI))
		}
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	secure := requestIsHTTPS(r)
	http.SetCookie(w, &http.Cookie{
		Name:     "pm_session",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// requireRole ensures the authenticated user meets the provided role requirement.
func requireRole(minRole storage.Role, next http.HandlerFunc) http.HandlerFunc {
	return requireWebAuth(func(w http.ResponseWriter, r *http.Request) {
		principal := getPrincipal(r)
		if principal == nil || !principal.HasRole(minRole) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleAuthLogin handles local username/password login and returns a session token
func handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Username == "" || req.Password == "" {
		http.Error(w, "username and password required", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	user, err := serverStore.AuthenticateUser(ctx, req.Username, req.Password)
	if err != nil {
		// rate limit
		if authRateLimiter != nil {
			clientIP := extractIPFromAddr(r.RemoteAddr)
			authRateLimiter.RecordFailure(clientIP, req.Username)
		}
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	ses, err := createSessionCookie(w, r, user.ID)
	if err != nil {
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "token": ses.Token, "expires_at": ses.ExpiresAt.Format(time.RFC3339)})
}

func createSessionCookie(w http.ResponseWriter, r *http.Request, userID int64) (*storage.Session, error) {
	ctx := context.Background()
	ses, err := serverStore.CreateSession(ctx, userID, 60*24)
	if err != nil {
		return nil, err
	}
	secure := requestIsHTTPS(r)
	cookie := &http.Cookie{
		Name:     "pm_session",
		Value:    ses.Token,
		Path:     "/",
		HttpOnly: true,
		Expires:  ses.ExpiresAt,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(w, cookie)
	return ses, nil
}

func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		parts := strings.Split(proto, ",")
		if len(parts) > 0 {
			if strings.TrimSpace(strings.ToLower(parts[0])) == "https" {
				return true
			}
		}
	}
	if serverConfig != nil && serverConfig.Server.ProxyUseHTTPS {
		return true
	}
	return false
}

// admin helper: get user from context and ensure admin role
func getUserFromContext(r *http.Request) *storage.User {
	if v := r.Context().Value(userContextKey); v != nil {
		if u, ok := v.(*storage.User); ok {
			return u
		}
	}
	return nil
}

func getPrincipal(r *http.Request) *Principal {
	if v := r.Context().Value(principalContextKey); v != nil {
		if p, ok := v.(*Principal); ok && p != nil {
			return p
		}
	}
	if u := getUserFromContext(r); u != nil {
		return newPrincipal(u)
	}
	return nil
}

func contextWithPrincipal(ctx context.Context, user *storage.User) context.Context {
	if user == nil {
		return ctx
	}
	principal := newPrincipal(user)
	ctx = context.WithValue(ctx, userContextKey, user)
	return context.WithValue(ctx, principalContextKey, principal)
}

// handleUsers handles GET (list users) and POST (create user) for admin UI
func handleUsers(w http.ResponseWriter, r *http.Request) {
	principal := getPrincipal(r)
	if principal == nil {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	if !principal.HasRole(storage.RoleAdmin) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	ctx := context.Background()
	switch r.Method {
	case http.MethodGet:
		users, err := serverStore.ListUsers(ctx)
		if err != nil {
			http.Error(w, "failed to list users", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(users)
		return
	case http.MethodPost:
		var req struct {
			Username  string   `json:"username"`
			Password  string   `json:"password"`
			Role      string   `json:"role"`
			TenantID  string   `json:"tenant_id,omitempty"`
			TenantIDs []string `json:"tenant_ids,omitempty"`
			Email     string   `json:"email,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Username == "" || req.Password == "" {
			http.Error(w, "username and password required", http.StatusBadRequest)
			return
		}
		role := storage.NormalizeRole(req.Role)
		tenantIDs := storage.SortTenantIDs(req.TenantIDs)
		if len(tenantIDs) == 0 {
			if tid := strings.TrimSpace(req.TenantID); tid != "" {
				tenantIDs = []string{tid}
			}
		}
		u := &storage.User{
			Username:  req.Username,
			Role:      role,
			TenantIDs: tenantIDs,
			TenantID:  "",
			Email:     req.Email,
		}
		if len(tenantIDs) > 0 {
			u.TenantID = tenantIDs[0]
		}
		if err := serverStore.CreateUser(ctx, u, req.Password); err != nil {
			http.Error(w, fmt.Sprintf("failed to create user: %v", err), http.StatusInternalServerError)
			return
		}
		// Do not return password hash
		u.PasswordHash = ""
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(u)
		return
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

// handleAuthLogout removes the session token
func handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := ""
	if ah := r.Header.Get("Authorization"); ah != "" {
		parts := strings.SplitN(ah, " ", 2)
		if len(parts) == 2 && parts[0] == "Bearer" {
			token = parts[1]
		}
	}
	if token == "" {
		if c, err := r.Cookie("pm_session"); err == nil {
			token = c.Value
		}
	}
	if token == "" {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	ctx := context.Background()
	_ = serverStore.DeleteSession(ctx, token)
	// expire cookie
	cookie := &http.Cookie{
		Name:     "pm_session",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(w, cookie)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// handleAuthMe returns current user info from context
func handleAuthMe(w http.ResponseWriter, r *http.Request) {
	principal := getPrincipal(r)
	if principal == nil || principal.User == nil {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	u := principal.User
	// don't expose password hash
	out := map[string]interface{}{
		"id":         u.ID,
		"username":   u.Username,
		"role":       u.Role,
		"tenant_id":  u.TenantID,
		"tenant_ids": principal.TenantIDs,
		"created_at": u.CreatedAt.Format(time.RFC3339),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// handleUser handles single-user operations: GET, PUT, DELETE (admin only)
func handleUser(w http.ResponseWriter, r *http.Request) {
	principal := getPrincipal(r)
	if principal == nil {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	if !principal.HasRole(storage.RoleAdmin) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// Extract ID from path /api/v1/users/{id}
	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/users/")
	if idStr == "" {
		http.Error(w, "user id required", http.StatusBadRequest)
		return
	}
	id, err := strconv.ParseInt(strings.Trim(idStr, "/"), 10, 64)
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	switch r.Method {
	case http.MethodGet:
		u, err := serverStore.GetUserByID(ctx, id)
		if err != nil {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		u.PasswordHash = ""
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(u)
		return
	case http.MethodDelete:
		if err := serverStore.DeleteUser(ctx, id); err != nil {
			http.Error(w, "failed to delete user", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		return
	case http.MethodPut, http.MethodPatch:
		var req struct {
			Role      string   `json:"role"`
			TenantID  string   `json:"tenant_id"`
			TenantIDs []string `json:"tenant_ids"`
			Email     string   `json:"email"`
			Password  string   `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		u, err := serverStore.GetUserByID(ctx, id)
		if err != nil {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		if req.Role != "" {
			u.Role = storage.NormalizeRole(req.Role)
		}
		if len(req.TenantIDs) > 0 {
			u.TenantIDs = storage.SortTenantIDs(req.TenantIDs)
		} else if tid := strings.TrimSpace(req.TenantID); tid != "" {
			u.TenantIDs = []string{tid}
		} else {
			u.TenantIDs = nil
		}
		if len(u.TenantIDs) > 0 {
			u.TenantID = u.TenantIDs[0]
		} else {
			u.TenantID = ""
		}
		if req.Email != "" {
			u.Email = req.Email
		}
		if err := serverStore.UpdateUser(ctx, u); err != nil {
			http.Error(w, "failed to update user", http.StatusInternalServerError)
			return
		}
		if req.Password != "" {
			if err := serverStore.UpdateUserPassword(ctx, id, req.Password); err != nil {
				http.Error(w, "failed to update password", http.StatusInternalServerError)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(u)
		return
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

// handleListSessions returns all sessions (admin only). Optionally filter by ?user_id={id}
func handleListSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	principal := getPrincipal(r)
	if principal == nil {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	if !principal.HasRole(storage.RoleAdmin) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	ctx := context.Background()
	sessions, err := serverStore.ListSessions(ctx)
	if err != nil {
		http.Error(w, "failed to list sessions", http.StatusInternalServerError)
		return
	}
	// optional filter by user_id
	userIDStr := r.URL.Query().Get("user_id")
	if userIDStr != "" {
		if uid, err := strconv.ParseInt(userIDStr, 10, 64); err == nil {
			var f []*storage.Session
			for _, s := range sessions {
				if s.UserID == uid {
					f = append(f, s)
				}
			}
			sessions = f
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessions)
}

// handleDeleteSession deletes a session by its stored token hash (admin only)
func handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	principal := getPrincipal(r)
	if principal == nil {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	if !principal.HasRole(storage.RoleAdmin) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	key := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")
	key = strings.Trim(key, "/")
	if key == "" {
		http.Error(w, "session key required", http.StatusBadRequest)
		return
	}
	ctx := context.Background()
	if err := serverStore.DeleteSessionByHash(ctx, key); err != nil {
		http.Error(w, "failed to delete session", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// sendEmail sends a simple plain-text email using SMTP settings.
// It prefers the runtime `serverConfig.SMTP` settings (if present and enabled),
// falling back to environment variables for compatibility.
func sendEmail(to string, subject string, body string) error {
	var host, user, pass, from string
	var port int

	// Prefer settings saved in-memory (UI/settings) when enabled
	if serverConfig != nil && serverConfig.SMTP.Enabled {
		host = serverConfig.SMTP.Host
		port = serverConfig.SMTP.Port
		user = serverConfig.SMTP.User
		pass = serverConfig.SMTP.Pass
		from = serverConfig.SMTP.From
	}

	// Fallback to env vars when settings not provided
	if host == "" {
		host = os.Getenv("SMTP_HOST")
	}
	if port == 0 {
		if p := os.Getenv("SMTP_PORT"); p != "" {
			if v, err := strconv.Atoi(p); err == nil {
				port = v
			}
		}
	}
	if user == "" {
		user = os.Getenv("SMTP_USER")
	}
	if pass == "" {
		pass = os.Getenv("SMTP_PASS")
	}
	if from == "" {
		from = os.Getenv("SMTP_FROM")
	}
	if from == "" {
		from = user
	}

	if host == "" || port == 0 {
		return fmt.Errorf("SMTP not configured")
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	auth := smtp.PlainAuth("", user, pass, host)
	msg := "From: " + from + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" + body
	return smtp.SendMail(addr, auth, from, []string{to}, []byte(msg))
}

// handlePasswordResetRequest accepts {email} and sends a reset token by email (if configured)
func handlePasswordResetRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Email == "" {
		http.Error(w, "email required", http.StatusBadRequest)
		return
	}
	// rate limiting: limit requests per IP+email to prevent abuse
	clientIP := extractIPFromAddr(r.RemoteAddr)
	if authRateLimiter != nil {
		if isBlocked, _ := authRateLimiter.IsBlocked(clientIP, req.Email); isBlocked {
			http.Error(w, "Too many requests. Try again later.", http.StatusTooManyRequests)
			return
		}
		// record attempt (counts towards limit)
		authRateLimiter.RecordFailure(clientIP, req.Email)
	}

	ctx := context.Background()
	u, err := serverStore.GetUserByEmail(ctx, req.Email)
	// Always return success to avoid revealing which emails exist
	if err != nil || u == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"sent": true})
		return
	}
	token, err := serverStore.CreatePasswordResetToken(ctx, u.ID, 60)
	if err != nil {
		http.Error(w, "failed to create token", http.StatusInternalServerError)
		return
	}
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	resetURL := fmt.Sprintf("%s://%s/reset?token=%s", scheme, r.Host, token)
	body := fmt.Sprintf("You requested a password reset for %s\n\nUse the following link to reset your password (valid 60 minutes):\n\n%s\n\nIf you did not request this, ignore this message.", req.Email, resetURL)
	_ = sendEmail(req.Email, "PrintMaster Password Reset", body)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"sent": true})
}

// handlePasswordResetConfirm accepts {token, password} to reset the password
func handlePasswordResetConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Token == "" || req.Password == "" {
		http.Error(w, "token and password required", http.StatusBadRequest)
		return
	}
	ctx := context.Background()
	userID, err := serverStore.ValidatePasswordResetToken(ctx, req.Token)
	if err != nil {
		http.Error(w, "invalid or expired token", http.StatusBadRequest)
		return
	}
	if err := serverStore.UpdateUserPassword(ctx, userID, req.Password); err != nil {
		http.Error(w, "failed to reset password", http.StatusInternalServerError)
		return
	}
	// Optionally delete any other outstanding tokens for this user
	_ = serverStore.DeletePasswordResetToken(ctx, req.Token)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
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
		logError("Failed to save audit entry", "agent_id", agentID, "action", action, "error", err)
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

func setupRoutes(cfg *Config) {
	// Health check (no auth required)
	http.HandleFunc("/health", handleHealth)

	// Version info (no auth required)
	http.HandleFunc("/api/version", handleVersion)

	// Config status (protected - requires login for UI warnings)
	http.HandleFunc("/api/config/status", requireWebAuth(handleConfigStatus))

	// SSE endpoint for real-time UI updates (protected)
	http.HandleFunc("/api/events", requireWebAuth(handleSSE))
	// Backwards-compatible SSE path used by some client bundles (/events)
	http.HandleFunc("/events", requireWebAuth(handleSSE))

	// Authentication (local)
	// Login (public) - creates a session
	http.HandleFunc("/api/v1/auth/login", handleAuthLogin)
	http.HandleFunc("/api/v1/auth/options", handleAuthOptions)
	// Logout (requires valid session)
	http.HandleFunc("/api/v1/auth/logout", requireWebAuth(handleAuthLogout))
	http.HandleFunc("/api/v1/auth/me", requireWebAuth(handleAuthMe))
	// SSO / OIDC provider management (admin only inside handlers)
	http.HandleFunc("/api/v1/sso/providers", requireRole(storage.RoleAdmin, handleOIDCProviders))
	http.HandleFunc("/api/v1/sso/providers/", requireRole(storage.RoleAdmin, handleOIDCProvider))

	// User management (admin only): list and create users
	http.HandleFunc("/api/v1/users", requireRole(storage.RoleAdmin, handleUsers))
	http.HandleFunc("/api/v1/users/", requireRole(storage.RoleAdmin, handleUser))
	// Sessions management (admin): list and revoke sessions
	http.HandleFunc("/api/v1/sessions", requireRole(storage.RoleAdmin, handleListSessions))
	http.HandleFunc("/api/v1/sessions/", requireRole(storage.RoleAdmin, handleDeleteSession))

	// Password reset endpoints (public)
	http.HandleFunc("/api/v1/users/reset/request", handlePasswordResetRequest)
	http.HandleFunc("/api/v1/users/reset/confirm", handlePasswordResetConfirm)

	// UI WebSocket endpoint (for live UI liveness/status) - require login
	http.HandleFunc("/api/ws/ui", requireWebAuth(handleUIWebSocket))

	// Agent API (v1)
	http.HandleFunc("/api/v1/agents/register", handleAgentRegister) // No auth - this generates token
	http.HandleFunc("/api/v1/agents/heartbeat", requireAuth(handleAgentHeartbeat))
	http.HandleFunc("/api/v1/agents/list", requireWebAuth(handleAgentsList)) // List all agents (for UI)
	http.HandleFunc("/api/v1/agents/", requireWebAuth(handleAgentDetails))   // Get single agent details (for UI)
	// Agent WebSocket channel uses its own token handshake; do not require UI auth here.
	http.HandleFunc("/api/v1/agents/ws", func(w http.ResponseWriter, r *http.Request) { // WebSocket endpoint
		handleAgentWebSocket(w, r, serverStore)
	})

	// Tenancy & join-token routes. The register-with-token path must remain
	// available even if admins disable tenancy, so register routes always and
	// let the package guard admin handlers via SetEnabled.
	featureEnabled := true
	if cfg != nil {
		featureEnabled = cfg.Tenancy.Enabled
	}
	tenancy.AuthMiddleware = requireWebAuth
	tenancy.SetServerVersion(Version)
	tenancy.SetEnabled(featureEnabled)
	tenancy.SetAgentEventSink(func(eventType string, data map[string]interface{}) {
		sseHub.Broadcast(SSEEvent{Type: eventType, Data: data})
	})
	tenancy.RegisterRoutes(serverStore)
	logInfo("Tenancy routes registered", "enabled", featureEnabled)

	// Proxy endpoints - require login
	http.HandleFunc("/api/v1/proxy/agent/", requireWebAuth(handleAgentProxy))   // Proxy to agent's own web UI
	http.HandleFunc("/api/v1/proxy/device/", requireWebAuth(handleDeviceProxy)) // Proxy to device web UI through agent
	http.HandleFunc("/proxy/", requireWebAuth(handleLegacyDeviceProxy))         // Legacy compatibility for shared UI links

	// Public OIDC endpoints
	http.HandleFunc("/auth/oidc/start/", handleOIDCStart)
	http.HandleFunc("/auth/oidc/callback", handleOIDCCallback)

	http.HandleFunc("/api/v1/devices/batch", requireAuth(handleDevicesBatch))
	http.HandleFunc("/api/v1/devices/list", requireWebAuth(handleDevicesList)) // List all devices (for UI)
	http.HandleFunc("/api/v1/metrics/batch", requireAuth(handleMetricsBatch))

	// Web UI endpoints - keep landing/static public so login assets load
	http.HandleFunc("/", handleWebUI)
	http.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		// Serve the dedicated login page from embedded web assets
		content, err := webFS.ReadFile("web/login.html")
		if err != nil {
			logWarn("Login page not found", "err", err)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(content)
	})
	http.HandleFunc("/static/", handleStatic)

	// UI metrics summary endpoint (protected)
	http.HandleFunc("/api/metrics", requireWebAuth(handleMetricsSummary))

	// Serve device metrics history from server DB. If the server has historical
	// metrics stored (uploaded by agents) this endpoint will return them. The
	// endpoint supports the same query parameters as the agent: `serial` plus
	// either `since` (RFC3339) or `period` (day|week|month|year). Default period
	// is `week` when nothing is supplied.
	http.HandleFunc("/api/devices/metrics/history", requireWebAuth(handleMetricsHistory))

	// Minimal settings & logs endpoints for the UI (placeholders)
	http.HandleFunc("/api/settings", requireWebAuth(handleSettings))
	http.HandleFunc("/api/settings/test-email", requireWebAuth(handleSettingsTestEmail))
	http.HandleFunc("/api/logs", requireWebAuth(handleLogs))
	http.HandleFunc("/api/audit/logs", requireRole(storage.RoleAdmin, handleAuditLogs))

	// Provide a lightweight proxy/compat endpoint for web UI credentials so the
	// server UI doesn't 404 when the shared cards call /device/webui-credentials.
	// If the server does not have credentials for the device, respond with
	// exists:false — agent UIs will use their own endpoint.
	http.HandleFunc("/device/webui-credentials", requireWebAuth(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			// Query param: serial
			serial := r.URL.Query().Get("serial")
			if serial == "" {
				w.WriteHeader(http.StatusBadRequest)
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"error":"serial required"}`))
				return
			}
			// Server does not centrally store per-device webui creds yet; return exists:false
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"exists": false}`))
			return
		case http.MethodPost:
			// For now, accept and acknowledge but do not persist
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"success": false, "message": "server cannot store credentials"}`))
			return
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	}))
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
	})
}

// handleSSE streams server-sent events to UI clients for real-time updates
func handleSSE(w http.ResponseWriter, r *http.Request) {
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

	logDebug("SSE client connected", "client_id", client.id)

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
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
			flusher.Flush()

		case <-r.Context().Done():
			// Client disconnected
			logDebug("SSE client disconnected", "client_id", client.id)
			return
		}
	}
}

// handleUIWebSocket upgrades a browser UI connection to WebSocket and forwards
// SSE hub events to the connected UI client. This provides a low-latency
// liveness/status channel for the web UI.
func handleUIWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade HTTP to WS using shared wrapper
	conn, err := wscommon.UpgradeHTTP(w, r)
	if err != nil {
		logWarn("Failed to upgrade UI WebSocket", "error", err)
		return
	}

	// Register with wsHub to receive bridged events from the SSE hub
	clientID := fmt.Sprintf("ui-%d", time.Now().UnixNano())
	ch := make(chan wscommon.Message, 20)
	wsHub.Register(clientID, ch)
	defer wsHub.Unregister(clientID)

	// Send initial version message
	versionMsg := wscommon.Message{
		Type: "version",
		Data: map[string]interface{}{
			"version":    Version,
			"build_time": BuildTime,
			"git_commit": GitCommit,
		},
		Timestamp: time.Now(),
	}
	if payload, jerr := versionMsg.Marshal(); jerr == nil {
		_ = conn.WriteRaw(payload, 5*time.Second)
	}

	// Forward hub events (from wsHub) to WS
	done := make(chan struct{})
	go func() {
		for {
			select {
			case ev, ok := <-ch:
				if !ok {
					return
				}
				if b, err := ev.Marshal(); err == nil {
					if err := conn.WriteRaw(b, 10*time.Second); err != nil {
						return
					}
				}
			case <-done:
				return
			}
		}
	}()

	// Keepalive pings to prevent idle connection termination by proxies/load-balancers.
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()
	go func() {
		for {
			select {
			case <-pingTicker.C:
				// Send a ping; if it fails, the read loop will notice and connection will close.
				_ = conn.WritePing(5 * time.Second)
			case <-done:
				return
			}
		}
	}()

	// Simple read loop to detect client disconnects. We don't expect inbound messages.
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		if _, err := conn.ReadMessage(); err != nil {
			// If the read fails (client disconnect or network error), exit read loop.
			break
		}
	}

	close(done)
	conn.Close()
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

func handleConfigStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Build list of searched config paths based on run mode
	var searchedPaths []string
	isService := !service.Interactive()

	if isService {
		// Running as service - only ProgramData locations
		programData := os.Getenv("PROGRAMDATA")
		if programData == "" {
			programData = "C:\\ProgramData"
		}
		searchedPaths = append(searchedPaths, filepath.Join(programData, "PrintMaster", "server", "config.toml"))
		searchedPaths = append(searchedPaths, filepath.Join(programData, "PrintMaster", "config.toml"))
	} else {
		// Running interactively - only local locations
		exePath, err := os.Executable()
		if err == nil {
			exeDir := filepath.Dir(exePath)
			searchedPaths = append(searchedPaths, filepath.Join(exeDir, "config.toml"))
		}
		searchedPaths = append(searchedPaths, "config.toml")
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"using_defaults": usingDefaultConfig,
		"errors":         configLoadErrors,
		"searched_paths": searchedPaths,
		"loaded_from":    loadedConfigPath,
		"is_service":     isService,
	})
}

// Agent registration - first contact from a new agent
func handleAgentRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	// The open /api/v1/agents/register endpoint previously allowed unauthenticated
	// registration and returned a credential. For security we disallow that path
	// and require agents to use the token-based onboarding flow: POST
	// /api/v1/agents/register-with-token with a valid join token. This prevents
	// accidental or unauthenticated agent registration.

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":   "agent registration via /api/v1/agents/register is disabled",
		"message": "Use POST /api/v1/agents/register-with-token with a valid join token",
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
	agent := r.Context().Value(agentContextKey).(*storage.Agent)

	// Update agent last_seen
	ctx := context.Background()
	if err := serverStore.UpdateAgentHeartbeat(ctx, agent.AgentID, req.Status); err != nil {
		logWarn("Failed to update heartbeat", "agent_id", agent.AgentID, "error", err)
		// Don't fail the request, just log it
	}

	// Log audit entry for heartbeat (only occasionally to reduce log volume)
	// Could add logic here to only log every Nth heartbeat
	clientIP := extractClientIP(r)
	logAuditEntry(ctx, agent.AgentID, "heartbeat", fmt.Sprintf("Status: %s", req.Status), clientIP)

	// Broadcast agent_heartbeat event to UI via SSE
	sseHub.Broadcast(SSEEvent{
		Type: "agent_heartbeat",
		Data: map[string]interface{}{
			"agent_id": agent.AgentID,
			"status":   req.Status,
		},
	})

	logDebug("Heartbeat received", "agent_id", agent.AgentID, "status", req.Status)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

// List all agents - for UI display
func handleAgentsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}

	principal := getPrincipal(r)
	if principal == nil {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	scope, ok := tenantScope(principal)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	ctx := context.Background()
	agents, err := serverStore.ListAgents(ctx)
	if err != nil {
		logError("Failed to list agents", "error", err)
		http.Error(w, "Failed to list agents", http.StatusInternalServerError)
		return
	}
	// If scoped, filter agents to only those the user may access
	if scope != nil {
		filtered := make([]*storage.Agent, 0, len(agents))
		for _, a := range agents {
			if a == nil {
				continue
			}
			if tenantAllowed(scope, a.TenantID) {
				filtered = append(filtered, a)
			}
		}
		agents = filtered
	}

	// Build response objects with a derived connection_type field
	type agentView struct {
		*storage.Agent
		ConnectionType string `json:"connection_type"`
	}

	var resp []agentView
	// Determine connection type using live WS map and last_seen recency
	const httpRecencyThreshold = 90 * time.Second
	for _, agent := range agents {
		agent.Token = "" // Don't expose tokens to UI

		connType := "none"
		if isAgentConnectedWS(agent.AgentID) {
			connType = "ws"
		} else if time.Since(agent.LastSeen) <= httpRecencyThreshold {
			connType = "http"
		}

		resp = append(resp, agentView{Agent: agent, ConnectionType: connType})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Get agent details by ID - for UI display (no auth required for now)
// Get agent details and perform management operations scoped by role/tenant.
func handleAgentDetails(w http.ResponseWriter, r *http.Request) {
	// Extract agent ID from URL path: /api/v1/agents/{agentID}
	path := r.URL.Path
	agentID := strings.TrimPrefix(path, "/api/v1/agents/")
	if agentID == "" || agentID == path {
		http.Error(w, "Agent ID required", http.StatusBadRequest)
		return
	}

	principal := getPrincipal(r)
	if principal == nil {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	scope, ok := tenantScope(principal)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet && !principal.HasRole(storage.RoleOperator) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	ctx := context.Background()
	switch r.Method { //nolint:exhaustive
	case http.MethodGet:
		{
			agent, err := serverStore.GetAgent(ctx, agentID)
			if err != nil {
				logError("Failed to get agent", "agent_id", agentID, "error", err)
				http.Error(w, "Agent not found", http.StatusNotFound)
				return
			}

			if !tenantAllowed(scope, agent.TenantID) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}

			// Get device count for this agent
			devices, err := serverStore.ListDevices(ctx, agentID)
			if err == nil {
				agent.DeviceCount = len(devices)
			}

			// Remove sensitive token from response
			agent.Token = ""

			// Include WS diagnostic counters (per-agent) in the response
			var pf int64
			var de int64
			wsDiagLock.RLock()
			pf = wsPingFailuresPerAgent[agent.AgentID]
			de = wsDisconnectEventsPerAgent[agent.AgentID]
			wsDiagLock.RUnlock()

			// Convert agent to a generic map so we can add extra fields without changing storage.Agent
			var obj map[string]interface{}
			buf, _ := json.Marshal(agent)
			_ = json.Unmarshal(buf, &obj)
			obj["ws_ping_failures"] = pf
			obj["ws_disconnect_events"] = de

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(obj)
			return
		}

	case http.MethodPost:
		{
			// Allow updating mutable agent fields (currently only 'name') from the UI
			var req struct {
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "Invalid JSON", http.StatusBadRequest)
				return
			}
			// Validate name length (basic)
			if len(req.Name) > 512 {
				http.Error(w, "Name too long", http.StatusBadRequest)
				return
			}

			agent, err := serverStore.GetAgent(ctx, agentID)
			if err != nil {
				logError("Failed to load agent for update", "agent_id", agentID, "error", err)
				http.Error(w, "Agent not found", http.StatusNotFound)
				return
			}
			if !tenantAllowed(scope, agent.TenantID) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if err := serverStore.UpdateAgentName(ctx, agentID, req.Name); err != nil {
				logError("Failed to update agent name", "agent_id", agentID, "error", err)
				http.Error(w, "Failed to update agent", http.StatusInternalServerError)
				return
			}

			// Return updated agent object (same shape as GET)
			agent, err = serverStore.GetAgent(ctx, agentID)
			if err != nil {
				logError("Failed to get agent after update", "agent_id", agentID, "error", err)
				http.Error(w, "Agent not found", http.StatusNotFound)
				return
			}

			// Remove sensitive token from response
			agent.Token = ""

			// Include WS diagnostic counters
			var pf int64
			var de int64
			wsDiagLock.RLock()
			pf = wsPingFailuresPerAgent[agent.AgentID]
			de = wsDisconnectEventsPerAgent[agent.AgentID]
			wsDiagLock.RUnlock()

			var obj map[string]interface{}
			buf, _ := json.Marshal(agent)
			_ = json.Unmarshal(buf, &obj)
			obj["ws_ping_failures"] = pf
			obj["ws_disconnect_events"] = de

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(obj)
			return
		}

	case http.MethodDelete:
		{
			agent, err := serverStore.GetAgent(ctx, agentID)
			if err != nil {
				logError("Failed to get agent before delete", "agent_id", agentID, "error", err)
				if errors.Is(err, sql.ErrNoRows) || strings.Contains(strings.ToLower(err.Error()), "not found") {
					http.Error(w, "Agent not found", http.StatusNotFound)
				} else {
					http.Error(w, "Failed to delete agent", http.StatusInternalServerError)
				}
				return
			}
			if !tenantAllowed(scope, agent.TenantID) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			// Delete agent and all associated data
			err = serverStore.DeleteAgent(ctx, agentID)
			if err != nil {
				logError("Failed to delete agent", "agent_id", agentID, "error", err)
				if err.Error() == "agent not found" {
					http.Error(w, "Agent not found", http.StatusNotFound)
				} else {
					http.Error(w, "Failed to delete agent", http.StatusInternalServerError)
				}
				return
			}

			// Close WebSocket connection if active
			closeAgentWebSocket(agentID)

			logInfo("Agent deleted", "agent_id", agentID)

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"message": "Agent deleted successfully",
			})
			return
		}

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAgentProxy proxies HTTP requests to the agent's own web UI through WebSocket
func handleAgentProxy(w http.ResponseWriter, r *http.Request) {
	// Extract agent ID from path: /api/v1/proxy/agent/{agentID}/{path...}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/proxy/agent/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 1 || parts[0] == "" {
		http.Error(w, "Agent ID required", http.StatusBadRequest)
		return
	}

	principal := getPrincipal(r)
	if principal == nil {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	scope, ok := tenantScope(principal)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	agentID := parts[0]
	targetPath := "/"
	if len(parts) > 1 {
		targetPath = "/" + parts[1]
	}

	// Add query string if present
	if r.URL.RawQuery != "" {
		targetPath += "?" + r.URL.RawQuery
	}

	// Check if agent is connected via WebSocket
	if !isAgentConnectedWS(agentID) {
		http.Error(w, "Agent not connected via WebSocket", http.StatusServiceUnavailable)
		return
	}

	// Get agent to determine local port (default 8080)
	ctx := context.Background()
	agent, err := serverStore.GetAgent(ctx, agentID)
	if err != nil {
		http.Error(w, "Agent not found", http.StatusNotFound)
		return
	}
	if !tenantAllowed(scope, agent.TenantID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// Build target URL for agent's local web UI
	// Agents typically run on http://localhost:8080
	targetURL := fmt.Sprintf("http://localhost:8080%s", targetPath)

	// TODO: Could add web_ui_port to agent metadata if needed

	// Proxy the request through WebSocket
	proxyThroughWebSocket(w, r, agentID, targetURL)
}

// handleDeviceProxy proxies HTTP requests to device web UIs through agent WebSocket
func handleDeviceProxy(w http.ResponseWriter, r *http.Request) {
	serial, targetPath, err := parseDeviceProxyPath(r.URL.Path, "/api/v1/proxy/device/")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	targetPath = appendQueryToPath(targetPath, r.URL.RawQuery)
	proxyDeviceRequest(w, r, serial, targetPath)
}

// handleLegacyDeviceProxy keeps historical /proxy/{serial}/ URLs working by routing
// them through the same device proxy implementation as the modern API endpoint.
func handleLegacyDeviceProxy(w http.ResponseWriter, r *http.Request) {
	serial, targetPath, err := parseDeviceProxyPath(r.URL.Path, "/proxy/")
	if err != nil {
		http.NotFound(w, r)
		return
	}

	targetPath = appendQueryToPath(targetPath, r.URL.RawQuery)
	proxyDeviceRequest(w, r, serial, targetPath)
}

func proxyDeviceRequest(w http.ResponseWriter, r *http.Request, serial string, targetPath string) {
	principal := getPrincipal(r)
	if principal == nil {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	scope, ok := tenantScope(principal)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	ctx := r.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	logInfo("Device proxy request", "serial", serial, "target_path", targetPath)

	device, err := serverStore.GetDevice(ctx, serial)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || strings.Contains(strings.ToLower(err.Error()), "not found") {
			logWarn("Device proxy lookup miss", "serial", serial)
			http.Error(w, "Device not found", http.StatusNotFound)
			return
		}

		logError("Failed to load device for proxy", "serial", serial, "error", err)
		http.Error(w, "Failed to query devices", http.StatusInternalServerError)
		return
	}
	if device == nil {
		logWarn("Device proxy lookup returned nil", "serial", serial)
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}

	if device.IP == "" {
		logWarn("Device proxy missing IP", "serial", serial)
		http.Error(w, "Device has no IP address", http.StatusBadRequest)
		return
	}

	if device.AgentID == "" {
		logWarn("Device proxy missing agent", "serial", serial)
		http.Error(w, "Device has no associated agent", http.StatusBadRequest)
		return
	}

	if !isAgentConnectedWS(device.AgentID) {
		logWarn("Device proxy agent offline", "serial", serial, "agent_id", device.AgentID)
		http.Error(w, "Device's agent not connected via WebSocket", http.StatusServiceUnavailable)
		return
	}

	agent, err := serverStore.GetAgent(ctx, device.AgentID)
	if err != nil {
		logWarn("Device proxy agent lookup failed", "serial", serial, "agent_id", device.AgentID, "error", err)
		http.Error(w, "Agent not found", http.StatusNotFound)
		return
	}
	if !tenantAllowed(scope, agent.TenantID) {
		logWarn("Device proxy forbidden", "serial", serial, "agent_id", device.AgentID, "tenant_id", agent.TenantID)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	targetURL := fmt.Sprintf("http://%s%s", device.IP, targetPath)
	proxyThroughWebSocket(w, r, device.AgentID, targetURL)
}

func parseDeviceProxyPath(fullPath, prefix string) (string, string, error) {
	if !strings.HasPrefix(fullPath, prefix) {
		return "", "", fmt.Errorf("invalid proxy prefix")
	}

	trimmed := strings.TrimPrefix(fullPath, prefix)
	trimmed = strings.TrimLeft(trimmed, "/")
	if strings.HasPrefix(trimmed, "device/") {
		trimmed = strings.TrimPrefix(trimmed, "device/")
		trimmed = strings.TrimLeft(trimmed, "/")
	}
	if trimmed == "" {
		return "", "", fmt.Errorf("device serial required")
	}

	parts := strings.SplitN(trimmed, "/", 2)
	serial := parts[0]
	targetPath := "/"
	if len(parts) > 1 && parts[1] != "" {
		targetPath = "/" + parts[1]
	}

	return serial, targetPath, nil
}

func appendQueryToPath(path, rawQuery string) string {
	if rawQuery == "" {
		return path
	}
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return path + separator + rawQuery
}

// proxyThroughWebSocket sends an HTTP request through WebSocket and returns the response
func proxyThroughWebSocket(w http.ResponseWriter, r *http.Request, agentID string, targetURL string) {
	// Generate unique request ID
	requestID := fmt.Sprintf("%s-%d", agentID, time.Now().UnixNano())
	start := time.Now()

	// Create response channel
	respChan := make(chan wscommon.Message, 1)

	// Register the channel for this request
	proxyRequestsLock.Lock()
	proxyRequests[requestID] = respChan
	proxyRequestsLock.Unlock()

	// Clean up on exit
	defer func() {
		proxyRequestsLock.Lock()
		delete(proxyRequests, requestID)
		proxyRequestsLock.Unlock()
		close(respChan)
	}()

	// Read request body if present
	var bodyStr string
	if r.Body != nil {
		bodyBytes, err := io.ReadAll(r.Body)
		if err == nil {
			bodyStr = base64.StdEncoding.EncodeToString(bodyBytes)
		}
	}

	// Extract headers
	headers := make(map[string]string)
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	logInfo("Proxy request dispatched",
		"agent_id", agentID,
		"request_id", requestID,
		"method", r.Method,
		"url", targetURL)

	// Send proxy request to agent via WebSocket
	if err := sendProxyRequest(agentID, requestID, targetURL, r.Method, headers, bodyStr); err != nil {
		logError("Failed to send proxy request",
			"agent_id", agentID,
			"request_id", requestID,
			"url", targetURL,
			"error", err)
		http.Error(w, "Failed to send proxy request", http.StatusInternalServerError)
		return
	}

	// Wait for response with timeout
	select {
	case resp := <-respChan:
		duration := time.Since(start)
		// Got response from agent
		statusCode := 200
		if code, ok := resp.Data["status_code"].(float64); ok {
			statusCode = int(code)
		}

		logInfo("Proxy response received",
			"agent_id", agentID,
			"request_id", requestID,
			"status", statusCode,
			"url", targetURL,
			"duration_ms", duration.Milliseconds())
		if duration > 5*time.Second {
			logWarn("Proxy response slow",
				"agent_id", agentID,
				"request_id", requestID,
				"url", targetURL,
				"duration_ms", duration.Milliseconds())
		}

		// Set response headers
		if respHeaders, ok := resp.Data["headers"].(map[string]interface{}); ok {
			for k, v := range respHeaders {
				if vStr, ok := v.(string); ok {
					w.Header().Set(k, vStr)
				}
			}
		}

		// Add custom header to indicate this is a proxied response
		// Agent UI can check for this header and disable device proxy buttons
		w.Header().Set("X-PrintMaster-Proxied", "true")
		w.Header().Set("X-PrintMaster-Agent-ID", agentID)

		// Ensure Content-Type is sensible for static assets. Some agent-side
		// servers may omit or return a generic "text/plain" Content-Type which
		// combined with X-Content-Type-Options: nosniff (set by server) will
		// cause browsers to block CSS/JS. If Content-Type looks generic or is
		// missing, try to infer from the target URL extension or sniff the body
		// bytes (below) and override the header so browsers accept the response.
		contentType := w.Header().Get("Content-Type")
		if contentType == "" || strings.HasPrefix(strings.ToLower(contentType), "text/plain") {
			// Try extension-based lookup
			if u, err := url.Parse(targetURL); err == nil {
				ext := strings.ToLower(path.Ext(u.Path))
				if ext != "" {
					if mt := mime.TypeByExtension(ext); mt != "" {
						w.Header().Set("Content-Type", mt)
						contentType = mt
					} else {
						// Fallback common mappings when mime.TypeByExtension fails
						switch ext {
						case ".js":
							w.Header().Set("Content-Type", "application/javascript")
							contentType = "application/javascript"
						case ".css":
							w.Header().Set("Content-Type", "text/css")
							contentType = "text/css"
						case ".json":
							w.Header().Set("Content-Type", "application/json")
							contentType = "application/json"
						case ".wasm":
							w.Header().Set("Content-Type", "application/wasm")
							contentType = "application/wasm"
						case ".svg":
							w.Header().Set("Content-Type", "image/svg+xml")
							contentType = "image/svg+xml"
						default:
							// leave as-is
						}
					}
				}
			}
		}

		// Remove server-level security headers that would block proxied agent content
		// The agent side already strips or rewrites CSP/X-Frame headers; remove them here
		// so the browser receives the agent-provided headers (or none) and the UI can load
		// external scripts/styles (for example, CDN-hosted flatpickr) when proxied.
		w.Header().Del("Content-Security-Policy")
		w.Header().Del("X-Frame-Options")

		w.WriteHeader(statusCode)

		// Write response body
		if bodyB64, ok := resp.Data["body"].(string); ok {
			bodyBytes, err := base64.StdEncoding.DecodeString(bodyB64)
			if err == nil {
				// If this is HTML, inject minimal proxy metadata so the agent bundle
				// can detect proxied mode and rewrite URLs itself. Handle gzip-compressed
				// responses from agents by decompressing, transforming, and recompressing.
				contentType := w.Header().Get("Content-Type")
				if strings.Contains(strings.ToLower(contentType), "text/html") {
					// Determine proxy base from the incoming request path so injected
					// <base> and runtime rewrites point to the same proxied prefix
					// (e.g. /api/v1/proxy/agent/{id}/ or /api/v1/proxy/device/{serial}/).
					proxyBase := computeProxyBaseFromRequest(r)

					// Detect gzip by magic bytes
					if len(bodyBytes) >= 2 && bodyBytes[0] == 0x1f && bodyBytes[1] == 0x8b {
						// Decompress
						gr, gerr := gzip.NewReader(bytes.NewReader(bodyBytes))
						if gerr == nil {
							decompressed, rerr := io.ReadAll(gr)
							_ = gr.Close()
							if rerr == nil {
								// Inject into decompressed HTML
								transformed := injectProxyMetaAndBase(decompressed, proxyBase, agentID, targetURL)
								// Recompress
								var buf bytes.Buffer
								gw := gzip.NewWriter(&buf)
								if _, werr := gw.Write(transformed); werr == nil {
									_ = gw.Close()
									bodyBytes = buf.Bytes()
									// Ensure Content-Encoding remains gzip
									w.Header().Set("Content-Encoding", "gzip")
									w.Header().Set("Content-Length", fmt.Sprintf("%d", len(bodyBytes)))
								} else {
									// If recompress fails, fall back to sending decompressed HTML and remove Content-Encoding
									_ = gw.Close()
									w.Header().Del("Content-Encoding")
									bodyBytes = transformed
								}
							}
						}
					} else {
						bodyBytes = injectProxyMetaAndBase(bodyBytes, proxyBase, agentID, targetURL)
					}
				}

				// For JavaScript (and similar) responses, rewrite common absolute
				// fetch/XHR occurrences so they route through the proxy. Also handle
				// gzip-compressed JS responses by decompressing/recompressing.
				ctLower := strings.ToLower(contentType)
				if strings.Contains(ctLower, "javascript") || strings.Contains(ctLower, "application/json") {
					// Compute proxy base so JS rewrites use the same prefix the client used
					proxyBase := computeProxyBaseFromRequest(r)
					// Detect gzip by magic bytes
					if len(bodyBytes) >= 2 && bodyBytes[0] == 0x1f && bodyBytes[1] == 0x8b {
						gr, gerr := gzip.NewReader(bytes.NewReader(bodyBytes))
						if gerr == nil {
							decompressed, rerr := io.ReadAll(gr)
							_ = gr.Close()
							if rerr == nil {
								transformed := rewriteProxyJS(decompressed, proxyBase, targetURL)
								// Recompress
								var buf bytes.Buffer
								gw := gzip.NewWriter(&buf)
								if _, werr := gw.Write(transformed); werr == nil {
									_ = gw.Close()
									bodyBytes = buf.Bytes()
									w.Header().Set("Content-Encoding", "gzip")
									w.Header().Set("Content-Length", fmt.Sprintf("%d", len(bodyBytes)))
								} else {
									_ = gw.Close()
									w.Header().Del("Content-Encoding")
									bodyBytes = transformed
								}
							}
						}
					} else {
						// Non-gzip JS: transform in-place
						proxyBase := computeProxyBaseFromRequest(r)
						bodyBytes = rewriteProxyJS(bodyBytes, proxyBase, targetURL)
					}
				}

				w.Write(bodyBytes)
			}
		}

	case <-time.After(30 * time.Second):
		duration := time.Since(start)
		http.Error(w, "Proxy request timeout", http.StatusGatewayTimeout)
		logWarn("Proxy request timeout",
			"agent_id", agentID,
			"request_id", requestID,
			"url", targetURL,
			"duration_ms", duration.Milliseconds())
	}
}

// injectProxyMetaAndBase inserts proxy meta tags and a <base> element into HTML
// and rewrites absolute occurrences of the agent origin to the proxy base.
func injectProxyMetaAndBase(body []byte, proxyBase string, agentID string, targetURL string) []byte {
	bodyStr := string(body)

	// Replace absolute origin occurrences (http(s)://host:port) and protocol-relative //host:port
	if u, err := url.Parse(targetURL); err == nil {
		origin := u.Scheme + "://" + u.Host
		bodyStr = strings.ReplaceAll(bodyStr, origin, proxyBase)
		protoRel := "//" + u.Host
		bodyStr = strings.ReplaceAll(bodyStr, protoRel, proxyBase)
	}

	// Rewrite common root-absolute attributes so they route through the proxy.
	// Do this before injecting our own <base> so we don't accidentally rewrite the
	// base href we add (which would cause duplicated proxy prefixes).
	bodyStr = strings.ReplaceAll(bodyStr, `src="/`, "src=\""+proxyBase)
	bodyStr = strings.ReplaceAll(bodyStr, `src='/`, "src='"+proxyBase)
	bodyStr = strings.ReplaceAll(bodyStr, `href="/`, "href=\""+proxyBase)
	bodyStr = strings.ReplaceAll(bodyStr, `href='/`, "href='"+proxyBase)
	bodyStr = strings.ReplaceAll(bodyStr, `action="/`, "action=\""+proxyBase)
	bodyStr = strings.ReplaceAll(bodyStr, `action='/`, "action='"+proxyBase)
	bodyStr = strings.ReplaceAll(bodyStr, `data-src="/`, "data-src=\""+proxyBase)
	bodyStr = strings.ReplaceAll(bodyStr, `data-src='/`, "data-src='"+proxyBase)
	// Inline CSS url() patterns
	bodyStr = strings.ReplaceAll(bodyStr, `url("/`, "url(\""+proxyBase)
	bodyStr = strings.ReplaceAll(bodyStr, `url('/`, "url('"+proxyBase)

	// Rewrite common JavaScript absolute calls so they route through the proxy.
	// This covers fetch('/...') and common XHR/open patterns which are not
	// affected by a <base> element and must be rewritten manually.
	bodyStr = strings.ReplaceAll(bodyStr, "fetch('/", "fetch('"+proxyBase)
	bodyStr = strings.ReplaceAll(bodyStr, "fetch(\"/", "fetch(\""+proxyBase)
	bodyStr = strings.ReplaceAll(bodyStr, "fetch( '/", "fetch( '"+proxyBase)
	bodyStr = strings.ReplaceAll(bodyStr, "fetch( \"/", "fetch( \""+proxyBase)

	// XMLHttpRequest / open() variants (common patterns)
	bodyStr = strings.ReplaceAll(bodyStr, "XMLHttpRequest.open('GET','/", "XMLHttpRequest.open('GET','"+proxyBase)
	bodyStr = strings.ReplaceAll(bodyStr, "XMLHttpRequest.open(\"GET\",\"/", "XMLHttpRequest.open(\"GET\",\""+proxyBase)
	bodyStr = strings.ReplaceAll(bodyStr, "open('GET','/", "open('GET','"+proxyBase)
	bodyStr = strings.ReplaceAll(bodyStr, "open(\"GET\",\"/", "open(\"GET\",\""+proxyBase)

	// Inject meta tag after <head> tag (do this last so injected base won't be rewritten above)
	headIdx := strings.Index(strings.ToLower(bodyStr), "<head>")
	if headIdx == -1 {
		return []byte(bodyStr)
	}
	insertPos := headIdx + len("<head>")
	// Small runtime shim injected into proxied HTML to rewrite root-absolute
	// fetch/XHR requests at runtime. This is more robust than string-only
	// rewrites because scripts can construct URLs dynamically (e.g. using
	// window.location.origin). The shim prepends the proxy base for any URL
	// that starts with '/'. Keep this small and defensive.
	// Runtime shim: only rewrite root-absolute URLs that are NOT already
	// prefixed with the proxy base. This avoids double-prefixing when the
	// server rewrites script literals (which produce URLs that start with
	// the proxy base) and the runtime shim then prepends the proxy base
	// again.
	script := `<script>(function(){try{var _pb="` + proxyBase + `";var _f=window.fetch;window.fetch=function(input,init){try{var u=typeof input==='string'?input:input&&input.url; if(typeof u==='string'){ if(!(u.indexOf(_pb)===0) && u.charAt(0)==='/'){ if(typeof input==='string') input=_pb+u.slice(1); else input=new Request(_pb+u.slice(1),input); } } }catch(e){} return _f.apply(this,arguments)};var _open=XMLHttpRequest.prototype.open;XMLHttpRequest.prototype.open=function(method,url){try{ if(typeof url==='string'){ if(!(url.indexOf(_pb)===0) && url.charAt(0)==='/'){ url=_pb+url.slice(1); } } }catch(e){} return _open.apply(this,arguments);} }catch(e){} })();</script>`

	// Inject meta tags and <base>. Note: globalSettings initialization is
	// provided via shared client script (common/web/shared.js) to keep
	// client-side globals centralized.
	metaTag := `<meta http-equiv="X-PrintMaster-Proxied" content="true"><meta http-equiv="X-PrintMaster-Agent-ID" content="` + agentID + `">` +
		`<base href="` + proxyBase + `">` + script

	bodyStr = bodyStr[:insertPos] + metaTag + bodyStr[insertPos:]

	return []byte(bodyStr)
}

// computeProxyBaseFromRequest derives the proxy base prefix used by the
// incoming request (e.g. /api/v1/proxy/agent/{id}/ or /api/v1/proxy/device/{serial}/)
// so injected <base> and runtime rewrites point to the same proxied prefix.
func computeProxyBaseFromRequest(r *http.Request) string {
	path := r.URL.Path
	prefix := "/api/v1/proxy/"
	idx := strings.Index(path, prefix)
	if idx == -1 {
		// fallback to root
		if strings.HasSuffix(path, "/") {
			return path
		}
		return path + "/"
	}
	rest := path[idx+len(prefix):]
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) >= 2 {
		return prefix + parts[0] + "/" + parts[1] + "/"
	}
	// Fallback: ensure trailing slash
	if !strings.HasSuffix(path, "/") {
		return path + "/"
	}
	return path
}

// rewriteProxyJS performs simple string-based rewrites on JavaScript/JSON
// payloads to convert absolute paths (e.g., fetch('/scan_metrics')) to the
// proxy-based path. This is a pragmatic approach and not a full JS parser.
func rewriteProxyJS(body []byte, proxyBase string, targetURL string) []byte {
	s := string(body)

	// Replace absolute origin occurrences
	if u, err := url.Parse(targetURL); err == nil {
		origin := u.Scheme + "://" + u.Host
		s = strings.ReplaceAll(s, origin, proxyBase)
		protoRel := "//" + u.Host
		s = strings.ReplaceAll(s, protoRel, proxyBase)
	}

	// JS fetch() common patterns
	s = strings.ReplaceAll(s, "fetch('/", "fetch('"+proxyBase)
	s = strings.ReplaceAll(s, "fetch(\"/", "fetch(\""+proxyBase)
	s = strings.ReplaceAll(s, "fetch( '/", "fetch( '"+proxyBase)
	s = strings.ReplaceAll(s, "fetch( \"/", "fetch( \""+proxyBase)

	// XHR / open patterns
	s = strings.ReplaceAll(s, "XMLHttpRequest.open('GET','/", "XMLHttpRequest.open('GET','"+proxyBase)
	s = strings.ReplaceAll(s, "XMLHttpRequest.open(\"GET\",\"/", "XMLHttpRequest.open(\"GET\",\""+proxyBase)
	s = strings.ReplaceAll(s, "open('GET','/", "open('GET','"+proxyBase)
	s = strings.ReplaceAll(s, "open(\"GET\",\"/", "open(\"GET\",\""+proxyBase)

	// Common API endpoints — rewrite simple literal occurrences
	s = strings.ReplaceAll(s, "'/settings'", "'"+proxyBase+"settings'")
	s = strings.ReplaceAll(s, "\"/settings\"", "\""+proxyBase+"settings\"")
	s = strings.ReplaceAll(s, "'/devices/list'", "'"+proxyBase+"devices/list'")
	s = strings.ReplaceAll(s, "\"/devices/list\"", "\""+proxyBase+"devices/list\"")
	s = strings.ReplaceAll(s, "'/scan_metrics'", "'"+proxyBase+"scan_metrics'")
	s = strings.ReplaceAll(s, "\"/scan_metrics\"", "\""+proxyBase+"scan_metrics\"")

	return []byte(s)
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
		logWarn("Invalid JSON in devices batch", "error", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	logInfo("Devices batch received", "agent_id", req.AgentID, "count", len(req.Devices))

	// Store each device
	ctx := context.Background()
	stored := 0
	for _, deviceMap := range req.Devices {
		// Convert map to Device struct (simplified - in production, use proper unmarshaling)
		device := &storage.Device{}
		device.AgentID = req.AgentID
		device.LastSeen = req.Timestamp
		device.FirstSeen = req.Timestamp
		device.CreatedAt = req.Timestamp

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
			logWarn("Device missing serial, skipping", "ip", device.IP)
			continue
		}

		if err := serverStore.UpsertDevice(ctx, device); err != nil {
			logError("Failed to store device", "serial", device.Serial, "error", err)
			continue
		}
		stored++

		// Broadcast device_updated event to UI via SSE
		sseHub.Broadcast(SSEEvent{
			Type: "device_updated",
			Data: map[string]interface{}{
				"agent_id":     device.AgentID,
				"serial":       device.Serial,
				"ip":           device.IP,
				"manufacturer": device.Manufacturer,
				"model":        device.Model,
				"hostname":     device.Hostname,
			},
		})
	}

	// Get authenticated agent from context
	agent := r.Context().Value(agentContextKey).(*storage.Agent)

	logInfo("Devices stored", "agent_id", agent.AgentID, "stored", stored, "total", len(req.Devices))

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

// handleDevicesList returns all devices for UI display
func handleDevicesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}

	principal := getPrincipal(r)
	if principal == nil {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	scope, ok := tenantScope(principal)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	ctx := context.Background()

	// Get all devices across all agents (will filter below for tenant users)
	devices, err := serverStore.ListAllDevices(ctx)
	if err != nil {
		logError("Failed to list devices", "error", err)
		http.Error(w, "Failed to list devices", http.StatusInternalServerError)
		return
	}

	// If scoped, filter devices to those owned by agents in the user's tenants
	if scope != nil {
		agents, aerr := serverStore.ListAgents(ctx)
		if aerr != nil {
			http.Error(w, "Failed to list agents for filtering", http.StatusInternalServerError)
			return
		}
		agentAllowed := make(map[string]struct{}, len(agents))
		for _, a := range agents {
			if a == nil {
				continue
			}
			if tenantAllowed(scope, a.TenantID) {
				agentAllowed[a.AgentID] = struct{}{}
			}
		}

		filtered := make([]*storage.Device, 0, len(devices))
		for _, d := range devices {
			if d == nil {
				continue
			}
			if _, ok := agentAllowed[d.AgentID]; ok {
				filtered = append(filtered, d)
			}
		}
		devices = filtered
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(devices)
}

// handleMetricsSummary returns a lightweight metrics summary for the UI
// It intentionally keeps the query simple (no heavy aggregation) and returns:
// - agents_count
// - devices_count
// - devices_with_metrics_24h (devices that have at least one metric in the last 24h)
// - recent: sample list of latest metrics for up to N devices
func handleMetricsSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}

	principal := getPrincipal(r)
	if principal == nil {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	scope, ok := tenantScope(principal)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	ctx := context.Background()

	// Count agents (may be filtered below for non-admins)
	agents, err := serverStore.ListAgents(ctx)
	if err != nil {
		logError("Failed to list agents for metrics summary", "error", err)
		http.Error(w, "Failed to fetch metrics summary", http.StatusInternalServerError)
		return
	}

	// Count devices
	devices, err := serverStore.ListAllDevices(ctx)
	if err != nil {
		logError("Failed to list devices for metrics summary", "error", err)
		http.Error(w, "Failed to fetch metrics summary", http.StatusInternalServerError)
		return
	}

	// If scoped, filter agents/devices to tenant scope
	if scope != nil {
		fAgents := make([]*storage.Agent, 0)
		for _, a := range agents {
			if a != nil && tenantAllowed(scope, a.TenantID) {
				fAgents = append(fAgents, a)
			}
		}
		agents = fAgents

		agentAllowed := make(map[string]struct{}, len(agents))
		for _, a := range agents {
			if a != nil {
				agentAllowed[a.AgentID] = struct{}{}
			}
		}
		fDevices := make([]*storage.Device, 0)
		for _, d := range devices {
			if d == nil {
				continue
			}
			if _, ok := agentAllowed[d.AgentID]; ok {
				fDevices = append(fDevices, d)
			}
		}
		devices = fDevices
	}

	// For a lightweight recent-metrics view, sample up to N devices and fetch their latest metrics
	const sampleN = 20
	recent := make([]map[string]interface{}, 0)
	devicesWithRecent := 0
	cutoff := time.Now().Add(-24 * time.Hour)

	for i, dev := range devices {
		if i >= sampleN {
			break
		}
		if dev == nil || dev.Serial == "" {
			continue
		}
		m, err := serverStore.GetLatestMetrics(ctx, dev.Serial)
		if err != nil {
			// no metrics for this device or DB error - skip
			continue
		}
		if m.Timestamp.After(cutoff) {
			devicesWithRecent++
		}
		recent = append(recent, map[string]interface{}{
			"serial":     m.Serial,
			"timestamp":  m.Timestamp,
			"page_count": m.PageCount,
		})
	}

	resp := map[string]interface{}{
		"agents_count":             len(agents),
		"devices_count":            len(devices),
		"devices_with_metrics_24h": devicesWithRecent,
		"recent":                   recent,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleMetricsHistory returns metrics history for a device from server store.
// Query params: serial (required) and either since (RFC3339) or period (day|week|month|year)
func handleMetricsHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}

	principal := getPrincipal(r)
	if principal == nil {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	scope, ok := tenantScope(principal)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	serial := r.URL.Query().Get("serial")
	if serial == "" {
		http.Error(w, "serial parameter required", http.StatusBadRequest)
		return
	}

	// Determine since time
	var since time.Time
	now := time.Now()

	sinceStr := r.URL.Query().Get("since")
	if sinceStr != "" {
		var err error
		since, err = time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			http.Error(w, "invalid since parameter (use RFC3339)", http.StatusBadRequest)
			return
		}
	} else {
		// period-based
		period := r.URL.Query().Get("period")
		if period == "" {
			period = "week"
		}
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
			since = now.Add(-7 * 24 * time.Hour)
		}
	}

	ctx := context.Background()
	if scope != nil {
		device, err := serverStore.GetDevice(ctx, serial)
		if err != nil || device == nil || device.AgentID == "" {
			http.Error(w, "device not found", http.StatusNotFound)
			return
		}
		agent, err := serverStore.GetAgent(ctx, device.AgentID)
		if err != nil {
			http.Error(w, "device not found", http.StatusNotFound)
			return
		}
		if !tenantAllowed(scope, agent.TenantID) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}
	history, err := serverStore.GetMetricsHistory(ctx, serial, since)
	if err != nil {
		logError("Failed to get metrics history", "serial", serial, "error", err)
		http.Error(w, "failed to get metrics history", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
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
		logWarn("Invalid JSON in metrics batch", "error", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	logInfo("Metrics batch received", "agent_id", req.AgentID, "count", len(req.Metrics))

	// Store each metric snapshot
	ctx := context.Background()
	stored := 0
	for _, metricMap := range req.Metrics {
		metric := &storage.MetricsSnapshot{}
		metric.AgentID = req.AgentID
		metric.Timestamp = req.Timestamp

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
			logError("Failed to store metrics", "serial", metric.Serial, "error", err)
			continue
		}
		stored++
	}

	// Get authenticated agent from context
	agent := r.Context().Value(agentContextKey).(*storage.Agent)

	logInfo("Metrics stored", "agent_id", agent.AgentID, "stored", stored, "total", len(req.Metrics))

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

// Web UI handlers
func handleWebUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "only GET allowed", http.StatusMethodNotAllowed)
		return
	}

	user, ok := ensureInteractiveSession(w, r)
	if !ok {
		return
	}
	r = r.WithContext(contextWithPrincipal(r.Context(), user))

	tmpl, err := template.ParseFS(webFS, "web/index.html")
	if err != nil {
		logError("Failed to parse index.html template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, nil); err != nil {
		logError("Failed to execute index.html template", "error", err)
	}
}

func handleStatic(w http.ResponseWriter, r *http.Request) {
	// Remove /static/ prefix to get file name
	fileName := strings.TrimPrefix(r.URL.Path, "/static/")

	// Serve shared assets from common/web package
	if fileName == "shared.css" {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write([]byte(sharedweb.SharedCSS))
		return
	}
	if fileName == "shared.js" {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write([]byte(sharedweb.SharedJS))
		return
	}
	if fileName == "metrics.js" {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write([]byte(sharedweb.MetricsJS))
		return
	}
	if fileName == "cards.js" {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write([]byte(sharedweb.CardsJS))
		return
	}
	// Serve vendored flatpickr files from the embedded common/web package so
	// they are served with correct MIME types and avoid CDN/CSP issues.
	if fileName == "flatpickr/flatpickr.min.js" {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write([]byte(sharedweb.FlatpickrJS))
		return
	}
	if fileName == "flatpickr/flatpickr.min.css" {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write([]byte(sharedweb.FlatpickrCSS))
		return
	}
	if fileName == "flatpickr/LICENSE.md" {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write([]byte(sharedweb.FlatpickrLicense))
		return
	}

	// Serve other files from embedded FS
	filePath := "web/" + fileName
	content, err := webFS.ReadFile(filePath)
	if err != nil {
		logWarn("Static file not found", "fileName", fileName)
		http.NotFound(w, r)
		return
	}

	// Set content type based on extension
	contentType := "text/plain"
	if strings.HasSuffix(filePath, ".css") {
		contentType = "text/css; charset=utf-8"
	} else if strings.HasSuffix(filePath, ".js") {
		contentType = "application/javascript; charset=utf-8"
	} else if strings.HasSuffix(filePath, ".json") {
		contentType = "application/json; charset=utf-8"
	} else if strings.HasSuffix(filePath, ".html") {
		contentType = "text/html; charset=utf-8"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=3600") // Cache for 1 hour
	w.Write(content)
}

// Minimal settings handler - returns a small placeholder payload and accepts POST
func handleSettings(w http.ResponseWriter, r *http.Request) {
	if serverConfig == nil {
		http.Error(w, "server config not loaded", http.StatusInternalServerError)
		return
	}

	switch r.Method {
	case http.MethodGet:
		// Return relevant settings for the UI
		out := map[string]interface{}{
			"server_http_port":       serverConfig.Server.HTTPPort,
			"server_db_path":         serverConfig.Database.Path,
			"server_https_port":      serverConfig.Server.HTTPSPort,
			"metrics_retention_days": 365,
			"audit_retention_days":   90,
			"smtp": map[string]interface{}{
				"enabled": serverConfig.SMTP.Enabled,
				"host":    serverConfig.SMTP.Host,
				"port":    serverConfig.SMTP.Port,
				"user":    serverConfig.SMTP.User,
				"from":    serverConfig.SMTP.From,
			},
			"auto_approve_agents": serverConfig.Server.AutoApproveAgents,
			"agent_timeout":       serverConfig.Server.AgentTimeoutMinutes,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
		return
	case http.MethodPost:
		// Accept settings updates from UI and persist to config file if possible
		var payload struct {
			ServerHTTPPort  int    `json:"server_http_port,omitempty"`
			ServerHTTPSPort int    `json:"server_https_port,omitempty"`
			ServerDBPath    string `json:"server_db_path,omitempty"`
			SMTP            struct {
				Enabled bool   `json:"enabled"`
				Host    string `json:"host"`
				Port    int    `json:"port"`
				User    string `json:"user"`
				Pass    string `json:"pass"`
				From    string `json:"from"`
			} `json:"smtp"`
			AutoApproveAgents bool `json:"auto_approve_agents"`
			AgentTimeout      int  `json:"agent_timeout"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Apply to in-memory config
		if payload.ServerHTTPPort != 0 {
			serverConfig.Server.HTTPPort = payload.ServerHTTPPort
		}
		if payload.ServerHTTPSPort != 0 {
			serverConfig.Server.HTTPSPort = payload.ServerHTTPSPort
		}
		if payload.ServerDBPath != "" {
			serverConfig.Database.Path = payload.ServerDBPath
		}
		// Agent management
		serverConfig.Server.AutoApproveAgents = payload.AutoApproveAgents
		if payload.AgentTimeout != 0 {
			serverConfig.Server.AgentTimeoutMinutes = payload.AgentTimeout
		}
		serverConfig.SMTP.Enabled = payload.SMTP.Enabled
		if payload.SMTP.Host != "" {
			serverConfig.SMTP.Host = payload.SMTP.Host
		}
		if payload.SMTP.Port != 0 {
			serverConfig.SMTP.Port = payload.SMTP.Port
		}
		if payload.SMTP.User != "" {
			serverConfig.SMTP.User = payload.SMTP.User
		}
		if payload.SMTP.Pass != "" {
			// Keep SMTP password env-only: set in-memory and update process env.
			serverConfig.SMTP.Pass = payload.SMTP.Pass
			// Update process env so immediate operations can use it
			_ = os.Setenv("SMTP_PASS", payload.SMTP.Pass)
			// Persist to a .env file next to the loaded config (fallback to cwd) so
			// operators can pick it up for future restarts without storing it in
			// the main config TOML.
			go func(pass string) {
				envPath := ".env"
				if loadedConfigPath != "" && loadedConfigPath != "defaults" {
					dir := filepath.Dir(loadedConfigPath)
					envPath = filepath.Join(dir, ".env")
				}
				_ = writeEnvFile(envPath, map[string]string{"SMTP_PASS": pass})
			}(payload.SMTP.Pass)
		}
		if payload.SMTP.From != "" {
			serverConfig.SMTP.From = payload.SMTP.From
		}

		// Persist to config file if we loaded a real file. When persisting, scrub
		// sensitive secrets (like SMTP.Pass) from the main TOML so secrets remain
		// env-only. Use a temporary copy to avoid mutating in-memory serverConfig.
		if loadedConfigPath != "defaults" && loadedConfigPath != "" {
			copyCfg := *serverConfig
			copyCfg.SMTP.Pass = ""
			if err := config.WriteTOML(loadedConfigPath, &copyCfg); err != nil {
				http.Error(w, fmt.Sprintf("failed to save config: %v", err), http.StatusInternalServerError)
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		return
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

// Minimal logs handler - returns an array of recent server log lines (best-effort)
func handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}

	// Try to read a logs file if present in the workspace (best-effort); otherwise return empty list
	var lines []string

	// Attempt to read workspace file from disk (not embedded) - this is optional
	if b, err := os.ReadFile("logs/build.log"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if strings.TrimSpace(l) != "" {
				lines = append(lines, l)
			}
		}
	}

	// If no lines found, return an informative placeholder
	if len(lines) == 0 {
		lines = []string{"No server logs available (placeholder response)."}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"logs": lines})
}

// handleAuditLogs exposes persisted agent/server audit trail entries to admins.
// Supports optional query parameters:
//   - hours: lookback window (default 24)
//   - since: RFC3339 timestamp overriding hours
//   - agent_id: filter entries for a specific agent UUID
func handleAuditLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}

	principal := getPrincipal(r)
	if principal == nil || !principal.HasRole(storage.RoleAdmin) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	query := r.URL.Query()
	agentID := strings.TrimSpace(query.Get("agent_id"))
	lookback := 24 * time.Hour
	if hoursStr := strings.TrimSpace(query.Get("hours")); hoursStr != "" {
		if hrs, err := strconv.Atoi(hoursStr); err == nil && hrs > 0 {
			lookback = time.Duration(hrs) * time.Hour
		} else {
			http.Error(w, "invalid hours parameter", http.StatusBadRequest)
			return
		}
	}
	since := time.Now().Add(-lookback)
	if sinceStr := strings.TrimSpace(query.Get("since")); sinceStr != "" {
		parsed, err := time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			http.Error(w, "invalid since parameter", http.StatusBadRequest)
			return
		}
		since = parsed
	}

	entries, err := serverStore.GetAuditLog(r.Context(), agentID, since)
	if err != nil {
		logError("Failed to load audit log", "agent_id", agentID, "error", err)
		http.Error(w, "failed to load audit log", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"entries":  entries,
		"agent_id": agentID,
		"since":    since.UTC(),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logError("Failed to encode audit log response", "error", err)
	}
}

// writeEnvFile updates or creates a simple KEY=VALUE env file at path with
// the provided vars. Existing keys are updated; other lines are preserved.
func writeEnvFile(path string, vars map[string]string) error {
	// Read existing file if present
	existing := map[string]string{}
	if b, err := os.ReadFile(path); err == nil {
		lines := strings.Split(string(b), "\n")
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if l == "" || strings.HasPrefix(l, "#") {
				continue
			}
			parts := strings.SplitN(l, "=", 2)
			if len(parts) == 2 {
				k := strings.TrimSpace(parts[0])
				v := strings.TrimSpace(parts[1])
				existing[k] = v
			}
		}
	}

	// Merge updates
	for k, v := range vars {
		existing[k] = v
	}

	// Build output content
	var buf bytes.Buffer
	for k, v := range existing {
		fmt.Fprintf(&buf, "%s=%s\n", k, v)
	}

	// Write file with owner-only perms when possible
	return os.WriteFile(path, buf.Bytes(), 0600)
}

// handleSettingsTestEmail accepts POST { to: "recipient@example.com" }
// and sends a short test email using the configured SMTP settings.
func handleSettingsTestEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		To string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	to := strings.TrimSpace(req.To)
	if to == "" {
		// fallback to configured From address
		if serverConfig != nil && serverConfig.SMTP.From != "" {
			to = serverConfig.SMTP.From
		}
	}
	if to == "" {
		http.Error(w, "no recipient specified", http.StatusBadRequest)
		return
	}

	// rate limiting to avoid SMTP abuse (count attempts by IP)
	clientIP := extractIPFromAddr(r.RemoteAddr)
	if authRateLimiter != nil {
		if isBlocked, _ := authRateLimiter.IsBlocked(clientIP, "settings-test-email"); isBlocked {
			http.Error(w, "Too many requests. Try again later.", http.StatusTooManyRequests)
			return
		}
		authRateLimiter.RecordFailure(clientIP, "settings-test-email")
	}

	body := fmt.Sprintf("This is a test email from PrintMaster sent at %s\n\nIf you received this message, SMTP settings are working.", time.Now().Format(time.RFC3339))
	if err := sendEmail(to, "PrintMaster: Test Email", body); err != nil {
		http.Error(w, fmt.Sprintf("failed to send test email: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"sent": true})
}
