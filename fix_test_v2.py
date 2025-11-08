import re

def convert_device_to_helper(content):
    """Convert all &Device{...} patterns to helper function calls"""
    
    # Pattern for multi-line Device with many fields - extract all fields dynamically
    def replace_multiline_device(match):
        full_match = match.group(0)
        var_name = match.group(1) if match.group(1) else 'device'
        
        # Extract field assignments
        serial = re.search(r'Serial:\s*"([^"]+)"', full_match)
        ip = re.search(r'IP:\s*"([^"]+)"', full_match)
        manufacturer = re.search(r'Manufacturer:\s*"([^"]+)"', full_match)
        model = re.search(r'Model:\s*"([^"]+)"', full_match)
        is_saved = re.search(r'IsSaved:\s*(\w+)', full_match)
        
        if not serial or not ip:
            return full_match  # Can't convert without serial/IP
        
        # Use appropriate helper
        if manufacturer and model:
            helper_call = f'{var_name} := newFullTestDevice("{serial.group(1)}", "{ip.group(1)}", "{manufacturer.group(1)}", "{model.group(1)}", {is_saved.group(1) if is_saved else "false"}, true)'
        else:
            helper_call = f'{var_name} := newTestDevice("{serial.group(1)}", "{ip.group(1)}", {is_saved.group(1) if is_saved else "false"}, true)'
        
        # Extract any additional fields that need manual assignment
        additional = []
        for field in ['DNSServers', 'DHCPServer', 'Consumables', 'StatusMessages', 'DiscoveryMethod', 'WalkFilename', 'RawData', 'Hostname', 'Firmware', 'MACAddress']:
            field_match = re.search(rf'{field}:\s*([^,}}]+(?:{{[^}}]*}})?)', full_match, re.DOTALL)
            if field_match:
                value = field_match.group(1).strip()
                # Handle multi-line values
                if '\n' in value:
                    value = value.replace('\n\t\t', ' ').replace('\n\t', ' ').strip()
                additional.append(f'\t{var_name}.{field} = {value}')
        
        result = helper_call
        if additional:
            result += '\n' + '\n'.join(additional)
        
        return result
    
    # Match: varname := &Device{ ... } (multi-line)
    pattern = r'(\w+)\s*:=\s*&Device\{[^}]*(?:\{[^}]*\}[^}]*)*\}'
    content = re.sub(pattern, replace_multiline_device, content, flags=re.DOTALL)
    
    # Match: store.Create(ctx, &Device{Serial: "X", IP: "Y", ...})
    def replace_inline_device(match):
        full_match = match.group(0)
        prefix = match.group(1)
        
        serial = re.search(r'Serial:\s*"([^"]+)"', full_match)
        ip = re.search(r'IP:\s*"([^"]+)"', full_match)
        manufacturer = re.search(r'Manufacturer:\s*"([^"]+)"', full_match)
        model = re.search(r'Model:\s*"([^"]+)"', full_match)
        is_saved = re.search(r'IsSaved:\s*(\w+)', full_match)
        
        if not serial or not ip:
            return full_match
        
        if manufacturer and model:
            return f'{prefix}newFullTestDevice("{serial.group(1)}", "{ip.group(1)}", "{manufacturer.group(1)}", "{model.group(1)}", {is_saved.group(1) if is_saved else "false"}, true))'
        else:
            return f'{prefix}newTestDevice("{serial.group(1)}", "{ip.group(1)}", {is_saved.group(1) if is_saved else "false"}, true))'
    
    pattern2 = r'(store\.Create\(ctx,\s*)&Device\{[^}]+\}'
    content = re.sub(pattern2, replace_inline_device, content)
    
    return content

def convert_metrics_to_helper(content):
    """Convert all &MetricsSnapshot{...} patterns to proper initialization"""
    
    def replace_metrics(match):
        full_match = match.group(0)
        var_name = match.group(1) if match.group(1) else 'snapshot'
        
        serial = re.search(r'Serial:\s*"([^"]+)"', full_match)
        page_count = re.search(r'PageCount:\s*(\d+)', full_match)
        
        if not serial:
            return full_match
        
        result = f'{var_name} := newTestMetrics("{serial.group(1)}", {page_count.group(1) if page_count else "0"})'
        
        # Check for custom TonerLevels
        toner_match = re.search(r'TonerLevels:\s*(map\[string\]interface\{}\{[^}]+\})', full_match, re.DOTALL)
        if toner_match:
            toner_value = toner_match.group(1).replace('\n\t\t', ' ').replace('\n\t', ' ').strip()
            result += f'\n\t{var_name}.TonerLevels = {toner_value}'
        
        return result
    
    pattern = r'(\w+)\s*:=\s*&MetricsSnapshot\{[^}]*(?:\{[^}]*\}[^}]*)*\}'
    content = re.sub(pattern, replace_metrics, content, flags=re.DOTALL)
    
    return content

# Process sqlite_test.go
print('Processing sqlite_test.go...')
with open('agent/storage/sqlite_test.go', 'r', encoding='utf-8') as f:
    content = f.read()

content = convert_device_to_helper(content)
content = convert_metrics_to_helper(content)

with open('agent/storage/sqlite_test.go', 'w', encoding='utf-8') as f:
    f.write(content)

print('✓ Fixed sqlite_test.go')
print('\nAll test files converted to use helper functions!')
