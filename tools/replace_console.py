#!/usr/bin/env python3
"""
Replace console.<method>( with window.__pm_shared.<method>( across the repo
Skips minified files and common static/vendor/docs folders.
Creates a .bak backup for each modified file.
"""
import re
import sys
from pathlib import Path

ROOT = Path(r"c:\temp\printmaster")
# Exclude these path fragments (normalized to forward slashes for comparison)
EXCLUDE_DIRS = ['/static/', '/flatpickr/', '/vendor/', '/node_modules/', '/docs/', '/.git/']
METHODS = ['debug','info','warn','error','trace','log']
PAT = re.compile(r"\bconsole\.({})\s*\(".format('|'.join(METHODS)))

if not ROOT.exists():
    print('Root path does not exist:', ROOT)
    sys.exit(1)

changed_files = []
for p in ROOT.rglob('*.js'):
    sp = str(p).replace('\\', '/').lower()
    # Skip files that match exclusions
    if any(x in sp for x in EXCLUDE_DIRS):
        continue
    # Skip minified files
    if '.min.' in p.name.lower():
        continue
    try:
        txt = p.read_text(encoding='utf-8')
    except Exception as e:
        print('Skipping (read error):', p, e)
        continue
    new_txt, n = PAT.subn(lambda m: f"window.__pm_shared.{m.group(1)}(", txt)
    if n > 0:
        bak = p.with_suffix(p.suffix + '.bak')
        if not bak.exists():
            bak.write_text(txt, encoding='utf-8')
        p.write_text(new_txt, encoding='utf-8')
        changed_files.append((p, n))
        print(f'Patched {p} ({n} replacements)')

print('\nSummary:')
print(f'Files changed: {len(changed_files)}')
for f,n in changed_files[:200]:
    print('-', f, n)

if len(changed_files) == 0:
    print('No changes made.')
else:
    print('\nBackups created with .js.bak suffix in same directories.')

print('Done')
