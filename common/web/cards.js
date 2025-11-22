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
            // Normalize source of metric-like fields: some callers will pass full
            // device objects, others pass raw_data or metrics snapshots. Prefer
            // raw_data/metrics when available.
            const raw = device.raw_data || device.metrics || device.latest || device.latest_metrics || device.metrics_latest || device;
            const hasColor = raw.color_impressions || raw.color_pages || device.color_impressions || device.color_pages || false;
            const hasBlack = raw.page_count || raw.mono_impressions || device.page_count || device.mono_impressions || false;
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
    const toners = buildTonerLevels(device) || {};
        const lifeCount = device.page_count || device.total_mono_impressions || 0;

        const graphId = 'usage-graph-' + (serial || (device.ip||'')).toString().replace(/[^a-zA-Z0-9]/g,'_');
        const usageGraphHTML = '<div id="' + graphId + '" class="usage-graph-container">' +
            '<div class="usage-graph-no-data">Loading usage data...</div>' +
            '</div>';

        // Consumables - render a compact (mini) section on the saved device card
        // so the card remains compact while the details modal shows the full UI.
        const consumablesSection = (toners && Object.keys(toners).length > 0) ?
            ('<div class="saved-device-card-section">' +
                '<div class="saved-device-card-section-title">Consumables</div>' +
                '<div class="consumable-container">' + renderMiniConsumablesSection(toners) + '</div></div>') : '';

        const capabilitiesHTML = (typeof renderCapabilities === 'function') ? renderCapabilities(device) : renderCapabilitiesFallback(device);
    const clipIcon = (window.__pm_shared && typeof window.__pm_shared.makeClipboardIcon === 'function') ? window.__pm_shared.makeClipboardIcon() : makeClipboardIconFallback();

        const ipVal = device.ip || 'N/A';
        const macVal = device.mac || '';
        const deviceKey = serial || ipVal || '';

        return `<div class="saved-device-card card-entering" data-device-key="${deviceKey}" data-make="${device.manufacturer||''}" data-model="${device.model||''}" data-ip="${device.ip||''}" data-serial="${serial}">` +
            `<div class="saved-device-card-header"><div class="saved-device-card-main">` +
            `<h5 class="saved-device-card-title">${device.manufacturer||'Unknown'} ${device.model||''}</h5>` +
            `${capabilitiesHTML}` +
            `<p class="saved-device-card-subtitle copyable" data-copy="${serial}">Serial: ${serial}${clipIcon}</p>` +
            `<p class="saved-device-card-subtitle"><span class="copyable" data-copy="${ipVal}" style="display:inline-flex;align-items:center;gap:4px;">IP: ${ipVal}${clipIcon}</span>` + (macVal?`<span class="copyable" data-copy="${macVal}" style="display:inline-flex;align-items:center;gap:4px;margin-left:8px;"> • MAC: ${macVal}${clipIcon}</span>`:'') + `</p>` +
            `</div><div style="display:flex;gap:8px;flex-wrap:wrap;">` +
            ((item && item.web_ui_url) ? `<button class="primary" style="font-size:12px" data-action="webui" data-webui-url="${item.web_ui_url}" data-serial="${serial}">WebUI</button>` : '') +
            `<button data-action="details" data-ip="${device.ip||''}" data-serial="${serial}" data-source="saved">Details</button>` +
            `<button class="delete" data-action="delete" data-serial="${serial}">Delete</button>` +
            `</div></div>` +
            `<div class="saved-device-card-grid"><div class="saved-device-card-inner-panel">` +
            `<div class="saved-device-card-section"><div class="saved-device-card-section-title">Device Info</div>` +
            `<div class="saved-device-card-row"><span class="saved-device-card-label">Asset #</span><span class="saved-device-card-value editable-field" data-action="edit" data-serial="${serial}" data-field="asset_number" data-current="${item&&item.asset_number?item.asset_number:''}">${item&&item.asset_number?item.asset_number:'(click to add)'}</span></div>` +
            `<div class="saved-device-card-row"><span class="saved-device-card-label">Location</span><span class="saved-device-card-value editable-field" data-action="edit" data-serial="${serial}" data-field="location" data-current="${item&&item.location?item.location:''}">${item&&item.location?item.location:'(click to add)'}</span></div>` +
            `<div class="saved-device-card-row"><span class="saved-device-card-label">Total Pages</span><span class="saved-device-card-value">${(lifeCount||0).toLocaleString()}</span></div>` +
            `</div>${consumablesSection}</div>${usageGraphHTML}</div></div>`;
    }

    // Preserve any existing global renderCapabilities if present, otherwise
    // provide a safe implementation that delegates to the fallback renderer.
    function renderCapabilities(device) {
        return renderCapabilitiesFallback(device);
    }

    // Render a consumable with smart coloring/icons and progress bar.
    // This mirrors the richer behavior from the agent bundle so both
    // saved cards and the details modal show consistent supply UI.
    function renderConsumable(name, value, isLevel) {
        const nameLower = (name || '').toLowerCase();

        // If it's a numeric level (0-100), show as progress bar
        if (isLevel) {
            const v = Number(value);
            const pct = isNaN(v) ? '' : Math.max(0, Math.min(100, v));

            // Determine color based on supply type and level
            let color = '#6c6';
            let icon = '';

            // Toner colors
            // Use a slightly lighter tone for black to ensure contrast in dark mode
            if (nameLower.includes('black') || nameLower === 'k') { color = '#444'; icon = '●'; }
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

    // Render a full consumables section given a map of toner/supply levels
    // Reused by saved cards and the details modal to avoid duplicated markup.
    function renderConsumablesSection(tonerLevels) {
        if (!tonerLevels || Object.keys(tonerLevels).length === 0) return '';
        let out = '<div class="consumables-section" style="background:rgba(0,0,0,0.2);border:1px solid rgba(255,255,255,0.05);border-radius:6px;padding:10px;margin-bottom:8px">';
        out += '<div style="font-weight:600;color:var(--highlight);margin-bottom:8px;font-size:14px">Consumables</div>';
        for (const [k, v] of Object.entries(tonerLevels)) {
            // If value looks numeric, treat as a percentage level; otherwise render as text
            const isNumeric = (v !== null && v !== undefined) && (typeof v === 'number' || (!isNaN(Number(v)) && String(v).trim() !== ''));
            out += renderConsumable(k, v, !!isNumeric);
        }
        out += '</div>';
        return out;
    }

    // Build a normalized toner-levels map from available sources.
    // Sources checked (in order): explicit toner_levels/toners on object,
    // provided metrics snapshot (latest), and legacy raw_data keys
    // like toner_level_black / toner_desc_black. Returns an object with
    // friendly keys (e.g. 'Black') mapped to either numeric percentage or
    // descriptive text when a numeric level isn't available.
    function buildTonerLevels(p, latest) {
        const out = {};
        try {
            // If object already has explicit structured toners, use it
            const explicit = p && (p.toners || p.toner);
            if (explicit && Object.keys(explicit).length > 0) {
                // Copy through
                Object.entries(explicit).forEach(([k, v]) => { out[k] = v; });
                return out;
            }

            // If latest metrics snapshot contains structured toners, prefer that
            const latestStructured = latest && (latest.toners || latest.toner) ? (latest.toners || latest.toner) : null;
            if (latestStructured && Object.keys(latestStructured).length > 0) {
                Object.entries(latestStructured).forEach(([k, v]) => { out[k] = v; });
                return out;
            }

            // Fall back to legacy raw_data keys on p.raw_data (toner_level_black etc.)
            const raw = (p && (p.raw_data || p.raw || p.metrics)) || {};
            // Helper to pick numeric value if present
            const pick = (keys) => {
                for (const k of keys) {
                    if (raw[k] !== undefined && raw[k] !== null) return raw[k];
                    if (latest && latest[k] !== undefined && latest[k] !== null) return latest[k];
                    if (p && p[k] !== undefined && p[k] !== null) return p[k];
                }
                return undefined;
            };

            const mappings = [
                { name: 'Black', keys: ['toner_level_black', 'toner_level_k', 'toner_level_1', 'black_level', 'black_toner'] , descKeys: ['toner_desc_black', 'toner_desc_k', 'toner_desc_1', 'toner_desc'] },
                { name: 'Cyan', keys: ['toner_level_cyan', 'toner_level_c', 'toner_level_2', 'cyan_level'], descKeys: ['toner_desc_cyan'] },
                { name: 'Magenta', keys: ['toner_level_magenta', 'toner_level_m', 'toner_level_3', 'magenta_level'], descKeys: ['toner_desc_magenta'] },
                { name: 'Yellow', keys: ['toner_level_yellow', 'toner_level_y', 'toner_level_4', 'yellow_level'], descKeys: ['toner_desc_yellow'] },
            ];

            for (const m of mappings) {
                const val = pick(m.keys);
                if (val !== undefined) {
                    out[m.name] = val;
                    continue;
                }
                // If no numeric level, try to provide a descriptive name from descKeys
                const desc = pick(m.descKeys);
                if (desc) out[m.name] = desc;
            }

            // As a last resort, scan raw for any keys that look like 'toner' or 'cartridge'
            if (Object.keys(out).length === 0) {
                for (const [k, v] of Object.entries(raw)) {
                    if (!k) continue;
                    const lk = k.toLowerCase();
                    if (lk.includes('toner') || lk.includes('cartridge') || lk.includes('supply')) {
                        // Use a cleaned-up key name
                        const pretty = k.replace(/_/g, ' ').replace(/toner level/i, '').trim() || k;
                        out[pretty] = v;
                    }
                }
            }

            // Post-process: only include consumables the device actually has.
            // Priority: explicit `p.consumables` list if present; otherwise infer
            // from device metrics (color pages) and raw values.
            if (p) {
                // Normalize keys for comparison
                const normalize = (s) => String(s || '').toLowerCase();

                // If the device provided a consumables array, use that to limit keys
                if (Array.isArray(p.consumables) && p.consumables.length > 0) {
                    const present = new Set();
                    for (const desc of p.consumables) {
                        const d = normalize(desc);
                        if (d.includes('black') || d.includes(' k') || d === 'k') present.add('black');
                        else if (d.includes('cyan') || d === 'c') present.add('cyan');
                        else if (d.includes('magenta') || d === 'm') present.add('magenta');
                        else if (d.includes('yellow') || d === 'y') present.add('yellow');
                        else {
                            // If description doesn't match a color, include it by its exact text
                            present.add(desc);
                        }
                    }
                    // Filter out color keys not present
                    for (const k of Object.keys(out)) {
                        const lk = normalize(k);
                        if (lk.includes('black')) {
                            if (!present.has('black')) delete out[k];
                        } else if (lk.includes('cyan')) {
                            if (!present.has('cyan')) delete out[k];
                        } else if (lk.includes('magenta')) {
                            if (!present.has('magenta')) delete out[k];
                        } else if (lk.includes('yellow')) {
                            if (!present.has('yellow')) delete out[k];
                        } else {
                            // keep non-color keys only if explicitly present
                            if (!present.has(k) && !present.has(normalize(k))) delete out[k];
                        }
                    }
                } else {
                    // No explicit consumables list. If device seems mono (no color usage),
                    // drop CMY entries unless they have non-empty descriptive values.
                    const hasColor = !!(p.color_impressions || p.color_pages || (p.meters && (p.meters.color_pages || p.meters.cyan)));
                    if (!hasColor) {
                        for (const k of Object.keys(out)) {
                            const lk = normalize(k);
                            if (lk.includes('cyan') || lk.includes('magenta') || lk.includes('yellow')) {
                                const v = out[k];
                                const isEmptyDesc = (v === '' || v === null || v === undefined);
                                const isZero = (typeof v === 'number' && v === 0);
                                if (isEmptyDesc || isZero) delete out[k];
                            }
                        }
                    }
                }
            }
        } catch (e) {
            // ignore
        }
        return out;
    }

        // Render a compact consumable representation suitable for display on a saved-device card
        // Uses smaller bars and abbreviated labels to keep cards dense.
        function renderMiniConsumable(name, value, isLevel) {
            const nameShort = (name || '').split(' ')[0]; // take first token as short label
            if (isLevel) {
                const v = Number(value);
                const pct = isNaN(v) ? 0 : Math.max(0, Math.min(100, v));
                // color selection similar to full render but simpler
                let color = '#6c6';
                const nl = nameShort.toLowerCase();
                if (nl.includes('black') || nl === 'k') color = '#444';
                else if (nl.includes('cyan') || nl === 'c') color = '#0097a7';
                else if (nl.includes('magenta') || nl === 'm') color = '#c2185b';
                else if (nl.includes('yellow') || nl === 'y') color = '#fbc02d';

                return '<div class="mini-consumable" style="display:flex;align-items:center;gap:8px;margin:4px 0;">'
                    + '<div style="width:56px;font-size:12px;color:var(--muted);">' + nameShort + '</div>'
                    + '<div style="flex:1;min-width:80px;background:#001f22;border-radius:6px;height:10px;position:relative;overflow:hidden">'
                    + '<div style="width:' + pct + '%;background:' + color + ';height:100%"></div>'
                    + '</div>'
                    + '<div style="width:36px;text-align:right;font-size:12px;color:var(--muted);">' + pct + '%</div>'
                    + '</div>';
            }
            // fallback textual mini entry
            return '<div class="mini-consumable" style="display:flex;align-items:center;gap:8px;margin:4px 0;">'
                + '<div style="width:56px;font-size:12px;color:var(--muted);">' + nameShort + '</div>'
                + '<div style="font-size:12px;color:var(--muted);">' + (value || '') + '</div>'
                + '</div>';
        }

        function renderMiniConsumablesSection(tonerLevels) {
            if (!tonerLevels || Object.keys(tonerLevels).length === 0) return '';
            // Render mini consumables inline on one row so cards remain compact
            let out = '<div class="mini-consumables" style="display:flex;flex-direction:row;gap:8px;align-items:center;">';
            for (const [k, v] of Object.entries(tonerLevels)) {
                const isNumeric = (v !== null && v !== undefined) && (typeof v === 'number' || (!isNaN(Number(v)) && String(v).trim() !== ''));
                out += renderMiniConsumable(k, v, !!isNumeric);
            }
            out += '</div>';
            return out;
        }

    async function checkDatabaseRotationWarning() {
        try {
            const res = await fetch('/database/rotation_warning');
            if (!res.ok) return;
            const data = await res.json();
            if (data && data.rotated) {
                const message = `The database was rotated due to a migration failure on ${data.rotated_at || 'recently'}.\n\nA fresh database has been created and the old database has been backed up to:\n${data.backup_path || 'unknown location'}\n\nAll discovered devices and historical metrics data from the previous database are not available in the current session. If you need to recover data, you can manually restore the backup file.\n\nClick OK to acknowledge this warning.`;
                const confirmed = await window.__pm_shared.showConfirm(message, 'Database Rotation Notice');
                if (confirmed) {
                    await fetch('/database/rotation_warning', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ack: true }) });
                }
            }
        } catch (err) {
            window.__pm_shared.error('Failed to check rotation warning:', err);
        }
    }

    // Export to shared namespace
    window.__pm_shared_cards = window.__pm_shared_cards || {};
    window.__pm_shared_cards.renderSavedCard = renderSavedCard;
    window.__pm_shared_cards.buildTonerLevels = buildTonerLevels;
    window.__pm_shared_cards.checkDatabaseRotationWarning = checkDatabaseRotationWarning;
    window.__pm_shared_cards.renderCapabilities = renderCapabilities;

    // Attach delegated click handler for copyable elements that use data-copy.
    // Await shared.ready so shared utilities (copyToClipboard, showToast) are
    // available and logging is consistent.
    document.addEventListener('click', async (ev) => {
        try {
            await (window.__pm_shared && window.__pm_shared.ready);
        } catch (e) {
            // If ready promise rejected or missing, continue to avoid blocking UI
        }
        try {
            const el = ev.target.closest && ev.target.closest('.copyable');
            if (!el) return;
            const val = el.getAttribute('data-copy');
            if (!val) return;
            try {
                window.__pm_shared.copyToClipboard(val, null, 'Copied to clipboard');
            } catch (err) {
                // fallback
                try { navigator.clipboard.writeText(val); window.__pm_shared.showToast && window.__pm_shared.showToast('Copied to clipboard', 'success'); } catch (e) {}
            }
        } catch (e) {
            try { window.__pm_shared && window.__pm_shared.debug && window.__pm_shared.debug('copyable handler error', e); } catch (er) {}
        }
    });

    // Delegated handler for action buttons in rendered cards. We await
    // the shared.ready promise to guarantee the shared API and logger are
    // available before calling into window.__pm_shared.
    document.addEventListener('click', async (ev) => {
        try {
            await (window.__pm_shared && window.__pm_shared.ready);
        } catch (e) {}
        try {
                // Support both data-action attributes and legacy .save-device-btn class
                let btn = ev.target.closest && (ev.target.closest('[data-action]') || ev.target.closest('.save-device-btn'));
                if (!btn) return;
                // Determine action: prefer explicit data-action, fall back to class-based legacy actions
                const action = btn.getAttribute('data-action') || (btn.classList && btn.classList.contains('save-device-btn') ? 'save' : null);
            try { window.__pm_shared && window.__pm_shared.trace && window.__pm_shared.trace('card action', action, btn.dataset); } catch (er) {}

            if (action === 'save' || btn.classList.contains('save-device-btn')) {
                // Old markup migration: support both data-action="save" and .save-device-btn
                const ip = btn.getAttribute('data-ip') || btn.getAttribute('data-device-ip');
                if (!ip) return;
                // Find the card element and provide a saving visual state
                const card = btn.closest && btn.closest('.device-card');
                try { btn.disabled = true; } catch (e) {}
                try {
                    if (card) {
                        // Indicate that the card is being saved (green flash)
                        card.classList.add('saving');
                        // ensure the element participates in smooth reflow
                        card.style.willChange = 'transform, max-height, margin';
                    }
                } catch (e) {}

                try {
                    // Prevent the agent-side save helper from immediately calling
                    // updatePrinters (which would re-render and remove the card
                    // before our exit animation completes). Request the save but
                    // ask it NOT to update the UI; we'll call updatePrinters after
                    // the animation finishes.
                    await window.__pm_shared.saveDiscoveredDevice(ip, false, false);

                    // On success, show saved state then animate removal so other
                    // cards slide into place smoothly (removing uses slideOut keyframes)
                    try {
                        if (card) {
                            card.classList.remove('saving');
                            card.classList.add('removing');
                            let handled = false;
                            const onEnd = () => {
                                if (handled) return; handled = true;
                                try { card.remove(); } catch (e) {}
                                try { if (typeof updatePrinters === 'function') updatePrinters(); } catch (e) {}
                                card.removeEventListener('animationend', onEnd);
                                card.removeEventListener('transitionend', onEnd);
                            };
                            card.addEventListener('animationend', onEnd);
                            card.addEventListener('transitionend', onEnd);
                            // safety fallback
                            setTimeout(onEnd, 700);
                        } else {
                            try { if (typeof updatePrinters === 'function') updatePrinters(); } catch (e) {}
                        }
                    } catch (e) {
                        try { if (typeof updatePrinters === 'function') updatePrinters(); } catch (er) {}
                    }

                    btn.textContent = 'Saved ✓';
                } catch (e) {
                    window.__pm_shared.error('Save failed:', e);
                    try { btn.disabled = false; } catch (er) {}
                    try { btn.textContent = 'Save'; } catch (er) {}
                    window.__pm_shared.showToast('Save failed: ' + (e && e.message ? e.message : e), 'error');
                }
                return;
            }

            if (action === 'webui') {
                const url = btn.getAttribute('data-webui-url');
                const serial = btn.getAttribute('data-serial');
                window.__pm_shared.showWebUIModal(url, serial);
                return;
            }

            if (action === 'open-direct') {
                const url = btn.getAttribute('data-webui-url');
                window.open(url, '_blank');
                return;
            }

            if (action === 'open-proxy') {
                const serial = btn.getAttribute('data-serial');
                window.__pm_shared.openDeviceUI(serial);
                return;
            }

            if (action === 'metrics') {
                const serial = btn.getAttribute('data-serial');
                const preset = btn.getAttribute('data-preset');
                window.__pm_shared.showDeviceMetricsModal(serial, preset);
                return;
            }

            // Server-specific actions (agents/devices list)
            if (action === 'view-agent') {
                const agentId = btn.getAttribute('data-agent-id');
                window.__pm_shared.viewAgentDetails(agentId);
                return;
            }

            if (action === 'open-agent') {
                const agentId = btn.getAttribute('data-agent-id');
                window.__pm_shared.openAgentUI(agentId);
                return;
            }

            if (action === 'delete-agent') {
                const agentId = btn.getAttribute('data-agent-id');
                const agentName = btn.getAttribute('data-agent-name');
                window.__pm_shared.deleteAgent(agentId, agentName);
                return;
            }

            if (action === 'open-device') {
                const serial = btn.getAttribute('data-serial');
                window.__pm_shared.openDeviceUI(serial);
                return;
            }

            if (action === 'view-metrics') {
                const serial = btn.getAttribute('data-serial');
                window.__pm_shared.openDeviceMetrics(serial);
                return;
            }

            if (action === 'show-printer-details') {
                // Prefer serial lookup when available, otherwise fall back to IP.
                const serial = btn.getAttribute('data-serial');
                const ip = btn.getAttribute('data-ip');
                const lookup = serial || ip;
                const source = btn.getAttribute('data-source') || 'discovered';
                window.__pm_shared.showPrinterDetails(lookup, source);
                return;
            }

            if (action === 'details') {
                // Prefer serial if provided on the button; many saved cards include data-serial.
                const serial = btn.getAttribute('data-serial');
                const ip = btn.getAttribute('data-ip');
                const lookup = serial || ip;
                const source = btn.getAttribute('data-source') || 'discovered';
                window.__pm_shared.showPrinterDetails(lookup, source);
                return;
            }

            if (action === 'delete') {
                const serial = btn.getAttribute('data-serial');
                window.__pm_shared.deleteSavedDevice(serial);
                return;
            }

            if (action === 'edit') {
                const serial = btn.getAttribute('data-serial');
                const field = btn.getAttribute('data-field');
                const current = btn.getAttribute('data-current');
                window.__pm_shared.editField(serial, field, current, btn);
                return;
            }
        } catch (e) {
            window.__pm_shared.error('card action handler error', e);
        }
    });

    // Shared modal renderer for printer details (moved from agent/web/app.js)
    // This builds the saved/discovered device modal and also provides a
    // small metrics-summary loader used by the modal. Consumers should
    // call `window.__pm_shared_cards.showPrinterDetailsData(p, source, parseDebug)`.
    function showPrinterDetailsData(p, source, parseDebug) {
        if (!p) return;
        source = source || 'discovered';
        const bodyEl = document.getElementById('printer_details_body');
        const overlay = document.getElementById('printer_details_overlay');
        const titleEl = document.getElementById('printer_details_title');
        const actionsEl = document.getElementById('printer_details_actions');

        try {
            titleEl.textContent = (source === 'saved') ? 'Saved Device' : 'Discovered Device';
        } catch (e) {}

        // Store the current printer IP to prevent glitchy updates from live data
        try { overlay.dataset.currentPrinterIp = p.ip || p.IP || ''; } catch (e) {}

        // Show modal overlay and prevent body scroll
        try { overlay.style.display = 'flex'; document.body.style.overflow = 'hidden'; } catch (e) {}

        // Helper renderers
        function renderInfoCard(title, content) {
            if (!content) return '';
                return '<div class="panel">' +
                    '<h4 style="margin-top:0;color:var(--highlight)">' + title + '</h4>' +
                    content +
                    '</div>';
        }

        function renderRow(label, value) {
            if (!value && value !== 0) return '';
            return '<div style="display:grid;grid-template-columns:auto 1fr;gap:4px 12px;font-size:13px;padding:4px 0">' +
                '<div style="color:var(--muted);white-space:nowrap">' + label + ':</div>' +
                '<div style="word-break:break-word">' + value + '</div>' +
                '</div>';
        }

        // Editable row helper (mirrors agent UI behaviour)
        const isLocked = (field) => Array.isArray(p.locked_fields) && p.locked_fields.some(f => (f.field || f.Field || '').toLowerCase() === field);
        function renderEditableRow(label, field, value, opts = { type: 'text', readonly: false, placeholder: '' }) {
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
        }

        // Build HTML (kept intentionally similar to agent implementation)
        let html = '<div class="settings-grid" style="margin-top:0">';

        if (!p || Object.keys(p).length === 0) {
            html += '<div style="color:var(--muted);padding:12px">No printer data available</div>';
            bodyEl.innerHTML = html + '</div>';
            return;
        }

        // Capabilities (use shared renderer)
        try {
            const capabilitiesHTML = window.__pm_shared_cards.renderCapabilities(p);
            if (capabilitiesHTML) html += renderInfoCard('Capabilities', capabilitiesHTML);
        } catch (e) {}

        // Device Info (editable rows with lock buttons)
        let deviceInfo = '<div style="display:flex;flex-direction:column;gap:6px">';
        deviceInfo += renderEditableRow('Manufacturer', 'manufacturer', p.manufacturer);
        deviceInfo += renderEditableRow('Model', 'model', p.model);
        deviceInfo += renderEditableRow('Serial', 'serial', p.serial, { type: 'text', readonly: true });
        deviceInfo += renderEditableRow('Firmware', 'firmware', p.firmware);
        deviceInfo += renderEditableRow('Asset Number', 'asset_number', p.asset_number);
        deviceInfo += renderEditableRow('Location', 'location', p.location);
        deviceInfo += renderEditableRow('Description', 'description', p.description, { type: 'textarea' });
        // Web UI with proxy buttons
        const webUIVal = p.web_ui_url || p.webui || '';
        const webUILocked = isLocked('web_ui_url');
        let webUiRow = '<div style="display:grid;grid-template-columns:auto 1fr auto;gap:4px 8px;align-items:center" data-field-row="web_ui_url">';
        webUiRow += '<div style="color:var(--muted)">Web UI:</div>';
        webUiRow += '<div style="display:flex;gap:4px;align-items:center">';
        webUiRow += '<input id="field_web_ui_url" type="text" value="' + webUIVal + '" ' + (webUILocked ? 'disabled' : '') + ' style="flex:1;' + (webUILocked ? 'background:var(--panel);opacity:0.8;' : '') + '">';
        if (webUIVal) {
            webUiRow += '<button style="font-size:11px;padding:2px 6px" data-action="open-direct" data-webui-url="' + webUIVal + '">Direct</button>';
            webUiRow += '<button style="font-size:11px;padding:2px 6px;background:#268bd2;color:#fff" data-action="open-proxy" data-serial="' + (p.serial || '') + '">Proxy</button>';
        }
        webUiRow += '</div>';
        webUiRow += '<button class="lock-btn' + (webUILocked ? ' locked' : '') + '" data-field="web_ui_url" title="' + (webUILocked ? 'Unlock field' : 'Lock field') + '"></button>';
        webUiRow += '</div>';
        deviceInfo += webUiRow;
        if (p.last_seen) deviceInfo += '<div style="color:var(--muted);font-size:12px;margin-top:6px">Last Seen: ' + new Date(p.last_seen).toLocaleString() + '</div>';
        if (p.first_seen) deviceInfo += '<div style="color:var(--muted);font-size:12px">First Seen: ' + new Date(p.first_seen).toLocaleString() + '</div>';
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

    // Network Info (editable)
    let networkInfo = '<div style="display:flex;flex-direction:column;gap:6px">';
    networkInfo += renderEditableRow('IP Address', 'ip', p.ip || p.IP || '');
    networkInfo += renderEditableRow('MAC Address', 'mac_address', p.mac || p.mac_address || '');
    networkInfo += renderEditableRow('Hostname', 'hostname', p.hostname);
    networkInfo += renderEditableRow('Subnet Mask', 'subnet_mask', p.subnet_mask);
    networkInfo += '</div>';
    html += renderInfoCard('Network', networkInfo);

        // Consumables (render as its own card). If we don't have explicit
        // consumable information yet, include a placeholder `printer_consumables_card`
        // which can later be populated from the latest metrics snapshot.
        const tonerLevels = buildTonerLevels(p) || {};
        if (tonerLevels && Object.keys(tonerLevels).length > 0) {
            // Render the consumables card with an explicit id so we can
            // detect it later and avoid duplicate insertions.
            html += '<div id="printer_consumables_card_actual">' + renderInfoCard('Consumables', renderConsumablesSection(tonerLevels)) + '</div>';
        } else {
            html += '<div id="printer_consumables_card"></div>';
        }

        // Interfaces: attempt to extract from parseDebug.raw_pdus (IF-MIB columns)
        try {
            if (parseDebug && Array.isArray(parseDebug.raw_pdus)) {
                const ifs = {};
                parseDebug.raw_pdus.forEach(rp => {
                    const oid = rp.oid || rp.OID || '';
                    if (oid.startsWith('1.3.6.1.2.1.2.2.1.')) {
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
        } catch (e) { /* non-critical */ }

        // Paper trays - collapsible
        try {
            if (p.paper_tray_status && p.paper_tray_status.length) {
                html += '<details style="background:rgba(0,0,0,0.2);border:1px solid rgba(255,255,255,0.05);border-radius:6px;padding:10px;margin-bottom:8px">';
                html += '<summary style="font-weight:600;color:var(--highlight);cursor:pointer;font-size:14px">Paper Trays (' + p.paper_tray_status.length + ')</summary>';
                html += '<div style="margin-top:8px">';
                p.paper_tray_status.forEach(t => html += '<div style="padding:6px;background:rgba(0,0,0,0.15);border-radius:4px;margin-bottom:4px;font-size:13px">' + t + '</div>');
                html += '</div></details>';
            }
        } catch (e) { /* ignore */ }

        // Status messages and alerts - collapsible
        try {
            if (p.status_messages && p.status_messages.length) {
                html += '<details style="background:rgba(0,0,0,0.2);border:1px solid rgba(255,255,255,0.05);border-radius:6px;padding:10px;margin-bottom:8px">';
                html += '<summary style="font-weight:600;color:var(--highlight);cursor:pointer;font-size:14px">Status Messages (' + p.status_messages.length + ')</summary>';
                html += '<div style="margin-top:8px">';
                p.status_messages.forEach(s => html += '<div style="padding:6px;background:rgba(0,0,0,0.15);border-radius:4px;margin-bottom:4px;font-size:13px">' + s + '</div>');
                html += '</div></details>';
            }
        } catch (e) { /* ignore */ }

        // Parse debug link (fallback to constructed endpoint if not present)
        try {
            const dbgLink = p.parse_debug_path || ('/parse_debug?ip=' + encodeURIComponent(p.ip || ''));
            html += '<div><a href="' + dbgLink + '" target="_blank">View Parse Debug</a></div>';
        } catch (e) {}

        html += '</div>'; // close settings-grid

        // Action buttons area
        html += '<div style="margin-top:16px;display:flex;gap:8px;flex-wrap:wrap;padding:12px;background:rgba(0,0,0,0.1);border-radius:6px">';
        html += '<button id="refresh_data_btn">Refresh Details</button>';
        if (source === 'saved') html += '<button id="collect_metrics_btn">Collect Metrics</button>';
        html += '<span id="refresh_status" style="color:var(--muted);align-self:center"></span>';
        html += '</div>';

    bodyEl.innerHTML = html;

        // Populate compact metrics (if any)
        (async function populatePrinterMetricsSummaryLocal() {
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

                // Render any consumable levels we can extract from the latest snapshot
                const latestToners = buildTonerLevels(p, latest) || {};
                if (latestToners && Object.keys(latestToners).length > 0) {
                    for (const [color, level] of Object.entries(latestToners)) {
                        const levelNum = typeof level === 'number' ? level : parseInt(level) || 0;
                        const levelColor = levelNum < 20 ? '#d32f2f' : (levelNum < 50 ? '#f57c00' : '#388e3c');
                        statsHtml += '<tr style="border-bottom:1px solid rgba(255,255,255,0.03)">';
                        statsHtml += '<td style="padding:6px 8px;color:var(--text)">' + color + '</td>';
                        statsHtml += '<td style="padding:6px 8px;text-align:right;color:' + levelColor + ';font-weight:600">' + levelNum + '%</td>';
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
                const summaryElFinal = document.getElementById('printer_metrics_summary');
                if (summaryElFinal) summaryElFinal.innerHTML = statsHtml;
                // If the device object didn't include toner_levels (schema v7+),
                // populate consumables UI from the latest metrics snapshot so
                // the modal still shows toner bars for saved devices.
                    try {
                        // If the main render didn't include consumables, populate the
                        // dedicated consumables card placeholder with data from the
                        // latest metrics snapshot so consumables and metrics are
                        // separate cards and don't duplicate bars.
                        const injected = buildTonerLevels(p, latest);
                        if (injected && Object.keys(injected).length > 0) {
                            const sectionHtml = renderConsumablesSection(injected);
                            // If a real consumables card was already rendered, don't add another
                            const existingActual = document.getElementById('printer_consumables_card_actual');
                            if (existingActual) {
                                // Optionally update existing card content
                                try { existingActual.innerHTML = renderInfoCard('Consumables', sectionHtml); } catch (e) {}
                            } else {
                                const consumablesPlaceholder = document.getElementById('printer_consumables_card');
                                if (consumablesPlaceholder) {
                                    consumablesPlaceholder.innerHTML = renderInfoCard('Consumables', sectionHtml);
                                    // mark as actual so future attempts don't duplicate
                                    consumablesPlaceholder.id = 'printer_consumables_card_actual';
                                } else {
                                    // Fallback (old behavior) if placeholder missing
                                    const metricsSummaryEl = document.getElementById('printer_metrics_summary');
                                    if (metricsSummaryEl && metricsSummaryEl.parentElement) {
                                        // Avoid duplicating: ensure there's no existing actual card first
                                        const already = document.getElementById('printer_consumables_card_actual');
                                        if (!already) metricsSummaryEl.insertAdjacentHTML('afterend', renderInfoCard('Consumables', sectionHtml));
                                    } else {
                                        const be = document.getElementById('printer_details_body');
                                        if (be) {
                                            const already = document.getElementById('printer_consumables_card_actual');
                                            if (!already) be.insertAdjacentHTML('afterbegin', renderInfoCard('Consumables', sectionHtml));
                                        }
                                    }
                                }
                            }
                        }
                    } catch (e) { /* non-critical UI enhancement */ }
            } catch (err) {
                try { const summaryEl = document.getElementById('printer_metrics_summary'); if (summaryEl) summaryEl.innerHTML = '<div style="color:var(--muted)">Metrics unavailable</div>'; } catch(_){ }
            }
        })();

        // Small post-render layout hook (if consumer provides applyMasonryLayout)
        setTimeout(() => {
            const modalGrid = bodyEl && bodyEl.querySelector ? bodyEl.querySelector('.settings-grid') : null;
            if (modalGrid && typeof applyMasonryLayout === 'function') {
                try { applyMasonryLayout(modalGrid); } catch (e) {}
            }
        }, 50);

        // Wire up lock toggles (delegates locking to server)
        try {
            const isLocked = (field) => Array.isArray(p.locked_fields) && p.locked_fields.some(f => (f.field || f.Field || '').toLowerCase() === field);

            bodyEl.addEventListener('click', async (e) => {
                const btn = e.target.closest && e.target.closest('.lock-btn');
                if (!btn) return;
                const field = btn.getAttribute('data-field');
                const locked = btn.classList.contains('locked');

                const inputEl = document.getElementById('field_' + field);
                if (!inputEl) return;

                try {
                    const r = await fetch('/devices/lock', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ serial: p.serial || p.Serial || '', field, lock: !locked }) });
                    if (r.ok) {
                        if (locked) {
                            btn.classList.remove('locked');
                            btn.title = 'Lock field';
                            inputEl.disabled = false;
                            inputEl.style.background = '';
                            inputEl.style.opacity = '';
                            inputEl.style.cursor = '';
                        } else {
                            btn.classList.add('locked');
                            btn.title = 'Unlock field';
                            inputEl.disabled = true;
                            inputEl.style.transition = 'background 0.3s ease, opacity 0.3s ease';
                            inputEl.style.background = 'var(--panel)';
                            inputEl.style.opacity = '0.8';
                            inputEl.style.cursor = 'not-allowed';
                        }
                    }
                } catch (_) {}
            });
        } catch (e) { window.__pm_shared.warn('lock wiring failed', e); }

        // Wire up refresh button
        try {
            document.getElementById('refresh_data_btn')?.addEventListener('click', async function () {
                const statusEl = document.getElementById('refresh_status');
                const btn = document.getElementById('refresh_data_btn');
                if (!btn) return;
                btn.disabled = true;
                if (statusEl) statusEl.textContent = ' Previewing updates...';
                try {
                    const body = { serial: p.serial || '', ip: p.ip };
                    const r = await fetch('/devices/preview', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
                    if (!r.ok) { const t = await r.text(); if (statusEl) statusEl.textContent = ' Error: ' + t; btn.disabled = false; return; }
                    const { proposed } = await r.json();
                    if (statusEl) statusEl.textContent = '';
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

                    document.getElementById('apply_selected_btn')?.addEventListener('click', async () => {
                        const checks = Array.from(container.querySelectorAll('.apply-proposed:checked'));
                        if (checks.length === 0) { if (statusEl) statusEl.textContent = ' No changes selected'; return; }
                        const payload = { serial: p.serial };
                        checks.forEach(ch => {
                            const f = ch.getAttribute('data-field');
                            let v = proposed[f];
                            payload[f] = v;
                        });
                        const ur = await fetch('/devices/update', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) });
                        if (!ur.ok) { const t = await ur.text(); if (statusEl) statusEl.textContent = ' Update failed: ' + t; return; }
                        if (statusEl) statusEl.textContent = ' Changes applied ✓';
                        await new Promise(res => setTimeout(res, 400));
                        // Re-open via agent helper which resolves discovered vs saved.
                        // Use serial (saved-device lookup) rather than IP because the
                        // shared wrapper now resolves by serial only.
                        try {
                            const serialId = p.serial || p.Serial;
                            // Use the shared wrapper which resolves by serial/device-key.
                            if (serialId && window.__pm_shared && typeof window.__pm_shared.showPrinterDetails === 'function') {
                                window.__pm_shared.showPrinterDetails(serialId, source);
                            } else if (serialId && typeof showPrinterDetails === 'function') {
                                // Fallback to any global implementation (legacy pages)
                                showPrinterDetails(serialId, source);
                            }
                        } catch (e) { /* best-effort, ignore */ }
                    });
                } catch (err) {
                    if (statusEl) statusEl.textContent = ' Failed: ' + err;
                } finally {
                    btn.disabled = false;
                }
            });
            // Run the preview check automatically when the details modal is opened
            // so users always see suggested fixes without needing to click Refresh.
            try {
                const autoRefreshBtn = document.getElementById('refresh_data_btn');
                if (autoRefreshBtn) {
                    // Slight defer so DOM is fully settled
                    setTimeout(() => {
                        try { autoRefreshBtn.click(); } catch (e) { /* best-effort */ }
                    }, 80);
                }
            } catch (e) { /* ignore */ }
        } catch (e) { window.__pm_shared.warn('refresh wiring failed', e); }

        // Wire up metrics collection button (saved devices only)
        try {
            if (source === 'saved') {
                document.getElementById('collect_metrics_btn')?.addEventListener('click', async function () {
                    const statusEl = document.getElementById('refresh_status');
                    const btn = document.getElementById('collect_metrics_btn');
                    if (!btn) return;
                    btn.disabled = true;
                    if (statusEl) statusEl.textContent = ' Collecting metrics...';
                    try {
                        const body = { serial: p.serial, ip: p.ip };
                        const r = await fetch('/devices/metrics/collect', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
                        if (!r.ok) { const t = await r.text(); if (statusEl) statusEl.textContent = ' Error: ' + t; btn.disabled = false; return; }
                        const result = await r.json();
                        if (statusEl) statusEl.textContent = ' Metrics saved ✓';
                        setTimeout(() => { if (statusEl) statusEl.textContent = ''; if (btn) btn.disabled = false; }, 2000);
                    } catch (err) {
                        if (statusEl) statusEl.textContent = ' Failed: ' + err;
                        if (btn) btn.disabled = false;
                    }
                });
            }
        } catch (e) { window.__pm_shared.warn('collect metrics wiring failed', e); }

        // Action buttons (delete/save/close)
        try {
            actionsEl.innerHTML = '';
            if (source === 'saved') {
                const deleteBtn = document.createElement('button');
                deleteBtn.textContent = 'Delete Device';
                deleteBtn.onclick = async function () {
                    deleteBtn.disabled = true;
                    deleteBtn.textContent = 'Deleting...';
                    try {
                        const r = await fetch('/devices/delete', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ serial: p.serial || p.Serial }) });
                        if (!r.ok) { deleteBtn.disabled = false; deleteBtn.textContent = 'Delete Device'; window.__pm_shared.showToast('Delete failed', 'error'); return; }
                        deleteBtn.textContent = 'Deleted ✓';
                        window.__pm_shared.showToast('Device deleted successfully', 'success');
                        const cardToRemove = document.querySelector('.saved-device-card[data-serial="' + (p.serial || p.Serial) + '"]');
                        if (cardToRemove) {
                                    try {
                                        cardToRemove.classList.add('removing');
                                        let handledRem = false;
                                        const onEndRem = () => {
                                            if (handledRem) return; handledRem = true;
                                            try { cardToRemove.remove(); } catch (e) {}
                                            try { overlay.style.display = 'none'; document.body.style.overflow = ''; delete overlay.dataset.currentPrinterIp; } catch (e) {}
                                            try { if (typeof updatePrinters === 'function') updatePrinters(); } catch (e) {}
                                            cardToRemove.removeEventListener('animationend', onEndRem);
                                            cardToRemove.removeEventListener('transitionend', onEndRem);
                                        };
                                        cardToRemove.addEventListener('animationend', onEndRem);
                                        cardToRemove.addEventListener('transitionend', onEndRem);
                                        // safety fallback
                                        setTimeout(onEndRem, 800);
                                    } catch (e) {
                                        overlay.style.display = 'none'; document.body.style.overflow = ''; delete overlay.dataset.currentPrinterIp; try { if (typeof updatePrinters === 'function') updatePrinters(); } catch (er) {}
                                    }
                                } else {
                                    overlay.style.display = 'none'; document.body.style.overflow = ''; delete overlay.dataset.currentPrinterIp; updatePrinters();
                                }
                    } catch (e) { window.__pm_shared.error('Delete failed:', e); window.__pm_shared.showToast('Delete failed: ' + e.message, 'error'); }
                };
                actionsEl.appendChild(deleteBtn);
            } else {
                const saveBtn = document.createElement('button');
                saveBtn.textContent = 'Save Device';
                saveBtn.classList.add('primary');
                saveBtn.onclick = async function () {
                    saveBtn.disabled = true; saveBtn.textContent = 'Saving...';
                    const statusLine = document.createElement('div'); statusLine.style.cssText = 'margin-top: 12px; font-size: 0.9em; color: #93a1a1; text-align: center;';
                    try {
                        await window.__pm_shared.saveDiscoveredDevice(p.IP || p.ip, true, false);
                        saveBtn.textContent = 'Saved ✓'; actionsEl.appendChild(statusLine);
                        const cardToRemove = document.querySelector('.device-card[data-ip="' + (p.IP || p.ip) + '"]'); if (cardToRemove) cardToRemove.classList.add('removing');
                        setTimeout(async () => {
                            try {
                                let dots = 0; statusLine.textContent = 'Gathering additional details';
                                const dotInterval = setInterval(() => { dots = (dots + 1) % 4; statusLine.textContent = 'Gathering additional details' + '.'.repeat(dots); }, 400);
                                const body = { ip: p.IP || p.ip, max_entries: 5000 };
                                const r = await fetch('/mib_walk', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
                                clearInterval(dotInterval);
                                if (r.ok) { const j = await r.json(); updatePrinters(); }
                                statusLine.textContent = '✓ Details updated'; statusLine.style.color = '#859900';
                                setTimeout(() => { overlay.style.display = 'none'; document.body.style.overflow = ''; delete overlay.dataset.currentPrinterIp; }, 1200);
                            } catch (e) { window.__pm_shared.warn('Background refresh failed:', e); statusLine.textContent = '⚠ Refresh incomplete (device saved)'; statusLine.style.color = '#b58900'; setTimeout(() => { overlay.style.display = 'none'; document.body.style.overflow = ''; delete overlay.dataset.currentPrinterIp; }, 1500); }
                        }, 100);
                    } catch (e) { window.__pm_shared.error('Save failed:', e); window.__pm_shared.showToast('Save failed: ' + e.message, 'error'); saveBtn.disabled = false; saveBtn.textContent = 'Save Device'; if (statusLine.parentNode) statusLine.remove(); }
                };
                actionsEl.appendChild(saveBtn);
            }
            const closeBtn = document.createElement('button'); closeBtn.textContent = 'Close'; closeBtn.onclick = () => { overlay.style.display = 'none'; document.body.style.overflow = ''; delete overlay.dataset.currentPrinterIp; }; actionsEl.appendChild(closeBtn);
        } catch (e) { window.__pm_shared.warn('actions wiring failed', e); }

        // Load existing Web UI credentials for this device and wire save button
        try {
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
                    } catch (_) {}
                })();

                document.getElementById('save_creds_btn')?.addEventListener('click', async function () {
                    const statusEl = document.getElementById('creds_status');
                    const btn = document.getElementById('save_creds_btn');
                    if (!p || !p.serial) { if (statusEl) { statusEl.textContent = 'Error: no serial'; statusEl.style.color = '#dc322f'; } return; }
                    if (btn) btn.disabled = true;
                    if (statusEl) { statusEl.textContent = 'Saving...'; statusEl.style.color = 'var(--muted)'; }
                    try {
                        const payload = {
                            serial: p.serial,
                            username: document.getElementById('cred_username').value.trim(),
                            password: document.getElementById('cred_password').value,
                            auth_type: document.getElementById('cred_auth_type').value,
                            auto_login: document.getElementById('cred_auto_login').checked
                        };
                        const r = await fetch('/device/webui-credentials', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) });
                        if (!r.ok) throw new Error('Save failed');
                        if (statusEl) { statusEl.textContent = '✓ Saved'; statusEl.style.color = '#859900'; }
                        const pwdEl = document.getElementById('cred_password'); if (pwdEl) pwdEl.value = '';
                    } catch (e) {
                        const statusEl2 = document.getElementById('creds_status'); if (statusEl2) { statusEl2.textContent = 'Error: ' + e; statusEl2.style.color = '#dc322f'; }
                    } finally { const btn2 = document.getElementById('save_creds_btn'); if (btn2) btn2.disabled = false; }
                });
            }
        } catch (e) { window.__pm_shared.warn('cred wiring failed', e); }
    }

    // Export shared modal renderer
    window.__pm_shared_cards.showPrinterDetailsData = showPrinterDetailsData;

    // Render a discovered device card (shared implementation)
    function renderDiscoveredCard(p, isSaved) {
        const ip = p.ip || p.address || '';
        const serial = p.serial || '';
        const make = (p.make || p.manufacturer || '').toString();
        const model = (p.model || '').toString();
        const deviceKey = serial || ip || '';

        const savedClass = isSaved ? ' saved' : '';

    const saveBtn = isSaved ? '<button class="btn small" disabled>Saved</button>' : '<button class="btn primary small save-device-btn" data-ip="' + ip + '">Save</button>';
    const proxyBtn = serial ? '<button class="btn small" data-action="open-proxy" data-serial="' + encodeURIComponent(serial) + '">Proxy</button>' : '';

        let html = '';
        html += '<div class="device-card' + savedClass + '" data-device-key="' + deviceKey + '" data-ip="' + ip + '" data-serial="' + serial + '" data-make="' + (make || '') + '" data-model="' + (model || '') + '">';
        html += '<div class="printer-card-header">';
        html += '<div style="display:flex;justify-content:space-between;align-items:center">';
        html += '<div style="display:flex;flex-direction:column">';
        html += '<strong>' + (make ? (make + ' ') : '') + (model || '') + '</strong>';
        html += '<span style="color:var(--muted);font-size:12px">' + ip + '</span>';
        html += '</div>'; // left
        html += '<div style="display:flex;gap:8px">' + saveBtn + proxyBtn + '</div>';
        html += '</div></div>';
        html += '<div class="printer-card-body">';
        if (serial) html += '<div><strong>Serial:</strong> <code>' + serial + '</code></div>';
        html += '</div></div>';

        return html;
    }

    // Show a progressive discovery card while information is being gathered
    function showDiscoveringCard(data) {
        try {
            const discoveredContainer = document.getElementById('discovered_devices_cards');
            if (!discoveredContainer) return;

            const ip = data.ip || 'Unknown IP';
            const serial = data.serial || '';
            const method = data.method || '';
            const status = data.status || 'discovering';

            const cardId = 'discovering-' + ip.replace(/\./g, '-');
            let card = document.getElementById(cardId);

            if (!card) {
                card = document.createElement('div');
                card.id = cardId;
                card.className = 'printer-card discovering';
                card.style.opacity = '0.7';
                card.style.border = '2px dashed var(--accent)';
                discoveredContainer.insertBefore(card, discoveredContainer.firstChild);
            }

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

            setTimeout(() => {
                const existingCard = document.getElementById(cardId);
                if (existingCard) existingCard.remove();
            }, 30000);
        } catch (e) {
            window.__pm_shared.warn('shared.showDiscoveringCard failed', e);
        }
    }

    // Export shared discovered-card functions
    window.__pm_shared_cards.renderDiscoveredCard = renderDiscoveredCard;
    window.__pm_shared_cards.showDiscoveringCard = showDiscoveringCard;

    // Note: backwards-compatible global exports removed. Consumers should use
    // the namespaced `window.__pm_shared_cards` API (renderSavedCard,
    // renderDiscoveredCard, showDiscoveringCard, checkDatabaseRotationWarning,
    // renderCapabilities).

})();

