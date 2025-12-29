// Package storage implements server metrics persistence.
package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Server metrics retention policy
const (
	ServerMetricsRawRetention    = 2 * time.Hour        // 2 hours of 10-second raw data
	ServerMetricsMinuteRetention = 7 * 24 * time.Hour   // 7 days of minute data
	ServerMetricsHourlyRetention = 90 * 24 * time.Hour  // 90 days of hourly data
	ServerMetricsDailyRetention  = 365 * 24 * time.Hour // 1 year of daily data
)

// InsertServerMetrics stores a new metrics snapshot.
func (s *BaseStore) InsertServerMetrics(ctx context.Context, snapshot *ServerMetricsSnapshot) error {
	if snapshot.Timestamp.IsZero() {
		snapshot.Timestamp = time.Now().UTC()
	}
	if snapshot.Tier == "" {
		snapshot.Tier = "raw"
	}

	fleetJSON, err := json.Marshal(snapshot.Fleet)
	if err != nil {
		return fmt.Errorf("marshal fleet: %w", err)
	}
	serverJSON, err := json.Marshal(snapshot.Server)
	if err != nil {
		return fmt.Errorf("marshal server: %w", err)
	}

	query := `
		INSERT INTO server_metrics_history (timestamp, tier, fleet_json, server_json)
		VALUES (?, ?, ?, ?)
	`
	_, err = s.execContext(ctx, query,
		snapshot.Timestamp.UTC().Format(time.RFC3339Nano),
		snapshot.Tier,
		string(fleetJSON),
		string(serverJSON),
	)
	return err
}

// GetServerMetrics retrieves time-series data based on query parameters.
func (s *BaseStore) GetServerMetrics(ctx context.Context, query ServerMetricsQuery) (*ServerMetricsTimeSeries, error) {
	if query.EndTime.IsZero() {
		query.EndTime = time.Now().UTC()
	}
	if query.StartTime.IsZero() {
		query.StartTime = query.EndTime.Add(-24 * time.Hour)
	}
	if query.Resolution == "" || query.Resolution == "auto" {
		query.Resolution = PickResolution(query.StartTime, query.EndTime)
	}
	if query.MaxPoints == 0 {
		query.MaxPoints = 1000 // Default max points for performance
	}

	// Query the appropriate tier
	// Note: id column is not included - it may not exist after TimescaleDB hypertable conversion
	sqlQuery := `
		SELECT timestamp, tier, fleet_json, server_json
		FROM server_metrics_history
		WHERE timestamp >= ? AND timestamp <= ? AND tier = ?
		ORDER BY timestamp ASC
	`
	args := []interface{}{
		query.StartTime.UTC().Format(time.RFC3339Nano),
		query.EndTime.UTC().Format(time.RFC3339Nano),
		query.Resolution,
	}

	// If requesting raw data but range is too long, fall back to hourly
	if query.Resolution == "raw" && query.EndTime.Sub(query.StartTime) > 24*time.Hour {
		// Count how many raw points we'd return
		var count int
		countQuery := `SELECT COUNT(*) FROM server_metrics_history WHERE timestamp >= ? AND timestamp <= ? AND tier = 'raw'`
		s.queryRowContext(ctx, countQuery, args[0], args[1]).Scan(&count)
		if count > query.MaxPoints*2 {
			// Too many raw points, switch to hourly
			query.Resolution = "hourly"
			args[2] = "hourly"
		}
	}

	rows, err := s.queryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("query server metrics: %w", err)
	}
	defer rows.Close()

	snapshots := make([]ServerMetricsSnapshot, 0)
	for rows.Next() {
		var snap ServerMetricsSnapshot
		var tsStr, fleetJSON, serverJSON string
		if err := rows.Scan(&tsStr, &snap.Tier, &fleetJSON, &serverJSON); err != nil {
			continue
		}
		if t, err := time.Parse(time.RFC3339Nano, tsStr); err == nil {
			snap.Timestamp = t
		} else if t, err := time.Parse("2006-01-02 15:04:05", tsStr); err == nil {
			snap.Timestamp = t.UTC()
		}
		json.Unmarshal([]byte(fleetJSON), &snap.Fleet)
		json.Unmarshal([]byte(serverJSON), &snap.Server)
		snapshots = append(snapshots, snap)
	}

	// Decimate if too many points
	if len(snapshots) > query.MaxPoints {
		snapshots = decimateSnapshots(snapshots, query.MaxPoints)
	}

	result := &ServerMetricsTimeSeries{
		StartTime:  query.StartTime,
		EndTime:    query.EndTime,
		Resolution: query.Resolution,
		PointCount: len(snapshots),
		Snapshots:  snapshots,
	}

	// Build chart series if requested
	if len(query.Series) > 0 {
		result.ChartSeries = BuildChartSeries(snapshots, query.Series)
	} else {
		result.ChartSeries = BuildChartSeries(snapshots, nil)
	}

	return result, nil
}

// GetLatestServerMetrics returns the most recent raw snapshot.
func (s *BaseStore) GetLatestServerMetrics(ctx context.Context) (*ServerMetricsSnapshot, error) {
	// Note: id column is not included - it may not exist after TimescaleDB hypertable conversion
	query := `
		SELECT timestamp, tier, fleet_json, server_json
		FROM server_metrics_history
		WHERE tier = 'raw'
		ORDER BY timestamp DESC
		LIMIT 1
	`
	row := s.queryRowContext(ctx, query)

	var snap ServerMetricsSnapshot
	var tsStr, fleetJSON, serverJSON string
	err := row.Scan(&tsStr, &snap.Tier, &fleetJSON, &serverJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if t, err := time.Parse(time.RFC3339Nano, tsStr); err == nil {
		snap.Timestamp = t
	}
	json.Unmarshal([]byte(fleetJSON), &snap.Fleet)
	json.Unmarshal([]byte(serverJSON), &snap.Server)

	return &snap, nil
}

// AggregateServerMetrics computes minute/hourly/daily aggregates from finer-grained data.
func (s *BaseStore) AggregateServerMetrics(ctx context.Context) error {
	now := time.Now().UTC()

	// Aggregate raw -> minute (for data older than 2 minutes, to allow for late arrivals)
	if err := s.aggregateServerMetricsTier(ctx, "raw", "minute", now.Add(-2*time.Minute), time.Minute); err != nil {
		return fmt.Errorf("aggregate raw->minute: %w", err)
	}

	// Aggregate minute -> hourly (for data older than 1 hour)
	if err := s.aggregateServerMetricsTier(ctx, "minute", "hourly", now.Add(-time.Hour), time.Hour); err != nil {
		return fmt.Errorf("aggregate minute->hourly: %w", err)
	}

	// Aggregate hourly -> daily (for data older than 24 hours)
	if err := s.aggregateServerMetricsTier(ctx, "hourly", "daily", now.Add(-24*time.Hour), 24*time.Hour); err != nil {
		return fmt.Errorf("aggregate hourly->daily: %w", err)
	}

	return nil
}

func (s *BaseStore) aggregateServerMetricsTier(ctx context.Context, srcTier, dstTier string, olderThan time.Time, bucketSize time.Duration) error {
	// Find the time range to aggregate
	query := `
		SELECT MIN(timestamp), MAX(timestamp)
		FROM server_metrics_history
		WHERE tier = ? AND timestamp < ?
	`
	var minTsStr, maxTsStr sql.NullString
	err := s.queryRowContext(ctx, query, srcTier, olderThan.Format(time.RFC3339Nano)).Scan(&minTsStr, &maxTsStr)
	if err != nil || !minTsStr.Valid {
		return nil // No data to aggregate
	}

	minTs, _ := time.Parse(time.RFC3339Nano, minTsStr.String)
	maxTs, _ := time.Parse(time.RFC3339Nano, maxTsStr.String)

	// Truncate to bucket boundaries
	minTs = minTs.Truncate(bucketSize)
	maxTs = maxTs.Truncate(bucketSize)

	// Process each bucket
	for bucketStart := minTs; !bucketStart.After(maxTs); bucketStart = bucketStart.Add(bucketSize) {
		bucketEnd := bucketStart.Add(bucketSize)

		// Check if we already have an aggregate for this bucket
		var existing int
		checkQuery := `SELECT COUNT(*) FROM server_metrics_history WHERE tier = ? AND timestamp >= ? AND timestamp < ?`
		s.queryRowContext(ctx, checkQuery, dstTier, bucketStart.Format(time.RFC3339Nano), bucketEnd.Format(time.RFC3339Nano)).Scan(&existing)
		if existing > 0 {
			continue // Already aggregated
		}

		// Get source data for this bucket
		srcQuery := `
			SELECT fleet_json, server_json
			FROM server_metrics_history
			WHERE tier = ? AND timestamp >= ? AND timestamp < ?
			ORDER BY timestamp ASC
		`
		rows, err := s.queryContext(ctx, srcQuery, srcTier, bucketStart.Format(time.RFC3339Nano), bucketEnd.Format(time.RFC3339Nano))
		if err != nil {
			continue
		}

		var fleetAgg FleetSnapshot
		var serverAgg ServerSnapshot
		var count int

		for rows.Next() {
			var fleetJSON, serverJSON string
			if err := rows.Scan(&fleetJSON, &serverJSON); err != nil {
				continue
			}

			var fleet FleetSnapshot
			var server ServerSnapshot
			json.Unmarshal([]byte(fleetJSON), &fleet)
			json.Unmarshal([]byte(serverJSON), &server)

			// Aggregate by taking max/sum as appropriate
			if fleet.TotalAgents > fleetAgg.TotalAgents {
				fleetAgg.TotalAgents = fleet.TotalAgents
			}
			if fleet.TotalDevices > fleetAgg.TotalDevices {
				fleetAgg.TotalDevices = fleet.TotalDevices
			}

			// Agent connection breakdown - take max
			if fleet.AgentsWS > fleetAgg.AgentsWS {
				fleetAgg.AgentsWS = fleet.AgentsWS
			}
			if fleet.AgentsHTTP > fleetAgg.AgentsHTTP {
				fleetAgg.AgentsHTTP = fleet.AgentsHTTP
			}
			if fleet.AgentsOffline > fleetAgg.AgentsOffline {
				fleetAgg.AgentsOffline = fleet.AgentsOffline
			}

			if fleet.TotalPages > fleetAgg.TotalPages {
				fleetAgg.TotalPages = fleet.TotalPages
			}
			if fleet.ColorPages > fleetAgg.ColorPages {
				fleetAgg.ColorPages = fleet.ColorPages
			}
			if fleet.MonoPages > fleetAgg.MonoPages {
				fleetAgg.MonoPages = fleet.MonoPages
			}
			if fleet.ScanCount > fleetAgg.ScanCount {
				fleetAgg.ScanCount = fleet.ScanCount
			}

			// For toner levels, take max (worst case in period)
			if fleet.TonerCritical > fleetAgg.TonerCritical {
				fleetAgg.TonerCritical = fleet.TonerCritical
			}
			if fleet.TonerLow > fleetAgg.TonerLow {
				fleetAgg.TonerLow = fleet.TonerLow
			}
			if fleet.TonerMedium > fleetAgg.TonerMedium {
				fleetAgg.TonerMedium = fleet.TonerMedium
			}
			if fleet.TonerHigh > fleetAgg.TonerHigh {
				fleetAgg.TonerHigh = fleet.TonerHigh
			}

			// For device status counts, take max (peak values in period)
			if fleet.DevicesOnline > fleetAgg.DevicesOnline {
				fleetAgg.DevicesOnline = fleet.DevicesOnline
			}
			if fleet.DevicesOffline > fleetAgg.DevicesOffline {
				fleetAgg.DevicesOffline = fleet.DevicesOffline
			}
			if fleet.DevicesWarning > fleetAgg.DevicesWarning {
				fleetAgg.DevicesWarning = fleet.DevicesWarning
			}
			if fleet.DevicesError > fleetAgg.DevicesError {
				fleetAgg.DevicesError = fleet.DevicesError
			}
			if fleet.DevicesJam > fleetAgg.DevicesJam {
				fleetAgg.DevicesJam = fleet.DevicesJam
			}
			if fleet.TonerUnknown > fleetAgg.TonerUnknown {
				fleetAgg.TonerUnknown = fleet.TonerUnknown
			}

			// Server stats - take average for goroutines/memory, max for DB
			serverAgg.Goroutines += server.Goroutines
			serverAgg.HeapAllocMB += server.HeapAllocMB
			serverAgg.TotalAllocMB += server.TotalAllocMB
			serverAgg.SysMB += server.SysMB
			if server.DBSizeBytes > serverAgg.DBSizeBytes {
				serverAgg.DBSizeBytes = server.DBSizeBytes
			}
			if server.DBMetricsRows > serverAgg.DBMetricsRows {
				serverAgg.DBMetricsRows = server.DBMetricsRows
			}
			serverAgg.WSConnections += server.WSConnections
			serverAgg.WSAgents += server.WSAgents

			count++
		}
		rows.Close()

		if count == 0 {
			continue
		}

		// Average the runtime stats
		serverAgg.Goroutines /= count
		serverAgg.HeapAllocMB /= count
		serverAgg.TotalAllocMB /= count
		serverAgg.SysMB /= count
		serverAgg.WSConnections /= count
		serverAgg.WSAgents /= count

		// Insert aggregated snapshot
		aggSnap := &ServerMetricsSnapshot{
			Timestamp: bucketStart.Add(bucketSize / 2), // Midpoint of bucket
			Tier:      dstTier,
			Fleet:     fleetAgg,
			Server:    serverAgg,
		}
		if err := s.InsertServerMetrics(ctx, aggSnap); err != nil {
			logWarn("Failed to insert aggregated server metrics", "tier", dstTier, "bucket", bucketStart, "error", err)
		}
	}

	return nil
}

// PruneServerMetrics removes old data based on retention policy.
func (s *BaseStore) PruneServerMetrics(ctx context.Context) error {
	now := time.Now().UTC()

	// Prune raw data older than retention (2 hours)
	rawCutoff := now.Add(-ServerMetricsRawRetention)
	if _, err := s.execContext(ctx,
		"DELETE FROM server_metrics_history WHERE tier = 'raw' AND timestamp < ?",
		rawCutoff.Format(time.RFC3339Nano)); err != nil {
		logWarn("Failed to prune raw server metrics", "error", err)
	}

	// Prune minute data older than retention (7 days)
	minuteCutoff := now.Add(-ServerMetricsMinuteRetention)
	if _, err := s.execContext(ctx,
		"DELETE FROM server_metrics_history WHERE tier = 'minute' AND timestamp < ?",
		minuteCutoff.Format(time.RFC3339Nano)); err != nil {
		logWarn("Failed to prune minute server metrics", "error", err)
	}

	// Prune hourly data older than retention (90 days)
	hourlyCutoff := now.Add(-ServerMetricsHourlyRetention)
	if _, err := s.execContext(ctx,
		"DELETE FROM server_metrics_history WHERE tier = 'hourly' AND timestamp < ?",
		hourlyCutoff.Format(time.RFC3339Nano)); err != nil {
		logWarn("Failed to prune hourly server metrics", "error", err)
	}

	// Prune daily data older than retention (1 year)
	dailyCutoff := now.Add(-ServerMetricsDailyRetention)
	if _, err := s.execContext(ctx,
		"DELETE FROM server_metrics_history WHERE tier = 'daily' AND timestamp < ?",
		dailyCutoff.Format(time.RFC3339Nano)); err != nil {
		logWarn("Failed to prune daily server metrics", "error", err)
	}

	return nil
}

// decimateSnapshots reduces the number of snapshots by picking every nth point.
func decimateSnapshots(snapshots []ServerMetricsSnapshot, maxPoints int) []ServerMetricsSnapshot {
	if len(snapshots) <= maxPoints {
		return snapshots
	}

	step := float64(len(snapshots)) / float64(maxPoints)
	result := make([]ServerMetricsSnapshot, 0, maxPoints)
	for i := 0.0; i < float64(len(snapshots)); i += step {
		idx := int(i)
		if idx >= len(snapshots) {
			idx = len(snapshots) - 1
		}
		result = append(result, snapshots[idx])
	}

	// Always include the last point
	if len(result) > 0 && result[len(result)-1].ID != snapshots[len(snapshots)-1].ID {
		result = append(result, snapshots[len(snapshots)-1])
	}

	return result
}
