import re
import sys

def add_helper_if_missing(content, filename):
    """Add test helper functions if they don't exist"""
    if 'func newTestDevice(' not in content:
        # Find the import section and add helpers after it
        import_end = content.find('\n\nfunc ')
        if import_end == -1:
            import_end = content.find('\nfunc ')
        
        helpers = '''
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
'''
        content = content[:import_end] + helpers + content[import_end:]
        print(f'  Added helper functions to {filename}')
    return content

def fix_device_multiline_comprehensive(content):
    """Fix all Device{} struct literal patterns - multi-line and inline"""
    
    # Pattern 1: Multi-line with Serial, IP, Manufacturer, Model, IsSaved, Consumables
    pattern1 = r'&Device\{\s+Serial:\s+"([^"]+)",\s+IP:\s+"([^"]+)",\s+Manufacturer:\s+"([^"]+)",\s+Model:\s+"([^"]+)",\s+IsSaved:\s+(\w+),\s+Consumables:\s+\[([^\]]*)\],?\s+\}'
    def replace1(m):
        return f'newFullTestDevice("{m.group(1)}", "{m.group(2)}", "{m.group(3)}", "{m.group(4)}", {m.group(5)}, true)'
    content = re.sub(pattern1, replace1, content)
    
    # Pattern 2: Multi-line with Serial, IP, Manufacturer, Model, IsSaved
    pattern2 = r'&Device\{\s+Serial:\s+"([^"]+)",\s+IP:\s+"([^"]+)",\s+Manufacturer:\s+"([^"]+)",\s+Model:\s+"([^"]+)",\s+IsSaved:\s+(\w+),?\s+\}'
    content = re.sub(pattern2, lambda m: f'newFullTestDevice("{m.group(1)}", "{m.group(2)}", "{m.group(3)}", "{m.group(4)}", {m.group(5)}, true)', content)
    
    # Pattern 3: Multi-line with Serial, IP, Manufacturer, Model (no IsSaved)
    pattern3 = r'&Device\{\s+Serial:\s+"([^"]+)",\s+IP:\s+"([^"]+)",\s+Manufacturer:\s+"([^"]+)",\s+Model:\s+"([^"]+)",?\s+\}'
    content = re.sub(pattern3, lambda m: f'newFullTestDevice("{m.group(1)}", "{m.group(2)}", "{m.group(3)}", "{m.group(4)}", false, true)', content)
    
    # Pattern 4: Inline with Serial, IP, Manufacturer, Model, IsSaved
    pattern4 = r'&Device\{Serial:\s*"([^"]+)",\s*IP:\s*"([^"]+)",\s*Manufacturer:\s*"([^"]+)",\s*Model:\s*"([^"]+)",\s*IsSaved:\s*(\w+)\}'
    content = re.sub(pattern4, lambda m: f'newFullTestDevice("{m.group(1)}", "{m.group(2)}", "{m.group(3)}", "{m.group(4)}", {m.group(5)}, true)', content)
    
    # Pattern 5: Inline with Serial, IP, IsSaved (most common inline pattern)
    pattern5 = r'&Device\{Serial:\s*"([^"]+)",\s*IP:\s*"([^"]+)",\s*IsSaved:\s*(\w+)\}'
    content = re.sub(pattern5, lambda m: f'newTestDevice("{m.group(1)}", "{m.group(2)}", {m.group(3)}, true)', content)
    
    return content

def fix_metrics_multiline_comprehensive(content):
    """Fix all MetricsSnapshot{} struct literal patterns"""
    
    # Pattern 1: Multi-line with Serial, Timestamp, PageCount, TonerLevels
    pattern1 = r'(\w+)\s*:=\s*&MetricsSnapshot\{\s+Serial:\s+"([^"]+)",\s+Timestamp:\s+([^,]+),\s+PageCount:\s+(\d+),\s+TonerLevels:\s+([^}]+)\}'
    def replace1(m):
        varname = m.group(1)
        serial = m.group(2)
        timestamp = m.group(3)
        pagecount = m.group(4)
        toner = m.group(5)
        return f'''{varname} := &MetricsSnapshot{{}}
	{varname}.Serial = "{serial}"
	{varname}.Timestamp = {timestamp}
	{varname}.PageCount = {pagecount}
	{varname}.TonerLevels = {toner}'''
    content = re.sub(pattern1, replace1, content)
    
    # Pattern 2: Multi-line with Serial, Timestamp, PageCount (no toner)
    pattern2 = r'(\w+)\s*:=\s*&MetricsSnapshot\{\s+Serial:\s+"([^"]+)",\s+Timestamp:\s+([^,]+),\s+PageCount:\s+(\d+),?\s+\}'
    def replace2(m):
        varname = m.group(1)
        return f'''{varname} := newTestMetrics("{m.group(2)}", {m.group(4)})'''
    content = re.sub(pattern2, replace2, content)
    
    # Pattern 3: Inline single-line patterns
    pattern3 = r'&MetricsSnapshot\{Serial:\s*"([^"]+)",\s*Timestamp:\s*([^,]+),\s*PageCount:\s*(\d+),?\s*\}'
    content = re.sub(pattern3, lambda m: f'newTestMetrics("{m.group(1)}", {m.group(3)})', content)
    
    return content

# Process scan_history_test.go
print('Processing scan_history_test.go...')
with open('agent/storage/scan_history_test.go', 'r', encoding='utf-8') as f:
    content = f.read()
content = add_helper_if_missing(content, 'scan_history_test.go')
content = fix_device_multiline_comprehensive(content)
with open('agent/storage/scan_history_test.go', 'w', encoding='utf-8') as f:
    f.write(content)
print('✓ Fixed scan_history_test.go')

# Process sqlite_test.go
print('Processing sqlite_test.go...')
with open('agent/storage/sqlite_test.go', 'r', encoding='utf-8') as f:
    content = f.read()
content = add_helper_if_missing(content, 'sqlite_test.go')
content = fix_device_multiline_comprehensive(content)
content = fix_metrics_multiline_comprehensive(content)
with open('agent/storage/sqlite_test.go', 'w', encoding='utf-8') as f:
    f.write(content)
print('✓ Fixed sqlite_test.go')

print('\nAll test files fixed!')

