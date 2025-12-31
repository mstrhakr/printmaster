//go:build windows
// +build windows

package usbproxy

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

// IsSupported returns whether USB proxy is supported on this platform
func IsSupported() bool {
	return true
}

// Windows DLL handles
var (
	kernel32  = syscall.NewLazyDLL("kernel32.dll")
	setupapi  = syscall.NewLazyDLL("setupapi.dll")
	winusbDll = syscall.NewLazyDLL("winusb.dll")

	procCreateFile                      = kernel32.NewProc("CreateFileW")
	procCloseHandle                     = kernel32.NewProc("CloseHandle")
	procSetupDiGetClassDevs             = setupapi.NewProc("SetupDiGetClassDevsW")
	procSetupDiEnumDeviceInterfaces     = setupapi.NewProc("SetupDiEnumDeviceInterfaces")
	procSetupDiGetDeviceInterfaceDetail = setupapi.NewProc("SetupDiGetDeviceInterfaceDetailW")
	procSetupDiDestroyDeviceInfoList    = setupapi.NewProc("SetupDiDestroyDeviceInfoList")
	procWinUsb_Initialize               = winusbDll.NewProc("WinUsb_Initialize")
	procWinUsb_Free                     = winusbDll.NewProc("WinUsb_Free")
	procWinUsb_QueryInterfaceSettings   = winusbDll.NewProc("WinUsb_QueryInterfaceSettings")
	procWinUsb_QueryPipe                = winusbDll.NewProc("WinUsb_QueryPipe")
	procWinUsb_SetPipePolicy            = winusbDll.NewProc("WinUsb_SetPipePolicy")
	procWinUsb_WritePipe                = winusbDll.NewProc("WinUsb_WritePipe")
	procWinUsb_ReadPipe                 = winusbDll.NewProc("WinUsb_ReadPipe")
)

// Windows constants
const (
	DIGCF_PRESENT         = 0x00000002
	DIGCF_DEVICEINTERFACE = 0x00000010
	DIGCF_ALLCLASSES      = 0x00000004
	INVALID_HANDLE_VALUE  = ^uintptr(0)
	GENERIC_READ          = 0x80000000
	GENERIC_WRITE         = 0x40000000
	FILE_SHARE_READ       = 0x00000001
	FILE_SHARE_WRITE      = 0x00000002
	OPEN_EXISTING         = 3
	FILE_FLAG_OVERLAPPED  = 0x40000000
	PIPE_TRANSFER_TIMEOUT = 0x03
	RAW_IO                = 0x07
	SPDRP_HARDWAREID      = 0x00000001
	SPDRP_DEVICEDESC      = 0x00000000
)

// GUID for WinUSB devices
var GUID_DEVINTERFACE_WINUSB = syscall.GUID{
	Data1: 0xDEE824EF,
	Data2: 0x729B,
	Data3: 0x4A0E,
	Data4: [8]byte{0x9C, 0x14, 0xB7, 0x11, 0x7D, 0x33, 0xA8, 0x17},
}

// GUID for USB devices (general)
var GUID_DEVINTERFACE_USB_DEVICE = syscall.GUID{
	Data1: 0xA5DCBF10,
	Data2: 0x6530,
	Data3: 0x11D2,
	Data4: [8]byte{0x90, 0x1F, 0x00, 0xC0, 0x4F, 0xB9, 0x51, 0xED},
}

// Windows structures
type spDeviceInterfaceData struct {
	Size     uint32
	GUID     syscall.GUID
	Flags    uint32
	Reserved uintptr
}

type usbInterfaceDescriptor struct {
	Length            byte
	DescriptorType    byte
	InterfaceNumber   byte
	AlternateSetting  byte
	NumEndpoints      byte
	InterfaceClass    byte
	InterfaceSubClass byte
	InterfaceProtocol byte
	Interface         byte
}

type winusbPipeInformation struct {
	PipeType          uint32
	PipeId            byte
	MaximumPacketSize uint16
	Interval          byte
}

// WindowsEnumerator implements USBDeviceEnumerator for Windows
type WindowsEnumerator struct {
	logger Logger
}

// NewEnumerator creates a USB device enumerator for Windows
func NewEnumerator(logger Logger) (USBDeviceEnumerator, error) {
	if logger == nil {
		logger = nullLogger{}
	}
	return &WindowsEnumerator{logger: logger}, nil
}

// Enumerate finds all USB printers that might support IPP-USB via WinUSB
func (e *WindowsEnumerator) Enumerate() ([]*USBPrinter, error) {
	var printers []*USBPrinter

	// Get device info set for WinUSB devices
	hDevInfo, _, err := procSetupDiGetClassDevs.Call(
		uintptr(unsafe.Pointer(&GUID_DEVINTERFACE_WINUSB)),
		0, 0,
		DIGCF_PRESENT|DIGCF_DEVICEINTERFACE,
	)

	if hDevInfo == INVALID_HANDLE_VALUE {
		return nil, fmt.Errorf("SetupDiGetClassDevs failed: %v", err)
	}
	defer procSetupDiDestroyDeviceInfoList.Call(hDevInfo)

	var deviceIndex uint32 = 0
	for {
		var interfaceData spDeviceInterfaceData
		interfaceData.Size = uint32(unsafe.Sizeof(interfaceData))

		ret, _, _ := procSetupDiEnumDeviceInterfaces.Call(
			hDevInfo, 0,
			uintptr(unsafe.Pointer(&GUID_DEVINTERFACE_WINUSB)),
			uintptr(deviceIndex),
			uintptr(unsafe.Pointer(&interfaceData)),
		)

		if ret == 0 {
			break
		}

		devicePath := e.getDeviceInterfaceDetail(hDevInfo, &interfaceData)
		if devicePath == "" {
			deviceIndex++
			continue
		}

		// Parse VID/PID/MI from device path
		vid, pid, mi := parseDevicePath(devicePath)
		if vid == 0 || pid == 0 {
			deviceIndex++
			continue
		}

		// Check if this looks like a printer (by VID or interface)
		isPrinter := false
		if _, ok := KnownPrinterVendors[vid]; ok {
			isPrinter = true
		}

		// Also check if the interface is a printer class interface
		// by examining the interface number (MI_xx in the path)
		interfaceInfo := e.getInterfaceInfo(devicePath)
		if interfaceInfo != nil && interfaceInfo.IsHTTPCapable() {
			isPrinter = true
		}

		if !isPrinter {
			deviceIndex++
			continue
		}

		// Get additional device info from registry
		serial, product, manufacturer := e.getDeviceInfoFromRegistry(devicePath, vid, pid)

		printer := &USBPrinter{
			DevicePath:      devicePath,
			VendorID:        vid,
			ProductID:       pid,
			InterfaceNumber: mi,
			Manufacturer:    manufacturer,
			Product:         product,
			SerialNumber:    serial,
			Status:          USBPrinterStatusAvailable,
			FirstSeen:       time.Now(),
			LastSeen:        time.Now(),
		}

		// Try to match with spooler port
		printer.SpoolerPortName = e.matchSpoolerPort(vid, pid, serial)

		e.logger.Debug("Found USB printer",
			"path", devicePath,
			"vid", fmt.Sprintf("%04X", vid),
			"pid", fmt.Sprintf("%04X", pid),
			"mi", mi,
			"manufacturer", manufacturer,
			"product", product,
			"serial", serial,
			"spooler_port", printer.SpoolerPortName)

		printers = append(printers, printer)
		deviceIndex++
	}

	return printers, nil
}

// GetDeviceDetails retrieves detailed information about a specific device
func (e *WindowsEnumerator) GetDeviceDetails(devicePath string) (*USBPrinter, error) {
	vid, pid, mi := parseDevicePath(devicePath)
	if vid == 0 {
		return nil, errors.New("invalid device path")
	}

	serial, product, manufacturer := e.getDeviceInfoFromRegistry(devicePath, vid, pid)

	printer := &USBPrinter{
		DevicePath:      devicePath,
		VendorID:        vid,
		ProductID:       pid,
		InterfaceNumber: mi,
		Manufacturer:    manufacturer,
		Product:         product,
		SerialNumber:    serial,
		Status:          USBPrinterStatusAvailable,
		LastSeen:        time.Now(),
	}

	return printer, nil
}

// CreateTransport creates a USB transport for the specified printer
func (e *WindowsEnumerator) CreateTransport(printer *USBPrinter) (USBTransport, error) {
	return NewWindowsTransport(printer.DevicePath, e.logger)
}

// getDeviceInterfaceDetail gets the device path string
func (e *WindowsEnumerator) getDeviceInterfaceDetail(hDevInfo uintptr, interfaceData *spDeviceInterfaceData) string {
	var requiredSize uint32
	procSetupDiGetDeviceInterfaceDetail.Call(
		hDevInfo,
		uintptr(unsafe.Pointer(interfaceData)),
		0, 0,
		uintptr(unsafe.Pointer(&requiredSize)),
		0,
	)

	if requiredSize == 0 {
		return ""
	}

	detailBuf := make([]byte, requiredSize)
	// Set cbSize field (4 bytes for size + 4 bytes for path on 64-bit, 6 bytes on 32-bit)
	if unsafe.Sizeof(uintptr(0)) == 8 {
		*(*uint32)(unsafe.Pointer(&detailBuf[0])) = 8
	} else {
		*(*uint32)(unsafe.Pointer(&detailBuf[0])) = 6
	}

	ret, _, _ := procSetupDiGetDeviceInterfaceDetail.Call(
		hDevInfo,
		uintptr(unsafe.Pointer(interfaceData)),
		uintptr(unsafe.Pointer(&detailBuf[0])),
		uintptr(requiredSize),
		0, 0,
	)

	if ret == 0 {
		return ""
	}

	pathPtr := unsafe.Pointer(&detailBuf[4])
	return syscall.UTF16ToString((*[256]uint16)(pathPtr)[:])
}

// getInterfaceInfo opens the device temporarily to get interface information
func (e *WindowsEnumerator) getInterfaceInfo(devicePath string) *USBInterface {
	pathPtr, _ := syscall.UTF16PtrFromString(devicePath)

	handle, _, _ := procCreateFile.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		GENERIC_READ|GENERIC_WRITE,
		FILE_SHARE_READ|FILE_SHARE_WRITE,
		0,
		OPEN_EXISTING,
		FILE_FLAG_OVERLAPPED,
		0,
	)

	if handle == INVALID_HANDLE_VALUE {
		return nil
	}
	defer procCloseHandle.Call(handle)

	var wHandle uintptr
	ret, _, _ := procWinUsb_Initialize.Call(handle, uintptr(unsafe.Pointer(&wHandle)))
	if ret == 0 {
		return nil
	}
	defer procWinUsb_Free.Call(wHandle)

	var ifaceDesc usbInterfaceDescriptor
	ret, _, _ = procWinUsb_QueryInterfaceSettings.Call(wHandle, 0, uintptr(unsafe.Pointer(&ifaceDesc)))
	if ret == 0 {
		return nil
	}

	info := &USBInterface{
		Number:       ifaceDesc.InterfaceNumber,
		AltSetting:   ifaceDesc.AlternateSetting,
		Class:        ifaceDesc.InterfaceClass,
		SubClass:     ifaceDesc.InterfaceSubClass,
		Protocol:     ifaceDesc.InterfaceProtocol,
		NumEndpoints: ifaceDesc.NumEndpoints,
	}

	// Find bulk endpoints
	for i := byte(0); i < ifaceDesc.NumEndpoints; i++ {
		var pipeInfo winusbPipeInformation
		ret, _, _ := procWinUsb_QueryPipe.Call(wHandle, 0, uintptr(i), uintptr(unsafe.Pointer(&pipeInfo)))
		if ret != 0 {
			if pipeInfo.PipeId&0x80 != 0 {
				info.InEndpoint = pipeInfo.PipeId
			} else {
				info.OutEndpoint = pipeInfo.PipeId
			}
			if pipeInfo.MaximumPacketSize > info.MaxPacketSize {
				info.MaxPacketSize = pipeInfo.MaximumPacketSize
			}
		}
	}

	return info
}

// getDeviceInfoFromRegistry retrieves device info from Windows registry
func (e *WindowsEnumerator) getDeviceInfoFromRegistry(devicePath string, vid, pid uint16) (serial, product, manufacturer string) {
	// Parse serial from device path if available
	// Device path format: \\?\usb#vid_03f0&pid_422a#SERIAL#{guid}
	pathUpper := strings.ToUpper(devicePath)

	// Try to extract serial from the path
	parts := strings.Split(pathUpper, "#")
	if len(parts) >= 3 {
		// The third part is typically the serial number
		potentialSerial := parts[2]
		// Serial numbers don't contain '&' - interface instances do
		if !strings.Contains(potentialSerial, "&") && potentialSerial != "" {
			serial = parts[2] // Keep original case
			if idx := strings.Index(devicePath, "#"); idx > 0 {
				if idx2 := strings.Index(devicePath[idx+1:], "#"); idx2 > 0 {
					if idx3 := strings.Index(devicePath[idx+idx2+2:], "#"); idx3 > 0 {
						serial = devicePath[idx+idx2+2 : idx+idx2+2+idx3]
					}
				}
			}
		}
	}

	// Look up device info in registry
	vidPidKey := fmt.Sprintf("VID_%04X&PID_%04X", vid, pid)
	usbKey, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Enum\USB\`+vidPidKey, registry.READ)
	if err == nil {
		defer usbKey.Close()

		// Enumerate instances to find one with matching serial or get info
		instances, _ := usbKey.ReadSubKeyNames(-1)
		for _, instance := range instances {
			instanceKey, err := registry.OpenKey(usbKey, instance, registry.READ)
			if err != nil {
				continue
			}

			// Get device description (usually product name)
			if desc, _, err := instanceKey.GetStringValue("DeviceDesc"); err == nil {
				// Format is often "@driver.inf,...;Description" - extract just the description
				if idx := strings.LastIndex(desc, ";"); idx >= 0 {
					product = desc[idx+1:]
				} else {
					product = desc
				}
			}

			// Get manufacturer
			if mfg, _, err := instanceKey.GetStringValue("Mfg"); err == nil {
				if idx := strings.LastIndex(mfg, ";"); idx >= 0 {
					manufacturer = mfg[idx+1:]
				} else {
					manufacturer = mfg
				}
			}

			// If no serial from path and this instance doesn't have '&', use it as serial
			if serial == "" && !strings.Contains(instance, "&") {
				serial = instance
			}

			instanceKey.Close()

			// If we found a matching serial, use this instance's info
			if serial != "" && strings.EqualFold(instance, serial) {
				break
			}
		}
	}

	// Fall back to vendor name lookup if no manufacturer found
	if manufacturer == "" {
		manufacturer = GetVendorName(vid)
	}

	return serial, product, manufacturer
}

// matchSpoolerPort tries to match this USB device with a Windows spooler port
func (e *WindowsEnumerator) matchSpoolerPort(vid, pid uint16, serial string) string {
	if serial == "" {
		return ""
	}

	// Look in USBPRINT registry key for matching device
	usbPrintKey, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Enum\USBPRINT`, registry.READ)
	if err != nil {
		return ""
	}
	defer usbPrintKey.Close()

	printerNames, _ := usbPrintKey.ReadSubKeyNames(-1)
	for _, printerName := range printerNames {
		printerKey, err := registry.OpenKey(usbPrintKey, printerName, registry.READ)
		if err != nil {
			continue
		}

		instances, _ := printerKey.ReadSubKeyNames(-1)
		printerKey.Close()

		for _, instance := range instances {
			// Instance format is often "7&xxxxx&0&USBxxx"
			// The USBxxx at the end is the port name
			instanceUpper := strings.ToUpper(instance)
			if strings.Contains(instanceUpper, "USB") {
				// Extract port name (USBxxx)
				idx := strings.LastIndex(instanceUpper, "&USB")
				if idx >= 0 {
					portName := instance[idx+1:]
					// Verify this is associated with our device by checking ContainerID
					instanceKey, err := registry.OpenKey(usbPrintKey, printerName+`\`+instance, registry.READ)
					if err == nil {
						containerID, _, _ := instanceKey.GetStringValue("ContainerID")
						instanceKey.Close()

						// Check if this ContainerID matches our USB device's ContainerID
						if containerID != "" {
							usbContainerID := e.getUSBDeviceContainerID(vid, pid, serial)
							if strings.EqualFold(containerID, usbContainerID) {
								return portName
							}
						}
					}
				}
			}
		}
	}

	return ""
}

// getUSBDeviceContainerID gets the ContainerID for a USB device
func (e *WindowsEnumerator) getUSBDeviceContainerID(vid, pid uint16, serial string) string {
	if serial == "" {
		return ""
	}

	vidPidKey := fmt.Sprintf("VID_%04X&PID_%04X", vid, pid)
	instanceKey, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Enum\USB\`+vidPidKey+`\`+serial, registry.READ)
	if err != nil {
		return ""
	}
	defer instanceKey.Close()

	containerID, _, _ := instanceKey.GetStringValue("ContainerID")
	return containerID
}

// parseDevicePath extracts VID, PID, and MI (interface) from a device path
func parseDevicePath(path string) (vid, pid uint16, mi uint8) {
	pathUpper := strings.ToUpper(path)

	// Find VID
	if idx := strings.Index(pathUpper, "VID_"); idx >= 0 && len(pathUpper) > idx+8 {
		fmt.Sscanf(pathUpper[idx+4:idx+8], "%04X", &vid)
	}

	// Find PID
	if idx := strings.Index(pathUpper, "PID_"); idx >= 0 && len(pathUpper) > idx+8 {
		fmt.Sscanf(pathUpper[idx+4:idx+8], "%04X", &pid)
	}

	// Find MI (interface number)
	if idx := strings.Index(pathUpper, "MI_"); idx >= 0 && len(pathUpper) > idx+5 {
		var miInt int
		fmt.Sscanf(pathUpper[idx+3:idx+5], "%02X", &miInt)
		mi = uint8(miInt)
	}

	return
}

// WindowsTransport implements USBTransport for Windows using WinUSB
type WindowsTransport struct {
	mu           sync.Mutex
	devicePath   string
	deviceHandle uintptr
	winusbHandle uintptr
	outPipe      byte
	inPipe       byte
	maxPacket    uint16
	logger       Logger
	isOpen       bool
	readTimeout  time.Duration
	writeTimeout time.Duration
}

// NewWindowsTransport creates a new Windows USB transport
func NewWindowsTransport(devicePath string, logger Logger) (*WindowsTransport, error) {
	if logger == nil {
		logger = nullLogger{}
	}
	return &WindowsTransport{
		devicePath:   devicePath,
		logger:       logger,
		readTimeout:  10 * time.Second,
		writeTimeout: 10 * time.Second,
	}, nil
}

// Open initializes the USB connection
func (t *WindowsTransport) Open() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.isOpen {
		return nil
	}

	pathPtr, _ := syscall.UTF16PtrFromString(t.devicePath)

	handle, _, err := procCreateFile.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		GENERIC_READ|GENERIC_WRITE,
		FILE_SHARE_READ|FILE_SHARE_WRITE,
		0,
		OPEN_EXISTING,
		FILE_FLAG_OVERLAPPED,
		0,
	)

	if handle == INVALID_HANDLE_VALUE {
		return fmt.Errorf("CreateFile failed: %v", err)
	}
	t.deviceHandle = handle

	var wHandle uintptr
	ret, _, err := procWinUsb_Initialize.Call(handle, uintptr(unsafe.Pointer(&wHandle)))
	if ret == 0 {
		procCloseHandle.Call(handle)
		return fmt.Errorf("WinUsb_Initialize failed: %v", err)
	}
	t.winusbHandle = wHandle

	// Query interface settings
	var ifaceDesc usbInterfaceDescriptor
	ret, _, _ = procWinUsb_QueryInterfaceSettings.Call(t.winusbHandle, 0, uintptr(unsafe.Pointer(&ifaceDesc)))
	if ret == 0 {
		t.close()
		return errors.New("WinUsb_QueryInterfaceSettings failed")
	}

	t.logger.Debug("USB interface info",
		"class", fmt.Sprintf("0x%02X", ifaceDesc.InterfaceClass),
		"subclass", fmt.Sprintf("0x%02X", ifaceDesc.InterfaceSubClass),
		"protocol", fmt.Sprintf("0x%02X", ifaceDesc.InterfaceProtocol),
		"endpoints", ifaceDesc.NumEndpoints)

	// Find bulk IN and OUT endpoints
	for i := byte(0); i < ifaceDesc.NumEndpoints; i++ {
		var pipeInfo winusbPipeInformation
		ret, _, _ := procWinUsb_QueryPipe.Call(t.winusbHandle, 0, uintptr(i), uintptr(unsafe.Pointer(&pipeInfo)))
		if ret != 0 {
			// Check if bulk transfer (PipeType 2)
			if pipeInfo.PipeType == 2 {
				if pipeInfo.PipeId&0x80 != 0 {
					t.inPipe = pipeInfo.PipeId
				} else {
					t.outPipe = pipeInfo.PipeId
				}
				if pipeInfo.MaximumPacketSize > t.maxPacket {
					t.maxPacket = pipeInfo.MaximumPacketSize
				}
			}
		}
	}

	if t.outPipe == 0 || t.inPipe == 0 {
		t.close()
		return errors.New("could not find bulk IN/OUT endpoints")
	}

	// Set read timeout
	timeout := uint32(t.readTimeout.Milliseconds())
	procWinUsb_SetPipePolicy.Call(t.winusbHandle, uintptr(t.inPipe), PIPE_TRANSFER_TIMEOUT, 4, uintptr(unsafe.Pointer(&timeout)))

	t.logger.Debug("USB transport opened",
		"path", t.devicePath,
		"out_pipe", fmt.Sprintf("0x%02X", t.outPipe),
		"in_pipe", fmt.Sprintf("0x%02X", t.inPipe),
		"max_packet", t.maxPacket)

	t.isOpen = true
	return nil
}

// Close releases the USB connection
func (t *WindowsTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.close()
}

func (t *WindowsTransport) close() error {
	if t.winusbHandle != 0 {
		procWinUsb_Free.Call(t.winusbHandle)
		t.winusbHandle = 0
	}
	if t.deviceHandle != 0 {
		procCloseHandle.Call(t.deviceHandle)
		t.deviceHandle = 0
	}
	t.isOpen = false
	return nil
}

// IsOpen returns whether the transport is currently open
func (t *WindowsTransport) IsOpen() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.isOpen
}

// DevicePath returns the device path this transport is connected to
func (t *WindowsTransport) DevicePath() string {
	return t.devicePath
}

// RoundTrip implements http.RoundTripper - sends HTTP request over USB
func (t *WindowsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.isOpen {
		return nil, errors.New("transport not open")
	}

	// Flush any stale data in the pipe
	t.flushPipe()

	// Clone and modify request for the printer
	outReq := req.Clone(req.Context())
	outReq.Host = "localhost"
	outReq.URL.Host = "localhost"
	outReq.URL.Scheme = "http"

	// Remove hop-by-hop headers
	removeHopByHopHeaders(outReq.Header)

	// Don't use keep-alive
	outReq.Close = true
	outReq.Header.Set("Connection", "close")

	// Add User-Agent if missing
	if outReq.Header.Get("User-Agent") == "" {
		outReq.Header.Set("User-Agent", "PrintMaster-USB-Proxy/1.0")
	}

	// Serialize request
	var reqBuf bytes.Buffer
	if err := outReq.Write(&reqBuf); err != nil {
		return nil, fmt.Errorf("failed to serialize request: %v", err)
	}

	// Send via USB
	if err := t.usbWrite(reqBuf.Bytes()); err != nil {
		return nil, fmt.Errorf("USB write failed: %v", err)
	}

	// Give printer time to process
	time.Sleep(50 * time.Millisecond)

	// Read response
	rawResponse, err := t.readFullResponse()
	if err != nil {
		return nil, fmt.Errorf("USB read failed: %v", err)
	}

	if len(rawResponse) == 0 {
		return nil, errors.New("empty response from printer")
	}

	// Parse response using Go's HTTP library
	bufReader := bufio.NewReader(bytes.NewReader(rawResponse))
	resp, err := http.ReadResponse(bufReader, outReq)
	if err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	return resp, nil
}

// flushPipe discards any stale data in the USB pipe
func (t *WindowsTransport) flushPipe() {
	readBuf := make([]byte, 4096)
	for i := 0; i < 5; i++ {
		var bytesRead uint32
		ret, _, _ := procWinUsb_ReadPipe.Call(
			t.winusbHandle,
			uintptr(t.inPipe),
			uintptr(unsafe.Pointer(&readBuf[0])),
			uintptr(len(readBuf)),
			uintptr(unsafe.Pointer(&bytesRead)),
			0,
		)
		if ret == 0 || bytesRead == 0 {
			break
		}
	}
}

// readFullResponse reads the complete HTTP response from USB
func (t *WindowsTransport) readFullResponse() ([]byte, error) {
	var fullResponse []byte
	readBuf := make([]byte, 8192)
	emptyReads := 0
	headerEnd := -1
	expectedLen := -1
	isChunked := false

	for i := 0; i < 500; i++ { // Max iterations
		var bytesRead uint32
		ret, _, _ := procWinUsb_ReadPipe.Call(
			t.winusbHandle,
			uintptr(t.inPipe),
			uintptr(unsafe.Pointer(&readBuf[0])),
			uintptr(len(readBuf)),
			uintptr(unsafe.Pointer(&bytesRead)),
			0,
		)

		if ret == 0 || bytesRead == 0 {
			emptyReads++
			if headerEnd > 0 {
				bodyLen := len(fullResponse) - headerEnd - 4
				if expectedLen >= 0 && bodyLen >= expectedLen {
					break
				}
				if emptyReads > 15 {
					break
				}
			} else {
				if emptyReads > 30 {
					if len(fullResponse) == 0 {
						return nil, errors.New("no response from printer")
					}
					break
				}
			}
			time.Sleep(30 * time.Millisecond)
			continue
		}
		emptyReads = 0

		fullResponse = append(fullResponse, readBuf[:bytesRead]...)

		// Parse headers once we have them
		if headerEnd < 0 {
			if idx := bytes.Index(fullResponse, []byte("\r\n\r\n")); idx > 0 {
				headerEnd = idx
				headers := string(fullResponse[:headerEnd])

				for _, line := range strings.Split(headers, "\r\n") {
					lower := strings.ToLower(line)
					if strings.HasPrefix(lower, "content-length:") {
						fmt.Sscanf(strings.TrimSpace(line[15:]), "%d", &expectedLen)
					} else if strings.HasPrefix(lower, "transfer-encoding:") && strings.Contains(lower, "chunked") {
						isChunked = true
					}
				}
			}
		}

		// Check completion
		if headerEnd > 0 {
			bodyLen := len(fullResponse) - headerEnd - 4
			if expectedLen >= 0 && bodyLen >= expectedLen {
				break
			}
			if isChunked && bytes.HasSuffix(fullResponse, []byte("0\r\n\r\n")) {
				break
			}
		}
	}

	return fullResponse, nil
}

// usbWrite writes data to the USB OUT pipe
func (t *WindowsTransport) usbWrite(data []byte) error {
	var bytesWritten uint32
	ret, _, _ := procWinUsb_WritePipe.Call(
		t.winusbHandle,
		uintptr(t.outPipe),
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)),
		uintptr(unsafe.Pointer(&bytesWritten)),
		0,
	)

	if ret == 0 {
		return errors.New("WinUsb_WritePipe failed")
	}
	if int(bytesWritten) != len(data) {
		return fmt.Errorf("partial write: %d of %d bytes", bytesWritten, len(data))
	}
	return nil
}

// removeHopByHopHeaders removes HTTP hop-by-hop headers per RFC 7230
func removeHopByHopHeaders(h http.Header) {
	if c := h.Get("Connection"); c != "" {
		for _, f := range strings.Split(c, ",") {
			if f = strings.TrimSpace(f); f != "" {
				h.Del(f)
			}
		}
	}

	hopByHop := []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Proxy-Connection",
		"Te",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
	}

	for _, header := range hopByHop {
		h.Del(header)
	}
}
