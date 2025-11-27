package autoupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"printmaster/common/logger"
	"printmaster/common/updatepolicy"

	"github.com/Masterminds/semver/v3"
)

const (
	defaultCheckInterval      = 24 * time.Hour
	defaultMinDiskSpaceMB     = 200
	defaultMaxRetries         = 5
	defaultRetryBaseDelay     = 2 * time.Second
	defaultRetryMaxDelay      = 5 * time.Minute
	defaultHealthCheckDelay   = 10 * time.Second
	defaultHealthCheckTimeout = 30 * time.Second
	updateDirName             = "autoupdate"
	stagingDirName            = "staging"
	backupDirName             = "backups"
	downloadDirName           = "downloads"
)

// Options configure the auto-update manager.
type Options struct {
	Log            *logger.Logger
	DataDir        string
	Enabled        bool
	CurrentVersion string
	Platform       string
	Arch           string
	Channel        string
	BinaryPath     string
	ServiceName    string
	MinDiskSpaceMB int64
	MaxRetries     int
	Clock          func() time.Time
	ServerClient   UpdateClient
	PolicyProvider PolicyProvider
	TelemetrySink  TelemetrySink
}

// UpdateClient abstracts the server communication needed for updates.
type UpdateClient interface {
	// GetLatestManifest fetches the signed manifest for the latest available version.
	GetLatestManifest(ctx context.Context, component, platform, arch, channel string) (*UpdateManifest, error)
	// DownloadArtifact downloads the artifact to the specified path, supporting range requests.
	DownloadArtifact(ctx context.Context, manifest *UpdateManifest, destPath string, resumeFrom int64) (int64, error)
}

// PolicyProvider resolves the effective update policy.
type PolicyProvider interface {
	EffectivePolicy() (updatepolicy.PolicySpec, updatepolicy.PolicySource, bool)
}

// TelemetrySink reports update status to the server.
type TelemetrySink interface {
	ReportUpdateStatus(ctx context.Context, payload TelemetryPayload) error
}

// Manager coordinates agent auto-update operations.
type Manager struct {
	log            *logger.Logger
	stateDir       string
	enabled        bool
	disabledReason string
	currentVersion string
	currentSemver  *semver.Version
	platform       string
	arch           string
	channel        string
	binaryPath     string
	serviceName    string
	minDiskSpace   int64
	maxRetries     int
	clock          func() time.Time
	client         UpdateClient
	policyProvider PolicyProvider
	telemetry      TelemetrySink

	mu             sync.RWMutex
	status         Status
	lastCheck      time.Time
	nextCheck      time.Time
	latestVersion  string
	latestManifest *UpdateManifest
	currentRun     *UpdateRun

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewManager creates an auto-update manager with the provided options.
func NewManager(opts Options) (*Manager, error) {
	if opts.CurrentVersion == "" {
		return nil, fmt.Errorf("current version is required")
	}

	dataDir := strings.TrimSpace(opts.DataDir)
	if dataDir == "" {
		dataDir = filepath.Join(os.TempDir(), "printmaster-agent")
	}

	platform := strings.TrimSpace(opts.Platform)
	if platform == "" {
		platform = runtime.GOOS
	}
	arch := strings.TrimSpace(opts.Arch)
	if arch == "" {
		arch = runtime.GOARCH
	}
	channel := strings.ToLower(strings.TrimSpace(opts.Channel))
	if channel == "" {
		channel = "stable"
	}

	currentSemver, parseErr := semver.NewVersion(strings.TrimPrefix(opts.CurrentVersion, "v"))
	if parseErr != nil {
		// Log but don't fail - we can still check for updates by string comparison
		if opts.Log != nil {
			opts.Log.Warn("Failed to parse current version as semver", "version", opts.CurrentVersion, "error", parseErr)
		}
	}

	stateDir := filepath.Join(dataDir, updateDirName)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create update state directory: %w", err)
	}
	for _, sub := range []string{stagingDirName, backupDirName, downloadDirName} {
		if err := os.MkdirAll(filepath.Join(stateDir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("failed to create %s directory: %w", sub, err)
		}
	}

	minDiskSpace := opts.MinDiskSpaceMB
	if minDiskSpace <= 0 {
		minDiskSpace = defaultMinDiskSpaceMB
	}
	maxRetries := opts.MaxRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}
	clock := opts.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}

	var disabledReason string
	if !opts.Enabled {
		disabledReason = "disabled by configuration"
	}
	if opts.ServerClient == nil {
		disabledReason = "no server client configured"
		opts.Enabled = false
	}

	binaryPath := opts.BinaryPath
	if binaryPath == "" {
		exec, err := os.Executable()
		if err == nil {
			binaryPath = exec
		}
	}

	return &Manager{
		log:            opts.Log,
		stateDir:       stateDir,
		enabled:        opts.Enabled,
		disabledReason: disabledReason,
		currentVersion: opts.CurrentVersion,
		currentSemver:  currentSemver,
		platform:       platform,
		arch:           arch,
		channel:        channel,
		binaryPath:     binaryPath,
		serviceName:    opts.ServiceName,
		minDiskSpace:   minDiskSpace,
		maxRetries:     maxRetries,
		clock:          clock,
		client:         opts.ServerClient,
		policyProvider: opts.PolicyProvider,
		telemetry:      opts.TelemetrySink,
		status:         StatusIdle,
		stopCh:         make(chan struct{}),
	}, nil
}

// Start begins the background update worker.
func (m *Manager) Start(ctx context.Context) {
	m.wg.Add(1)
	go m.runLoop(ctx)
}

// Stop gracefully shuts down the update worker.
func (m *Manager) Stop() {
	close(m.stopCh)
	m.wg.Wait()
}

// Status returns the current manager status for UI/API consumption.
func (m *Manager) Status() ManagerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	checkIntervalDays := 7
	policySource := ""
	if m.policyProvider != nil {
		spec, source, ok := m.policyProvider.EffectivePolicy()
		if ok && spec.UpdateCheckDays > 0 {
			checkIntervalDays = spec.UpdateCheckDays
		}
		policySource = string(source)
	}

	return ManagerStatus{
		Enabled:           m.enabled,
		DisabledReason:    m.disabledReason,
		CurrentVersion:    m.currentVersion,
		LatestVersion:     m.latestVersion,
		UpdateAvailable:   m.latestManifest != nil && m.latestVersion != m.currentVersion,
		Status:            m.status,
		LastCheckAt:       m.lastCheck,
		NextCheckAt:       m.nextCheck,
		PolicySource:      policySource,
		CheckIntervalDays: checkIntervalDays,
		Channel:           m.channel,
		Platform:          m.platform,
		Arch:              m.arch,
	}
}

// CheckNow triggers an immediate update check.
func (m *Manager) CheckNow(ctx context.Context) error {
	m.mu.Lock()
	if m.status != StatusIdle && m.status != StatusPending {
		m.mu.Unlock()
		return fmt.Errorf("update operation already in progress: %s", m.status)
	}
	m.mu.Unlock()

	return m.performCheck(ctx)
}

func (m *Manager) runLoop(ctx context.Context) {
	defer m.wg.Done()

	if !m.enabled {
		m.logDebug("Auto-update worker disabled", "reason", m.disabledReason)
		return
	}

	// Calculate initial check delay with jitter
	m.scheduleNextCheck()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case <-time.After(m.timeUntilNextCheck()):
			if err := m.performCheck(ctx); err != nil {
				m.logWarn("Update check failed", "error", err)
			}
			m.scheduleNextCheck()
		}
	}
}

func (m *Manager) scheduleNextCheck() {
	m.mu.Lock()
	defer m.mu.Unlock()

	interval := defaultCheckInterval
	if m.policyProvider != nil {
		spec, _, ok := m.policyProvider.EffectivePolicy()
		if ok && spec.UpdateCheckDays > 0 {
			interval = time.Duration(spec.UpdateCheckDays) * 24 * time.Hour
		}
	}

	// Add jitter (up to 10% of interval)
	jitter := time.Duration(rand.Int63n(int64(interval / 10)))
	m.nextCheck = m.clock().Add(interval + jitter)
}

func (m *Manager) timeUntilNextCheck() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	until := time.Until(m.nextCheck)
	if until < 0 {
		until = time.Minute // Fallback minimum
	}
	return until
}

func (m *Manager) performCheck(ctx context.Context) error {
	m.setStatus(StatusChecking)
	defer func() {
		if m.status == StatusChecking {
			m.setStatus(StatusIdle)
		}
	}()

	// Check if we're within maintenance window
	if !m.isInMaintenanceWindow() {
		m.logDebug("Skipping update check - outside maintenance window")
		m.setStatus(StatusIdle)
		return nil
	}

	// Check effective policy
	if m.policyProvider != nil {
		_, source, enabled := m.policyProvider.EffectivePolicy()
		if !enabled {
			m.logDebug("Auto-update disabled by policy", "source", source)
			m.setStatus(StatusIdle)
			return nil
		}
	}

	// Fetch latest manifest from server
	manifest, err := m.client.GetLatestManifest(ctx, "agent", m.platform, m.arch, m.channel)
	if err != nil {
		m.reportTelemetry(ctx, Status(StatusFailed), ErrCodeServerError, err.Error())
		return fmt.Errorf("failed to fetch manifest: %w", err)
	}

	m.mu.Lock()
	m.lastCheck = m.clock()
	m.latestVersion = manifest.Version
	m.latestManifest = manifest
	m.mu.Unlock()

	// Check if update is needed
	if !m.isUpdateNeeded(manifest) {
		m.logDebug("No update needed", "current", m.currentVersion, "latest", manifest.Version)
		return nil
	}

	m.logInfo("Update available", "current", m.currentVersion, "target", manifest.Version)

	// Pre-flight: disk space check
	if err := m.checkDiskSpace(manifest.SizeBytes); err != nil {
		m.reportTelemetry(ctx, StatusFailed, ErrCodeDiskSpace, err.Error())
		return err
	}

	// Proceed with update
	m.setStatus(StatusPending)
	return m.executeUpdate(ctx, manifest)
}

func (m *Manager) executeUpdate(ctx context.Context, manifest *UpdateManifest) error {
	run := &UpdateRun{
		ID:             fmt.Sprintf("run-%d", m.clock().UnixNano()),
		Status:         StatusDownloading,
		RequestedAt:    m.clock(),
		CurrentVersion: m.currentVersion,
		TargetVersion:  manifest.Version,
		Channel:        m.channel,
		Platform:       m.platform,
		Arch:           m.arch,
		SizeBytes:      manifest.SizeBytes,
	}

	m.mu.Lock()
	m.currentRun = run
	m.mu.Unlock()

	// Download phase
	m.setStatus(StatusDownloading)
	run.StartedAt = m.clock()
	m.reportTelemetry(ctx, StatusDownloading, "", "")

	downloadPath := filepath.Join(m.stateDir, downloadDirName, fmt.Sprintf("agent-%s-%s-%s%s",
		manifest.Version, m.platform, m.arch, m.binaryExtension()))

	downloadStart := m.clock()
	if err := m.downloadWithRetry(ctx, manifest, downloadPath); err != nil {
		run.Status = StatusFailed
		run.ErrorCode = ErrCodeDownloadFailed
		run.ErrorMessage = err.Error()
		run.CompletedAt = m.clock()
		m.setStatus(StatusFailed)
		m.reportTelemetry(ctx, StatusFailed, ErrCodeDownloadFailed, err.Error())
		return err
	}
	run.DownloadedAt = m.clock()
	run.DownloadTimeMs = run.DownloadedAt.Sub(downloadStart).Milliseconds()

	// Verify hash
	if err := m.verifyHash(downloadPath, manifest.SHA256); err != nil {
		run.Status = StatusFailed
		run.ErrorCode = ErrCodeHashMismatch
		run.ErrorMessage = err.Error()
		run.CompletedAt = m.clock()
		m.setStatus(StatusFailed)
		m.reportTelemetry(ctx, StatusFailed, ErrCodeHashMismatch, err.Error())
		os.Remove(downloadPath)
		return err
	}

	// Staging phase
	m.setStatus(StatusStaging)
	run.Status = StatusStaging
	m.reportTelemetry(ctx, StatusStaging, "", "")

	stagingPath := filepath.Join(m.stateDir, stagingDirName, filepath.Base(downloadPath))
	if err := m.stageBinary(downloadPath, stagingPath); err != nil {
		run.Status = StatusFailed
		run.ErrorCode = ErrCodeStagingFailed
		run.ErrorMessage = err.Error()
		run.CompletedAt = m.clock()
		m.setStatus(StatusFailed)
		m.reportTelemetry(ctx, StatusFailed, ErrCodeStagingFailed, err.Error())
		return err
	}

	// Backup current binary
	backupPath := filepath.Join(m.stateDir, backupDirName, fmt.Sprintf("agent-%s-%s-%s%s",
		m.currentVersion, m.platform, m.arch, m.binaryExtension()))
	if err := m.backupBinary(backupPath); err != nil {
		m.logWarn("Failed to backup current binary", "error", err)
		// Continue anyway - new install may still work
	}

	// Apply phase
	m.setStatus(StatusApplying)
	run.Status = StatusApplying
	m.reportTelemetry(ctx, StatusApplying, "", "")

	if err := m.applyUpdate(stagingPath); err != nil {
		run.Status = StatusFailed
		run.ErrorCode = ErrCodeApplyFailed
		run.ErrorMessage = err.Error()
		run.CompletedAt = m.clock()
		m.setStatus(StatusFailed)
		m.reportTelemetry(ctx, StatusFailed, ErrCodeApplyFailed, err.Error())
		// Attempt rollback
		if rollbackErr := m.rollback(backupPath); rollbackErr != nil {
			m.logError("Rollback failed", "error", rollbackErr)
		}
		return err
	}

	// Success
	run.Status = StatusSucceeded
	run.CompletedAt = m.clock()
	m.setStatus(StatusRestarting)
	m.reportTelemetry(ctx, StatusSucceeded, "", "")

	m.logInfo("Update applied successfully", "from", m.currentVersion, "to", manifest.Version)

	// Trigger restart (this function likely won't return)
	return m.restartService()
}

func (m *Manager) downloadWithRetry(ctx context.Context, manifest *UpdateManifest, destPath string) error {
	var lastErr error
	delay := defaultRetryBaseDelay

	for attempt := 1; attempt <= m.maxRetries; attempt++ {
		// Check for existing partial download
		var resumeFrom int64
		if info, err := os.Stat(destPath + ".partial"); err == nil {
			resumeFrom = info.Size()
		}

		downloaded, err := m.client.DownloadArtifact(ctx, manifest, destPath+".partial", resumeFrom)
		if err == nil && downloaded > 0 {
			// Rename partial to final
			if err := os.Rename(destPath+".partial", destPath); err != nil {
				return fmt.Errorf("failed to finalize download: %w", err)
			}
			return nil
		}

		lastErr = err
		m.logWarn("Download attempt failed", "attempt", attempt, "error", err)

		if attempt < m.maxRetries {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			// Exponential backoff with jitter
			delay = delay * 2
			if delay > defaultRetryMaxDelay {
				delay = defaultRetryMaxDelay
			}
			delay += time.Duration(rand.Int63n(int64(delay / 4)))
		}
	}

	return fmt.Errorf("download failed after %d attempts: %w", m.maxRetries, lastErr)
}

func (m *Manager) verifyHash(filePath, expectedHash string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file for verification: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("failed to compute hash: %w", err)
	}

	actualHash := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actualHash, expectedHash) {
		return fmt.Errorf("hash mismatch: expected %s, got %s", expectedHash, actualHash)
	}

	return nil
}

func (m *Manager) stageBinary(srcPath, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	return copyFile(srcPath, destPath)
}

func (m *Manager) backupBinary(backupPath string) error {
	if m.binaryPath == "" {
		return fmt.Errorf("binary path not set")
	}
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
		return err
	}
	return copyFile(m.binaryPath, backupPath)
}

func (m *Manager) applyUpdate(stagingPath string) error {
	if m.binaryPath == "" {
		return fmt.Errorf("binary path not set")
	}

	// On Windows, we can't replace a running binary directly.
	// We need to use a helper process or schedule the replacement.
	if runtime.GOOS == "windows" {
		return m.applyUpdateWindows(stagingPath)
	}

	// On Unix, we can typically just rename
	return m.applyUpdateUnix(stagingPath)
}

func (m *Manager) applyUpdateUnix(stagingPath string) error {
	// Make the new binary executable
	if err := os.Chmod(stagingPath, 0o755); err != nil {
		return fmt.Errorf("failed to set executable permission: %w", err)
	}

	// Rename the staging file to replace the current binary
	if err := os.Rename(stagingPath, m.binaryPath); err != nil {
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	return nil
}

func (m *Manager) applyUpdateWindows(stagingPath string) error {
	// On Windows, we write a helper batch file that:
	// 1. Waits for the current process to exit
	// 2. Copies the new binary
	// 3. Starts the new binary
	// This is similar to the server's approach

	helperScript := fmt.Sprintf(`@echo off
timeout /t 2 /nobreak >nul
copy /y "%s" "%s"
if %%errorlevel%% neq 0 exit /b %%errorlevel%%
start "" "%s"
del "%%~f0"
`, stagingPath, m.binaryPath, m.binaryPath)

	helperPath := filepath.Join(m.stateDir, "update_helper.bat")
	if err := os.WriteFile(helperPath, []byte(helperScript), 0o755); err != nil {
		return fmt.Errorf("failed to write update helper: %w", err)
	}

	return nil // The actual update will happen on restart
}

func (m *Manager) rollback(backupPath string) error {
	if backupPath == "" || m.binaryPath == "" {
		return fmt.Errorf("rollback not possible: missing paths")
	}

	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup file not found: %s", backupPath)
	}

	return copyFile(backupPath, m.binaryPath)
}

func (m *Manager) restartService() error {
	// This will depend on how the agent is running
	// For now, just exit - the service manager should restart us
	m.logInfo("Triggering restart for update")
	os.Exit(0)
	return nil // Never reached
}

func (m *Manager) isUpdateNeeded(manifest *UpdateManifest) bool {
	if manifest == nil || manifest.Version == "" {
		return false
	}

	// Simple string comparison fallback
	if m.currentSemver == nil {
		return manifest.Version != m.currentVersion
	}

	targetSemver, err := semver.NewVersion(strings.TrimPrefix(manifest.Version, "v"))
	if err != nil {
		return manifest.Version != m.currentVersion
	}

	// Check version pin strategy
	if m.policyProvider != nil {
		spec, _, ok := m.policyProvider.EffectivePolicy()
		if ok {
			if !m.isVersionAllowed(targetSemver, spec) {
				return false
			}
		}
	}

	return targetSemver.GreaterThan(m.currentSemver)
}

func (m *Manager) isVersionAllowed(target *semver.Version, spec updatepolicy.PolicySpec) bool {
	if spec.TargetVersion != "" {
		// Specific target version pinned
		targetPinned, err := semver.NewVersion(strings.TrimPrefix(spec.TargetVersion, "v"))
		if err == nil && !target.Equal(targetPinned) {
			return false
		}
	}

	switch spec.VersionPinStrategy {
	case updatepolicy.VersionPinMajor:
		// Only allow same major, or allow major if explicitly enabled
		if target.Major() != m.currentSemver.Major() && !spec.AllowMajorUpgrade {
			return false
		}
	case updatepolicy.VersionPinMinor:
		// Only allow same major.minor
		if target.Major() != m.currentSemver.Major() || target.Minor() != m.currentSemver.Minor() {
			if !spec.AllowMajorUpgrade {
				return false
			}
		}
	case updatepolicy.VersionPinPatch:
		// Only allow same major.minor.patch line (shouldn't reach here if same)
		return false
	}

	return true
}

func (m *Manager) isInMaintenanceWindow() bool {
	if m.policyProvider == nil {
		return true // No policy = always allowed
	}

	spec, _, ok := m.policyProvider.EffectivePolicy()
	if !ok || !spec.MaintenanceWindow.Enabled {
		return true
	}

	window := spec.MaintenanceWindow
	now := m.clock()

	// Load timezone
	loc := time.UTC
	if window.Timezone != "" {
		if tz, err := time.LoadLocation(window.Timezone); err == nil {
			loc = tz
		}
	}

	localNow := now.In(loc)

	// Check day of week
	if len(window.DaysOfWeek) > 0 {
		dayAllowed := false
		currentDay := int(localNow.Weekday())
		for _, d := range window.DaysOfWeek {
			if d == currentDay {
				dayAllowed = true
				break
			}
		}
		if !dayAllowed {
			return false
		}
	}

	// Check time of day
	currentMinutes := localNow.Hour()*60 + localNow.Minute()
	startMinutes := window.StartHour*60 + window.StartMin
	endMinutes := window.EndHour*60 + window.EndMin

	if startMinutes <= endMinutes {
		// Same day window
		return currentMinutes >= startMinutes && currentMinutes < endMinutes
	}
	// Overnight window
	return currentMinutes >= startMinutes || currentMinutes < endMinutes
}

func (m *Manager) checkDiskSpace(requiredBytes int64) error {
	// Need space for: download + staging + backup
	requiredMB := (requiredBytes * 3) / (1024 * 1024)
	if requiredMB < m.minDiskSpace {
		requiredMB = m.minDiskSpace
	}

	available, err := getAvailableDiskSpaceMB(m.stateDir)
	if err != nil {
		m.logWarn("Failed to check disk space", "error", err)
		return nil // Don't block on disk check failure
	}

	if available < requiredMB {
		return fmt.Errorf("insufficient disk space: need %d MB, have %d MB", requiredMB, available)
	}

	return nil
}

func (m *Manager) setStatus(s Status) {
	m.mu.Lock()
	m.status = s
	m.mu.Unlock()
}

func (m *Manager) reportTelemetry(ctx context.Context, status Status, errCode, errMsg string) {
	if m.telemetry == nil {
		return
	}

	payload := TelemetryPayload{
		Status:         status,
		CurrentVersion: m.currentVersion,
		Timestamp:      m.clock(),
	}

	m.mu.RLock()
	if m.currentRun != nil {
		payload.RunID = m.currentRun.ID
		payload.TargetVersion = m.currentRun.TargetVersion
		payload.SizeBytes = m.currentRun.SizeBytes
		payload.DownloadTimeMs = m.currentRun.DownloadTimeMs
	}
	m.mu.RUnlock()

	if errCode != "" {
		payload.ErrorCode = errCode
		payload.ErrorMessage = errMsg
	}

	go func() {
		if err := m.telemetry.ReportUpdateStatus(context.Background(), payload); err != nil {
			m.logWarn("Failed to report telemetry", "error", err)
		}
	}()
}

func (m *Manager) binaryExtension() string {
	if m.platform == "windows" {
		return ".exe"
	}
	return ""
}

func (m *Manager) logInfo(msg string, args ...interface{}) {
	if m.log != nil {
		m.log.Info(msg, args...)
	}
}

func (m *Manager) logDebug(msg string, args ...interface{}) {
	if m.log != nil {
		m.log.Debug(msg, args...)
	}
}

func (m *Manager) logWarn(msg string, args ...interface{}) {
	if m.log != nil {
		m.log.Warn(msg, args...)
	}
}

func (m *Manager) logError(msg string, args ...interface{}) {
	if m.log != nil {
		m.log.Error(msg, args...)
	}
}

// copyFile copies a file from src to dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	return out.Close()
}
