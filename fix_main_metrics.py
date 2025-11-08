import re

def fix_metrics_snapshot_in_file(filepath):
    """Fix MetricsSnapshot struct literals in a file"""
    with open(filepath, 'r', encoding='utf-8') as f:
        content = f.read()
    
    # Pattern: storageSnapshot := &storage.MetricsSnapshot{ ... }
    # This is a multi-line pattern that needs careful handling
    pattern = r'(storageSnapshot\s*:=\s*)&storage\.MetricsSnapshot\{\s+((?:[^}]+\n)+)\s+\}'
    
    def replace_snapshot(match):
        var_decl = match.group(1)
        fields_block = match.group(2)
        
        # Extract field assignments
        field_lines = []
        for line in fields_block.strip().split('\n'):
            line = line.strip()
            if line and not line.startswith('//'):
                # Convert "Field: value," to "storageSnapshot.Field = value"
                if ':' in line:
                    field_match = re.match(r'(\w+):\s*(.+?),?\s*$', line)
                    if field_match:
                        field_name = field_match.group(1)
                        field_value = field_match.group(2).rstrip(',')
                        field_lines.append(f'\t\t\tstorageSnapshot.{field_name} = {field_value}')
        
        result = f'{var_decl}&storage.MetricsSnapshot{{}}\n'
        result += '\n'.join(field_lines)
        return result
    
    # Apply the replacement
    content = re.sub(pattern, replace_snapshot, content, flags=re.MULTILINE)
    
    with open(filepath, 'w', encoding='utf-8') as f:
        f.write(content)
    
    print(f'✓ Fixed {filepath}')

# Fix agent/main.go
print('Processing agent/main.go...')
fix_metrics_snapshot_in_file('agent/main.go')

print('\nDone!')
