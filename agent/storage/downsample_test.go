package storage

import (
	"context"
	"testing"
	"time"
)

func TestSQLiteStore_GetTieredMetricsHistory_RangeBounded(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	serial := "TEST_METRICS_RANGE"

	// Seed a minimal device to satisfy foreign key expectations in some schemas.
	dev := newFullTestDevice(serial, "192.168.1.200", "HP", "LaserJet", true, true)
	if err := store.Create(ctx, dev); err != nil {
		t.Fatalf("Failed to create device: %v", err)
	}

	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// Helper inserts directly into tier tables using the store's DB.
	insertRaw := func(ts time.Time, pages int) {
		t.Helper()
		_, err := store.db.ExecContext(ctx,
			`INSERT INTO metrics_raw (serial, timestamp, page_count, color_pages, mono_pages, scan_count, toner_levels) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			serial, ts.Format(time.RFC3339Nano), pages, 0, 0, 0, `{}`,
		)
		if err != nil {
			t.Fatalf("Failed to insert raw: %v", err)
		}
	}
	insertHourly := func(ts time.Time, pages int) {
		t.Helper()
		_, err := store.db.ExecContext(ctx,
			`INSERT INTO metrics_hourly (serial, hour_start, sample_count, page_count_min, page_count_max, page_count_avg, color_pages_min, color_pages_max, color_pages_avg, mono_pages_min, mono_pages_max, mono_pages_avg, scan_count_min, scan_count_max, scan_count_avg, toner_levels_avg) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			serial, ts.Format(time.RFC3339Nano), 1,
			pages, pages, pages,
			0, 0, 0,
			0, 0, 0,
			0, 0, 0,
			`{}`,
		)
		if err != nil {
			t.Fatalf("Failed to insert hourly: %v", err)
		}
	}
	insertDaily := func(ts time.Time, pages int) {
		t.Helper()
		_, err := store.db.ExecContext(ctx,
			`INSERT INTO metrics_daily (serial, day_start, sample_count, page_count_min, page_count_max, page_count_avg, color_pages_min, color_pages_max, color_pages_avg, mono_pages_min, mono_pages_max, mono_pages_avg, scan_count_min, scan_count_max, scan_count_avg, toner_levels_avg) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			serial, ts.Format(time.RFC3339Nano), 1,
			pages, pages, pages,
			0, 0, 0,
			0, 0, 0,
			0, 0, 0,
			`{}`,
		)
		if err != nil {
			t.Fatalf("Failed to insert daily: %v", err)
		}
	}
	insertMonthly := func(ts time.Time, pages int) {
		t.Helper()
		_, err := store.db.ExecContext(ctx,
			`INSERT INTO metrics_monthly (serial, month_start, sample_count, page_count_min, page_count_max, page_count_avg, color_pages_min, color_pages_max, color_pages_avg, mono_pages_min, mono_pages_max, mono_pages_avg, scan_count_min, scan_count_max, scan_count_avg, toner_levels_avg) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			serial, ts.Format(time.RFC3339Nano), 1,
			pages, pages, pages,
			0, 0, 0,
			0, 0, 0,
			0, 0, 0,
			`{}`,
		)
		if err != nil {
			t.Fatalf("Failed to insert monthly: %v", err)
		}
	}

	// Insert points inside and outside the window for each tier.
	insertRaw(base.Add(-2*time.Hour), 10)          // outside
	insertRaw(base.Add(1*time.Hour), 20)           // inside
	insertHourly(base.Add(-48*time.Hour), 100)     // outside
	insertHourly(base.Add(24*time.Hour), 200)      // inside
	insertDaily(base.Add(-48*time.Hour), 1000)     // outside
	insertDaily(base.Add(72*time.Hour), 2000)      // inside
	insertMonthly(base.Add(-720*time.Hour), 10000) // outside
	insertMonthly(base.Add(720*time.Hour), 20000)  // inside

	since := base
	until := base.Add(10 * 24 * time.Hour)

	got, err := store.GetTieredMetricsHistory(ctx, serial, since, until)
	if err != nil {
		t.Fatalf("GetTieredMetricsHistory returned error: %v", err)
	}
	if len(got) == 0 {
		t.Fatalf("Expected non-empty history")
	}

	for _, snap := range got {
		if snap.Timestamp.Before(since) || snap.Timestamp.After(until) {
			t.Fatalf("Returned snapshot out of range: ts=%s since=%s until=%s tier=%s", snap.Timestamp, since, until, snap.Tier)
		}
		if snap.Tier == "" {
			t.Fatalf("Expected Tier to be set")
		}
	}
}
