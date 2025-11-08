package storage

import "time"

// Helper function to create test device with embedded struct
func newTestDevice(serial, ip string, isSaved, visible bool) *Device {
	d := &Device{}
	d.Serial = serial
	d.IP = ip
	d.IsSaved = isSaved
	d.Visible = visible
	return d
}

// Helper function to create test device with more fields
func newFullTestDevice(serial, ip, manufacturer, model string, isSaved, visible bool) *Device {
	d := &Device{}
	d.Serial = serial
	d.IP = ip
	d.Manufacturer = manufacturer
	d.Model = model
	d.IsSaved = isSaved
	d.Visible = visible
	return d
}

// Helper function to create test metrics snapshot
func newTestMetrics(serial string, pageCount int) *MetricsSnapshot {
	m := &MetricsSnapshot{}
	m.Serial = serial
	m.Timestamp = time.Now()
	m.PageCount = pageCount
	m.TonerLevels = make(map[string]interface{})
	return m
}
