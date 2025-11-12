function showPrinterDetailsData(p, source, parseDebug) {
    // Delegated to shared implementation in `common/web/cards.js`.
    try {
        return window.__pm_shared_cards.showPrinterDetailsData(p, source, parseDebug);
    } catch (e) {
        console.warn('shared showPrinterDetailsData failed', e);
    }
}

// Update the discovered and saved printers UI by querying the backend and
// rendering cards using the shared card renderers in `common/web/cards.js`.
function updatePrinters() {
    try {
        const showKnownDevices = document.getElementById('show_saved_in_discovered')?.checked || false;

        // Map slider index to minute values (0 = all time)
        const timeFilterValues = [1,2,5,10,15,30,60,120,180,360,720,1440,4320,0];
        const slider = document.getElementById('time_slider');
        const index = slider ? parseInt(slider.value) : (timeFilterValues.length - 1);
        const timeMinutes = timeFilterValues[index] || 0;

        let discoveredEndpoint = '/devices/discovered?include_known=' + showKnownDevices;
        if (timeMinutes > 0) discoveredEndpoint += '&minutes=' + timeMinutes;

        Promise.all([
            fetch(discoveredEndpoint).then(r => r.ok ? r.json() : []),
            fetch('/devices/list').then(r => r.ok ? r.json() : [])
        ]).then(([discovered, saved]) => {
            window.discoveredPrinters = discovered || [];

            const discoveredContainer = document.getElementById('discovered_devices_cards');
            if (!discoveredContainer) return;

            const savedSerials = new Set();
            const savedIPs = new Set();
            if (Array.isArray(saved)) {
                saved.forEach(item => {
                    const p = item.printer_info || {};
                    const serial = item.serial || '';
                    const ip = p.ip || '';
                    if (serial) savedSerials.add(serial);
                    if (ip) savedIPs.add(ip);
                });
            }

            // Autosave/display controls
            const autosaveEnabled = document.getElementById('autosave_checkbox')?.checked || false;
            const showDiscoveredAnyway = document.getElementById('show_discovered_devices_anyway')?.checked || false;
            const discoveredSection = document.getElementById('discovered_section');
            if (discoveredSection) {
                const shouldShowDiscovered = !autosaveEnabled || (autosaveEnabled && showDiscoveredAnyway);
                discoveredSection.style.display = shouldShowDiscovered ? 'block' : 'none';
            }

            // Render discovered
            if (!Array.isArray(discovered) || discovered.length === 0) {
                discoveredContainer.innerHTML = '<div style="color:var(--muted);padding:12px">No discovered printers</div>';
                const statsEl = document.getElementById('discovered_stats'); if (statsEl) statsEl.innerHTML = '';
            } else {
                let lowTonerCount = 0;
                discovered.forEach(p => {
                    const toners = p.toner_levels || {};
                    for (const c in toners) { if (toners[c] < 20) { lowTonerCount++; break; } }
                });
                const statsHtml = '<span style="color:var(--text)"><strong>Total:</strong> ' + discovered.length + '</span>' +
                    '<span style="color:#b58900"><strong>Low Toner:</strong> ' + lowTonerCount + '</span>';
                const statsEl = document.getElementById('discovered_stats'); if (statsEl) statsEl.innerHTML = statsHtml;

                let cardsHTML = '';
                discovered.forEach(p => {
                    const isSaved = (p.serial && savedSerials.has(p.serial)) || (p.ip && savedIPs.has(p.ip));
                    cardsHTML += window.__pm_shared_cards.renderDiscoveredCard(p, isSaved);
                });
                discoveredContainer.innerHTML = cardsHTML;
            }

            // Autosave new discovered devices if enabled
            if (autosaveEnabled && Array.isArray(discovered) && discovered.length > 0) {
                if (!window.autosavedIPs) window.autosavedIPs = new Set();
                discovered.forEach(p => {
                    const isSaved = (p.serial && savedSerials.has(p.serial)) || (p.ip && savedIPs.has(p.ip));
                    if (p.ip && !isSaved && !window.autosavedIPs.has(p.ip)) {
                        window.autosavedIPs.add(p.ip);
                    window.__pm_shared.saveDiscoveredDevice(p.ip, true).catch(() => { window.autosavedIPs.delete(p.ip); });
                    }
                });
            }

        }).catch(e => { console.error('updatePrinters discovered error', e); });

        // Render saved devices
        fetch('/devices/list').then(r => r.ok ? r.json() : []).then(saved => {
            const savedContainer = document.getElementById('saved_devices_cards');
            if (!savedContainer) return;

            if (!Array.isArray(saved) || saved.length === 0) {
                savedContainer.innerHTML = '<div style="color:var(--muted);padding:12px">No saved devices</div>';
                const statsEl = document.getElementById('saved_stats'); if (statsEl) statsEl.innerHTML = '';
            } else {
                let lowTonerCount = 0;
                saved.forEach(item => {
                    const p = item.printer_info || {};
                    const toners = p.toner_levels || {};
                    for (const c in toners) { if (toners[c] < 20) { lowTonerCount++; break; } }
                });
                const statsHtml = '<span style="color:var(--text)"><strong>Total:</strong> ' + saved.length + '</span>' +
                    '<span style="color:#b58900"><strong>Low Toner:</strong> ' + lowTonerCount + '</span>';
                const statsEl = document.getElementById('saved_stats'); if (statsEl) statsEl.innerHTML = statsHtml;

                const existingCards = Array.from(savedContainer.querySelectorAll('.saved-device-card'));
                const existingKeys = new Set(existingCards.map(c => c.dataset.deviceKey));
                const isInitialLoad = existingCards.length === 0;

                    if (isInitialLoad) {
                    let cardsHTML = '';
                    saved.forEach(item => { cardsHTML += window.__pm_shared_cards.renderSavedCard(item); });
                    savedContainer.innerHTML = cardsHTML;
                    saved.forEach(item => { if (item.serial) window.__pm_shared_metrics.loadUsageGraph(item.serial); });
                } else {
                    saved.forEach(item => {
                        const deviceKey = item.serial || (item.printer_info && item.printer_info.ip) || '';
                        if (!deviceKey) return;
                        if (!existingKeys.has(deviceKey)) {
                            const tempDiv = document.createElement('div');
                            tempDiv.innerHTML = window.__pm_shared_cards.renderSavedCard(item);
                            const newCard = tempDiv.firstElementChild;
                            if (newCard) savedContainer.appendChild(newCard);
                            requestAnimationFrame(() => { if (newCard) newCard.classList.add('card-entering'); });
                            if (item.serial) window.__pm_shared_metrics.loadUsageGraph(item.serial);
                        }
                    });

                    const savedKeys = new Set(saved.map(item => item.serial || (item.printer_info && item.printer_info.ip) || ''));
                    existingCards.forEach(card => {
                        const key = card.dataset.deviceKey;
                        if (key && !savedKeys.has(key)) { card.classList.add('removing'); setTimeout(() => card.remove(), 400); }
                    });
                }
            }
        }).catch(e => { console.error('updatePrinters saved error', e); });

    } catch (e) {
        console.error('updatePrinters failed', e);
    }
}

// Database backend field toggles are provided by the shared bundle
// (common/web/shared.js) and exported as window.__pm_shared.toggleDatabaseFields.
// We intentionally do not keep a local copy here to avoid duplication.

// Clear entire database (backup and reset)
async function clearDatabase() {
    const confirmed = await window.__pm_shared.showConfirm(
        'This will backup the current database and start fresh. All saved devices, metrics, and history will be moved to a backup file. Continue?',
        'Clear Database',
        true
    );
    if (!confirmed) return;
    
    try {
        const r = await fetch('/database/clear', { method: 'POST' });
        if (!r.ok) throw new Error('Database clear failed');
        const result = await r.json();
        if (result.reload) {
            window.__pm_shared.showToast('Database backed up and reset successfully. Reloading...', 'success');
            setTimeout(() => window.location.reload(), 1500);
        }
    } catch (e) {
        console.error('Database clear failed:', e);
        window.__pm_shared.showToast('Database clear failed: ' + e.message, 'error');
    }
}

// Show saved device details in modal
async function showSavedDeviceDetails(serial) {
    if (!serial) return;
    try {
        const r = await fetch('/devices/get?serial=' + encodeURIComponent(serial));
        if (!r.ok) throw new Error('Device not found');
        const device = await r.json();

        // Device from database has lowercase field names; use shared modal renderer
        // which normalizes both formats and is provided by common/web/cards.js
        window.__pm_shared_cards.showPrinterDetailsData(device, 'saved');
    } catch (e) {
        window.__pm_shared.showToast('Failed to load device: ' + e.message, 'error');
    }
}

// Delete saved device
async function deleteSavedDevice(serial) {
    if (!serial) return;
    const confirmed = await window.__pm_shared.showConfirm(
        'Delete this device? This will remove it from the database but it may be re-discovered if still on the network.',
        'Delete Device',
        true
    );
    if (!confirmed) return;
    
    try {
        const r = await fetch('/devices/delete', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ serial: serial }) });
        if (!r.ok) throw new Error('Delete failed');
        window.__pm_shared.showToast('Device deleted successfully', 'success');
        updatePrinters();
    } catch (e) {
    console.error('Delete failed:', e);
    window.__pm_shared.showToast('Delete failed: ' + e.message, 'error');
    }
}

// Edit a device field inline (Asset Number, Location)
async function editField(serial, fieldName, currentValue, element) {
    if (!serial || !fieldName) return;

    const displayName = fieldName === 'asset_number' ? 'Asset Number' : 'Location';
    const newValue = await window.__pm_shared.showPrompt('Enter new ' + displayName + ':', currentValue || '');

    if (newValue === null) return; // User cancelled

    try {
        const body = { serial: serial };
        body[fieldName] = newValue;

        const r = await fetch('/devices/update', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        });

        if (!r.ok) throw new Error('Update failed');

        // Update the display
        element.textContent = newValue;
        element.onclick = function () { editField(serial, fieldName, newValue, element); };
        window.__pm_shared.showToast(`${displayName} updated successfully`, 'success');

    } catch (e) {
        console.error('Update failed:', e);
        window.__pm_shared.showToast('Update failed: ' + e.message, 'error');
    }
}

// Delete all saved devices
async function deleteAllSavedDevices() {
    try {
        // Get list of saved devices
        const r = await fetch('/devices/list');
        if (!r.ok) throw new Error('Failed to fetch device list');
        const saved = await r.json();

        if (!Array.isArray(saved) || saved.length === 0) {
            window.__pm_shared.showToast('No saved devices to delete', 'info');
            return;
        }

        // Confirm deletion
        const confirmed = await window.__pm_shared.showConfirm(
            `Delete all ${saved.length} saved device(s)? This cannot be undone.`,
            'Delete All Devices',
            true
        );
        if (!confirmed) return;

        // Delete each device
        let deleted = 0;
        let failed = 0;
        for (const item of saved) {
            const serial = item.serial || (item.info && item.info.serial) || (item.printer_info && item.printer_info.serial);
            if (!serial) continue;

            try {
                const delResp = await fetch('/devices/delete', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ serial: serial }) });
                if (delResp.ok) {
                    deleted++;
                } else {
                    failed++;
                }
            } catch (e) {
                failed++;
                console.error('Failed to delete device ' + serial + ':', e);
            }
        }

        updatePrinters();
        console.log('Delete complete: ' + deleted + ' deleted, ' + failed + ' failed');
        if (deleted > 0) {
            window.__pm_shared.showToast(`Deleted ${deleted} device${deleted !== 1 ? 's' : ''} successfully`, 'success');
        }
        if (failed > 0) {
            window.__pm_shared.showToast(`Failed to delete ${failed} device${failed !== 1 ? 's' : ''}`, 'error');
        }
    } catch (e) {
    console.error('Delete all failed:', e);
    window.__pm_shared.showToast('Delete all failed: ' + e.message, 'error');
    }
}

// Store all log entries for client-side filtering
let allLogEntries = [];

// Parse and colorize a log line
function colorizeLogLine(line) {
    const levelColors = {
        'ERROR': '#ef4444',
        'WARN': '#f59e0b',
        'INFO': '#93a1a1',
        'DEBUG': '#6b7280',
        'TRACE': '#4b5563'
    };
    
    // Match log format: timestamp [LEVEL] message
    const match = line.match(/^(\S+)\s+\[(\w+)\]\s+(.*)$/);
    if (match) {
        const [, timestamp, level, message] = match;
        const color = levelColors[level] || '#93a1a1';
        return `<span style="color:#586e75">${timestamp}</span> <span style="color:${color};font-weight:bold">[${level}]</span> <span style="color:${color}">${message}</span>`;
    }
    return line;
}

// Filter and display logs based on current filters
function filterAndDisplayLogs() {
    const logEl = document.getElementById('log');
    if (!logEl) return;
    
    const levelFilter = document.getElementById('log_level_filter')?.value || '';
    const searchFilter = document.getElementById('log_search_filter')?.value.toLowerCase() || '';
    
    let filtered = allLogEntries;
    
    // Filter by level
    if (levelFilter) {
        filtered = filtered.filter(line => line.includes(`[${levelFilter}]`));
    }
    
    // Filter by search text
    if (searchFilter) {
        filtered = filtered.filter(line => line.toLowerCase().includes(searchFilter));
    }
    
    // Colorize and display
    const html = filtered.map(line => colorizeLogLine(line)).join('\n');
    logEl.innerHTML = html || '<span style="color:#586e75">(no matching entries)</span>';
    
    // Auto-scroll to bottom if not paused
    const pauseCheckbox = document.getElementById('pause_autoscroll');
    if (!pauseCheckbox || !pauseCheckbox.checked) {
        logEl.scrollTop = logEl.scrollHeight;
    }
}

// Load initial log contents once (SSE will stream new entries in real-time)
function updateLog() {
    fetch('/logs').then(r => r.text()).then(t => {
        const lines = t.split('\n').filter(line => line.trim());
        allLogEntries = lines;
        filterAndDisplayLogs();
    });
}

async function copyLogs() {
    try {
        const text = await fetch('/logs').then(r => r.text());
        await copyToClipboard(text, null, 'All logs copied to clipboard');
    } catch (e) {
        console.error('Copy logs failed:', e);
        window.__pm_shared.showToast('Failed to copy logs: ' + e.message, 'error');
    }
}

async function clearLogs() {
    try {
        const confirmed = await window.__pm_shared.showConfirm(
            'Clear logs? This will rotate the current log file and start fresh.',
            'Clear Logs'
        );
        if (!confirmed) return;
        
        const resp = await fetch('/logs/clear', { method: 'POST' });
        if (!resp.ok) {
            const text = await resp.text();
            window.__pm_shared.showToast('Clear logs failed: ' + text, 'error');
            return;
        }
        
        // Clear the display and array
        allLogEntries = [];
        const logEl = document.getElementById('log');
        if (logEl) {
            logEl.innerHTML = '<span style="color:#586e75">(logs cleared - waiting for new entries)</span>';
        }
        
    window.__pm_shared.showToast('Logs cleared and rotated', 'success');
    } catch (e) {
        console.error('Clear logs failed:', e);
        window.__pm_shared.showToast('Failed to clear logs: ' + e.message, 'error');
    }
}

async function downloadLogs() {
    try {
        const resp = await fetch('/logs/archive');
        if (!resp.ok) {
            const t = await resp.text();
            window.__pm_shared.showToast('Download failed: ' + t, 'error');
            return;
        }
        const blob = await resp.blob();
        let filename = 'logs.zip';
        const cd = resp.headers.get('Content-Disposition') || '';
        const m = /filename="?([^";]+)"?/i.exec(cd);
        if (m && m[1]) filename = m[1];
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url; a.download = filename; document.body.appendChild(a); a.click(); a.remove();
        URL.revokeObjectURL(url);
    window.__pm_shared.showToast('Log archive downloaded', 'success');
    } catch (e) {
        console.error('Download failed:', e);
        window.__pm_shared.showToast('Failed to download logs: ' + e.message, 'error');
    }
}

function updateMetrics() {
    fetch('/scan_metrics').then(r => r.ok ? r.json() : Promise.resolve(null)).then(m => {
        const el = document.getElementById('metrics_text');
        if (!el) return;
        let txt = '';
        if (m) txt += 'hosts=' + (m.total_hosts_scanned || 0) + ' printers=' + (m.detected_printers || 0) + ' missing_mfg=' + (m.missing_manufacturer || 0);
        el.textContent = txt;
    }).catch(() => { })
}

// Shared UI log appender. If elId omitted, appends to global '#log'.
function appendUiLog(msg, elId) {
    try {
        const id = elId || 'log';
        const el = document.getElementById(id);
        if (!el) return;
        // determine whether user is near bottom
        const atBottom = (el.scrollHeight - el.scrollTop - el.clientHeight) < 50;
        el.textContent += new Date().toISOString() + ' ' + msg + '\n';
        // if pause checkbox checked, do not auto-scroll
        const paused = document.getElementById('pause_autoscroll');
        if (paused && paused.checked) return;
        if (atBottom) {
            el.scrollTop = el.scrollHeight;
        }
    } catch (e) { /* ignore UI errors */ }
}
function loadSavedRanges() {
    fetch('/settings').then(r => r.ok ? r.json() : Promise.resolve(null)).then(d => {
        if (!d) return;
        const txt = (d.discovery && d.discovery.ranges_text) ? d.discovery.ranges_text : '';
        document.getElementById('ranges_text').value = txt;
        // On load, estimate expansion and warn if too large
        try {
            const cnt = estimateRangeCount(txt);
            const MAX_ADDRS = 10000;
            if (cnt > MAX_ADDRS) {
                window.__pm_shared.showToast(`Saved ranges expand to ${cnt} addresses which exceeds the allowed maximum of ${MAX_ADDRS}. Manual IP scanning may be disabled. Reduce ranges or enable passive discovery.`, 'error', 8000);
            }
        } catch (e) {
            // Non-fatal - ignore parse failures here
        }
    })
}

function saveRanges() {
    let txt = document.getElementById('ranges_text').value;
    // Client-side guard: prevent saving ranges that expand beyond safe threshold
    try {
        const cnt = estimateRangeCount(txt);
        const MAX_ADDRS = 10000; // keep consistent with server-side policy
        if (cnt > MAX_ADDRS) {
            window.__pm_shared.showToast(`Cannot save ranges: expansion would produce ${cnt} addresses (over max ${MAX_ADDRS})`, 'error', 6000);
            return;
        }
    } catch (e) {
        // If estimation fails, proceed and let server validate
    }
    fetch('/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ discovery: { ranges_text: txt } }) })
        .then(async r => {
                if (!r.ok) { 
                let t = await r.text(); 
                window.__pm_shared.showToast('Save failed: ' + t, 'error');
                return; 
            }
            window.__pm_shared.showToast('Ranges saved', 'success');
        })
        .catch(e => {
            window.__pm_shared.showToast('Save failed: ' + e.message, 'error');
        })
}

// Estimate number of IP addresses represented by the ranges text.
// Conservative: returns a large number (>MAX) if parsing uncertain.
function estimateRangeCount(text) {
    if (!text || !text.trim()) return 0;
    const lines = text.split('\n');
    let total = 0;
    for (let raw of lines) {
        const s = raw.trim();
        if (!s || s.startsWith('#')) continue;

        // CIDR
        const cidrMatch = s.match(/^(\d{1,3}(?:\.\d{1,3}){3})\/(\d{1,2})$/);
        if (cidrMatch) {
            const prefix = parseInt(cidrMatch[2], 10);
            if (isNaN(prefix) || prefix < 0 || prefix > 32) throw new Error('invalid CIDR');
            const hostBits = 32 - prefix;
            if (hostBits >= 31) return Number.MAX_SAFE_INTEGER;
            const cnt = Math.pow(2, hostBits);
            total += cnt;
            continue;
        }

        // Wildcard like 192.168.1.x or 192.168.1.*
        if (s.endsWith('.x') || s.endsWith('.*')) {
            total += 256;
            continue;
        }

        // Dash range
        if (s.includes('-')) {
            const parts = s.split('-').map(p => p.trim());
            const left = parts[0];
            const right = parts[1];
            // If right is full IP
            if (/^\d{1,3}(?:\.\d{1,3}){3}$/.test(right)) {
                const start = ipToUint32_js(left);
                const end = ipToUint32_js(right);
                if (end < start) throw new Error('end before start');
                total += (end - start + 1);
                continue;
            }
            // shorthand: right supplies last octet(s) or single number
            const lparts = left.split('.');
            const rparts = right.split('.');
            if (lparts.length !== 4) throw new Error('invalid left ip');
            // build end IP
            let endParts = [];
            if (rparts.length >= 1 && rparts.length <= 3) {
                const copy = 4 - rparts.length;
                for (let i = 0; i < copy; i++) endParts.push(lparts[i]);
                for (let rp of rparts) endParts.push(rp);
                const start = ipToUint32_js(left);
                const end = ipToUint32_js(endParts.join('.'));
                if (end < start) throw new Error('end before start');
                total += (end - start + 1);
                continue;
            }
            // unknown format -> conservative
            return Number.MAX_SAFE_INTEGER;
        }

        // Single IP
        if (/^\d{1,3}(?:\.\d{1,3}){3}$/.test(s)) {
            total += 1;
            continue;
        }

        // Unrecognized -> conservative large
        return Number.MAX_SAFE_INTEGER;
    }
    return total;
}

// helper: parse IPv4 dotted quad to uint32 (JS)
function ipToUint32_js(ip) {
    const parts = ip.split('.').map(n => parseInt(n, 10));
    if (parts.length !== 4) throw new Error('invalid ip');
    let n = 0;
    for (let i = 0; i < 4; i++) {
        if (isNaN(parts[i]) || parts[i] < 0 || parts[i] > 255) throw new Error('invalid ip octet');
        n = (n << 8) + (parts[i] & 0xFF);
    }
    return n >>> 0;
}

function clearRanges() {
    fetch('/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ discovery: { ranges_text: '' } }) })
        .then(() => loadSavedRanges())
}

// Update the detected subnet display in settings
function updateSubnetDisplay() {
    fetch('/settings').then(r => r.ok ? r.json() : Promise.resolve(null)).then(d => {
        const subnet = (d && d.discovery && d.discovery.detected_subnet) ? d.discovery.detected_subnet : '(unable to detect)';
        const subnetEl = document.getElementById('detected_subnet_display');
        if (subnetEl) subnetEl.textContent = subnet;
        // If detected subnet is a CIDR and expands beyond MAX_ADDRS, disable the subnet scan toggle
        try {
            const MAX_ADDRS = 10000;
            if (subnet && subnet.includes('/') ) {
                const cnt = estimateRangeCount(subnet);
                const scanEl = document.getElementById('scan_local_subnet_enabled');
                if (cnt > MAX_ADDRS) {
                    if (scanEl) {
                        scanEl.checked = false;
                        scanEl.disabled = true;
                    }
                    // Persistently turn off subnet scanning so server/agent also respects it
                    try {
                        fetch('/settings', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ discovery: { subnet_scan: false } })
                        }).then(r => {
                            if (!r.ok) {
                                console.warn('Failed to persist subnet_scan=false');
                            }
                        }).catch(e => console.warn('Persist subnet_scan failed', e));
                    } catch (e) {
                        console.warn('Persist subnet_scan failed', e);
                    }

                    // Update inline reason text so users see why the control is disabled
                    try {
                        const reasonEl = document.getElementById('detected_subnet_reason');
                        if (reasonEl) {
                            reasonEl.textContent = `Disabled: ${subnet} expands to ${cnt} addresses (> ${MAX_ADDRS}). Subnet scanning has been turned off to avoid large scans. Use manual ranges or enable passive discovery.`;
                            reasonEl.style.display = 'block';
                        }
                    } catch (e) {
                        // ignore
                    }

                    try {
                        // If user previously chose "Don't show this again" skip alert
                        if (!localStorage.getItem('hideConfigWarning')) {
                            // showAlert(message, title, isDangerous, showDontRemindCheckbox)
                            window.__pm_shared.showAlert(
                                `Detected subnet ${subnet} expands to ${cnt} addresses (over ${MAX_ADDRS}). Subnet scanning has been disabled to avoid excessive scans. It has been turned off and cannot be re-enabled until you update ranges or enable manually. Use manual ranges or passive discovery.`,
                                'Subnet scanning disabled',
                                true,
                                true
                            );
                        }
                    } catch (e) { try { console.warn('alert failed', e); } catch(_){} }
                } else {
                    if (scanEl) {
                        // ensure it's enabled (but don't override user's saved value)
                        scanEl.disabled = false;
                    }
                    try {
                        const reasonEl = document.getElementById('detected_subnet_reason');
                        if (reasonEl) { reasonEl.style.display = 'none'; reasonEl.textContent = ''; }
                    } catch (e) {}
                }
            }
        } catch (e) {
            // ignore parsing errors
        }
    })
}

// UI effects when Auto Discover is toggled (called after settings are saved)
function toggleAutoDiscoverUI() {
    // Hide/show Discover Now button based on Auto Discover state with animation
    const autoDiscoverEnabled = document.getElementById('auto_discover_checkbox')?.checked ?? false;
    const discoverBtn = document.getElementById('discover_now_btn');
    const showAnywayContainer = document.getElementById('show_discover_button_anyway_container');
    const showAnywayToggle = document.getElementById('show_discover_button_anyway');
    const showAnyway = showAnywayToggle?.checked ?? false;

    if (discoverBtn) {
        // Logic: Show button when auto discover is OFF, OR when "Show Anyway" is enabled
        const shouldShowButton = !autoDiscoverEnabled || (autoDiscoverEnabled && showAnyway);
        
        if (shouldShowButton) {
            // Show and fade in
            discoverBtn.style.display = 'inline-block';
            discoverBtn.style.opacity = '0';
            discoverBtn.style.transform = 'scale(0.95)';
            setTimeout(() => {
                discoverBtn.style.transition = 'opacity 0.3s ease, transform 0.3s ease';
                discoverBtn.style.opacity = '1';
                discoverBtn.style.transform = 'scale(1)';
            }, 10);
        } else {
            // Fade out and hide (only when auto discover is ON and "Show Anyway" is OFF)
            discoverBtn.style.transition = 'opacity 0.3s ease, transform 0.3s ease';
            discoverBtn.style.opacity = '0';
            discoverBtn.style.transform = 'scale(0.95)';
            setTimeout(() => {
                discoverBtn.style.display = 'none';
            }, 300);
        }
    }

    // Show/hide the "Show Discover Button Anyway" toggle container
    if (showAnywayContainer) {
        showAnywayContainer.style.display = autoDiscoverEnabled ? 'flex' : 'none';
    }
}



// Show/hide the ranges configuration dropdown based on manual_ranges setting
function toggleRangesDropdown() {
    const enabled = document.getElementById('manual_ranges_enabled')?.checked ?? true;
    const rangesEl = document.getElementById('ranges_text');
    if (!rangesEl) return;
    // Find the nearest details container (our Advanced Active Probes details) or parent
    const container = rangesEl.closest('details') || rangesEl.parentElement;
    if (!container) return;

    if (enabled) {
        container.style.display = '';
        try { container.open = true; } catch (e) { /* ignore */ }
    } else {
        try { container.open = false; } catch (e) { /* ignore */ }
        container.style.display = 'none';
    }
}

// Toggle top-level IP scanning UI. Hides/shows elements that should be disabled when IP scanning is off.
function toggleIPScanningUI() {
    const enabled = document.getElementById('ip_scanning_enabled')?.checked ?? true;
    document.querySelectorAll('.ipscan-subtoggle').forEach(el => {
        el.style.display = enabled ? '' : 'none';
    });
    // Ensure ranges visibility respects both parent and manual_ranges checkbox
    toggleRangesDropdown();
}

// Ranges are now displayed in ranges_text textarea via loadSavedRanges() function

// if advanced manufacturer selector exists, wire its change handler (guarded)
(function () { const m = document.getElementById('mib_mfg'); if (!m) return; m.addEventListener('change', function (e) { const v = e.target.value; const custom = document.getElementById('mib_custom'); if (custom) { custom.style.display = (v === 'custom') ? 'inline-block' : 'none'; } }); })();

// DEPRECATED: Manual MIB walk functionality is superseded by automated discovery pipeline
// See: docs/DEPRECATED_PENDING_REMOVAL.md
// TODO: Remove this function and associated UI elements after verification
function runMibWalk() {
    // Simplified single-IP manual walk: only IP, community and version are required.
    const ipEl = document.getElementById('mib_ip');
    if (!ipEl) { window.__pm_shared.showToast('Manual walk UI missing', 'error'); return; }
    const ip = ipEl.value.trim();
    if (!ip) { window.__pm_shared.showToast('Enter target IP', 'error'); return; }
    const community = (document.getElementById('mib_community') || {}).value ? document.getElementById('mib_community').value.trim() : '';
    const version = (document.getElementById('mib_version') || {}).value || 'v2c';
    const body = { ip: ip, community: community, version: version, max_entries: 2000 };
    const meta = document.getElementById('mib_results_meta');
    const tbody = document.getElementById('mib_results_tbody');
    if (meta) { meta.style.display = 'block'; meta.textContent = 'Running MIB walk...'; }
    if (tbody) tbody.innerHTML = '';
    fetch('/mib_walk', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
        .then(async r => {
            if (!r.ok) { const t = await r.text(); if (meta) meta.textContent = 'Error: ' + t; return; }
            return r.json();
        })
        .then(j => { if (!j) return; renderMibResults(j); }).catch(e => { if (meta) meta.textContent = 'Request failed: ' + e; });
}

// DEPRECATED: Manual MIB walk functionality is superseded by automated discovery pipeline
// See: docs/DEPRECATED_PENDING_REMOVAL.md
// TODO: Remove this function and associated UI elements after verification
// Run a MIB walk for a specific discovered printer IP and show results in the MIB Walk panel
function runMibWalkFor(ip) {
    if (!ip) return;
    // switch to Devices tab so results are visible
    showTab('devices');
    const meta = document.getElementById('mib_results_meta');
    const tbody = document.getElementById('mib_results_tbody');
    if (meta) { meta.style.display = 'block'; meta.textContent = 'Running MIB walk for ' + ip + '...'; }
    if (tbody) tbody.innerHTML = '';
    // Use simplified single-IP walk parameters (default max_entries)
    const community = (document.getElementById('mib_community') || {}).value ? document.getElementById('mib_community').value.trim() : '';
    const version = (document.getElementById('mib_version') || {}).value || 'v2c';
    const body = { ip: ip, community: community, version: version, max_entries: 2000 };
    fetch('/mib_walk', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
        .then(async r => {
            if (!r.ok) { const t = await r.text(); if (meta) meta.textContent = 'Error: ' + t; return; }
            return r.json();
        })
        .then(j => { if (!j) return; renderMibResults(j); }).catch(e => { if (meta) meta.textContent = 'Request failed: ' + e; });
}

function viewDiag(ip) {
    if (!ip) return;
    // ensure the Devices tab is visible so diagnostics are visible
    showTab('devices');
    const pre = document.getElementById('mib_results');
    pre.textContent = 'Loading diagnostics for ' + ip + '...';
    fetch('/parse_debug?ip=' + encodeURIComponent(ip)).then(async r => {
        if (!r.ok) { const t = await r.text(); pre.textContent = 'No diagnostics: ' + t; return; }
        const j = await r.json();
        pre.textContent = JSON.stringify(j, null, 2);
    }).catch(e => { pre.textContent = 'Request failed: ' + e; });
}

// Show printer details modal with printer data already loaded
// source: 'discovered' or 'saved' to control title and action buttons
function showPrinterDetailsData(p, source, parseDebug) {
    if (!p) return;
    source = source || 'discovered';
    const bodyEl = document.getElementById('printer_details_body');
    const overlay = document.getElementById('printer_details_overlay');
    const titleEl = document.getElementById('printer_details_title');
    const actionsEl = document.getElementById('printer_details_actions');

    titleEl.textContent = (source === 'saved') ? 'Saved Device' : 'Discovered Device';
    
    // Store the current printer IP to prevent glitchy updates from live data
    overlay.dataset.currentPrinterIp = p.ip || p.IP || '';
    
    // Show modal overlay and prevent body scroll
    overlay.style.display = 'flex';
    document.body.style.overflow = 'hidden';

    // Render card with consistent styling
    function renderInfoCard(title, content) {
        if (!content) return '';
        return '<div class="panel">' +
            '<h4 style="margin-top:0;color:var(--highlight)">' + title + '</h4>' +
            content +
            '</div>';
    }
    
    // Render grid row (label: value)
    function renderRow(label, value) {
        if (!value) return '';
        return '<div style="display:grid;grid-template-columns:auto 1fr;gap:4px 12px;font-size:13px;padding:4px 0">' +
            '<div style="color:var(--muted);white-space:nowrap">' + label + ':</div>' +
            '<div style="word-break:break-word">' + value + '</div>' +
            '</div>';
    }
    
    let html = '<div class="settings-grid" style="margin-top:0">';

    // Debug: show if printer object is empty
    if (!p || Object.keys(p).length === 0) {
        html += '<div style="color:var(--muted);padding:12px">No printer data available</div>';
        bodyEl.innerHTML = html + '</div>';
        return;
    }

    // Editable device fields with lock toggles
    const isLocked = (field) => Array.isArray(p.locked_fields) && p.locked_fields.some(f => (f.field || f.Field || '').toLowerCase() === field);

    const renderEditableRow = (label, field, value, opts = { type: 'text', readonly: false, placeholder: '' }) => {
        const locked = isLocked(field);
        const inputId = 'field_' + field;
        const safeVal = (value === undefined || value === null) ? '' : value;
        const isReadonly = opts.readonly || locked;
        const disabledStyle = locked ? 'background:var(--panel);color:var(--text);border-color:var(--border);cursor:not-allowed;opacity:0.8;' : '';

        let row = '<div style="display:grid;grid-template-columns:auto 1fr auto;gap:4px 8px;align-items:center;padding:4px 0" data-field-row="' + field + '">';
        row += '<div style="color:var(--muted)">' + label + ':</div>';
        if (opts.type === 'textarea') {
            row += '<textarea id="' + inputId + '" ' + (isReadonly ? 'readonly' : '') + ' ' + (locked ? 'disabled' : '') + ' placeholder="' + (opts.placeholder || '') + '" style="min-height:56px;' + disabledStyle + '">' + safeVal + '</textarea>';
        } else {
            row += '<input id="' + inputId + '" type="' + opts.type + '" ' + (isReadonly ? 'readonly' : '') + ' ' + (locked ? 'disabled' : '') + ' value="' + safeVal + '" placeholder="' + (opts.placeholder || '') + '" style="' + disabledStyle + '">';
        }
        row += '<button class="lock-btn' + (locked ? ' locked' : '') + '" data-field="' + field + '" title="' + (locked ? 'Unlock field' : 'Lock field') + '"' + (opts.readonly ? ' style="visibility:hidden"' : '') + '></button>';
        row += '</div>';
        return row;
    };

    // Capabilities Card
    // Use the canonical shared implementation. The shared loader is injected
    // synchronously, so callers can reference the namespaced API directly.
    const capabilitiesHTML = window.__pm_shared_cards.renderCapabilities(p);
    if (capabilitiesHTML) {
        html += renderInfoCard('Capabilities', capabilitiesHTML);
    }
    
    // Device Info (editable)
    let deviceInfo = '<div style="display:flex;flex-direction:column;gap:6px">';
    deviceInfo += renderEditableRow('Manufacturer', 'manufacturer', p.manufacturer);
    deviceInfo += renderEditableRow('Model', 'model', p.model);
    deviceInfo += renderEditableRow('Serial', 'serial', p.serial, { type: 'text', readonly: true });
    deviceInfo += renderEditableRow('Firmware', 'firmware', p.firmware);
    deviceInfo += renderEditableRow('Asset Number', 'asset_number', p.asset_number);
    deviceInfo += renderEditableRow('Location', 'location', p.location);
    deviceInfo += renderEditableRow('Description', 'description', p.description, { type: 'textarea' });
    // Web UI with proxy buttons
    const webUILocked = isLocked('web_ui_url');
    const webUIVal = (p.web_ui_url === undefined || p.web_ui_url === null) ? '' : p.web_ui_url;
    const webUIDisabledStyle = webUILocked ? 'background:var(--panel);color:var(--text);border-color:var(--border);cursor:not-allowed;opacity:0.8;' : '';
    deviceInfo += '<div style="display:grid;grid-template-columns:auto 1fr auto;gap:4px 8px;align-items:center" data-field-row="web_ui_url">';
    deviceInfo += '<div style="color:var(--muted)">Web UI:</div>';
    deviceInfo += '<div style="display:flex;gap:4px;align-items:center">';
    deviceInfo += '<input id="field_web_ui_url" type="text" value="' + webUIVal + '" ' + (webUILocked ? 'disabled' : '') + ' style="flex:1;' + webUIDisabledStyle + '">';
    if (webUIVal) {
        deviceInfo += '<button style="font-size:11px;padding:2px 6px" data-action="open-direct" data-webui-url="' + webUIVal + '">Direct</button>';
        deviceInfo += '<button style="font-size:11px;padding:2px 6px;background:#268bd2;color:#fff" data-action="open-proxy" data-serial="' + (p.serial || '') + '">Proxy</button>';
    }
    deviceInfo += '</div>';
    deviceInfo += '<button class="lock-btn' + (webUILocked ? ' locked' : '') + '" data-field="web_ui_url" title="' + (webUILocked ? 'Unlock field' : 'Lock field') + '"></button>';
    deviceInfo += '</div>';
    if (p.last_seen) {
        deviceInfo += '<div style="color:var(--muted);font-size:12px;margin-top:6px">Last Seen: ' + new Date(p.last_seen).toLocaleString() + '</div>';
    }
    if (p.first_seen) {
        deviceInfo += '<div style="color:var(--muted);font-size:12px">First Seen: ' + new Date(p.first_seen).toLocaleString() + '</div>';
    }
    deviceInfo += '</div>';
    html += renderInfoCard('Device Info', deviceInfo);

    // Metrics card for saved devices (compact summary + quick-open buttons)
    if (source === 'saved' && p.serial) {
        const metricsHtml = '<div id="printer_metrics_summary" style="margin-top:8px"></div>' +
            '<div style="display:flex;gap:8px;align-items:center;margin-top:8px">' +
            '<button class="primary" data-action="metrics" data-serial="' + p.serial + '">Open Metrics</button>' +
            '<button style="font-size:12px;padding:6px" data-action="metrics" data-serial="' + p.serial + '" data-preset="7day">Open Last 7 Days</button>' +
            '</div>';
        html += renderInfoCard('Metrics', metricsHtml);
    }

    // Network Info Card
    let networkInfo = '<div style="display:flex;flex-direction:column;gap:6px">';
    networkInfo += renderEditableRow('IP Address', 'ip', p.ip);
    networkInfo += '<div style="display:grid;grid-template-columns:auto 1fr;gap:4px 8px;align-items:center;padding:4px 0">' +
        '<div style="color:var(--muted)">MAC Address:</div>' +
        '<div style="font-family:monospace">' + (p.mac_address || '') + '</div></div>';
    networkInfo += renderEditableRow('Hostname', 'hostname', p.hostname);
    networkInfo += renderEditableRow('Subnet Mask', 'subnet_mask', p.subnet_mask);
    networkInfo += renderEditableRow('Gateway', 'gateway', p.gateway);
    networkInfo += renderEditableRow('DNS Servers', 'dns_servers', (p.dns_servers || []).join(', '), { placeholder: 'comma separated' });
    networkInfo += renderEditableRow('DHCP Server', 'dhcp_server', p.dhcp_server);
    networkInfo += '</div>';
    html += renderInfoCard('Network', networkInfo);

    // Web UI Credentials Card (for proxy auto-login)
    // Determine default username based on manufacturer
    const mfg = (p.manufacturer || '').toLowerCase();
    let defaultUser = 'admin';
    let passwordHint = 'Serial Number';
    if (mfg.includes('kyocera')) {
        defaultUser = 'Admin'; // Capital A for Kyocera
    } else if (mfg.includes('epson')) {
        defaultUser = 'EPSON';
        passwordHint = 'Serial Number (default)';
    } else if (mfg.includes('hp')) {
        passwordHint = 'Blank (default) or set password';
    }

    // Only render credentials card if feature is enabled in settings
    if (globalSettings.security.credentials_enabled !== false) {
        let credsInfo = '<div style="color:var(--muted);font-size:12px;margin-bottom:8px">For automatic proxy login. Password is encrypted at rest.</div>';
        credsInfo += '<div style="display:grid;gap:6px">';
        credsInfo += '<div style="display:grid;grid-template-columns:auto 1fr;gap:4px 8px;align-items:center">';
        credsInfo += '<div style="color:var(--muted)">Username:</div>';
        credsInfo += '<input id="cred_username" type="text" placeholder="' + defaultUser + '" style="width:100%">';
        credsInfo += '</div>';
        credsInfo += '<div style="display:grid;grid-template-columns:auto 1fr;gap:4px 8px;align-items:center">';
        credsInfo += '<div style="color:var(--muted)">Password:</div>';
        credsInfo += '<input id="cred_password" type="password" placeholder="' + passwordHint + '" style="width:100%">';
        credsInfo += '</div>';
        credsInfo += '<div style="display:grid;grid-template-columns:auto 1fr;gap:4px 8px;align-items:center">';
        credsInfo += '<div style="color:var(--muted)">Auth Type:</div>';
        credsInfo += '<select id="cred_auth_type" style="width:100%"><option value="basic">HTTP Basic</option><option value="form">Form Login</option></select>';
        credsInfo += '</div>';
        credsInfo += '<div style="display:flex;align-items:center;gap:8px">';
        credsInfo += '<input type="checkbox" id="cred_auto_login">';
        credsInfo += '<label for="cred_auto_login" style="color:var(--text);cursor:pointer">Enable auto-login when using proxy</label>';
        credsInfo += '</div>';
        credsInfo += '<div style="display:flex;gap:8px;margin-top:4px">';
        credsInfo += '<button id="save_creds_btn" style="flex:1">Save Credentials</button>';
        credsInfo += '<span id="creds_status" style="color:var(--muted);align-self:center;font-size:12px"></span>';
        credsInfo += '</div>';
        credsInfo += '</div>';
        html += renderInfoCard('Web UI Credentials (optional)', credsInfo);
    }

    // Page Counters Card - show all available metrics
    const rawData = p.raw_data || {};
    const metersData = rawData.meters || p.meters || {};
    const counterItems = [];

    // Add main counters
    if (p.page_count) counterItems.push({ label: 'Total Pages', value: p.page_count });
    if (p.mono_impressions || rawData.mono_impressions) counterItems.push({ label: 'Mono Impressions', value: p.mono_impressions || rawData.mono_impressions });
    if (p.color_impressions || rawData.color_impressions) counterItems.push({ label: 'Color Impressions', value: p.color_impressions || rawData.color_impressions });

    // Add per-color counters if available
    if (rawData.black_impressions) counterItems.push({ label: 'Black', value: rawData.black_impressions });
    if (rawData.cyan_impressions) counterItems.push({ label: 'Cyan', value: rawData.cyan_impressions });
    if (rawData.magenta_impressions) counterItems.push({ label: 'Magenta', value: rawData.magenta_impressions });
    if (rawData.yellow_impressions) counterItems.push({ label: 'Yellow', value: rawData.yellow_impressions });

    // Add category meters
    if (metersData.scan_pages) counterItems.push({ label: 'Scan Pages', value: metersData.scan_pages });
    if (metersData.copier_pages) counterItems.push({ label: 'Copier Pages', value: metersData.copier_pages });
    if (metersData.fax_pages) counterItems.push({ label: 'Fax Pages', value: metersData.fax_pages });
    if (metersData.printer_pages) counterItems.push({ label: 'Printer Pages', value: metersData.printer_pages });

    if (counterItems.length > 0) {
        let countersContent = '<div style="display:flex;flex-direction:column;gap:2px">';
        counterItems.forEach(item => {
            countersContent += renderRow(item.label, item.value);
        });
        countersContent += '</div>';
        html += renderInfoCard('Page Counters', countersContent);
    }

    // Additional device properties
    const additionalItems = [];
    if (rawData.uptime_seconds) {
        const days = Math.floor(rawData.uptime_seconds / 86400);
        const hours = Math.floor((rawData.uptime_seconds % 86400) / 3600);
        additionalItems.push({ label: 'Uptime', value: days + 'd ' + hours + 'h' });
    }
    if (rawData.duplex_supported) additionalItems.push({ label: 'Duplex', value: 'Supported' });
    if (rawData.admin_contact) additionalItems.push({ label: 'Admin Contact', value: rawData.admin_contact });
    if (rawData.asset_id) additionalItems.push({ label: 'Asset ID', value: rawData.asset_id });

    if (additionalItems.length > 0) {
        let additionalContent = '<div style="display:flex;flex-direction:column;gap:2px">';
        additionalItems.forEach(item => {
            additionalContent += renderRow(item.label, item.value);
        });
        additionalContent += '</div>';
        html += renderInfoCard('Additional Info', additionalContent);
    }

    // Metrics History Section moved into Device Info card for saved devices

    // Unified Consumables section - combines toner levels, waste toner, maintenance boxes, etc.
    function renderConsumable(name, value, isLevel) {
        const nameLower = name.toLowerCase();

        // If it's a numeric level (0-100), show as progress bar
        if (isLevel) {
            const v = Number(value);
            const pct = isNaN(v) ? '' : Math.max(0, Math.min(100, v));

            // Determine color based on supply type and level
            let color = '#6c6';
            let icon = '';

            // Toner colors
            if (nameLower.includes('black') || nameLower === 'k') { color = '#111'; icon = '●'; }
            else if (nameLower.includes('cyan') || nameLower === 'c') { color = '#0097a7'; icon = '●'; }
            else if (nameLower.includes('magenta') || nameLower === 'm') { color = '#c2185b'; icon = '●'; }
            else if (nameLower.includes('yellow') || nameLower === 'y') { color = '#fbc02d'; icon = '●'; }
            // Waste/Maintenance items (reverse logic - high is bad)
            else if (nameLower.includes('waste') || nameLower.includes('maintenance')) {
                icon = '⚠';
                if (pct === '') color = '#888';
                else if (pct > 80) color = '#d32f2f';
                else if (pct > 50) color = '#f57c00';
                else color = '#388e3c';
            }
            // Other supplies (low is bad)
            else {
                icon = '▮';
                if (pct === '') color = '#888';
                else if (pct < 20) color = '#d32f2f';
                else if (pct < 50) color = '#f57c00';
                else color = '#388e3c';
            }

            const pctTextColor = (nameLower.includes('yellow') ? '#000' : '#fff');
            let html = '<div style="margin-top:6px">';
            html += '<div style="font-size:13px;font-weight:600;color:var(--text);margin-bottom:4px">' + icon + ' ' + name + '</div>';
            html += '<div style="background:#001f22;border:1px solid rgba(255,255,255,0.06);padding:6px;border-radius:8px;max-width:100%;width:100%;position:relative">';
            html += '<div style="width:' + pct + '%;background:' + color + ';height:18px;border-radius:6px;box-shadow:inset 0 -2px 4px rgba(0,0,0,0.4)"></div>';
            html += '<div style="position:absolute;left:8px;top:2px;font-size:12px;color:' + pctTextColor + '">' + (pct !== '' ? pct + '%' : 'n/a') + '</div>';
            html += '</div></div>';
            return html;
        } else {
            // Text description (e.g., part numbers, status messages)
            return '<div style="margin-top:6px;display:grid;grid-template-columns:auto 1fr;gap:4px 12px;font-size:13px"><div style="color:var(--muted)">' + name + ':</div><div>' + value + '</div></div>';
        }
    }

    // Consumables section - show all supply levels (toner, waste, maintenance, etc.)
    const tonerLevels = p.toner_levels;

    if (tonerLevels && Object.keys(tonerLevels).length > 0) {
        html += '<div style="background:rgba(0,0,0,0.2);border:1px solid rgba(255,255,255,0.05);border-radius:6px;padding:10px;margin-bottom:8px">';
        html += '<div style="font-weight:600;color:var(--highlight);margin-bottom:8px;font-size:14px">Consumables</div>';

        // Render all consumable levels with progress bars
        for (let k in tonerLevels) {
            html += renderConsumable(k, tonerLevels[k], true);
        }

        html += '</div>';
    }

    // Interfaces: attempt to extract from parseDebug.RawPDUs (IF-MIB columns)
    if (parseDebug && Array.isArray(parseDebug.raw_pdus)) {
        const ifs = {};
        parseDebug.raw_pdus.forEach(rp => {
            const oid = rp.oid || rp.OID || '';
            if (oid.startsWith('1.3.6.1.2.1.2.2.1.')) {
                // column form: ...1.3.6.1.2.1.2.2.1.<col>.<ifIndex>
                const parts = oid.split('.');
                const col = parts[parts.length - 2];
                const idx = parts[parts.length - 1];
                if (!ifs[idx]) ifs[idx] = { index: idx };
                const v = rp.str_value || rp.StrValue || '';
                switch (col) {
                    case '2': ifs[idx].descr = v; break; // ifDescr
                    case '3': ifs[idx].type = v; break; // ifType
                    case '5': ifs[idx].speed = v; break; // ifSpeed
                    case '6': ifs[idx].mac = v; break; // ifPhysAddress
                }
            }
        });
        const keys = Object.keys(ifs);
        if (keys.length > 0) {
            html += '<details style="background:rgba(0,0,0,0.2);border:1px solid rgba(255,255,255,0.05);border-radius:6px;padding:10px;margin-bottom:8px">';
            html += '<summary style="font-weight:600;color:var(--highlight);cursor:pointer;font-size:14px;margin-bottom:8px">Network Interfaces (' + keys.length + ')</summary>';
            keys.forEach(k => {
                const it = ifs[k];
                html += '<div style="background:rgba(0,0,0,0.15);border-radius:4px;padding:8px;margin-bottom:6px;font-size:13px">';
                html += '<div style="display:grid;grid-template-columns:auto 1fr;gap:4px 12px">';
                if (it.descr) html += '<div style="color:var(--muted)">Interface:</div><div>' + it.descr + '</div>';
                if (it.mac) html += '<div style="color:var(--muted)">MAC:</div><div style="font-family:monospace;font-size:12px">' + it.mac + '</div>';
                if (it.speed) html += '<div style="color:var(--muted)">Speed:</div><div>' + it.speed + '</div>';
                html += '</div></div>';
            });
            html += '</details>';
        }
    }

    // Paper trays - collapsible
    if (p.paper_tray_status && p.paper_tray_status.length) {
        html += '<details style="background:rgba(0,0,0,0.2);border:1px solid rgba(255,255,255,0.05);border-radius:6px;padding:10px;margin-bottom:8px">';
        html += '<summary style="font-weight:600;color:var(--highlight);cursor:pointer;font-size:14px">Paper Trays (' + p.paper_tray_status.length + ')</summary>';
        html += '<div style="margin-top:8px">';
        p.paper_tray_status.forEach(t => html += '<div style="padding:6px;background:rgba(0,0,0,0.15);border-radius:4px;margin-bottom:4px;font-size:13px">' + t + '</div>');
        html += '</div></details>';
    }

    // Status messages and alerts - collapsible
    if (p.status_messages && p.status_messages.length) {
        html += '<details style="background:rgba(0,0,0,0.2);border:1px solid rgba(255,255,255,0.05);border-radius:6px;padding:10px;margin-bottom:8px">';
        html += '<summary style="font-weight:600;color:var(--highlight);cursor:pointer;font-size:14px">Status Messages (' + p.status_messages.length + ')</summary>';
        html += '<div style="margin-top:8px">';
        p.status_messages.forEach(s => html += '<div style="padding:6px;background:rgba(0,0,0,0.15);border-radius:4px;margin-bottom:4px;font-size:13px">' + s + '</div>');
        html += '</div></details>';
    }

    // Parse debug link (fallback to constructed endpoint if not present)
    const dbgLink = p.parse_debug_path || ('/parse_debug?ip=' + encodeURIComponent(p.ip || ''));
    html += '<div><a href="' + dbgLink + '" target="_blank">View Parse Debug</a></div>';

    // Action buttons for refreshing device data and collecting metrics
    html += '</div>'; // Close settings-grid
    
    // Action buttons below the grid
    html += '<div style="margin-top:16px;display:flex;gap:8px;flex-wrap:wrap;padding:12px;background:rgba(0,0,0,0.1);border-radius:6px">';
    html += '<button id="refresh_data_btn">Refresh Details</button>';
    if (source === 'saved') {
        html += '<button id="collect_metrics_btn">Collect Metrics</button>';
    }
    html += '<span id="refresh_status" style="color:var(--muted);align-self:center"></span>';
    html += '</div>';

    bodyEl.innerHTML = html;

    // Populate small metrics summary into Device Info (if present)
    (async function populatePrinterMetricsSummary() {
        try {
            if (source !== 'saved' || !p || !p.serial) return;
            const summaryEl = document.getElementById('printer_metrics_summary');
            if (!summaryEl) return;
            summaryEl.textContent = 'Loading metrics summary...';

            const url = '/api/devices/metrics/history?serial=' + encodeURIComponent(p.serial) + '&period=7day';
            const res = await fetch(url);
            if (!res.ok) { summaryEl.innerHTML = '<div style="color:var(--muted)">No metrics data available</div>'; return; }
            const history = await res.json();
            if (!history || history.length === 0) { summaryEl.innerHTML = '<div style="color:var(--muted)">No metrics data available</div>'; return; }

            const latest = history[history.length - 1];
            const oldest = history[0];
            const durationMs = new Date(latest.timestamp).getTime() - new Date(oldest.timestamp).getTime();
            const durationDays = Math.max(1, durationMs / (24 * 60 * 60 * 1000));

            let statsHtml = '<table style="width:100%;border-collapse:collapse;margin-bottom:6px;font-size:12px">';
            statsHtml += '<thead><tr style="border-bottom:1px solid rgba(255,255,255,0.06)">';
            statsHtml += '<th style="text-align:left;padding:6px 8px;color:var(--highlight);font-weight:600">Metric</th>';
            statsHtml += '<th style="text-align:right;padding:6px 8px;color:var(--highlight);font-weight:600">Lifetime Total</th>';
            statsHtml += '<th style="text-align:right;padding:6px 8px;color:var(--highlight);font-weight:600">Period Diff</th>';
            statsHtml += '<th style="text-align:right;padding:6px 8px;color:var(--highlight);font-weight:600">Avg/Day</th>';
            statsHtml += '</tr></thead><tbody>';

            const lifetimePages = latest.page_count || 0;
            const periodPages = lifetimePages - (oldest.page_count || 0);
            const avgPages = (periodPages / durationDays).toFixed(1);
            statsHtml += '<tr style="border-bottom:1px solid rgba(255,255,255,0.03)">';
            statsHtml += '<td style="padding:6px 8px;color:var(--text)">Total Pages</td>';
            statsHtml += '<td style="padding:6px 8px;text-align:right;color:var(--text);font-weight:600">' + lifetimePages.toLocaleString() + '</td>';
            statsHtml += '<td style="padding:6px 8px;text-align:right;color:#268bd2;font-weight:600">' + periodPages.toLocaleString() + '</td>';
            statsHtml += '<td style="padding:6px 8px;text-align:right;color:var(--muted)">' + avgPages + '</td>';
            statsHtml += '</tr>';

            if (latest.mono_pages !== undefined || latest.mono_impressions !== undefined) {
                const lifetimeMono = latest.mono_pages || latest.mono_impressions || 0;
                const periodMono = lifetimeMono - (oldest.mono_pages || oldest.mono_impressions || 0);
                const avgMono = (periodMono / durationDays).toFixed(1);
                statsHtml += '<tr style="border-bottom:1px solid rgba(255,255,255,0.03)">';
                statsHtml += '<td style="padding:6px 8px;color:var(--text)">Mono Pages</td>';
                statsHtml += '<td style="padding:6px 8px;text-align:right;color:var(--text);font-weight:600">' + (lifetimeMono.toLocaleString ? lifetimeMono.toLocaleString() : lifetimeMono) + '</td>';
                statsHtml += '<td style="padding:6px 8px;text-align:right;color:#268bd2;font-weight:600">' + (periodMono.toLocaleString ? periodMono.toLocaleString() : periodMono) + '</td>';
                statsHtml += '<td style="padding:6px 8px;text-align:right;color:var(--muted)">' + avgMono + '</td>';
                statsHtml += '</tr>';
            }

            // Toner levels - show current levels if present
            if (latest.toner_levels && Object.keys(latest.toner_levels).length > 0) {
                for (const [color, level] of Object.entries(latest.toner_levels)) {
                    const levelNum = typeof level === 'number' ? level : parseInt(level) || 0;
                    const levelColor = levelNum < 20 ? '#d32f2f' : (levelNum < 50 ? '#f57c00' : '#388e3c');
                    statsHtml += '<tr style="border-bottom:1px solid rgba(255,255,255,0.03)">';
                    statsHtml += '<td style="padding:6px 8px;color:var(--text)">' + color + '</td>';
                    statsHtml += '<td style="padding:6px 8px;text-align:right;color:' + levelColor + ';font-weight:600">' + levelNum + '%</td>';
                    // Render a visual progress bar for the current toner level in the
                    // toner color. Keep the percentage in the Lifetime Total column
                    // (as above) and render the bar across the remaining two columns.
                    statsHtml += '<td style="padding:6px 8px;text-align:right;color:var(--muted)" colspan="2">';
                    statsHtml += '<div style="width:100%;background:rgba(255,255,255,0.03);border:1px solid rgba(255,255,255,0.03);padding:6px;border-radius:6px;">';
                    statsHtml += '<div role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow="' + levelNum + '" title="' + levelNum + '%" ' +
                        'style="height:12px;border-radius:6px;width:' + levelNum + '%;background:' + levelColor + ';box-shadow:inset 0 -2px 4px rgba(0,0,0,0.3)"></div>';
                    statsHtml += '</div>';
                    statsHtml += '</td>';
                    statsHtml += '</tr>';
                }
            }

            statsHtml += '</tbody></table>';
            summaryEl.innerHTML = statsHtml;
        } catch (err) {
            try { const summaryEl = document.getElementById('printer_metrics_summary'); if (summaryEl) summaryEl.innerHTML = '<div style="color:var(--muted)">Metrics unavailable</div>'; } catch(_){}
        }
    })();

    // Apply masonry layout to modal grid after a short delay for rendering
    setTimeout(() => {
        const modalGrid = bodyEl.querySelector('.settings-grid');
        if (modalGrid && typeof applyMasonryLayout === 'function') {
            applyMasonryLayout(modalGrid);
        }
    }, 50);

    // Wire up lock toggles
    bodyEl.addEventListener('click', async (e) => {
        const btn = e.target.closest('.lock-btn');
        if (!btn) return;
        const field = btn.getAttribute('data-field');
        const locked = btn.classList.contains('locked');

        // Find the input/textarea for this field
        const inputEl = document.getElementById('field_' + field);
        if (!inputEl) return;

        try {
            const r = await fetch('/devices/lock', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ serial: p.serial, field, lock: !locked }) });
            if (r.ok) {
                if (locked) {
                    // Unlocking - enable the field
                    btn.classList.remove('locked');
                    btn.title = 'Lock field';
                    inputEl.disabled = false;
                    inputEl.style.background = '';
                    inputEl.style.opacity = '';
                    inputEl.style.cursor = '';
                } else {
                    // Locking - disable the field with animation
                    btn.classList.add('locked');
                    btn.title = 'Unlock field';
                    inputEl.disabled = true;
                    inputEl.style.transition = 'background 0.3s ease, opacity 0.3s ease';
                    inputEl.style.background = 'var(--panel)';
                    inputEl.style.opacity = '0.8';
                    inputEl.style.cursor = 'not-allowed';
                }
            }
        } catch (_) { }
    });

    // Wire up refresh button - switch to Preview workflow
    document.getElementById('refresh_data_btn').addEventListener('click', async function () {
        const statusEl = document.getElementById('refresh_status');
        const btn = document.getElementById('refresh_data_btn');
        btn.disabled = true;
        statusEl.textContent = ' Previewing updates...';
        try {
            const body = { serial: p.serial || '', ip: p.ip };
            const r = await fetch('/devices/preview', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
            if (!r.ok) { const t = await r.text(); statusEl.textContent = ' Error: ' + t; btn.disabled = false; return; }
            const { proposed } = await r.json();
            statusEl.textContent = '';
            // Render a diff section
            const fields = ['manufacturer', 'model', 'hostname', 'firmware', 'ip', 'subnet_mask', 'gateway', 'dns_servers', 'dhcp_server', 'asset_number', 'location', 'description', 'web_ui_url'];
            let diffHtml = '<div id="diff_card" style="background:rgba(255,255,255,0.03);border:1px solid rgba(255,255,255,0.08);border-radius:6px;padding:10px;margin:8px 0">';
            diffHtml += '<div style="font-weight:600;color:var(--highlight);margin-bottom:6px;font-size:14px">Proposed Updates</div>';
            diffHtml += '<div style="display:grid;grid-template-columns:auto 1fr auto;gap:6px 8px">';
            fields.forEach(f => {
                let currentVal = (f === 'dns_servers') ? (p.dns_servers || []).join(', ') : (p[f] || '');
                let proposedVal = (f === 'dns_servers') ? (proposed[f] || []).join(', ') : (proposed[f] || '');
                const locked = isLocked(f);
                if (String(currentVal) !== String(proposedVal) && !locked && proposedVal !== '') {
                    diffHtml += '<div style="color:var(--muted)">' + f.replace(/_/g, ' ') + '</div>';
                    diffHtml += '<div><div style="font-size:12px;color:var(--muted)">' + (currentVal || '<i>empty</i>') + ' →</div><div>' + proposedVal + '</div></div>';
                    diffHtml += '<label style="justify-self:end"><input type="checkbox" class="apply-proposed" data-field="' + f + '"> Apply</label>';
                }
            });
            diffHtml += '</div>';
            diffHtml += '<div style="margin-top:8px;text-align:right"><button id="apply_selected_btn" class="primary">Apply Selected</button></div>';
            diffHtml += '</div>';
            const container = document.createElement('div');
            container.innerHTML = diffHtml;
            bodyEl.insertBefore(container, bodyEl.firstChild);

            document.getElementById('apply_selected_btn').addEventListener('click', async () => {
                const checks = Array.from(container.querySelectorAll('.apply-proposed:checked'));
                if (checks.length === 0) { statusEl.textContent = ' No changes selected'; return; }
                // Build update payload from selected fields reading proposed values
                const payload = { serial: p.serial };
                checks.forEach(ch => {
                    const f = ch.getAttribute('data-field');
                    let v = proposed[f];
                    if (f === 'dns_servers' && Array.isArray(v)) {
                        payload[f] = v; // array
                    } else {
                        payload[f] = v;
                    }
                });
                const ur = await fetch('/devices/update', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) });
                if (!ur.ok) { const t = await ur.text(); statusEl.textContent = ' Update failed: ' + t; return; }
                statusEl.textContent = ' Changes applied ✓';
                await new Promise(res => setTimeout(res, 400));
                showPrinterDetails(p.ip, source);
            });
        } catch (err) {
            statusEl.textContent = ' Failed: ' + err;
        } finally {
            btn.disabled = false;
        }
    });

    // Wire up metrics collection button (saved devices only)
    if (source === 'saved') {
        document.getElementById('collect_metrics_btn')?.addEventListener('click', async function () {
            const statusEl = document.getElementById('refresh_status');
            const btn = document.getElementById('collect_metrics_btn');
            btn.disabled = true;
            statusEl.textContent = ' Collecting metrics...';
            try {
                const body = { serial: p.serial, ip: p.ip };
                const r = await fetch('/devices/metrics/collect', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
                if (!r.ok) {
                    const t = await r.text();
                    statusEl.textContent = ' Error: ' + t;
                    btn.disabled = false;
                    return;
                }
                const result = await r.json();
                statusEl.textContent = ' Metrics saved ✓';
                setTimeout(() => { statusEl.textContent = ''; btn.disabled = false; }, 2000);
            } catch (err) {
                statusEl.textContent = ' Failed: ' + err;
                btn.disabled = false;
            }
        });
    }

    // Update action buttons based on source
    actionsEl.innerHTML = '';
    if (source === 'saved') {
        // Saved device: show Delete button
        const deleteBtn = document.createElement('button');
        deleteBtn.textContent = 'Delete Device';
        deleteBtn.onclick = async function () {
            deleteBtn.disabled = true;
            deleteBtn.textContent = 'Deleting...';
            try {
                const r = await fetch('/devices/delete', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ serial: p.Serial }) });
                    if (!r.ok) {
                    deleteBtn.disabled = false;
                    deleteBtn.textContent = 'Delete Device';
                    console.error('Delete failed');
                    window.__pm_shared.showToast('Delete failed', 'error');
                    return;
                }
                deleteBtn.textContent = 'Deleted ✓';
                window.__pm_shared.showToast('Device deleted successfully', 'success');

                // Animate card removal before closing modal
                const cardToRemove = document.querySelector('.saved-device-card[data-serial="' + p.Serial + '"]');
                if (cardToRemove) {
                    cardToRemove.classList.add('removing');
                    setTimeout(() => {
                        overlay.style.display = 'none';
                        document.body.style.overflow = '';
                        delete overlay.dataset.currentPrinterIp;
                        updatePrinters();
                    }, 400);
                } else {
                    overlay.style.display = 'none';
                    document.body.style.overflow = '';
                    delete overlay.dataset.currentPrinterIp;
                    updatePrinters();
                }
            } catch (e) {
                console.error('Delete failed:', e);
                window.__pm_shared.showToast('Delete failed: ' + e.message, 'error');
            }
        };
        actionsEl.appendChild(deleteBtn);
    } else {
        // Discovered device: show Save button
        const saveBtn = document.createElement('button');
        saveBtn.textContent = 'Save Device';
        saveBtn.classList.add('primary');
        saveBtn.onclick = async function () {
            saveBtn.disabled = true;
            saveBtn.textContent = 'Saving...';

            // Create status line for refresh progress
            const statusLine = document.createElement('div');
            statusLine.style.cssText = 'margin-top: 12px; font-size: 0.9em; color: #93a1a1; text-align: center;';

            try {
                // Fast save first (instant response, no confirmation popup)
                await window.__pm_shared.saveDiscoveredDevice(p.IP, true, false);
                saveBtn.textContent = 'Saved ✓';
                actionsEl.appendChild(statusLine);

                // Animate card removal from discovered section
                const cardToRemove = document.querySelector('.device-card[data-ip="' + p.IP + '"]');
                if (cardToRemove) {
                    cardToRemove.classList.add('removing');
                }

                // Then trigger background refresh for additional details
                setTimeout(async () => {
                    try {
                        // Show refreshing indicator with animated dots
                        let dots = 0;
                        statusLine.textContent = 'Gathering additional details';
                        const dotInterval = setInterval(() => {
                            dots = (dots + 1) % 4;
                            statusLine.textContent = 'Gathering additional details' + '.'.repeat(dots);
                        }, 400);

                        // Do full walk in background to get any additional details
                        const body = { ip: p.IP, max_entries: 5000 };
                        const r = await fetch('/mib_walk', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });

                        clearInterval(dotInterval);

                        if (r.ok) {
                            const j = await r.json();
                            statusLine.textContent = 'Processing network details...';
                            // Data is already stored in DB from the walk
                            updatePrinters();
                        } statusLine.textContent = '✓ Details updated';
                        statusLine.style.color = '#859900';
                        setTimeout(() => { 
                            overlay.style.display = 'none';
                            document.body.style.overflow = '';
                            delete overlay.dataset.currentPrinterIp;
                        }, 1200);
                    } catch (e) {
                        // Background refresh failed, but device is already saved
                        console.warn('Background refresh failed:', e);
                        statusLine.textContent = '⚠ Refresh incomplete (device saved)';
                        statusLine.style.color = '#b58900';
                        setTimeout(() => { 
                            overlay.style.display = 'none';
                            document.body.style.overflow = '';
                            delete overlay.dataset.currentPrinterIp;
                        }, 1500);
                    }
                }, 100);
            } catch (e) {
                console.error('Save failed:', e);
                window.__pm_shared.showToast('Save failed: ' + e.message, 'error');
                saveBtn.disabled = false;
                saveBtn.textContent = 'Save Device';
                if (statusLine.parentNode) {
                    statusLine.remove();
                }
            }
        };
        actionsEl.appendChild(saveBtn);
    }
    const closeBtn = document.createElement('button');
    closeBtn.textContent = 'Close';
    closeBtn.onclick = () => { 
        overlay.style.display = 'none';
        document.body.style.overflow = '';
        delete overlay.dataset.currentPrinterIp;
    };
    actionsEl.appendChild(closeBtn);

    // Load existing Web UI credentials for this device
    if (p && p.serial) {
        (async function () {
            try {
                const r = await fetch('/device/webui-credentials?serial=' + encodeURIComponent(p.serial));
                if (r.ok) {
                    const creds = await r.json();
                    if (creds.exists) {
                        document.getElementById('cred_username').value = creds.username || '';
                        document.getElementById('cred_auth_type').value = creds.auth_type || 'basic';
                        document.getElementById('cred_auto_login').checked = creds.auto_login || false;
                        document.getElementById('creds_status').textContent = '✓ Saved';
                        document.getElementById('creds_status').style.color = '#859900';
                    }
                }
            } catch (_) { }
        })();
    }

    // Wire up save credentials button
    document.getElementById('save_creds_btn').addEventListener('click', async function () {
        const statusEl = document.getElementById('creds_status');
        const btn = document.getElementById('save_creds_btn');
        if (!p || !p.serial) { statusEl.textContent = 'Error: no serial'; statusEl.style.color = '#dc322f'; return; }
        btn.disabled = true;
        statusEl.textContent = 'Saving...';
        statusEl.style.color = 'var(--muted)';
        try {
            const payload = {
                serial: p.serial,
                username: document.getElementById('cred_username').value.trim(),
                password: document.getElementById('cred_password').value,
                auth_type: document.getElementById('cred_auth_type').value,
                auto_login: document.getElementById('cred_auto_login').checked
            };
            const r = await fetch('/device/webui-credentials', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload)
            });
            if (!r.ok) throw new Error('Save failed');
            statusEl.textContent = '✓ Saved';
            statusEl.style.color = '#859900';
            document.getElementById('cred_password').value = ''; // clear password field
        } catch (e) {
            statusEl.textContent = 'Error: ' + e;
            statusEl.style.color = '#dc322f';
        } finally {
            btn.disabled = false;
        }
    });
}

// Show printer details modal by fetching device info by IP
// source: 'discovered' or 'saved' to control title and action buttons
function showPrinterDetails(ip, source) {
    if (!ip) return;
    source = source || 'discovered'; // default to discovered
    const bodyEl = document.getElementById('printer_details_body');
    const modal = document.getElementById('printer_details_modal');

    bodyEl.textContent = 'Loading...';
    modal.classList.remove('hidden');

    // Try to find device by IP (checks discovered in-memory and database)
    fetch('/devices/discovered').then(r => r.json()).then(discovered => {
        // Check discovered printers first
        let p = discovered.find(d => d.ip === ip);
        if (p) {
            // Also fetch parse debug to extract raw PDUs
            fetch('/parse_debug?ip=' + encodeURIComponent(ip))
                .then(r => r.ok ? r.json() : null)
                .then(parseDebug => window.__pm_shared_cards.showPrinterDetailsData(p, source, parseDebug))
                .catch(() => window.__pm_shared_cards.showPrinterDetailsData(p, source, null));
            return;
        }

        // Not in discovered, try database - saved list returns wrapped printer_info
        fetch('/devices/list').then(r => r.json()).then(saved => {
            const item = saved.find(d => (d.printer_info && d.printer_info.ip === ip));
                if (item && item.printer_info) {
                window.__pm_shared_cards.showPrinterDetailsData(item.printer_info, 'saved', null);
            } else {
                bodyEl.textContent = 'Device not found';
            }
        }).catch(e => { bodyEl.textContent = 'Error loading device: ' + e; });
    }).catch(e => { bodyEl.textContent = 'Error loading devices: ' + e; });
}

// Load device metrics history and display in UI with interactive timeframe selector
// If targetId is provided, render UI into that element. Otherwise render into default '#metrics_content'.
async function loadDeviceMetrics(serial, targetId) {
    console.log('[Metrics] loadDeviceMetrics called for serial:', serial);
    let contentEl = null;
    if (targetId) contentEl = document.getElementById(targetId);
    if (!contentEl) contentEl = document.getElementById('metrics_content');
    if (!contentEl) {
        console.error('[Metrics] metrics_content element not found and no target available');
        return;
    }
    console.log('[Metrics] Rendering metrics into element:', contentEl.id || contentEl.tagName);

    // Create interactive metrics UI
    let html = '';

    // Toggle to show/hide the datetime selector (hidden by default)
    const toggleTarget = targetId || '';
    html += '<div style="display:flex;justify-content:flex-end;margin-bottom:8px">';
    html += '<button id="metrics_toggle_time_btn" data-action="toggle-time" data-target="' + toggleTarget + '" aria-expanded="false" style="padding:6px 10px;font-size:13px">Show time selector</button>';
    html += '</div>';

    // Datetime range picker - hidden by default
    html += '<div id="metrics_time_selector" class="hidden">';
    html += '<div id="metrics_custom_range" style="margin-bottom:16px;padding:16px;background:rgba(0,0,0,0.2);border:1px solid rgba(255,255,255,0.05);border-radius:6px">';

    // Quick preset buttons (will be enabled/disabled based on available data)
    html += '<div style="margin-bottom:16px">';
    html += '<div style="font-size:12px;color:var(--muted);margin-bottom:8px">Quick Select:</div>';
    html += '<div style="display:flex;gap:6px;flex-wrap:wrap">';
    html += '<button id="preset_day" data-action="preset" data-preset="day" data-serial="' + serial + '" style="padding:6px 12px;font-size:13px;min-height:32px">Last 24 Hours</button>';
    html += '<button id="preset_week" data-action="preset" data-preset="week" data-serial="' + serial + '" style="padding:6px 12px;font-size:13px;min-height:32px">Last 7 Days</button>';
    html += '<button id="preset_month" data-action="preset" data-preset="month" data-serial="' + serial + '" style="padding:6px 12px;font-size:13px;min-height:32px">Last 30 Days</button>';
    html += '<button id="preset_year" data-action="preset" data-preset="year" data-serial="' + serial + '" style="padding:6px 12px;font-size:13px;min-height:32px">Last Year</button>';
    html += '<button id="preset_all" data-action="preset" data-preset="all" data-serial="' + serial + '" style="padding:6px 12px;font-size:13px;min-height:32px">All Time</button>';
    html += '</div>'; // close metrics_custom_range
    html += '</div>'; // close metrics_time_selector
    html += '</div>';

    // Custom datetime range picker with flatpickr
    html += '<div style="margin-bottom:12px">';
    html += '<div style="font-size:12px;color:var(--muted);margin-bottom:8px">Custom Range:</div>';
    html += '<input type="text" id="metrics_datetime_range" placeholder="Select date range..." style="width:100%;background:#073642;border:1px solid #004b56;color:var(--text);padding:10px;border-radius:4px;font-size:14px;min-height:38px;cursor:pointer" readonly />';
    html += '</div>';

    // Data range info
    html += '<div style="font-size:11px;color:var(--muted);margin-bottom:8px;padding:6px;background:rgba(0,0,0,0.1);border-radius:4px">';
    html += 'Available: <span id="metrics_data_range_start">Loading...</span> to <span id="metrics_data_range_end">Loading...</span>';
    html += '</div>';

    // Apply button
    html += '<button data-action="refresh" data-serial="' + serial + '" style="width:100%;padding:10px;font-size:14px;min-height:40px;font-weight:600;background:#268bd2;color:#fff">Update Chart</button>';
    html += '</div>';

    // Stats summary
    html += '<div id="metrics_stats" style="margin-bottom:12px;font-size:12px"></div>';

    // Chart canvas
    html += '<canvas id="metrics_chart" width="500" height="200" style="width:100%;max-height:200px;background:#001f22;border:1px solid rgba(255,255,255,0.06);border-radius:4px"></canvas>';

    contentEl.innerHTML = html;

    // Initialize metrics data range (will be set after first data load)
    window.metricsDataRange = { min: null, max: null, serial: serial, flatpickr: null };

    // Initialize flatpickr and load data
    await initializeCustomDatetimePicker(serial, contentEl);
    // Ensure chart/table refresh runs after picker init so UI shows data without requiring the user to click
    try {
        // Small defer to allow DOM/layout to settle
        setTimeout(() => {
            try { refreshMetricsChart(serial); } catch (e) { console.warn('[Metrics] auto refresh failed:', e); }
        }, 50);
    } catch (e) {
        console.warn('[Metrics] Failed to auto-refresh after init:', e);
    }
}

// Initialize custom datetime picker with actual data bounds
async function initializeCustomDatetimePicker(serial, contentElOverride) {
    console.log('[Metrics] initializeCustomDatetimePicker called');
    try {
        // Fetch all available metrics to determine data range
        const url = '/api/devices/metrics/history?serial=' + encodeURIComponent(serial) + '&period=year';
        console.log('[Metrics] Fetching:', url);
        const res = await fetch(url);
        if (!res.ok) {
            console.error('[Metrics] API returned status:', res.status);
            return;
        }

        const history = await res.json();
        console.log('[Metrics] Received', history?.length || 0, 'data points');
        // Use provided content element or fall back to global
        const contentEl = contentElOverride || document.getElementById('metrics_content');
        if (!history || history.length === 0) {
            console.warn('[Metrics] No history data available');
            if (contentEl) {
                const startEl = contentEl.querySelector('#metrics_data_range_start');
                const endEl = contentEl.querySelector('#metrics_data_range_end');
                if (startEl) startEl.textContent = 'No data';
                if (endEl) endEl.textContent = 'No data';
                contentEl.innerHTML = '<div style="color:var(--muted);padding:12px">No metrics data available yet. Collect metrics first.</div>';
            } else {
                const globalStart = document.getElementById('metrics_data_range_start');
                const globalEnd = document.getElementById('metrics_data_range_end');
                if (globalStart) globalStart.textContent = 'No data';
                if (globalEnd) globalEnd.textContent = 'No data';
                const globalContent = document.getElementById('metrics_content');
                if (globalContent) globalContent.innerHTML = '<div style="color:var(--muted);padding:12px">No metrics data available yet. Collect metrics first.</div>';
            }
            return;
        }

        const minTime = new Date(history[0].timestamp);
        const maxTime = new Date(history[history.length - 1].timestamp);

        // Store data range globally
        window.metricsDataRange.min = minTime;
        window.metricsDataRange.max = maxTime;

        // Update labels inside provided content element if present
        if (contentEl) {
            const startEl = contentEl.querySelector('#metrics_data_range_start');
            const endEl = contentEl.querySelector('#metrics_data_range_end');
            if (startEl) startEl.textContent = minTime.toLocaleString();
            if (endEl) endEl.textContent = maxTime.toLocaleString();
        } else {
            const globalStart = document.getElementById('metrics_data_range_start');
            const globalEnd = document.getElementById('metrics_data_range_end');
            if (globalStart) globalStart.textContent = minTime.toLocaleString();
            if (globalEnd) globalEnd.textContent = maxTime.toLocaleString();
        }

        // Default to full available range (All Time)
        const now = maxTime;

        // Check if flatpickr is available
        if (typeof flatpickr === 'undefined') {
            console.error('[Metrics] flatpickr library not loaded');
            if (contentEl) contentEl.innerHTML = '<div style="color:#d33;padding:12px">Error: Date picker library not loaded. Please refresh the page.</div>';
            return;
        }
        console.log('[Metrics] Initializing flatpickr with full range:', minTime, 'to', maxTime);
        // Initialize flatpickr with range mode
        const selector = (contentElOverride ? ('#' + (contentElOverride.id || 'metrics_datetime_range')) : '#metrics_datetime_range');
        // If contentElOverride is provided, use the input inside it
        const targetSelector = contentElOverride ? (contentElOverride.querySelector('#metrics_datetime_range')) : document.querySelector('#metrics_datetime_range');
        const fpInstance = flatpickr(targetSelector, {
            mode: 'range',
            enableTime: true,
            dateFormat: 'Y-m-d H:i',
            minDate: minTime,
            maxDate: maxTime,
            // Default to all available data on initial load
            defaultDate: [minTime, maxTime],
            time_24hr: true,
            onChange: function (selectedDates, dateStr, instance) {
                console.log('[Metrics] Range changed:', selectedDates);
                // Auto-refresh chart when range changes
                if (selectedDates.length === 2) {
                    refreshMetricsChart(serial);
                }
            },
            onReady: function (selectedDates, dateStr, instance) {
                console.log('[Metrics] flatpickr ready, refreshing chart');
                // Refresh chart with initial range
                refreshMetricsChart(serial);
            }
        });

        // Store flatpickr instance for programmatic updates
        window.metricsDataRange.flatpickr = fpInstance;

        // Enable/disable preset buttons based on available data range
        updatePresetButtonStates(minTime, maxTime);

    } catch (e) {
        console.error('[Metrics] Failed to initialize datetime picker:', e);
        const contentEl = document.getElementById('metrics_content');
        if (contentEl) {
            contentEl.innerHTML = '<div style="color:#d33;padding:12px">Error loading metrics: ' + e.message + '</div>';
        }
    }
}

// Update preset button states based on available data range
function updatePresetButtonStates(minTime, maxTime) {
    const dataRangeMs = maxTime.getTime() - minTime.getTime();
    const dayMs = 24 * 60 * 60 * 1000;

    // Calculate how much data we have
    const hasDayData = dataRangeMs >= dayMs;
    const hasWeekData = dataRangeMs >= 7 * dayMs;
    const hasMonthData = dataRangeMs >= 30 * dayMs;
    const hasYearData = dataRangeMs >= 365 * dayMs;

    // Enable/disable buttons
    const dayBtn = document.getElementById('preset_day');
    const weekBtn = document.getElementById('preset_week');
    const monthBtn = document.getElementById('preset_month');
    const yearBtn = document.getElementById('preset_year');

    if (dayBtn) dayBtn.disabled = !hasDayData;
    if (weekBtn) weekBtn.disabled = !hasWeekData;
    if (monthBtn) monthBtn.disabled = !hasMonthData;
    if (yearBtn) yearBtn.disabled = !hasYearData;

    // All Time button is always enabled (just shows all available data)
}

// Quick range preset button handler
window.setMetricsQuickRange = function (preset, serial) {
    if (!window.metricsDataRange || !window.metricsDataRange.min || !window.metricsDataRange.flatpickr) return;

    const minTime = window.metricsDataRange.min;
    const maxTime = window.metricsDataRange.max;
    const fp = window.metricsDataRange.flatpickr;

    let startTime;
    switch (preset) {
        case 'all':
            startTime = minTime;
            break;
        case 'year':
            startTime = new Date(Math.max(minTime.getTime(), maxTime.getTime() - 365 * 24 * 60 * 60 * 1000));
            break;
        case 'month':
            startTime = new Date(Math.max(minTime.getTime(), maxTime.getTime() - 30 * 24 * 60 * 60 * 1000));
            break;
        case 'week':
            startTime = new Date(Math.max(minTime.getTime(), maxTime.getTime() - 7 * 24 * 60 * 60 * 1000));
            break;
        case 'day':
            startTime = new Date(Math.max(minTime.getTime(), maxTime.getTime() - 24 * 60 * 60 * 1000));
            break;
    }

    // Update flatpickr selection
    fp.setDate([startTime, maxTime], true);
}

// Refresh metrics chart based on selected timeframe
window.refreshMetricsChart = async function (serial) {
    console.log('[Metrics] refreshMetricsChart called for serial:', serial);
    // Prefer modal body if present, otherwise default metrics_content
    const container = document.getElementById('metrics_modal_body') || document.getElementById('metrics_content');
    if (!container) {
        console.error('[Metrics] No metrics container found');
        return;
    }
    const statsEl = container.querySelector('#metrics_stats') || document.getElementById('metrics_stats');
    const canvas = container.querySelector('#metrics_chart') || document.getElementById('metrics_chart');
    if (!canvas || !statsEl) {
        console.error('[Metrics] Missing chart elements within container - canvas:', !!canvas, 'stats:', !!statsEl);
        return;
    }

    try {
        // Get selected dates from flatpickr
        const fp = window.metricsDataRange?.flatpickr;
        if (!fp || !fp.selectedDates || fp.selectedDates.length !== 2) {
            console.warn('[Metrics] No valid date range selected');
            statsEl.textContent = 'Please select a date range';
            return;
        }
        console.log('[Metrics] Using date range:', fp.selectedDates);

        const startTime = fp.selectedDates[0];
        const endTime = fp.selectedDates[1];

        // Validate range
        if (endTime <= startTime) {
            statsEl.textContent = 'End time must be after start time';
            return;
        }

        // Fetch data
        const startISO = startTime.toISOString();
        const endISO = endTime.toISOString();
        const url = '/api/devices/metrics/history?serial=' + encodeURIComponent(serial) + '&since=' + encodeURIComponent(startISO) + '&until=' + encodeURIComponent(endISO);
        console.log('[Metrics] Fetching chart data:', url);
        const res = await fetch(url);

        if (!res.ok) {
            console.error('[Metrics] Chart data API returned status:', res.status);
            statsEl.textContent = 'No metrics data available yet.';
            return;
        }

        const history = await res.json();
        console.log('[Metrics] Received', history?.length || 0, 'chart data points');
        if (!history || history.length === 0) {
            console.warn('[Metrics] No data in selected timeframe');
            statsEl.textContent = 'No metrics data in selected timeframe.';
            drawEmptyChart(canvas);
            return;
        }

        // Calculate stats - comprehensive metrics table
        const latest = history[history.length - 1];
        const oldest = history[0];
        const durationMs = new Date(latest.timestamp).getTime() - new Date(oldest.timestamp).getTime();
        const durationDays = Math.max(1, durationMs / (24 * 60 * 60 * 1000));

        // Build comprehensive metrics table
        let statsHtml = '<table style="width:100%;border-collapse:collapse;margin-bottom:12px;font-size:12px">';
        statsHtml += '<thead><tr style="border-bottom:2px solid rgba(255,255,255,0.1)">';
        statsHtml += '<th style="text-align:left;padding:6px 8px;color:var(--highlight);font-weight:600">Metric</th>';
        statsHtml += '<th style="text-align:right;padding:6px 8px;color:var(--highlight);font-weight:600">Lifetime Total</th>';
        statsHtml += '<th style="text-align:right;padding:6px 8px;color:var(--highlight);font-weight:600">Period Diff</th>';
        statsHtml += '<th style="text-align:right;padding:6px 8px;color:var(--highlight);font-weight:600">Avg/Day</th>';
        statsHtml += '</tr></thead><tbody>';

        // Total Pages
        const lifetimePages = latest.page_count || 0;
        const periodPages = lifetimePages - (oldest.page_count || 0);
        const avgPagesPerDay = (periodPages / durationDays).toFixed(1);
        statsHtml += '<tr style="border-bottom:1px solid rgba(255,255,255,0.05)">';
        statsHtml += '<td style="padding:6px 8px;color:var(--text)">Total Pages</td>';
        statsHtml += '<td style="padding:6px 8px;text-align:right;color:var(--text);font-weight:600">' + lifetimePages.toLocaleString() + '</td>';
        statsHtml += '<td style="padding:6px 8px;text-align:right;color:#268bd2;font-weight:600">' + periodPages.toLocaleString() + '</td>';
        statsHtml += '<td style="padding:6px 8px;text-align:right;color:var(--muted)">' + avgPagesPerDay + '</td>';
        statsHtml += '</tr>';

        // Color Pages (if available)
        if (latest.color_pages !== undefined && latest.color_pages > 0) {
            const lifetimeColor = latest.color_pages || 0;
            const periodColor = lifetimeColor - (oldest.color_pages || 0);
            const avgColorPerDay = (periodColor / durationDays).toFixed(1);
            statsHtml += '<tr style="border-bottom:1px solid rgba(255,255,255,0.05)">';
            statsHtml += '<td style="padding:6px 8px;color:var(--text)">Color Pages</td>';
            statsHtml += '<td style="padding:6px 8px;text-align:right;color:var(--text);font-weight:600">' + lifetimeColor.toLocaleString() + '</td>';
            statsHtml += '<td style="padding:6px 8px;text-align:right;color:#268bd2;font-weight:600">' + periodColor.toLocaleString() + '</td>';
            statsHtml += '<td style="padding:6px 8px;text-align:right;color:var(--muted)">' + avgColorPerDay + '</td>';
            statsHtml += '</tr>';
        }

        // Mono Pages (if available)
        if (latest.mono_pages !== undefined && latest.mono_pages > 0) {
            const lifetimeMono = latest.mono_pages || 0;
            const periodMono = lifetimeMono - (oldest.mono_pages || 0);
            const avgMonoPerDay = (periodMono / durationDays).toFixed(1);
            statsHtml += '<tr style="border-bottom:1px solid rgba(255,255,255,0.05)">';
            statsHtml += '<td style="padding:6px 8px;color:var(--text)">Mono Pages</td>';
            statsHtml += '<td style="padding:6px 8px;text-align:right;color:var(--text);font-weight:600">' + lifetimeMono.toLocaleString() + '</td>';
            statsHtml += '<td style="padding:6px 8px;text-align:right;color:#268bd2;font-weight:600">' + periodMono.toLocaleString() + '</td>';
            statsHtml += '<td style="padding:6px 8px;text-align:right;color:var(--muted)">' + avgMonoPerDay + '</td>';
            statsHtml += '</tr>';
        }

        // Scans (if available)
        if (latest.scan_count !== undefined && latest.scan_count > 0) {
            const lifetimeScans = latest.scan_count || 0;
            const periodScans = lifetimeScans - (oldest.scan_count || 0);
            const avgScansPerDay = (periodScans / durationDays).toFixed(1);
            statsHtml += '<tr style="border-bottom:1px solid rgba(255,255,255,0.05)">';
            statsHtml += '<td style="padding:6px 8px;color:var(--text)">Scans</td>';
            statsHtml += '<td style="padding:6px 8px;text-align:right;color:var(--text);font-weight:600">' + lifetimeScans.toLocaleString() + '</td>';
            statsHtml += '<td style="padding:6px 8px;text-align:right;color:#268bd2;font-weight:600">' + periodScans.toLocaleString() + '</td>';
            statsHtml += '<td style="padding:6px 8px;text-align:right;color:var(--muted)">' + avgScansPerDay + '</td>';
            statsHtml += '</tr>';
        }

        // Toner levels (show current levels from latest snapshot)
        if (latest.toner_levels && Object.keys(latest.toner_levels).length > 0) {
            for (const [color, level] of Object.entries(latest.toner_levels)) {
                const levelNum = typeof level === 'number' ? level : parseInt(level) || 0;
                const levelColor = levelNum < 20 ? '#d32f2f' : (levelNum < 50 ? '#f57c00' : '#388e3c');
                statsHtml += '<tr style="border-bottom:1px solid rgba(255,255,255,0.05)">';
                statsHtml += '<td style="padding:6px 8px;color:var(--text)">' + color + '</td>';
                statsHtml += '<td style="padding:6px 8px;text-align:right;color:' + levelColor + ';font-weight:600">' + levelNum + '%</td>';
                statsHtml += '<td style="padding:6px 8px;text-align:right;color:var(--muted)" colspan="2">Current Level</td>';
                statsHtml += '</tr>';
            }
        }

        statsHtml += '</tbody></table>';
        statsEl.innerHTML = statsHtml;
        console.log('[Metrics] Stats updated, drawing chart');

        // Draw chart
        drawMetricsChart(canvas, history, startTime, endTime);

        // Render a horizontally-scrollable metrics table showing each snapshot and a delete button
        try {
            const tableContainerId = 'metrics_table_container';
            let tableHtml = '<div id="' + tableContainerId + '" class="metrics-table-container" style="margin-top:12px;padding:8px;background:rgba(0,0,0,0.05);border-radius:6px;overflow-x:auto">';
            tableHtml += '<table class="metrics-table" style="border-collapse:collapse;min-width:800px;font-size:13px">';
            tableHtml += '<thead><tr style="border-bottom:2px solid rgba(255,255,255,0.06)">';
            // Timestamp header includes inline delete button column
            tableHtml += '<th style="padding:8px 12px;text-align:left">Timestamp</th>';
            tableHtml += '<th style="padding:8px 12px;text-align:right">Total</th>';
            tableHtml += '<th style="padding:8px 12px;text-align:right">Color</th>';
            tableHtml += '<th style="padding:8px 12px;text-align:right">Mono</th>';
            tableHtml += '<th style="padding:8px 12px;text-align:right">Scans</th>';
            tableHtml += '<th style="padding:8px 12px;text-align:right">Fax</th>';
            tableHtml += '</tr></thead><tbody>';

            history.forEach(item => {
                const ts = new Date(item.timestamp).toLocaleString();
                tableHtml += '<tr style="border-bottom:1px solid rgba(255,255,255,0.03)">';
                // Place delete button inline with timestamp (right-aligned within the same cell)
                tableHtml += '<td style="padding:8px 12px;display:flex;align-items:center;justify-content:space-between">';
                tableHtml += '<span>' + ts + ' <span style="color:var(--muted);font-size:11px;margin-left:6px">(' + (item.tier || 'raw') + ')</span></span>';
                tableHtml += '<span style="margin-left:12px">';
                tableHtml += '<button class="trash-btn" data-id="' + (item.id || '') + '" data-tier="' + (item.tier || '') + '" title="Delete this metrics row"></button>';
                tableHtml += '</span>';
                tableHtml += '</td>';
                tableHtml += '<td style="padding:8px 12px;text-align:right">' + ((item.page_count||0).toLocaleString()) + '</td>';
                tableHtml += '<td style="padding:8px 12px;text-align:right">' + ((item.color_pages||0).toLocaleString()) + '</td>';
                tableHtml += '<td style="padding:8px 12px;text-align:right">' + ((item.mono_pages||0).toLocaleString()) + '</td>';
                tableHtml += '<td style="padding:8px 12px;text-align:right">' + ((item.scan_count||0).toLocaleString()) + '</td>';
                tableHtml += '<td style="padding:8px 12px;text-align:right">' + ((item.fax_pages||0).toLocaleString()) + '</td>';
                tableHtml += '</tr>';
            });

            tableHtml += '</tbody></table></div>';
            // Create a stable panel wrapper with id so we can reliably find/replace it later
            const panelId = 'metrics_rows_panel';
            const panelHtml = '<div class="panel" id="' + panelId + '"><h4 style="margin-top:0;color:var(--highlight)">Metrics Rows</h4>' + tableHtml + '</div>';

            // Remove existing panel if present
            const old = document.getElementById(panelId);
            if (old && old.parentElement) old.parentElement.removeChild(old);

            // Insert panel into the current container (modal or page)
            if (container) {
                const metricsPanel = container.closest('.panel');
                if (metricsPanel && metricsPanel.parentElement) {
                    metricsPanel.insertAdjacentHTML('afterend', panelHtml);
                } else {
                    container.insertAdjacentHTML('beforeend', panelHtml);
                }
            } else {
                canvas.insertAdjacentHTML('afterend', panelHtml);
            }

            // Delegate click handler for delete buttons within the current container
            const metricsContainerForEvents = document.getElementById('metrics_modal_body') || document.getElementById('metrics_content');
            if (metricsContainerForEvents) {
                // Remove previous handler if present to avoid duplicate listeners
                if (metricsContainerForEvents._metricsDeleteHandler) {
                    metricsContainerForEvents.removeEventListener('click', metricsContainerForEvents._metricsDeleteHandler);
                    metricsContainerForEvents._metricsDeleteHandler = null;
                }

                const handler = async (e) => {
                    const btn = e.target.closest('.trash-btn');
                    if (!btn) return;
                    const id = btn.getAttribute('data-id');
                    const tier = btn.getAttribute('data-tier') || '';
                    if (!id) return;

                    // Use project's nicer modal confirm if available
                    const confirmed = await window.__pm_shared.showConfirm('Delete this metrics row? This action cannot be undone.', 'Confirm Delete', true);
                    if (!confirmed) return;

                    try {
                        btn.disabled = true;
                        const resp = await fetch('/api/devices/metrics/delete', {
                            method: 'POST', headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ id: Number(id), tier: tier })
                        });
                            if (resp.status === 204) {
                            // Refresh chart after deletion
                            refreshMetricsChart(serial);
                        } else {
                            const txt = await resp.text();
                                await window.__pm_shared.showConfirm('Failed to delete metric: ' + txt, 'Delete Failed', false);
                        }
                    } catch (err) {
                        await window.__pm_shared.showConfirm('Error deleting metric: ' + err, 'Delete Failed', false);
                    } finally {
                        btn.disabled = false;
                    }
                };

                metricsContainerForEvents.addEventListener('click', handler);
                metricsContainerForEvents._metricsDeleteHandler = handler;
            }
        } catch (e) {
            console.warn('[Metrics] Failed to render metrics table:', e);
        }

    } catch (e) {
        console.error('[Metrics] Error refreshing chart:', e);
        statsEl.textContent = 'Failed to load metrics: ' + e.message;
    }
}

// Draw smooth line graph showing cumulative page count over time
function drawMetricsChart(canvas, history, startTime, endTime) {
    console.log('[Metrics] drawMetricsChart called with', history.length, 'points');
    const ctx = canvas.getContext('2d');
    if (!ctx) {
        console.error('[Metrics] Failed to get canvas 2d context');
        return;
    }

    // Handle high DPI displays (Retina, etc.)
    const dpr = window.devicePixelRatio || 1;
    const rect = canvas.getBoundingClientRect();
    console.log('[Metrics] Canvas dimensions:', rect.width, 'x', rect.height, 'DPR:', dpr);
    canvas.width = rect.width * dpr;
    canvas.height = rect.height * dpr;
    ctx.scale(dpr, dpr);

    const width = rect.width;
    const height = rect.height;

    // Clear canvas
    ctx.clearRect(0, 0, canvas.width, canvas.height);

    if (!history || history.length < 1) {
        drawEmptyChart(canvas);
        return;
    }

    // Chart dimensions with padding
    const padding = { top: 30, right: 40, bottom: 50, left: 60 };
    const chartWidth = width - padding.left - padding.right;
    const chartHeight = height - padding.top - padding.bottom;

    // Convert history to plottable data points
    const dataPoints = history.map(snapshot => ({
        time: new Date(snapshot.timestamp).getTime(),
        value: snapshot.page_count || 0
    })).filter(p => p.time >= startTime.getTime() && p.time <= endTime.getTime());

    if (dataPoints.length === 0) {
        drawEmptyChart(canvas);
        return;
    }

    // Find min/max values for scaling
    const minValue = Math.min(...dataPoints.map(p => p.value));
    const maxValue = Math.max(...dataPoints.map(p => p.value));
    const valueRange = maxValue - minValue || 1;
    const timeRange = endTime.getTime() - startTime.getTime();

    // Helper function to map data to canvas coordinates
    const mapX = (time) => padding.left + ((time - startTime.getTime()) / timeRange) * chartWidth;
    const mapY = (value) => padding.top + chartHeight - ((value - minValue) / valueRange) * chartHeight;

    // Draw background grid
    ctx.strokeStyle = 'rgba(255,255,255,0.05)';
    ctx.lineWidth = 1;

    // Horizontal grid lines
    const ySteps = 5;
    for (let i = 0; i <= ySteps; i++) {
        const value = minValue + (valueRange / ySteps) * i;
        const y = mapY(value);

        ctx.beginPath();
        ctx.moveTo(padding.left, y);
        ctx.lineTo(padding.left + chartWidth, y);
        ctx.stroke();
    }

    // Vertical grid lines (time-based)
    const xSteps = 6;
    for (let i = 0; i <= xSteps; i++) {
        const time = startTime.getTime() + (timeRange / xSteps) * i;
        const x = mapX(time);

        ctx.strokeStyle = 'rgba(255,255,255,0.05)';
        ctx.beginPath();
        ctx.moveTo(x, padding.top);
        ctx.lineTo(x, padding.top + chartHeight);
        ctx.stroke();
    }

    // Draw axes
    ctx.strokeStyle = 'rgba(255,255,255,0.2)';
    ctx.lineWidth = 2;
    ctx.beginPath();
    ctx.moveTo(padding.left, padding.top);
    ctx.lineTo(padding.left, padding.top + chartHeight);
    ctx.lineTo(padding.left + chartWidth, padding.top + chartHeight);
    ctx.stroke();

    // Draw Y-axis labels (page counts)
    ctx.fillStyle = 'rgba(255,255,255,0.6)';
    ctx.font = '11px monospace';
    ctx.textAlign = 'right';
    for (let i = 0; i <= ySteps; i++) {
        const value = Math.round(minValue + (valueRange / ySteps) * i);
        const y = mapY(value);
        ctx.fillText(value.toLocaleString(), padding.left - 8, y + 4);
    }

    // Draw X-axis labels (time)
    ctx.textAlign = 'center';
    ctx.fillStyle = 'rgba(255,255,255,0.6)';
    ctx.font = '10px sans-serif';

    const durationHours = timeRange / (60 * 60 * 1000);
    const formatTime = (time) => {
        const d = new Date(time);
        if (durationHours <= 6) {
            // Show hours and minutes
            return d.getHours().toString().padStart(2, '0') + ':' + d.getMinutes().toString().padStart(2, '0');
        } else if (durationHours <= 48) {
            // Show day and hour
            return (d.getMonth() + 1) + '/' + d.getDate() + ' ' + d.getHours() + 'h';
        } else {
            // Show date only
            return (d.getMonth() + 1) + '/' + d.getDate();
        }
    };

    for (let i = 0; i <= xSteps; i++) {
        const time = startTime.getTime() + (timeRange / xSteps) * i;
        const x = mapX(time);
        const label = formatTime(time);
        ctx.save();
        ctx.translate(x, padding.top + chartHeight + 12);
        ctx.rotate(-Math.PI / 6);
        ctx.fillText(label, 0, 0);
        ctx.restore();
    }

    // Draw smooth gradient area under the line
    if (dataPoints.length > 0) {
        ctx.beginPath();
        ctx.moveTo(mapX(dataPoints[0].time), padding.top + chartHeight);
        ctx.lineTo(mapX(dataPoints[0].time), mapY(dataPoints[0].value));

        // Draw smooth curve through points using quadratic curves
        for (let i = 0; i < dataPoints.length - 1; i++) {
            const curr = dataPoints[i];
            const next = dataPoints[i + 1];
            const currX = mapX(curr.time);
            const currY = mapY(curr.value);
            const nextX = mapX(next.time);
            const nextY = mapY(next.value);

            // Control point for smooth curve (midpoint)
            const cpX = (currX + nextX) / 2;
            const cpY = (currY + nextY) / 2;

            ctx.quadraticCurveTo(currX, currY, cpX, cpY);
        }

        // Final point
        const last = dataPoints[dataPoints.length - 1];
        ctx.lineTo(mapX(last.time), mapY(last.value));
        ctx.lineTo(mapX(last.time), padding.top + chartHeight);
        ctx.closePath();

        // Gradient fill
        const gradient = ctx.createLinearGradient(0, padding.top, 0, padding.top + chartHeight);
        gradient.addColorStop(0, 'rgba(38, 139, 210, 0.3)'); // #268bd2 with alpha
        gradient.addColorStop(1, 'rgba(38, 139, 210, 0.05)');
        ctx.fillStyle = gradient;
        ctx.fill();
    }

    // Draw the main line
    if (dataPoints.length > 0) {
        ctx.beginPath();
        ctx.moveTo(mapX(dataPoints[0].time), mapY(dataPoints[0].value));

        for (let i = 0; i < dataPoints.length - 1; i++) {
            const curr = dataPoints[i];
            const next = dataPoints[i + 1];
            const currX = mapX(curr.time);
            const currY = mapY(curr.value);
            const nextX = mapX(next.time);
            const nextY = mapY(next.value);

            // Control point for smooth curve
            const cpX = (currX + nextX) / 2;
            const cpY = (currY + nextY) / 2;

            ctx.quadraticCurveTo(currX, currY, cpX, cpY);
        }

        // Final segment
        const last = dataPoints[dataPoints.length - 1];
        ctx.lineTo(mapX(last.time), mapY(last.value));

        ctx.strokeStyle = '#268bd2'; // Solarized blue
        ctx.lineWidth = 2.5;
        ctx.lineJoin = 'round';
        ctx.lineCap = 'round';
        ctx.stroke();
    }

    // Draw data points as circles (visible when zoomed in)
    if (dataPoints.length <= 50) {
        ctx.fillStyle = '#268bd2';
        ctx.strokeStyle = '#073642'; // Dark background color
        ctx.lineWidth = 2;

        dataPoints.forEach(point => {
            const x = mapX(point.time);
            const y = mapY(point.value);

            ctx.beginPath();
            ctx.arc(x, y, 4, 0, Math.PI * 2);
            ctx.fill();
            ctx.stroke();
        });
    }

    // Chart title
    ctx.fillStyle = 'rgba(255,255,255,0.8)';
    ctx.font = 'bold 12px sans-serif';
    ctx.textAlign = 'left';
    ctx.fillText('Page Count Over Time', padding.left, 18);

    // Data point count indicator (top right)
    ctx.font = '10px monospace';
    ctx.textAlign = 'right';
    ctx.fillStyle = 'rgba(255,255,255,0.5)';
    ctx.fillText(dataPoints.length + ' points', width - padding.right, 18);
}

// Draw empty chart placeholder
function drawEmptyChart(canvas) {
    const ctx = canvas.getContext('2d');

    // Handle high DPI displays
    const dpr = window.devicePixelRatio || 1;
    const rect = canvas.getBoundingClientRect();
    canvas.width = rect.width * dpr;
    canvas.height = rect.height * dpr;
    ctx.scale(dpr, dpr);

    ctx.clearRect(0, 0, canvas.width, canvas.height);
    ctx.fillStyle = 'rgba(255,255,255,0.3)';
    ctx.font = '12px sans-serif';
    ctx.textAlign = 'center';
    ctx.fillText('No data available', rect.width / 2, rect.height / 2);
}

// Use /devices/list to query database and refresh individual devices as needed

function loadUnknowns() {
    fetch('/unknown_manufacturers').then(r => r.json()).then(lines => {
        const pre = document.getElementById('unknowns');
        pre.textContent = (lines && lines.length) ? lines.join('\n') : '(no entries)';
    }).catch(e => { document.getElementById('unknowns').textContent = 'Failed to load: ' + e });
}

setInterval(updateMetrics, 2000);

// Connect to SSE for real-time updates (replaces polling)
// If this page is being served through the central server proxy, the server
// injects a meta tag `X-PrintMaster-Proxied`. In that case we must connect
// to the server-side SSE endpoint under `/api/events`. Otherwise use the
// agent-local `/events` endpoint.
const __isProxied = !!document.querySelector('meta[http-equiv="X-PrintMaster-Proxied"]');
const __ssePath = __isProxied ? '/api/events' : '/events';
console.log('[SSE] connecting to', __ssePath, 'proxied=', __isProxied);
const eventSource = new EventSource(__ssePath);

eventSource.addEventListener('connected', (e) => {
    console.log('SSE connected:', e.data);
    // Load initial logs when connected
    updateLog();
});

eventSource.addEventListener('log_entry', (e) => {
    const data = JSON.parse(e.data);
    // Format log entry similar to the /logs endpoint format
    let line = data.timestamp + ' [' + data.level + '] ' + data.message;
    if (data.context && Object.keys(data.context).length > 0) {
        for (const [k, v] of Object.entries(data.context)) {
            // Properly stringify the value - handle objects, arrays, and primitives
            let valueStr;
            if (typeof v === 'object' && v !== null) {
                valueStr = JSON.stringify(v);
            } else {
                valueStr = String(v);
            }
            line += ' ' + k + '=' + valueStr;
        }
    }
    
    // Add to our log entries array
    allLogEntries.push(line);
    
    // Limit array size to prevent memory issues (keep last 5000 entries)
    if (allLogEntries.length > 5000) {
        allLogEntries = allLogEntries.slice(-5000);
    }
    
    // Re-filter and display
    filterAndDisplayLogs();
});

eventSource.addEventListener('device_discovered', (e) => {
    console.log('New device discovered:', e.data);
    
    // Check if modal is open - pause updates to prevent glitchiness
    const overlay = document.getElementById('printer_details_overlay');
    if (overlay && overlay.style.display === 'flex' && overlay.dataset.currentPrinterIp) {
        console.log('Skipping updatePrinters() - modal is open');
        return;
    }
    
    updatePrinters(); // Refresh device list
});

eventSource.addEventListener('device_updated', (e) => {
    console.log('Device updated:', e.data);
    
    // Check if modal is open showing this device - don't update to prevent glitchiness
    const overlay = document.getElementById('printer_details_overlay');
    if (overlay && overlay.style.display === 'flex' && overlay.dataset.currentPrinterIp) {
        try {
            const updatedDevice = JSON.parse(e.data);
            const modalIP = overlay.dataset.currentPrinterIp;
            const updatedIP = updatedDevice.ip || updatedDevice.IP || '';
            
            // Skip update if modal is showing this device
            if (modalIP === updatedIP) {
                console.log('Skipping updatePrinters() - modal open for this device');
                return;
            }
        } catch (err) {
            console.warn('Failed to parse device_updated event:', err);
        }
    }
    
    updatePrinters(); // Refresh device list
});

eventSource.addEventListener('device_discovering', (e) => {
    const data = JSON.parse(e.data);
    console.log('Device discovering:', data);
    // Show progressive discovery card (fail-fast if shared renderer unavailable)
    window.__pm_shared_cards.showDiscoveringCard(data);
});

eventSource.addEventListener('metrics_update', (e) => {
    const data = JSON.parse(e.data);
    updateMetricsChart(data);
});

eventSource.onerror = (e) => {
    try {
        console.error('[SSE] connection error, will auto-reconnect', { error: e, readyState: eventSource.readyState, path: __ssePath, timestamp: new Date().toISOString() });
    } catch (err) {
        try { console.error('SSE connection error (failed to log details)'); } catch(_){}
    }
};

// Load auto-discover checkbox state on page load (from unified settings)
fetch('/settings').then(r => r.json()).then(all => {
    const disc = all.discovery || {};
    // Apply discovery-related toggles immediately so UI reflects server state on first paint
    const autoDiscoverEl = document.getElementById('auto_discover_checkbox');
    if (autoDiscoverEl) autoDiscoverEl.checked = disc.auto_discover_enabled === true;
    const autosaveEl = document.getElementById('autosave_checkbox');
    if (autosaveEl) autosaveEl.checked = disc.autosave_discovered_devices === true;
    // Ensure 'Show Discovered Devices Anyway' container visibility follows autosave setting
    const showDiscoveredAnywayContainer = document.getElementById('show_discovered_devices_anyway_container');
    if (showDiscoveredAnywayContainer) showDiscoveredAnywayContainer.style.display = (disc.autosave_discovered_devices === true) ? 'flex' : 'none';
}).catch(e => {
    console.error('Failed to load auto discover state:', e);
});

// UI helpers
function toggleMobileNav() {
    const nav = document.getElementById('mobile_nav');
    if (nav) {
        nav.classList.toggle('open');
    }
}

// Theme toggle functionality
function toggleTheme() {
    const checkbox = document.getElementById('theme-toggle-checkbox');
    const body = document.body;

    // Check if View Transitions API is supported
    if (!document.startViewTransition) {
        // Fallback for browsers that don't support View Transitions (Safari, Firefox)
        if (checkbox.checked) {
            body.classList.add('light-mode');
            localStorage.setItem('theme', 'light');
        } else {
            body.classList.remove('light-mode');
            localStorage.setItem('theme', 'dark');
        }
        return;
    }

    // Get the theme toggle button position to center the expanding circle
    const btn = document.getElementById('theme_toggle_btn');
    if (btn) {
        const rect = btn.getBoundingClientRect();
        const x = rect.left + rect.width / 2;
        const y = rect.top + rect.height / 2;

        // Calculate the maximum distance to cover the entire viewport
        const maxRadius = Math.hypot(
            Math.max(x, window.innerWidth - x),
            Math.max(y, window.innerHeight - y)
        );

        // Set CSS custom properties for the circle animation
        document.documentElement.style.setProperty('--circle-x', x + 'px');
        document.documentElement.style.setProperty('--circle-y', y + 'px');
        document.documentElement.style.setProperty('--circle-radius', maxRadius + 'px');
    }

    // Start view transition with expanding circle animation
    const transition = document.startViewTransition(() => {
        if (checkbox.checked) {
            body.classList.add('light-mode');
            localStorage.setItem('theme', 'light');
        } else {
            body.classList.remove('light-mode');
            localStorage.setItem('theme', 'dark');
        }
    });
}

// Load saved theme preference on page load
function loadThemePreference() {
    const savedTheme = localStorage.getItem('theme');
    const checkbox = document.getElementById('theme-toggle-checkbox');

    if (savedTheme === 'light') {
        document.body.classList.add('light-mode');
        if (checkbox) {
            checkbox.checked = true;
        }
    }
}

// Apply masonry layout to settings grid (or any .settings-grid) - GLOBAL FUNCTION
function applyMasonryLayout(targetGrid) {
    const grid = targetGrid || document.querySelector('.settings-grid');
    if (!grid) return;

    const panels = Array.from(grid.querySelectorAll('.panel'));
    if (panels.length === 0) return;

    // Determine number of columns based on viewport width
    const width = window.innerWidth;
    let numColumns = 1;
    if (width >= 1600) numColumns = 3;
    else if (width >= 1200) numColumns = 2;

    // Reset grid to column layout
    grid.style.display = 'flex';
    grid.style.flexWrap = 'wrap';
    grid.style.gap = '0';
    grid.style.alignItems = 'flex-start';

    // Create column containers
    const columns = [];
    for (let i = 0; i < numColumns; i++) {
        const col = document.createElement('div');
        col.style.flex = '1';
        col.style.minWidth = '0';
        col.style.display = 'flex';
        col.style.flexDirection = 'column';
        col.style.gap = '16px';
        col.style.padding = '0 8px';
        columns.push(col);
    }

    // Track column heights
    const columnHeights = new Array(numColumns).fill(0);

    // Distribute panels to shortest column
    panels.forEach(panel => {
        // Find shortest column
        const shortestIndex = columnHeights.indexOf(Math.min(...columnHeights));
        
        // Add panel to that column
        columns[shortestIndex].appendChild(panel);
        
        // Update column height (approximate based on offsetHeight)
        columnHeights[shortestIndex] += panel.offsetHeight + 16; // +16 for gap
    });

    // Clear grid and append columns
    grid.innerHTML = '';
    columns.forEach(col => grid.appendChild(col));
}

// Load theme preference when page loads
// Global state: check if UI is being accessed through server proxy
let isProxiedFromServer = false;

document.addEventListener('DOMContentLoaded', function () {
    loadThemePreference();
    window.__pm_shared.toggleDatabaseFields();

    // Check if we're being accessed through the server's proxy
    // The server adds a special meta tag when proxying
    const proxiedMeta = document.querySelector('meta[http-equiv="X-PrintMaster-Proxied"]');
    if (proxiedMeta) {
        isProxiedFromServer = true;
        console.log('Agent UI is being accessed through server proxy - disabling nested proxy features');
    }

    // Check for database rotation warning on first page load (provided by shared cards)
    window.__pm_shared_cards.checkDatabaseRotationWarning();

    // Initialize advanced settings visibility
    const advancedVisible = localStorage.getItem('settingsAdvancedVisible') === 'true';
    const advancedCheckbox = document.getElementById('settings_advanced_toggle');
    if (advancedCheckbox) {
        advancedCheckbox.checked = advancedVisible;
        // Apply initial state without animation
        const advancedElements = document.querySelectorAll('.advanced-setting');
        advancedElements.forEach(el => {
            if (advancedVisible) {
                // Use flex for mini-toggle-container, default for others
                el.style.display = el.classList.contains('mini-toggle-container') ? 'flex' : '';
            } else {
                el.style.display = 'none';
            }
        });
    }

    // Attach event listeners for elements that had inline handlers
    const themeToggle = document.getElementById('theme-toggle-checkbox');
    if (themeToggle) {
        themeToggle.addEventListener('change', toggleTheme);
    }

    const hamburger = document.querySelector('.hamburger-menu');
    if (hamburger) {
        hamburger.addEventListener('click', toggleMobileNav);
    }

    const discoverBtn = document.getElementById('discover_now_btn');
    if (discoverBtn) {
        discoverBtn.addEventListener('click', function () {
            fetch("/discover", { method: "POST" }).then(() => {
                setTimeout(updatePrinters, 500);
            });
        });
    }

    const discoverSettingsBtn = document.getElementById('discover_now_settings_btn');
    if (discoverSettingsBtn) {
        discoverSettingsBtn.addEventListener('click', function () {
            fetch("/discover", { method: "POST" }).then(() => {
                setTimeout(updatePrinters, 500);
            });
        });
    }

    // Immediate visibility handlers for "Show Anyway" containers (no save, just show/hide)
    const autoDiscoverCheckbox = document.getElementById('auto_discover_checkbox');
    if (autoDiscoverCheckbox) {
        autoDiscoverCheckbox.addEventListener('change', function () {
            const showAnywayContainer = document.getElementById('show_discover_button_anyway_container');
            const showAnywayToggle = document.getElementById('show_discover_button_anyway');
            if (showAnywayContainer) {
                showAnywayContainer.style.display = this.checked ? 'flex' : 'none';
                // When hiding, uncheck the toggle to prevent unexpected UI state
                if (!this.checked && showAnywayToggle) {
                    showAnywayToggle.checked = false;
                }
            }
        });
    }

    // Note: Autosave checkbox immediate visibility handlers moved to toggleAutosaveUI()
    // to consolidate all autosave logic in one place

    // Passive Discovery master toggle - shows/hides methods and preserves state
    const passiveDiscoveryCheckbox = document.getElementById('passive_discovery_enabled');
    if (passiveDiscoveryCheckbox) {
        // Store previous state of passive discovery methods
        if (!window.__passiveDiscoveryState) {
            window.__passiveDiscoveryState = {};
        }

        passiveDiscoveryCheckbox.addEventListener('change', function () {
            const container = document.getElementById('passive_discovery_methods_container');
            const methodIds = ['discovery_live_wsd_enabled', 'discovery_live_mdns_enabled',
                'discovery_live_ssdp_enabled', 'discovery_live_snmptrap_enabled',
                'discovery_live_llmnr_enabled'];

            if (container) {
                if (this.checked) {
                    // Show container
                    container.style.display = 'block';
                    // Restore previous states
                    methodIds.forEach(id => {
                        const el = document.getElementById(id);
                        if (el && window.__passiveDiscoveryState[id] !== undefined) {
                            el.checked = window.__passiveDiscoveryState[id];
                        }
                    });
                } else {
                    // Save current states before hiding
                    methodIds.forEach(id => {
                        const el = document.getElementById(id);
                        if (el) {
                            window.__passiveDiscoveryState[id] = el.checked;
                            el.checked = false;
                        }
                    });
                    // Hide container
                    container.style.display = 'none';
                }
            }
        });
    }

    const showDiscoverAnywayToggle = document.getElementById('show_discover_button_anyway');
    if (showDiscoverAnywayToggle) {
        showDiscoverAnywayToggle.addEventListener('change', function () {
            const discoverBtn = document.getElementById('discover_now_btn');
            if (discoverBtn) {
                if (this.checked) {
                    discoverBtn.style.display = 'inline-block';
                    discoverBtn.style.opacity = '1';
                    discoverBtn.style.transform = 'scale(1)';
                } else {
                    discoverBtn.style.display = 'none';
                }
            }
        });
    }

    const showDiscoveredAnywayToggle = document.getElementById('show_discovered_devices_anyway');
    if (showDiscoveredAnywayToggle) {
        showDiscoveredAnywayToggle.addEventListener('change', function () {
            // Only affect "Show Known Devices" if this toggle is visible (i.e., autosave is enabled)
            const container = document.getElementById('show_discovered_devices_anyway_container');
            const isVisible = container && container.style.display !== 'none';
            
            if (isVisible) {
                // Force-enable the "Show Known Devices" checkbox when this is toggled on
                const showSavedCheckbox = document.getElementById('show_saved_in_discovered');
                if (showSavedCheckbox && this.checked) {
                    showSavedCheckbox.checked = true;
                }
            }
            updatePrinters();
        });
    }

    const showSavedCheckbox = document.getElementById('show_saved_in_discovered');
    if (showSavedCheckbox) {
        showSavedCheckbox.addEventListener('change', updatePrinters);
    }

    const settingsAutosave = document.getElementById('settings_autosave');
    if (settingsAutosave) {
        settingsAutosave.addEventListener('change', toggleAutoSave);
    }

    const settingsAdvanced = document.getElementById('settings_advanced_toggle');
    if (settingsAdvanced) {
        settingsAdvanced.addEventListener('change', toggleAdvancedSettings);
    }

    const scanLocalSubnet = document.getElementById('scan_local_subnet_enabled');
    if (scanLocalSubnet) {
        scanLocalSubnet.addEventListener('change', toggleRangesDropdown);
    }

    const manualRanges = document.getElementById('manual_ranges_enabled');
    if (manualRanges) {
        manualRanges.addEventListener('change', toggleRangesDropdown);
    }

    const dbBackendType = document.getElementById('db_backend_type');
    if (dbBackendType) {
    dbBackendType.addEventListener('change', function () { window.__pm_shared.toggleDatabaseFields(); });
    }

    // Attach tab button listeners
    document.querySelectorAll('.tab').forEach(btn => {
        btn.addEventListener('click', function () {
            const target = this.getAttribute('data-target');
            if (target) showTab(target);
        });
    });

    // Attach settings link listener
    const settingsLinks = document.querySelectorAll('.settings-link');
    settingsLinks.forEach(link => {
        link.addEventListener('click', function (e) {
            e.preventDefault();
            showTab('settings');
            return false;
        });
    });

    // Attach settings button listeners
    const applyBtn = document.getElementById('settings_apply_btn');
    if (applyBtn) {
        applyBtn.addEventListener('click', function () {
            saveAllSettings(this);
        });
    }

    const revertBtn = document.getElementById('settings_revert_btn');
    if (revertBtn) {
        revertBtn.addEventListener('click', loadSettings);
    }

    const resetBtn = document.getElementById('settings_reset_btn');
    if (resetBtn) {
        resetBtn.addEventListener('click', resetSettings);
    }

    // Discovered search filter
    const discoveredSearch = document.getElementById('discovered_search');
    if (discoveredSearch) {
        discoveredSearch.addEventListener('input', function () {
            filterDiscoveredCards(this.value);
        });
    }

    // Time filter slider
    const timeSlider = document.getElementById('time_slider');
    if (timeSlider) {
        timeSlider.addEventListener('input', function () {
            updateTimeFilter(this.value);
        });
        timeSlider.addEventListener('change', updatePrinters);
    }

    // Discovered section buttons
    const saveAllBtn = document.querySelector('#discovered_section button[onclick*="saveAllDiscovered"]');
    if (saveAllBtn) {
        saveAllBtn.removeAttribute('onclick');
        saveAllBtn.addEventListener('click', saveAllDiscovered);
    }

    const clearDiscBtn = document.querySelector('#discovered_section button[onclick*="clearDiscovered"]');
    if (clearDiscBtn) {
        clearDiscBtn.removeAttribute('onclick');
        clearDiscBtn.addEventListener('click', clearDiscovered);
    }

    // Saved search filter
    const savedSearch = document.getElementById('saved_search');
    if (savedSearch) {
        savedSearch.addEventListener('input', function () {
            filterSavedCards(this.value);
        });
    }

    // Delete all saved devices button
    const deleteAllBtn = document.querySelector('button[onclick*="deleteAllSavedDevices"]');
    if (deleteAllBtn) {
        deleteAllBtn.removeAttribute('onclick');
        deleteAllBtn.addEventListener('click', deleteAllSavedDevices);
    }

    // Clear ranges button
    const clearRangesBtn = document.querySelector('button[onclick*="clearRanges"]');
    if (clearRangesBtn) {
        clearRangesBtn.removeAttribute('onclick');
        clearRangesBtn.addEventListener('click', clearRanges);
    }

    // Regenerate certificates button
    const regenerateCertsBtn = document.getElementById('regenerate_certs_btn');
    if (regenerateCertsBtn) {
        regenerateCertsBtn.addEventListener('click', async function () {
            const _confirmed = await window.__pm_shared.showConfirm('Regenerate TLS certificates? This will create new self-signed certificates. You will need to restart the agent for changes to take effect.', 'Regenerate Certificates', true);
            if (!_confirmed) return;
            try {
                const response = await fetch('/api/regenerate-certs', { method: 'POST' });
                const result = await response.json();
                    if (response.ok) {
                    window.__pm_shared.showAlert(result.message + '\n\nCert: ' + result.cert + '\nKey: ' + result.key, 'Certificates generated', false, false);
                } else {
                    window.__pm_shared.showAlert('Failed to regenerate certificates: ' + (result.error || response.statusText), 'Regenerate Certificates', true, false);
                }
            } catch (err) {
                window.__pm_shared.showAlert('Error regenerating certificates: ' + err.message, 'Regenerate Certificates', true, false);
            }
        });
    }

    // Trace tags buttons
    const loadTraceBtn = document.querySelector('button[onclick*="loadTraceTags"]');
    if (loadTraceBtn) {
        loadTraceBtn.removeAttribute('onclick');
        loadTraceBtn.addEventListener('click', loadTraceTags);
    }

    const saveTraceBtn = document.querySelector('button[onclick*="saveTraceTags"]');
    if (saveTraceBtn) {
        saveTraceBtn.removeAttribute('onclick');
        saveTraceBtn.addEventListener('click', saveTraceTags);
    }

    // Clear database button
    const clearDbBtn = document.getElementById('clear_database_btn');
    if (clearDbBtn) {
        clearDbBtn.addEventListener('click', clearDatabase);
    }

    // Log buttons
    const copyLogsBtn = document.getElementById('copy_logs_btn');
    if (copyLogsBtn) {
        copyLogsBtn.addEventListener('click', copyLogs);
    }

    const downloadLogsBtn = document.getElementById('download_logs_btn');
    if (downloadLogsBtn) {
        downloadLogsBtn.addEventListener('click', downloadLogs);
    }

    const clearLogBtn = document.getElementById('clear_log_btn');
    if (clearLogBtn) {
        clearLogBtn.addEventListener('click', clearLogs);
    }
    
    // Log filters
    const logLevelFilter = document.getElementById('log_level_filter');
    if (logLevelFilter) {
        logLevelFilter.addEventListener('change', filterAndDisplayLogs);
    }
    
    const logSearchFilter = document.getElementById('log_search_filter');
    if (logSearchFilter) {
        logSearchFilter.addEventListener('input', filterAndDisplayLogs);
    }

    // Printer details modal close button, X button, and overlay backdrop click
    const detailsOverlay = document.getElementById('printer_details_overlay');
    const modalCloseBtn = document.querySelector('#printer_details_actions button');
    const printerDetailsCloseX = document.getElementById('printer_details_close_x');
    
    const closePrinterDetailsModal = function() {
        if (detailsOverlay) {
            detailsOverlay.style.display = 'none';
            document.body.style.overflow = '';
            delete detailsOverlay.dataset.currentPrinterIp;
        }
    };
    
    if (modalCloseBtn) {
        modalCloseBtn.addEventListener('click', closePrinterDetailsModal);
    }
    
    if (printerDetailsCloseX) {
        printerDetailsCloseX.addEventListener('click', closePrinterDetailsModal);
    }
    
    // Close modal when clicking backdrop
    if (detailsOverlay) {
        detailsOverlay.addEventListener('click', function(e) {
            if (e.target === detailsOverlay) {
                closePrinterDetailsModal();
            }
        });
    }

    // WebUI modal buttons
    const webuiProxyBtn = document.getElementById('webui_proxy_btn');
    const webuiDirectBtn = document.getElementById('webui_direct_btn');
    const webuiCloseBtn = document.querySelector('.webui-modal-close');
    const webuiCloseX = document.getElementById('webui_modal_close_x');
    const webuiModal = document.getElementById('webui_modal');
    
    if (webuiProxyBtn) {
        // Disable proxy button if we're already being proxied from server
        if (isProxiedFromServer) {
            webuiProxyBtn.disabled = true;
            webuiProxyBtn.style.opacity = '0.5';
            webuiProxyBtn.style.cursor = 'not-allowed';
            
            // Update button text to explain why it's disabled
            const proxyTitle = webuiProxyBtn.querySelector('.webui-modal-button-title');
            const proxyDesc = webuiProxyBtn.querySelector('.webui-modal-button-desc');
            if (proxyTitle) proxyTitle.textContent = '🔐 Proxy UI (Already Proxied)';
            if (proxyDesc) proxyDesc.textContent = 'You are already accessing this through the server proxy';
        } else {
            webuiProxyBtn.addEventListener('click', openProxyUI);
        }
    }
    
    if (webuiDirectBtn) {
        webuiDirectBtn.addEventListener('click', openDirectUI);
    }
    
    if (webuiCloseBtn) {
        webuiCloseBtn.addEventListener('click', closeWebUIModal);
    }
    
    if (webuiCloseX) {
        webuiCloseX.addEventListener('click', closeWebUIModal);
    }
    
    // Close webui modal when clicking backdrop
    if (webuiModal) {
        webuiModal.addEventListener('click', function(e) {
            if (e.target === webuiModal) {
                closeWebUIModal();
            }
        });
    }

    // Apply masonry on load
    applyMasonryLayout();

    // Reapply on window resize (debounced)
    let resizeTimeout;
    window.addEventListener('resize', function() {
        clearTimeout(resizeTimeout);
        resizeTimeout = setTimeout(() => {
            // Get all panels from all columns
            const grid = document.querySelector('.settings-grid');
            if (!grid) return;
            const allPanels = Array.from(grid.querySelectorAll('.panel'));
            
            // Store panels temporarily
            const tempContainer = document.createElement('div');
            allPanels.forEach(panel => tempContainer.appendChild(panel));
            
            // Clear grid
            grid.innerHTML = '';
            
            // Re-add panels to grid
            allPanels.forEach(panel => grid.appendChild(panel));
            
            // Reapply masonry
            applyMasonryLayout();
        }, 250);
    });

    // Reapply when advanced settings toggle (affects panel visibility/height)
    const advancedToggle = document.getElementById('show_advanced');
    if (advancedToggle) {
        advancedToggle.addEventListener('change', function() {
            setTimeout(() => applyMasonryLayout(), 100);
        });
    }
});

function showTab(name) {
    const all = document.querySelectorAll('[data-tab]');
    all.forEach(el => { el.classList.add('hidden'); });
    document.querySelectorAll('[data-tab="' + name + '"]').forEach(el => { el.classList.remove('hidden'); });

    // toggle active class on tab buttons (both desktop and mobile)
    const tabs = document.querySelectorAll('.tab');
    tabs.forEach(t => { t.classList.remove('active'); });
    const activeBtns = document.querySelectorAll('.tab[data-target="' + name + '"]');
    activeBtns.forEach(btn => { btn.classList.add('active'); });

    // Update mobile menu label
    const label = document.getElementById('current_tab_label');
    if (label) {
        const tabNames = {
            'devices': 'Devices',
            'settings': 'Settings',
            'logs': 'Logs'
        };
        label.textContent = 'Menu - ' + (tabNames[name] || name);
    }

    // Close mobile nav after selection
    const nav = document.getElementById('mobile_nav');
    if (nav) {
        nav.classList.remove('open');
    }


    // if devices tab, refresh both discovered and saved devices tables
    if (name === 'devices') {
        updatePrinters();
    }

    // if settings tab, load settings
    if (name === 'settings') {
        loadSettings();
    }
}



// Modal handlers


// Load settings from /settings endpoint and populate ALL UI elements
function loadSettings() {
    fetch('/settings').then(async r => {
        if (!r.ok) { console.warn('failed to load settings'); return; }
        const s = await r.json();
        const dev = s.developer || {};
        const disc = s.discovery || {};
        const sec = s.security || {};

        // Store security settings globally for use in device modal rendering
        globalSettings.security = sec;

        // Populate all form fields from settings
        document.getElementById('dev_debug_logging').value = dev.log_level || 'info';
        document.getElementById('dev_dump_parse_debug').checked = !!dev.dump_parse_debug;
        document.getElementById('dev_show_legacy').checked = !!dev.show_legacy;
        document.getElementById('dev_snmp_community').value = dev.snmp_community || '';
        document.getElementById('dev_snmp_timeout').value = dev.snmp_timeout_ms || 2000;
        document.getElementById('dev_snmp_retries').value = dev.snmp_retries || 1;
        document.getElementById('dev_discover_concurrency').value = dev.discover_concurrency || 50;
        document.getElementById('dev_asset_id_regex').value = dev.asset_id_regex || '';

        document.getElementById('scan_local_subnet_enabled').checked = disc.subnet_scan !== false;
        document.getElementById('manual_ranges_enabled').checked = disc.manual_ranges !== false;
    // Master IP scanning toggle (default enabled)
    const ipScanEl = document.getElementById('ip_scanning_enabled');
    if (ipScanEl) { ipScanEl.checked = disc.ip_scanning_enabled !== false; }
        document.getElementById('discovery_arp_enabled').checked = disc.arp_enabled !== false;
        document.getElementById('discovery_icmp_enabled').checked = disc.icmp_enabled !== false;
        document.getElementById('discovery_tcp_enabled').checked = disc.tcp_enabled !== false;
        document.getElementById('discovery_snmp_enabled').checked = disc.snmp_enabled !== false;
        document.getElementById('discovery_mdns_enabled').checked = disc.mdns_enabled === true;
        document.getElementById('discovery_live_mdns_enabled').checked = disc.auto_discover_live_mdns === true;
        document.getElementById('discovery_live_wsd_enabled').checked = disc.auto_discover_live_wsd === true;
        document.getElementById('discovery_live_ssdp_enabled').checked = disc.auto_discover_live_ssdp === true;
        document.getElementById('discovery_live_snmptrap_enabled').checked = disc.auto_discover_live_snmptrap === true;
        document.getElementById('discovery_live_llmnr_enabled').checked = disc.auto_discover_live_llmnr === true;
        document.getElementById('metrics_rescan_enabled').checked = disc.metrics_rescan_enabled === true;
        document.getElementById('metrics_rescan_interval').value = disc.metrics_rescan_interval_minutes ?? 60;
        document.getElementById('auto_discover_checkbox').checked = disc.auto_discover_enabled === true;
        document.getElementById('autosave_checkbox').checked = disc.autosave_discovered_devices === true;

        // Load the "Show Anyway" toggle states
        document.getElementById('show_discover_button_anyway').checked = disc.show_discover_button_anyway === true;
        document.getElementById('show_discovered_devices_anyway').checked = disc.show_discovered_devices_anyway === true;

        // Load passive discovery master toggle and determine if any methods are enabled
        const anyPassiveMethodEnabled = disc.auto_discover_live_mdns === true ||
            disc.auto_discover_live_wsd === true ||
            disc.auto_discover_live_ssdp === true ||
            disc.auto_discover_live_snmptrap === true ||
            disc.auto_discover_live_llmnr === true;
        const passiveEnabled = disc.passive_discovery_enabled !== false && anyPassiveMethodEnabled;
        document.getElementById('passive_discovery_enabled').checked = passiveEnabled;

        // Update UI state based on loaded settings
    // Ensure ranges toggle and IP scanning UI reflect server settings. Some browsers
    // or racey initialization can cause the checkbox state to be overwritten by
    // other startup handlers; apply the desired state and re-run the UI toggles
    // immediately and once again after a short delay to be robust.
    const desiredManualRanges = disc.manual_ranges !== false;
    const manualRangesEl = document.getElementById('manual_ranges_enabled');
    if (manualRangesEl) {
        manualRangesEl.checked = desiredManualRanges;
    }
    toggleRangesDropdown();
    toggleIPScanningUI();
    // Re-apply after a tick in case other init code overwrote the checkbox
    setTimeout(() => {
        const el = document.getElementById('manual_ranges_enabled');
        if (el) {
            el.checked = desiredManualRanges;
        }
        toggleRangesDropdown();
        toggleIPScanningUI();
    }, 60);
        updateSubnetDisplay();
        document.getElementById('discover_now_btn').style.display = (disc.auto_discover_enabled === true) ? 'none' : 'inline-block';

        // Update the visibility of the "Show Anyway" toggle containers based on loaded settings
        const showDiscoverAnywayContainer = document.getElementById('show_discover_button_anyway_container');
        if (showDiscoverAnywayContainer) {
            showDiscoverAnywayContainer.style.display = (disc.auto_discover_enabled === true) ? 'flex' : 'none';
        }
        const showDiscoveredAnywayContainer = document.getElementById('show_discovered_devices_anyway_container');
        if (showDiscoveredAnywayContainer) {
            showDiscoveredAnywayContainer.style.display = (disc.autosave_discovered_devices === true) ? 'flex' : 'none';
        }

        // Update passive discovery methods container visibility
        const passiveMethodsContainer = document.getElementById('passive_discovery_methods_container');
        if (passiveMethodsContainer) {
            passiveMethodsContainer.style.display = passiveEnabled ? 'block' : 'none';
        }

        // Populate security settings
        const credentialsCheckbox = document.getElementById('enable_saved_credentials');
        if (credentialsCheckbox) {
            credentialsCheckbox.checked = sec.credentials_enabled !== false;
        }

        // Populate network settings (HTTP/HTTPS)
        const enableHttpCheckbox = document.getElementById('enable_http');
        if (enableHttpCheckbox) {
            enableHttpCheckbox.checked = sec.enable_http !== false;
        }
        const httpPortInput = document.getElementById('http_port');
        if (httpPortInput) {
            httpPortInput.value = sec.http_port || '8080';
        }
        const enableHttpsCheckbox = document.getElementById('enable_https');
        if (enableHttpsCheckbox) {
            enableHttpsCheckbox.checked = sec.enable_https !== false;
        }
        const httpsPortInput = document.getElementById('https_port');
        if (httpsPortInput) {
            httpsPortInput.value = sec.https_port || '8443';
        }
        const redirectCheckbox = document.getElementById('redirect_http_to_https');
        if (redirectCheckbox) {
            redirectCheckbox.checked = sec.redirect_http_to_https === true;
        }
        const customCertPathInput = document.getElementById('custom_cert_path');
        if (customCertPathInput) {
            customCertPathInput.value = sec.custom_cert_path || '';
        }
        const customKeyPathInput = document.getElementById('custom_key_path');
        if (customKeyPathInput) {
            customKeyPathInput.value = sec.custom_key_path || '';
        }

        // Apply the "Show Discover Button Anyway" toggle state
        if (disc.show_discover_button_anyway === true && disc.auto_discover_enabled === true) {
            document.getElementById('discover_now_btn').style.display = 'inline-block';
        }

        // Apply the "Show Discovered Devices Anyway" toggle state (only if autosave is enabled)
        if (disc.show_discovered_devices_anyway === true && disc.autosave_discovered_devices === true) {
            const showSavedCheckbox = document.getElementById('show_saved_in_discovered');
            if (showSavedCheckbox) {
                showSavedCheckbox.checked = true;
            }
        }

        updatePrinters();
        loadTraceTags();
    }).catch(e => { console.error('loadSettings failed', e); });
}

// Available trace tag categories for granular logging, organized by section
const TRACE_TAG_CATEGORIES = {
    'Proxy': [
        { id: 'proxy_request', label: 'Requests' },
        { id: 'proxy_response', label: 'Responses' },
        { id: 'proxy_director', label: 'Director/Header Rewrites' },
        { id: 'proxy_body_rewrite', label: 'Body Rewrites' }
    ],
    'SNMP': [
        { id: 'snmp_walk', label: 'SNMP Walks' },
        { id: 'snmp_trap', label: 'SNMP Traps' }
    ],
    'Metrics': [
        { id: 'metrics_collection', label: 'Metrics Collection' },
        { id: 'metrics_rescan', label: 'Metrics Rescan' }
    ],
    'Discovery': [
        { id: 'discovery_scan', label: 'Discovery Scan' },
        { id: 'mdns', label: 'mDNS' },
        { id: 'wsd', label: 'WS-Discovery' },
        { id: 'ssdp', label: 'SSDP' },
        { id: 'llmnr', label: 'LLMNR' },
        { id: 'arp', label: 'ARP' }
    ],
    'Device Operations': [
        { id: 'device_save', label: 'Device Save' },
        { id: 'device_delete', label: 'Device Delete' }
    ]
};

function loadTraceTags() {
    const container = document.getElementById('trace_tags_container');
    if (!container) {
        console.error('trace_tags_container element not found');
        return;
    }

    fetch('/settings/trace_tags').then(async r => {
        if (!r.ok) {
            console.error('Failed to load trace tags:', r.status, r.statusText);
            container.innerHTML = '<span style="color:var(--muted);font-size:12px">Failed to load tags (status ' + r.status + ')</span>';
            return;
        }
        const data = await r.json();
        console.log('Loaded trace tags:', data);
        // API returns tags as a map/object {tag: bool}, not an array
        const enabledTagsMap = data.tags || {};

        container.innerHTML = '';

        if (Object.keys(TRACE_TAG_CATEGORIES).length === 0) {
            container.innerHTML = '<span style="color:var(--muted);font-size:12px">No trace tag categories defined</span>';
            return;
        }

        let totalTags = 0;

        // Render each section
        Object.keys(TRACE_TAG_CATEGORIES).forEach(sectionName => {
            const tags = TRACE_TAG_CATEGORIES[sectionName];

            // Section header with toggle all checkbox
            const sectionDiv = document.createElement('div');
            sectionDiv.style.gridColumn = '1 / -1';
            sectionDiv.style.marginTop = totalTags > 0 ? '12px' : '0';
            sectionDiv.style.paddingBottom = '6px';
            sectionDiv.style.borderBottom = '1px solid var(--border)';
            sectionDiv.style.display = 'flex';
            sectionDiv.style.alignItems = 'center';
            sectionDiv.style.gap = '8px';

            const sectionToggle = document.createElement('input');
            sectionToggle.type = 'checkbox';
            sectionToggle.className = 'section-toggle';
            sectionToggle.dataset.section = sectionName;
            sectionToggle.onchange = () => toggleTraceSection(sectionName, sectionToggle.checked);

            const sectionLabel = document.createElement('span');
            sectionLabel.style.fontWeight = '500';
            sectionLabel.style.color = 'var(--highlight)';
            sectionLabel.style.fontSize = '14px';
            sectionLabel.textContent = sectionName;

            sectionDiv.appendChild(sectionToggle);
            sectionDiv.appendChild(sectionLabel);
            container.appendChild(sectionDiv);

            // Render tags in this section
            tags.forEach(tag => {
                const label = document.createElement('label');
                label.style.display = 'flex';
                label.style.alignItems = 'center';
                label.style.gap = '6px';
                label.style.fontSize = '13px';
                label.style.cursor = 'pointer';
                label.style.paddingLeft = '20px';

                const checkbox = document.createElement('input');
                checkbox.type = 'checkbox';
                checkbox.id = 'trace_tag_' + tag.id;
                checkbox.className = 'trace-tag-checkbox';
                checkbox.dataset.section = sectionName;
                checkbox.checked = enabledTagsMap[tag.id] === true;
                checkbox.onchange = () => updateSectionToggle(sectionName);

                label.appendChild(checkbox);
                label.appendChild(document.createTextNode(tag.label));
                container.appendChild(label);
                totalTags++;
            });

            // Update section toggle state
            updateSectionToggle(sectionName);
        });

        console.log('Rendered', totalTags, 'trace tag checkboxes in', Object.keys(TRACE_TAG_CATEGORIES).length, 'sections');
    }).catch(e => {
        console.error('loadTraceTags failed', e);
        container.innerHTML = '<span style="color:var(--muted);font-size:12px">Error: ' + e.message + '</span>';
    });
}

function toggleTraceSection(sectionName, checked) {
    const checkboxes = document.querySelectorAll('.trace-tag-checkbox[data-section="' + sectionName + '"]');
    checkboxes.forEach(cb => cb.checked = checked);
}

function updateSectionToggle(sectionName) {
    const checkboxes = document.querySelectorAll('.trace-tag-checkbox[data-section="' + sectionName + '"]');
    const sectionToggle = document.querySelector('.section-toggle[data-section="' + sectionName + '"]');
    if (!sectionToggle) return;

    let allChecked = true;
    let anyChecked = false;
    checkboxes.forEach(cb => {
        if (cb.checked) anyChecked = true;
        else allChecked = false;
    });

    sectionToggle.checked = allChecked;
    sectionToggle.indeterminate = anyChecked && !allChecked;
}

function saveTraceTags() {
    // Build tags object as {tag_id: true/false}
    const tagsMap = {};
    Object.keys(TRACE_TAG_CATEGORIES).forEach(sectionName => {
        TRACE_TAG_CATEGORIES[sectionName].forEach(tag => {
            const checkbox = document.getElementById('trace_tag_' + tag.id);
            if (checkbox) {
                tagsMap[tag.id] = checkbox.checked;
            }
        });
    });

    console.log('Saving trace tags:', tagsMap);

    fetch('/settings/trace_tags', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ tags: tagsMap })
    }).then(async r => {
        if (!r.ok) {
            window.__pm_shared.showToast('Failed to save trace tags', 'error');
            return;
        }
        window.__pm_shared.showToast('Trace tags saved successfully', 'success');
    }).catch(e => {
        console.error('saveTraceTags failed', e);
        window.__pm_shared.showToast('Failed to save trace tags', 'error');
    });
}



// Saved walks helpers
function refreshMibWalks() {
    // Now list merged devices rather than raw saved walks
    fetch('/devices/list').then(r => r.json()).then(arr => {
        const tbody = document.getElementById('mib_walks_tbody'); tbody.innerHTML = '';
        // arr elements: { serial, path, info }
        arr.forEach(item => {
            const tr = document.createElement('tr'); tr.style.borderTop = '1px solid rgba(255,255,255,0.03)';
            const nameTd = document.createElement('td'); nameTd.textContent = item.path || ''; nameTd.style.fontFamily = 'monospace';
            const ipTd = document.createElement('td'); ipTd.textContent = (item.info && item.info.ip) ? item.info.ip : '';
            const dtTd = document.createElement('td'); dtTd.textContent = '';
            const cntTd = document.createElement('td'); cntTd.style.textAlign = 'right'; cntTd.textContent = '';
            const actionsTd = document.createElement('td'); actionsTd.style.display = 'flex'; actionsTd.style.gap = '6px';
            const viewBtn = document.createElement('button'); viewBtn.textContent = 'View'; viewBtn.onclick = () => { viewDevice(item.serial); };
            const delBtn = document.createElement('button'); delBtn.textContent = 'Delete'; delBtn.onclick = async () => {
                delBtn.disabled = true;
                delBtn.textContent = 'Deleting...';
                const r = await fetch('/devices/delete', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ serial: item.serial }) });
                if (!r.ok) { delBtn.disabled = false; delBtn.textContent = 'Delete'; window.__pm_shared.showToast('Delete failed', 'error'); return; }
                window.__pm_shared.showToast('Device deleted successfully', 'success');
                refreshMibWalks();
            };
            actionsTd.appendChild(viewBtn); actionsTd.appendChild(delBtn);
            tr.appendChild(nameTd); tr.appendChild(ipTd); tr.appendChild(dtTd); tr.appendChild(cntTd); tr.appendChild(actionsTd);
            tbody.appendChild(tr);
        });
    }).catch(e => { console.error('failed to load devices', e); });
}

// Removed legacy alias: refreshDevices()

function viewDevice(serial) {
    const meta = document.getElementById('mib_results_meta'); const tbody = document.getElementById('mib_results_tbody');
    meta.style.display = 'block'; meta.textContent = 'Loading device ' + serial + '...'; tbody.innerHTML = '';
    fetch('/devices/get?serial=' + encodeURIComponent(serial)).then(async r => {
        if (!r.ok) { meta.textContent = 'Not found'; return; }
        const j = await r.json();
        // Direct device format (compatibility wrapper removed)
        let deviceData = j;
        if (deviceData) {
            renderPrinterInfo(deviceData);
            // add a refresh button to meta so user can refresh this device
            const btn = document.createElement('button'); btn.textContent = 'Refresh Device';
            btn.onclick = async () => {
                btn.disabled = true; const old = btn.textContent; btn.textContent = 'Refreshing...';
                try {
                    const resp = await fetch('/devices/refresh', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ serial }) });
                    if (!resp.ok) {
                        const txt = await resp.text();
                        window.__pm_shared.showToast('Refresh failed: ' + txt, 'error');
                        return;
                    }
                    window.__pm_shared.showToast('Refresh queued for ' + serial, 'success');
                } catch (e) { window.__pm_shared.showToast('Refresh failed: ' + e.message, 'error'); }
                finally { btn.disabled = false; btn.textContent = old; }
            };
            meta.appendChild(document.createTextNode(' ')); meta.appendChild(btn);
            meta.dataset.deviceSerial = serial;
            meta.textContent = 'Device: ' + serial;
        } else {
            meta.textContent = 'No device data available';
        }
    }).catch(e => { meta.textContent = 'Load failed: ' + e; });
}

function renderPrinterInfo(pi) {
    const tbody = document.getElementById('mib_results_tbody'); tbody.innerHTML = '';
    // show a handful of key/value rows
    const addRow = (k, v) => {
        const tr = document.createElement('tr');
        const act = document.createElement('td'); act.textContent = '';
        const sym = document.createElement('td'); sym.textContent = k;
        const typ = document.createElement('td'); typ.textContent = typeof v;
        const val = document.createElement('td'); val.textContent = (v === null ? '' : (typeof v === 'object' ? JSON.stringify(v) : String(v)));
        const oid = document.createElement('td'); oid.textContent = '';
        tr.appendChild(act); tr.appendChild(sym); tr.appendChild(typ); tr.appendChild(val); tr.appendChild(oid);
        tbody.appendChild(tr);
    };
    addRow('IP', pi.ip || '');
    addRow('Manufacturer', pi.manufacturer || '');
    addRow('Model', pi.model || '');
    addRow('Serial', pi.serial || '');
    addRow('Hostname', pi.hostname || '');
    addRow('MAC', pi.mac_address || '');
    addRow('Firmware', pi.firmware || '');
    addRow('PageCount', pi.page_count || '');
    if (pi.toner_levels) addRow('TonerLevels', pi.toner_levels);
    if (pi.consumables) addRow('Consumables', pi.consumables);
    if (pi.status_messages && pi.status_messages.length > 0) addRow('StatusMessages', pi.status_messages);
    if (pi.last_seen) addRow('LastSeen', new Date(pi.last_seen).toLocaleString());
}



function saveDevSettings() {
    const logLevel = document.getElementById('dev_debug_logging').value;
    const body = {
        log_level: logLevel,
        dump_parse_debug: document.getElementById('dev_dump_parse_debug').checked,
        show_legacy: document.getElementById('dev_show_legacy').checked,
        snmp_community: document.getElementById('dev_snmp_community').value,
        snmp_timeout_ms: parseInt(document.getElementById('dev_snmp_timeout').value) || 2000,
        snmp_retries: parseInt(document.getElementById('dev_snmp_retries').value) || 1,
        discover_concurrency: parseInt(document.getElementById('dev_discover_concurrency').value) || 50,
        asset_id_regex: document.getElementById('dev_asset_id_regex').value || ''
    };
    // POST developer settings as part of the unified /settings endpoint
    fetch('/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ developer: body }) })
        .then(async r => { 
            if (!r.ok) { 
                const t = await r.text(); 
                console.error('Save failed:', t); 
                window.__pm_shared.showToast('Save failed: ' + t, 'error');
                return; 
            } 
            window.__pm_shared.showToast('Settings saved successfully', 'success');
        })
        .catch(e => { console.error('Save failed:', e); window.__pm_shared.showToast('Save failed: ' + e.message, 'error'); });
}

// Toggle auto-save mode
function toggleAutoSave() {
    const enabled = document.getElementById('settings_autosave').checked;
    const buttonsDiv = document.getElementById('settings_buttons');
    const advancedLabel = document.getElementById('advanced_toggle_label');

    if (enabled) {
        // Hide buttons and move advanced toggle fully right
        buttonsDiv.style.display = 'none';
        if (advancedLabel) {
            advancedLabel.style.marginLeft = 'auto';
        }
        // Save the preference
        localStorage.setItem('settings_autosave', 'true');
        // Add onchange handlers to all settings inputs
        addAutoSaveHandlers();
    } else {
        // Show buttons and adjust spacing
        buttonsDiv.style.display = 'flex';
        if (advancedLabel) {
            advancedLabel.style.marginLeft = '12px';
        }
        localStorage.setItem('settings_autosave', 'false');
        // Remove onchange handlers
        removeAutoSaveHandlers();
    }
}

// Toggle visibility of advanced settings controls
function toggleAdvancedSettings() {
    try {
        const enabled = document.getElementById('settings_advanced_toggle')?.checked || false;
        // Elements marked as advanced-setting should be shown/hidden
        document.querySelectorAll('.advanced-setting').forEach(el => {
            if (enabled) {
                el.style.display = '';
            } else {
                el.style.display = 'none';
            }
        });

        // Textareas or other advanced inputs may use a dedicated class
        document.querySelectorAll('.advanced-setting-textarea').forEach(el => {
            if (enabled) el.style.display = '';
            else el.style.display = 'none';
        });

        // Persist preference
        try { localStorage.setItem('settings_advanced', enabled ? 'true' : 'false'); } catch (e) {}
    } catch (e) {
        console.error('toggleAdvancedSettings failed', e);
        throw e;
    }
}

// Update the compact time filter display label from slider index
function updateTimeFilter(index) {
    const labels = ['1m','2m','5m','10m','15m','30m','1h','2h','3h','6h','12h','1d','3d','All Time'];
    let idx = parseInt(index, 10);
    if (isNaN(idx) || idx < 0) idx = labels.length - 1;
    if (idx >= labels.length) idx = labels.length - 1;
    const el = document.getElementById('time_filter_value');
    if (el) el.textContent = labels[idx];
}

function showAutosaveFeedback() {
    const feedback = document.getElementById('autosave_feedback');
    if (!feedback) return;

    feedback.classList.add('show');
    setTimeout(() => {
        feedback.classList.remove('show');
    }, 2000);
}

// Add auto-save handlers to all settings inputs
function addAutoSaveHandlers() {
    // keep references so we can remove listeners later
    window.__settingsChangeHandler = () => { saveAllSettings().then(() => showAutosaveFeedback()); };
    window.__manualRangesHandler = () => { toggleRangesDropdown(); saveAllSettings().then(() => showAutosaveFeedback()); };
    window.__ipScanningHandler = () => { toggleIPScanningUI(); saveAllSettings().then(() => showAutosaveFeedback()); };

    // Discovery settings
    document.getElementById('scan_local_subnet_enabled')?.addEventListener('change', window.__settingsChangeHandler);
    const manualRangesEl = document.getElementById('manual_ranges_enabled');
    if (manualRangesEl) { manualRangesEl.addEventListener('change', window.__manualRangesHandler); }
    document.getElementById('discovery_arp_enabled')?.addEventListener('change', window.__settingsChangeHandler);
    document.getElementById('discovery_icmp_enabled')?.addEventListener('change', window.__settingsChangeHandler);
    document.getElementById('discovery_tcp_enabled')?.addEventListener('change', window.__settingsChangeHandler);
    document.getElementById('discovery_snmp_enabled')?.addEventListener('change', window.__settingsChangeHandler);
    document.getElementById('discovery_mdns_enabled')?.addEventListener('change', window.__settingsChangeHandler);
    document.getElementById('ip_scanning_enabled')?.addEventListener('change', window.__ipScanningHandler);
    // Auto-save ranges when the textarea loses focus
    const rangesEl = document.getElementById('ranges_text');
    if (rangesEl) { rangesEl.addEventListener('blur', window.__settingsChangeHandler); }
    // Auto Discover and Autosave toggles (with UI effects)
    window.__autoDiscoverHandler = () => { toggleAutoDiscoverUI(); saveAllSettings().then(() => showAutosaveFeedback()); };
    window.__autosaveHandler = () => { toggleAutosaveUI(); saveAllSettings().then(() => showAutosaveFeedback()); };
    const autoDiscoverEl = document.getElementById('auto_discover_checkbox');
    if (autoDiscoverEl) { autoDiscoverEl.addEventListener('change', window.__autoDiscoverHandler); }
    const autosaveEl = document.getElementById('autosave_checkbox');
    if (autosaveEl) { autosaveEl.addEventListener('change', window.__autosaveHandler); }

    // "Show Anyway" toggles (trigger save and update UI)
    window.__showDiscoverAnywayHandler = () => {
        const checked = document.getElementById('show_discover_button_anyway')?.checked;
        const container = document.getElementById('show_discover_button_anyway_container');
        const discoverBtn = document.getElementById('discover_now_btn');
        
        // Only affect discover button if this toggle is visible (auto discover must be enabled)
        const isVisible = container && container.style.display !== 'none';
        
        if (isVisible && discoverBtn) {
            if (checked) {
                discoverBtn.style.display = 'inline-block';
            } else {
                discoverBtn.style.display = 'none';
            }
        }
        
        saveAllSettings().then(() => showAutosaveFeedback());
    };
    window.__showDiscoveredAnywayHandler = () => {
        const checked = document.getElementById('show_discovered_devices_anyway')?.checked;
        const showSavedCheckbox = document.getElementById('show_saved_in_discovered');
        const container = document.getElementById('show_discovered_devices_anyway_container');
        
        // Only affect "Show Known Devices" if this toggle is visible (autosave must be enabled)
        const isVisible = container && container.style.display !== 'none';
        
        if (isVisible && checked && showSavedCheckbox) {
            // When "Show Anyway" is enabled, force-enable "Show Known Devices"
            // (discovered section stays visible and shows all devices including saved ones)
            showSavedCheckbox.checked = true;
        }
        
        // Always update printers to show/hide discovered section
        updatePrinters();
        
        // Save the preference
        saveAllSettings().then(() => showAutosaveFeedback());
    };
    const showDiscoverAnywayEl = document.getElementById('show_discover_button_anyway');
    if (showDiscoverAnywayEl) { showDiscoverAnywayEl.addEventListener('change', window.__showDiscoverAnywayHandler); }
    const showDiscoveredAnywayEl = document.getElementById('show_discovered_devices_anyway');
    if (showDiscoveredAnywayEl) { showDiscoveredAnywayEl.addEventListener('change', window.__showDiscoveredAnywayHandler); }

    // Passive discovery master toggle gets standard change handler
    document.getElementById('passive_discovery_enabled')?.addEventListener('change', window.__settingsChangeHandler);
    document.getElementById('discovery_live_mdns_enabled')?.addEventListener('change', window.__settingsChangeHandler);
    document.getElementById('discovery_live_wsd_enabled')?.addEventListener('change', window.__settingsChangeHandler);
    document.getElementById('discovery_live_ssdp_enabled')?.addEventListener('change', window.__settingsChangeHandler);
    document.getElementById('discovery_live_snmptrap_enabled')?.addEventListener('change', window.__settingsChangeHandler);
    document.getElementById('discovery_live_llmnr_enabled')?.addEventListener('change', window.__settingsChangeHandler);
    document.getElementById('metrics_rescan_enabled')?.addEventListener('change', window.__settingsChangeHandler);
    // Remove IP scanning handlers when autosave disabled
    document.getElementById('metrics_rescan_interval')?.addEventListener('change', window.__settingsChangeHandler);

    // Developer settings
    document.getElementById('dev_debug_logging')?.addEventListener('change', window.__settingsChangeHandler);
    document.getElementById('dev_dump_parse_debug')?.addEventListener('change', window.__settingsChangeHandler);
    document.getElementById('dev_show_legacy')?.addEventListener('change', window.__settingsChangeHandler);
    document.getElementById('dev_snmp_community')?.addEventListener('change', window.__settingsChangeHandler);
    document.getElementById('dev_snmp_timeout')?.addEventListener('change', window.__settingsChangeHandler);
    document.getElementById('dev_snmp_retries')?.addEventListener('change', window.__settingsChangeHandler);
    document.getElementById('dev_discover_concurrency')?.addEventListener('change', window.__settingsChangeHandler);
    document.getElementById('dev_asset_id_regex')?.addEventListener('change', window.__settingsChangeHandler);
}

// Remove auto-save handlers
function removeAutoSaveHandlers() {
    if (!window.__settingsChangeHandler) return;
    document.getElementById('scan_local_subnet_enabled')?.removeEventListener('change', window.__settingsChangeHandler);
    const manualRangesEl = document.getElementById('manual_ranges_enabled');
    if (manualRangesEl && window.__manualRangesHandler) { manualRangesEl.removeEventListener('change', window.__manualRangesHandler); }
    document.getElementById('discovery_arp_enabled')?.removeEventListener('change', window.__settingsChangeHandler);
    document.getElementById('discovery_icmp_enabled')?.removeEventListener('change', window.__settingsChangeHandler);
    document.getElementById('discovery_tcp_enabled')?.removeEventListener('change', window.__settingsChangeHandler);
    document.getElementById('discovery_snmp_enabled')?.removeEventListener('change', window.__settingsChangeHandler);
    document.getElementById('discovery_mdns_enabled')?.removeEventListener('change', window.__settingsChangeHandler);
    const rangesEl = document.getElementById('ranges_text');
    if (rangesEl) { rangesEl.removeEventListener('blur', window.__settingsChangeHandler); }
    // Note: auto_discover_checkbox and autosave_checkbox use separate handlers stored in window
    const autoDiscoverEl = document.getElementById('auto_discover_checkbox');
    if (autoDiscoverEl && window.__autoDiscoverHandler) { autoDiscoverEl.removeEventListener('change', window.__autoDiscoverHandler); }
    const autosaveEl = document.getElementById('autosave_checkbox');
    if (autosaveEl && window.__autosaveHandler) { autosaveEl.removeEventListener('change', window.__autosaveHandler); }

    // Remove "Show Anyway" toggle handlers
    const showDiscoverAnywayEl = document.getElementById('show_discover_button_anyway');
    if (showDiscoverAnywayEl && window.__showDiscoverAnywayHandler) { showDiscoverAnywayEl.removeEventListener('change', window.__showDiscoverAnywayHandler); }
    const showDiscoveredAnywayEl = document.getElementById('show_discovered_devices_anyway');
    if (showDiscoveredAnywayEl && window.__showDiscoveredAnywayHandler) { showDiscoveredAnywayEl.removeEventListener('change', window.__showDiscoveredAnywayHandler); }

    document.getElementById('passive_discovery_enabled')?.removeEventListener('change', window.__settingsChangeHandler);
    document.getElementById('discovery_live_mdns_enabled')?.removeEventListener('change', window.__settingsChangeHandler);
    document.getElementById('discovery_live_wsd_enabled')?.removeEventListener('change', window.__settingsChangeHandler);
    document.getElementById('discovery_live_ssdp_enabled')?.removeEventListener('change', window.__settingsChangeHandler);
    document.getElementById('discovery_live_snmptrap_enabled')?.removeEventListener('change', window.__settingsChangeHandler);
    document.getElementById('discovery_live_llmnr_enabled')?.removeEventListener('change', window.__settingsChangeHandler);
    document.getElementById('metrics_rescan_enabled')?.removeEventListener('change', window.__settingsChangeHandler);
    document.getElementById('ip_scanning_enabled')?.removeEventListener('change', window.__ipScanningHandler);
    document.getElementById('metrics_rescan_interval')?.removeEventListener('change', window.__settingsChangeHandler);

    document.getElementById('dev_debug_logging')?.removeEventListener('change', window.__settingsChangeHandler);
    document.getElementById('dev_dump_parse_debug')?.removeEventListener('change', window.__settingsChangeHandler);
    document.getElementById('dev_show_legacy')?.removeEventListener('change', window.__settingsChangeHandler);
    document.getElementById('dev_snmp_community')?.removeEventListener('change', window.__settingsChangeHandler);
    document.getElementById('dev_snmp_timeout')?.removeEventListener('change', window.__settingsChangeHandler);
    document.getElementById('dev_snmp_retries')?.removeEventListener('change', window.__settingsChangeHandler);
    document.getElementById('dev_discover_concurrency')?.removeEventListener('change', window.__settingsChangeHandler);
    document.getElementById('dev_asset_id_regex')?.removeEventListener('change', window.__settingsChangeHandler);
}

// Unified save for both discovery and developer settings
async function saveAllSettings(btn) {
    try {
        if (btn) { btn.disabled = true; btn.textContent = 'Applying...'; }
        // Compose discovery settings
        const discoverySettings = {
            // Master IP scanning toggle
            ip_scanning_enabled: document.getElementById('ip_scanning_enabled')?.checked ?? true,
            // IP Sources
            subnet_scan: document.getElementById('scan_local_subnet_enabled')?.checked ?? true,
            manual_ranges: document.getElementById('manual_ranges_enabled')?.checked ?? false,

            // Active Probes
            arp_enabled: document.getElementById('discovery_arp_enabled')?.checked ?? true,
            icmp_enabled: document.getElementById('discovery_icmp_enabled')?.checked ?? true,
            tcp_enabled: document.getElementById('discovery_tcp_enabled')?.checked ?? true,
            mdns_enabled: document.getElementById('discovery_mdns_enabled')?.checked ?? false,

            // Device Identification
            snmp_enabled: document.getElementById('discovery_snmp_enabled')?.checked ?? true,

            // Passive Discovery
            auto_discover_enabled: document.getElementById('auto_discover_checkbox')?.checked ?? false,
            autosave_discovered_devices: document.getElementById('autosave_checkbox')?.checked ?? false,
            show_discover_button_anyway: document.getElementById('show_discover_button_anyway')?.checked ?? false,
            show_discovered_devices_anyway: document.getElementById('show_discovered_devices_anyway')?.checked ?? false,
            passive_discovery_enabled: document.getElementById('passive_discovery_enabled')?.checked ?? false,
            auto_discover_live_mdns: document.getElementById('discovery_live_mdns_enabled')?.checked ?? true,
            auto_discover_live_wsd: document.getElementById('discovery_live_wsd_enabled')?.checked ?? true,
            auto_discover_live_ssdp: document.getElementById('discovery_live_ssdp_enabled')?.checked ?? false,
            auto_discover_live_snmptrap: document.getElementById('discovery_live_snmptrap_enabled')?.checked ?? false,
            auto_discover_live_llmnr: document.getElementById('discovery_live_llmnr_enabled')?.checked ?? false,

            // Metrics Monitoring
            metrics_rescan_enabled: document.getElementById('metrics_rescan_enabled')?.checked ?? false,
            metrics_rescan_interval_minutes: parseInt(document.getElementById('metrics_rescan_interval')?.value ?? 60)
        };

        // Include ranges text directly in unified save payload
        const rangesTextEl = document.getElementById('ranges_text');
        if (rangesTextEl) {
            discoverySettings.ranges_text = rangesTextEl.value || '';
        }

        // Compose developer settings
        const logLevel = document.getElementById('dev_debug_logging').value;
        const devSettings = {
            log_level: logLevel,
            debug_logging: (logLevel === 'debug' || logLevel === 'trace'), // backward compat
            dump_parse_debug: document.getElementById('dev_dump_parse_debug').checked,
            show_legacy: document.getElementById('dev_show_legacy').checked,
            snmp_community: document.getElementById('dev_snmp_community').value,
            snmp_timeout_ms: parseInt(document.getElementById('dev_snmp_timeout').value) || 2000,
            snmp_retries: parseInt(document.getElementById('dev_snmp_retries').value) || 1,
            discover_concurrency: parseInt(document.getElementById('dev_discover_concurrency').value) || 50,
            asset_id_regex: document.getElementById('dev_asset_id_regex').value || ''
        };

        // Compose security settings
        const securitySettings = {
            credentials_enabled: document.getElementById('enable_saved_credentials')?.checked ?? true,
            enable_http: document.getElementById('enable_http')?.checked ?? true,
            http_port: document.getElementById('http_port')?.value || '8080',
            enable_https: document.getElementById('enable_https')?.checked ?? true,
            https_port: document.getElementById('https_port')?.value || '8443',
            redirect_http_to_https: document.getElementById('redirect_http_to_https')?.checked ?? false,
            custom_cert_path: document.getElementById('custom_cert_path')?.value || '',
            custom_key_path: document.getElementById('custom_key_path')?.value || ''
        };

        const rUnified = await fetch('/settings', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ discovery: discoverySettings, developer: devSettings, security: securitySettings })
        });
        if (!rUnified.ok) {
            const t = await rUnified.text();
            throw new Error('Failed to save settings: ' + t);
        }

    console.log('All settings saved successfully');
        if (btn) {
            btn.textContent = '✓ Applied';
            setTimeout(() => { btn.textContent = 'Apply'; btn.disabled = false; }, 1500);
        }
    window.__pm_shared.showToast('Settings saved successfully', 'success');
        return Promise.resolve();
    } catch (e) {
        console.error('Save failed:', e);
        if (!btn) {
            // Autosave failed silently in background, just log it
            console.warn('Autosave failed:', e.message);
            } else {
            window.__pm_shared.showToast('Save failed: ' + e.message, 'error');
            btn.textContent = 'Apply';
            btn.disabled = false;
        }
        return Promise.reject(e);
    }
}

async function resetSettings() {
    const confirmed = await window.__pm_shared.showConfirm(
        'Reset all settings to defaults?\n\nThis will restore default settings unless they are manually configured in config.json.',
        'Reset Settings',
        true
    );
    if (!confirmed) return;
    
    fetch('/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ reset: true }) })
        .then(async r => {
            if (!r.ok) {
                const t = await r.text();
                console.error('Reset failed:', t);
                window.__pm_shared.showToast('Reset failed: ' + t, 'error');
                return;
            }
            loadSettings();
            window.__pm_shared.showToast('Settings reset successfully', 'success');
        })
        .catch(e => { console.error('Reset failed:', e); window.__pm_shared.showToast('Reset failed: ' + e.message, 'error'); });
}

// Clipboard icon is provided by shared helpers (window.__pm_shared.makeClipboardIcon)

// WebUI Modal functions
let currentWebUIURL = '';
let currentSerial = '';

function showWebUIModal(webUIURL, serial) {
    currentWebUIURL = webUIURL;
    currentSerial = serial;
    document.getElementById('webui_modal').classList.add('active');
}

function closeWebUIModal() {
    document.getElementById('webui_modal').classList.remove('active');
    currentWebUIURL = '';
    currentSerial = '';
}

function openProxyUI() {
    if (currentSerial) {
        window.open('/proxy/' + currentSerial, '_blank');
        closeWebUIModal();
    }
}

function openDirectUI() {
    if (currentWebUIURL) {
        window.open(currentWebUIURL, '_blank');
        closeWebUIModal();
    }
}

// Metrics modal (delegate to shared implementation)
function showDeviceMetricsModal(serial, preset) {
    if (!serial) return;
    if (typeof window !== 'undefined' && typeof window.showMetricsModal === 'function') {
        try {
            window.showMetricsModal({ serial, preset });
            return;
        } catch (e) {
            console.warn('shared.showMetricsModal failed', e);
        }
    }
    // Fallback: minimal alert
    window.__pm_shared.showAlert('Metrics UI not available for ' + serial, 'Metrics', false, false);
}

function closeDeviceMetricsModal() {
    const modal = document.getElementById('metrics_modal');
    if (!modal) return;
    modal.style.display = 'none';
}

// Toggle the visibility of the metrics time selector for a given container.
// targetId: optional id of the container that holds the metrics UI (e.g. 'metrics_modal_body' or 'metrics_content').
function toggleMetricsTimeSelector(targetId) {
    try {
        let container = null;
        if (targetId) container = document.getElementById(targetId);
        // Fallbacks: modal body or generic metrics content
        if (!container) container = document.getElementById('metrics_modal_body') || document.getElementById('metrics_content') || document.body;

        const selector = container.querySelector('#metrics_time_selector');
        const btn = container.querySelector('#metrics_toggle_time_btn');
        if (!selector || !btn) return;

        const nowHidden = selector.classList.toggle('hidden');
        btn.textContent = nowHidden ? 'Show time selector' : 'Hide time selector';
        btn.setAttribute('aria-expanded', (!nowHidden).toString());
    } catch (e) {
        console.warn('toggleMetricsTimeSelector failed', e);
    }
}

// Initialize UI after all functions are defined
updatePrinters();
loadSavedRanges();
showTab('devices'); // Show devices tab by default

// Clear search filter boxes on page load
const discoveredSearchInput = document.getElementById('discovered_search');
if (discoveredSearchInput) {
    discoveredSearchInput.value = '';
}
const savedSearchInput = document.getElementById('saved_search');
if (savedSearchInput) {
    savedSearchInput.value = '';
}

// Initialize time filter slider display
updateTimeFilter(13); // Start at "All Time"

// Check auto-save preference
const autoSave = localStorage.getItem('settings_autosave') === 'true';
if (autoSave) {
    document.getElementById('settings_autosave').checked = true;
    toggleAutoSave();
}

