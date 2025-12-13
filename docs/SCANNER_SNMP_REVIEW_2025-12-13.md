# Scanner / Parsing / SNMP Review (2025-12-13)

Scope: `agent/scanner/*`, vendor parsing (`agent/scanner/vendor/*`), core parse/merge (`agent/agent/parse.go`), and scan orchestration (`agent/scanner_api.go`).

This doc captures *observed issues* (bad practices, inefficiencies, structural risks) and *recommended cleanup/fixes*. It is intentionally blunt and implementation-focused.

---

## 1) Pipeline structure & concurrency

### 1.1 Liveness probe is serial per-host

- **Evidence**: `agent/scanner/pipeline.go` → `tcpProbe(ip, ports, timeout)` dials ports sequentially.
- **Why this is inefficient**: Each host blocks a worker for up to `len(ports) * timeout`. With defaults (3 ports, 500ms), a closed-host worst-case is ~1.5s of worker time per IP.
- **Suggested fix**:
  - Probe ports concurrently per host using a small bounded semaphore (e.g., 2-3 goroutines per host), or
  - Use a single dial with `context` + per-port deadlines, or
  - Consider ICMP/ARP-based liveness prior to TCP (depending on permissions/platform).

### 1.2 Jitter uses `math/rand` global without seeding

- **Evidence**: `agent/scanner/pipeline.go` uses `rand.Intn` for startup jitter.
- **Impact**: jitter sequence repeats across restarts (not correctness-critical but undermines “randomization”).
- **Suggested fix**: seed once (or use `rand.New` with per-process seed).

### 1.3 Deep scan results are `interface{}` with errors as values

- **Evidence**: `agent/scanner/pipeline.go` → `StartDeepScanPool()` emits `chan interface{}` and sends `error` values as results.
- **Impact**: downstream must type-switch; easy to mishandle errors as successful results.
- **Suggested fix**: change to typed result struct, e.g.
  - `type DeepScanResult struct { IP string; Result *scanner.QueryResult; Err error }`.

### 1.4 Detection ignores liveness errors (observability)

- **Evidence**: `StartDetectionPool` passes `lr.OpenPorts` even when `lr.Err != nil`.
- **Impact**: makes it hard to distinguish “no open ports” vs “probe failure”.
- **Suggested fix**: propagate `lr.Err` into `DetectionResult` and log/track separately.

---

## 2) SNMP query approach & batching

### 2.1 `QueryFull` walks `1.3.6.1.4.1` (all enterprises)

- **Evidence**: `agent/scanner/query.go` → `QueryFull` roots include `1.3.6.1.4.1`.
- **Risks**:
  - Runtime unpredictability and timeouts.
  - Memory pressure (PDU slice growth).
  - The 10k cap can truncate before reaching useful vendor subtrees.
- **Suggested fix**:
  - Use preflight GET (`sysObjectID`) and walk *only* `1.3.6.1.4.1.<enterprise>` for the detected vendor.
  - Consider moving the “broad enterprise walk” behind an explicit diagnostics-only flag.

### 2.2 `defaultOIDBatchSize = 3` is likely too small

- **Evidence**: `agent/scanner/snmp_batch.go`.
- **Impact**: too many round-trips for scalar OIDs; RTT dominates.
- **Suggested fix**: increase default (e.g., 10–25) and make it configurable; add vendor-specific caps if needed.

### 2.3 Walks are only done for `SupplyOIDs()` in `QueryMetrics`

- **Evidence**: `agent/scanner/query.go` splits `scalarOIDs` vs `tableRoots` only when `profile == QueryMetrics` and uses vendor `SupplyOIDs()` for walks.
- **Impact**: other table-shaped metric families (e.g., Epson function counters) cannot request walks, leading to brittle GET-on-leaf behavior.
- **Suggested fix**:
  - Add a vendor interface for general table roots (e.g., `WalkOIDs()` or `TableOIDs(profile, caps)`), not only supplies.

### 2.4 SNMP v3 is “accepted” but not configured

- **Evidence**: `agent/scanner/snmp.go` accepts `SNMP_VERSION=3` but doesn’t configure v3 user/auth/priv.
- **Impact**: confusing runtime failures.
- **Suggested fix**:
  - If v3 not supported: reject with explicit error.
  - If supported: add config surface for v3 security params and wire into gosnmp.

---

## 3) Vendor selection & hint handling

### 3.1 Vendor registry has the right primitives; query hint path is fragile

- **Evidence**:
  - `agent/scanner/vendor/registry.go` provides `GetVendorByName`.
  - `agent/scanner/query.go` contains a “crude fallback” path via `enterpriseFromHint(...)` (observed earlier).
- **Impact**: mis-detection → wrong OID set → wasted queries/missed metrics.
- **Suggested fix**: if `vendorHint != ""`, do direct registry lookup first.

### 3.2 Enterprise mapping is small and will miss many brands

- **Evidence**: `EnterpriseOIDMap` in `agent/scanner/vendor/registry.go` covers a handful.
- **Impact**: detection falls back to heuristics often.
- **Suggested fix**: expand mapping gradually as vendors are encountered; keep heuristics as fallback.

---

## 4) Vendor parsing inefficiencies

### 4.1 Many vendor parsers are O(n*m) because `getOIDInt` scans PDUs repeatedly

- **Evidence**: `agent/scanner/vendor/generic.go` implements `getOIDInt(pdus, oid)` by scanning full PDU slice each call.
  - HP (`hp.go`), Kyocera (`kyocera.go`), Epson (`epson.go`) call `getOIDInt` many times.
- **Impact**: for large walks (hundreds/thousands PDUs), this becomes CPU-hot.
- **Suggested fix**: build an indexed lookup (normalized OID → PDU/value) once per parse.
  - Example: `type PDUIndex struct { byOID map[string]gosnmp.SnmpPDU }` and helper getters.

### 4.2 Generic supplies table parsing is heavy and does repeated string ops

- **Evidence**: `agent/scanner/vendor/generic.go` → `parseSuppliesTable`.
- **Risks**:
  - Many allocations (string conversions, map entries).
  - Description normalization can be expensive.
- **Suggested fix**:
  - Index PDUs by OID prefix, avoid repeated `strings.HasPrefix` across entire PDU set.
  - Consider parsing supplies only when requested (metrics profile) to reduce work in essential/minimal flows.

### 4.3 Epson function counters are treated as scalar GETs

- **Evidence**: `agent/scanner/vendor/epson.go` builds function counter OIDs with `.1` `.2` `.3` `.4`.
- **Impact**: indices can vary; GET-on-leaf can miss data.
- **Suggested fix**: support walking table roots as described in §2.3.

### 4.4 HP firmware extraction is potentially very expensive

- **Evidence**: `agent/scanner/vendor/hp.go` → `extractFirmwareVersion(pdus)` does nested scanning.
- **Impact**: worst-case quadratic-ish scanning when strings are long.
- **Suggested fix**:
  - Restrict to known OIDs (sysDescr / specific HP OIDs),
  - Stop early after first plausible match,
  - Avoid scanning across all PDUs.

---

## 5) Epson remote-mode (ST2) structural concerns

### 5.1 Remote-mode uses hardcoded community `public`

- **Evidence**: `agent/scanner/vendor/epson_remote_client.go` → `FetchEpsonRemoteMetricsWithIP` calls `NewVendorSNMPClient(ip, "public", timeoutSeconds)`.
- **Impact**: breaks on non-public communities; inconsistent behavior vs main SNMP client.
- **Suggested fix**: use shared SNMP config (community/version) from scanner/agent config.

### 5.2 Separate SNMP client implementation duplicates policy

- **Evidence**: `agent/scanner/vendor/snmp_helper.go` hardcodes SNMPv2c and `Retries: 1`.
- **Impact**: divergence, inconsistent reliability.
- **Suggested fix**: pass in a client factory or config object; avoid duplicating SNMP connection rules.

### 5.3 Remote-mode likely not active in main discovery flows

- **Evidence**:
  - `agent/agent/parse.go` supports `MergeVendorMetricsWithContext(ctx, ..., ip, timeoutSeconds)`.
  - `agent/scanner_api.go` uses `MergeVendorMetrics(&pi, ...)` (no IP/context).
- **Impact**: remote-mode may never run in discovery/metrics flows even when enabled.
- **Suggested fix**: call `MergeVendorMetricsWithContext` from discovery/metrics, passing IP + timeout.

---

## 6) Agent parsing (`ParsePDUs`) observations

### 6.1 `ParsePDUs` records *all* raw PDUs for debug

- **Evidence**: `agent/agent/parse.go` captures `RawPDUs` including hex decode.
- **Impact**: potential memory and storage cost, especially if called on full-walk results frequently.
- **Suggested fix**:
  - Make debug capture conditional (feature flag / sampling / max PDU limit), or
  - Persist only summaries + selected OIDs.

### 6.2 Vendor metrics merge currently skips Generic module entirely

- **Evidence**: `MergeVendorMetricsWithContext` returns early if vendor is `Generic`.
- **Impact**: Generic supply parsing may not run unless handled elsewhere; verify desired behavior.
- **Suggested fix**: confirm where generic metrics are supposed to be captured; adjust behavior or rename semantics (e.g., “Generic is a real parser, not a no-op”).

---

## 7) Logging hotspots / operational issues

### 7.1 `CollectMetricsWithOIDs` logs every PDU

- **Evidence**: `agent/scanner_api.go` → loop `for i, pdu := range result.PDUs { appLogger.Debug("Metrics PDU received", ...) }`.
- **Impact**: severe log spam and CPU/I/O overhead for large results.
- **Suggested fix**: log summary + sample, or guard behind a strict trace flag.

---

## 8) Duplication / drift risk

### 8.1 Multiple SNMP stacks still exist

- **Evidence**: `agent/scanner/*` (new unified query) vs legacy functions in `agent/agent/snmp.go`.
- **Impact**: policy drift and confusing behavior.
- **Suggested cleanup**: identify active call sites; migrate all runtime use to unified scanner, then delete or isolate legacy code.

---

## 9) Persistence / storage write path (agent SQLite)

This section covers how discovery results are persisted via `agent.UpsertDiscoveredPrinter` 7 `DeviceStorage.StoreDiscoveredDevice` 7 `storage.SQLiteStore`.

### 9.1 Discovery currently does 3 separate DB writes per device

- **Evidence**: `agent/main.go` 7 `deviceStorageAdapter.StoreDiscoveredDevice`:
  - `store.Upsert(device)`
  - `store.AddScanHistory(snapshot)`
  - `store.SaveMetricsSnapshot(metrics)`
- **Impact**: for each discovered device you do at least 3 statements (often more), with no transaction tying them together.
- **Risks**:
  - Partial persistence on failure (device row updated but no metrics, etc.).
  - Unnecessary I/O overhead under large scans.
- **Suggested fix**:
  - Add a store method that persists the trio in a single transaction (e.g., `StoreDiscovery(ctx, device, scanSnapshot, metricsSnapshot)`), or
  - Have `StoreDiscoveredDevice` open a transaction and call internal `*_Tx` helpers.

### 9.2 `Upsert` does a read-before-write (`Get` then `Update`)

- **Evidence**: `agent/storage/sqlite.go` 7 `Upsert` calls `Get(serial)` then `Update`.
- **Impact**: doubles the number of DB round-trips for the common case (existing device).
- **Why it exists**: to preserve `CreatedAt`, `FirstSeen`, `IsSaved`, and `LockedFields`.
- **Suggested fix**:
  - Convert to a single-statement UPSERT that preserves these fields (SQLite `INSERT ... ON CONFLICT(serial) DO UPDATE SET ...`), or
  - At least collapse into a transaction and avoid unmarshalling large JSON blobs when only a few fields are required.

### 9.3 `Update` overwrites most columns every time (write amplification)

- **Evidence**: `agent/storage/sqlite.go` 7 `Update` marshals multiple JSON blobs (`Consumables`, `StatusMessages`, `DNSServers`, `RawData`, `LockedFields`) and updates many columns on every call.
- **Impact**:
  - JSON marshal alloc + CPU even when values did not change.
  - Larger SQLite write-set -> more fsync/locking overhead.
- **Suggested fix**:
  - If you keep this shape: introduce a minimal "touch" update for heartbeat (`last_seen`, maybe `ip`) separate from full-device updates.
  - Consider computing a "device fingerprint" for heavy JSON columns and skipping updates when unchanged.

### 9.4 `AddScanHistory` always writes a full snapshot

- **Evidence**: `deviceStorageAdapter.StoreDiscoveredDevice` unconditionally calls `AddScanHistory`.
- **Impact**: scan history grows quickly; also includes `raw_data` which can be large.
- **Suggested fix**:
  - Only append scan history when something meaningfully changed (diff against last snapshot), or
  - Make history frequency configurable (e.g., max 1 per device per N minutes), or
  - Store a reduced snapshot for routine scans and keep full snapshots for diagnostics.

### 9.5 `UpsertDiscoveredPrinter` uses `context.Background()`

- **Evidence**: `agent/agent/helpers.go` 7 `UpsertDiscoveredPrinter` creates `ctx := context.Background()`.
- **Impact**: DB writes cannot be cancelled when a scan is cancelled/shutting down.
- **Suggested fix**: accept/pass a `context.Context` from the caller (scan pipeline / request handler) so storage participates in cancellation.

### 9.6 SSE event classification likely mis-detects "new" devices

- **Evidence**: `agent/main.go` 7 `StoreDiscoveredDevice` sets:
  - `device := storage.PrinterInfoToDevice(pi, false)`
  - then `store.Upsert(ctx, device)`
  - then computes `isNew := device.FirstSeen.Equal(device.LastSeen) || time.Since(device.FirstSeen) < time.Second`.
- **Risk**: `Upsert` preserves `FirstSeen` from the existing row, but `device` in memory does not get updated from DB. Depending on how `PrinterInfoToDevice` initializes `FirstSeen`, this can incorrectly treat existing devices as "new" (or vice versa).
- **Suggested fix**: determine "new" from the DB operation itself (e.g., UPSERT returning whether it inserted), or query the old `first_seen`/existence explicitly once and base event on that.

---

## 10) Metrics implementation (tiered storage + API + UI)

### 10.1 Metrics are stored as raw snapshots + periodic tiered rollups

- **Evidence**:
  - Raw insert: `agent/storage/sqlite.go` → `(*SQLiteStore).SaveMetricsSnapshot`
  - Rollups + cleanup: `agent/storage/downsample.go` → `PerformFullDownsampling`, `DownsampleRawToHourly`, `DownsampleHourlyToDaily`, `DownsampleDailyToMonthly`, `CleanupOldTieredMetrics`
  - Scheduler: `agent/main.go` → `runMetricsDownsampler` (every 6h, with 30s startup delay)
- **Strengths**: the Netdata-style tiering approach is correct in principle and keeps long-range queries fast.

### 10.2 `SaveMetricsSnapshot` does a DB read on every write

- **Evidence**: `SaveMetricsSnapshot` calls `GetLatestMetrics` to drop counter decreases.
- **Impact**: each snapshot save is at least 2 DB ops (read + insert). Under frequent polling or manual collection bursts, this can become a bottleneck.
- **Suggested fix**:
  - If you keep the guard: store "latest" in-memory per device for the current process (with periodic refresh), or
  - Keep a `devices.latest_metrics_*` cached row and update it transactionally alongside insert.

### 10.3 Tiered history queries scan whole tables then filter in Go

- **Evidence**: `agent/storage/downsample.go` → `GetTieredMetricsHistory` queries each tier with `WHERE serial = ? ORDER BY ...` and then filters by `(since, until)` in Go.
- **Impact**: returns/iterates potentially large result sets unnecessarily (especially hourly/daily/monthly tiers), defeating indexes like `idx_metrics_*_serial_timestamp`.
- **Suggested fix**:
  - Add SQL range predicates per tier:
    - raw: `WHERE serial=? AND timestamp BETWEEN ? AND ?`
    - hourly: `WHERE serial=? AND hour_start BETWEEN ? AND ?`
    - daily: `WHERE serial=? AND day_start BETWEEN ? AND ?`
    - monthly: `WHERE serial=? AND month_start BETWEEN ? AND ?`
  - This also reduces memory allocations and JSON unmarshal work.

### 10.4 Rollup logic is not transactional and can be non-idempotent by overwrite

- **Evidence**: rollups use `INSERT OR REPLACE` into aggregate tables.
- **Impact**:
  - `OR REPLACE` deletes and reinserts, which can be heavier than `ON CONFLICT DO UPDATE`.
  - Rollup + cleanup are not wrapped in a transaction; a crash mid-run can leave partial rollups.
- **Suggested fix**:
  - Wrap `PerformFullDownsampling` in a single transaction where practical.
  - Prefer `INSERT ... ON CONFLICT(serial,bucket) DO UPDATE SET ...`.

### 10.5 Toner averaging uses `GROUP_CONCAT(JSON)` and custom parsing

- **Evidence**: `agent/storage/downsample.go` → `averageTonerLevels` parses `GROUP_CONCAT(toner_levels)` via custom brace/quote tracking.
- **Risks**:
  - Large strings for busy devices -> memory spikes.
  - Parsing is brittle and hard to maintain.
- **Suggested fix**:
  - Store toner levels in a normalized table (serial, timestamp, color, level) for easier aggregation, or
  - Keep JSON but aggregate with a streaming scan of rows (`SELECT toner_levels FROM ...`) and average in Go without concatenating into a single giant string.

### 10.6 Metrics API endpoints use `context.Background()`

- **Evidence**: `agent/main.go` handlers for `/api/devices/metrics/latest`, `/api/devices/metrics/history`, `/api/devices/metrics/delete` and the manual collector use `ctx := context.Background()`.
- **Impact**: request cancellation/timeouts won’t stop DB queries; slow queries can pile up.
- **Suggested fix**: use `r.Context()` and apply explicit timeouts for long queries.

### 10.7 Metrics UI is shared and well-factored, but can be heavy for long ranges

- **Evidence**:
  - Shared metrics UI: `common/web/metrics.js` (loaded by `agent/web/index.html` via `static/metrics.js`).
  - Metrics modal wrapper: `common/web/shared.js` (`window.showMetricsModal`).
- **Behavior**:
  - The UI fetches `/api/devices/metrics/history` and renders a chart + a per-row table with delete buttons.
- **Risks**:
  - Long ranges (year/all) can return many points (especially if tier selection bugs/overlaps) and lead to heavy DOM/canvas work.
- **Suggested fix**:
  - Add a server-side max points (or downsample further for UI) and return a pre-decimated series.
  - Consider paging the "Metrics Rows" table for large ranges.

---

## 11) Device details implementation (API + UI)

### 11.1 `/devices/get` synthesizes toner levels from multiple sources

- **Evidence**: `agent/main.go` `/devices/get`:
  - reads latest metrics snapshot (`GetLatestMetrics`) for `page_count` and `toner_levels`
  - falls back to `device.RawData[toner_level_*]` when metrics toner is absent
- **Impact**: correctness depends on historical storage format; can drift between schema versions.
- **Suggested fix**:
  - Define a single canonical representation for consumables/toner in the API response (prefer metrics snapshot for “current”, device table for “identity”).

### 11.2 Device list endpoint returns compatibility wrappers

- **Evidence**: `agent/main.go` `/devices/list` returns `{serial, path, printer_info, info, asset_number, location, web_ui_url}`.
- **Impact**: compatibility layers tend to accumulate, and consumers start depending on multiple aliases.
- **Suggested fix**:
  - Keep the wrapper, but document it and gradually migrate UI to a single schema.

### 11.3 RawData is used as a catch-all for device details

- **Evidence**: `agent/storage/convert.go` stores many fields into `device.RawData` (capabilities, meters, learned OIDs, tray status, detection reasons, etc.).
- **Impact**:
  - Lots of `interface{}` (float64 vs int) conversions on read.
  - Harder to query/filter server-side.
- **Suggested fix**:
  - Promote frequently used fields into typed columns or typed JSON sub-objects, and reserve RawData for rarely used diagnostics.

### 11.4 UI details modal uses shared renderers and adds live tools

- **Evidence**:
  - Device details renderer: `common/web/cards.js` `showPrinterDetailsData` (plus locks/preview/apply updates).
  - Agent UI opens details via `window.__pm_shared.showPrinterDetails(serial, 'saved')` (see `agent/web/app.js`).
- **Strengths**: shared device renderer avoids drift between Agent and Server web UIs.
- **Potential issue**: the agent UI still contains a legacy IP-based `showPrinterDetails(ip, source)` implementation; the shared wrapper now resolves by serial only. This increases confusion and makes it easier to re-introduce IP-based mismatches.
- **Suggested fix**: remove/retire the legacy IP-based details function from `agent/web/app.js` (or gate it clearly), and standardize on serial as the canonical key end-to-end.

---

## 12) Prioritized remediation list (P0/P1/P2)

### P0 (Correctness / scale breakers)

1) **Fix tiered metrics history query to be range-bounded in SQL**
  - **Problem**: `GetTieredMetricsHistory` fetches all rows per tier per serial, then filters in Go.
  - **Why P0**: long-lived agents will accumulate large tier tables; this turns UI/API reads into unbounded scans.
  - **Change**: add `BETWEEN` bounds on the bucket timestamp columns and ensure indexes exist.
  - **Files**: `agent/storage/downsample.go` (`GetTieredMetricsHistory`).
  - **Tests**: add storage tests validating queries return only in-range rows and tier selection returns monotonic timestamps.

2) **Stop walking `1.3.6.1.4.1` by default in full scans**
  - **Problem**: broad enterprise walks are unpredictable and can be huge.
  - **Why P0**: network storms, timeouts, and memory blowups on some devices.
  - **Change**: constrain enterprise walks to vendor-specific roots only; prefer targeted GETs for known OIDs.
  - **Files**: `agent/scanner/query.go` (`QueryFull`), vendor OID sets.
  - **Tests**: scanner tests that ensure full scans never include the enterprise root unless explicitly enabled.

3) **Use request contexts in handlers and long DB operations**
  - **Problem**: endpoints use `context.Background()` so cancellations/timeouts are ignored.
  - **Why P0**: under load, slow DB queries can accumulate and degrade responsiveness.
  - **Change**: use `r.Context()` and add timeouts where needed.
  - **Files**: `agent/main.go` metrics endpoints; consider similar patterns elsewhere.
  - **Tests**: handler-level tests with canceled contexts (where feasible) or store methods that respect context.

### P1 (High leverage perf / maintainability)

4) **Increase SNMP GET batch size and reduce repeated PDU scans**
  - **Problem**: `defaultOIDBatchSize=3` is extremely small; many parsers do repeated O(n*m) loops.
  - **Impact**: excessive round-trips and CPU churn.
  - **Change**: raise batch size (empirically tune), build OID→PDU maps once per response.
  - **Files**: `agent/scanner/snmp_batch.go`, vendor parsers in `agent/scanner/vendor/*`.
  - **Tests**: microbenchmarks for parsing; unit tests for vendor OID mapping.

5) **Make discovery persistence transactional (device upsert + scan history + metrics)**
  - **Problem**: discovery path performs multiple writes without a transaction.
  - **Impact**: partial writes on crash; amplification.
  - **Change**: wrap per-device persistence in a transaction, or design a single “store discovery result” method.
  - **Files**: `agent/main.go` storage adapter + `agent/storage/sqlite.go`.
  - **Tests**: transaction rollback tests; invariants like “scan_history row implies device exists”.

6) **Replace `GROUP_CONCAT(JSON)` toner averaging with streaming aggregation**
  - **Problem**: concatenating many JSON strings into one huge string is memory-heavy and brittle.
  - **Change**: scan rows and average in Go, or normalize toner levels.
  - **Files**: `agent/storage/downsample.go` (`averageTonerLevels`).
  - **Tests**: parsing robustness tests (quotes/braces), large-volume tests to prevent regressions.

7) **Retire legacy IP-based device details code paths in UI**
  - **Problem**: shared UI is serial-based; legacy IP-based modal logic still exists and causes drift.
  - **Change**: standardize on serial everywhere; keep IP only as display field.
  - **Files**: `agent/web/app.js`, `common/web/shared.js`, `common/web/cards.js`.

### P2 (Quality improvements / future-proofing)

8) **Clarify and document API schemas (device vs metrics “current state”)**
  - **Problem**: `/devices/get` synthesizes toner levels from multiple sources; compatibility wrappers persist.
  - **Change**: document a canonical schema and migrate consumers to it.
  - **Files**: `docs/API.md` and relevant handlers.

9) **Downsampler idempotency and transactional safety**
  - **Problem**: `INSERT OR REPLACE` can be heavier than `ON CONFLICT DO UPDATE`; rollup runs can be partially applied.
  - **Change**: transaction-wrap rollups and use upserts.
  - **Files**: `agent/storage/downsample.go`.

10) **UI guardrails for large metrics ranges**
  - **Problem**: metrics modal can render a large table; repeated fetches for bounds + range.
  - **Change**: endpoint for bounds only, UI paging, max points/decimation.
  - **Files**: `common/web/metrics.js`, metrics API handler.

---

## 13) Suggested remediation phases (easy wins first, highest impact early)

### Phase 0 — Safe, fast wins (same-day changes)

Goal: reduce worst-case load/latency without changing product behavior.

1) **Range-bound SQL for tiered metrics history (P0-1)**
  - Small, localized change with immediate UI/API speedup.
  - Also reduces memory usage and JSON payload sizes.

2) **Use request context in metrics handlers (P0-3)**
  - Low-risk mechanical change: swap `context.Background()` → `r.Context()`.
  - Helps avoid request pileups under slow disk/DB.

3) **Bump SNMP GET batch size (P1-4, partial)**
  - One constant change, plus basic guardrails (e.g., cap max OIDs per request).
  - Immediate reduction in SNMP round-trips.

### Phase 1 — Pressing scale/correctness (P0 completion)

Goal: avoid runaway scans and unbounded work as deployments grow.

4) **Remove default enterprise-root walk in full scan (P0-2)**
  - Replace with vendor-root enterprise walks only (or behind a feature flag).
  - Dramatically reduces the risk of “one device melts the scan”.

5) **Discovery persistence transactionality (P1-5)**
  - Convert the current multi-write path into a single transaction per device.
  - Makes "device + scan_history + metrics" atomic and reduces partial state.

### Phase 2 — Structural improvements (measured refactors)

Goal: keep behavior but reduce long-term maintenance and CPU churn.

6) **Replace repeated O(n*m) PDU scans with maps (P1-4)**
  - Build `map[string]SnmpPDU` once per response and reuse in parsers.

7) **Fix toner rollup aggregation approach (P1-6)**
  - Eliminate `GROUP_CONCAT(JSON)` and move to streaming row aggregation.

8) **Retire IP-based device detail UI paths (P1-7)**
  - Standardize the UI on serial as the only canonical identifier.

### Phase 3 — UX/observability polish (nice-to-have, but valuable)

Goal: prevent UI degradation on huge fleets and make issues diagnosable.

9) **Metrics UI guardrails and API support (P2-10)**
  - Add a “bounds-only” endpoint (min/max timestamp) so UI doesn’t fetch `period=year` just to learn bounds.
  - Add max points or server-side decimation, and paginate the table.

10) **Document canonical schemas (P2-8) + downsampler idempotency (P2-9)**
  - Helps prevent new compatibility layers and makes future refactors safer.

---

## Proposed cleanup plan (high-level)

1. **Correctness first**:
   - Fix Epson remote-mode to honor configured community/version.
   - Ensure remote-mode can run via `MergeVendorMetricsWithContext` in main flows.
  - Make discovery persistence atomic (transaction) to avoid partial state.
2. **Performance next**:
   - Increase and make configurable SNMP GET batch size.
   - Index PDUs once per parse; remove repeated full-slice scans.
   - Reduce logging volume in metrics collection.
  - Reduce DB write amplification (avoid read-before-write and full-row updates when unnecessary).
3. **Structural cleanup**:
   - Replace `interface{}` deep scan output with typed results.
   - Restrict `QueryFull` to vendor enterprise subtree.
   - Consolidate/remove legacy SNMP paths.

---

## Notes / Open questions

- Confirm intended behavior for Generic vendor parsing: should it populate supplies/total pages in normal flows?
- Confirm whether SNMP config is intended to be env-only or also controlled via agent config/UI.
- Confirm which discovery/metrics codepaths are currently active in production (feature flags).
