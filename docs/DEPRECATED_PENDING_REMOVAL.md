# Deprecated Code Pending Removal

This document tracks code that has been superseded or is no longer used, but hasn't been removed yet to avoid breaking changes during migration.

## Logging System

### Old System (Pending Removal)
- **Location**: `agent/main.go`
- **Function**: `logMsg(msg string)`
- **Description**: Simple timestamp-prefixed logging to in-memory buffer and file
- **Status**: Being phased out in favor of structured logger
- **Migration Plan**:
  1. ✅ Phase 1: Logger package created and tested
  2. 🔄 Phase 2: Gradually replace logMsg() calls with appLogger calls
  3. ⏳ Phase 3: Once all high-traffic paths use structured logging, remove logMsg()
- **Blockers**: 
  - Many call sites still use logMsg() (~100+ locations)
  - Some functions pass logMsg as callback (need to refactor)
- **Target Removal**: After Phase 2 completion

### Old Log Buffer (Pending Removal)
- **Location**: `agent/main.go`
- **Variables**: `logMutex`, `logBuffer []string`
- **Description**: In-memory circular buffer for logs displayed in UI
- **Status**: Will be replaced by logger.GetBuffer()
- **Migration Plan**:
  1. Update UI endpoints to use appLogger.GetBuffer()
  2. Remove old logBuffer management code
- **Target Removal**: After UI integration (Phase 3)

## Discovery Methods

### Potential Abandoned Flows
*To be confirmed during integration audit*

None identified yet - will track here as we discover them during logging integration.

## HTTP Handlers

### Potential Unused Endpoints
*To be confirmed during usage audit*

None identified yet - will track here if we find unused API endpoints.

## UI Functions

### Manual MIB Walk Functions (Deprecated)
- **Location**: `agent/web/app.js`
- **Functions**: 
  - `runMibWalk()` - Manual MIB walk from UI
  - `runMibWalkFor(ip)` - MIB walk for specific IP
- **Description**: Legacy manual SNMP MIB walking interface
- **Status**: Superseded by automated device discovery and deep scan pipeline
- **Reason for Deprecation**: 
  - Full device information now gathered automatically during discovery
  - Deep scan pipeline provides more comprehensive data collection
  - Manual walks are maintenance burden and rarely used
- **Migration Plan**:
  1. Verify no critical functionality depends on these functions
  2. Remove UI elements that call these functions (MIB walk panel)
  3. Remove the functions and related code
- **Target Removal**: After verification that automated scanning provides all needed data

### Legacy Range Configuration (Consider Deprecation)
- **Location**: `agent/web/app.js`
- **Functions**: 
  - `saveRanges()` - Save IP ranges to settings
  - `clearRanges()` - Clear configured ranges
  - `loadSavedRanges()` - Load saved ranges
- **Description**: Simple text-based IP range configuration
- **Status**: Still in use but may be superseded by advanced discovery settings
- **Note**: These functions work and are actively used - evaluate if they should be enhanced or kept as-is

## Notes
- Do not remove anything from this list without team consensus
- Test thoroughly before removing any deprecated code
- Update this document as new deprecated code is identified
- Mark items as ✅ when migration is complete
