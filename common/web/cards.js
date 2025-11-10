// Shared card view utilities for PrintMaster UIs
// Exports renderSavedCard(item) and checkDatabaseRotationWarning()
(function(){
    function makeClipboardIconFallback() {
        return '<svg class="clipboard-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' +
            '<rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect>' +
            '<path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path>' +
            '</svg>';
    }

    function renderCapabilitiesFallback(device) {
        if (!device) return '';
        const caps = [];
        try {
            const hasColor = device.color_impressions || device.color_pages || false;
            const hasBlack = device.page_count || device.mono_impressions || false;
            if (hasColor) caps.push('<span class="capability-badge">Color</span>');
            if (hasBlack && !hasColor) caps.push('<span class="capability-badge">Mono</span>');
            if (device.fax) caps.push('<span class="capability-badge">Fax</span>');
            if (device.scan) caps.push('<span class="capability-badge">Scan</span>');
            if (device.duplex) caps.push('<span class="capability-badge">Duplex</span>');
        } catch (e) {
            return '';
        }
        return caps.length ? '<div class="capabilities-container">' + caps.join('') + '</div>' : '';
    }

    function renderSavedCard(item) {
        const device = (item && item.printer_info) || {};
        const serial = item && item.serial ? item.serial : '';
        const toners = device.toner_levels || {};
        const lifeCount = device.page_count || device.total_mono_impressions || 0;

        const graphId = 'usage-graph-' + (serial || (device.ip||'')).toString().replace(/[^a-zA-Z0-9]/g,'_');
        const usageGraphHTML = '<div id="' + graphId + '" class="usage-graph-container">' +
            '<div class="usage-graph-no-data">Loading usage data...</div>' +
            '</div>';

        // Consumables
        let consumablesHTML = '';
        const tonerColors = { black:'#2c2c2c', cyan:'#00bcd4', magenta:'#e91e63', yellow:'#ffc107' };
        for (const color in toners) {
            const level = toners[color] || 0;
            const bg = (tonerColors[color.toLowerCase()]||'#666');
            const low = level < 20;
            consumablesHTML += '<div class="consumable-item">' +
                '<div class="consumable-icon" style="background:' + bg + '20">' + '</div>' +
                '<span class="consumable-label">' + (color.charAt(0).toUpperCase()+color.slice(1)) + '</span>' +
                '<div class="consumable-bar"><div class="consumable-bar-fill" style="width:' + level + '%;background:' + bg + (low ? ';opacity:0.5' : '') + '">' + (level>15?level+'%':'') + '</div></div>' +
                '<span style="min-width:45px;text-align:right;font-family:monospace;color:var(--text);' + (low ? 'color:var(--highlight);font-weight:600' : '') + '">' + (level<=15?level+'%':'') + '</span>' +
                '</div>';
        }

        const consumablesSection = consumablesHTML ? '<div class="saved-device-card-section">' +
            '<div class="saved-device-card-section-title">Consumables</div><div class="consumable-container">' + consumablesHTML + '</div></div>' : '';

        const capabilitiesHTML = (typeof renderCapabilities === 'function') ? renderCapabilities(device) : renderCapabilitiesFallback(device);
        const clipIcon = (typeof makeClipboardIcon === 'function') ? makeClipboardIcon() : makeClipboardIconFallback();

        const ipVal = device.ip || 'N/A';
        const macVal = device.mac || '';
        const deviceKey = serial || ipVal || '';

        return `<div class="saved-device-card card-entering" data-device-key="${deviceKey}" data-make="${device.manufacturer||''}" data-model="${device.model||''}" data-ip="${device.ip||''}" data-serial="${serial}">` +
            `<div class="saved-device-card-header"><div class="saved-device-card-main">` +
            `<h5 class="saved-device-card-title">${device.manufacturer||'Unknown'} ${device.model||''}</h5>` +
            `${capabilitiesHTML}` +
            `<p class="saved-device-card-subtitle copyable" onclick="copyToClipboard('${serial}', this.querySelector('.clipboard-icon'))">Serial: ${serial}${clipIcon}</p>` +
            `<p class="saved-device-card-subtitle"><span class="copyable" onclick="copyToClipboard('${ipVal}', this.querySelector('.clipboard-icon'))" style="display:inline-flex;align-items:center;gap:4px;">IP: ${ipVal}${clipIcon}</span>` + (macVal?`<span class="copyable" onclick="copyToClipboard('${macVal}', this.querySelector('.clipboard-icon'))" style="display:inline-flex;align-items:center;gap:4px;margin-left:8px;"> • MAC: ${macVal}${clipIcon}</span>`:'') + `</p>` +
            `</div><div style="display:flex;gap:8px;flex-wrap:wrap;">` +
            ((item && item.web_ui_url) ? `<button class="primary" style="font-size:12px" onclick="showWebUIModal('${item.web_ui_url}', '${serial}')">WebUI</button>` : '') +
            `<button onclick="showPrinterDetails('${device.ip||''}','saved')">Details</button>` +
            `<button class="delete" onclick="deleteSavedDevice('${serial}')">Delete</button>` +
            `</div></div>` +
            `<div class="saved-device-card-grid"><div class="saved-device-card-inner-panel">` +
            `<div class="saved-device-card-section"><div class="saved-device-card-section-title">Device Info</div>` +
            `<div class="saved-device-card-row"><span class="saved-device-card-label">Asset #</span><span class="saved-device-card-value editable-field" onclick="editField('${serial}','asset_number','${item&&item.asset_number?item.asset_number:''}',this)">${item&&item.asset_number?item.asset_number:'(click to add)'}</span></div>` +
            `<div class="saved-device-card-row"><span class="saved-device-card-label">Location</span><span class="saved-device-card-value editable-field" onclick="editField('${serial}','location','${item&&item.location?item.location:''}',this)">${item&&item.location?item.location:'(click to add)'}</span></div>` +
            `<div class="saved-device-card-row"><span class="saved-device-card-label">Total Pages</span><span class="saved-device-card-value">${(lifeCount||0).toLocaleString()}</span></div>` +
            `</div>${consumablesSection}</div>${usageGraphHTML}</div></div>`;
    }

    // Preserve any existing global renderCapabilities if present, otherwise
    // provide a safe implementation that delegates to the fallback renderer.
    var __pm_existing_renderCapabilities = (typeof window !== 'undefined' && typeof window.renderCapabilities === 'function') ? window.renderCapabilities : null;

    function renderCapabilities(device) {
        if (__pm_existing_renderCapabilities) {
            try {
                return __pm_existing_renderCapabilities(device);
            } catch (e) {
                // If the existing implementation throws, fall back to our safe renderer
                return renderCapabilitiesFallback(device);
            }
        }
        return renderCapabilitiesFallback(device);
    }

    async function checkDatabaseRotationWarning() {
        try {
            const res = await fetch('/database/rotation_warning');
            if (!res.ok) return;
            const data = await res.json();
            if (data && data.rotated) {
                const message = `The database was rotated due to a migration failure on ${data.rotated_at || 'recently'}.\n\nA fresh database has been created and the old database has been backed up to:\n${data.backup_path || 'unknown location'}\n\nAll discovered devices and historical metrics data from the previous database are not available in the current session. If you need to recover data, you can manually restore the backup file.\n\nClick OK to acknowledge this warning.`;
                const confirmed = (typeof showConfirm === 'function') ? await showConfirm(message, 'Database Rotation Notice') : window.confirm(message);
                if (confirmed) {
                    await fetch('/database/rotation_warning', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ack: true }) });
                }
            }
        } catch (err) {
            console.error('Failed to check rotation warning:', err);
        }
    }

    // Export to shared namespace
    window.__pm_shared_cards = window.__pm_shared_cards || {};
    window.__pm_shared_cards.renderSavedCard = renderSavedCard;
    window.__pm_shared_cards.checkDatabaseRotationWarning = checkDatabaseRotationWarning;
    window.__pm_shared_cards.renderCapabilities = renderCapabilities;

    // Backwards-compatible globals if consumers expect them
    if (typeof window.renderSavedCard !== 'function') window.renderSavedCard = renderSavedCard;
    if (typeof window.checkDatabaseRotationWarning !== 'function') window.checkDatabaseRotationWarning = checkDatabaseRotationWarning;
    if (typeof window.renderCapabilities !== 'function') window.renderCapabilities = renderCapabilities;

})();
