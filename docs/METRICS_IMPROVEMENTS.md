# Metrics Collection Improvements

## Issue
Metrics collection was returning zeros or minimal data for page counts, impressions, and other device counters.

## Root Cause
The metrics collection was only querying a single marker OID (`.1.3.6.1.2.1.43.10.2.1.4.1.1`), which works for mono printers but doesn't capture color printer data that uses multiple marker indices.

## Changes Made

### 1. Enhanced OID Queries for All Vendors

Updated all vendor modules to query multiple marker indices:

- **Marker 1** (`.1.3.6.1.2.1.43.10.2.1.4.1.1`) - Black/Mono pages
- **Marker 2** (`.1.3.6.1.2.1.43.10.2.1.4.1.2`) - Combined color impressions
- **Marker 3** (`.1.3.6.1.2.1.43.10.2.1.4.1.3`) - Cyan impressions
- **Marker 4** (`.1.3.6.1.2.1.43.10.2.1.4.1.4`) - Magenta impressions
- **Marker 5** (`.1.3.6.1.2.1.43.10.2.1.4.1.5`) - Yellow impressions
- **Marker 6** (`.1.3.6.1.2.1.43.10.2.1.4.1.6`) - Additional marker (vendor-specific)

**Files Modified:**
- `agent/scanner/vendor/hp.go`
- `agent/scanner/vendor/generic.go`
- `agent/scanner/vendor/canon.go`
- `agent/scanner/vendor/brother.go`

### 2. Enhanced Debug Logging

Added comprehensive PDU-level debug logging in `agent/scanner_api.go`:

```go
// Debug log all PDU values
for i, pdu := range result.PDUs {
    appLogger.Debug("Metrics PDU received",
        "ip", ip,
        "index", i,
        "oid", pdu.Name,
        "type", pdu.Type,
        "value", pdu.Value)
}
```

This allows us to see:
- Which OIDs are being queried
- What values are actually returned from devices
- Whether devices are responding with NoSuchObject/NoSuchInstance errors
- The data types of returned values

### 3. Printer-MIB Standard Reference

According to RFC 3805 (Printer-MIB v2):

- **prtMarkerTable** (`.1.3.6.1.2.1.43.10`) - Contains marker subsystem info
- **prtMarkerLifeCount** (`.1.3.6.1.2.1.43.10.2.1.4`) - Impressions counter per marker
- **prtMarkerEntry** indexed by `hrDeviceIndex` and `prtMarkerIndex`

**Color printers** typically have:
- Index 1: Black marker (mono pages)
- Index 2: Combined color (total color impressions)
- Index 3-5: Individual CMY markers (some devices)

**Monochrome printers** typically have:
- Index 1: Black marker only

### 4. Existing Parser Support

The `agent/agent/parse.go` already has logic to parse multiple marker indices:

```go
// prtMarkerLifeCount primary marker 1
if strings.HasPrefix(name, "1.3.6.1.2.1.43.10.2.1.4.1.") {
    suf := strings.TrimPrefix(name, "1.3.6.1.2.1.43.10.2.1.4.1.")
    parts := strings.Split(suf, ".")
    if len(parts) >= 1 {
        if idx, err := strconv.Atoi(parts[0]); err == nil {
            if iv, ok := toInt(v.Value); ok {
                markerCounts[idx] = iv
            }
        }
    }
    continue
}
```

This populates the `markerCounts` map which is then normalized into:
- `meters["total_pages"]` - Sum of all markers
- `meters["mono_pages"]` - Marker 1 (black)
- `meters["color_pages"]` - Sum of markers 2-5

## Testing

To test the improvements:

1. **Check debug logs** after running metrics collection:
   - Look for "Metrics PDU received" log entries
   - Verify OIDs are being queried (should see multiple `.43.10.2.1.4.1.X` OIDs)
   - Check returned values (should be integers, not NoSuchObject errors)

2. **Verify device responses**:
   - Some older/simpler printers may only implement marker 1
   - High-end color MFPs should respond to markers 1-5
   - NoSuchObject errors are normal for unsupported markers

3. **Check metrics endpoint** (`/metrics/refresh`):
   - PageCount should be populated if any marker responds
   - ColorPages should be populated for color devices
   - MonoPages should be populated for all devices

## Next Steps

If zeros persist after this change:

1. **Check SNMP connectivity**:
   - Verify SNMP is enabled on the device
   - Verify correct community string (default: "public")
   - Check firewall isn't blocking UDP port 161

2. **Try SNMP Walk**:
   - Use `snmpwalk` tool to verify device responds
   - Check if device implements Printer-MIB v1 vs v2
   - Some devices may use non-standard OID trees

3. **Vendor-specific OIDs**:
   - HP printers may have additional OIDs in `.1.3.6.1.4.1.11.*`
   - Canon printers use `.1.3.6.1.4.1.1602.*`
   - Brother printers use `.1.3.6.1.4.1.2435.*`
   - Consult vendor MIB documentation for specific models

4. **Check device capabilities**:
   - Some printers may require authentication (SNMPv3)
   - Some devices may have SNMP disabled in admin settings
   - Legacy printers may only support SNMPv1 (not v2c)

## References

- RFC 3805: Printer MIB v2
- RFC 1759: Printer MIB v1 (obsoleted)
- `docs/PRINTER_MIBS.md` - Project-specific MIB documentation
- `docs/Printer-MIB.mib` - Full Printer-MIB specification
