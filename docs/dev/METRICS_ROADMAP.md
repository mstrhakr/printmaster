# Metrics & Insights Roadmap

**Purpose**: Track potential metrics improvements and analytics features for PrintMaster. Remove items as they are implemented.

**Last Updated**: December 2025

---

## Current Data Collection

| Category | What We Collect | Storage Location |
|----------|-----------------|------------------|
| **Page Counts** | Total pages, mono pages, color pages, scan count | `metrics_raw/hourly/daily/monthly` |
| **Supplies** | Toner levels (CMYK), waste toner, drum life, fuser life | `toner_levels` JSON |
| **Device Identity** | Serial, IP, model, hostname, MAC, firmware | `devices` table |
| **Status** | Status messages, error states | `status_messages` |
| **Network** | Gateway, subnet, DNS, DHCP server | `devices` table |
| **Local Printers** | Print jobs, user names, document names, pages per job | `local_print_jobs` |

---

## Potential Improvements

### 1. Cost Analytics 💰

**Business Value**: Very High - Companies need to track and allocate print costs

**Metrics to Add**:
- Cost per page (configurable per device/consumable type)
- Monthly/quarterly print cost totals
- Color vs mono cost breakdown (color is 5-10x more expensive)
- Cost allocation by user (from `local_print_jobs.user_name`)
- Cost allocation by department (requires department mapping)
- Projected monthly cost based on trends

**Implementation Notes**:
```go
type CostMetrics struct {
    CostPerPage        float64
    MonthlyPrintCost   float64
    ColorCostRatio     float64
    CostByDepartment   map[string]float64
    CostByUser         map[string]float64
    ProjectedMonthlyCost float64
}
```

**Required Settings**:
- Default cost per mono page
- Default cost per color page
- Per-device cost overrides
- Consumable costs (for TCO calculation)

---

### 2. Utilization & Efficiency Metrics 📊

**Business Value**: High - Enables fleet right-sizing and capacity planning

**Metrics to Add**:
- Daily average pages per device
- Peak usage hours (identify busy periods)
- Idle time percentage (device sitting unused)
- Duty ratio (actual usage vs manufacturer-rated duty cycle)
- Average job size (pages per print job)
- Color mix ratio (% of jobs using color)
- Prints per workday

**Why It Matters**:
- Identify **underutilized devices** → candidates for removal
- Identify **overutilized devices** → need higher duty cycle model
- **Right-size the fleet** → significant cost savings

**Example Query**:
```sql
-- Find printers printing <100 pages/month (underutilized)
SELECT serial, model, AVG(page_count_avg) as avg_daily
FROM metrics_daily 
WHERE day_start > datetime('now', '-30 days')
GROUP BY serial
HAVING avg_daily < 4  -- ~100/month
```

**Implementation Notes**:
```go
type UtilizationMetrics struct {
    DailyAveragePages     int
    PeakUsageHours        []int
    IdleTimePercent       float64
    DutyRatio             float64
    AverageJobSize        float64
    ColorMixRatio         float64
    PrintsPerWorkday      int
}
```

---

### 3. Supply Lifecycle Predictions 🔮

**Business Value**: Very High - Proactive supply management, never run out

**Metrics to Add**:
- Supply depletion rate (% per day based on usage)
- Estimated empty date per supply
- Days remaining per supply
- Reorder alert (based on configurable lead time)
- Average cartridge life in pages (historical)
- Cost per page derived from actual supply usage

**Implementation Notes**:
```go
type SupplyPrediction struct {
    Supply           string    // "toner_black"
    CurrentLevel     int       // 23%
    DepletionRate    float64   // % per day
    EstimatedEmpty   time.Time
    DaysRemaining    int
    ReorderAlert     bool
    AverageLifePages int
}

type SupplyHistory struct {
    Level     int
    Timestamp time.Time
    PageCount int
}

// Calculate using linear regression on level vs time
// Factor in recent print volume trends
```

**Required Settings**:
- Reorder threshold (days before empty)
- Supplier lead time
- Low supply warning threshold (%)

---

### 4. ~~Paper Tray Status 📄~~ ✅ IMPLEMENTED

**Status**: Implemented in December 2025

**Implementation Details**:
- Added `PaperTray` struct to `agent/agent/types.go` and `common/storage/types.go`
- Added `PaperTrayOIDs()` method to all vendor modules (HP, Epson, Kyocera, Generic)
- Added `ParsePaperTrays()` function in `agent/scanner/vendor/generic.go`
- Paper trays included in `QueryMetrics` and `QueryEssential` profiles
- Data available in `PrinterInfo.PaperTrays` and `MetricsSnapshot.PaperTrays`

**Collected Data**:
```go
type PaperTray struct {
    Index        int    // Tray index (1, 2, 3...)
    Name         string // Tray name ("Tray 1", "Manual Feed")
    MediaType    string // Paper type ("Letter", "A4", "Legal")
    CurrentLevel int    // Current sheets
    MaxCapacity  int    // Tray capacity
    LevelPercent int    // 0-100 percentage
    Status       string // "ok", "low", "empty", "unknown"
}
```

**Files Changed**:
- `agent/agent/types.go` - Added PaperTray struct and PaperTrays field
- `agent/scanner/vendor/registry.go` - Added PaperTrayOIDs() to interface
- `agent/scanner/vendor/generic.go` - Implemented PaperTrayOIDs() and ParsePaperTrays()
- `agent/scanner/vendor/hp.go` - Implemented PaperTrayOIDs()
- `agent/scanner/vendor/epson.go` - Implemented PaperTrayOIDs()
- `agent/scanner/vendor/kyocera.go` - Implemented PaperTrayOIDs()
- `agent/scanner/query.go` - Added paper tray OIDs to query profiles
- `agent/agent/parse.go` - Added paper tray parsing to ParsePDUs()
- `common/storage/types.go` - Added PaperTray to common types and MetricsSnapshot

---

### 5. Error & Maintenance Tracking 🔧

**Business Value**: High - Proactive maintenance, reduce downtime

**Metrics to Add**:
- Errors in last 24h / 7d / 30d
- Paper jam count
- Common error types (top 5 error messages)
- Mean time between errors (MTBE)
- Device health score (0-100)
- Fuser life remaining (pages)
- Drum life remaining (pages)
- Maintenance kit due date

**Data Sources**:
- `hrPrinterDetectedErrorState` OID (already defined)
- Status messages (already collecting)
- Supply levels for consumable parts

**Implementation Notes**:
```go
type DeviceHealth struct {
    ErrorsLast24h         int
    ErrorsLast7d          int
    JamCount              int
    CommonErrorTypes      []string
    MeanTimeBetweenErrors time.Duration
    MaintenanceScore      int
    FuserLifeRemaining    int
    DrumLifeRemaining     int
    MaintenanceKitDue     time.Time
}
```

---

### 6. Print Job Analytics 📋

**Business Value**: Medium - User behavior insights, compliance

**Current State**: Already collecting `local_print_jobs` table

**Metrics to Add**:
- Top users by print volume
- Average pages per job
- Color jobs percentage
- After-hours printing percentage (outside 9-5)
- Document type breakdown (infer from document_name: PDF, Word, Email, Web)
- Single-page job count (often test prints → waste)
- Large jobs aborted (started but cancelled)
- Unauthorized printing detection

**Implementation Notes**:
```go
type JobAnalytics struct {
    TopUsers            []UserPrintStats
    AveragePagesPerJob  float64
    ColorJobsPercent    float64
    AfterHoursPrinting  float64
    DocumentCategories  map[string]int
    SinglePageJobs      int
    LargeJobsAborted    int
    UnauthorizedPrinting bool
}

type UserPrintStats struct {
    Username    string
    TotalPages  int
    ColorPages  int
    JobCount    int
    CostEstimate float64
}
```

---

### 7. Environmental/Sustainability Metrics 🌱

**Business Value**: Medium - ESG reporting, corporate sustainability

**Metrics to Add**:
- Pages printed this month/quarter/year
- Trees equivalent (1 tree ≈ 8,333 pages)
- CO2 footprint estimate (500 pages ≈ 2.4kg CO2)
- Duplex savings (pages saved by duplex printing)
- Duplex adoption rate (% of jobs using duplex)
- Paper waste estimate (failed/reprinted jobs)

**Implementation Notes**:
```go
type EnvironmentalMetrics struct {
    PagesThisMonth     int
    TreesEquivalent    float64
    CO2FootprintKg     float64
    DuplexSavingsPages int
    DuplexAdoptionRate float64
    PaperWasteEstimate int
}

// Constants for calculation
const (
    PagesPerTree    = 8333
    CO2PerPage      = 0.0048 // kg CO2 per page
)
```

---

### 8. Fleet-Wide Dashboard Metrics 📈

**Business Value**: High - Executive/management visibility

**Metrics to Add**:

**Fleet Summary**:
- Total devices
- Active devices (seen in last 24h)
- Offline devices
- Devices by type (Color MFP, Mono Printer, etc.)

**Consumption**:
- Total pages this month
- Color pages this month
- Estimated print cost this month

**Alerts Summary**:
- Low toner devices count
- Low paper devices count
- Error state devices count
- Devices needing maintenance

**Efficiency**:
- Fleet average utilization
- Underutilized device count (candidates for removal)
- Overutilized device count (need upgrade)

**Trends**:
- Print volume change vs last month (%)
- Cost change vs last month (%)
- Year-over-year comparison

**Implementation Notes**:
```go
type FleetInsights struct {
    TotalDevices          int
    ActiveDevices         int
    OfflineDevices        int
    
    TotalPagesThisMonth   int64
    ColorPagesThisMonth   int64
    PrintCostThisMonth    float64
    
    LowTonerDevices       int
    LowPaperDevices       int
    ErrorStateDevices     int
    
    FleetUtilization      float64
    UnderutilizedCount    int
    OverutilizedCount     int
    
    PrintVolumeChange     float64
    CostChangePercent     float64
}
```

---

## Implementation Priority Matrix

| Feature | Effort | Business Value | Priority |
|---------|--------|----------------|----------|
| ~~Paper tray levels~~ | ~~Low~~ | ~~High~~ | ~~**1**~~ ✅ **IMPLEMENTED** |
| Supply depletion prediction | Medium | Very High | **2** |
| Cost analytics | Medium | Very High | **3** |
| Utilization metrics | Low | High | **4** |
| Error/health tracking | Medium | High | **5** |
| Fleet dashboard metrics | Medium | High | **6** |
| Job analytics | Low | Medium | **7** |
| Environmental metrics | Low | Medium | **8** |

---

## Implementation Checklist

### Phase 1: Quick Wins
- [x] Paper tray level collection (add to SNMP queries) ✅ December 2025
- [ ] Basic utilization calculation (pages/day from existing data)
- [ ] Fleet summary dashboard (aggregate existing metrics)

### Phase 2: Predictive Analytics
- [ ] Supply history tracking table
- [ ] Depletion rate calculation
- [ ] Reorder alert system
- [ ] Maintenance prediction

### Phase 3: Cost Management
- [ ] Cost settings UI
- [ ] Cost calculation engine
- [ ] Cost reports by device/user/department
- [ ] Budget tracking

### Phase 4: Advanced Analytics
- [ ] User behavior analytics
- [ ] Document type classification
- [ ] Environmental reporting
- [ ] Trend analysis and forecasting

---

## Database Schema Additions (Future)

```sql
-- Supply history for depletion tracking
CREATE TABLE supply_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    serial TEXT NOT NULL,
    supply_key TEXT NOT NULL,  -- "toner_black", "drum_life", etc.
    level INTEGER NOT NULL,
    page_count INTEGER,        -- Device page count at this reading
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (serial) REFERENCES devices(serial) ON DELETE CASCADE
);

CREATE INDEX idx_supply_history_serial ON supply_history(serial);
CREATE INDEX idx_supply_history_timestamp ON supply_history(timestamp);

-- Paper tray status
CREATE TABLE paper_trays (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    serial TEXT NOT NULL,
    tray_index INTEGER NOT NULL,
    tray_name TEXT,
    media_type TEXT,
    current_level INTEGER,
    max_capacity INTEGER,
    last_updated DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (serial) REFERENCES devices(serial) ON DELETE CASCADE,
    UNIQUE(serial, tray_index)
);

-- Error/event log
CREATE TABLE device_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    serial TEXT NOT NULL,
    event_type TEXT NOT NULL,  -- "error", "warning", "jam", "maintenance"
    message TEXT,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    resolved_at DATETIME,
    FOREIGN KEY (serial) REFERENCES devices(serial) ON DELETE CASCADE
);

CREATE INDEX idx_device_events_serial ON device_events(serial);
CREATE INDEX idx_device_events_type ON device_events(event_type);
CREATE INDEX idx_device_events_timestamp ON device_events(timestamp);

-- Cost settings
CREATE TABLE cost_settings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    serial TEXT,              -- NULL for default settings
    mono_cost_per_page REAL DEFAULT 0.02,
    color_cost_per_page REAL DEFAULT 0.10,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(serial)
);
```

---

## API Endpoints (Future)

```
GET /api/v1/analytics/fleet          - Fleet-wide summary
GET /api/v1/analytics/utilization    - Utilization metrics
GET /api/v1/analytics/costs          - Cost breakdown
GET /api/v1/analytics/supplies       - Supply predictions
GET /api/v1/analytics/environmental  - Sustainability metrics

GET /api/v1/devices/{serial}/analytics     - Device-specific analytics
GET /api/v1/devices/{serial}/predictions   - Supply/maintenance predictions
GET /api/v1/devices/{serial}/health        - Health score and issues

GET /api/v1/reports/utilization      - Utilization report
GET /api/v1/reports/costs            - Cost report
GET /api/v1/reports/sustainability   - Environmental report
```

---

## Notes

- All metrics should support multi-tenant filtering
- Consider caching for expensive calculations
- Historical data retention policies needed for analytics tables
- UI components needed for each metric category
