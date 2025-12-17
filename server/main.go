// PrintMaster Server - Central management hub for PrintMaster agents
// Aggregates data from multiple agents, provides reporting, alerting, and web UI
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/tls"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/fs"
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
	alertsapi "printmaster/server/alerts"
	authz "printmaster/server/authz"
	"printmaster/server/packager"
	releases "printmaster/server/releases"
	selfupdate "printmaster/server/selfupdate"
	serversettings "printmaster/server/settings"
	"printmaster/server/storage"
	tenancy "printmaster/server/tenancy"
	updatepolicy "printmaster/server/updatepolicy"
	"runtime"
	"sort"
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

const (
	uiLogLineLimit              = 500
	installerCleanupInterval    = 6 * time.Hour
	installerCleanupGracePeriod = 10 * time.Minute
	installerCleanupRunTimeout  = 2 * time.Minute
	installerCleanupListTimeout = 30 * time.Second
	httpRecencyThreshold        = 90 * time.Second
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

var assetVersionTag = computeAssetVersion()

func computeAssetVersion() string {
	candidates := []string{strings.TrimSpace(GitCommit), strings.TrimSpace(Version), strings.TrimSpace(BuildTime)}
	for _, candidate := range candidates {
		if candidate == "" || candidate == "unknown" {
			continue
		}
		if candidate == "dev" && strings.TrimSpace(GitCommit) == "unknown" {
			continue
		}
		return sanitizeAssetVersion(candidate)
	}
	return strconv.FormatInt(time.Now().Unix(), 10)
}

func sanitizeAssetVersion(input string) string {
	if input == "" {
		return "dev"
	}
	var b strings.Builder
	for _, r := range input {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		b.WriteRune('-')
	}
	return b.String()
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

var (
	serverLogger        *logger.Logger
	serverStore         storage.Store
	settingsResolver    *serversettings.Resolver
	authRateLimiter     *AuthRateLimiter     // Rate limiter for failed auth attempts
	configLoadErrors    []string             // Track config loading errors for display in UI
	usingDefaultConfig  bool                 // Flag to indicate if using defaults vs loaded config
	loadedConfigPath    string               // Path of the config file that was successfully loaded
	sseHub              *SSEHub              // SSE hub for real-time UI updates
	wsHub               *wscommon.Hub        // In-process hub for websocket-capable UI clients
	serverConfig        *Config              // Loaded server configuration (accessible to handlers)
	serverLogDir        string               // Directory containing server logs for UI fetches
	configSourceTracker *ConfigSourceTracker // Tracks which keys were set by env vars
	releaseManager      *releases.Manager
	intakeWorker        *releases.IntakeWorker // Release intake worker for syncing GitHub releases
	selfUpdateManager   *selfupdate.Manager    // Self-update manager for server binary updates
	alertEvaluator      *alertsapi.Evaluator   // Alert evaluation background worker
)

var processStart = time.Now()

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
	healthCheck := flag.Bool("health", false, "Perform local health check against /health and exit")
	selfUpdateApply := flag.String("selfupdate-apply", "", "Internal use only: path to self-update instruction")

	// Service management flags
	svcCommand := flag.String("service", "", "Service command: install, uninstall, start, stop, restart, run")
	flag.Parse()

	if *selfUpdateApply != "" {
		if err := selfupdate.RunApplyHelper(*selfUpdateApply); err != nil {
			fmt.Fprintf(os.Stderr, "Self-update helper failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

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

	// Lightweight health probe for Docker/monitoring: call local /health and exit.
	if *healthCheck {
		if err := runHealthCheck(*configPath); err != nil {
			fmt.Fprintf(os.Stderr, "health check failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("healthy")
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
			if loadedCfg, tracker, err := LoadConfig(resolved); err == nil {
				cfg = loadedCfg
				configSourceTracker = tracker
				if absPath, err := filepath.Abs(resolved); err == nil {
					loadedConfigPath = absPath
				} else {
					loadedConfigPath = resolved
				}
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
			if loadedCfg, tracker, err := LoadConfig(configPath); err == nil {
				cfg = loadedCfg
				configSourceTracker = tracker
				if absPath, err := filepath.Abs(configPath); err == nil {
					loadedConfigPath = absPath
				} else {
					loadedConfigPath = configPath
				}
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
		configSourceTracker = newConfigSourceTracker()
		// Apply environment overrides even when no config file is present
		applyEnvOverrides(cfg, configSourceTracker)
		loadedConfigPath = "defaults"
		usingDefaultConfig = true
	} else {
		usingDefaultConfig = false
	}

	// Always apply environment overrides for database configuration (supports SERVER_DB_* and DB_*).
	// Path-related overrides are only relevant for SQLite; other drivers rely on DSN/host credentials.
	config.ApplyDatabaseEnvOverrides(&cfg.Database, "SERVER")
	dbDriver := strings.ToLower(cfg.Database.EffectiveDriver())
	isSQLiteDriver := dbDriver == "sqlite" || dbDriver == "sqlite3" || dbDriver == "modernc" || dbDriver == "modernc-sqlite"

	if isSQLiteDriver {
		// If the env var points to a directory, append the default filename
		// so users can set either a directory or a full file path.
		if cfg.Database.Path != "" {
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
	}

	logInfo("PrintMaster Server starting", "version", Version, "protocol_version", ProtocolVersion)
	logInfo("Build metadata", "build_time", BuildTime, "git_commit", GitCommit, "build_type", BuildType)
	logDebug("Runtime", "go", runtime.Version(), "os", runtime.GOOS, "arch", runtime.GOARCH)

	// Initialize logger
	if isSQLiteDriver {
		if cfg.Database.Path == "" {
			if runtime.GOOS == "windows" && !isService {
				if userDir, err := config.GetDataDirectory("server", false); err == nil {
					cfg.Database.Path = filepath.Join(userDir, "server.db")
					logDebug("Using per-user data directory for database", "path", cfg.Database.Path)
				} else {
					logWarn("Failed to resolve user data directory; falling back to default DB path", "error", err)
					cfg.Database.Path = storage.GetDefaultDBPath()
				}
			} else {
				cfg.Database.Path = storage.GetDefaultDBPath()
			}
		}

		if cfg.Database.Path != "" && runtime.GOOS == "windows" && !isService {
			// If the path still points into ProgramData (custom config/env override), verify permissions
			pd := os.Getenv("PROGRAMDATA")
			if pd == "" {
				pd = "C:\\ProgramData"
			}
			if strings.HasPrefix(strings.ToLower(cfg.Database.Path), strings.ToLower(pd)) {
				parent := filepath.Dir(cfg.Database.Path)
				if err := os.MkdirAll(parent, 0755); err != nil {
					logInfo("ProgramData path not writable; switching to per-user data directory", "programdata", pd, "error", err)
					if userDir, derr := config.GetDataDirectory("server", false); derr == nil {
						cfg.Database.Path = filepath.Join(userDir, "server.db")
					} else {
						logWarn("Failed to resolve user data directory; keeping existing DB path", "error", derr)
					}
				}
			}
		}

		if cfg.Database.Path != "" {
			if absDBPath, err := filepath.Abs(cfg.Database.Path); err == nil {
				cfg.Database.Path = absDBPath
			}
		}
	} else if cfg.Database.Path != "" {
		logDebug("Ignoring SQLite database path because non-sqlite driver is configured", "driver", dbDriver, "path", cfg.Database.Path)
		cfg.Database.Path = ""
	}

	// Determine log directory based on whether we're running as a service
	logDir, err := config.GetLogDirectory("server", isService)
	if err != nil {
		logFatal("Failed to get log directory", "error", err)
	}
	serverLogDir = logDir

	serverLogger = logger.NewWithComponent(logger.LevelFromString(cfg.Logging.Level), logDir, "server", 1000)
	logInfo("Server starting", "version", Version, "protocol", ProtocolVersion, "config", loadedConfigPath)

	// Save loaded config globally for handlers
	serverConfig = cfg

	// Initialize database
	dbLogFields := []interface{}{"driver", dbDriver}
	if isSQLiteDriver {
		dbLogFields = append(dbLogFields, "path", cfg.Database.Path)
	} else {
		dbLogFields = append(dbLogFields, "host", cfg.Database.Host, "database", cfg.Database.Name)
		if cfg.Database.DSN != "" {
			dbLogFields = append(dbLogFields, "dsn_source", "explicit")
		} else {
			dbLogFields = append(dbLogFields, "dsn_source", "built")
		}
	}
	logInfo("Using database", dbLogFields...)
	logInfo("Initializing database", dbLogFields...)

	// Inject structured logger into storage package so DB initialization logs are structured
	storage.SetLogger(serverLogger)
	serverStore, err = storage.NewStore(&cfg.Database)
	if err != nil {
		logFatal("Failed to initialize database", "error", err)
	}
	defer serverStore.Close()
	settingsResolver = serversettings.NewResolver(serverStore)
	tenancy.SetAgentSettingsBuilder(func(ctx context.Context, tenantID string, agentID string) (string, interface{}, error) {
		snapshot, err := serversettings.BuildAgentSnapshot(ctx, settingsResolver, tenantID, agentID)
		if err != nil {
			return "", nil, err
		}
		if snapshot.Version == "" {
			return "", nil, nil
		}
		return snapshot.Version, snapshot, nil
	})

	logInfo("Database initialized successfully")

	releaseManager, err = releases.NewManager(serverStore, serverLogger, releases.ManagerOptions{})
	if err != nil {
		logWarn("Release manifest manager disabled", "error", err)
	} else if _, err := releaseManager.EnsureActiveKey(ctx); err != nil {
		logWarn("Failed to ensure signing key", "error", err)
	}

	if worker, err := releases.NewIntakeWorker(serverStore, serverLogger, releases.Options{
		GitHubToken:     os.Getenv("GITHUB_TOKEN"),
		UserAgent:       fmt.Sprintf("printmaster-server/%s release-intake", Version),
		ManifestManager: releaseManager,
	}); err != nil {
		logWarn("Release intake worker disabled", "error", err)
	} else {
		intakeWorker = worker
		go intakeWorker.Run(ctx)
	}

	var (
		dataDir           string
		installerCacheDir string
		installerKeyPath  string
	)
	if resolvedDir, derr := config.GetDataDirectory("server", isService); derr == nil {
		dataDir = resolvedDir
	} else {
		dataDir = filepath.Join(os.TempDir(), "printmaster")
		logDebug("Falling back to temp data directory", "dir", dataDir, "error", derr)
	}
	installerCacheDir = filepath.Join(dataDir, "installers")
	installerKeyPath = filepath.Join(dataDir, "secrets", "installer.key")
	packagerManager, err := packager.NewManager(serverStore, serverLogger, packager.ManagerOptions{
		CacheDir:          installerCacheDir,
		DefaultTTL:        7 * 24 * time.Hour,
		EncryptionKeyPath: installerKeyPath,
		Builders: []packager.Builder{
			packager.NewZipBuilder(),
			packager.NewTarGzBuilder(),
		},
	})
	if err != nil {
		logWarn("Installer packager disabled", "error", err)
	} else {
		tenancy.SetInstallerPackager(packagerManager)
		logInfo("Installer packager initialized", "cache_dir", installerCacheDir)
		startInstallerCleanupWorker(ctx, serverStore, installerCacheDir)
	}

	// Serialize database config for self-update helper
	dbConfigJSON, err := json.Marshal(map[string]interface{}{
		"driver":   cfg.Database.Driver,
		"path":     cfg.Database.Path,
		"host":     cfg.Database.Host,
		"port":     cfg.Database.Port,
		"name":     cfg.Database.Name,
		"user":     cfg.Database.User,
		"password": cfg.Database.Password,
	})
	if err != nil {
		logWarn("Failed to serialize database config", "error", err)
	}

	if manager, err := selfupdate.NewManager(selfupdate.Options{
		Store:          serverStore,
		Log:            serverLogger,
		DataDir:        dataDir,
		Enabled:        cfg.Server.SelfUpdateEnabled,
		CurrentVersion: Version,
		Component:      "server",
		Channel:        "stable",
		Platform:       runtime.GOOS,
		Arch:           runtime.GOARCH,
		DatabaseConfig: string(dbConfigJSON),
		ServiceName:    getServiceConfig().Name,
	}); err != nil {
		logWarn("Self-update manager disabled", "error", err)
	} else {
		selfUpdateManager = manager
		manager.Start(ctx)
	}

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
	if existingUser, err := serverStore.GetUserByUsername(bctx, adminUser); err != nil {
		logWarn("Failed to check for existing admin user", "user", adminUser, "error", err)
	} else if existingUser == nil {
		// create admin user
		u := &storage.User{Username: adminUser, Role: storage.RoleAdmin}
		if err := serverStore.CreateUser(bctx, u, adminPass); err != nil {
			logWarn("Failed to create initial admin user", "user", adminUser, "error", err)
		} else {
			logInfo("Initial admin user created (default)", "user", adminUser)
		}
	}

	// Bootstrap auto-join token if INIT_SECRET is set (for Docker Compose auto-registration)
	initSecret := os.Getenv("INIT_SECRET")
	if initSecret != "" {
		// Check if this secret was already used
		secretUsedFile := filepath.Join(dataDir, ".init_secret_used")
		if _, err := os.Stat(secretUsedFile); os.IsNotExist(err) {
			// Secret not yet used - create a one-time join token with the init secret as the token value
			// This allows agents with the same INIT_SECRET to auto-register
			defaultTenant := ""
			tenants, err := serverStore.ListTenants(bctx)
			if err == nil && len(tenants) > 0 {
				defaultTenant = tenants[0].ID
			}

			if defaultTenant != "" {
				// Use the init secret directly as the join token
				jt := &storage.JoinToken{
					ID:        "init-secret",
					TenantID:  defaultTenant,
					ExpiresAt: time.Now().Add(365 * 24 * time.Hour), // Valid for 1 year
					OneTime:   false,                                // Allow multiple agents to use it
					CreatedAt: time.Now(),
				}

				// Store it with the init secret as the raw token
				if _, err := serverStore.CreateJoinTokenWithSecret(bctx, jt, initSecret); err != nil {
					logWarn("Failed to create init secret join token", "error", err)
				} else {
					logInfo("Auto-join token created from INIT_SECRET (allows agent auto-registration)")
				}
			}
		} else {
			logInfo("INIT_SECRET was previously used, skipping auto-join token creation")
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

	// Start alert evaluator background worker
	alertEvaluator = alertsapi.NewEvaluator(serverStore, alertsapi.EvaluatorConfig{
		Interval: 60 * time.Second,
		Logger:   nil, // Uses slog.Default()
	})
	alertEvaluator.Start()
	defer alertEvaluator.Stop()
	logInfo("Alert evaluator started", "interval", "60s")

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
		clientIP := getRealIP(r)

		// Log the incoming request at debug level
		logDebug("Incoming request",
			"method", r.Method,
			"path", r.URL.Path,
			"client_ip", clientIP,
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
		// Extract client IP address (respects X-Forwarded-For when behind proxy)
		clientIP := getRealIP(r)

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
					logAuditEntry(ctx, &storage.AuditEntry{
						ActorType: storage.AuditActorAgent,
						ActorID:   tokenPrefix,
						Action:    "auth_blocked",
						Details: fmt.Sprintf("IP blocked after %d failed attempts with token %s... Error: %s",
							attemptCount, tokenPrefix, err.Error()),
						IPAddress: clientIP,
						UserAgent: r.Header.Get("User-Agent"),
						Severity:  storage.AuditSeverityWarn,
						Metadata: map[string]interface{}{
							"attempt_count": attemptCount,
							"protocol":      "http",
						},
					})
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
			clientIP := getRealIP(r)
			authRateLimiter.RecordFailure(clientIP, req.Username)
		}
		logAuditEntry(ctx, &storage.AuditEntry{
			ActorType: storage.AuditActorUser,
			ActorID:   strings.ToLower(req.Username),
			ActorName: req.Username,
			Action:    "auth.login",
			Severity:  storage.AuditSeverityWarn,
			Details:   "Invalid username or password",
			IPAddress: extractClientIP(r),
			UserAgent: r.Header.Get("User-Agent"),
			Metadata: map[string]interface{}{
				"result": "failure",
			},
		})
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	ses, err := createSessionCookie(w, r, user.ID)
	if err != nil {
		serverLogger.Error("Failed to create session cookie after login", "user_id", user.ID, "username", user.Username, "error", err)
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}
	rememberUserTenantHint(w, r, user)

	logAuditEntry(ctx, &storage.AuditEntry{
		ActorType: storage.AuditActorUser,
		ActorID:   user.Username,
		ActorName: user.Username,
		TenantID:  user.TenantID,
		Action:    "auth.login",
		Details:   "User authenticated via local login",
		Metadata: map[string]interface{}{
			"result":     "success",
			"session_id": maskSensitiveToken(ses.Token),
			"expires_at": ses.ExpiresAt.Format(time.RFC3339),
		},
		IPAddress: extractClientIP(r),
		UserAgent: r.Header.Get("User-Agent"),
	})

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

func authorizeRequest(r *http.Request, action authz.Action, resource authz.ResourceRef) error {
	principal := getPrincipal(r)
	if principal == nil {
		return authz.ErrUnauthorized
	}
	subject := authz.Subject{
		Role:             principal.Role,
		AllowedTenantIDs: principal.AllowedTenantIDs(),
		IsAdmin:          principal.IsAdmin(),
	}
	return authz.Authorize(subject, action, resource)
}

func authorizeOrReject(w http.ResponseWriter, r *http.Request, action authz.Action, resource authz.ResourceRef) bool {
	if err := authorizeRequest(r, action, resource); err != nil {
		status := http.StatusForbidden
		if errors.Is(err, authz.ErrUnauthorized) {
			status = http.StatusUnauthorized
		}
		http.Error(w, http.StatusText(status), status)
		return false
	}
	return true
}

// handlePasswordPolicy returns the current password policy requirements (public endpoint for UI)
func handlePasswordPolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	policy := struct {
		MinLength      int    `json:"min_length"`
		RequireUpper   bool   `json:"require_upper"`
		RequireLower   bool   `json:"require_lower"`
		RequireNumber  bool   `json:"require_number"`
		RequireSpecial bool   `json:"require_special"`
		Description    string `json:"description"`
	}{
		MinLength:      8,
		RequireUpper:   false,
		RequireLower:   false,
		RequireNumber:  false,
		RequireSpecial: false,
		Description:    "Minimum 8 characters",
	}
	if serverConfig != nil {
		minLen := serverConfig.Security.PasswordMinLength
		if minLen <= 0 {
			minLen = 8
		}
		policy.MinLength = minLen
		policy.RequireUpper = serverConfig.Security.PasswordRequireUpper
		policy.RequireLower = serverConfig.Security.PasswordRequireLower
		policy.RequireNumber = serverConfig.Security.PasswordRequireNumber
		policy.RequireSpecial = serverConfig.Security.PasswordRequireSpecial
		policy.Description = serverConfig.Security.PasswordRequirements()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(policy)
}

// handleUsers handles GET (list users) and POST (create user) for admin UI
func handleUsers(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	switch r.Method {
	case http.MethodGet:
		if !authorizeOrReject(w, r, authz.ActionSettingsServerRead, authz.ResourceRef{}) {
			return
		}
		if !authorizeOrReject(w, r, authz.ActionUsersRead, authz.ResourceRef{}) {
			return
		}
		users, err := serverStore.ListUsers(ctx)
		if err != nil {
			serverLogger.Error("Failed to list users", "error", err)
			http.Error(w, "failed to list users", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(users)
		return
	case http.MethodPost:
		if !authorizeOrReject(w, r, authz.ActionSettingsServerWrite, authz.ResourceRef{}) {
			return
		}
		if !authorizeOrReject(w, r, authz.ActionUsersWrite, authz.ResourceRef{}) {
			return
		}
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
		// Validate password against policy
		if serverConfig != nil {
			if err := serverConfig.Security.ValidatePassword(req.Password); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
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
			serverLogger.Error("Failed to create user", "username", req.Username, "role", role, "error", err)
			http.Error(w, fmt.Sprintf("failed to create user: %v", err), http.StatusInternalServerError)
			return
		}
		// Do not return password hash
		u.PasswordHash = ""
		actorType, actorID, actorName, actorTenant := auditActorFromPrincipal(r)
		logAuditEntry(ctx, &storage.AuditEntry{
			ActorType:  actorType,
			ActorID:    actorID,
			ActorName:  actorName,
			TenantID:   actorTenant,
			Action:     "user.create",
			TargetType: "user",
			TargetID:   u.Username,
			Details:    fmt.Sprintf("Created user %s with role %s", u.Username, u.Role),
			Metadata: map[string]interface{}{
				"role":       u.Role,
				"tenant_ids": u.TenantIDs,
				"email":      u.Email,
			},
			IPAddress: extractClientIP(r),
		})
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
	actorType, actorID, actorName, actorTenant := auditActorFromPrincipal(r)
	logAuditEntry(ctx, &storage.AuditEntry{
		ActorType: actorType,
		ActorID:   actorID,
		ActorName: actorName,
		TenantID:  actorTenant,
		Action:    "auth.logout",
		Details:   "Session terminated",
		Metadata: map[string]interface{}{
			"token_prefix": maskSensitiveToken(token),
		},
		IPAddress: extractClientIP(r),
		UserAgent: r.Header.Get("User-Agent"),
	})
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
	// Compute session token hash for current session identification
	var sessionTokenHash string
	if token := sessionTokenFromRequest(r); token != "" {
		sessionTokenHash = storage.TokenHash(token)
	}
	// don't expose password hash
	out := map[string]interface{}{
		"id":                 u.ID,
		"username":           u.Username,
		"role":               u.Role,
		"tenant_id":          u.TenantID,
		"tenant_ids":         principal.TenantIDs,
		"created_at":         u.CreatedAt.Format(time.RFC3339),
		"session_token_hash": sessionTokenHash,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// handleUser handles single-user operations: GET, PUT, DELETE (admin only)
func handleUser(w http.ResponseWriter, r *http.Request) {
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
		if !authorizeOrReject(w, r, authz.ActionUsersRead, authz.ResourceRef{}) {
			return
		}
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
		if !authorizeOrReject(w, r, authz.ActionUsersWrite, authz.ResourceRef{}) {
			return
		}
		u, err := serverStore.GetUserByID(ctx, id)
		if err != nil {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		if err := serverStore.DeleteUser(ctx, id); err != nil {
			serverLogger.Error("Failed to delete user", "user_id", id, "username", u.Username, "error", err)
			http.Error(w, "failed to delete user", http.StatusInternalServerError)
			return
		}
		actorType, actorID, actorName, actorTenant := auditActorFromPrincipal(r)
		logAuditEntry(ctx, &storage.AuditEntry{
			ActorType:  actorType,
			ActorID:    actorID,
			ActorName:  actorName,
			TenantID:   actorTenant,
			Action:     "user.delete",
			TargetType: "user",
			TargetID:   u.Username,
			Details:    fmt.Sprintf("Deleted user %s", u.Username),
			Metadata: map[string]interface{}{
				"user_id":    u.ID,
				"role":       u.Role,
				"tenant_ids": u.TenantIDs,
			},
			IPAddress: extractClientIP(r),
		})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		return
	case http.MethodPut, http.MethodPatch:
		if !authorizeOrReject(w, r, authz.ActionUsersWrite, authz.ResourceRef{}) {
			return
		}
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
		originalRole := u.Role
		originalTenantIDs := append([]string{}, u.TenantIDs...)
		originalEmail := u.Email
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
			serverLogger.Error("Failed to update user", "user_id", id, "username", u.Username, "error", err)
			http.Error(w, "failed to update user", http.StatusInternalServerError)
			return
		}
		actorType, actorID, actorName, actorTenant := auditActorFromPrincipal(r)
		logAuditEntry(ctx, &storage.AuditEntry{
			ActorType:  actorType,
			ActorID:    actorID,
			ActorName:  actorName,
			TenantID:   actorTenant,
			Action:     "user.update",
			TargetType: "user",
			TargetID:   u.Username,
			Details:    fmt.Sprintf("Updated user %s", u.Username),
			Metadata: map[string]interface{}{
				"role_before":       originalRole,
				"role_after":        u.Role,
				"tenant_ids_before": originalTenantIDs,
				"tenant_ids_after":  u.TenantIDs,
				"email_before":      originalEmail,
				"email_after":       u.Email,
			},
			IPAddress: extractClientIP(r),
		})
		if req.Password != "" {
			// Validate password against policy
			if serverConfig != nil {
				if err := serverConfig.Security.ValidatePassword(req.Password); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
			}
			if err := serverStore.UpdateUserPassword(ctx, id, req.Password); err != nil {
				serverLogger.Error("Failed to update user password", "user_id", id, "error", err)
				http.Error(w, "failed to update password", http.StatusInternalServerError)
				return
			}
			logAuditEntry(ctx, &storage.AuditEntry{
				ActorType:  actorType,
				ActorID:    actorID,
				ActorName:  actorName,
				TenantID:   actorTenant,
				Action:     "user.password.rotate",
				TargetType: "user",
				TargetID:   u.Username,
				Severity:   storage.AuditSeverityWarn,
				Details:    fmt.Sprintf("Password reset for user %s", u.Username),
				Metadata: map[string]interface{}{
					"user_id": u.ID,
				},
				IPAddress: extractClientIP(r),
			})
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
	if !authorizeOrReject(w, r, authz.ActionSessionsRead, authz.ResourceRef{}) {
		return
	}
	ctx := context.Background()
	sessions, err := serverStore.ListSessions(ctx)
	if err != nil {
		serverLogger.Error("Failed to list sessions", "error", err)
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
	if !authorizeOrReject(w, r, authz.ActionSessionsWrite, authz.ResourceRef{}) {
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
		serverLogger.Error("Failed to delete session", "key", key, "error", err)
		http.Error(w, "failed to delete session", http.StatusInternalServerError)
		return
	}
	actorType, actorID, actorName, actorTenant := auditActorFromPrincipal(r)
	logAuditEntry(ctx, &storage.AuditEntry{
		ActorType:  actorType,
		ActorID:    actorID,
		ActorName:  actorName,
		TenantID:   actorTenant,
		Action:     "session.delete",
		TargetType: "session",
		TargetID:   key,
		Details:    "Session revoked by administrator",
		Metadata: map[string]interface{}{
			"session_key": key,
		},
		IPAddress: extractClientIP(r),
	})
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
	clientIP := getRealIP(r)
	if authRateLimiter != nil {
		if isBlocked, _ := authRateLimiter.IsBlocked(clientIP, req.Email); isBlocked {
			serverLogger.Warn("Password reset rate limited", "email", req.Email, "ip", clientIP)
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
		serverLogger.Error("Failed to create password reset token", "user_id", u.ID, "email", req.Email, "error", err)
		http.Error(w, "failed to create token", http.StatusInternalServerError)
		return
	}
	logAuditEntry(ctx, &storage.AuditEntry{
		ActorType:  storage.AuditActorUser,
		ActorID:    u.Username,
		ActorName:  u.Username,
		TenantID:   u.TenantID,
		Action:     "password.reset.request",
		TargetType: "user",
		TargetID:   u.Username,
		Severity:   storage.AuditSeverityWarn,
		Details:    "Password reset link requested",
		Metadata: map[string]interface{}{
			"email":        strings.ToLower(req.Email),
			"token_prefix": maskSensitiveToken(token),
		},
		IPAddress: extractClientIP(r),
		UserAgent: r.Header.Get("User-Agent"),
	})
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
		serverLogger.Warn("Invalid or expired password reset token used", "error", err, "ip", extractClientIP(r))
		http.Error(w, "invalid or expired token", http.StatusBadRequest)
		return
	}
	userRecord, _ := serverStore.GetUserByID(ctx, userID)
	if err := serverStore.UpdateUserPassword(ctx, userID, req.Password); err != nil {
		serverLogger.Error("Failed to reset password", "user_id", userID, "error", err)
		http.Error(w, "failed to reset password", http.StatusInternalServerError)
		return
	}
	// Optionally delete any other outstanding tokens for this user
	_ = serverStore.DeletePasswordResetToken(ctx, req.Token)
	actorID := fmt.Sprintf("user:%d", userID)
	actorName := ""
	tenantID := ""
	if userRecord != nil {
		actorID = userRecord.Username
		actorName = userRecord.Username
		tenantID = userRecord.TenantID
	}
	logAuditEntry(ctx, &storage.AuditEntry{
		ActorType:  storage.AuditActorUser,
		ActorID:    actorID,
		ActorName:  actorName,
		TenantID:   tenantID,
		Action:     "password.reset.confirm",
		TargetType: "user",
		TargetID:   actorID,
		Severity:   storage.AuditSeverityWarn,
		Details:    "Password reset completed via emailed token",
		Metadata: map[string]interface{}{
			"token_prefix": maskSensitiveToken(req.Token),
		},
		IPAddress: extractClientIP(r),
		UserAgent: r.Header.Get("User-Agent"),
	})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// handleUserInvite sends an invitation email to a new user
func handleUserInvite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorizeOrReject(w, r, authz.ActionUsersWrite, authz.ResourceRef{}) {
		return
	}

	var req struct {
		Email    string `json:"email"`
		Username string `json:"username,omitempty"`
		Role     string `json:"role"`
		TenantID string `json:"tenant_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Email == "" {
		http.Error(w, "email required", http.StatusBadRequest)
		return
	}

	ctx := context.Background()

	// Check if email already has an account
	if existing, _ := serverStore.GetUserByEmail(ctx, req.Email); existing != nil {
		http.Error(w, "a user with this email already exists", http.StatusConflict)
		return
	}

	// Get actor info for audit
	actorType, actorID, actorName, actorTenant := auditActorFromPrincipal(r)

	// Create invitation (48 hour expiry)
	inv := &storage.UserInvitation{
		Email:     req.Email,
		Username:  req.Username,
		Role:      storage.NormalizeRole(req.Role),
		TenantID:  req.TenantID,
		CreatedBy: actorName,
	}
	token, err := serverStore.CreateUserInvitation(ctx, inv, 48*60) // 48 hours
	if err != nil {
		serverLogger.Error("Failed to create user invitation", "email", req.Email, "role", req.Role, "error", err)
		http.Error(w, "failed to create invitation", http.StatusInternalServerError)
		return
	}

	// Build invitation URL
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	host := r.Host
	inviteURL := fmt.Sprintf("%s://%s/accept-invite?token=%s", scheme, host, token)

	// Send email
	subject := "You've been invited to PrintMaster"
	body := fmt.Sprintf(`Hello,

You have been invited to join PrintMaster as a %s.

Click the link below to set up your account:
%s

This invitation expires in 48 hours.

If you did not expect this invitation, you can safely ignore this email.
`, req.Role, inviteURL)

	if err := sendEmail(req.Email, subject, body); err != nil {
		// Log failure but still return success (invitation is created, just email failed)
		logAuditEntry(ctx, &storage.AuditEntry{
			ActorType:  actorType,
			ActorID:    actorID,
			ActorName:  actorName,
			TenantID:   actorTenant,
			Action:     "user.invite.email_failed",
			TargetType: "user",
			TargetID:   req.Email,
			Severity:   storage.AuditSeverityWarn,
			Details:    fmt.Sprintf("Invitation created but email failed: %v", err),
			IPAddress:  extractClientIP(r),
		})
		http.Error(w, fmt.Sprintf("invitation created but email failed: %v", err), http.StatusInternalServerError)
		return
	}

	logAuditEntry(ctx, &storage.AuditEntry{
		ActorType:  actorType,
		ActorID:    actorID,
		ActorName:  actorName,
		TenantID:   actorTenant,
		Action:     "user.invite",
		TargetType: "user",
		TargetID:   req.Email,
		Details:    fmt.Sprintf("Sent invitation to %s with role %s", req.Email, req.Role),
		Metadata: map[string]interface{}{
			"role":      req.Role,
			"tenant_id": req.TenantID,
		},
		IPAddress: extractClientIP(r),
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Invitation sent",
	})
}

// handleInviteValidate validates an invitation token and returns invite details (public endpoint)
// GET /api/v1/users/invite/validate?token=...
func handleInviteValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "token required", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	inv, err := serverStore.GetUserInvitation(ctx, token)
	if err != nil {
		logWarn("Invalid invitation token", "error", err)
		http.Error(w, "Invalid or expired invitation", http.StatusNotFound)
		return
	}

	// Get tenant name if applicable
	var tenantName string
	if inv.TenantID != "" {
		if t, err := serverStore.GetTenant(ctx, inv.TenantID); err == nil && t != nil {
			tenantName = t.Name
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"email":       inv.Email,
		"username":    inv.Username,
		"role":        inv.Role,
		"tenant_id":   inv.TenantID,
		"tenant_name": tenantName,
	})
}

// handleInviteAccept accepts an invitation and creates the user account (public endpoint)
// POST /api/v1/users/invite/accept
func handleInviteAccept(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Token    string `json:"token"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Token == "" || req.Username == "" || req.Password == "" {
		http.Error(w, "token, username, and password are required", http.StatusBadRequest)
		return
	}

	// Validate username
	req.Username = strings.TrimSpace(req.Username)
	if len(req.Username) < 3 {
		http.Error(w, "username must be at least 3 characters", http.StatusBadRequest)
		return
	}

	ctx := context.Background()

	// Validate the invitation token
	inv, err := serverStore.GetUserInvitation(ctx, req.Token)
	if err != nil {
		logWarn("Invalid invitation token on accept", "error", err)
		http.Error(w, "Invalid or expired invitation", http.StatusNotFound)
		return
	}

	// Check if username already exists
	existingUsers, _ := serverStore.ListUsers(ctx)
	for _, u := range existingUsers {
		if strings.EqualFold(u.Username, req.Username) {
			http.Error(w, "Username already taken", http.StatusConflict)
			return
		}
	}

	// Create the user (CreateUser handles password hashing internally)
	user := &storage.User{
		Username:  req.Username,
		Email:     inv.Email,
		Role:      inv.Role,
		TenantID:  inv.TenantID,
		CreatedAt: time.Now(),
	}

	if err := serverStore.CreateUser(ctx, user, req.Password); err != nil {
		logError("Failed to create user from invitation", "error", err, "email", inv.Email)
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	// Mark invitation as used
	if err := serverStore.MarkInvitationUsed(ctx, inv.ID); err != nil {
		logWarn("Failed to mark invitation as used", "error", err, "id", inv.ID)
	}

	// Audit log
	logAuditEntry(ctx, &storage.AuditEntry{
		ActorType:  storage.AuditActorSystem,
		ActorID:    "invitation",
		ActorName:  "Invitation System",
		TenantID:   inv.TenantID,
		Action:     "user.created_from_invite",
		TargetType: "user",
		TargetID:   req.Username,
		Details:    fmt.Sprintf("User %s created from invitation (invited by %s)", req.Username, inv.CreatedBy),
		Metadata: map[string]interface{}{
			"email":      inv.Email,
			"role":       inv.Role,
			"invited_by": inv.CreatedBy,
		},
		IPAddress: extractClientIP(r),
	})

	logInfo("User created from invitation", "username", req.Username, "email", inv.Email, "role", inv.Role)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Account created successfully",
	})
}

// logAuditEntry persists an audit event using the shared storage helper. Default values are applied here
// so callers can focus on describing the action, actor, and optional context.
func logAuditEntry(ctx context.Context, entry *storage.AuditEntry) {
	if entry == nil {
		return
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	if entry.ActorType == "" {
		entry.ActorType = storage.AuditActorSystem
	}
	if entry.ActorID == "" {
		entry.ActorID = "UNKNOWN"
	}
	if entry.Severity == "" {
		entry.Severity = storage.AuditSeverityInfo
	}

	if err := serverStore.SaveAuditEntry(ctx, entry); err != nil {
		logError("Failed to save audit entry", "action", entry.Action, "actor_type", entry.ActorType, "actor_id", entry.ActorID, "error", err)
	}
}

func auditActorFromPrincipal(r *http.Request) (storage.AuditActorType, string, string, string) {
	if r == nil {
		return storage.AuditActorSystem, "UNKNOWN", "", ""
	}
	if principal := getPrincipal(r); principal != nil && principal.User != nil {
		return storage.AuditActorUser, principal.User.Username, principal.User.Username, principal.User.TenantID
	}
	return storage.AuditActorSystem, "UNKNOWN", "", ""
}

func maskSensitiveToken(token string) string {
	if token == "" {
		return ""
	}
	if len(token) <= 8 {
		return token
	}
	return token[:8] + "..."
}

func logRequestAudit(r *http.Request, entry *storage.AuditEntry) {
	if entry == nil {
		return
	}
	ctx := context.Background()
	if r != nil {
		ctx = r.Context()
	}
	actorType, actorID, actorName, actorTenant := auditActorFromPrincipal(r)
	if entry.ActorType == "" {
		entry.ActorType = actorType
	}
	if entry.ActorID == "" || entry.ActorID == "UNKNOWN" {
		entry.ActorID = actorID
	}
	if entry.ActorName == "" {
		entry.ActorName = actorName
	}
	if entry.TenantID == "" {
		entry.TenantID = actorTenant
	}
	if r != nil {
		if entry.IPAddress == "" {
			entry.IPAddress = extractClientIP(r)
		}
		if entry.UserAgent == "" {
			entry.UserAgent = r.Header.Get("User-Agent")
		}
	}
	logAuditEntry(ctx, entry)
}

func metadataWithCommandPayload(payload map[string]interface{}) map[string]interface{} {
	meta := make(map[string]interface{})
	if len(payload) > 0 {
		meta["command_payload"] = payload
	}
	return meta
}

func getCommandStringField(payload map[string]interface{}, key string) string {
	if len(payload) == 0 {
		return ""
	}
	if value, ok := payload[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func logAgentUpdateAuditFromRequest(r *http.Request, agent *storage.Agent, action, details string, metadata map[string]interface{}) {
	entry := buildAgentUpdateAuditEntry(agent, action, details, metadata)
	if entry == nil {
		return
	}
	logRequestAudit(r, entry)
}

func buildAgentUpdateAuditEntry(agent *storage.Agent, action, details string, metadata map[string]interface{}) *storage.AuditEntry {
	if agent == nil {
		return nil
	}
	meta := cloneMetadata(metadata)
	meta["agent_id"] = agent.AgentID
	if agent.Name != "" {
		meta["agent_name"] = agent.Name
	}
	if agent.Hostname != "" {
		meta["hostname"] = agent.Hostname
	}
	if agent.Version != "" {
		meta["agent_version"] = agent.Version
	}

	meta["agent_display_name"] = displayNameForAgent(agent)

	return &storage.AuditEntry{
		Action:     action,
		TargetType: "agent",
		TargetID:   agent.AgentID,
		TenantID:   agent.TenantID,
		Severity:   storage.AuditSeverityInfo,
		Details:    details,
		Metadata:   meta,
	}
}

func cloneMetadata(src map[string]interface{}) map[string]interface{} {
	if len(src) == 0 {
		return map[string]interface{}{}
	}
	dup := make(map[string]interface{}, len(src))
	for k, v := range src {
		dup[k] = v
	}
	return dup
}

func displayNameForAgent(agent *storage.Agent) string {
	if agent == nil {
		return ""
	}
	if name := strings.TrimSpace(agent.Name); name != "" {
		return name
	}
	if host := strings.TrimSpace(agent.Hostname); host != "" {
		return host
	}
	return agent.AgentID
}

// extractClientIP gets the client IP address from the request
// Uses getRealIP which respects X-Forwarded-For when behind a trusted proxy
func extractClientIP(r *http.Request) string {
	return getRealIP(r)
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
	http.HandleFunc("/api/v1/auth/tenant-lookup", handleTenantLookup)
	// Logout (requires valid session)
	http.HandleFunc("/api/v1/auth/logout", requireWebAuth(handleAuthLogout))
	http.HandleFunc("/api/v1/auth/me", requireWebAuth(handleAuthMe))
	// SSO / OIDC provider management
	http.HandleFunc("/api/v1/sso/providers", requireWebAuth(handleOIDCProviders))
	http.HandleFunc("/api/v1/sso/providers/", requireWebAuth(handleOIDCProvider))

	// User management: list/create/update/delete users
	http.HandleFunc("/api/v1/users", requireWebAuth(handleUsers))
	http.HandleFunc("/api/v1/users/invite", requireWebAuth(handleUserInvite)) // Must be before /users/ catch-all
	http.HandleFunc("/api/v1/users/invite/validate", handleInviteValidate)    // Public - validate invitation token
	http.HandleFunc("/api/v1/users/invite/accept", handleInviteAccept)        // Public - accept invitation and create account
	http.HandleFunc("/api/v1/users/", requireWebAuth(handleUser))
	http.HandleFunc("/api/v1/users/password-policy", handlePasswordPolicy)
	// Sessions management: list and revoke sessions
	http.HandleFunc("/api/v1/sessions", requireWebAuth(handleListSessions))
	http.HandleFunc("/api/v1/sessions/", requireWebAuth(handleDeleteSession))

	// Password reset endpoints (public)
	http.HandleFunc("/api/v1/users/reset/request", handlePasswordResetRequest)
	http.HandleFunc("/api/v1/users/reset/confirm", handlePasswordResetConfirm)

	// UI WebSocket endpoint (for live UI liveness/status) - require login
	http.HandleFunc("/api/ws/ui", requireWebAuth(handleUIWebSocket))

	// Agent API (v1)
	http.HandleFunc("/api/v1/agents/register", handleAgentRegister) // No auth - this generates token
	http.HandleFunc("/api/v1/agents/heartbeat", requireAuth(handleAgentHeartbeat))
	http.HandleFunc("/api/v1/agents/device-auth/start", handleAgentDeviceAuthStart)
	http.HandleFunc("/api/v1/agents/device-auth/poll", handleAgentDeviceAuthPoll)
	http.HandleFunc("/api/v1/agents/list", requireWebAuth(handleAgentsList))       // List all agents (for UI)
	http.HandleFunc("/api/v1/agents/command/", requireWebAuth(handleAgentCommand)) // Send command to agent (for UI)
	http.HandleFunc("/api/v1/agents/", requireWebAuth(handleAgentDetails))         // Get single agent details (for UI)
	// Agent WebSocket channel uses its own token handshake; do not require UI auth here.
	http.HandleFunc("/api/v1/agents/ws", func(w http.ResponseWriter, r *http.Request) { // WebSocket endpoint
		handleAgentWebSocket(w, r, serverStore)
	})

	// Agent update endpoints (authenticated by agent token)
	http.HandleFunc("/api/v1/agents/update/manifest", requireAuth(handleAgentUpdateManifest))
	http.HandleFunc("/api/v1/agents/update/download/", requireAuth(handleAgentUpdateDownload))
	http.HandleFunc("/api/v1/agents/update/telemetry", requireAuth(handleAgentUpdateTelemetry))

	// Tenancy & join-token routes. The register-with-token path must remain
	// available even if admins disable tenancy, so register routes always and
	// let the package guard admin handlers via SetEnabled.
	featureEnabled := true
	if cfg != nil {
		featureEnabled = cfg.Tenancy.Enabled
	}
	tenancy.AuthMiddleware = requireWebAuth
	tenancy.SetAuthorizer(func(r *http.Request, action authz.Action, resource authz.ResourceRef) error {
		return authorizeRequest(r, action, resource)
	})
	tenancy.SetServerVersion(Version)
	tenancy.SetLogger(serverLogger)
	tenancy.SetEnabled(featureEnabled)
	tenancy.SetAgentEventSink(func(eventType string, data map[string]interface{}) {
		sseHub.Broadcast(SSEEvent{Type: eventType, Data: data})
	})
	tenancy.SetAuditLogger(logRequestAudit)
	tenancy.RegisterRoutes(serverStore)
	logInfo("Tenancy routes registered", "enabled", featureEnabled)

	settingsAPI := serversettings.NewAPI(serverStore, settingsResolver, serversettings.APIOptions{
		AuthMiddleware: requireWebAuth,
		Authorizer: func(r *http.Request, action authz.Action, resource authz.ResourceRef) error {
			return authorizeRequest(r, action, resource)
		},
		ActorResolver: func(r *http.Request) string {
			if principal := getPrincipal(r); principal != nil && principal.User != nil {
				return principal.User.Username
			}
			return ""
		},
		AuditLogger: logRequestAudit,
		LockedKeysChecker: func() map[string]bool {
			if configSourceTracker == nil {
				return nil
			}
			return configSourceTracker.EnvKeys
		},
	})
	settingsAPI.RegisterRoutes(serversettings.RouteConfig{
		Mux:                 http.DefaultServeMux,
		FeatureEnabled:      featureEnabled,
		RegisterTenantAlias: true,
	})
	logInfo("Settings routes registered", "enabled", featureEnabled)

	updatePolicyAPI := updatepolicy.NewAPI(serverStore, updatepolicy.APIOptions{
		AuthMiddleware: requireWebAuth,
		Authorizer: func(r *http.Request, action authz.Action, resource authz.ResourceRef) error {
			return authorizeRequest(r, action, resource)
		},
		ActorResolver: func(r *http.Request) string {
			if principal := getPrincipal(r); principal != nil && principal.User != nil {
				return principal.User.Username
			}
			return ""
		},
		AuditLogger: logRequestAudit,
	})
	updatePolicyAPI.RegisterRoutes(updatepolicy.RouteConfig{
		Mux:                 http.DefaultServeMux,
		FeatureEnabled:      featureEnabled,
		RegisterTenantAlias: true,
	})
	logInfo("Update policy routes registered", "enabled", featureEnabled)

	// Alerts API routes
	alertsAPI := alertsapi.NewAPI(serverStore, alertsapi.APIOptions{
		AuthMiddleware: requireWebAuth,
		Authorizer: func(r *http.Request, action authz.Action, resource authz.ResourceRef) error {
			return authorizeRequest(r, action, resource)
		},
		ActorResolver: func(r *http.Request) string {
			if principal := getPrincipal(r); principal != nil && principal.User != nil {
				return principal.User.Username
			}
			return ""
		},
		AuditLogger: logRequestAudit,
	})
	alertsAPI.RegisterRoutes(alertsapi.RouteConfig{
		Mux:            http.DefaultServeMux,
		FeatureEnabled: true, // Alerts always enabled
	})
	logInfo("Alerts routes registered", "enabled", true)

	// Reports API routes
	http.HandleFunc("/api/v1/reports", requireWebAuth(handleReports))
	http.HandleFunc("/api/v1/reports/summary", requireWebAuth(handleReportSummary))
	http.HandleFunc("/api/v1/reports/types", requireWebAuth(handleReportTypes))
	http.HandleFunc("/api/v1/reports/", requireWebAuth(handleReport))
	http.HandleFunc("/api/v1/report-schedules", requireWebAuth(handleReportSchedulesCollection))
	http.HandleFunc("/api/v1/report-schedules/", requireWebAuth(handleSchedule))
	http.HandleFunc("/api/v1/report-runs", requireWebAuth(handleReportRunsCollection))
	http.HandleFunc("/api/v1/report-runs/", requireWebAuth(handleReportRunResult))
	logInfo("Reports routes registered", "enabled", true)

	if releaseManager != nil {
		releaseAPI := releases.NewAPI(releaseManager, releases.APIOptions{
			AuthMiddleware: requireWebAuth,
			Authorizer: func(r *http.Request, action authz.Action, resource authz.ResourceRef) error {
				return authorizeRequest(r, action, resource)
			},
		})
		releaseAPI.RegisterRoutes(http.DefaultServeMux)
		logInfo("Release routes registered", "enabled", true)
	} else {
		logWarn("Release routes disabled", "reason", "release manager unavailable")
	}

	// Release sync trigger and artifacts list endpoints
	http.HandleFunc("/api/v1/releases/sync", requireWebAuth(handleReleasesSync))
	http.HandleFunc("/api/v1/releases/artifacts", requireWebAuth(handleReleasesArtifacts))
	http.HandleFunc("/api/v1/releases/latest-agent-version", requireWebAuth(handleLatestAgentVersion))

	// Self-update history endpoint
	http.HandleFunc("/api/v1/selfupdate/runs", requireWebAuth(handleSelfUpdateRuns))
	http.HandleFunc("/api/v1/selfupdate/status", requireWebAuth(handleSelfUpdateStatus))
	http.HandleFunc("/api/v1/selfupdate/check", requireWebAuth(handleSelfUpdateCheck))

	// Server settings (read/write via sanitized API)
	http.HandleFunc("/api/v1/server/settings", requireWebAuth(handleServerSettings))

	// Server settings sources (metadata about which keys are locked by env overrides)
	http.HandleFunc("/api/v1/server/settings/sources", requireWebAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		// Build list of locked keys (those set by environment variables)
		lockedKeys := make([]string, 0)
		effectiveValues := make(map[string]interface{})
		if configSourceTracker != nil && configSourceTracker.EnvKeys != nil {
			lockedKeys = make([]string, 0, len(configSourceTracker.EnvKeys))
			for key := range configSourceTracker.EnvKeys {
				lockedKeys = append(lockedKeys, key)
			}
			// Include effective runtime values for locked keys
			effectiveValues = getEffectiveConfigValues(lockedKeys)
		}
		resp := map[string]interface{}{
			"locked_keys":      lockedKeys,
			"lock_reason":      "environment_variable",
			"effective_values": effectiveValues,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))

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

	// Device management endpoints that proxy to agent (for server UI Details modal)
	http.HandleFunc("/devices/preview", requireWebAuth(handleDevicePreviewProxy))
	http.HandleFunc("/devices/metrics/collect", requireWebAuth(handleDeviceMetricsCollectProxy))

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
	http.HandleFunc("/accept-invite", func(w http.ResponseWriter, r *http.Request) {
		// Serve the invitation acceptance page from embedded web assets
		content, err := webFS.ReadFile("web/accept-invite.html")
		if err != nil {
			logWarn("Accept invite page not found", "err", err)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(content)
	})
	http.HandleFunc("/device-auth/", handleDeviceAuthPage)
	http.HandleFunc("/api/v1/device-auth/requests/", requireWebAuth(handleDeviceAuthRequestRoute))
	http.HandleFunc("/static/", handleStatic)

	// UI metrics summary endpoint (protected)
	http.HandleFunc("/api/metrics", requireWebAuth(handleMetricsSummary))
	http.HandleFunc("/api/metrics/aggregated", requireWebAuth(handleMetricsAggregated))

	// Serve device metrics history from server DB. If the server has historical
	// metrics stored (uploaded by agents) this endpoint will return them. The
	// endpoint supports the same query parameters as the agent: `serial` plus
	// either `since` (RFC3339) or `period` (day|week|month|year). Default period
	// is `week` when nothing is supplied.
	http.HandleFunc("/api/devices/metrics/history", requireWebAuth(handleMetricsHistory))

	// Minimal settings & logs endpoints for the UI (placeholders)
	http.HandleFunc("/api/logs", requireWebAuth(handleLogs))
	http.HandleFunc("/api/logs/clear", requireWebAuth(handleLogsClear))
	http.HandleFunc("/api/audit/logs", requireWebAuth(handleAuditLogs))

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

type healthAttempt struct {
	url      string
	insecure bool
}

// runHealthCheck probes the local /health endpoint over HTTP/HTTPS using configured ports.
// Returns nil on success; otherwise an error summarizing all failed attempts.
func runHealthCheck(configFlag string) error {
	cfg := DefaultConfig()

	// Load configuration if provided so we honor custom ports/env overrides.
	if resolved := config.ResolveConfigPath("SERVER", configFlag); resolved != "" {
		if _, err := os.Stat(resolved); err == nil {
			if loaded, _, loadErr := LoadConfig(resolved); loadErr == nil {
				cfg = loaded
			}
		}
	}

	attempts := make([]healthAttempt, 0, 2)
	if cfg.Server.HTTPPort > 0 {
		attempts = append(attempts, healthAttempt{url: fmt.Sprintf("http://127.0.0.1:%d/health", cfg.Server.HTTPPort)})
	}
	if cfg.Server.HTTPSPort > 0 {
		attempts = append(attempts, healthAttempt{url: fmt.Sprintf("https://127.0.0.1:%d/health", cfg.Server.HTTPSPort), insecure: true})
	}

	// Fallback to defaults if nothing configured
	if len(attempts) == 0 {
		attempts = append(attempts, healthAttempt{url: "http://127.0.0.1:9090/health"})
	}

	var errs []string
	for _, attempt := range attempts {
		if err := probeHealthEndpoint(attempt.url, attempt.insecure); err != nil {
			errMsg := fmt.Sprintf("%s: %v", attempt.url, err)
			errs = append(errs, errMsg)
			continue
		}
		return nil
	}

	if len(errs) == 0 {
		return fmt.Errorf("no health endpoints to probe")
	}

	return fmt.Errorf("%s", strings.Join(errs, "; "))
}

func probeHealthEndpoint(endpoint string, insecure bool) error {
	client := &http.Client{Timeout: 5 * time.Second}
	if insecure {
		// Skip TLS verification for self-signed/local certs used by the server.
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var payload struct {
		Status string `json:"status"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	if strings.ToLower(strings.TrimSpace(payload.Status)) != "healthy" {
		return fmt.Errorf("status=%s", payload.Status)
	}

	return nil
}

// handleSSE streams server-sent events to UI clients for real-time updates
func handleSSE(w http.ResponseWriter, r *http.Request) {
	if !authorizeOrReject(w, r, authz.ActionEventsSubscribe, authz.ResourceRef{}) {
		return
	}
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
	if !authorizeOrReject(w, r, authz.ActionUIWebsocketConnect, authz.ResourceRef{}) {
		return
	}
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
	if !authorizeOrReject(w, r, authz.ActionConfigRead, authz.ResourceRef{}) {
		return
	}
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
		AgentID         string    `json:"agent_id"`
		Timestamp       time.Time `json:"timestamp"`
		Status          string    `json:"status"`
		SettingsVersion string    `json:"settings_version,omitempty"`
		// Version info - sent to keep server DB up to date after agent updates
		Version         string `json:"version,omitempty"`
		ProtocolVersion string `json:"protocol_version,omitempty"`
		Hostname        string `json:"hostname,omitempty"`
		IP              string `json:"ip,omitempty"`
		Platform        string `json:"platform,omitempty"`
		OSVersion       string `json:"os_version,omitempty"`
		GoVersion       string `json:"go_version,omitempty"`
		Architecture    string `json:"architecture,omitempty"`
		BuildType       string `json:"build_type,omitempty"`
		GitCommit       string `json:"git_commit,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Get authenticated agent from context
	agentCtx := r.Context().Value(agentContextKey)
	if agentCtx == nil {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	agent, ok := agentCtx.(*storage.Agent)
	if !ok || agent == nil {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}

	// Update agent using shared heartbeat logic
	ctx := context.Background()
	hbData := &storage.HeartbeatData{
		Status:          req.Status,
		Version:         req.Version,
		ProtocolVersion: req.ProtocolVersion,
		Hostname:        req.Hostname,
		IP:              req.IP,
		Platform:        req.Platform,
		OSVersion:       req.OSVersion,
		GoVersion:       req.GoVersion,
		Architecture:    req.Architecture,
		BuildType:       req.BuildType,
		GitCommit:       req.GitCommit,
	}

	if agentUpdate := hbData.BuildAgentUpdate(agent.AgentID); agentUpdate != nil {
		// Full info update (version/metadata fields present)
		if err := serverStore.UpdateAgentInfo(ctx, agentUpdate); err != nil {
			logWarn("Failed to update agent info", "agent_id", agent.AgentID, "error", err)
		}
	} else {
		// Simple heartbeat - just update last_seen and status
		if err := serverStore.UpdateAgentHeartbeat(ctx, agent.AgentID, req.Status); err != nil {
			logWarn("Failed to update heartbeat", "agent_id", agent.AgentID, "error", err)
		}
	}

	var snapshot serversettings.AgentSnapshot
	if settingsResolver != nil {
		if snap, err := serversettings.BuildAgentSnapshot(ctx, settingsResolver, agent.TenantID, agent.AgentID); err != nil {
			logWarn("Failed to build settings snapshot", "agent_id", agent.AgentID, "error", err)
		} else {
			snapshot = snap
		}
	}

	// Log audit entry for heartbeat (only occasionally to reduce log volume)
	// Could add logic here to only log every Nth heartbeat
	clientIP := extractClientIP(r)
	logAuditEntry(ctx, &storage.AuditEntry{
		ActorType: storage.AuditActorAgent,
		ActorID:   agent.AgentID,
		ActorName: agent.Name,
		TenantID:  agent.TenantID,
		Action:    "heartbeat",
		Details:   fmt.Sprintf("Status: %s", req.Status),
		Metadata: map[string]interface{}{
			"status": req.Status,
		},
		IPAddress: clientIP,
	})

	// Broadcast agent_heartbeat event to UI via SSE
	sseHub.Broadcast(SSEEvent{
		Type: "agent_heartbeat",
		Data: map[string]interface{}{
			"agent_id": agent.AgentID,
			"status":   req.Status,
		},
	})

	logDebug("Heartbeat received", "agent_id", agent.AgentID, "status", req.Status)

	resp := map[string]interface{}{
		"success": true,
	}
	if snapshot.Version != "" {
		resp["settings_version"] = snapshot.Version
		if req.SettingsVersion != snapshot.Version {
			resp["settings_snapshot"] = snapshot
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func deriveAgentConnectionType(agent *storage.Agent) string {
	if agent == nil {
		return "none"
	}
	if isAgentConnectedWS(agent.AgentID) {
		return "ws"
	}
	if !agent.LastSeen.IsZero() && time.Since(agent.LastSeen) <= httpRecencyThreshold {
		return "http"
	}
	return "none"
}

// List all agents - for UI display
func handleAgentsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	if !authorizeOrReject(w, r, authz.ActionAgentsRead, authz.ResourceRef{}) {
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
		ConnectionType string   `json:"connection_type"`
		SiteIDs        []string `json:"site_ids,omitempty"`
	}

	resp := make([]agentView, 0, len(agents))
	// Determine connection type using live WS map and last_seen recency
	for _, agent := range agents {
		agent.Token = "" // Don't expose tokens to UI

		connType := deriveAgentConnectionType(agent)

		// Fetch site IDs for this agent
		siteIDs, _ := serverStore.GetAgentSiteIDs(ctx, agent.AgentID)

		resp = append(resp, agentView{Agent: agent, ConnectionType: connType, SiteIDs: siteIDs})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleAgentCommand sends a command to an agent via WebSocket
// POST /api/v1/agents/command/{agentID}
// Body: {"command": "check_update" | "restart" | ...}
func handleAgentCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	// Extract agent ID from URL path: /api/v1/agents/command/{agentID}
	path := r.URL.Path
	agentID := strings.TrimPrefix(path, "/api/v1/agents/command/")
	if agentID == "" || agentID == path {
		http.Error(w, "Agent ID required", http.StatusBadRequest)
		return
	}

	principal := getPrincipal(r)
	if principal == nil {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}

	var req struct {
		Command string                 `json:"command"`
		Data    map[string]interface{} `json:"data,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Command == "" {
		http.Error(w, "command required", http.StatusBadRequest)
		return
	}

	// Validate agent exists and user has access
	ctx := context.Background()
	agent, err := serverStore.GetAgent(ctx, agentID)
	if err != nil {
		http.Error(w, "Agent not found", http.StatusNotFound)
		return
	}

	scope, ok := tenantScope(principal)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if !tenantAllowed(scope, agent.TenantID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if !authorizeOrReject(w, r, authz.ActionAgentsWrite, authz.ResourceRef{TenantIDs: []string{agent.TenantID}}) {
		return
	}

	// Check if agent is connected via WebSocket
	conn, connected := getAgentWSConnection(agentID)
	if !connected {
		http.Error(w, "Agent not connected via WebSocket", http.StatusServiceUnavailable)
		return
	}

	// Send command via WebSocket
	msg := wscommon.Message{
		Type:      wscommon.MessageTypeCommand,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"command": req.Command,
		},
	}
	if req.Data != nil {
		for k, v := range req.Data {
			msg.Data[k] = v
		}
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		logError("Failed to marshal command message", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	if err := conn.WriteRaw(payload, 10*time.Second); err != nil {
		logWarn("Failed to send command to agent", "agent_id", agentID, "command", req.Command, "error", err)
		http.Error(w, "Failed to send command", http.StatusInternalServerError)
		return
	}

	logInfo("Sent command to agent", "agent_id", agentID, "command", req.Command)

	switch req.Command {
	case "check_update":
		meta := metadataWithCommandPayload(req.Data)
		trigger := getCommandStringField(req.Data, "origin")
		if trigger == "" {
			trigger = "manual"
		}
		meta["trigger"] = trigger
		logAgentUpdateAuditFromRequest(r, agent, "agent.update.check",
			fmt.Sprintf("Update check triggered for %s", displayNameForAgent(agent)), meta)
	case "force_update":
		meta := metadataWithCommandPayload(req.Data)
		reason := getCommandStringField(req.Data, "reason")
		if reason != "" {
			meta["reason"] = reason
		}
		logAgentUpdateAuditFromRequest(r, agent, "agent.update.force",
			fmt.Sprintf("Forced reinstall triggered for %s", displayNameForAgent(agent)), meta)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Command '%s' sent to agent", req.Command),
	})
}

// Get agent details by ID - for UI display (no auth required for now)
// Get agent details and perform management operations scoped by role/tenant.
func handleAgentDetails(w http.ResponseWriter, r *http.Request) {
	// Extract agent ID from URL: /api/v1/agents/{agentID}
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

	ctx := context.Background()
	switch r.Method { //nolint:exhaustive
	case http.MethodGet:
		{
			agent, err := serverStore.GetAgent(ctx, agentID)
			if err != nil || agent == nil {
				logError("Failed to get agent", "agent_id", agentID, "error", err)
				http.Error(w, "Agent not found", http.StatusNotFound)
				return
			}

			if !tenantAllowed(scope, agent.TenantID) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if !authorizeOrReject(w, r, authz.ActionAgentsRead, authz.ResourceRef{TenantIDs: []string{agent.TenantID}}) {
				return
			}

			// Get device count for this agent
			devices, err := serverStore.ListDevices(ctx, agentID)
			if err == nil {
				agent.DeviceCount = len(devices)
			}

			// Remove sensitive token from response
			agent.Token = ""

			connType := deriveAgentConnectionType(agent)

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
			obj["connection_type"] = connType
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
			if err != nil || agent == nil {
				logError("Failed to load agent for update", "agent_id", agentID, "error", err)
				http.Error(w, "Agent not found", http.StatusNotFound)
				return
			}
			if !tenantAllowed(scope, agent.TenantID) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if !authorizeOrReject(w, r, authz.ActionAgentsWrite, authz.ResourceRef{TenantIDs: []string{agent.TenantID}}) {
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

			connType := deriveAgentConnectionType(agent)

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
			obj["connection_type"] = connType
			obj["ws_ping_failures"] = pf
			obj["ws_disconnect_events"] = de

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(obj)
			return
		}

	case http.MethodDelete:
		{
			agent, err := serverStore.GetAgent(ctx, agentID)
			if err != nil || agent == nil {
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
			if !authorizeOrReject(w, r, authz.ActionAgentsDelete, authz.ResourceRef{TenantIDs: []string{agent.TenantID}}) {
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
	if !authorizeOrReject(w, r, authz.ActionProxyAgentConnect, authz.ResourceRef{TenantIDs: []string{agent.TenantID}}) {
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
	if !authorizeOrReject(w, r, authz.ActionProxyDeviceConnect, authz.ResourceRef{TenantIDs: []string{agent.TenantID}}) {
		return
	}

	// Proxy to the AGENT's local device proxy endpoint, not directly to the device.
	// The agent's /proxy/{serial}/... endpoint handles all the complex stuff:
	// - Vendor-specific authentication (Kyocera, Epson, etc.)
	// - Session/cookie management
	// - Content rewriting for relative URLs
	// - Static resource caching
	// This way we leverage the battle-tested agent proxy instead of reimplementing it poorly.
	agentProxyURL := fmt.Sprintf("http://localhost:8080/proxy/%s%s", serial, targetPath)
	proxyThroughWebSocket(w, r, device.AgentID, agentProxyURL)
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
	// Detect if we're proxying to the agent's device proxy endpoint
	// In this case, the agent already handles all content rewriting, so we just need
	// to translate the agent's prefix (/proxy/{serial}/) to server's prefix (/api/v1/proxy/device/{serial}/)
	isAgentDeviceProxy := strings.Contains(targetURL, "/proxy/") && strings.HasPrefix(targetURL, "http://localhost:8080/proxy/")
	var agentProxySerial string
	if isAgentDeviceProxy {
		// Extract serial from agent proxy URL: http://localhost:8080/proxy/{serial}/...
		afterProxy := strings.TrimPrefix(targetURL, "http://localhost:8080/proxy/")
		if idx := strings.Index(afterProxy, "/"); idx > 0 {
			agentProxySerial = afterProxy[:idx]
		} else {
			agentProxySerial = afterProxy
		}
	}

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
		"url", targetURL,
		"is_agent_device_proxy", isAgentDeviceProxy)

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

		// Set response headers from agent
		if respHeaders, ok := resp.Data["headers"].(map[string]interface{}); ok {
			for k, v := range respHeaders {
				if vStr, ok := v.(string); ok {
					w.Header().Set(k, vStr)
				}
			}
		}

		// Add custom header to indicate this is a proxied response
		w.Header().Set("X-PrintMaster-Proxied", "true")
		w.Header().Set("X-PrintMaster-Agent-ID", agentID)

		// Ensure Content-Type is sensible for static assets
		// First, force-fix known problematic extensions (printers often send wrong/empty types)
		if u, err := url.Parse(targetURL); err == nil {
			ext := strings.ToLower(path.Ext(u.Path))
			switch ext {
			case ".jq": // .jq is used by some printers for JavaScript (always force correct type)
				w.Header().Set("Content-Type", "application/javascript")
			}
		}

		contentType := w.Header().Get("Content-Type")
		if contentType == "" || strings.HasPrefix(strings.ToLower(contentType), "text/plain") {
			if u, err := url.Parse(targetURL); err == nil {
				ext := strings.ToLower(path.Ext(u.Path))
				if ext != "" {
					if mt := mime.TypeByExtension(ext); mt != "" {
						w.Header().Set("Content-Type", mt)
						contentType = mt
					} else {
						switch ext {
						case ".js": // standard JavaScript
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
						}
					}
				}
			}
		}

		// Remove server-level security headers that would block proxied content
		w.Header().Del("Content-Security-Policy")
		w.Header().Del("X-Frame-Options")

		// Process response body BEFORE calling WriteHeader (so we can update Content-Length/Content-Encoding)
		var bodyBytes []byte
		if bodyB64, ok := resp.Data["body"].(string); ok {
			var err error
			bodyBytes, err = base64.StdEncoding.DecodeString(bodyB64)
			if err != nil {
				bodyBytes = nil
			}
		}

		// For agent device proxy responses, the agent already did all the content rewriting
		// with /proxy/{serial}/ prefix. We just need to translate that to the server's
		// /api/v1/proxy/device/{serial}/ prefix. Much simpler!
		if isAgentDeviceProxy && agentProxySerial != "" && bodyBytes != nil {
			agentPrefix := "/proxy/" + agentProxySerial
			serverPrefix := "/api/v1/proxy/device/" + agentProxySerial

			// Handle gzip-compressed responses
			if len(bodyBytes) >= 2 && bodyBytes[0] == 0x1f && bodyBytes[1] == 0x8b {
				gr, gerr := gzip.NewReader(bytes.NewReader(bodyBytes))
				if gerr == nil {
					decompressed, rerr := io.ReadAll(gr)
					_ = gr.Close()
					if rerr == nil {
						// Simple string replacement of agent prefix to server prefix
						transformed := bytes.ReplaceAll(decompressed, []byte(agentPrefix), []byte(serverPrefix))
						// Recompress
						var buf bytes.Buffer
						gw := gzip.NewWriter(&buf)
						if _, werr := gw.Write(transformed); werr == nil {
							_ = gw.Close()
							bodyBytes = buf.Bytes()
							w.Header().Set("Content-Encoding", "gzip")
						} else {
							_ = gw.Close()
							w.Header().Del("Content-Encoding")
							bodyBytes = transformed
						}
					}
				}
			} else {
				// Simple string replacement - agent prefix to server prefix
				bodyBytes = bytes.ReplaceAll(bodyBytes, []byte(agentPrefix), []byte(serverPrefix))
				w.Header().Del("Content-Encoding")
			}

			// Also rewrite Location headers for redirects
			if loc := w.Header().Get("Location"); loc != "" {
				w.Header().Set("Location", strings.ReplaceAll(loc, agentPrefix, serverPrefix))
			}

			// Rewrite Set-Cookie paths
			for _, cookie := range w.Header().Values("Set-Cookie") {
				if strings.Contains(cookie, agentPrefix) {
					w.Header().Del("Set-Cookie")
					w.Header().Add("Set-Cookie", strings.ReplaceAll(cookie, agentPrefix, serverPrefix))
				}
			}

			// Update Content-Length and write response
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(bodyBytes)))
			w.WriteHeader(statusCode)
			if bodyBytes != nil {
				w.Write(bodyBytes)
			}
			return
		}

		// === Below is for AGENT UI PROXY only (proxying to agent's own web UI) ===
		// Device proxying now goes through agent's /proxy/{serial}/ endpoint which
		// handles all the complex content rewriting, so we just do prefix translation above.

		// Transform HTML responses (for agent UI proxy)
		if bodyBytes != nil && strings.Contains(strings.ToLower(contentType), "text/html") {
			proxyBase := computeProxyBaseFromRequest(r)

			// Detect gzip by magic bytes
			if len(bodyBytes) >= 2 && bodyBytes[0] == 0x1f && bodyBytes[1] == 0x8b {
				gr, gerr := gzip.NewReader(bytes.NewReader(bodyBytes))
				if gerr == nil {
					decompressed, rerr := io.ReadAll(gr)
					_ = gr.Close()
					if rerr == nil {
						transformed := injectProxyMetaAndBase(decompressed, proxyBase, agentID, targetURL)
						// Recompress
						var buf bytes.Buffer
						gw := gzip.NewWriter(&buf)
						if _, werr := gw.Write(transformed); werr == nil {
							_ = gw.Close()
							bodyBytes = buf.Bytes()
							w.Header().Set("Content-Encoding", "gzip")
						} else {
							_ = gw.Close()
							w.Header().Del("Content-Encoding")
							bodyBytes = transformed
						}
					}
				}
			} else {
				bodyBytes = injectProxyMetaAndBase(bodyBytes, proxyBase, agentID, targetURL)
				// Remove Content-Encoding if it was set (we're sending uncompressed now)
				w.Header().Del("Content-Encoding")
			}
		}

		// Transform JavaScript/JSON responses
		ctLower := strings.ToLower(contentType)
		if bodyBytes != nil && (strings.Contains(ctLower, "javascript") || strings.Contains(ctLower, "application/json")) {
			proxyBase := computeProxyBaseFromRequest(r)

			if len(bodyBytes) >= 2 && bodyBytes[0] == 0x1f && bodyBytes[1] == 0x8b {
				gr, gerr := gzip.NewReader(bytes.NewReader(bodyBytes))
				if gerr == nil {
					decompressed, rerr := io.ReadAll(gr)
					_ = gr.Close()
					if rerr == nil {
						transformed := rewriteProxyJS(decompressed, proxyBase, targetURL)
						var buf bytes.Buffer
						gw := gzip.NewWriter(&buf)
						if _, werr := gw.Write(transformed); werr == nil {
							_ = gw.Close()
							bodyBytes = buf.Bytes()
							w.Header().Set("Content-Encoding", "gzip")
						} else {
							_ = gw.Close()
							w.Header().Del("Content-Encoding")
							bodyBytes = transformed
						}
					}
				}
			} else {
				bodyBytes = rewriteProxyJS(bodyBytes, proxyBase, targetURL)
			}
		}

		// Update Content-Length to match transformed body
		if bodyBytes != nil {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(bodyBytes)))
		} else {
			w.Header().Del("Content-Length")
		}

		// NOW write headers and body
		w.WriteHeader(statusCode)
		if bodyBytes != nil {
			w.Write(bodyBytes)
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

	// Rewrite relative paths with ../ to absolute proxy paths.
	// Many printer web UIs (e.g., Epson at /PRESENTATION/ADVANCED/COMMON/TOP)
	// use paths like ../INFO_PRTINFO/TOP which resolve relative to the current
	// page location. We need to properly resolve these against the target URL.
	bodyStr = rewriteParentRelativePaths(bodyStr, proxyBase, targetURL)

	// NOTE: fetch/XHR URL rewriting is now handled by shared.js which intercepts
	// these calls at runtime and prefixes URLs based on the <base> element.
	// This is cleaner than string-based rewrites which can miss dynamically
	// constructed URLs.

	// Inject meta tag after <head> tag
	headIdx := strings.Index(strings.ToLower(bodyStr), "<head>")
	if headIdx == -1 {
		return []byte(bodyStr)
	}
	insertPos := headIdx + len("<head>")
	// Inject meta tags and <base>. The fetch/XHR interception is now handled
	// by shared.js which detects the <base> element and prefixes URLs automatically.
	// This is much cleaner than injecting inline scripts.
	metaTag := `<meta http-equiv="X-PrintMaster-Proxied" content="true"><meta http-equiv="X-PrintMaster-Agent-ID" content="` + agentID + `">` +
		`<base href="` + proxyBase + `">`

	bodyStr = bodyStr[:insertPos] + metaTag + bodyStr[insertPos:]

	return []byte(bodyStr)
}

// rewriteParentRelativePaths rewrites relative paths that start with ../ to
// absolute proxy paths. This handles patterns like href="../INFO/TOP" which
// are common in printer web UIs (e.g., Epson pages at /PRESENTATION/ADVANCED/COMMON/TOP
// use ../INFO_PRTINFO/TOP to link to /PRESENTATION/ADVANCED/INFO_PRTINFO/TOP).
func rewriteParentRelativePaths(s string, proxyBase string, targetURL string) string {
	// Parse the target URL to get the path for resolving relative references
	var basePath string
	if u, err := url.Parse(targetURL); err == nil {
		basePath = path.Dir(u.Path) // Directory containing the current page
	}

	// Common HTML attribute patterns with ../ relative paths
	patterns := []struct {
		prefix string
		quote  string
	}{
		{`href="`, `"`},
		{`href='`, `'`},
		{`src="`, `"`},
		{`src='`, `'`},
		{`action="`, `"`},
		{`action='`, `'`},
	}

	for _, p := range patterns {
		s = rewriteAttrParentPaths(s, p.prefix, p.quote, proxyBase, basePath)
	}
	return s
}

// rewriteAttrParentPaths finds attribute values starting with ../ and rewrites
// them to absolute proxy paths by properly resolving the relative path.
func rewriteAttrParentPaths(s string, attrPrefix string, quote string, proxyBase string, basePath string) string {
	var result strings.Builder
	result.Grow(len(s))

	searchFrom := 0
	for {
		// Find next attribute
		idx := strings.Index(s[searchFrom:], attrPrefix)
		if idx == -1 {
			result.WriteString(s[searchFrom:])
			break
		}
		absIdx := searchFrom + idx

		// Write everything up to and including the attribute prefix
		result.WriteString(s[searchFrom : absIdx+len(attrPrefix)])

		// Find the value start position
		valueStart := absIdx + len(attrPrefix)

		// Check if value starts with ../
		if strings.HasPrefix(s[valueStart:], "../") {
			// Find the end of the attribute value
			endQuote := strings.Index(s[valueStart:], quote)
			if endQuote == -1 {
				// Malformed HTML, just continue
				searchFrom = valueStart
				continue
			}

			relPath := s[valueStart : valueStart+endQuote]

			// Resolve the relative path against the base path
			// path.Join + path.Clean handles ../ properly
			resolvedPath := path.Clean(path.Join(basePath, relPath))

			// Write the rewritten absolute proxy path
			result.WriteString(proxyBase)
			// Remove leading slash from resolved path since proxyBase ends with /
			if strings.HasPrefix(resolvedPath, "/") {
				result.WriteString(resolvedPath[1:])
			} else {
				result.WriteString(resolvedPath)
			}
			result.WriteString(quote)

			searchFrom = valueStart + endQuote + 1
		} else {
			// Not a ../ path, continue searching
			searchFrom = valueStart
		}
	}

	return result.String()
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

// rewriteProxyJS handles only origin replacement in JavaScript payloads.
// Runtime fetch/XHR interception is handled by the shared.js interceptor
// which detects the <base href> tag and auto-prefixes root-absolute URLs.
func rewriteProxyJS(body []byte, proxyBase string, targetURL string) []byte {
	s := string(body)

	// Replace absolute origin occurrences (hardcoded external URLs)
	if u, err := url.Parse(targetURL); err == nil {
		origin := u.Scheme + "://" + u.Host
		s = strings.ReplaceAll(s, origin, proxyBase)
		protoRel := "//" + u.Host
		s = strings.ReplaceAll(s, protoRel, proxyBase)
	}

	return []byte(s)
}

// handleDevicePreviewProxy proxies /devices/preview requests to the device's agent
// This allows the server UI to use the same "Refresh Details" button as the agent UI
func handleDevicePreviewProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body to get device serial/IP
	var req struct {
		Serial string `json:"serial"`
		IP     string `json:"ip"`
	}
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Look up device to find the agent
	ctx := context.Background()
	device, err := serverStore.GetDevice(ctx, req.Serial)
	if err != nil || device == nil {
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}

	if device.AgentID == "" {
		http.Error(w, "Device has no associated agent", http.StatusBadRequest)
		return
	}

	if !isAgentConnectedWS(device.AgentID) {
		http.Error(w, "Device's agent not connected", http.StatusServiceUnavailable)
		return
	}

	// Proxy to agent's /devices/preview endpoint
	agentURL := "http://localhost:8080/devices/preview"

	// Reconstruct the request with body
	proxyReq, _ := http.NewRequest(http.MethodPost, agentURL, bytes.NewReader(bodyBytes))
	proxyReq.Header.Set("Content-Type", "application/json")

	// Use the WebSocket proxy infrastructure
	proxyThroughWebSocket(w, r, device.AgentID, agentURL)
}

// handleDeviceMetricsCollectProxy proxies /devices/metrics/collect requests to the device's agent
// This allows the server UI to use the same "Collect Metrics" button as the agent UI
func handleDeviceMetricsCollectProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body to get device serial/IP
	var req struct {
		Serial string `json:"serial"`
		IP     string `json:"ip"`
	}
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Look up device to find the agent
	ctx := context.Background()
	device, err := serverStore.GetDevice(ctx, req.Serial)
	if err != nil || device == nil {
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}

	if device.AgentID == "" {
		http.Error(w, "Device has no associated agent", http.StatusBadRequest)
		return
	}

	if !isAgentConnectedWS(device.AgentID) {
		http.Error(w, "Device's agent not connected", http.StatusServiceUnavailable)
		return
	}

	// Proxy to agent's /devices/metrics/collect endpoint
	agentURL := "http://localhost:8080/devices/metrics/collect"

	// Use the WebSocket proxy infrastructure
	proxyThroughWebSocket(w, r, device.AgentID, agentURL)
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
	logAuditEntry(ctx, &storage.AuditEntry{
		ActorType: storage.AuditActorAgent,
		ActorID:   agent.AgentID,
		ActorName: agent.Name,
		TenantID:  agent.TenantID,
		Action:    "upload_devices",
		Details:   fmt.Sprintf("Uploaded %d devices (%d stored)", len(req.Devices), stored),
		Metadata: map[string]interface{}{
			"received": len(req.Devices),
			"stored":   stored,
		},
		IPAddress: clientIP,
	})

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
	if !authorizeOrReject(w, r, authz.ActionDevicesRead, authz.ResourceRef{}) {
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

	// If scoped, filter agents/devices to tenant scope
	if scope != nil {
		agents, err := serverStore.ListAgents(ctx)
		if err != nil {
			logError("Failed to list agents", "error", err)
			http.Error(w, "Failed to list devices", http.StatusInternalServerError)
			return
		}

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

	// Enrich devices with latest metrics (toner levels, page counts)
	enriched := enrichDevicesWithMetrics(ctx, devices)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(enriched)
}

// enrichDevicesWithMetrics adds latest toner levels and page counts to devices
func enrichDevicesWithMetrics(ctx context.Context, devices []*storage.Device) []*storage.DeviceWithMetrics {
	if len(devices) == 0 {
		return []*storage.DeviceWithMetrics{}
	}

	// Collect all serials
	serials := make([]string, 0, len(devices))
	for _, d := range devices {
		if d != nil && d.Serial != "" {
			serials = append(serials, d.Serial)
		}
	}

	// Batch fetch latest metrics
	metricsMap, err := serverStore.GetLatestMetricsBatch(ctx, serials)
	if err != nil {
		logError("Failed to fetch latest metrics for devices", "error", err)
		// Continue without metrics rather than failing
		metricsMap = make(map[string]*storage.MetricsSnapshot)
	}

	// Build enriched list
	result := make([]*storage.DeviceWithMetrics, 0, len(devices))
	for _, d := range devices {
		if d == nil {
			continue
		}
		enriched := &storage.DeviceWithMetrics{Device: *d}
		if m, ok := metricsMap[d.Serial]; ok && m != nil {
			enriched.TonerLevels = m.TonerLevels
			enriched.PageCount = m.PageCount
			enriched.ColorPages = m.ColorPages
			enriched.MonoPages = m.MonoPages
			enriched.ScanCount = m.ScanCount
			enriched.LastMetricsAt = &m.Timestamp
		}
		result = append(result, enriched)
	}

	return result
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
	if !authorizeOrReject(w, r, authz.ActionMetricsSummaryRead, authz.ResourceRef{}) {
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

	// Calculate stats
	stats := map[string]interface{}{
		"agents_count":             len(agents),
		"devices_count":            len(devices),
		"devices_with_metrics_24h": 0, // Placeholder
		"recent":                   []interface{}{},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleMetricsAggregated returns fleet-wide aggregated metrics for the dashboard
func handleMetricsAggregated(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	if !authorizeOrReject(w, r, authz.ActionMetricsSummaryRead, authz.ResourceRef{}) {
		return
	}

	principal := getPrincipal(r)
	if principal == nil {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}

	// Parse time range
	sinceStr := r.URL.Query().Get("since")
	var since time.Time
	if sinceStr != "" {
		var err error
		since, err = time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			// Try parsing without timezone if it fails (some clients might send simplified ISO)
			since, err = time.Parse("2006-01-02T15:04:05", sinceStr)
			if err != nil {
				http.Error(w, "invalid since parameter", http.StatusBadRequest)
				return
			}
		}
	} else {
		// Default to 24h
		since = time.Now().Add(-24 * time.Hour)
	}

	ctx := context.Background()
	tenantIDs := principal.AllowedTenantIDs()

	agg, err := serverStore.GetAggregatedMetrics(ctx, since, tenantIDs)
	if err != nil {
		logError("Failed to get aggregated metrics", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	attachServerStats(ctx, agg)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agg)
}

func attachServerStats(ctx context.Context, agg *storage.AggregatedMetrics) {
	if agg == nil {
		return
	}
	stats := storage.ServerStats{
		GeneratedAt:   time.Now().UTC(),
		Hostname:      "PrintMaster",
		UptimeSeconds: int64(time.Since(processStart).Seconds()),
		Runtime: storage.RuntimeStats{
			GoVersion:    runtime.Version(),
			NumCPU:       runtime.NumCPU(),
			NumGoroutine: runtime.NumGoroutine(),
			StartTime:    processStart.UTC(),
		},
	}
	if host, err := os.Hostname(); err == nil && host != "" {
		stats.Hostname = host
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	stats.Runtime.Memory = storage.MemoryStats{
		HeapAlloc:  mem.HeapAlloc,
		HeapSys:    mem.HeapSys,
		StackInUse: mem.StackInuse,
		StackSys:   mem.StackSys,
		TotalAlloc: mem.TotalAlloc,
		Sys:        mem.Sys,
	}
	if dbStats, err := serverStore.GetDatabaseStats(ctx); err != nil {
		logWarn("Failed to load database stats", "error", err)
	} else {
		stats.Database = dbStats
	}
	agg.Server = stats
}

// handleMetricsHistory returns metrics history for a device from server store.
// Query params: serial (required) and either since (RFC3339) or period (day|week|month|year)
func handleMetricsHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	if !authorizeOrReject(w, r, authz.ActionMetricsHistoryRead, authz.ResourceRef{}) {
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
		if !authorizeOrReject(w, r, authz.ActionMetricsHistoryRead, authz.ResourceRef{TenantIDs: []string{agent.TenantID}}) {
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
	logAuditEntry(ctx, &storage.AuditEntry{
		ActorType: storage.AuditActorAgent,
		ActorID:   agent.AgentID,
		ActorName: agent.Name,
		TenantID:  agent.TenantID,
		Action:    "upload_metrics",
		Details:   fmt.Sprintf("Uploaded %d metric snapshots (%d stored)", len(req.Metrics), stored),
		Metadata: map[string]interface{}{
			"received": len(req.Metrics),
			"stored":   stored,
		},
		IPAddress: clientIP,
	})

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

	if _, ok := ensureInteractiveSession(w, r); !ok {
		return
	}

	tmpl, err := template.ParseFS(webFS, "web/index.html")
	if err != nil {
		logError("Failed to parse index.html template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := struct {
		AssetVersion string
	}{AssetVersion: assetVersionTag}
	if err := tmpl.Execute(w, data); err != nil {
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

// handleSelfUpdateRuns returns recent self-update run history
func handleSelfUpdateRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	if !authorizeOrReject(w, r, authz.ActionLogsRead, authz.ResourceRef{}) {
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	runs, err := serverStore.ListSelfUpdateRuns(r.Context(), limit)
	if err != nil {
		logWarn("Failed to list self-update runs", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"runs": runs})
}

// handleSelfUpdateStatus returns current self-update manager status
func handleSelfUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	if !authorizeOrReject(w, r, authz.ActionSettingsServerRead, authz.ResourceRef{}) {
		return
	}
	var status selfupdate.Status
	if selfUpdateManager != nil {
		status = selfUpdateManager.Status()
	} else {
		status = selfupdate.Status{
			Enabled:        false,
			DisabledReason: "manager not initialized",
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handleSelfUpdateCheck triggers an immediate update check
func handleSelfUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !authorizeOrReject(w, r, authz.ActionSettingsServerWrite, authz.ResourceRef{}) {
		return
	}
	if selfUpdateManager == nil {
		http.Error(w, "self-update manager not available", http.StatusServiceUnavailable)
		return
	}
	if err := selfUpdateManager.CheckNow(r.Context()); err != nil {
		logWarn("Self-update check failed", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"message": "Update check initiated"})
}

// Minimal logs handler - returns an array of recent server log lines (best-effort)
func handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	if !authorizeOrReject(w, r, authz.ActionLogsRead, authz.ResourceRef{}) {
		return
	}

	lines := collectServerLogLines(uiLogLineLimit)
	if len(lines) == 0 {
		lines = []string{"No server logs available yet."}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"logs": lines})
}

// handleLogsClear rotates the server log file and clears the in-memory buffer
func handleLogsClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !authorizeOrReject(w, r, authz.ActionLogsRead, authz.ResourceRef{}) {
		return
	}

	// Clear in-memory buffer if using structured logger
	if serverLogger != nil {
		serverLogger.ClearBuffer()
	}

	// Rotate the log file
	if serverLogDir != "" {
		logPath := filepath.Join(serverLogDir, "server.log")
		rotatedPath := filepath.Join(serverLogDir, fmt.Sprintf("server.log.%s", time.Now().Format("20060102-150405")))
		if err := os.Rename(logPath, rotatedPath); err != nil && !os.IsNotExist(err) {
			logWarn("Failed to rotate log file", "err", err)
		}
		// Create new empty log file
		if f, err := os.Create(logPath); err == nil {
			f.Close()
		}
	}

	logInfo("Logs cleared by user request")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"message": "Logs cleared"})
}

func collectServerLogLines(limit int) []string {
	if limit <= 0 {
		limit = uiLogLineLimit
	}

	if serverLogger != nil {
		entries := serverLogger.GetBuffer()
		if len(entries) > 0 {
			start := 0
			if len(entries) > limit {
				start = len(entries) - limit
			}
			lines := make([]string, 0, len(entries)-start)
			for _, entry := range entries[start:] {
				lines = append(lines, formatUILogEntry(entry))
			}
			return lines
		}
	}

	return tailServerLogFile(limit)
}

func formatUILogEntry(entry logger.LogEntry) string {
	timestamp := entry.Timestamp.Format(time.RFC3339)
	level := logger.LevelToString(entry.Level)
	line := fmt.Sprintf("%s [%s] %s", timestamp, level, entry.Message)

	if len(entry.Context) > 0 {
		keys := make([]string, 0, len(entry.Context))
		for k := range entry.Context {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			line += fmt.Sprintf(" %s=%v", k, entry.Context[k])
		}
	}

	return line
}

func tailServerLogFile(limit int) []string {
	if serverLogDir == "" {
		return nil
	}
	logPath := filepath.Join(serverLogDir, "server.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		return nil
	}
	rows := strings.Split(string(data), "\n")
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		trimmed := strings.TrimSpace(row)
		if trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	if len(lines) == 0 {
		return nil
	}
	if limit > 0 && len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines
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
	if !authorizeOrReject(w, r, authz.ActionAuditLogsRead, authz.ResourceRef{}) {
		return
	}

	query := r.URL.Query()
	actorID := strings.TrimSpace(query.Get("actor_id"))
	if actorID == "" {
		actorID = strings.TrimSpace(query.Get("agent_id"))
	}
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

	entries, err := serverStore.GetAuditLog(r.Context(), actorID, since)
	if err != nil {
		logError("Failed to load audit log", "actor_id", actorID, "error", err)
		http.Error(w, "failed to load audit log", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"entries":  entries,
		"actor_id": actorID,
		"since":    since.UTC(),
	}

	if actorID != "" {
		response["agent_id"] = actorID // legacy key for pre-actor UI clients
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logError("Failed to encode audit log response", "error", err)
	}
}

// ===== Server Settings API =====

type serverSettingsRequest struct {
	Server   *serverSettingsServerSection   `json:"server"`
	Security *serverSettingsSecuritySection `json:"security"`
	TLS      *serverSettingsTLSSection      `json:"tls"`
	Logging  *serverSettingsLoggingSection  `json:"logging"`
	SMTP     *serverSettingsSMTPSection     `json:"smtp"`
}

type serverSettingsServerSection struct {
	HTTPPort            *int    `json:"http_port"`
	HTTPSPort           *int    `json:"https_port"`
	BindAddress         *string `json:"bind_address"`
	BehindProxy         *bool   `json:"behind_proxy"`
	ProxyUseHTTPS       *bool   `json:"proxy_use_https"`
	AutoApproveAgents   *bool   `json:"auto_approve_agents"`
	AgentTimeoutMinutes *int    `json:"agent_timeout_minutes"`
}

type serverSettingsSecuritySection struct {
	RateLimitEnabled       *bool `json:"rate_limit_enabled"`
	RateLimitMaxAttempts   *int  `json:"rate_limit_max_attempts"`
	RateLimitBlockMinutes  *int  `json:"rate_limit_block_minutes"`
	RateLimitWindowMinutes *int  `json:"rate_limit_window_minutes"`
}

type serverSettingsTLSSection struct {
	Mode        *string                           `json:"mode"`
	Domain      *string                           `json:"domain"`
	CertPath    *string                           `json:"cert_path"`
	KeyPath     *string                           `json:"key_path"`
	LetsEncrypt *serverSettingsLetsEncryptSection `json:"letsencrypt"`
}

type serverSettingsLetsEncryptSection struct {
	Domain    *string `json:"domain"`
	Email     *string `json:"email"`
	CacheDir  *string `json:"cache_dir"`
	AcceptTOS *bool   `json:"accept_tos"`
}

type serverSettingsLoggingSection struct {
	Level *string `json:"level"`
}

type serverSettingsSMTPSection struct {
	Enabled *bool   `json:"enabled"`
	Host    *string `json:"host"`
	Port    *int    `json:"port"`
	User    *string `json:"user"`
	Pass    *string `json:"pass"`
	From    *string `json:"from"`
}

type serverSettingsUpdateResult struct {
	ChangedKeys     []string `json:"changed_keys"`
	RestartRequired bool     `json:"restart_required"`
}

func handleServerSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !authorizeOrReject(w, r, authz.ActionSettingsServerRead, authz.ResourceRef{}) {
			return
		}
		resp := buildServerSettingsResponse(serverConfig)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	case http.MethodPut:
		if !authorizeOrReject(w, r, authz.ActionSettingsServerWrite, authz.ResourceRef{}) {
			return
		}
		var req serverSettingsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON payload", http.StatusBadRequest)
			return
		}
		result, err := applyServerSettings(serverConfig, &req)
		if err != nil {
			logWarn("Server settings update failed", "error", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(result.ChangedKeys) > 0 {
			actorType, actorID, actorName, actorTenant := auditActorFromPrincipal(r)
			logAuditEntry(r.Context(), &storage.AuditEntry{
				ActorType: actorType,
				ActorID:   actorID,
				ActorName: actorName,
				TenantID:  actorTenant,
				Action:    "settings.server.update",
				Details:   fmt.Sprintf("changed keys: %s", strings.Join(result.ChangedKeys, ", ")),
				IPAddress: extractClientIP(r),
				UserAgent: r.Header.Get("User-Agent"),
				Severity:  storage.AuditSeverityInfo,
			})
		}
		resp := map[string]interface{}{
			"settings":         buildServerSettingsResponse(serverConfig),
			"restart_required": result.RestartRequired,
			"changed_keys":     result.ChangedKeys,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func buildServerSettingsResponse(cfg *Config) map[string]interface{} {
	if cfg == nil {
		return map[string]interface{}{"error": "config not loaded"}
	}
	return map[string]interface{}{
		"version":         Version,
		"tenancy_enabled": cfg.Tenancy.Enabled,
		"config_source":   loadedConfigPath,
		"using_defaults":  usingDefaultConfig,
		"server": map[string]interface{}{
			"http_port":             cfg.Server.HTTPPort,
			"https_port":            cfg.Server.HTTPSPort,
			"bind_address":          cfg.Server.BindAddress,
			"behind_proxy":          cfg.Server.BehindProxy,
			"proxy_use_https":       cfg.Server.ProxyUseHTTPS,
			"auto_approve_agents":   cfg.Server.AutoApproveAgents,
			"agent_timeout_minutes": cfg.Server.AgentTimeoutMinutes,
		},
		"security": map[string]interface{}{
			"rate_limit_enabled":        cfg.Security.RateLimitEnabled,
			"rate_limit_max_attempts":   cfg.Security.RateLimitMaxAttempts,
			"rate_limit_block_minutes":  cfg.Security.RateLimitBlockMinutes,
			"rate_limit_window_minutes": cfg.Security.RateLimitWindowMinutes,
		},
		"tls": map[string]interface{}{
			"mode":                   cfg.TLS.Mode,
			"domain":                 cfg.TLS.Domain,
			"cert_path":              cfg.TLS.CertPath,
			"key_path":               cfg.TLS.KeyPath,
			"letsencrypt_domain":     cfg.TLS.LetsEncrypt.Domain,
			"letsencrypt_email":      cfg.TLS.LetsEncrypt.Email,
			"letsencrypt_cache_dir":  cfg.TLS.LetsEncrypt.CacheDir,
			"letsencrypt_accept_tos": cfg.TLS.LetsEncrypt.AcceptTOS,
		},
		"database": map[string]interface{}{
			"path": cfg.Database.Path,
		},
		"logging": map[string]interface{}{
			"level": cfg.Logging.Level,
		},
		"smtp": map[string]interface{}{
			"enabled": cfg.SMTP.Enabled,
			"host":    cfg.SMTP.Host,
			"port":    cfg.SMTP.Port,
			"user":    cfg.SMTP.User,
			"from":    cfg.SMTP.From,
		},
	}
}

func isConfigKeyLocked(key string) bool {
	if configSourceTracker == nil || configSourceTracker.EnvKeys == nil {
		return false
	}
	return configSourceTracker.EnvKeys[key]
}

// getEffectiveConfigValues returns the runtime config values for the specified keys.
// Used to show actual values for environment-locked settings in the UI.
func getEffectiveConfigValues(keys []string) map[string]interface{} {
	values := make(map[string]interface{})
	if serverConfig == nil {
		return values
	}
	cfg := serverConfig

	for _, key := range keys {
		switch key {
		// Server settings
		case "server.http_port":
			values[key] = cfg.Server.HTTPPort
		case "server.https_port":
			values[key] = cfg.Server.HTTPSPort
		case "server.behind_proxy":
			values[key] = cfg.Server.BehindProxy
		case "server.proxy_use_https":
			values[key] = cfg.Server.ProxyUseHTTPS
		case "server.bind_address":
			values[key] = cfg.Server.BindAddress
		case "server.auto_approve_agents":
			values[key] = cfg.Server.AutoApproveAgents
		case "server.agent_timeout_minutes":
			values[key] = cfg.Server.AgentTimeoutMinutes
		case "server.self_update_enabled":
			values[key] = cfg.Server.SelfUpdateEnabled

		// Releases settings
		case "releases.max_releases":
			values[key] = cfg.Releases.MaxReleases
		case "releases.poll_interval_minutes":
			values[key] = cfg.Releases.PollIntervalMinutes

		// Self update settings
		case "self_update.channel":
			values[key] = cfg.SelfUpdate.Channel
		case "self_update.max_artifacts":
			values[key] = cfg.SelfUpdate.MaxArtifacts
		case "self_update.check_interval_minutes":
			values[key] = cfg.SelfUpdate.CheckIntervalMinutes

		// TLS settings
		case "tls.mode":
			values[key] = cfg.TLS.Mode
		case "tls.cert_path":
			values[key] = cfg.TLS.CertPath
		case "tls.key_path":
			values[key] = cfg.TLS.KeyPath
		case "tls.letsencrypt.domain":
			values[key] = cfg.TLS.LetsEncrypt.Domain
		case "tls.letsencrypt.email":
			values[key] = cfg.TLS.LetsEncrypt.Email
		case "tls.letsencrypt.accept_tos":
			values[key] = cfg.TLS.LetsEncrypt.AcceptTOS

		// SMTP settings
		case "smtp.enabled":
			values[key] = cfg.SMTP.Enabled
		case "smtp.host":
			values[key] = cfg.SMTP.Host
		case "smtp.port":
			values[key] = cfg.SMTP.Port
		case "smtp.user":
			values[key] = cfg.SMTP.User
		case "smtp.from":
			values[key] = cfg.SMTP.From
		// Note: smtp.pass is intentionally not exposed for security

		// Logging settings
		case "logging.level":
			values[key] = cfg.Logging.Level

		// Database settings
		case "database.path":
			values[key] = cfg.Database.Path

		// Tenancy settings
		case "tenancy.enabled":
			values[key] = cfg.Tenancy.Enabled
		}
	}
	return values
}

func ensureConfigKeyEditable(key string) error {
	if isConfigKeyLocked(key) {
		return fmt.Errorf("%s is managed by environment variables", key)
	}
	return nil
}

func applyServerSettings(cfg *Config, req *serverSettingsRequest) (*serverSettingsUpdateResult, error) {
	if cfg == nil {
		return nil, fmt.Errorf("server configuration not loaded")
	}
	if req == nil {
		return &serverSettingsUpdateResult{}, nil
	}
	original := *cfg
	changedKeys := make([]string, 0)
	restartRequired := false

	var markChanged = func(key string, needsRestart bool) {
		changedKeys = append(changedKeys, key)
		if needsRestart {
			restartRequired = true
		}
	}

	if section := req.Server; section != nil {
		if section.HTTPPort != nil {
			if err := ensureConfigKeyEditable("server.http_port"); err != nil {
				*cfg = original
				return nil, err
			}
			if err := validatePort(*section.HTTPPort); err != nil {
				*cfg = original
				return nil, fmt.Errorf("invalid server.http_port: %w", err)
			}
			if cfg.Server.HTTPPort != *section.HTTPPort {
				cfg.Server.HTTPPort = *section.HTTPPort
				markChanged("server.http_port", true)
			}
		}
		if section.HTTPSPort != nil {
			if err := ensureConfigKeyEditable("server.https_port"); err != nil {
				*cfg = original
				return nil, err
			}
			if err := validatePort(*section.HTTPSPort); err != nil {
				*cfg = original
				return nil, fmt.Errorf("invalid server.https_port: %w", err)
			}
			if cfg.Server.HTTPSPort != *section.HTTPSPort {
				cfg.Server.HTTPSPort = *section.HTTPSPort
				markChanged("server.https_port", true)
			}
		}
		if section.BindAddress != nil {
			if err := ensureConfigKeyEditable("server.bind_address"); err != nil {
				*cfg = original
				return nil, err
			}
			value := strings.TrimSpace(*section.BindAddress)
			if value == "" {
				*cfg = original
				return nil, fmt.Errorf("server.bind_address cannot be empty")
			}
			if cfg.Server.BindAddress != value {
				cfg.Server.BindAddress = value
				markChanged("server.bind_address", true)
			}
		}
		if section.BehindProxy != nil {
			if err := ensureConfigKeyEditable("server.behind_proxy"); err != nil {
				*cfg = original
				return nil, err
			}
			if cfg.Server.BehindProxy != *section.BehindProxy {
				cfg.Server.BehindProxy = *section.BehindProxy
				markChanged("server.behind_proxy", true)
			}
		}
		if section.ProxyUseHTTPS != nil {
			if err := ensureConfigKeyEditable("server.proxy_use_https"); err != nil {
				*cfg = original
				return nil, err
			}
			if cfg.Server.ProxyUseHTTPS != *section.ProxyUseHTTPS {
				cfg.Server.ProxyUseHTTPS = *section.ProxyUseHTTPS
				markChanged("server.proxy_use_https", true)
			}
		}
		if section.AutoApproveAgents != nil {
			if err := ensureConfigKeyEditable("server.auto_approve_agents"); err != nil {
				*cfg = original
				return nil, err
			}
			if cfg.Server.AutoApproveAgents != *section.AutoApproveAgents {
				cfg.Server.AutoApproveAgents = *section.AutoApproveAgents
				markChanged("server.auto_approve_agents", true)
			}
		}
		if section.AgentTimeoutMinutes != nil {
			if err := ensureConfigKeyEditable("server.agent_timeout_minutes"); err != nil {
				*cfg = original
				return nil, err
			}
			if *section.AgentTimeoutMinutes <= 0 {
				*cfg = original
				return nil, fmt.Errorf("server.agent_timeout_minutes must be positive")
			}
			if cfg.Server.AgentTimeoutMinutes != *section.AgentTimeoutMinutes {
				cfg.Server.AgentTimeoutMinutes = *section.AgentTimeoutMinutes
				markChanged("server.agent_timeout_minutes", true)
			}
		}
	}

	if section := req.Security; section != nil {
		if section.RateLimitEnabled != nil {
			if err := ensureConfigKeyEditable("security.rate_limit_enabled"); err != nil {
				*cfg = original
				return nil, err
			}
			if cfg.Security.RateLimitEnabled != *section.RateLimitEnabled {
				cfg.Security.RateLimitEnabled = *section.RateLimitEnabled
				markChanged("security.rate_limit_enabled", true)
			}
		}
		if section.RateLimitMaxAttempts != nil {
			if err := ensureConfigKeyEditable("security.rate_limit_max_attempts"); err != nil {
				*cfg = original
				return nil, err
			}
			if *section.RateLimitMaxAttempts <= 0 {
				*cfg = original
				return nil, fmt.Errorf("security.rate_limit_max_attempts must be positive")
			}
			if cfg.Security.RateLimitMaxAttempts != *section.RateLimitMaxAttempts {
				cfg.Security.RateLimitMaxAttempts = *section.RateLimitMaxAttempts
				markChanged("security.rate_limit_max_attempts", true)
			}
		}
		if section.RateLimitBlockMinutes != nil {
			if err := ensureConfigKeyEditable("security.rate_limit_block_minutes"); err != nil {
				*cfg = original
				return nil, err
			}
			if *section.RateLimitBlockMinutes <= 0 {
				*cfg = original
				return nil, fmt.Errorf("security.rate_limit_block_minutes must be positive")
			}
			if cfg.Security.RateLimitBlockMinutes != *section.RateLimitBlockMinutes {
				cfg.Security.RateLimitBlockMinutes = *section.RateLimitBlockMinutes
				markChanged("security.rate_limit_block_minutes", true)
			}
		}
		if section.RateLimitWindowMinutes != nil {
			if err := ensureConfigKeyEditable("security.rate_limit_window_minutes"); err != nil {
				*cfg = original
				return nil, err
			}
			if *section.RateLimitWindowMinutes <= 0 {
				*cfg = original
				return nil, fmt.Errorf("security.rate_limit_window_minutes must be positive")
			}
			if cfg.Security.RateLimitWindowMinutes != *section.RateLimitWindowMinutes {
				cfg.Security.RateLimitWindowMinutes = *section.RateLimitWindowMinutes
				markChanged("security.rate_limit_window_minutes", true)
			}
		}
	}

	if section := req.TLS; section != nil {
		if section.Mode != nil {
			if err := ensureConfigKeyEditable("tls.mode"); err != nil {
				*cfg = original
				return nil, err
			}
			mode := strings.ToLower(strings.TrimSpace(*section.Mode))
			switch mode {
			case "self-signed", "custom", "letsencrypt":
			default:
				*cfg = original
				return nil, fmt.Errorf("unsupported tls.mode: %s", mode)
			}
			if cfg.TLS.Mode != mode {
				cfg.TLS.Mode = mode
				markChanged("tls.mode", true)
			}
		}
		if section.Domain != nil {
			if err := ensureConfigKeyEditable("tls.domain"); err != nil {
				*cfg = original
				return nil, err
			}
			domain := strings.TrimSpace(*section.Domain)
			if domain == "" {
				*cfg = original
				return nil, fmt.Errorf("tls.domain cannot be empty")
			}
			if cfg.TLS.Domain != domain {
				cfg.TLS.Domain = domain
				markChanged("tls.domain", true)
			}
		}
		if section.CertPath != nil {
			if err := ensureConfigKeyEditable("tls.cert_path"); err != nil {
				*cfg = original
				return nil, err
			}
			path := strings.TrimSpace(*section.CertPath)
			if cfg.TLS.CertPath != path {
				cfg.TLS.CertPath = path
				markChanged("tls.cert_path", true)
			}
		}
		if section.KeyPath != nil {
			if err := ensureConfigKeyEditable("tls.key_path"); err != nil {
				*cfg = original
				return nil, err
			}
			path := strings.TrimSpace(*section.KeyPath)
			if cfg.TLS.KeyPath != path {
				cfg.TLS.KeyPath = path
				markChanged("tls.key_path", true)
			}
		}
		if le := section.LetsEncrypt; le != nil {
			if le.Domain != nil {
				if err := ensureConfigKeyEditable("tls.letsencrypt.domain"); err != nil {
					*cfg = original
					return nil, err
				}
				domain := strings.TrimSpace(*le.Domain)
				if domain == "" {
					*cfg = original
					return nil, fmt.Errorf("tls.letsencrypt.domain cannot be empty")
				}
				if cfg.TLS.LetsEncrypt.Domain != domain {
					cfg.TLS.LetsEncrypt.Domain = domain
					markChanged("tls.letsencrypt.domain", true)
				}
			}
			if le.Email != nil {
				if err := ensureConfigKeyEditable("tls.letsencrypt.email"); err != nil {
					*cfg = original
					return nil, err
				}
				email := strings.TrimSpace(*le.Email)
				if email == "" {
					*cfg = original
					return nil, fmt.Errorf("tls.letsencrypt.email cannot be empty")
				}
				if cfg.TLS.LetsEncrypt.Email != email {
					cfg.TLS.LetsEncrypt.Email = email
					markChanged("tls.letsencrypt.email", true)
				}
			}
			if le.CacheDir != nil {
				if err := ensureConfigKeyEditable("tls.letsencrypt.cache_dir"); err != nil {
					*cfg = original
					return nil, err
				}
				cache := strings.TrimSpace(*le.CacheDir)
				if cache == "" {
					cache = cfg.TLS.LetsEncrypt.CacheDir
				}
				if cfg.TLS.LetsEncrypt.CacheDir != cache {
					cfg.TLS.LetsEncrypt.CacheDir = cache
					markChanged("tls.letsencrypt.cache_dir", true)
				}
			}
			if le.AcceptTOS != nil {
				if err := ensureConfigKeyEditable("tls.letsencrypt.accept_tos"); err != nil {
					*cfg = original
					return nil, err
				}
				if cfg.TLS.LetsEncrypt.AcceptTOS != *le.AcceptTOS {
					cfg.TLS.LetsEncrypt.AcceptTOS = *le.AcceptTOS
					markChanged("tls.letsencrypt.accept_tos", true)
				}
			}
		}
	}

	if section := req.Logging; section != nil && section.Level != nil {
		if err := ensureConfigKeyEditable("logging.level"); err != nil {
			*cfg = original
			return nil, err
		}
		level := strings.ToUpper(strings.TrimSpace(*section.Level))
		switch level {
		case "ERROR", "WARN", "WARNING", "INFO", "DEBUG", "TRACE":
		default:
			*cfg = original
			return nil, fmt.Errorf("invalid logging.level: %s", level)
		}
		if !strings.EqualFold(cfg.Logging.Level, level) {
			cfg.Logging.Level = strings.ToLower(level)
			changedKeys = append(changedKeys, "logging.level")
			if serverLogger != nil {
				serverLogger.SetLevel(logger.LevelFromString(level))
			}
		}
	}

	if section := req.SMTP; section != nil {
		if section.Enabled != nil {
			if err := ensureConfigKeyEditable("smtp.enabled"); err != nil {
				*cfg = original
				return nil, err
			}
			if cfg.SMTP.Enabled != *section.Enabled {
				cfg.SMTP.Enabled = *section.Enabled
				markChanged("smtp.enabled", true)
			}
		}
		if section.Host != nil {
			if err := ensureConfigKeyEditable("smtp.host"); err != nil {
				*cfg = original
				return nil, err
			}
			host := strings.TrimSpace(*section.Host)
			if cfg.SMTP.Host != host {
				cfg.SMTP.Host = host
				markChanged("smtp.host", true)
			}
		}
		if section.Port != nil {
			if err := ensureConfigKeyEditable("smtp.port"); err != nil {
				*cfg = original
				return nil, err
			}
			if *section.Port <= 0 || *section.Port > 65535 {
				*cfg = original
				return nil, fmt.Errorf("smtp.port must be between 1 and 65535")
			}
			if cfg.SMTP.Port != *section.Port {
				cfg.SMTP.Port = *section.Port
				markChanged("smtp.port", true)
			}
		}
		if section.User != nil {
			if err := ensureConfigKeyEditable("smtp.user"); err != nil {
				*cfg = original
				return nil, err
			}
			user := strings.TrimSpace(*section.User)
			if cfg.SMTP.User != user {
				cfg.SMTP.User = user
				markChanged("smtp.user", true)
			}
		}
		if section.Pass != nil {
			if err := ensureConfigKeyEditable("smtp.pass"); err != nil {
				*cfg = original
				return nil, err
			}
			pass := strings.TrimSpace(*section.Pass)
			if cfg.SMTP.Pass != pass {
				cfg.SMTP.Pass = pass
				markChanged("smtp.pass", true)
			}
		}
		if section.From != nil {
			if err := ensureConfigKeyEditable("smtp.from"); err != nil {
				*cfg = original
				return nil, err
			}
			from := strings.TrimSpace(*section.From)
			if cfg.SMTP.From != from {
				cfg.SMTP.From = from
				markChanged("smtp.from", true)
			}
		}
	}

	if len(changedKeys) == 0 {
		return &serverSettingsUpdateResult{ChangedKeys: changedKeys, RestartRequired: restartRequired}, nil
	}

	if loadedConfigPath == "" {
		*cfg = original
		return nil, fmt.Errorf("config path not set; cannot persist changes")
	}

	if err := config.WriteTOML(loadedConfigPath, cfg); err != nil {
		*cfg = original
		return nil, fmt.Errorf("failed to persist configuration: %w", err)
	}

	logInfo("Server settings updated", "changed", strings.Join(changedKeys, ","), "restart_required", restartRequired)
	return &serverSettingsUpdateResult{ChangedKeys: changedKeys, RestartRequired: restartRequired}, nil
}

func validatePort(port int) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	return nil
}

func startInstallerCleanupWorker(ctx context.Context, store storage.Store, cacheDir string) {
	if store == nil || cacheDir == "" {
		return
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		logWarn("Installer cleanup worker disabled; unable to prepare cache directory", "dir", cacheDir, "error", err)
		return
	}
	runCleanup := func() {
		runCtx, cancel := context.WithTimeout(context.Background(), installerCleanupRunTimeout)
		defer cancel()
		cleanupInstallerBundles(runCtx, store)
		cleanupInstallerCacheFiles(runCtx, store, cacheDir)
	}
	go func() {
		logInfo("Installer cleanup worker started", "interval", installerCleanupInterval.String(), "cache_dir", cacheDir)
		runCleanup()
		ticker := time.NewTicker(installerCleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				logDebug("Installer cleanup worker stopping")
				return
			case <-ticker.C:
				runCleanup()
			}
		}
	}()
}

func cleanupInstallerBundles(ctx context.Context, store storage.Store) {
	if store == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cutoff := time.Now().UTC()
	deleted, err := store.DeleteExpiredInstallerBundles(ctx, cutoff)
	if err != nil {
		logWarn("Failed to purge expired installer bundles", "error", err)
		return
	}
	if deleted > 0 {
		logInfo("Purged expired installer bundles", "count", deleted)
	} else {
		logDebug("No expired installer bundles found during cleanup")
	}
}

func cleanupInstallerCacheFiles(ctx context.Context, store storage.Store, cacheDir string) {
	if cacheDir == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, err := os.Stat(cacheDir); errors.Is(err, os.ErrNotExist) {
		return
	} else if err != nil {
		logWarn("Installer cache cleanup skipped", "dir", cacheDir, "error", err)
		return
	}
	paths, err := collectActiveInstallerBundlePaths(ctx, store)
	if err != nil {
		logWarn("Installer cache cleanup skipped; failed to list bundles", "error", err)
		return
	}
	removedFiles := 0
	err = filepath.WalkDir(cacheDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			logDebug("Installer cache walk error", "path", path, "error", walkErr)
			return nil
		}
		if d.IsDir() {
			return nil
		}
		absPath, err := filepath.Abs(path)
		if err == nil {
			if _, ok := paths[absPath]; ok {
				return nil
			}
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if time.Since(info.ModTime()) < installerCleanupGracePeriod {
			return nil
		}
		if err := os.Remove(path); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				logDebug("Failed to remove stale installer cache file", "path", path, "error", err)
			}
			return nil
		}
		removedFiles++
		return nil
	})
	if err != nil {
		logWarn("Installer cache cleanup walk failed", "dir", cacheDir, "error", err)
	}
	removedDirs := removeEmptyInstallerDirs(cacheDir)
	if removedFiles > 0 || removedDirs > 0 {
		logInfo("Installer cache cleanup removed entries", "files", removedFiles, "dirs", removedDirs)
	} else {
		logDebug("Installer cache cleanup had nothing to remove")
	}
}

func collectActiveInstallerBundlePaths(ctx context.Context, store storage.Store) (map[string]struct{}, error) {
	paths := make(map[string]struct{})
	if store == nil {
		return paths, nil
	}
	listCtx, cancel := context.WithTimeout(ctx, installerCleanupListTimeout)
	defer cancel()
	bundles, err := store.ListInstallerBundles(listCtx, "", 0)
	if err != nil {
		return nil, err
	}
	for _, bundle := range bundles {
		if bundle == nil || bundle.BundlePath == "" {
			continue
		}
		absPath, err := filepath.Abs(bundle.BundlePath)
		if err != nil {
			continue
		}
		paths[absPath] = struct{}{}
	}
	return paths, nil
}

func removeEmptyInstallerDirs(cacheDir string) int {
	dirs := make([]string, 0, 32)
	_ = filepath.WalkDir(cacheDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			dirs = append(dirs, path)
		}
		return nil
	})
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	removed := 0
	for _, dir := range dirs {
		if dir == cacheDir {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		if len(entries) == 0 {
			if err := os.Remove(dir); err == nil {
				removed++
			} else if !errors.Is(err, os.ErrNotExist) {
				logDebug("Failed to remove empty installer cache directory", "dir", dir, "error", err)
			}
		}
	}
	return removed
}

// handleAgentUpdateManifest returns the latest update manifest for the requesting agent.
func handleAgentUpdateManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		AgentID   string `json:"agent_id"`
		Component string `json:"component"`
		Platform  string `json:"platform"`
		Arch      string `json:"arch"`
		Channel   string `json:"channel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Component == "" {
		req.Component = "agent"
	}
	if req.Channel == "" {
		req.Channel = "stable"
	}

	// Fetch matching manifest from release manager
	if releaseManager == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "release manager not initialized",
		})
		return
	}

	manifest, err := releaseManager.GetLatestManifest(r.Context(), req.Component, req.Platform, req.Arch, req.Channel)
	if err != nil {
		logWarn("Failed to get agent update manifest", "error", err, "component", req.Component, "platform", req.Platform)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	// Add download URL if not present
	if manifest != nil && manifest.DownloadURL == "" {
		manifest.DownloadURL = fmt.Sprintf("/api/v1/agents/update/download/%s/%s/%s-%s",
			manifest.Component, manifest.Version, manifest.Platform, manifest.Arch)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"manifest": manifest,
	})
}

// handleAgentUpdateDownload streams the update artifact to the agent.
// Supports HTTP Range requests for resumable downloads.
// URL pattern: /api/v1/agents/update/download/{component}/{version}/{platform}-{arch}
func handleAgentUpdateDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}

	// Parse URL: /api/v1/agents/update/download/{component}/{version}/{platform}-{arch}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/agents/update/download/")
	parts := strings.Split(path, "/")
	if len(parts) < 3 {
		http.Error(w, "invalid download path", http.StatusBadRequest)
		return
	}

	component := parts[0]
	version := parts[1]
	platformArch := parts[2]

	// Split platform-arch
	dashIdx := strings.LastIndex(platformArch, "-")
	if dashIdx <= 0 {
		http.Error(w, "invalid platform-arch format", http.StatusBadRequest)
		return
	}
	platform := platformArch[:dashIdx]
	arch := platformArch[dashIdx+1:]

	// Get artifact from release manager
	if releaseManager == nil {
		http.Error(w, "release manager not initialized", http.StatusServiceUnavailable)
		return
	}

	artifact, err := serverStore.GetReleaseArtifact(r.Context(), component, version, platform, arch)
	if err != nil {
		logWarn("Agent update artifact not found", "component", component, "version", version, "platform", platform, "arch", arch, "error", err)
		http.Error(w, "artifact not found", http.StatusNotFound)
		return
	}

	if artifact.CachePath == "" {
		http.Error(w, "artifact not cached", http.StatusServiceUnavailable)
		return
	}

	file, err := os.Open(artifact.CachePath)
	if err != nil {
		logWarn("Failed to open artifact cache", "path", artifact.CachePath, "error", err)
		http.Error(w, "artifact unavailable", http.StatusServiceUnavailable)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		http.Error(w, "failed to stat artifact", http.StatusInternalServerError)
		return
	}

	// Determine filename for Content-Disposition
	filename := filepath.Base(artifact.CachePath)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Type", "application/octet-stream")

	// ServeContent handles Range requests automatically
	http.ServeContent(w, r, filename, info.ModTime(), file)
}

// handleAgentUpdateTelemetry receives update progress/status reports from agents.
func handleAgentUpdateTelemetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		AgentID        string                 `json:"agent_id"`
		RunID          string                 `json:"run_id,omitempty"`
		Status         string                 `json:"status"`
		CurrentVersion string                 `json:"current_version"`
		TargetVersion  string                 `json:"target_version,omitempty"`
		ErrorCode      string                 `json:"error_code,omitempty"`
		ErrorMessage   string                 `json:"error_message,omitempty"`
		Timestamp      time.Time              `json:"timestamp"`
		Metadata       map[string]interface{} `json:"metadata,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Log telemetry for monitoring/alerting
	logInfo("Agent update telemetry",
		"agent_id", req.AgentID,
		"status", req.Status,
		"current_version", req.CurrentVersion,
		"target_version", req.TargetVersion,
		"error_code", req.ErrorCode,
	)

	// TODO: Store telemetry for dashboards/reporting

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

// handleReleasesSync triggers an immediate sync of release artifacts from GitHub.
func handleReleasesSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if intakeWorker == nil {
		http.Error(w, "release intake worker not initialized", http.StatusServiceUnavailable)
		return
	}
	if err := intakeWorker.RunOnce(r.Context()); err != nil {
		logWarn("Release sync failed", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}
	logInfo("Release sync completed successfully")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "release sync completed",
	})
}

// handleReleasesArtifacts lists cached release artifacts.
func handleReleasesArtifacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	component := r.URL.Query().Get("component")
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}

	artifacts, err := serverStore.ListReleaseArtifacts(r.Context(), component, limit)
	if err != nil {
		logWarn("Failed to list release artifacts", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "failed to list artifacts",
		})
		return
	}

	// Build response with sanitized paths (don't expose full server filesystem paths)
	type artifactResponse struct {
		ID           int64     `json:"id"`
		Component    string    `json:"component"`
		Version      string    `json:"version"`
		Platform     string    `json:"platform"`
		Arch         string    `json:"arch"`
		Channel      string    `json:"channel"`
		SHA256       string    `json:"sha256"`
		SizeBytes    int64     `json:"size_bytes"`
		Cached       bool      `json:"cached"`
		PublishedAt  time.Time `json:"published_at"`
		DownloadedAt time.Time `json:"downloaded_at"`
	}

	resp := make([]artifactResponse, 0, len(artifacts))
	for _, a := range artifacts {
		resp = append(resp, artifactResponse{
			ID:           a.ID,
			Component:    a.Component,
			Version:      a.Version,
			Platform:     a.Platform,
			Arch:         a.Arch,
			Channel:      a.Channel,
			SHA256:       a.SHA256,
			SizeBytes:    a.SizeBytes,
			Cached:       a.CachePath != "",
			PublishedAt:  a.PublishedAt,
			DownloadedAt: a.DownloadedAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"artifacts": resp,
		"count":     len(resp),
	})
}

// handleLatestAgentVersion returns the latest agent version from cached artifacts.
// This is used by the UI to show "Update available" indicators.
func handleLatestAgentVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// First, try to get from cached artifacts (most reliable since these are signed/verified)
	artifacts, err := serverStore.ListReleaseArtifacts(r.Context(), "agent", 1)
	if err == nil && len(artifacts) > 0 {
		// Artifacts are sorted by published_at descending, so first is latest
		latest := artifacts[0]
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"version":      latest.Version,
			"published_at": latest.PublishedAt,
			"source":       "cache",
		})
		return
	}

	// Fallback: use server version (agent and server share version tags)
	ver := Version
	if ver != "" && ver != "dev" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"version": ver,
			"source":  "server",
		})
		return
	}

	// No version available
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": "no agent version information available",
	})
}
