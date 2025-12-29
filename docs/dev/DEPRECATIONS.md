# Deprecations and Removed Features

This document tracks features that have been removed or are pending removal, with rationale and migration notes.

---

## Removed Features

### Candidate/MIB Profile Workflow
- **Status**: ✅ Removed
- **When**: Early 2025
- **What**: Agent no longer loads or parses vendor candidate files or MIB profiles. All associated UI and HTTP endpoints removed.
- **Rationale**: Simplify agent, reduce maintenance cost, avoid tight coupling to vendor-specific data files
- **Migration**: None required. Discovery now relies on Printer-MIB and minimal vendor-agnostic heuristics
- **Cleanup**: Any data under `mib_profiles/` can be deleted safely

### Sandbox Simulation
- **Status**: ✅ Removed  
- **What**: Sandbox feature (simulate candidates against saved walks) removed
- **Rationale**: Depended on candidates/MIB profiles; added complexity without core value
- **Migration**: Use built-in discovery and targeted diagnostic walks

### `/mib_walk` HTTP Endpoint
- **Status**: ✅ Removed
- **What**: On-demand MIB walk HTTP endpoint
- **Rationale**: Encourage bounded, targeted walks inside discovery pipeline; avoid broad, ad-hoc walks
- **Migration**: Use discovery and "Walk All" device action in UI where applicable; targeted walks occur automatically for confirmed printers

### `/saved_ranges` HTTP Endpoint
- **Status**: ✅ Removed (Replaced)
- **When**: October 2025
- **What**: Legacy IP ranges endpoint
- **Replacement**: Use unified `/settings` endpoint (GET/POST for `discovery.ranges_text`)
- **Migration**: 
  ```javascript
  // Old:
  fetch('/saved_ranges');
  
  // New:
  const settings = await fetch('/settings').then(r => r.json());
  const ranges = settings.discovery.ranges_text;
  ```

### Manual MIB Walk UI Functions
- **Status**: ✅ Removed
- **Location**: `agent/web/app.js`
- **Functions**: `runMibWalk()`, `runMibWalkFor(ip)`
- **Rationale**: Full device information now gathered automatically during discovery; deep scan pipeline provides comprehensive data collection
- **Migration**: Automated scanning provides all needed data

---

## Removed Features (Previously Pending)

### Old Logging System
- **Status**: ✅ Removed
- **When**: December 2025
- **Location**: Was in `agent/main.go`
- **Functions**: `logMsg(msg string)`, `logMutex`, `logBuffer []string`
- **Description**: Simple timestamp-prefixed logging to in-memory buffer and file
- **Replacement**: Structured logger package (`common/logger/logger.go`) - now used throughout codebase
- **Migration**: Complete - all logging now uses `appLogger` structured logging

---

## Deprecated (Still Supported)

### `/settings/subnet_scan` HTTP Endpoint
- **Status**: ⚠️ Deprecated (still works)
- **Replacement**: Use `/settings` endpoint (GET/POST for `discovery.subnet_scan`)
- **Migration**: 
  ```javascript
  // Old:
  fetch('/settings/subnet_scan');
  
  // New:
  const settings = await fetch('/settings').then(r => r.json());
  const enabled = settings.discovery.subnet_scan;
  ```
- **Removal Date**: v1.0 (will be removed in 1.0 release)

---

## Migration Checklist

When removing deprecated code:
- [ ] Search codebase for all usages
- [ ] Update documentation (API.md, README.md)
- [ ] Add migration guide for users
- [ ] Test thoroughly before removal
- [ ] Mark as ✅ in this document
- [ ] Update CHANGELOG.md

---

## Notes

- Tests and code paths updated to avoid all candidate/MIB profile logic
- Do not remove anything from "Pending Removal" section without team consensus
- All removals should follow semantic versioning rules:
  - **PATCH**: Remove internal deprecated code (not user-facing)
  - **MINOR**: Deprecate features (add warnings, keep working)
  - **MAJOR**: Remove deprecated features (breaking change)

---

*Last Updated: December 28, 2025*
