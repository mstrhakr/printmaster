# Deprecations and Removed Features

This document tracks features that have been removed or deprecated, with brief rationale and migration notes.

## Removed: Candidate/MIB profile workflow
- Status: Removed
- Summary: The agent no longer loads or parses vendor candidate files or MIB profiles. All associated UI and HTTP endpoints have been removed.
- Rationale: Simplify the agent, reduce maintenance cost, and avoid tight coupling to vendor-specific data files.
- Migration: None required. Discovery relies on Printer-MIB and minimal vendor-agnostic heuristics.

## Removed: Sandbox simulation
- Status: Removed
- Summary: The Sandbox feature (simulate candidates against saved walks) has been removed.
- Rationale: Depended on candidates/MIB profiles; added complexity without core value.
- Migration: None. Use built-in discovery and targeted diagnostic walks.

## Removed: `/mib_walk` endpoint
- Status: Removed
- Summary: The on-demand MIB walk HTTP endpoint has been removed.
- Rationale: Encourage bounded, targeted walks inside the discovery pipeline; avoid broad, ad-hoc walks.
- Migration: Use discovery and “Walk All” device action in the UI where applicable; targeted walks occur automatically for confirmed printers.

## Notes
- Any data under `mib_profiles/` is no longer used by the agent and can be deleted safely.
- Tests and code paths have been updated to avoid all candidate/MIB profile logic.
