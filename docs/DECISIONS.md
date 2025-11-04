# Design decisions and rationale

This document captures the key design decisions made so far and the rationale behind them.

Language and packaging
- Go chosen for a single, cross-platform static binary footprint, minimal runtime dependencies, and good concurrency support.

Discovery approach
- Use an in-memory store for discovered devices (fast, memory-only, no external DB by default).
- Prefer a producer/consumer scanning pipeline: cheap liveness checks first, then expensive SNMP reads for candidates.

Probes and fallbacks
- Fast probes are TCP connect to common printer ports (9100, 80, 443, 515) to detect responsive devices quickly.
- Implement raw ICMP where possible; fall back to system `ping` when raw sockets are unavailable.
- Read ARP/neighbor caches to prioritize likely candidates. Platforms supported so far: Linux `/proc/net/arp`, generic `arp -a` parsing. Windows PowerShell parsing was planned and partially implemented.

SNMP
- Use `gosnmp` (SNMP v2c) for deep queries. Default community string currently set to `public`. OID set includes model, serial, and page counts (sysDescr, prtGeneralSerialNumber, prtMarkerLifeCount).

Safety and UX
- Default maximum expansion for user-provided ranges: 4096 addresses.
- Scans run asynchronously and report progress via `/scan_status` to avoid blocking the UI.
- Persist user ranges and preferences to `config.json` for easy management.

Future & optional
- Add mDNS/SSDP/WSD discovery backends as opt-in features.
- Add UDP SNMP quick-probe to Stage A for better SNMP detection.
 - Add job IDs, cancelation (server-side `/scan_cancel` and cooperative context cancellation), and per-scan reporting.
 - Discovery backends (mDNS/SSDP/WSD) will feed candidates into the Stage A producer so Auto Discover can use both IP-based scanning and service advertisements.
