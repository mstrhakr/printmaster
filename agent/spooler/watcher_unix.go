//go:build linux || darwin
// +build linux darwin

package spooler

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// IsSupported returns whether spooler watching is supported on this platform
func IsSupported() bool {
	// Check if CUPS is available by looking for lpstat
	_, err := exec.LookPath("lpstat")
	return err == nil
}

// Watcher monitors CUPS for printer and job changes
type Watcher struct {
	config WatcherConfig
	logger Logger

	mu       sync.RWMutex
	printers map[string]*LocalPrinter        // keyed by printer name
	jobs     map[string]map[uint32]*PrintJob // keyed by printer name, then job ID

	// Event channel for job state changes
	events chan JobEvent

	// Track job IDs we've seen to detect completions
	seenJobs map[string]map[uint32]bool // printer -> job ID -> seen

	// Lifecycle
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running bool
}

// NewWatcher creates a new CUPS spooler watcher
func NewWatcher(config WatcherConfig, logger Logger) *Watcher {
	if logger == nil {
		logger = nullLogger{}
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 5 * time.Second
	}

	return &Watcher{
		config:   config,
		logger:   logger,
		printers: make(map[string]*LocalPrinter),
		jobs:     make(map[string]map[uint32]*PrintJob),
		seenJobs: make(map[string]map[uint32]bool),
		events:   make(chan JobEvent, 100),
	}
}

// Start begins monitoring CUPS
func (w *Watcher) Start() error {
	if !IsSupported() {
		return fmt.Errorf("CUPS is not available on this system")
	}

	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.ctx, w.cancel = context.WithCancel(context.Background())
	w.running = true
	w.mu.Unlock()

	// Do initial enumeration
	if err := w.refreshPrinters(); err != nil {
		w.logger.Warn("Initial printer enumeration failed", "error", err)
	}

	// Start polling goroutine
	w.wg.Add(1)
	go w.pollLoop()

	w.logger.Info("CUPS spooler watcher started", "poll_interval", w.config.PollInterval)
	return nil
}

// Stop halts the spooler watcher
func (w *Watcher) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	w.running = false
	w.cancel()
	w.mu.Unlock()

	w.wg.Wait()
	close(w.events)
	w.logger.Info("CUPS spooler watcher stopped")
}

// Events returns the channel for job events
func (w *Watcher) Events() <-chan JobEvent {
	return w.events
}

// GetPrinters returns all discovered printers
func (w *Watcher) GetPrinters() []*LocalPrinter {
	w.mu.RLock()
	defer w.mu.RUnlock()

	result := make([]*LocalPrinter, 0, len(w.printers))
	for _, p := range w.printers {
		copy := *p
		result = append(result, &copy)
	}
	return result
}

// GetPrinter returns a specific printer by name
func (w *Watcher) GetPrinter(name string) (*LocalPrinter, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	p, ok := w.printers[name]
	if !ok {
		return nil, false
	}
	copy := *p
	return &copy, true
}

// GetJobs returns all jobs for a printer
func (w *Watcher) GetJobs(printerName string) []*PrintJob {
	w.mu.RLock()
	defer w.mu.RUnlock()

	printerJobs, ok := w.jobs[printerName]
	if !ok {
		return nil
	}

	result := make([]*PrintJob, 0, len(printerJobs))
	for _, j := range printerJobs {
		copy := *j
		result = append(result, &copy)
	}
	return result
}

// SetBaseline sets the baseline page count for a printer
func (w *Watcher) SetBaseline(printerName string, baseline int64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	p, ok := w.printers[printerName]
	if !ok {
		return false
	}
	p.BaselinePages = baseline
	return true
}

// SetTracking enables/disables page tracking for a printer
func (w *Watcher) SetTracking(printerName string, enabled bool) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	p, ok := w.printers[printerName]
	if !ok {
		return false
	}
	p.TrackingEnabled = enabled
	return true
}

// pollLoop runs the main polling loop
func (w *Watcher) pollLoop() {
	defer w.wg.Done()

	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			if err := w.refreshPrinters(); err != nil {
				w.logger.Warn("Printer refresh failed", "error", err)
			}
			w.refreshJobs()
		}
	}
}

// refreshPrinters enumerates all printers from CUPS
func (w *Watcher) refreshPrinters() error {
	// Get printer list using lpstat -p
	// Output format: "printer PrinterName is idle.  enabled since ..."
	ctx, cancel := context.WithTimeout(w.ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "lpstat", "-p")
	output, err := cmd.Output()
	if err != nil {
		// No printers is not an error
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil
		}
		return fmt.Errorf("lpstat -p failed: %w", err)
	}

	// Get default printer
	defaultPrinter := w.getDefaultPrinter()

	// Get printer details using lpstat -v for device URIs
	deviceURIs := w.getDeviceURIs()

	// Parse printer list
	now := time.Now()
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	printerRegex := regexp.MustCompile(`^printer\s+(\S+)\s+(.*)$`)

	for scanner.Scan() {
		line := scanner.Text()
		matches := printerRegex.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		name := matches[1]
		statusLine := matches[2]
		seen[name] = true

		// Parse status
		status := "unknown"
		var statusCode uint32
		if strings.Contains(statusLine, "is idle") {
			status = "ready"
			statusCode = 0
		} else if strings.Contains(statusLine, "now printing") {
			status = "printing"
			statusCode = 0x00000400 // Use Windows constant for compatibility
		} else if strings.Contains(statusLine, "disabled") {
			status = "offline"
			statusCode = 0x00000080
		}

		// Get device URI and classify printer type
		deviceURI := deviceURIs[name]
		printerType := classifyPrinterFromURI(deviceURI)

		// Filter based on config
		if printerType == PrinterTypeNetwork && !w.config.IncludeNetworkPrinters {
			continue
		}
		if printerType == PrinterTypeVirtual && !w.config.IncludeVirtualPrinters {
			continue
		}

		w.mu.Lock()
		existing, exists := w.printers[name]
		if exists {
			// Update existing
			existing.Status = status
			existing.StatusCode = statusCode
			existing.LastSeen = now
			existing.PortName = deviceURI
			existing.IsDefault = (name == defaultPrinter)
		} else {
			// Create new
			autoTrack := (printerType == PrinterTypeUSB && w.config.AutoTrackUSB) ||
				(printerType == PrinterTypeLocal && w.config.AutoTrackLocal)

			printer := &LocalPrinter{
				Name:            name,
				PortName:        deviceURI,
				DriverName:      w.getDriverName(name),
				Type:            printerType,
				IsDefault:       name == defaultPrinter,
				Status:          status,
				StatusCode:      statusCode,
				FirstSeen:       now,
				LastSeen:        now,
				TrackingEnabled: autoTrack,
			}

			// Try to get additional info
			w.enrichPrinterInfo(printer)

			w.printers[name] = printer
			w.jobs[name] = make(map[uint32]*PrintJob)
			w.seenJobs[name] = make(map[uint32]bool)
			w.logger.Info("Discovered CUPS printer", "name", name, "type", printerType, "uri", deviceURI)
		}
		w.mu.Unlock()
	}

	// Remove printers that are no longer present
	w.mu.Lock()
	for name := range w.printers {
		if !seen[name] {
			w.logger.Info("Printer removed", "name", name)
			delete(w.printers, name)
			delete(w.jobs, name)
			delete(w.seenJobs, name)
		}
	}
	w.mu.Unlock()

	return nil
}

// getDefaultPrinter returns the system default printer name
func (w *Watcher) getDefaultPrinter() string {
	ctx, cancel := context.WithTimeout(w.ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "lpstat", "-d")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	// Output format: "system default destination: PrinterName"
	line := strings.TrimSpace(string(output))
	if strings.HasPrefix(line, "system default destination:") {
		return strings.TrimSpace(strings.TrimPrefix(line, "system default destination:"))
	}
	return ""
}

// getDeviceURIs returns a map of printer name to device URI
func (w *Watcher) getDeviceURIs() map[string]string {
	ctx, cancel := context.WithTimeout(w.ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "lpstat", "-v")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	// Output format: "device for PrinterName: usb://HP/LaserJet%20Pro%20M404?serial=XXX"
	result := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	deviceRegex := regexp.MustCompile(`^device\s+for\s+(\S+):\s+(.*)$`)

	for scanner.Scan() {
		line := scanner.Text()
		matches := deviceRegex.FindStringSubmatch(line)
		if matches != nil {
			result[matches[1]] = matches[2]
		}
	}

	return result
}

// getDriverName returns the driver/make-model for a printer
func (w *Watcher) getDriverName(printerName string) string {
	ctx, cancel := context.WithTimeout(w.ctx, 5*time.Second)
	defer cancel()

	// Use lpoptions to get printer info
	cmd := exec.CommandContext(ctx, "lpoptions", "-p", printerName, "-l")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	// Look for make-model or similar
	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(strings.ToLower(line), "make") {
			return strings.TrimSpace(line)
		}
	}

	return ""
}

// enrichPrinterInfo tries to get additional info about a printer
func (w *Watcher) enrichPrinterInfo(printer *LocalPrinter) {
	// Try to parse manufacturer/model from URI or driver name
	if printer.PortName != "" {
		// USB URIs often have format: usb://Manufacturer/Model?serial=XXX
		if strings.HasPrefix(printer.PortName, "usb://") {
			uri := strings.TrimPrefix(printer.PortName, "usb://")
			parts := strings.SplitN(uri, "/", 2)
			if len(parts) >= 1 {
				printer.Manufacturer = strings.ReplaceAll(parts[0], "%20", " ")
			}
			if len(parts) >= 2 {
				modelPart := parts[1]
				// Remove query string
				if idx := strings.Index(modelPart, "?"); idx >= 0 {
					// Check for serial in query
					query := modelPart[idx+1:]
					modelPart = modelPart[:idx]
					if strings.HasPrefix(query, "serial=") {
						printer.SerialNumber = strings.TrimPrefix(query, "serial=")
					}
				}
				printer.Model = strings.ReplaceAll(modelPart, "%20", " ")
			}
		}
	}
}

// classifyPrinterFromURI determines printer type from CUPS device URI
func classifyPrinterFromURI(uri string) PrinterType {
	uri = strings.ToLower(uri)

	// USB printers
	if strings.HasPrefix(uri, "usb://") || strings.HasPrefix(uri, "usb:") {
		return PrinterTypeUSB
	}

	// Local ports (parallel, serial)
	if strings.HasPrefix(uri, "parallel://") || strings.HasPrefix(uri, "serial://") ||
		strings.HasPrefix(uri, "/dev/") {
		return PrinterTypeLocal
	}

	// Network printers
	if strings.HasPrefix(uri, "ipp://") || strings.HasPrefix(uri, "ipps://") ||
		strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://") ||
		strings.HasPrefix(uri, "socket://") || strings.HasPrefix(uri, "lpd://") ||
		strings.HasPrefix(uri, "smb://") {
		return PrinterTypeNetwork
	}

	// Virtual printers (CUPS-PDF, file, etc.)
	if strings.HasPrefix(uri, "cups-pdf://") || strings.HasPrefix(uri, "file://") ||
		strings.HasPrefix(uri, "pipe://") || strings.Contains(uri, "pdf") {
		return PrinterTypeVirtual
	}

	return PrinterTypeUnknown
}

// refreshJobs polls for job changes and emits events
func (w *Watcher) refreshJobs() {
	ctx, cancel := context.WithTimeout(w.ctx, 10*time.Second)
	defer cancel()

	// Get all active jobs using lpstat -o
	// Output format: "PrinterName-123 username 1234 Mon Dec 19 12:34:56 2025"
	cmd := exec.CommandContext(ctx, "lpstat", "-o")
	output, err := cmd.Output()
	if err != nil {
		// No jobs is not an error
		return
	}

	now := time.Now()

	// Track current jobs per printer
	currentJobs := make(map[string]map[uint32]bool)

	w.mu.RLock()
	for name := range w.printers {
		currentJobs[name] = make(map[uint32]bool)
	}
	w.mu.RUnlock()

	// Parse job list
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	// Job ID format is typically "PrinterName-JobNumber"
	jobRegex := regexp.MustCompile(`^(\S+)-(\d+)\s+(\S+)\s+(\d+)\s+(.*)$`)

	for scanner.Scan() {
		line := scanner.Text()
		matches := jobRegex.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		printerName := matches[1]
		jobIDStr := matches[2]
		userName := matches[3]
		sizeStr := matches[4]
		// dateStr := matches[5] // Date string, parsing is complex

		jobID64, _ := strconv.ParseUint(jobIDStr, 10, 32)
		jobID := uint32(jobID64)
		size, _ := strconv.ParseInt(sizeStr, 10, 64)

		currentJobs[printerName][jobID] = true

		w.mu.Lock()
		printer, exists := w.printers[printerName]
		if !exists {
			w.mu.Unlock()
			continue
		}

		printerJobs, _ := w.jobs[printerName]
		existingJob, jobExists := printerJobs[jobID]

		if !jobExists {
			// New job
			job := &PrintJob{
				JobID:       jobID,
				PrinterName: printerName,
				UserName:    userName,
				Status:      "queued",
				Size:        size,
				Submitted:   now,
				TotalPages:  -1, // Unknown
			}

			// Try to get more job details
			w.enrichJobInfo(job)

			printerJobs[jobID] = job

			// Mark as seen
			if w.seenJobs[printerName] == nil {
				w.seenJobs[printerName] = make(map[uint32]bool)
			}
			w.seenJobs[printerName][jobID] = true

			printer.JobCount++

			w.mu.Unlock()

			// Emit event
			w.emitEvent(JobEvent{
				Type:        JobEventAdded,
				Job:         job,
				PrinterName: printerName,
				Timestamp:   now,
			})
		} else {
			// Update existing job
			oldStatus := existingJob.Status

			// Check if job is now printing
			if w.isJobPrinting(printerName, jobID) {
				existingJob.Status = "printing"
				if existingJob.StartTime.IsZero() {
					existingJob.StartTime = now
				}
			}

			w.mu.Unlock()

			// Emit event if status changed to printing
			if oldStatus != "printing" && existingJob.Status == "printing" {
				w.emitEvent(JobEvent{
					Type:        JobEventStarted,
					Job:         existingJob,
					PrinterName: printerName,
					Timestamp:   now,
				})
			}
		}
	}

	// Check for completed jobs (jobs that were seen before but are now gone)
	w.mu.Lock()
	for printerName, seenJobIDs := range w.seenJobs {
		printer, exists := w.printers[printerName]
		if !exists {
			continue
		}

		for jobID := range seenJobIDs {
			if !currentJobs[printerName][jobID] {
				// Job is no longer in queue - it completed or was deleted
				job, jobExists := w.jobs[printerName][jobID]

				if jobExists && printer.TrackingEnabled {
					// Assume job completed successfully and add pages
					pagesAdded := int32(1) // Default to 1 if unknown
					if job.TotalPages > 0 {
						pagesAdded = job.TotalPages
					}

					printer.TotalPages += int64(pagesAdded)
					// Track color vs mono separately
					if job.IsColor {
						printer.TotalColorPages += int64(pagesAdded)
					} else {
						printer.TotalMonoPages += int64(pagesAdded)
					}
					printer.LastPageUpdate = now

					// Emit completion event
					w.emitEvent(JobEvent{
						Type:        JobEventCompleted,
						Job:         job,
						PrinterName: printerName,
						Timestamp:   now,
						PagesAdded:  pagesAdded,
					})

					w.logger.Debug("Job completed", "printer", printerName, "job_id", jobID, "pages", pagesAdded, "color", job.IsColor)
				}

				// Clean up
				delete(w.jobs[printerName], jobID)
				delete(w.seenJobs[printerName], jobID)
				if printer.JobCount > 0 {
					printer.JobCount--
				}
			}
		}
	}
	w.mu.Unlock()
}

// isJobPrinting checks if a specific job is currently printing
func (w *Watcher) isJobPrinting(printerName string, jobID uint32) bool {
	ctx, cancel := context.WithTimeout(w.ctx, 5*time.Second)
	defer cancel()

	// Check printer status for "now printing" with job ID
	cmd := exec.CommandContext(ctx, "lpstat", "-p", printerName)
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	line := string(output)
	return strings.Contains(line, "now printing") && strings.Contains(line, fmt.Sprintf("%s-%d", printerName, jobID))
}

// enrichJobInfo tries to get additional info about a job using lpq and lpstat -l
func (w *Watcher) enrichJobInfo(job *PrintJob) {
	ctx, cancel := context.WithTimeout(w.ctx, 5*time.Second)
	defer cancel()

	// Use lpstat -l to get more job details including options
	cmd := exec.CommandContext(ctx, "lpstat", "-l", "-o", fmt.Sprintf("%s-%d", job.PrinterName, job.JobID))
	output, err := cmd.Output()
	if err == nil {
		// Parse lpstat -l output for color info and page count
		// Format includes lines like:
		//   "Number of pages: 5"
		//   "ColorModel=CMYK" or "print-color-mode=color"
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			lineLower := strings.ToLower(line)

			// Check for page count
			if strings.Contains(lineLower, "number of pages") {
				parts := strings.Split(line, ":")
				if len(parts) == 2 {
					if pages, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 32); err == nil && pages > 0 {
						job.TotalPages = int32(pages)
					}
				}
			}

			// Check for color mode indicators
			if strings.Contains(lineLower, "colormodel") ||
				strings.Contains(lineLower, "print-color-mode") ||
				strings.Contains(lineLower, "output-mode") {
				if strings.Contains(lineLower, "color") ||
					strings.Contains(lineLower, "cmyk") ||
					strings.Contains(lineLower, "rgb") {
					job.IsColor = true
				}
			}
		}
	}

	// Fallback to lpq for document name
	cmd = exec.CommandContext(ctx, "lpq", "-P", job.PrinterName)
	output, err = cmd.Output()
	if err != nil {
		return
	}

	// Try to parse document name and pages from lpq output
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	jobStr := fmt.Sprintf("%d", job.JobID)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, jobStr) {
			// Try to extract document name - format varies
			parts := strings.Fields(line)
			for i, part := range parts {
				if part == jobStr && i > 0 {
					// Document name might be before job ID
					job.DocumentName = parts[i-1]
					break
				}
			}
		}
	}
}

// emitEvent sends an event to the events channel
func (w *Watcher) emitEvent(event JobEvent) {
	select {
	case w.events <- event:
	default:
		w.logger.Warn("Event channel full, dropping event", "type", event.Type, "printer", event.PrinterName)
	}
}
