// Global settings state
let globalSettings = {
    security: {
        credentials_enabled: true // Default: enabled
    }
};

// Toast notification system
function showToast(message, type = 'success', duration = 3000) {
    const container = document.getElementById('toast_container');
    if (!container) return;
    
    const toast = document.createElement('div');
    toast.className = `toast toast-${type}`;
    
    const icons = {
        success: '✓',
        error: '✗',
        info: 'ℹ'
    };
    
    toast.innerHTML = `
        <span class="toast-icon">${icons[type] || icons.info}</span>
        <span class="toast-message">${message}</span>
    `;
    
    container.appendChild(toast);
    
    // Auto-remove after duration
    setTimeout(() => {
        toast.classList.add('toast-hiding');
        setTimeout(() => {
            if (toast.parentNode) {
                toast.parentNode.removeChild(toast);
            }
        }, 300); // Match animation duration
    }, duration);
}

// Confirmation modal system (replaces browser confirm dialogs)
function showConfirm(message, title = 'Confirm Action', isDangerous = false) {
    return new Promise((resolve) => {
        const modal = document.getElementById('confirm_modal');
        const titleEl = document.getElementById('confirm_modal_title');
        const messageEl = document.getElementById('confirm_modal_message');
        const confirmBtn = document.getElementById('confirm_modal_confirm');
        const cancelBtn = document.getElementById('confirm_modal_cancel');
        const closeX = document.getElementById('confirm_modal_close_x');
        
        if (!modal || !titleEl || !messageEl || !confirmBtn || !cancelBtn) {
            // Fallback to browser confirm if modal not available
            resolve(confirm(message));
            return;
        }
        
        // Set content
        titleEl.textContent = title;
        messageEl.textContent = message;
        
        // Style confirm button based on danger level
        confirmBtn.className = isDangerous ? 
            'modal-button modal-button-danger' : 
            'modal-button modal-button-primary';
        
        // Show modal
        modal.style.display = 'flex';
        
        // Handle confirm
        const onConfirm = () => {
            cleanup();
            resolve(true);
        };
        
        // Handle cancel
        const onCancel = () => {
            cleanup();
            resolve(false);
        };
        
        // Cleanup function
        const cleanup = () => {
            modal.style.display = 'none';
            confirmBtn.removeEventListener('click', onConfirm);
            cancelBtn.removeEventListener('click', onCancel);
            if (closeX) closeX.removeEventListener('click', onCancel);
            modal.removeEventListener('click', onBackdropClick);
        };
        
        // Backdrop click closes modal
        const onBackdropClick = (e) => {
            if (e.target === modal) {
                onCancel();
            }
        };
        
        // Attach event listeners
        confirmBtn.addEventListener('click', onConfirm);
        cancelBtn.addEventListener('click', onCancel);
        if (closeX) closeX.addEventListener('click', onCancel);
        modal.addEventListener('click', onBackdropClick);
    });
}

// Check for database rotation warning on page load
async function checkDatabaseRotationWarning() {
    try {
        const response = await fetch('/database/rotation_warning');
        if (!response.ok) return;
        
        const data = await response.json();
        if (data.rotated) {
            const message = `The database was rotated due to a migration failure on ${data.rotated_at || 'recently'}.\n\nA fresh database has been created and the old database has been backed up to:\n${data.backup_path || 'unknown location'}\n\nAll discovered devices and historical metrics data from the previous database are not available in the current session. If you need to recover data, you can manually restore the backup file.\n\nClick OK to acknowledge this warning.`;
            
            const confirmed = await showConfirm(
                message,
                'Database Rotation Notice',
                false // Not dangerous, just informational
            );
            
            if (confirmed) {
                // Clear the rotation warning flag so it doesn't show again
                await fetch('/database/rotation_warning', { 
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' }
                });
            }
        }
    } catch (err) {
        console.error('Failed to check rotation warning:', err);
        // Non-fatal - don't interrupt page load
    }
}

// Run a one-off scan using the current ranges textarea (without saving).
// Posts JSON to /scan so the server will use the new pipeline (liveness->detection->deep-scan).
// client-side state for device table sorting
let deviceSortField = 'IP';
let deviceSortDir = 1; // 1 asc, -1 desc

function setDeviceSort(field) {
    if (deviceSortField === field) deviceSortDir = -deviceSortDir; else { deviceSortField = field; deviceSortDir = 1; }
    updatePrinters();
}

function exportDevicesCSV() {
    fetch('/devices/discovered').then(r => r.json()).then(d => {
        if (!Array.isArray(d) || d.length === 0) { console.log('No devices to export'); return; }
        const search = (document.getElementById('device_search') || {}).value || '';
        const rows = [];
        const headers = ['IP', 'Hostname', 'Manufacturer', 'Model', 'Serial', 'Subnet', 'Gateway', 'PageCount', 'Mono', 'Color'];
        rows.push(headers.join(','));
        d.forEach(p => {
            const combined = [p.ip, p.hostname, p.manufacturer, p.model, p.serial, p.subnet_mask, p.gateway, p.page_count || '', p.mono_impressions || p.total_mono_impressions || '', p.color_impressions || ''];
            const line = combined.map(v => '"' + String(v || '').replace(/"/g, '""') + '"').join(',');
            if (!search || JSON.stringify(combined).toLowerCase().includes(search.toLowerCase())) rows.push(line);
        });
        const blob = new Blob([rows.join('\n')], { type: 'text/csv' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a'); a.href = url; a.download = 'devices.csv'; document.body.appendChild(a); a.click(); a.remove(); URL.revokeObjectURL(url);
    }).catch(e => { console.error('Export failed:', e); showToast('Export failed: ' + e.message, 'error'); });
}

// Render mini vertical toner bars (compact color-coded display)
function renderMiniTonerBars(tonerLevels) {
    if (!tonerLevels || Object.keys(tonerLevels).length === 0) return '<span style="color:var(--muted)">—</span>';
    let html = '<div style="display:flex;gap:3px;align-items:flex-end;height:24px">';
    for (let k in tonerLevels) {
        const v = Number(tonerLevels[k]);
        const pct = isNaN(v) ? 0 : Math.max(0, Math.min(100, v));
        const keyLower = k.toLowerCase();
        let color = '#888'; // default gray for unknown types
        if (keyLower.includes('black') || keyLower === 'k') color = '#111';
        else if (keyLower.includes('cyan') || keyLower === 'c') color = '#0097a7';
        else if (keyLower.includes('magenta') || keyLower === 'm') color = '#c2185b';
        else if (keyLower.includes('yellow') || keyLower === 'y') color = '#fbc02d';
        const height = Math.max(2, (pct * 20) / 100); // 20px max height
        html += '<div title="' + k + ': ' + pct + '%" style="width:8px;height:' + height + 'px;background:' + color + ';border-radius:2px 2px 0 0;border:1px solid rgba(0,0,0,0.3)"></div>';
    }
    html += '</div>';
    return html;
}

// Filter table rows by search text
function filterTable(tableId, searchText) {
    const container = document.getElementById(tableId);
    if (!container) return;

    const table = container.querySelector('table');
    if (!table) return;

    const tbody = table.querySelector('tbody');
    if (!tbody) return;

    const searchLower = searchText.toLowerCase().trim();
    const rows = tbody.querySelectorAll('tr');

    rows.forEach(row => {
        if (!searchLower) {
            row.style.display = '';
            return;
        }

        const text = row.textContent.toLowerCase();
        row.style.display = text.includes(searchLower) ? '' : 'none';
    });
}

// Centralized copy to clipboard function with toast notification
// iconElement: optional icon element to add visual feedback
// customMessage: optional custom toast message (if not provided, shows "Copied: <text>")
async function copyToClipboard(text, iconElement, customMessage) {
    if (!text || text === 'N/A' || text === 'Not Set') return;
    
    try {
        if (navigator.clipboard && window.isSecureContext) {
            await navigator.clipboard.writeText(text);
        } else {
            // Fallback for non-secure contexts
            const ta = document.createElement('textarea');
            ta.value = text;
            ta.setAttribute('readonly', '');
            ta.style.position = 'fixed';
            ta.style.left = '-9999px';
            document.body.appendChild(ta);
            ta.select();
            document.execCommand('copy');
            ta.remove();
        }
        
        // Visual feedback on the icon if provided
        if (iconElement) {
            iconElement.classList.add('copied');
            setTimeout(() => {
                iconElement.classList.remove('copied');
            }, 1500);
        }
        
        // Show toast notification
        if (customMessage) {
            showToast(customMessage, 'success', 2000);
        } else {
            const displayText = text.length > 40 ? text.substring(0, 40) + '...' : text;
            showToast(`Copied: ${displayText}`, 'success', 2000);
        }
    } catch (err) {
        console.error('Copy failed:', err);
        showToast('Failed to copy to clipboard', 'error');
        throw err;
    }
}

// Get status indicator based on toner levels
function getStatusIndicator(toners) {
    if (!toners || typeof toners !== 'object') {
        return '<span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:#586e75;margin-right:6px" title="Unknown status"></span>';
    }

    let minLevel = 100;
    for (const color in toners) {
        const level = toners[color] || 0;
        if (level < minLevel) minLevel = level;
    }

    if (minLevel <= 10) {
        return '<span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:#dc322f;margin-right:6px" title="Critical: ' + minLevel + '% toner"></span>';
    } else if (minLevel <= 20) {
        return '<span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:#b58900;margin-right:6px" title="Low: ' + minLevel + '% toner"></span>';
    } else {
        return '<span style="display:inline-block;width:8px;height:8px;border-radius:50%;background:#859900;margin-right:6px" title="Good: ' + minLevel + '% toner"></span>';
    }
}

// Sort table by column
function sortTable(tableId, columnIndex, dataType = 'string') {
    const container = document.getElementById(tableId);
    if (!container) return;

    const table = container.querySelector('table');
    if (!table) return;

    const tbody = table.querySelector('tbody');
    if (!tbody) return;

    const rows = Array.from(tbody.querySelectorAll('tr'));

    // Get current sort direction from data attribute
    const currentSort = tbody.getAttribute('data-sort-col');
    const currentDir = tbody.getAttribute('data-sort-dir') || 'asc';
    const newDir = (currentSort == columnIndex && currentDir === 'asc') ? 'desc' : 'asc';

    rows.sort((a, b) => {
        const aText = a.cells[columnIndex]?.textContent.trim() || '';
        const bText = b.cells[columnIndex]?.textContent.trim() || '';

        let aVal = aText;
        let bVal = bText;

        if (dataType === 'number') {
            aVal = parseFloat(aText.replace(/[^0-9.-]/g, '')) || 0;
            bVal = parseFloat(bText.replace(/[^0-9.-]/g, '')) || 0;
        } else if (dataType === 'ip') {
            aVal = aText.split('.').map(n => parseInt(n) || 0);
            bVal = bText.split('.').map(n => parseInt(n) || 0);
            for (let i = 0; i < 4; i++) {
                if (aVal[i] !== bVal[i]) return newDir === 'asc' ? aVal[i] - bVal[i] : bVal[i] - aVal[i];
            }
            return 0;
        }

        if (dataType === 'number') {
            return newDir === 'asc' ? aVal - bVal : bVal - aVal;
        }

        return newDir === 'asc' ? aVal.localeCompare(bVal) : bVal.localeCompare(aVal);
    });

    // Update tbody
    rows.forEach(row => tbody.appendChild(row));

    // Store sort state
    tbody.setAttribute('data-sort-col', columnIndex);
    tbody.setAttribute('data-sort-dir', newDir);

    // Update header indicators
    const headers = table.querySelectorAll('thead th');
    headers.forEach((th, idx) => {
        th.innerHTML = th.innerHTML.replace(/ [▲▼]/g, '');
        if (idx === columnIndex) {
            th.innerHTML += newDir === 'asc' ? ' ▲' : ' ▼';
        }
    });
}

// Render discovered device card
function renderDiscoveredCard(device, isSaved) {
    const saveButton = isSaved
        ? '<button class="saved" disabled>Saved</button>'
        : '<button class="save" onclick="saveDiscoveredDevice(\'' + device.ip + '\')">Save</button>';

    const clipIcon = makeClipboardIcon();
    const ipVal = device.ip || 'N/A';
    const macVal = device.mac || 'N/A';
    const serialVal = device.serial || 'No Serial';

    return `<div class="device-card card-entering" data-make="${device.manufacturer || ''}" data-model="${device.model || ''}" data-ip="${device.ip || ''}" data-serial="${device.serial || ''}">
        <div class="device-card-header">
            <div>
                <h5 class="device-card-title">${device.manufacturer || 'Unknown'} ${device.model || ''}</h5>
                <p class="device-card-subtitle copyable" onclick="copyToClipboard('${serialVal}', this.querySelector('.clipboard-icon'))">
                    ${serialVal}${clipIcon}
                </p>
            </div>
        </div>
        <div class="device-card-info">
            <div class="device-card-row">
                <span class="device-card-label">IP Address</span>
                <span class="device-card-value copyable" onclick="copyToClipboard('${ipVal}', this.querySelector('.clipboard-icon'))">
                    ${ipVal}${clipIcon}
                </span>
            </div>
            <div class="device-card-row">
                <span class="device-card-label">MAC Address</span>
                <span class="device-card-value copyable" onclick="copyToClipboard('${macVal}', this.querySelector('.clipboard-icon'))">
                    ${macVal}${clipIcon}
                </span>
            </div>
            <div class="device-card-row">
                <span class="device-card-label">Location</span>
                <span class="device-card-value">${device.location || 'Not Set'}</span>
            </div>
            <div class="device-card-row">
                <span class="device-card-label">Asset #</span>
                <span class="device-card-value">${device.asset_number || 'Not Set'}</span>
            </div>
        </div>
        <div class="device-card-actions">
            <button onclick="showPrinterDetails('${device.ip}','discovered')">View</button>
            ${saveButton}
        </div>
    </div>`;
}

// Render saved device card
// Helper function to render capability badges
function renderCapabilities(device) {
    const capabilities = [];
    const toners = device.toner_levels || {};
    const modelLower = (device.model || '').toLowerCase();
    const vendor = (device.manufacturer || '').toLowerCase();

    // Detect which colors are present from toner levels
    const hasCyan = device.toner_level_cyan || toners.cyan || toners.Cyan;
    const hasMagenta = device.toner_level_magenta || toners.magenta || toners.Magenta;
    const hasYellow = device.toner_level_yellow || toners.yellow || toners.Yellow;
    const hasBlack = device.toner_level_black || toners.black || toners.Black ||
        (device.mono_impressions && device.mono_impressions > 0);

    // Enhanced color detection with model-based inference
    let hasColor = hasCyan || hasMagenta || hasYellow ||
        (device.color_impressions && device.color_impressions > 0);

    // Epson color models
    if (vendor.includes('epson')) {
        // AM-C = Advanced MFP Color, WF-C = WorkForce Color, CW-C = ColorWorks
        if (modelLower.includes('am-c') || modelLower.includes('wf-c') || modelLower.includes('cw-c')) {
            hasColor = true;
        }
        // WF-M = WorkForce Mono (explicitly mono)
        if (modelLower.includes('wf-m')) {
            hasColor = false;
        }
    }

    // Kyocera color models (ending in 'ci' = color imaging)
    if (vendor.includes('kyocera')) {
        if (modelLower.endsWith('ci') || modelLower.includes('ci ')) {
            hasColor = true;
        }
    }

    const hasDuplex = device.duplex_supported === true;

    // Enhanced MFP capability detection
    let hasCopy = modelLower.includes('mfp') || modelLower.includes('multifunction') ||
        modelLower.includes('taskalfa') || // Kyocera MFPs
        modelLower.includes('am-c') || // Epson Advanced MFP
        (device.copy_impressions && device.copy_impressions > 0);
    
    let hasScan = hasCopy || (device.scan_count && device.scan_count > 0);
    let hasFax = modelLower.includes('fax') || /m\d+f/.test(modelLower) ||
        (device.fax_pages && device.fax_pages > 0);
    
    // Detect laser vs inkjet
    let isLaser = false;
    let isInkjet = false;
    
    // Check toner vs ink in consumable names
    const consumableStr = JSON.stringify(toners).toLowerCase();
    if (consumableStr.includes('toner') || consumableStr.includes('drum')) {
        isLaser = true;
    } else if (consumableStr.includes('ink')) {
        isInkjet = true;
    }
    
    // Model-based technology detection
    if (!isLaser && !isInkjet) {
        // Epson - primarily inkjet manufacturer (strong default)
        if (vendor.includes('epson')) {
            // AcuLaser is the ONLY laser line (discontinued)
            if (modelLower.includes('aculaser') || modelLower.includes('al-')) {
                isLaser = true;
            } else {
                // All other Epson models are inkjet
                isInkjet = true;
            }
        }
        // Kyocera (primarily laser)
        if (vendor.includes('kyocera')) {
            isLaser = true;
        }
        // HP
        if (vendor.includes('hp') || vendor.includes('hewlett')) {
            if (modelLower.includes('laserjet')) {
                isLaser = true;
            } else if (modelLower.includes('officejet') || modelLower.includes('deskjet') || 
                       modelLower.includes('envy') || modelLower.includes('pagewide')) {
                isInkjet = true;
            }
        }
        // Canon
        if (vendor.includes('canon')) {
            if (modelLower.includes('imageclass')) {
                isLaser = true;
            } else if (modelLower.includes('pixma') || modelLower.includes('maxify')) {
                isInkjet = true;
            }
        }
        // Brother
        if (vendor.includes('brother')) {
            if (modelLower.includes('hl-l') || modelLower.includes('dcp-l') || modelLower.includes('mfc-l')) {
                isLaser = true;
            } else if (modelLower.includes('hl-j') || modelLower.includes('dcp-j') || modelLower.includes('mfc-j')) {
                isInkjet = true;
            }
        }
    }

    // Build color badge with CMYK indicators
    if (hasColor || hasBlack) {
        let colorHTML = '<span class="capability-badge capability-color" title="';
        let colorList = [];
        if (hasCyan) colorList.push('Cyan');
        if (hasMagenta) colorList.push('Magenta');
        if (hasYellow) colorList.push('Yellow');
        if (hasBlack) colorList.push('Black');
        colorHTML += colorList.join(', ') + '">';

        // Show individual color dots
        colorHTML += '<span class="color-indicators">';
        if (hasCyan) colorHTML += '<span class="color-dot color-cyan"></span>';
        if (hasMagenta) colorHTML += '<span class="color-dot color-magenta"></span>';
        if (hasYellow) colorHTML += '<span class="color-dot color-yellow"></span>';
        if (hasBlack) colorHTML += '<span class="color-dot color-black"></span>';
        colorHTML += '</span>';

        colorHTML += hasColor ? 'Color' : 'Mono';
        colorHTML += '</span>';
        capabilities.push(colorHTML);
    }
    
    // Technology badges (laser vs inkjet)
    if (isLaser) {
        capabilities.push('<span class="capability-badge capability-laser" title="Laser/Toner technology">Laser</span>');
    } else if (isInkjet) {
        capabilities.push('<span class="capability-badge capability-inkjet" title="Inkjet technology">Inkjet</span>');
    }

    if (hasCopy) {
        capabilities.push('<span class="capability-badge capability-copy" title="Copy function">Copy</span>');
    }
    if (hasScan) {
        capabilities.push('<span class="capability-badge capability-scan" title="Scan function">Scan</span>');
    }
    if (hasFax) {
        capabilities.push('<span class="capability-badge capability-fax" title="Fax function">Fax</span>');
    }
    if (hasDuplex) {
        capabilities.push('<span class="capability-badge capability-duplex" title="Duplex (2-sided) printing">Duplex</span>');
    }

    return capabilities.length > 0 ? '<div class="capabilities-container">' + capabilities.join('') + '</div>' : '';
}

function renderSavedCard(item) {
    const device = item.printer_info || {};
    const serial = item.serial || '';
    const toners = device.toner_levels || {};
    const lifeCount = device.page_count || device.total_mono_impressions || device.mono_impressions || 0;
    const colorCount = device.color_impressions || 0;
    const monoCount = device.mono_impressions || 0;

    // Placeholder for usage graph - will be populated with real data after render
    const graphId = 'usage-graph-' + serial.replace(/[^a-zA-Z0-9]/g, '_');
    let usageGraphHTML = '<div id="' + graphId + '" class="usage-graph-container">' +
        '<div class="usage-graph-no-data">Loading usage data...</div>' +
        '</div>';

    // Render consumables (toner, drum, fuser, etc) with icons
    let consumablesHTML = '';
    const tonerColors = {
        'black': { bg: '#2c2c2c', icon: '⬛' },
        'cyan': { bg: '#00bcd4', icon: '🔷' },
        'magenta': { bg: '#e91e63', icon: '🔶' },
        'yellow': { bg: '#ffc107', icon: '🟨' }
    };

    for (const color in toners) {
        const level = toners[color] || 0;
        const colorInfo = tonerColors[color.toLowerCase()] || { bg: '#666', icon: '⬜' };
        const lowLevel = level < 20;
        consumablesHTML += '<div class="consumable-item">' +
            '<div class="consumable-icon" style="background:' + colorInfo.bg + '20">' + colorInfo.icon + '</div>' +
            '<span class="consumable-label">' + color.charAt(0).toUpperCase() + color.slice(1) + '</span>' +
            '<div class="consumable-bar">' +
            '<div class="consumable-bar-fill" style="width:' + level + '%;background:' + colorInfo.bg + (lowLevel ? ';opacity:0.5' : '') + '">' + (level > 15 ? level + '%' : '') + '</div>' +
            '</div>' +
            '<span style="min-width:45px;text-align:right;font-family:monospace;color:var(--text);' + (lowLevel ? 'color:var(--highlight);font-weight:600' : '') + '">' + (level <= 15 ? level + '%' : '') + '</span>' +
            '</div>';
    }

    // Add other consumables if available
    if (device.drum_life) {
        consumablesHTML += '<div class="consumable-item">' +
            '<div class="consumable-icon" style="background:rgba(108,117,125,0.2)">🥁</div>' +
            '<span class="consumable-label">Drum</span>' +
            '<div class="consumable-bar">' +
            '<div class="consumable-bar-fill" style="width:' + device.drum_life + '%;background:#6c757d">' + (device.drum_life > 15 ? device.drum_life + '%' : '') + '</div>' +
            '</div>' +
            '<span style="min-width:45px;text-align:right;font-family:monospace;color:var(--text)">' + (device.drum_life <= 15 ? device.drum_life + '%' : '') + '</span>' +
            '</div>';
    }
    if (device.fuser_life) {
        consumablesHTML += '<div class="consumable-item">' +
            '<div class="consumable-icon" style="background:rgba(255,87,34,0.2)">🔥</div>' +
            '<span class="consumable-label">Fuser</span>' +
            '<div class="consumable-bar">' +
            '<div class="consumable-bar-fill" style="width:' + device.fuser_life + '%;background:#ff5722">' + (device.fuser_life > 15 ? device.fuser_life + '%' : '') + '</div>' +
            '</div>' +
            '<span style="min-width:45px;text-align:right;font-family:monospace;color:var(--text)">' + (device.fuser_life <= 15 ? device.fuser_life + '%' : '') + '</span>' +
            '</div>';
    }

    // Web UI button (opens modal)
    let webUIButton = '';
    if (item.web_ui_url) {
        webUIButton = '<button class="primary" style="font-size:12px" onclick="showWebUIModal(\'' + item.web_ui_url + '\', \'' + serial + '\')">WebUI</button>';
    }

    const consumablesSection = consumablesHTML ? '<div class="saved-device-card-section">' +
        '<div class="saved-device-card-section-title">Consumables</div>' +
        '<div class="consumable-container">' + consumablesHTML + '</div>' +
        '</div>' : '';

    const usageSection = '<div class="saved-device-card-inner-panel">' +
        '<div class="saved-device-card-section">' +
        '<div class="saved-device-card-section-title">Usage Trend (7 Days)</div>' +
        usageGraphHTML +
        '</div>' +
        '</div>';

    // Count section with color/mono breakdown
    let countsHTML = '<div class="saved-device-card-row">' +
        '<span class="saved-device-card-label">Total Pages</span>' +
        '<span class="saved-device-card-value">' + lifeCount.toLocaleString() + '</span>' +
        '</div>';
    if (monoCount > 0) {
        countsHTML += '<div class="saved-device-card-row">' +
            '<span class="saved-device-card-label">Mono</span>' +
            '<span class="saved-device-card-value">' + monoCount.toLocaleString() + '</span>' +
            '</div>';
    }
    if (colorCount > 0) {
        countsHTML += '<div class="saved-device-card-row">' +
            '<span class="saved-device-card-label">Color</span>' +
            '<span class="saved-device-card-value">' + colorCount.toLocaleString() + '</span>' +
            '</div>';
    }

    const clipIcon = makeClipboardIcon();
    const ipVal = device.ip || 'N/A';
    const macVal = device.mac || '';
    const capabilitiesHTML = renderCapabilities(device);

    const deviceKey = serial || device.ip || '';
    
    return `<div class="saved-device-card card-entering" data-device-key="${deviceKey}" data-make="${device.manufacturer || ''}" data-model="${device.model || ''}" data-ip="${device.ip || ''}" data-serial="${serial}">
        <div class="saved-device-card-header">
            <div class="saved-device-card-main">
                <h5 class="saved-device-card-title">${device.manufacturer || 'Unknown'} ${device.model || ''}</h5>
                ${capabilitiesHTML}
                <p class="saved-device-card-subtitle copyable" onclick="copyToClipboard('${serial}', this.querySelector('.clipboard-icon'))">Serial: ${serial}${clipIcon}</p>
                <p class="saved-device-card-subtitle">
                    <span class="copyable" onclick="copyToClipboard('${ipVal}', this.querySelector('.clipboard-icon'))" style="display:inline-flex;align-items:center;gap:4px;">IP: ${ipVal}${clipIcon}</span>
                    ${macVal ? `<span class="copyable" onclick="copyToClipboard('${macVal}', this.querySelector('.clipboard-icon'))" style="display:inline-flex;align-items:center;gap:4px;margin-left:8px;"> • MAC: ${macVal}${clipIcon}</span>` : ''}
                </p>
            </div>
            <div style="display:flex;gap:8px;flex-wrap:wrap;">
                ${webUIButton}
                <button onclick="showPrinterDetails('${device.ip}','saved')">Details</button>
                <button class="delete" onclick="deleteSavedDevice('${serial}')">Delete</button>
            </div>
        </div>
        <div class="saved-device-card-grid">
            <div class="saved-device-card-inner-panel">
                <div class="saved-device-card-section">
                    <div class="saved-device-card-section-title">Device Info</div>
                    <div class="saved-device-card-row">
                        <span class="saved-device-card-label">Asset #</span>
                        <span class="saved-device-card-value editable-field" onclick="editField('${serial}','asset_number','${item.asset_number || ''}',this)">${item.asset_number || '(click to add)'}</span>
                    </div>
                    <div class="saved-device-card-row">
                        <span class="saved-device-card-label">Location</span>
                        <span class="saved-device-card-value editable-field" onclick="editField('${serial}','location','${item.location || ''}',this)">${item.location || '(click to add)'}</span>
                    </div>
                    ${countsHTML}
                </div>
                ${consumablesSection}
            </div>
            ${usageSection}
        </div>
    </div>`;
}

// Load and render usage graph for a saved device card
async function loadUsageGraph(serial) {
    const graphId = 'usage-graph-' + serial.replace(/[^a-zA-Z0-9]/g, '_');
    const container = document.getElementById(graphId);
    if (!container) return;

    try {
        // Fetch 7 days of metrics
        const res = await fetch('/api/devices/metrics/history?serial=' + encodeURIComponent(serial) + '&period=week');
        if (!res.ok) {
            container.innerHTML = '<div class="usage-graph-no-data">No usage data available</div>';
            return;
        }

        const history = await res.json();
        if (!history || history.length === 0) {
            container.innerHTML = '<div class="usage-graph-no-data">No usage data yet</div>';
            return;
        }

        // Group by day and calculate deltas
        const dailyData = {};
        let prevCount = 0;

        history.forEach((snap, i) => {
            const count = snap.page_count || 0;
            const date = new Date(snap.timestamp);
            const dayKey = date.toISOString().split('T')[0];

            // Calculate delta from previous snapshot
            const delta = i > 0 ? Math.max(0, count - prevCount) : 0;

            if (!dailyData[dayKey]) {
                dailyData[dayKey] = { date: date, pages: 0, count: count };
            }
            dailyData[dayKey].pages += delta;
            dailyData[dayKey].count = Math.max(dailyData[dayKey].count, count);

            prevCount = count;
        });

        // Convert to array and sort by date
        const dataPoints = Object.values(dailyData)
            .sort((a, b) => a.date - b.date)
            .filter(d => d.pages > 0); // Only show days with activity

        if (dataPoints.length === 0) {
            container.innerHTML = '<div class="usage-graph-no-data">No print activity in past 7 days</div>';
            return;
        }

        // Render SVG line graph
        renderUsageGraphSVG(container, dataPoints, serial);

    } catch (err) {
        console.error('Failed to load usage graph:', err);
        container.innerHTML = '<div class="usage-graph-no-data">Failed to load data</div>';
    }
}

// Render SVG line graph with 3D feel
function renderUsageGraphSVG(container, dataPoints, serial) {
    const width = container.offsetWidth - 16; // Account for padding
    const height = 80;
    const padding = { top: 10, right: 10, bottom: 10, left: 10 };
    const graphWidth = width - padding.left - padding.right;
    const graphHeight = height - padding.top - padding.bottom;

    // Find max value for scaling
    const maxPages = Math.max(...dataPoints.map(d => d.pages), 1);

    // Generate SVG path for line and area
    let linePath = '';
    let areaPath = '';
    const points = [];

    dataPoints.forEach((d, i) => {
        const x = padding.left + (i / (dataPoints.length - 1)) * graphWidth;
        const y = padding.top + graphHeight - (d.pages / maxPages) * graphHeight;
        points.push({ x, y, data: d });

        if (i === 0) {
            linePath += 'M ' + x + ' ' + y;
            areaPath += 'M ' + x + ' ' + (padding.top + graphHeight) + ' L ' + x + ' ' + y;
        } else {
            linePath += ' L ' + x + ' ' + y;
            areaPath += ' L ' + x + ' ' + y;
        }
    });

    // Close area path
    const lastX = padding.left + graphWidth;
    areaPath += ' L ' + lastX + ' ' + (padding.top + graphHeight) + ' Z';

    // Generate grid lines (3 horizontal lines)
    let gridLines = '';
    for (let i = 0; i <= 2; i++) {
        const y = padding.top + (i / 2) * graphHeight;
        gridLines += '<line class="usage-graph-grid-line" x1="' + padding.left + '" y1="' + y + '" x2="' + (padding.left + graphWidth) + '" y2="' + y + '"/>';
    }

    // Generate point circles
    let circlesHTML = '';
    points.forEach(p => {
        const title = p.data.date.toLocaleDateString() + ': ' + p.data.pages.toLocaleString() + ' pages';
        circlesHTML += '<circle class="usage-graph-point" cx="' + p.x + '" cy="' + p.y + '" r="3.5" title="' + title + '"><title>' + title + '</title></circle>';
    });

    // Format date labels
    const startDate = dataPoints[0].date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
    const endDate = dataPoints[dataPoints.length - 1].date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });

    container.innerHTML =
        '<svg class="usage-graph-svg" viewBox="0 0 ' + width + ' ' + height + '" preserveAspectRatio="none">' +
        '<defs>' +
        '<linearGradient id="usage-gradient-' + serial.replace(/[^a-zA-Z0-9]/g, '_') + '" x1="0%" y1="0%" x2="0%" y2="100%">' +
        '<stop offset="0%" style="stop-color:var(--accent);stop-opacity:0.6"/>' +
        '<stop offset="100%" style="stop-color:var(--accent);stop-opacity:0.1"/>' +
        '</linearGradient>' +
        '</defs>' +
        gridLines +
        '<path class="usage-graph-area" d="' + areaPath + '" fill="url(#usage-gradient-' + serial.replace(/[^a-zA-Z0-9]/g, '_') + ')"/>' +
        '<path class="usage-graph-line" d="' + linePath + '"/>' +
        circlesHTML +
        '</svg>' +
        '<div class="usage-graph-labels">' +
        '<span>' + startDate + '</span>' +
        '<span>' + endDate + '</span>' +
        '</div>';
}

// Time filter values: slider position to minutes mapping
const timeFilterValues = [1, 2, 5, 10, 15, 30, 60, 120, 180, 360, 720, 1440, 4320, 0]; // 0 = all time
const timeFilterLabels = ['1m', '2m', '5m', '10m', '15m', '30m', '1h', '2h', '3h', '6h', '12h', '24h', '3d', 'All Time'];

// Update time filter display and slider gradient
function updateTimeFilter(value) {
    const index = parseInt(value);
    const minutes = timeFilterValues[index];
    const label = timeFilterLabels[index];
    document.getElementById('time_filter_value').textContent = label;

    // Update slider gradient based on position
    const slider = document.getElementById('time_slider');
    const percent = (index / (timeFilterValues.length - 1)) * 100;
    slider.style.background = 'linear-gradient(to right, var(--accent) 0%, var(--accent) ' + percent + '%, var(--bg) ' + percent + '%)';
}

// Get current time filter in minutes (0 = all time)
function getTimeFilterMinutes() {
    const slider = document.getElementById('time_slider');
    const index = parseInt(slider.value);
    return timeFilterValues[index];
}

// Toggle time filter visibility with animation
function toggleAdvancedSettings() {
    const checkbox = document.getElementById('settings_advanced_toggle');
    const advancedElements = document.querySelectorAll('.advanced-setting');
    const isVisible = checkbox.checked;

    advancedElements.forEach(el => {
        if (isVisible) {
            // Show with smooth animation
            // Check if element is a mini-toggle-container (needs flex) or regular element (needs block)
            const displayValue = el.classList.contains('mini-toggle-container') ? 'flex' : '';
            el.style.display = displayValue;
            el.style.maxHeight = '0';
            el.style.opacity = '0';
            el.style.overflow = 'hidden';
            el.style.marginBottom = '0';

            // Get natural height
            const naturalHeight = el.scrollHeight;

            setTimeout(() => {
                el.style.transition = 'max-height 0.4s cubic-bezier(0.4, 0, 0.2, 1), opacity 0.4s ease, margin-bottom 0.4s ease';
                el.style.maxHeight = naturalHeight + 'px';
                el.style.opacity = '1';
                el.style.marginBottom = '16px';
            }, 10);

            // Remove max-height after animation to allow dynamic resizing
            setTimeout(() => {
                el.style.maxHeight = '';
                el.style.overflow = '';
            }, 450);
        } else {
            // Hide with smooth animation
            const currentHeight = el.scrollHeight;
            el.style.maxHeight = currentHeight + 'px';
            el.style.overflow = 'hidden';

            setTimeout(() => {
                el.style.transition = 'max-height 0.4s cubic-bezier(0.4, 0, 0.2, 1), opacity 0.4s ease, margin-bottom 0.4s ease';
                el.style.maxHeight = '0';
                el.style.opacity = '0';
                el.style.marginBottom = '0';
            }, 10);

            setTimeout(() => {
                el.style.display = 'none';
            }, 450);
        }
    });

    // Store preference in localStorage
    localStorage.setItem('settingsAdvancedVisible', isVisible ? 'true' : 'false');
}

// Filter discovered cards
function filterDiscoveredCards(searchTerm) {
    const cards = document.querySelectorAll('#discovered_devices_cards .device-card');
    const term = searchTerm.toLowerCase();
    cards.forEach(card => {
        const make = card.getAttribute('data-make').toLowerCase();
        const model = card.getAttribute('data-model').toLowerCase();
        const ip = card.getAttribute('data-ip').toLowerCase();
        const serial = card.getAttribute('data-serial').toLowerCase();
        const matches = make.includes(term) || model.includes(term) || ip.includes(term) || serial.includes(term);
        card.style.display = matches ? '' : 'none';
    });
}

// Filter saved cards
function filterSavedCards(searchTerm) {
    const cards = document.querySelectorAll('#saved_devices_cards .saved-device-card');
    const term = searchTerm.toLowerCase();
    cards.forEach(card => {
        const make = card.getAttribute('data-make').toLowerCase();
        const model = card.getAttribute('data-model').toLowerCase();
        const ip = card.getAttribute('data-ip').toLowerCase();
        const serial = card.getAttribute('data-serial').toLowerCase();
        const matches = make.includes(term) || model.includes(term) || ip.includes(term) || serial.includes(term);
        card.style.display = matches ? '' : 'none';
    });
}

function showDiscoveringCard(data) {
    // Show a progressive discovery card that fills in as we gather information
    const discoveredContainer = document.getElementById('discovered_devices_cards');
    if (!discoveredContainer) return;

    const ip = data.ip || 'Unknown IP';
    const serial = data.serial || '';
    const method = data.method || '';
    const status = data.status || 'discovering';

    // Create or update card with unique ID based on IP
    const cardId = 'discovering-' + ip.replace(/\./g, '-');
    let card = document.getElementById(cardId);

    if (!card) {
        // Create new card
        card = document.createElement('div');
        card.id = cardId;
        card.className = 'printer-card discovering';
        card.style.opacity = '0.7';
        card.style.border = '2px dashed var(--accent)';
        discoveredContainer.insertBefore(card, discoveredContainer.firstChild);
    }

    // Build card content progressively
    let cardHtml = '<div class="printer-card-header" style="border-bottom:1px solid var(--border)">';
    cardHtml += '<div style="display:flex;justify-content:space-between;align-items:center">';
    cardHtml += '<div style="display:flex;align-items:center;gap:8px">';
    cardHtml += '<span class="spinner" style="display:inline-block"></span>';
    cardHtml += '<span style="color:var(--accent);font-weight:500">Discovering...</span>';
    cardHtml += '</div>';
    cardHtml += '<span style="color:var(--muted);font-size:12px">' + method + '</span>';
    cardHtml += '</div></div>';

    cardHtml += '<div class="printer-card-body">';
    cardHtml += '<div style="margin-bottom:8px"><strong>IP:</strong> <code>' + ip + '</code></div>';

    if (serial) {
        cardHtml += '<div style="margin-bottom:8px"><strong>Serial:</strong> <code>' + serial + '</code></div>';
        if (status === 'gathering_details') {
            cardHtml += '<div style="color:var(--muted);font-size:12px">🔍 Gathering manufacturer, model, and details...</div>';
        }
    } else {
        cardHtml += '<div style="color:var(--muted);font-size:12px">🔍 Getting serial number...</div>';
    }

    cardHtml += '</div>';
    card.innerHTML = cardHtml;

    // Auto-remove this card after 30 seconds (will be replaced by real device card)
    setTimeout(() => {
        const existingCard = document.getElementById(cardId);
        if (existingCard) {
            existingCard.remove();
        }
    }, 30000);
}

function updatePrinters() {
    // Check if user wants to see known devices in discovered list
    // When checked: shows ALL recently discovered devices (including known/saved ones)
    // When unchecked: shows only NEW devices (filters out already saved devices)
    const showKnownDevices = document.getElementById('show_saved_in_discovered')?.checked || false;

    // Get time filter value
    const timeMinutes = getTimeFilterMinutes();

    // Build endpoint with query parameters
    // include_known=true shows all recently discovered devices including saved ones
    // minutes=X filters to devices discovered in last X minutes (0 = all time)
    let discoveredEndpoint = '/devices/discovered?include_known=' + showKnownDevices;
    if (timeMinutes > 0) {
        discoveredEndpoint += '&minutes=' + timeMinutes;
    }

    Promise.all([
        fetch(discoveredEndpoint).then(r => r.json()),
        fetch('/devices/list').then(r => r.json())
    ]).then(([discovered, saved]) => {
        // discovered is now always PrinterInfo[] from /devices/discovered

        // Store discovered printers globally so saveDiscoveredDevice can access them
        window.discoveredPrinters = discovered || [];

        const discoveredContainer = document.getElementById('discovered_devices_cards');
        if (!discoveredContainer) return;

        // Build set of saved device serials and IPs for quick lookup
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

        // Check if autosave is enabled and if we should show discovered devices anyway
        const autosaveEnabled = document.getElementById('autosave_checkbox').checked;
        const showDiscoveredAnyway = document.getElementById('show_discovered_devices_anyway')?.checked || false;
        const discoveredSection = document.getElementById('discovered_section');
        
        if (discoveredSection) {
            // Show discovered section if: autosave is OFF, OR (autosave is ON AND showDiscoveredAnyway is checked)
            const shouldShowDiscovered = !autosaveEnabled || (autosaveEnabled && showDiscoveredAnyway);
            
            if (!shouldShowDiscovered) {
                // Hide with animation
                if (discoveredSection.style.display !== 'none') {
                    discoveredSection.style.transition = 'max-height 0.4s ease, opacity 0.3s ease';
                    discoveredSection.style.maxHeight = '0';
                    discoveredSection.style.opacity = '0';
                    discoveredSection.style.overflow = 'hidden';
                    setTimeout(() => {
                        discoveredSection.style.display = 'none';
                    }, 400);
                }
            } else {
                // Show with animation
                if (discoveredSection.style.display === 'none') {
                    discoveredSection.style.display = 'block';
                    discoveredSection.style.maxHeight = '0';
                    discoveredSection.style.opacity = '0';
                    discoveredSection.style.overflow = 'hidden';
                    setTimeout(() => {
                        discoveredSection.style.transition = 'max-height 0.4s ease, opacity 0.3s ease';
                        discoveredSection.style.maxHeight = '3000px';
                        discoveredSection.style.opacity = '1';
                        setTimeout(() => {
                            discoveredSection.style.overflow = 'visible';
                            discoveredSection.style.maxHeight = 'none';
                        }, 400);
                    }, 10);
                }
            }
        }

        // Render discovered printers table
        if (!Array.isArray(discovered) || discovered.length === 0) {
            discoveredContainer.innerHTML = '<div style="color:var(--muted);padding:12px">No discovered printers</div>';
            document.getElementById('discovered_stats').innerHTML = '';
        } else {
            // Calculate stats
            let lowTonerCount = 0;
            discovered.forEach(p => {
                const toners = p.toner_levels || {};
                for (const color in toners) {
                    if (toners[color] < 20) {
                        lowTonerCount++;
                        break;
                    }
                }
            });

            // Render stats
            const statsHtml = '<span style="color:var(--text)"><strong>Total:</strong> ' + discovered.length + '</span>' +
                '<span style="color:#b58900"><strong>Low Toner:</strong> ' + lowTonerCount + '</span>';
            document.getElementById('discovered_stats').innerHTML = statsHtml;

            // Render cards
            let cardsHTML = '';
            discovered.forEach(p => {
                const isSaved = (p.serial && savedSerials.has(p.serial)) || (p.ip && savedIPs.has(p.ip));
                cardsHTML += renderDiscoveredCard(p, isSaved);
            });
            discoveredContainer.innerHTML = cardsHTML;
        }

        // If autosave enabled, auto-save new devices (only those not already saved)
        if (autosaveEnabled && discovered.length > 0) {
            if (!window.autosavedIPs) window.autosavedIPs = new Set();
            discovered.forEach(p => {
                const isSaved = (p.serial && savedSerials.has(p.serial)) || (p.ip && savedIPs.has(p.ip));
                if (p.ip && !isSaved && !window.autosavedIPs.has(p.ip)) {
                    window.autosavedIPs.add(p.ip);
                    saveDiscoveredDevice(p.ip, true).catch(() => { window.autosavedIPs.delete(p.ip); });
                }
            });
        }
    }).catch(e => { console.error(e) });

    // Fetch saved devices
    fetch('/devices/list').then(r => r.json()).then(saved => {
        const savedContainer = document.getElementById('saved_devices_cards');
        if (!savedContainer) return;

        if (!Array.isArray(saved) || saved.length === 0) {
            savedContainer.innerHTML = '<div style="color:var(--muted);padding:12px">No saved devices</div>';
            document.getElementById('saved_stats').innerHTML = '';
        } else {
            // Calculate stats
            let lowTonerCount = 0;
            saved.forEach(item => {
                const p = item.printer_info || {};
                const toners = p.toner_levels || {};
                for (const color in toners) {
                    if (toners[color] < 20) {
                        lowTonerCount++;
                        break;
                    }
                }
            });

            // Render stats
            const statsHtml = '<span style="color:var(--text)"><strong>Total:</strong> ' + saved.length + '</span>' +
                '<span style="color:#b58900"><strong>Low Toner:</strong> ' + lowTonerCount + '</span>';
            document.getElementById('saved_stats').innerHTML = statsHtml;

            // Incremental update: only add new cards, keep existing ones
            const existingCards = Array.from(savedContainer.querySelectorAll('.saved-device-card'));
            const existingKeys = new Set(existingCards.map(card => card.dataset.deviceKey));
            const isInitialLoad = existingCards.length === 0;
            
            if (isInitialLoad) {
                // Initial load - render all cards at once without animation
                let cardsHTML = '';
                saved.forEach(item => {
                    cardsHTML += renderSavedCard(item);
                });
                savedContainer.innerHTML = cardsHTML;
                
                // Load usage graphs for all saved devices
                saved.forEach(item => {
                    if (item.serial) {
                        loadUsageGraph(item.serial);
                    }
                });
            } else {
                // Incremental update - only add new cards with animation
                saved.forEach(item => {
                    const deviceKey = item.serial || item.printer_info?.ip || '';
                    if (!deviceKey) return;
                    
                    if (!existingKeys.has(deviceKey)) {
                        // New device - add with animation at the end
                        const tempDiv = document.createElement('div');
                        tempDiv.innerHTML = renderSavedCard(item);
                        const newCard = tempDiv.firstElementChild;
                        savedContainer.appendChild(newCard);
                        
                        // Trigger animation after DOM insertion
                        requestAnimationFrame(() => {
                            newCard.classList.add('card-entering');
                        });
                        
                        // Load usage graph for new device
                        if (item.serial) {
                            loadUsageGraph(item.serial);
                        }
                    }
                });
                
                // Remove cards that no longer exist in saved list
                const savedKeys = new Set(saved.map(item => item.serial || item.printer_info?.ip || ''));
                existingCards.forEach(card => {
                    const key = card.dataset.deviceKey;
                    if (key && !savedKeys.has(key)) {
                        card.classList.add('removing');
                        setTimeout(() => card.remove(), 400);
                    }
                });
            }
        }
    }).catch(e => { console.error(e) });
}
// Save a discovered device by marking it as saved in the database
// Returns a Promise that resolves when save completes (for use in modals/batch operations)
async function saveDiscoveredDevice(ip, skipConfirm, forceWalk) {
    if (!ip) return Promise.reject('No IP provided');
    if (!skipConfirm) {
        const confirmed = await showConfirm(`Save discovered device ${ip} into saved devices?`, 'Save Device');
        if (!confirmed) return Promise.reject('Cancelled');
    }

    try {
        // Find the device serial from discovered printers
        const device = window.discoveredPrinters.find(p => p.ip === ip);
        if (!device) throw new Error('Device not found in discovered list');

        const serial = device.serial;
        if (!serial) throw new Error('Device has no serial number');

        // Mark as saved in database
        const resp = await fetch('/devices/save', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ serial: serial })
        });
        if (!resp.ok) {
            const txt = await resp.text();
            throw new Error('Save failed: ' + txt);
        }

        updatePrinters();
        return ip;
    } catch (e) {
        console.error(e);
        throw e;
    }
}

// UI effects when autosave is toggled (called after settings are saved)
function toggleAutosaveUI() {
    const enabled = document.getElementById('autosave_checkbox').checked;
    const showDiscoveredAnywayContainer = document.getElementById('show_discovered_devices_anyway_container');
    const showDiscoveredAnywayToggle = document.getElementById('show_discovered_devices_anyway');
    const showSavedCheckbox = document.getElementById('show_saved_in_discovered');

    if (enabled) {
        // Run Save All immediately in silent mode (no alerts)
        if (!window.autosavedIPs) window.autosavedIPs = new Set();
        saveAllDiscovered(true);

        // When autosave is enabled:
        // - Always show saved/known devices in discovered list (they're auto-saved)
        // - This makes it clear what devices have been captured
        if (showSavedCheckbox) {
            showSavedCheckbox.checked = true;
        }

        // Show the "Show Discovered Devices Anyway" toggle container
        // (allows user to keep discovered section visible if desired)
        if (showDiscoveredAnywayContainer) {
            showDiscoveredAnywayContainer.style.display = 'flex';
        }
    } else {
        // Clear tracking set when disabled
        if (window.autosavedIPs) window.autosavedIPs.clear();

        // When autosave is disabled:
        // - Hide the "Show Anyway" toggle container
        // - Uncheck "Show Anyway" toggle to reset state
        // - Optionally uncheck "Show Known Devices" to hide saved devices (cleaner view)
        if (showDiscoveredAnywayContainer) {
            showDiscoveredAnywayContainer.style.display = 'none';
        }
        if (showDiscoveredAnywayToggle) {
            showDiscoveredAnywayToggle.checked = false;
        }
        
        // Uncheck "Show Known Devices" when autosave is disabled
        // This provides a cleaner discovered list showing only NEW devices
        if (showSavedCheckbox) {
            showSavedCheckbox.checked = false;
        }
    }

    updatePrinters();
}

// Save all discovered devices (bulk operation via API)
// silent: if true, suppresses alerts (used for autosave)
async function saveAllDiscovered(silent) {
    try {
        // Use bulk save endpoint for efficiency
        const resp = await fetch('/devices/save/all', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' }
        });

        if (!resp.ok) {
            const txt = await resp.text();
            throw new Error('Save all failed: ' + txt);
        }

        const result = await resp.json();
        const count = result.count || 0;

        console.log('Save All complete: ' + count + ' devices saved');
        if (!silent && count > 0) {
            showToast(`Successfully saved ${count} device${count !== 1 ? 's' : ''}`, 'success');
        }

        updatePrinters();
    } catch (e) {
        console.error('Save All failed:', e);
        if (!silent) showToast('Save All failed: ' + e.message, 'error');
    }
}

// Clear discovered devices (hides them, doesn't delete)
async function clearDiscovered() {
    try {
        const r = await fetch('/devices/clear_discovered', { method: 'POST' });
        if (!r.ok) throw new Error('Clear failed');
        if (window.autosavedIPs) window.autosavedIPs.clear();
        updatePrinters();
        showToast('Discovered devices cleared', 'success');
    } catch (e) {
        console.error('Clear failed:', e);
        showToast('Clear failed: ' + e.message, 'error');
    }
}

// Toggle database backend credential fields based on selection
function toggleDatabaseFields() {
    const selector = document.getElementById('db_backend_type');
    if (!selector) return;

    const selectedBackend = selector.value;

    // Hide all backend field groups
    const allFieldGroups = [
        'db_sqlite_fields',
        'db_postgresql_fields',
        'db_mysql_fields',
        'db_mssql_fields',
        'db_mongodb_fields'
    ];
    allFieldGroups.forEach(id => {
        const el = document.getElementById(id);
        if (el) el.style.display = 'none';
    });

    // Show the selected backend's fields
    const targetId = 'db_' + selectedBackend + '_fields';
    const targetEl = document.getElementById(targetId);
    if (targetEl) targetEl.style.display = 'flex';

    // Show/hide clear database section (SQLite only)
    const clearSection = document.getElementById('clear_db_section');
    if (clearSection) {
        clearSection.style.display = (selectedBackend === 'sqlite') ? 'block' : 'none';
    }
}

// Clear entire database (backup and reset)
async function clearDatabase() {
    const confirmed = await showConfirm(
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
            showToast('Database backed up and reset successfully. Reloading...', 'success');
            setTimeout(() => window.location.reload(), 1500);
        }
    } catch (e) {
        console.error('Database clear failed:', e);
        showToast('Database clear failed: ' + e.message, 'error');
    }
}

// Show saved device details in modal
async function showSavedDeviceDetails(serial) {
    if (!serial) return;
    try {
        const r = await fetch('/devices/get?serial=' + encodeURIComponent(serial));
        if (!r.ok) throw new Error('Device not found');
        const device = await r.json();

        // Device from database has lowercase field names, but showPrinterDetailsData 
        // normalizes both formats, so we can pass the device directly
        showPrinterDetailsData(device, 'saved');
    } catch (e) {
        showToast('Failed to load device: ' + e.message, 'error');
    }
}

// Delete saved device
async function deleteSavedDevice(serial) {
    if (!serial) return;
    const confirmed = await showConfirm(
        'Delete this device? This will remove it from the database but it may be re-discovered if still on the network.',
        'Delete Device',
        true
    );
    if (!confirmed) return;
    
    try {
        const r = await fetch('/devices/delete', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ serial: serial }) });
        if (!r.ok) throw new Error('Delete failed');
        showToast('Device deleted successfully', 'success');
        updatePrinters();
    } catch (e) {
        console.error('Delete failed:', e);
        showToast('Delete failed: ' + e.message, 'error');
    }
}

// Edit a device field inline (Asset Number, Location)
async function editField(serial, fieldName, currentValue, element) {
    if (!serial || !fieldName) return;

    const displayName = fieldName === 'asset_number' ? 'Asset Number' : 'Location';
    const newValue = prompt('Enter new ' + displayName + ':', currentValue || '');

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
        showToast(`${displayName} updated successfully`, 'success');

    } catch (e) {
        console.error('Update failed:', e);
        showToast('Update failed: ' + e.message, 'error');
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
            showToast('No saved devices to delete', 'info');
            return;
        }

        // Confirm deletion
        const confirmed = await showConfirm(
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
            showToast(`Deleted ${deleted} device${deleted !== 1 ? 's' : ''} successfully`, 'success');
        }
        if (failed > 0) {
            showToast(`Failed to delete ${failed} device${failed !== 1 ? 's' : ''}`, 'error');
        }
    } catch (e) {
        console.error('Delete all failed:', e);
        showToast('Delete all failed: ' + e.message, 'error');
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
        showToast('Failed to copy logs: ' + e.message, 'error');
    }
}

async function clearLogs() {
    try {
        const confirmed = await showConfirm(
            'Clear logs? This will rotate the current log file and start fresh.',
            'Clear Logs'
        );
        if (!confirmed) return;
        
        const resp = await fetch('/logs/clear', { method: 'POST' });
        if (!resp.ok) {
            const text = await resp.text();
            showToast('Clear logs failed: ' + text, 'error');
            return;
        }
        
        // Clear the display and array
        allLogEntries = [];
        const logEl = document.getElementById('log');
        if (logEl) {
            logEl.innerHTML = '<span style="color:#586e75">(logs cleared - waiting for new entries)</span>';
        }
        
        showToast('Logs cleared and rotated', 'success');
    } catch (e) {
        console.error('Clear logs failed:', e);
        showToast('Failed to clear logs: ' + e.message, 'error');
    }
}

async function downloadLogs() {
    try {
        const resp = await fetch('/logs/archive');
        if (!resp.ok) {
            const t = await resp.text();
            showToast('Download failed: ' + t, 'error');
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
        showToast('Log archive downloaded', 'success');
    } catch (e) {
        console.error('Download failed:', e);
        showToast('Failed to download logs: ' + e.message, 'error');
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
        document.getElementById('ranges_text').value = (d.discovery && d.discovery.ranges_text) ? d.discovery.ranges_text : '';
    })
}

function saveRanges() {
    let txt = document.getElementById('ranges_text').value;
    fetch('/settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ discovery: { ranges_text: txt } }) })
        .then(async r => {
            if (!r.ok) { 
                let t = await r.text(); 
                showToast('Save failed: ' + t, 'error'); 
                return; 
            }
            showToast('Ranges saved', 'success');
        })
        .catch(e => {
            showToast('Save failed: ' + e.message, 'error');
        })
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
    const rangesSection = document.getElementById('ranges_config_section');
    if (!rangesSection) return;

    if (enabled) {
        // Show with animation
        rangesSection.style.display = 'block';
        rangesSection.style.maxHeight = '0';
        rangesSection.style.opacity = '0';
        rangesSection.style.overflow = 'hidden';
        setTimeout(() => {
            rangesSection.style.transition = 'max-height 0.4s ease, opacity 0.3s ease';
            rangesSection.style.maxHeight = '800px';
            rangesSection.style.opacity = '1';
        }, 10);
    } else {
        // Hide with animation
        rangesSection.style.transition = 'max-height 0.4s ease, opacity 0.3s ease';
        rangesSection.style.maxHeight = '0';
        rangesSection.style.opacity = '0';
        setTimeout(() => {
            rangesSection.style.display = 'none';
            rangesSection.style.overflow = 'visible';
        }, 400);
    }
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
    if (!ipEl) { showToast('Manual walk UI missing', 'error'); return; }
    const ip = ipEl.value.trim();
    if (!ip) { showToast('Enter target IP', 'error'); return; }
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
    const capabilitiesHTML = renderCapabilities(p);
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
        deviceInfo += '<button style="font-size:11px;padding:2px 6px" onclick="window.open(\'' + webUIVal + '\', \'_blank\')">Direct</button>';
        deviceInfo += '<button style="font-size:11px;padding:2px 6px;background:#268bd2;color:#fff" onclick="window.open(\'/proxy/' + (p.serial || '') + '\', \'_blank\')">Proxy</button>';
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

    // Metrics History Section (only for saved devices)
    if (source === 'saved' && p.serial) {
        let metricsContent = '<div id="metrics_content" style="font-size:13px;color:var(--muted)">Loading metrics...</div>';
        html += renderInfoCard('Metrics History', metricsContent);

        // Load metrics asynchronously
        setTimeout(() => loadDeviceMetrics(p.serial), 100);
    }

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
                    showToast('Delete failed', 'error');
                    return;
                }
                deleteBtn.textContent = 'Deleted ✓';
                showToast('Device deleted successfully', 'success');

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
                showToast('Delete failed: ' + e.message, 'error');
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
                await saveDiscoveredDevice(p.IP, true, false);
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
                showToast('Save failed: ' + e.message, 'error');
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
                .then(parseDebug => showPrinterDetailsData(p, source, parseDebug))
                .catch(() => showPrinterDetailsData(p, source, null));
            return;
        }

        // Not in discovered, try database - saved list returns wrapped printer_info
        fetch('/devices/list').then(r => r.json()).then(saved => {
            const item = saved.find(d => (d.printer_info && d.printer_info.ip === ip));
            if (item && item.printer_info) {
                showPrinterDetailsData(item.printer_info, 'saved', null);
            } else {
                bodyEl.textContent = 'Device not found';
            }
        }).catch(e => { bodyEl.textContent = 'Error loading device: ' + e; });
    }).catch(e => { bodyEl.textContent = 'Error loading devices: ' + e; });
}

// Load device metrics history and display in UI with interactive timeframe selector
async function loadDeviceMetrics(serial) {
    console.log('[Metrics] loadDeviceMetrics called for serial:', serial);
    const contentEl = document.getElementById('metrics_content');
    if (!contentEl) {
        console.error('[Metrics] metrics_content element not found');
        return;
    }
    console.log('[Metrics] Found metrics_content element');

    // Create interactive metrics UI
    let html = '';

    // Datetime range picker - always visible
    html += '<div id="metrics_custom_range" style="margin-bottom:16px;padding:16px;background:rgba(0,0,0,0.2);border:1px solid rgba(255,255,255,0.05);border-radius:6px">';

    // Quick preset buttons (will be enabled/disabled based on available data)
    html += '<div style="margin-bottom:16px">';
    html += '<div style="font-size:12px;color:var(--muted);margin-bottom:8px">Quick Select:</div>';
    html += '<div style="display:flex;gap:6px;flex-wrap:wrap">';
    html += '<button id="preset_day" onclick="setMetricsQuickRange(\'day\', \'' + serial + '\')" style="padding:6px 12px;font-size:13px;min-height:32px">Last 24 Hours</button>';
    html += '<button id="preset_week" onclick="setMetricsQuickRange(\'week\', \'' + serial + '\')" style="padding:6px 12px;font-size:13px;min-height:32px">Last 7 Days</button>';
    html += '<button id="preset_month" onclick="setMetricsQuickRange(\'month\', \'' + serial + '\')" style="padding:6px 12px;font-size:13px;min-height:32px">Last 30 Days</button>';
    html += '<button id="preset_year" onclick="setMetricsQuickRange(\'year\', \'' + serial + '\')" style="padding:6px 12px;font-size:13px;min-height:32px">Last Year</button>';
    html += '<button id="preset_all" onclick="setMetricsQuickRange(\'all\', \'' + serial + '\')" style="padding:6px 12px;font-size:13px;min-height:32px">All Time</button>';
    html += '</div>';
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
    html += '<button onclick="refreshMetricsChart(\'' + serial + '\')" style="width:100%;padding:10px;font-size:14px;min-height:40px;font-weight:600;background:#268bd2;color:#fff">Update Chart</button>';
    html += '</div>';

    // Stats summary
    html += '<div id="metrics_stats" style="margin-bottom:12px;font-size:12px"></div>';

    // Chart canvas
    html += '<canvas id="metrics_chart" width="500" height="200" style="width:100%;max-height:200px;background:#001f22;border:1px solid rgba(255,255,255,0.06);border-radius:4px"></canvas>';

    contentEl.innerHTML = html;

    // Initialize metrics data range (will be set after first data load)
    window.metricsDataRange = { min: null, max: null, serial: serial, flatpickr: null };

    // Initialize flatpickr and load data
    await initializeCustomDatetimePicker(serial);
}

// Initialize custom datetime picker with actual data bounds
async function initializeCustomDatetimePicker(serial) {
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
        if (!history || history.length === 0) {
            console.warn('[Metrics] No history data available');
            document.getElementById('metrics_data_range_start').textContent = 'No data';
            document.getElementById('metrics_data_range_end').textContent = 'No data';
            document.getElementById('metrics_content').innerHTML = '<div style="color:var(--muted);padding:12px">No metrics data available yet. Collect metrics first.</div>';
            return;
        }

        const minTime = new Date(history[0].timestamp);
        const maxTime = new Date(history[history.length - 1].timestamp);

        // Store data range globally
        window.metricsDataRange.min = minTime;
        window.metricsDataRange.max = maxTime;

        // Update labels
        document.getElementById('metrics_data_range_start').textContent = minTime.toLocaleString();
        document.getElementById('metrics_data_range_end').textContent = maxTime.toLocaleString();

        // Initialize to last 7 days
        const now = maxTime;
        const weekAgo = new Date(Math.max(minTime.getTime(), now.getTime() - 7 * 24 * 60 * 60 * 1000));

        // Check if flatpickr is available
        if (typeof flatpickr === 'undefined') {
            console.error('[Metrics] flatpickr library not loaded');
            document.getElementById('metrics_content').innerHTML = '<div style="color:#d33;padding:12px">Error: Date picker library not loaded. Please refresh the page.</div>';
            return;
        }

        console.log('[Metrics] Initializing flatpickr with range:', weekAgo, 'to', now);
        // Initialize flatpickr with range mode
        const fpInstance = flatpickr('#metrics_datetime_range', {
            mode: 'range',
            enableTime: true,
            dateFormat: 'Y-m-d H:i',
            minDate: minTime,
            maxDate: maxTime,
            defaultDate: [weekAgo, now],
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
    const statsEl = document.getElementById('metrics_stats');
    const canvas = document.getElementById('metrics_chart');
    if (!canvas || !statsEl) {
        console.error('[Metrics] Missing chart elements - canvas:', !!canvas, 'stats:', !!statsEl);
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
const eventSource = new EventSource('/events');

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
    // Show progressive discovery card
    showDiscoveringCard(data);
});

eventSource.addEventListener('metrics_update', (e) => {
    const data = JSON.parse(e.data);
    updateMetricsChart(data);
});

eventSource.onerror = (e) => {
    console.error('SSE connection error, will auto-reconnect');
};

// Load auto-discover checkbox state on page load (from unified settings)
fetch('/settings').then(r => r.json()).then(all => {
    const disc = all.discovery || {};
    document.getElementById('auto_discover_checkbox').checked = disc.auto_discover_enabled === true;
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
document.addEventListener('DOMContentLoaded', function () {
    loadThemePreference();
    toggleDatabaseFields(); // Initialize database field visibility

    // Check for database rotation warning on first page load
    checkDatabaseRotationWarning();

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
        dbBackendType.addEventListener('change', toggleDatabaseFields);
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
            if (!confirm('Regenerate TLS certificates? This will create new self-signed certificates. You will need to restart the agent for changes to take effect.')) {
                return;
            }
            try {
                const response = await fetch('/api/regenerate-certs', { method: 'POST' });
                const result = await response.json();
                if (response.ok) {
                    alert(result.message + '\n\nCert: ' + result.cert + '\nKey: ' + result.key);
                } else {
                    alert('Failed to regenerate certificates: ' + (result.error || response.statusText));
                }
            } catch (err) {
                alert('Error regenerating certificates: ' + err.message);
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
        webuiProxyBtn.addEventListener('click', openProxyUI);
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
        toggleRangesDropdown();
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

    fetch('/dev_settings/trace_tags').then(async r => {
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

    fetch('/dev_settings/trace_tags', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ tags: tagsMap })
    }).then(async r => {
        if (!r.ok) {
            showToast('Failed to save trace tags', 'error');
            return;
        }
        showToast('Trace tags saved successfully', 'success');
    }).catch(e => {
        console.error('saveTraceTags failed', e);
        showToast('Failed to save trace tags', 'error');
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
                if (!r.ok) { delBtn.disabled = false; delBtn.textContent = 'Delete'; showToast('Delete failed', 'error'); return; }
                showToast('Device deleted successfully', 'success');
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
                        showToast('Refresh failed: ' + txt, 'error'); return;
                    }
                    showToast('Refresh queued for ' + serial, 'success');
                } catch (e) { showToast('Refresh failed: ' + e.message, 'error'); }
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
    fetch('/dev_settings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
        .then(async r => { 
            if (!r.ok) { 
                const t = await r.text(); 
                console.error('Save failed:', t); 
                showToast('Save failed: ' + t, 'error'); 
                return; 
            } 
            showToast('Settings saved successfully', 'success');
        })
        .catch(e => { console.error('Save failed:', e); showToast('Save failed: ' + e.message, 'error'); });
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

    // Discovery settings
    document.getElementById('scan_local_subnet_enabled')?.addEventListener('change', window.__settingsChangeHandler);
    const manualRangesEl = document.getElementById('manual_ranges_enabled');
    if (manualRangesEl) { manualRangesEl.addEventListener('change', window.__manualRangesHandler); }
    document.getElementById('discovery_arp_enabled')?.addEventListener('change', window.__settingsChangeHandler);
    document.getElementById('discovery_icmp_enabled')?.addEventListener('change', window.__settingsChangeHandler);
    document.getElementById('discovery_tcp_enabled')?.addEventListener('change', window.__settingsChangeHandler);
    document.getElementById('discovery_snmp_enabled')?.addEventListener('change', window.__settingsChangeHandler);
    document.getElementById('discovery_mdns_enabled')?.addEventListener('change', window.__settingsChangeHandler);
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
        showToast('Settings saved successfully', 'success');
        return Promise.resolve();
    } catch (e) {
        console.error('Save failed:', e);
        if (!btn) {
            // Autosave failed silently in background, just log it
            console.warn('Autosave failed:', e.message);
        } else {
            showToast('Save failed: ' + e.message, 'error');
            btn.textContent = 'Apply';
            btn.disabled = false;
        }
        return Promise.reject(e);
    }
}

async function resetSettings() {
    const confirmed = await showConfirm(
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
                showToast('Reset failed: ' + t, 'error');
                return;
            }
            loadSettings();
            showToast('Settings reset successfully', 'success');
        })
        .catch(e => { console.error('Reset failed:', e); showToast('Reset failed: ' + e.message, 'error'); });
}

// Clipboard copy functionality (duplicate removed - see function at top of file)

function makeClipboardIcon() {
    return '<svg class="clipboard-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' +
        '<rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect>' +
        '<path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path>' +
        '</svg>';
}

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
