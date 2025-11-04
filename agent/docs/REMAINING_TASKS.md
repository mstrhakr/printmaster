Remaining tasks for Printer MIB pairing & candidate workflows

Overview

This document lists remaining work items, priorities, acceptance criteria, and testing notes for the features implemented during the recent changes: pairing of list columns (name/level) from saved MIB walks, UI flow to inspect saved walks, and persisting candidate mappings.

High-level goals

- Let the agent detect and present column-based consumable information (e.g. toner names and levels) from saved MIB walks.
- Allow users to persist discovered column mappings into vendor candidate profiles quickly and reliably.
- Provide robust normalization and tests for edge-cases across vendors.

Priority tasks

1) Per-pair quick-add persistence (high)
- What: Add a direct "Add" button in the printer details modal to POST the chosen column OID to `/vendor/add_oid` with `column_type` set (e.g. `toner_names` or `toner_levels`), optionally with vendor selection.
- Why: Speed up the mapping workflow so users can persist mappings found during walk inspection without opening the full editor.
- Files: `main.go` (client JS), `mib_suggestions_api.go` (server handler already accepts `column_type` but tests/behavior should be added/verified).
- Acceptance: Clicking the button persists the mapping into the appropriate candidate file and shows a confirmation message. Unit tests mock the handler and assert payload and file contents.

2) Store `probe_columns` in candidate JSON (high)
- What: Add a top-level `probe_columns` object to candidate JSON (e.g. `{"probe_columns": {"1.3.6.1.2.1.43.11.1.1.6.1": "toner_names"}}`). Separate column mappings from single-value `probe_properties` for clarity.
- Why: Keeps column hints separate and makes UI editing simpler.
- Files: `mib_suggestions_api.go` (read/write), candidate schema docs.
- Acceptance: After adding a column via `/vendor/add_oid` with `column_type`, the candidate file contains `probe_columns` with normalized OID -> canonical token.

3) UI: Candidate editor to show/edit `probe_columns` (medium)
- What: Modify the candidate editor UI to list `probe_columns`, allow edit/remove, and save back to candidate JSON.
- Files: `main.go` (client JS UI), server `vendor/update` remains compatible.
- Acceptance: Users can edit and save `probe_columns` and changes persist to disk.

4) Unit tests: multi-ink pairing and edge cases (high)
- What: Expand pairing tests to include:
  - Multiple-black instances (e.g. BK1, BK2), ensure they map correctly.
  - Missing level column (name only), expect level=-1.
  - IP-style instance suffixes (e.g. .10.20.30.40), ensure normalization matches by suffix.
  - Non-numeric level values (e.g. "85%", hex-encoded), ensure CoerceToInt handles them or fallback to -1.
- Files: `agent/paired_toner_test.go`, additional tests under `agent/agent`.
- Acceptance: Tests assert correct pairing and are deterministic.

5) Unit test: vendorAddOid normalization (medium)
- What: Ensure `/vendor/add_oid` strips instance suffixes and stores normalized OIDs in candidate JSON. Test for both numeric- and IP-suffixed instance forms.
- Files: `vendor_handlers_test.go`.
- Acceptance: Candidate JSON contains normalized OID keys.

6) Add sample saved-walk fixtures (low)
- What: Add a small, representative saved-walk JSON under `tests/fixtures/` used by UI and server tests.
- Why: Gives developers deterministic data for tests and manual QA.
- Acceptance: Tests referencing these fixtures should pass.

7) Docs: candidate schema and UI flows (done)
- What: Documented in `docs/REMAINING_TASKS.md` and propose update to `docs/MIB_PROFILES.md`.
- Acceptance: Developers can read the doc to understand the schema and UI flow.

8) Polish UX (low)
- Heuristics to auto-select vendor in the Add modal, undo/remove mappings, and small styling/labels.

Testing notes

- Run unit tests frequently:

```powershell
cd c:\temp\printmaster\agent
go test ./...
```

- Manual UI test:
  - Start the agent web UI (or `go run main.go`) and open the MIB Walk tab.
  - Click "View Details" on a saved walk. Confirm the modal shows PrinterInfo and the Detected column pairs section.
  - Click "Add Name Column" / "Add Level Column" on a pair to add it to a candidate; verify the candidate file is updated under `mib_profiles/candidates/`.

Implementation notes and suggestions

- Use an explicit `probe_columns` map in candidate JSON to avoid overloading `probe_properties`.
- Normalize OIDs by stripping the last instance suffix when writing candidates; add a small test harness for different suffix patterns.
- Consider adding a simple client-side confirmation toast when mappings are successfully persisted.

If you want, I can implement task #1 (direct POST quick-add from the details modal) next — it’s a small change and gives immediate UX wins. Otherwise tell me which task to start and I’ll implement it and run tests.