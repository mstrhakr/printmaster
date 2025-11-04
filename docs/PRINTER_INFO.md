# Printer Info Schema and Meter History

This document describes the `printer_info` device profile schema used by PrintMaster and the design for storing meter/impression history for time-series analysis.

## Current device layout

Device profiles are stored under `./logs/devices/<serial>.json` and contain these important top-level keys:

- `serial` (string): device serial number (unique key)
- `properties` (object): metadata such as `created_at`, `modified_at`, `last_walk`, `oid_count`, and `missing_fields`
- `printer_info` (object): single authoritative snapshot of the parsed SNMP-derived fields for the device
- `changelog` (array): diff entries recording changes applied during merges
- `reference_walk` (array): embedded MIB walk records (oid/type/value) when a saved walk was used
- `reference_walk_files` (array): filenames of saved walks used to build/refresh the profile
- `history` (array): merge history metadata entries

Example (trimmed):

```
{
  "serial": "SN12345",
  "properties": {
    "created_at": "2025-10-30T12:00:00Z",
    "modified_at": "2025-10-31T12:00:00Z",
    "last_walk": "logs/mib_walk_10_2_106_72_20251030T...json",
    "oid_count": 832,
    "missing_fields": 1
  },
  "printer_info": {
    "ip": "10.2.106.72",
    "manufacturer": "HP",
    "model": "LaserJet Pro",
    "serial": "SN12345",
    "hostname": "printer-1.example.local",
    "meters": { "total_pages": 123456, "mono_pages": 100000, "color_pages": 23456 }
  },
  "changelog": [...],
  "reference_walk": [...]
}
```

## New: `printer_info.meters`

We now store a normalized `meters` map inside `printer_info` to make impressions and counters machine-friendly and vendor-agnostic.

Normalized keys (conservative initial set):

- `total_pages` — lifetime total pages when available (preferred explicit page count; otherwise sum of marker counters)
- `mono_pages` / `black` — monochrome (black) impressions
- `color_pages` — combined color impressions (sum of color markers)
- `cyan`, `magenta`, `yellow` — individual color marker counts when available
- `copier_pages`, `printer_pages`, `fax_pages`, `scan_pages`, `local_pages`, `banner_pages` — mapped heuristics for PrintAudit-like categories when vendor MIBs provide labeled counters

The parser (`ParsePDUs`) builds this map from marker counters, page counters, and by scanning PDUs for keywords (vendor labels) matching PrintAudit categories. The device profile persist step writes `printer_info.meters` into the device JSON.

## Meter history (design)

Goal: provide a simple, robust way to show "impressions today" and time-window deltas (1 day, 7 day, 30 day), and to support charts.

Design decisions:

1. Store timestamped full snapshots in `printer_info.meter_history`.
   - Each entry is `{ "ts": RFC3339, "source": <walk filename or 'refresh'>, "meters": { ... }, "deltas": { ... } }`.
   - Storing full snapshots keeps the original lifetime counters so we can recompute deltas and handle late-arriving data.

2. Compute and store deltas in each snapshot for quick display.
   - On merge, compute `delta_total = meters.total_pages - previous.meters.total_pages` (if previous exists) and record it under `deltas`.

3. Retention/pruning policy (configurable):
   - Keep hourly snapshots for the last 48 hours, then keep one snapshot per day for older entries up to 365 days by default.
   - Provide a dev setting to tune retention windows during testing.

4. API surface:
   - Add `/devices/{serial}/meters` to return `meter_history` and commonly requested aggregates (last 24h delta, last 7-day daily totals).
   - UI will fetch this endpoint to render delta cards and charts.

Reasons for choosing snapshots + deltas

- Snapshots are resilient: if we later change normalization or discover additional counters we can recompute deltas. Deltas-only loses lifetime counters and limits flexibility.
- Storing the delta with the snapshot offers fast reads for the common "impressions today" metric.

## Merge behavior (summary)

- Merges are serialized per-serial (per-device) using the merge queue / per-serial locks so concurrent scans/refreshes do not corrupt device files.
- When a new merge occurs the flow is:
  1. Parse PDUs into `PrinterInfo` (including `Meters`).
  2. Load existing device profile (if any).
  3. Compute changelog diff and append a changelog entry.
  4. Append a `meter_history` snapshot with deltas computed vs the previous snapshot.
  5. Persist device file atomically.

## UI notes

- The device details UI will display `printer_info.meters.total_pages` and the delta since the last day (from `meter_history`) directly on the card.
- A chart placeholder will be added; we will choose a lightweight charting library (Chart.js or similar) once endpoint performance is validated.

## Implementation notes & next steps

- The agent currently builds `printer_info.meters` and persists it. The next steps are:
  - Implement `printer_info.meter_history` append/delta logic in the merge path.
  - Add the `/devices/{serial}/meters` endpoint and a small UI card + chart placeholder.
  - Add unit tests for meter history pruning and delta correctness.
  - Document the retention configuration and add it to `dev_settings.json` if desired.

If you want, I can implement the merge-time meter history append next and wire the API and UI. Which of the next steps should I take now?
