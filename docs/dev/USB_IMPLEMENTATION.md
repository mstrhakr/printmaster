# USB Printer Support Implementation Plan

**Version**: 1.0.0 Target (gousbsnmp development v0.8.0+)  
**Priority**: CRITICAL for 1.0  
**Status**: Strategy Revised - Pure Go Approach

---

## Executive Summary

**The Problem**: Many MSP clients have USB printers with NO network visibility. These printers are invisible to network-based monitoring tools, creating a blind spot in MPS (Managed Print Services).

**The Solution**: Cross-platform USB printer monitoring with full metrics extraction via pure Go implementation using IEEE 1284.4 (SNMP-over-USB).

**The Innovation**: PrintMaster will be the **ONLY open-source print monitoring tool** with comprehensive USB printer support including page counts, toner levels, and device info - in **100% pure Go** with zero native dependencies.

## Strategy Revision (November 2025)

**Previous Plan**: C++ helper for Windows, Go helper for Linux/Mac  
**New Plan**: Pure Go implementation via gousbsnmp library

**Why the Change:**
- **Avoid Double Work**: Building C++ helper then replacing with Go is wasted effort
- **True Cross-Platform**: Single binary works everywhere (Windows/Linux/macOS/Raspberry Pi)
- **Super Lightweight**: No C++ dependencies, no DLLs, no shared libraries
- **Manageable Complexity**: 2-3 months to build gousbsnmp, integrate in v1.0
- **Competitive Advantage**: Pure Go USB support is unique in the industry

**Timeline:**
- **v0.8.0-v0.9.0**: Build gousbsnmp library in parallel (side project)
- **v1.0.0**: Integrate mature gousbsnmp into PrintMaster agent

---

## Architecture Overview

### Pure Go Single-Component Design

```
┌─────────────────────────────────────────────────────────────┐
│ PrintMaster Agent (Go)                                       │
│ ┌─────────────────────────────────────────────────────────┐ │
│ │ Network Discovery (existing)                             │ │
│ │  • mDNS, SSDP, SNMP, ARP, etc.                          │ │
│ │  • Queries: 192.168.1.0/24 via SNMP                     │ │
│ └─────────────────────────────────────────────────────────┘ │
│                                                               │
│ ┌─────────────────────────────────────────────────────────┐ │
│ │ USB Discovery (NEW - Pure Go via gousbsnmp)             │ │
│ │  • Enumerate USB printers via gousb                     │ │
│ │  • For each USB printer:                                │ │
│ │    1. Open USB device (gousb)                          │ │
│ │    2. Find IEEE 1284.4 interface                       │ │
│ │    3. Send SNMP query via IEEE 1284.4 framing         │ │
│ │    4. Parse SNMP response (gosnmp)                    │ │
│ │    5. Merge into device database                       │ │
│ └─────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
                             │
                             ▼ uses
         ┌────────────────────────────────────────┐
         │ gousbsnmp Library (Pure Go)            │
         ├────────────────────────────────────────┤
         │ github.com/google/gousb                │
         │ + github.com/gosnmp/gosnmp             │
         │ + IEEE 1284.4 protocol implementation  │
         └────────────────────────────────────────┘
                             │
                             ▼
                     ┌──────────────┐
                     │ USB Printer   │
                     │ (via USB port)│
                     └──────────────┘
```

---

## Why Pure Go?

### **Previous Plan: C++ Helper** ❌
- **Problem**: Double work - build C++ helper, then replace with Go later
- **Result**: Wasted engineering time, platform-specific build complexity
- **Decision**: Skip this approach entirely

### **New Plan: gousbsnmp Library** ✅ **GAME CHANGER**
- **Benefits**:
  - **Single Binary**: No C++ dependencies, no DLLs, works everywhere
  - **True Cross-Platform**: Windows, Linux, macOS, Raspberry Pi from one codebase
  - **Super Lightweight**: Minimal footprint, no external libraries
  - **Long-term Maintainability**: Pure Go is easier to maintain than C++ + Go
  - **Competitive Advantage**: No other open-source tool has pure Go USB printer support
- **Complexity**: 6/10 difficulty, 2-3 months for production-ready implementation
- **Timeline**: Build in parallel during v0.8.0-v0.9.0, integrate in v1.0

---

## gousbsnmp Library Development Plan

### Phase 1: USB Layer (2 weeks, Difficulty: 2/10)

**Library**: github.com/google/gousb  
**Goal**: Enumerate USB printers and open devices

```go
package gousbsnmp

import "github.com/google/gousb"

// EnumeratePrinters finds all USB printer devices
func EnumeratePrinters(ctx *gousb.Context) ([]*USBPrinter, error) {
    var printers []*USBPrinter
    
    devices, _ := ctx.OpenDevices(func(desc *gousb.DeviceDesc) bool {
        // USB printer class: 0x07
        return desc.Class == gousb.ClassPrinter
    })
    
    for _, dev := range devices {
        printers = append(printers, &USBPrinter{Device: dev})
    }
    return printers, nil
}

// USBPrinter represents a USB printer device
type USBPrinter struct {
    Device    *gousb.Device
    Interface *gousb.Interface
    InEp      *gousb.InEndpoint
    OutEp     *gousb.OutEndpoint
}

// Open prepares the printer for communication
func (p *USBPrinter) Open() error {
    // Claim interface 0 (usually printer data)
    intf, _ := p.Device.OpenInterface(0)
    p.Interface = intf
    
    // Find IN/OUT endpoints for bidirectional communication
    p.InEp, _ = intf.InEndpoint(1)
    p.OutEp, _ = intf.OutEndpoint(2)
    return nil
}
```

### Phase 2: IEEE 1284.4 Protocol (4 weeks, Difficulty: 5/10)

**Goal**: Implement IEEE 1284.4 packet framing for USB printers

IEEE 1284.4 Packet Format:
```
┌────────────────────────────────────────────────┐
│ Packet Header (5 bytes)                        │
├────────────────────────────────────────────────┤
│ 0x00           Command (0x00 = SNMP)          │
│ 0x01           Socket ID (0x01 = SNMP socket) │
│ [length:2]     Data length (big-endian)       │
│ 0x00           Flags                          │
├────────────────────────────────────────────────┤
│ SNMP PDU (variable length)                     │
│ ... BER-encoded SNMP GetRequest ...           │
└────────────────────────────────────────────────┘
```

```go
package gousbsnmp

import "encoding/binary"

// IEEE1284Packet wraps SNMP data in IEEE 1284.4 format
type IEEE1284Packet struct {
    Command  byte   // 0x00 = SNMP
    SocketID byte   // 0x01 = SNMP socket
    Length   uint16 // Data length
    Flags    byte   // Usually 0x00
    Data     []byte // SNMP PDU
}

// Encode creates a IEEE 1284.4 packet
func (p *IEEE1284Packet) Encode() []byte {
    buf := make([]byte, 5+len(p.Data))
    buf[0] = p.Command
    buf[1] = p.SocketID
    binary.BigEndian.PutUint16(buf[2:4], p.Length)
    buf[4] = p.Flags
    copy(buf[5:], p.Data)
    return buf
}

// Decode parses a IEEE 1284.4 packet from USB response
func (p *IEEE1284Packet) Decode(data []byte) error {
    if len(data) < 5 {
        return fmt.Errorf("packet too short")
    }
    p.Command = data[0]
    p.SocketID = data[1]
    p.Length = binary.BigEndian.Uint16(data[2:4])
    p.Flags = data[4]
    p.Data = data[5:]
    return nil
}
```

### Phase 3: SNMP Integration (2 weeks, Difficulty: 3/10)

**Library**: github.com/gosnmp/gosnmp  
**Goal**: Use gosnmp for SNMP marshaling, send via IEEE 1284.4

```go
package gousbsnmp

import "github.com/gosnmp/gosnmp"

// SNMPClient wraps a USB printer for SNMP queries
type SNMPClient struct {
    Printer *USBPrinter
    Timeout time.Duration
}

// Get performs an SNMP GET request via USB
func (c *SNMPClient) Get(oids []string) (*gosnmp.SnmpPacket, error) {
    // Build SNMP GetRequest using gosnmp
    snmpReq := &gosnmp.SnmpPacket{
        Version:   gosnmp.Version2c,
        Community: "public",
        PDUType:   gosnmp.GetRequest,
        RequestID: rand.Uint32(),
        Variables: makeVarBinds(oids),
    }
    
    // Marshal SNMP packet to BER encoding
    snmpData, err := snmpReq.MarshalMsg()
    if err != nil {
        return nil, err
    }
    
    // Wrap in IEEE 1284.4 packet
    packet := &IEEE1284Packet{
        Command:  0x00,
        SocketID: 0x01,
        Length:   uint16(len(snmpData)),
        Flags:    0x00,
        Data:     snmpData,
    }
    
    // Send via USB
    _, err = c.Printer.OutEp.Write(packet.Encode())
    if err != nil {
        return nil, err
    }
    
    // Read response
    respBuf := make([]byte, 8192)
    n, err := c.Printer.InEp.Read(respBuf)
    if err != nil {
        return nil, err
    }
    
    // Decode IEEE 1284.4 response
    respPacket := &IEEE1284Packet{}
    respPacket.Decode(respBuf[:n])
    
    // Parse SNMP response
    snmpResp := &gosnmp.SnmpPacket{}
    _, err = snmpResp.UnmarshalMsg(respPacket.Data)
    return snmpResp, err
}
```

### Phase 4: Testing & Hardening (4 weeks, Difficulty: 8/10)

**Goal**: Test with real printers, handle edge cases

**Test Matrix**:
- HP (most common, good SNMP support)
- Canon (varies by model)
- Epson (sometimes only PJL)
- Brother (usually good)
- Kyocera (enterprise-grade SNMP)

**Edge Cases**:
- Printer doesn't support IEEE 1284.4 (fallback to PJL)
- USB timeout/disconnect during query
- Malformed SNMP responses
- Security: permissions on different platforms
  - Windows: Admin rights needed for USB access
  - Linux: udev rules or root required
  - macOS: Entitlements for USB access

**Timeline**: v0.8.0-v0.9.0 (parallel development, doesn't block other work)

---

## Legacy Implementation (DEPRECATED - For Reference Only)

The sections below document the original C++ + Go helper approach. This is **NO LONGER THE PLAN** but kept for historical reference in case we need to fall back.

### Component 1: Windows USB Query Helper (C++) [DEPRECATED]

**Location**: `usb-query/windows/`

**Dependencies**:
- CMake (build system)
- Net-SNMP library OR custom PJL implementation
- nlohmann/json (JSON output)

**Functionality**:
```cpp
// printmaster-usb-query.exe USB001 --json

int main(int argc, char* argv[]) {
    string portName = argv[1];  // "USB001"
    
    // Open USB device
    HANDLE hPrinter = CreateFile("\\\\.\\USB001", ...);
    
    // Option A: SNMP over USB (IEEE 1284.4)
    SNMPUSBTransport transport(hPrinter);
    json result = {
        {"success", true},
        {"page_count", transport.GetInt("1.3.6.1.2.1.43.10.2.1.4.1.1")},
        {"black_toner", transport.GetInt("1.3.6.1.2.1.43.11.1.1.6.1.1")},
        {"serial", transport.GetString("1.3.6.1.2.1.43.5.1.1.17.1")},
        // ... more OIDs
    };
    
    // Option B: PJL (Printer Job Language) - fallback
    // Send: @PJL INFO PAGECOUNT, @PJL INFO SUPPLIES
    // Parse text response
    
    cout << result.dump() << endl;
    return 0;
}
```

**OIDs to Query** (same as network printers):
- `1.3.6.1.2.1.43.10.2.1.4.1.1` - Total page count
- `1.3.6.1.2.1.43.10.2.1.4.1.2` - Mono impressions
- `1.3.6.1.2.1.43.10.2.1.4.1.3` - Color impressions
- `1.3.6.1.2.1.43.11.1.1.6.1.*` - Toner/ink levels (multiple)
- `1.3.6.1.2.1.43.5.1.1.17.1` - Serial number
- `1.3.6.1.2.1.25.3.2.1.3.1` - Model name
- `1.3.6.1.2.1.25.3.2.1.4.1` - Firmware version

**PJL Commands** (if SNMP fails):
```
\x1B%-12345X@PJL
@PJL INFO ID
@PJL INFO PAGECOUNT
@PJL INFO SUPPLIES
@PJL DINQUIRE SERIALNUMBER
@PJL DINQUIRE TOTALCOUNTER
\x1B%-12345X
```

**Build**:
```powershell
cd usb-query/windows
mkdir build && cd build
cmake .. -G "Visual Studio 17 2022" -A x64
cmake --build . --config Release
# Output: printmaster-usb-query.exe
```

---

### Component 2: Linux/Mac USB Query Helper (Go)

**Location**: `usb-query/main.go` (with build tags)

**Dependencies**: None (uses CUPS commands)

**Functionality**:
```go
//go:build linux || darwin

package main

import (
    "encoding/json"
    "os/exec"
    "strings"
)

type USBMetrics struct {
    Success      bool              `json:"success"`
    Model        string            `json:"model"`
    Manufacturer string            `json:"manufacturer"`
    Serial       string            `json:"serial"`
    PageCount    int               `json:"page_count"`
    TonerLevels  map[string]int    `json:"toner_levels"`
    Status       string            `json:"status"`
}

func QueryUSBPrinter(deviceName string) (*USBMetrics, error) {
    metrics := &USBMetrics{Success: true, TonerLevels: make(map[string]int)}
    
    // Get printer info via lpstat
    cmd := exec.Command("lpstat", "-l", "-p", deviceName)
    output, _ := cmd.CombinedOutput()
    parseStatus(string(output), metrics)
    
    // Query IPP attributes via ipptool
    ippRequest := `{
        OPERATION Get-Printer-Attributes
        ATTR uri printer-uri ipp://localhost/printers/` + deviceName + `
        EXPECT printer-impressions-completed OF-TYPE integer
        EXPECT marker-levels OF-TYPE integer
        EXPECT marker-names OF-TYPE name
    }`
    
    cmd = exec.Command("ipptool", "-t", "ipp://localhost/printers/"+deviceName, "-")
    cmd.Stdin = strings.NewReader(ippRequest)
    output, _ = cmd.CombinedOutput()
    
    parseIPPAttributes(string(output), metrics)
    
    return metrics, nil
}

func main() {
    deviceName := os.Args[1]
    metrics, err := QueryUSBPrinter(deviceName)
    
    if err != nil {
        json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
            "success": false,
            "error": err.Error(),
        })
        return
    }
    
    json.NewEncoder(os.Stdout).Encode(metrics)
}
```

**Build**:
```bash
cd usb-query
GOOS=linux go build -o printmaster-usb-query-linux
GOOS=darwin go build -o printmaster-usb-query-darwin
```

---

### Component 3: Agent Integration (Go) [UPDATED FOR v1.0]

**Location**: `agent/agent/usb_discover.go` (v1.0 implementation)

```go
package agent

import (
    "context"
    "github.com/mstrhakr/gousbsnmp"  // Our pure Go library
)

// DiscoverUSBPrinters enumerates and queries USB printers via gousbsnmp
func DiscoverUSBPrinters(ctx context.Context) ([]PrinterInfo, error) {
    // Initialize USB context
    usbCtx := gousb.NewContext()
    defer usbCtx.Close()
    
    // Find all USB printers
    printers, err := gousbsnmp.EnumeratePrinters(usbCtx)
    if err != nil {
        return nil, err
    }
    
    var results []PrinterInfo
    
    for _, printer := range printers {
        // Open USB device
        if err := printer.Open(); err != nil {
            log.Printf("Failed to open USB printer: %v", err)
            continue
        }
        defer printer.Device.Close()
        
        // Create SNMP client
        client := &gousbsnmp.SNMPClient{
            Printer: printer,
            Timeout: 5 * time.Second,
        }
        
        // Query same OIDs as network printers
    
    if err != nil {
        return nil, err
    }
    
    // Query each USB printer
    printers := []PrinterInfo{}
    for _, port := range ports {
        info, err := queryUSBPrinter(ctx, port)
        if err != nil {
            log.Printf("Failed to query USB printer %s: %v", port.Name, err)
            continue
        }
        printers = append(printers, info)
    }
    
    return printers, nil
}

func queryUSBPrinter(ctx context.Context, port USBPrinterPort) (PrinterInfo, error) {
    // Determine helper binary path
    helperBinary := getHelperPath()
    
    // Spawn helper
    cmd := exec.CommandContext(ctx, helperBinary, port.PortName, "--json")
    output, err := cmd.CombinedOutput()
    if err != nil {
        return PrinterInfo{}, err
    }
    
    // Parse JSON response
    var metrics USBMetrics
    if err := json.Unmarshal(output, &metrics); err != nil {
        return PrinterInfo{}, err
    }
    
    // Convert to PrinterInfo (same struct as network printers!)
    return PrinterInfo{
        IP:           "usb://" + port.PortName,  // Special indicator
        Manufacturer: metrics.Manufacturer,
        Model:        metrics.Model,
        Serial:       metrics.Serial,
        PageCount:    metrics.PageCount,
        TonerLevels:  metrics.TonerLevels,
        LastSeen:     time.Now(),
        DiscoveryMethods: []string{"usb"},
    }, nil
}

func getHelperPath() string {
    execDir := filepath.Dir(os.Args[0])
    
    switch runtime.GOOS {
    case "windows":
        return filepath.Join(execDir, "printmaster-usb-query.exe")
    case "linux":
        return filepath.Join(execDir, "printmaster-usb-query-linux")
    case "darwin":
        return filepath.Join(execDir, "printmaster-usb-query-darwin")
    default:
        return ""
    }
}
```

**Windows USB Enumeration**:
```go
//go:build windows

import "github.com/StackExchange/wmi"

type Win32_Printer struct {
    Name     string
    PortName string
    Local    bool
}

func enumerateWindowsUSB(ctx context.Context) ([]USBPrinterPort, error) {
    var printers []Win32_Printer
    query := "SELECT Name, PortName FROM Win32_Printer WHERE Local = TRUE"
    err := wmi.QueryWithContext(ctx, query, &printers)
    
    usbPorts := []USBPrinterPort{}
    for _, p := range printers {
        if strings.HasPrefix(p.PortName, "USB") {
            usbPorts = append(usbPorts, USBPrinterPort{
                Name:     p.Name,
                PortName: p.PortName,
            })
        }
    }
    return usbPorts, err
}
```

**Linux/Mac USB Enumeration**:
```go
//go:build linux || darwin

func enumerateCUPSUSB(ctx context.Context) ([]USBPrinterPort, error) {
    cmd := exec.CommandContext(ctx, "lpstat", "-v")
    output, err := cmd.CombinedOutput()
    if err != nil {
        return nil, err
    }
    
    // Parse output: "device for HP_LaserJet: usb://HP/LaserJet"
    usbPorts := []USBPrinterPort{}
    scanner := bufio.NewScanner(bytes.NewReader(output))
    for scanner.Scan() {
        line := scanner.Text()
        if strings.Contains(line, "usb://") {
            // Extract printer name
            parts := strings.Split(line, ":")
            if len(parts) >= 2 {
                name := strings.TrimSpace(strings.TrimPrefix(parts[0], "device for"))
                usbPorts = append(usbPorts, USBPrinterPort{
                    Name:     name,
                    PortName: name,  // CUPS uses printer name
                })
            }
        }
    }
    return usbPorts, nil
}
```

---

## Data Schema

### USB Printers in Database

USB printers use the **same `devices` table** as network printers:

```sql
INSERT INTO devices (
    serial,          -- Primary key (from USB query)
    ip,              -- "usb://USB001" or "usb://HP_LaserJet"
    manufacturer,
    model,
    page_count,
    toner_level_black,
    -- ... all other fields
    discovery_methods  -- JSON: ["usb"]
)
```

**Special handling**:
- `ip` field: Use `usb://` prefix to indicate USB connection
- `discovery_methods`: Include `"usb"` in array
- All other fields: Identical to network printers

**Benefit**: UI doesn't need to know if printer is USB or network!

---

## Configuration

### agent/config.ini.example

```ini
[usb]
# Enable USB printer discovery
enabled = true

# Timeout for USB queries (seconds)
query_timeout = 10

# Retry failed queries
retry_on_failure = true

# Path to helper binary (auto-detected if empty)
helper_path = 

# Log USB query details for debugging
debug_logging = false
```

---

## Build Integration

### build.ps1 (Updated)

```powershell
function Build-USBQuery {
    param($Platform = "windows")
    
    Write-Host "Building USB Query Helper for $Platform..." -ForegroundColor Cyan
    
    if ($Platform -eq "windows") {
        # Build C++ helper
        Push-Location usb-query/windows
        if (-not (Test-Path "build")) { mkdir build }
        cd build
        cmake .. -G "Visual Studio 17 2022" -A x64
        cmake --build . --config Release
        Copy-Item Release\printmaster-usb-query.exe ..\..\..\agent\
        Pop-Location
    } else {
        # Build Go helper
        Push-Location usb-query
        $env:GOOS = $Platform
        go build -o "printmaster-usb-query-$Platform"
        Copy-Item "printmaster-usb-query-$Platform" ..\agent\
        Pop-Location
    }
}

# Main build logic
if ($Component -eq "agent" -or $Component -eq "both") {
    Build-USBQuery "windows"  # For Windows builds
    Build-Agent
}
```

---

## Testing Strategy

### Phase 1: Proof of Concept (Weekend)
- [ ] Build minimal C++ helper that queries ONE OID from ONE USB printer
- [ ] Verify JSON output format
- [ ] Test with 2-3 different printer brands

### Phase 2: Integration (Week 1)
- [ ] Integrate helper into Agent
- [ ] Test USB enumeration on Windows
- [ ] Test end-to-end: USB printer appears in web UI

### Phase 3: Cross-Platform (Week 2)
- [ ] Implement Linux CUPS helper
- [ ] Test on Ubuntu/Debian
- [ ] Implement macOS CUPS helper
- [ ] Test on macOS

### Phase 4: Reliability (Week 3)
- [ ] Test with 10+ different printer models
- [ ] Handle edge cases (printer offline, permission denied, etc.)
- [ ] Add retry logic
- [ ] Add timeout handling

### Phase 5: Polish (Week 4)
- [ ] Performance optimization
- [ ] Error messages
- [ ] Documentation
- [ ] Release v0.3.0

---

## Known Limitations

### What Works
✅ USB printers connected to the agent's host machine  
✅ Standard SNMP OIDs (HP, Canon, Epson, Brother, etc.)  
✅ PJL-compatible printers (fallback)  
✅ Page counts, toner levels, device info

### What Doesn't Work
❌ USB printers connected to other machines (requires agent on each machine)  
❌ Consumer inkjets without PJL/SNMP support  
❌ Printers with vendor-locked protocols  
❌ Real-time monitoring (queries only when discovery runs)

---

## Competitive Advantage

**This makes PrintMaster UNIQUE:**

| Feature | PrintMaster | Papercut | SnipeIT | Other Tools |
|---------|-------------|----------|---------|-------------|
| Network discovery | ✅ | ✅ | ❌ | ✅ |
| USB monitoring | ✅ | ❌ | ❌ | ❌ |
| Open source | ✅ | ❌ | ✅ | Varies |
| Cross-platform USB | ✅ | N/A | N/A | N/A |
| Full metrics via USB | ✅ | N/A | N/A | N/A |

**Market impact**: This positions PrintMaster as the **only open-source solution** for comprehensive print monitoring including USB printers.

---

## Timeline

**v0.2.0** (This week):
- Ship agent-server communication
- Foundation for multi-site

**v0.3.0** (3-4 weeks):
- USB support (Windows + Linux + Mac)
- Testing with real printers
- Documentation

**v0.4.0+** (Following months):
- Refinement based on user feedback
- Additional vendor support
- Performance optimization

---

## Success Criteria for v0.3.0

Before releasing v0.3.0, we must demonstrate:

✅ Windows C++ helper successfully queries 5+ different USB printers  
✅ Linux Go helper successfully queries 3+ different USB printers  
✅ Agent integrates both helpers seamlessly  
✅ USB printers appear in web UI alongside network printers  
✅ All metrics displayed correctly (page counts, toner, etc.)  
✅ Error handling works (printer offline, permission denied)  
✅ Documentation complete (configuration, troubleshooting)  
✅ Performance acceptable (query completes in <5 seconds)

---

## References

- [Net-SNMP Library](http://www.net-snmp.org/)
- [CUPS API Documentation](https://www.cups.org/doc/api-overview.html)
- [IEEE 1284.4 Standard](https://standards.ieee.org/standard/1284_4-2000.html)
- [PJL Technical Reference](http://h20000.www2.hp.com/bc/docs/support/SupportManual/bpl13208/bpl13208.pdf)
- [Printer MIB (RFC 3805)](https://datatracker.ietf.org/doc/html/rfc3805)
