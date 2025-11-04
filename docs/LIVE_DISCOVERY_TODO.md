# Live Discovery Methods - Implementation Status

## ✅ Implemented

### 1. mDNS/DNS-SD (Bonjour/Zeroconf)
- **Status**: COMPLETE
- **File**: `agent/agent/mdns.go`
- **Platform**: Cross-platform (macOS, Linux, Windows)
- **Services**: `_ipp._tcp`, `_ipps._tcp`, `_printer._tcp`
- **Library**: `github.com/grandcat/zeroconf`
- **Best For**: macOS/Linux environments, modern printers with Bonjour support

### 2. WS-Discovery (Web Services Discovery)
- **Status**: COMPLETE
- **File**: `agent/agent/wsdiscovery.go`
- **Platform**: Windows native, cross-platform listener
- **Protocol**: SOAP over UDP multicast (239.255.255.250:3702)
- **Best For**: Windows environments, HP/Canon/Epson printers
- **Features**:
  - Hello/Bye message listening
  - Probe/ProbeMatch for active discovery
  - Rich device metadata

### 3. SSDP/UPnP (Simple Service Discovery Protocol)
- **Status**: COMPLETE
- **File**: `agent/agent/ssdp.go`
- **Platform**: Cross-platform
- **Protocol**: HTTP over UDP multicast (239.255.255.250:1900)
- **Best For**: Broad device discovery, some modern printers
- **Features**:
  - NOTIFY message listening
  - M-SEARCH for active discovery
  - Periodic re-discovery (every 5 minutes)

### 4. SNMP Traps (Event-Driven Discovery)
- **Status**: COMPLETE ✅
- **File**: `agent/agent/snmptraps.go`
- **Platform**: Cross-platform (requires admin/root privileges)
- **Protocol**: SNMP Trap (UDP port 162)
- **Library**: `github.com/gosnmp/gosnmp`
- **Best For**: Event-driven discovery, real-time printer status monitoring
- **Features**:
  - Listens for SNMP v1/v2c trap notifications
  - Detects common printer traps (status changes, supply alerts, etc.)
  - Auto-enriches trap source IPs with SNMP walks
  - 10-minute throttling to prevent duplicate discoveries
- **Requirements**:
  - Requires administrator/root privileges to bind port 162
  - Printers must be configured to send traps to agent IP
  - Uses "public" community string by default

### 5. LLMNR (Link-Local Multicast Name Resolution)
- **Status**: COMPLETE ✅
- **File**: `agent/agent/llmnr.go`
- **Platform**: Cross-platform (Windows-focused)
- **Protocol**: UDP port 5355, multicast 224.0.0.252 (RFC 4795)
- **Best For**: Windows hostname resolution, printer discovery in pure Windows LANs
- **Features**:
  - Listens for LLMNR queries and responses
  - Extracts hostnames and IPv4 addresses from DNS packets
  - Filters for printer-like hostnames (contains "printer", "hp", "mfp", etc.)
  - Auto-enriches discovered IPs with SNMP walks
  - 10-minute throttling to prevent duplicate discoveries
  - Handles DNS label parsing with compression pointers
- **Implementation**: Custom DNS packet parser
- **UI Control**: Checkbox "Live LLMNR" in Settings

## 📋 Future Implementations (Priority Order)

### 6. NetBIOS/WINS Broadcasts (Legacy Windows)
- **Priority**: LOW
- **Reason**: Declining protocol, noisy, mostly obsolete
- **Protocol**: UDP 137-139 (NetBIOS Name Service)
- **Use Case**: Very old Windows network devices
- **Benefits**:
  - Detect legacy Windows-networked printers
  - Completeness for ancient infrastructure
- **Drawbacks**:
  - Very noisy protocol
  - Security concerns (broadcast-based)
  - Mostly replaced by LLMNR and mDNS
- **Library**: Custom implementation
- **Implementation Notes**:
  - Listen for NetBIOS name broadcasts
  - Parse NBT (NetBIOS over TCP/IP) packets
  - Filter for printer-related names
  - High false-positive rate expected
- **Configuration Needed**:
  - UI checkbox for "Live NetBIOS" (default OFF)
  - Warning about network noise

### 7. IPP-USB (USB Printer Discovery)
- **Priority**: LOW (out of scope for network scanning)
- **Reason**: Local USB printers, not network-based
- **Protocol**: IPP over USB (port 60000)
- **Use Case**: Locally connected USB printers
- **Benefits**:
  - Discover USB printers on the agent host
  - Complete view of all printers (network + USB)
- **Library**: `github.com/google/gousb` or OS-specific USB APIs
- **Implementation Notes**:
  - Enumerate USB devices
  - Filter for printer class (0x07)
  - Query IPP capabilities via USB
  - Not a "live" discovery, more of a periodic USB scan
- **Configuration Needed**:
  - UI checkbox for "USB Printer Discovery"
  - Separate UI section for USB vs. Network devices

### 8. LLDP (Link Layer Discovery Protocol) - Network Infrastructure
- **Priority**: LOW (requires packet capture, not printer-specific)
- **Reason**: More for network topology than printer discovery
- **Protocol**: Ethernet multicast (01:80:c2:00:00:0e)
- **Use Case**: Network topology mapping, switch-attached device discovery
- **Benefits**:
  - Discover network switches and their connected devices
  - Build network topology map
  - Indirectly discover printers by VLAN/switch port
- **Library**: `github.com/gopacket/gopacket` with pcap
- **Implementation Notes**:
  - Requires raw socket or pcap access
  - Parse LLDP frames
  - Extract neighbor information
  - Not directly useful for printers but helpful for infrastructure view
- **Configuration Needed**:
  - UI checkbox for "Network Topology (LLDP)"
  - Network topology visualization (future feature)

---

## Implementation Checklist for Future Methods

For each new live discovery method, follow this pattern:

1. **Create discovery file**: `agent/agent/{protocol}.go`
   - `Start{Protocol}Browser(ctx, logFn, enqueue)` function
   - Protocol-specific message parsing
   - IP extraction and job enqueueing
   
2. **Add to main.go**:
   - Variables: `live{Protocol}Mu`, `live{Protocol}Cancel`, `live{Protocol}Running`, `live{Protocol}Seen`
   - Functions: `startLive{Protocol}()`, `stopLive{Protocol}()`
   - Startup: Check settings and auto-start if enabled
   
3. **Update UI** (Settings → Auto Discover Settings):
   - Add checkbox: `<input type="checkbox" id="discovery_live_{protocol}_enabled" />`
   - Add description with platform/use case
   
4. **Update JavaScript**:
   - Add field to `saveDiscoverySettings()`: `auto_discover_live_{protocol}`
   - Add field to `loadDiscoverySettings()`: load checkbox state
   - Add default in error handler
   
5. **Update settings endpoint** (`/settings/discovery`):
   - Add default in GET handler: `"auto_discover_live_{protocol}": false`
   - Add start/stop handler in POST: check `autoDiscoverEnabled` and toggle worker
   
6. **Testing**:
   - Verify protocol listener starts/stops correctly
   - Test IP extraction and enqueueing
   - Verify SNMP enrichment works
   - Check 10-minute throttling
   - Confirm logs show discoveries

---

## Notes

- All live discovery methods use the same **10-minute throttling** pattern
- All require **Auto Discover enabled** (master switch)
- All run in **separate goroutines** with context cancellation
- All auto-enrich discovered IPs with **SNMP via RefreshDevice**
- All fall back to **minimal device creation** if SNMP fails
- All log discoveries to **scan_events.log**

---

## Performance Considerations

- Multiple multicast listeners can coexist (mDNS, WS-Discovery, SSDP, LLMNR)
- SNMP Traps requires opening privileged port 162 (may need admin/root)
- NetBIOS is very chatty - only enable if absolutely necessary
- USB discovery is local-only, shouldn't impact network performance
- LLDP requires raw socket/pcap access (elevated privileges)

---

## Platform-Specific Recommendations

### Windows Environments
- ✅ WS-Discovery (native, high priority)
- ✅ SSDP/UPnP (good coverage)
- ✅ mDNS (for Bonjour Print Services)
- ⏳ LLMNR (Windows-specific, useful for pure Windows LANs)
- ⏳ NetBIOS (legacy only, low priority)

### macOS/Linux Environments
- ✅ mDNS (native, high priority)
- ✅ SSDP/UPnP (good coverage)
- ✅ WS-Discovery (works cross-platform)
- ⏳ SNMP Traps (universal, platform-agnostic)

### Mixed Environments
- ✅ Enable all three: mDNS + WS-Discovery + SSDP
- ⏳ Add SNMP Traps for event-driven discovery
