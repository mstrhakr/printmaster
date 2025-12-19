//go:build windows
// +build windows

package spooler

import (
	"context"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// IsSupported returns whether spooler watching is supported on this platform
func IsSupported() bool {
	return true
}

var (
	winspool              = syscall.NewLazyDLL("winspool.drv")
	procEnumPrinters      = winspool.NewProc("EnumPrintersW")
	procOpenPrinter       = winspool.NewProc("OpenPrinterW")
	procClosePrinter      = winspool.NewProc("ClosePrinter")
	procEnumJobs          = winspool.NewProc("EnumJobsW")
	procGetDefaultPrinter = winspool.NewProc("GetDefaultPrinterW")
)

// Windows constants for printer enumeration
const (
	PRINTER_ENUM_LOCAL       = 0x00000002
	PRINTER_ENUM_CONNECTIONS = 0x00000004
	PRINTER_ENUM_NAME        = 0x00000008

	// Printer status flags
	PRINTER_STATUS_PAUSED            = 0x00000001
	PRINTER_STATUS_ERROR             = 0x00000002
	PRINTER_STATUS_PENDING_DELETION  = 0x00000004
	PRINTER_STATUS_PAPER_JAM         = 0x00000008
	PRINTER_STATUS_PAPER_OUT         = 0x00000010
	PRINTER_STATUS_MANUAL_FEED       = 0x00000020
	PRINTER_STATUS_PAPER_PROBLEM     = 0x00000040
	PRINTER_STATUS_OFFLINE           = 0x00000080
	PRINTER_STATUS_IO_ACTIVE         = 0x00000100
	PRINTER_STATUS_BUSY              = 0x00000200
	PRINTER_STATUS_PRINTING          = 0x00000400
	PRINTER_STATUS_OUTPUT_BIN_FULL   = 0x00000800
	PRINTER_STATUS_NOT_AVAILABLE     = 0x00001000
	PRINTER_STATUS_WAITING           = 0x00002000
	PRINTER_STATUS_PROCESSING        = 0x00004000
	PRINTER_STATUS_INITIALIZING      = 0x00008000
	PRINTER_STATUS_WARMING_UP        = 0x00010000
	PRINTER_STATUS_TONER_LOW         = 0x00020000
	PRINTER_STATUS_NO_TONER          = 0x00040000
	PRINTER_STATUS_PAGE_PUNT         = 0x00080000
	PRINTER_STATUS_USER_INTERVENTION = 0x00100000
	PRINTER_STATUS_OUT_OF_MEMORY     = 0x00200000
	PRINTER_STATUS_DOOR_OPEN         = 0x00400000
	PRINTER_STATUS_SERVER_UNKNOWN    = 0x00800000
	PRINTER_STATUS_POWER_SAVE        = 0x01000000

	// Job status flags
	JOB_STATUS_PAUSED            = 0x00000001
	JOB_STATUS_ERROR             = 0x00000002
	JOB_STATUS_DELETING          = 0x00000004
	JOB_STATUS_SPOOLING          = 0x00000008
	JOB_STATUS_PRINTING          = 0x00000010
	JOB_STATUS_OFFLINE           = 0x00000020
	JOB_STATUS_PAPEROUT          = 0x00000040
	JOB_STATUS_PRINTED           = 0x00000080
	JOB_STATUS_DELETED           = 0x00000100
	JOB_STATUS_BLOCKED_DEVQ      = 0x00000200
	JOB_STATUS_USER_INTERVENTION = 0x00000400
	JOB_STATUS_RESTART           = 0x00000800
	JOB_STATUS_COMPLETE          = 0x00001000
	JOB_STATUS_RETAINED          = 0x00002000
)

// PRINTER_INFO_2 structure (Windows)
type printerInfo2 struct {
	ServerName         *uint16
	PrinterName        *uint16
	ShareName          *uint16
	PortName           *uint16
	DriverName         *uint16
	Comment            *uint16
	Location           *uint16
	DevMode            uintptr
	SepFile            *uint16
	PrintProcessor     *uint16
	Datatype           *uint16
	Parameters         *uint16
	SecurityDescriptor uintptr
	Attributes         uint32
	Priority           uint32
	DefaultPriority    uint32
	StartTime          uint32
	UntilTime          uint32
	Status             uint32
	Jobs               uint32
	AveragePPM         uint32
}

// JOB_INFO_2 structure (Windows) - includes DevMode for color detection
type jobInfo2 struct {
	JobID              uint32
	PrinterName        *uint16
	MachineName        *uint16
	UserName           *uint16
	Document           *uint16
	NotifyName         *uint16
	Datatype           *uint16
	PrintProcessor     *uint16
	Parameters         *uint16
	DriverName         *uint16
	DevMode            *devMode
	Status             *uint16
	SecurityDescriptor uintptr
	StatusCode         uint32
	Priority           uint32
	Position           uint32
	StartTime          uint32
	UntilTime          uint32
	TotalPages         uint32
	Size               uint32
	Submitted          syscall.Systemtime
	Time               uint32
	PagesPrinted       uint32
}

// DEVMODE structure (partial - we only need the color field)
// The full structure is much larger, but we access fields by offset
type devMode struct {
	DeviceName    [32]uint16
	SpecVersion   uint16
	DriverVersion uint16
	Size          uint16
	DriverExtra   uint16
	Fields        uint32
	// Union of orientation/paper/etc...
	Orientation      int16
	PaperSize        int16
	PaperLength      int16
	PaperWidth       int16
	Scale            int16
	Copies           int16
	DefaultSource    int16
	PrintQuality     int16
	Color            int16 // DMCOLOR_MONOCHROME=1, DMCOLOR_COLOR=2
	Duplex           int16
	YResolution      int16
	TTOption         int16
	Collate          int16
	FormName         [32]uint16
	LogPixels        uint16
	BitsPerPel       uint32
	PelsWidth        uint32
	PelsHeight       uint32
	DisplayFlags     uint32
	DisplayFrequency uint32
	// More fields follow but we don't need them
}

// DEVMODE field constants
const (
	DM_COLOR           = 0x00000800 // dmColor field is valid
	DMCOLOR_MONOCHROME = 1
	DMCOLOR_COLOR      = 2
)

// Watcher monitors the Windows print spooler for printer and job changes
type Watcher struct {
	config WatcherConfig
	logger Logger

	mu       sync.RWMutex
	printers map[string]*LocalPrinter        // keyed by printer name
	jobs     map[string]map[uint32]*PrintJob // keyed by printer name, then job ID

	// Event channel for job state changes
	events chan JobEvent

	// Lifecycle
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running bool
}

// NewWatcher creates a new spooler watcher
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
		events:   make(chan JobEvent, 100),
	}
}

// Start begins monitoring the print spooler
func (w *Watcher) Start() error {
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

	w.logger.Info("Spooler watcher started", "poll_interval", w.config.PollInterval)
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
	w.logger.Info("Spooler watcher stopped")
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
		// Return a copy
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

// refreshPrinters enumerates all printers from the spooler
func (w *Watcher) refreshPrinters() error {
	// Get required buffer size
	var needed, returned uint32
	flags := uint32(PRINTER_ENUM_LOCAL | PRINTER_ENUM_CONNECTIONS)

	procEnumPrinters.Call(
		uintptr(flags),
		0, // pName (NULL for all)
		2, // Level 2 for PRINTER_INFO_2
		0, // pPrinterEnum (NULL to get size)
		0, // cbBuf
		uintptr(unsafe.Pointer(&needed)),
		uintptr(unsafe.Pointer(&returned)),
	)

	if needed == 0 {
		return nil
	}

	// Allocate buffer and enumerate
	buf := make([]byte, needed)
	ret, _, _ := procEnumPrinters.Call(
		uintptr(flags),
		0,
		2,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(needed),
		uintptr(unsafe.Pointer(&needed)),
		uintptr(unsafe.Pointer(&returned)),
	)

	if ret == 0 {
		return syscall.GetLastError()
	}

	// Get default printer name for comparison
	defaultPrinter := w.getDefaultPrinter()

	// Parse results
	now := time.Now()
	seen := make(map[string]bool)

	structSize := unsafe.Sizeof(printerInfo2{})
	for i := uint32(0); i < returned; i++ {
		info := (*printerInfo2)(unsafe.Pointer(&buf[uintptr(i)*structSize]))

		name := utf16PtrToString(info.PrinterName)
		if name == "" {
			continue
		}

		portName := utf16PtrToString(info.PortName)
		driverName := utf16PtrToString(info.DriverName)
		printerType := classifyPrinter(portName, driverName)

		// Filter based on config
		if printerType == PrinterTypeNetwork && !w.config.IncludeNetworkPrinters {
			continue
		}
		if printerType == PrinterTypeVirtual && !w.config.IncludeVirtualPrinters {
			continue
		}

		seen[name] = true

		w.mu.Lock()
		printer, exists := w.printers[name]
		if !exists {
			// New printer discovered
			printer = &LocalPrinter{
				Name:       name,
				PortName:   portName,
				DriverName: driverName,
				Type:       printerType,
				FirstSeen:  now,
				TrackingEnabled: (printerType == PrinterTypeUSB && w.config.AutoTrackUSB) ||
					(printerType == PrinterTypeLocal && w.config.AutoTrackLocal),
			}
			w.printers[name] = printer
			w.jobs[name] = make(map[uint32]*PrintJob)
			w.logger.Info("Discovered local printer",
				"name", name,
				"port", portName,
				"type", printerType,
				"tracking", printer.TrackingEnabled)
		}

		// Update printer info
		printer.PortName = portName
		printer.DriverName = driverName
		printer.Type = printerType
		printer.IsDefault = (name == defaultPrinter)
		printer.IsShared = (info.Attributes & 0x00000008) != 0 // PRINTER_ATTRIBUTE_SHARED
		printer.Status = statusCodeToString(info.Status)
		printer.StatusCode = info.Status
		printer.JobCount = int(info.Jobs)
		printer.LastSeen = now

		// Try to parse manufacturer/model from driver name
		if printer.Manufacturer == "" || printer.Model == "" {
			mfg, model := parseDriverName(driverName)
			if printer.Manufacturer == "" {
				printer.Manufacturer = mfg
			}
			if printer.Model == "" {
				printer.Model = model
			}
		}

		w.mu.Unlock()
	}

	// Mark printers not seen as offline
	w.mu.Lock()
	for name, printer := range w.printers {
		if !seen[name] {
			printer.Status = "offline"
		}
	}
	w.mu.Unlock()

	return nil
}

// refreshJobs enumerates jobs for all tracked printers
func (w *Watcher) refreshJobs() {
	w.mu.RLock()
	printerNames := make([]string, 0, len(w.printers))
	for name, p := range w.printers {
		if p.TrackingEnabled {
			printerNames = append(printerNames, name)
		}
	}
	w.mu.RUnlock()

	for _, name := range printerNames {
		w.refreshPrinterJobs(name)
	}
}

// refreshPrinterJobs enumerates jobs for a specific printer
func (w *Watcher) refreshPrinterJobs(printerName string) {
	// Open printer
	printerNameUTF16, _ := syscall.UTF16PtrFromString(printerName)
	var handle syscall.Handle
	ret, _, _ := procOpenPrinter.Call(
		uintptr(unsafe.Pointer(printerNameUTF16)),
		uintptr(unsafe.Pointer(&handle)),
		0,
	)
	if ret == 0 {
		return
	}
	defer procClosePrinter.Call(uintptr(handle))

	// Get required buffer size using JOB_INFO_2 (level 2) for color info
	var needed, returned uint32
	procEnumJobs.Call(
		uintptr(handle),
		0,   // FirstJob
		100, // NoJobs (max to enumerate)
		2,   // Level (JOB_INFO_2 for DevMode/color info)
		0,   // pJob
		0,   // cbBuf
		uintptr(unsafe.Pointer(&needed)),
		uintptr(unsafe.Pointer(&returned)),
	)

	if needed == 0 {
		// No jobs - check for completed jobs
		w.checkCompletedJobs(printerName, nil)
		return
	}

	// Allocate buffer and enumerate
	buf := make([]byte, needed)
	ret, _, _ = procEnumJobs.Call(
		uintptr(handle),
		0,
		100,
		2, // JOB_INFO_2
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(needed),
		uintptr(unsafe.Pointer(&needed)),
		uintptr(unsafe.Pointer(&returned)),
	)

	if ret == 0 {
		return
	}

	// Parse jobs
	now := time.Now()
	currentJobs := make(map[uint32]bool)
	structSize := unsafe.Sizeof(jobInfo2{})

	for i := uint32(0); i < returned; i++ {
		info := (*jobInfo2)(unsafe.Pointer(&buf[uintptr(i)*structSize]))

		jobID := info.JobID
		currentJobs[jobID] = true

		// Detect color from DevMode
		isColor := false
		if info.DevMode != nil {
			// Check if color field is valid and set to color
			if info.DevMode.Fields&DM_COLOR != 0 {
				isColor = info.DevMode.Color == DMCOLOR_COLOR
			}
		}

		job := &PrintJob{
			JobID:        jobID,
			PrinterName:  printerName,
			DocumentName: utf16PtrToString(info.Document),
			UserName:     utf16PtrToString(info.UserName),
			MachineName:  utf16PtrToString(info.MachineName),
			Status:       jobStatusToString(info.StatusCode),
			StatusCode:   info.StatusCode,
			Priority:     info.Priority,
			TotalPages:   int32(info.TotalPages),
			PagesPrinted: int32(info.PagesPrinted),
			Size:         int64(info.Size),
			Submitted:    systemtimeToTime(info.Submitted),
			IsColor:      isColor,
		}

		w.mu.Lock()
		printerJobs := w.jobs[printerName]
		if printerJobs == nil {
			printerJobs = make(map[uint32]*PrintJob)
			w.jobs[printerName] = printerJobs
		}

		existingJob, exists := printerJobs[jobID]
		if !exists {
			// New job
			printerJobs[jobID] = job
			w.mu.Unlock()

			w.emitEvent(JobEvent{
				Type:        JobEventAdded,
				Job:         job,
				PrinterName: printerName,
				Timestamp:   now,
			})
		} else {
			// Check for state changes
			wasStarted := existingJob.StatusCode&JOB_STATUS_PRINTING != 0
			isStarted := info.StatusCode&JOB_STATUS_PRINTING != 0

			printerJobs[jobID] = job
			w.mu.Unlock()

			if !wasStarted && isStarted {
				w.emitEvent(JobEvent{
					Type:        JobEventStarted,
					Job:         job,
					PrinterName: printerName,
					Timestamp:   now,
				})
			}
		}
	}

	// Check for completed/deleted jobs
	w.checkCompletedJobs(printerName, currentJobs)
}

// checkCompletedJobs finds jobs that are no longer in the queue
func (w *Watcher) checkCompletedJobs(printerName string, currentJobs map[uint32]bool) {
	w.mu.Lock()
	printerJobs := w.jobs[printerName]
	if printerJobs == nil {
		w.mu.Unlock()
		return
	}

	var completedJobs []*PrintJob
	for jobID, job := range printerJobs {
		if currentJobs == nil || !currentJobs[jobID] {
			completedJobs = append(completedJobs, job)
			delete(printerJobs, jobID)
		}
	}
	w.mu.Unlock()

	// Emit completion events and update page counts
	now := time.Now()
	for _, job := range completedJobs {
		// Determine pages printed
		pages := job.TotalPages
		if pages <= 0 {
			pages = job.PagesPrinted
		}
		if pages <= 0 {
			pages = 1 // Assume at least 1 page if we can't determine
		}

		// Update printer page count, tracking color vs mono separately
		w.mu.Lock()
		if printer, ok := w.printers[printerName]; ok && printer.TrackingEnabled {
			printer.TotalPages += int64(pages)
			if job.IsColor {
				printer.TotalColorPages += int64(pages)
			} else {
				printer.TotalMonoPages += int64(pages)
			}
			printer.LastPageUpdate = now
			w.logger.Debug("Updated page count",
				"printer", printerName,
				"job", job.DocumentName,
				"pages", pages,
				"color", job.IsColor,
				"total", printer.TotalPages)
		}
		w.mu.Unlock()

		w.emitEvent(JobEvent{
			Type:        JobEventCompleted,
			Job:         job,
			PrinterName: printerName,
			Timestamp:   now,
			PagesAdded:  pages,
		})
	}
}

// emitEvent sends an event to the events channel (non-blocking)
func (w *Watcher) emitEvent(event JobEvent) {
	select {
	case w.events <- event:
	default:
		w.logger.Warn("Event channel full, dropping event", "type", event.Type, "printer", event.PrinterName)
	}
}

// getDefaultPrinter returns the name of the default printer
func (w *Watcher) getDefaultPrinter() string {
	var needed uint32
	procGetDefaultPrinter.Call(0, uintptr(unsafe.Pointer(&needed)))
	if needed == 0 {
		return ""
	}

	buf := make([]uint16, needed)
	ret, _, _ := procGetDefaultPrinter.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&needed)),
	)
	if ret == 0 {
		return ""
	}

	return syscall.UTF16ToString(buf)
}

// Helper functions

func utf16PtrToString(p *uint16) string {
	if p == nil {
		return ""
	}
	// Find null terminator
	var s []uint16
	for ptr := p; *ptr != 0; ptr = (*uint16)(unsafe.Pointer(uintptr(unsafe.Pointer(ptr)) + 2)) {
		s = append(s, *ptr)
	}
	return syscall.UTF16ToString(s)
}

func classifyPrinter(portName, driverName string) PrinterType {
	portUpper := strings.ToUpper(portName)
	driverUpper := strings.ToUpper(driverName)

	// Check for USB
	if strings.HasPrefix(portUpper, "USB") {
		return PrinterTypeUSB
	}

	// Check for virtual printers
	if strings.Contains(driverUpper, "PDF") ||
		strings.Contains(driverUpper, "XPS") ||
		strings.Contains(driverUpper, "ONENOTE") ||
		strings.Contains(driverUpper, "FAX") ||
		strings.Contains(portUpper, "PORTPROMPT") ||
		strings.Contains(portUpper, "NUL:") {
		return PrinterTypeVirtual
	}

	// Check for network
	if strings.Contains(portUpper, "\\\\") || // UNC path
		strings.Contains(portUpper, ":") && !strings.HasPrefix(portUpper, "LPT") && !strings.HasPrefix(portUpper, "COM") {
		// Likely IP:port or network path
		return PrinterTypeNetwork
	}

	// Local ports
	if strings.HasPrefix(portUpper, "LPT") || strings.HasPrefix(portUpper, "COM") {
		return PrinterTypeLocal
	}

	return PrinterTypeUnknown
}

func statusCodeToString(status uint32) string {
	if status == 0 {
		return "ready"
	}
	if status&PRINTER_STATUS_OFFLINE != 0 {
		return "offline"
	}
	if status&PRINTER_STATUS_ERROR != 0 {
		return "error"
	}
	if status&PRINTER_STATUS_PRINTING != 0 {
		return "printing"
	}
	if status&PRINTER_STATUS_PAUSED != 0 {
		return "paused"
	}
	if status&PRINTER_STATUS_PAPER_OUT != 0 {
		return "paper_out"
	}
	if status&PRINTER_STATUS_PAPER_JAM != 0 {
		return "paper_jam"
	}
	if status&PRINTER_STATUS_NO_TONER != 0 {
		return "no_toner"
	}
	if status&PRINTER_STATUS_TONER_LOW != 0 {
		return "toner_low"
	}
	if status&PRINTER_STATUS_BUSY != 0 {
		return "busy"
	}
	if status&PRINTER_STATUS_WARMING_UP != 0 {
		return "warming_up"
	}
	if status&PRINTER_STATUS_POWER_SAVE != 0 {
		return "power_save"
	}
	return "unknown"
}

func jobStatusToString(status uint32) string {
	if status&JOB_STATUS_PRINTING != 0 {
		return "printing"
	}
	if status&JOB_STATUS_PRINTED != 0 {
		return "printed"
	}
	if status&JOB_STATUS_COMPLETE != 0 {
		return "complete"
	}
	if status&JOB_STATUS_SPOOLING != 0 {
		return "spooling"
	}
	if status&JOB_STATUS_PAUSED != 0 {
		return "paused"
	}
	if status&JOB_STATUS_ERROR != 0 {
		return "error"
	}
	if status&JOB_STATUS_DELETING != 0 {
		return "deleting"
	}
	if status&JOB_STATUS_DELETED != 0 {
		return "deleted"
	}
	if status&JOB_STATUS_OFFLINE != 0 {
		return "offline"
	}
	return "pending"
}

func systemtimeToTime(st syscall.Systemtime) time.Time {
	return time.Date(
		int(st.Year),
		time.Month(st.Month),
		int(st.Day),
		int(st.Hour),
		int(st.Minute),
		int(st.Second),
		int(st.Milliseconds)*1e6,
		time.Local,
	)
}

func parseDriverName(driverName string) (manufacturer, model string) {
	// Common patterns: "HP LaserJet Pro M404", "Canon iR-ADV C5535 III"
	// Try to extract manufacturer from first word
	parts := strings.Fields(driverName)
	if len(parts) == 0 {
		return "", driverName
	}

	first := strings.ToUpper(parts[0])
	knownManufacturers := map[string]string{
		"HP":      "Hewlett-Packard",
		"HEWLETT": "Hewlett-Packard",
		"CANON":   "Canon",
		"EPSON":   "Epson",
		"BROTHER": "Brother",
		"LEXMARK": "Lexmark",
		"XEROX":   "Xerox",
		"RICOH":   "Ricoh",
		"KYOCERA": "Kyocera",
		"KONICA":  "Konica Minolta",
		"SHARP":   "Sharp",
		"SAMSUNG": "Samsung",
		"OKI":     "OKI",
		"DELL":    "Dell",
		"FUJI":    "Fuji Xerox",
		"TOSHIBA": "Toshiba",
		"PANTUM":  "Pantum",
	}

	if mfg, ok := knownManufacturers[first]; ok {
		manufacturer = mfg
		if len(parts) > 1 {
			model = strings.Join(parts[1:], " ")
		}
	} else if first == "HEWLETT-PACKARD" || first == "HEWLETT" {
		manufacturer = "Hewlett-Packard"
		if len(parts) > 1 {
			// Skip "Packard" if present
			startIdx := 1
			if len(parts) > 1 && strings.ToUpper(parts[1]) == "PACKARD" {
				startIdx = 2
			}
			if startIdx < len(parts) {
				model = strings.Join(parts[startIdx:], " ")
			}
		}
	} else {
		model = driverName
	}

	return manufacturer, model
}
