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
    function renderCapabilities(device) {
        return renderCapabilitiesFallback(device);
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
            console.error('Failed to check rotation warning:', err);
        }
    }

    // Export to shared namespace
    window.__pm_shared_cards = window.__pm_shared_cards || {};
    window.__pm_shared_cards.renderSavedCard = renderSavedCard;
    window.__pm_shared_cards.checkDatabaseRotationWarning = checkDatabaseRotationWarning;
    window.__pm_shared_cards.renderCapabilities = renderCapabilities;

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
                '<h4 style="margin-top:0;color:var(--highlight)'> + title + '</h4>' +
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

        // Device Info
        let deviceInfo = '<div style="display:flex;flex-direction:column;gap:6px">';
        deviceInfo += '<div style="display:grid;grid-template-columns:auto 1fr auto;gap:4px 8px;align-items:center;padding:4px 0">' +
            '<div style="color:var(--muted)">Manufacturer:</div><div>' + (p.manufacturer || '') + '</div></div>';
        deviceInfo += '<div style="display:grid;grid-template-columns:auto 1fr auto;gap:4px 8px;align-items:center;padding:4px 0">' +
            '<div style="color:var(--muted)">Model:</div><div>' + (p.model || '') + '</div></div>';
        deviceInfo += '<div style="display:grid;grid-template-columns:auto 1fr auto;gap:4px 8px;align-items:center;padding:4px 0">' +
            '<div style="color:var(--muted)">Serial:</div><div>' + (p.serial || '') + '</div></div>';
        if (p.last_seen) deviceInfo += '<div style="color:var(--muted);font-size:12px;margin-top:6px">Last Seen: ' + new Date(p.last_seen).toLocaleString() + '</div>';
        deviceInfo += '</div>';
        html += renderInfoCard('Device Info', deviceInfo);

        // Metrics card for saved devices (compact summary + quick-open buttons)
        if (source === 'saved' && p.serial) {
            const metricsHtml = '<div id="printer_metrics_summary" style="margin-top:8px"></div>' +
                '<div style="display:flex;gap:8px;align-items:center;margin-top:8px">' +
                '<button class="primary" onclick="showDeviceMetricsModal(\'' + p.serial + '\')">Open Metrics</button>' +
                '<button style="font-size:12px;padding:6px" onclick="showDeviceMetricsModal(\'' + p.serial + '\', \'7day\')">Open Last 7 Days</button>' +
                '</div>';
            html += renderInfoCard('Metrics', metricsHtml);
        }

        // Network Info (simple representation)
        let networkInfo = '<div style="display:flex;flex-direction:column;gap:6px">';
        networkInfo += renderRow('IP Address', p.ip || p.IP || '');
        networkInfo += renderRow('MAC Address', p.mac || p.mac_address || '');
        networkInfo += '</div>';
        html += renderInfoCard('Network', networkInfo);

        // Consumables (if present)
        const tonerLevels = p.toner_levels || p.toners || p.toner || {};
        if (tonerLevels && Object.keys(tonerLevels).length > 0) {
            let consumablesHtml = '<div class="consumable-container">';
            const tonerColors = { black:'#2c2c2c', cyan:'#00bcd4', magenta:'#e91e63', yellow:'#ffc107' };
            Object.entries(tonerLevels).forEach(([name, val]) => {
                const levelNum = Number(val) || 0;
                const bg = tonerColors[name.toLowerCase()] || '#666';
                const low = levelNum < 20;
                consumablesHtml += '<div class="consumable-item">' +
                    '<div class="consumable-icon" style="background:' + bg + '20"></div>' +
                    '<span class="consumable-label">' + name + '</span>' +
                    '<div class="consumable-bar"><div class="consumable-bar-fill" style="width:' + levelNum + '%;background:' + bg + (low ? ';opacity:0.5' : '') + '">' + (levelNum>15?levelNum+'%':'') + '</div></div>' +
                    '<span style="min-width:45px;text-align:right;font-family:monospace;color:var(--text);' + (low ? 'color:var(--highlight);font-weight:600' : '') + '">' + (levelNum<=15?levelNum+'%':'') + '</span>' +
                    '</div>';
            });
            consumablesHtml += '</div>';
            html += '<div class="panel" style="margin-top:8px">' +
                '<h4 style="margin-top:0;color:var(--highlight)">Consumables</h4>' +
                consumablesHtml +
                '</div>';
        }

        html += '</div>'; // close settings-grid

        // Action buttons area
        html += '<div style="margin-top:16px;display:flex;gap:8px;flex-wrap:wrap;padding:12px;background:rgba(0,0,0,0.1);border-radius:6px">';
        html += '<button id="refresh_data_btn">Refresh Details</button>';
        if (source === 'saved') html += '<button id="collect_metrics_btn">Collect Metrics</button>';
        html += '<span id="refresh_status" style="color:var(--muted);align-self:center"></span>';
        html += '</div>';

        try { bodyEl.innerHTML = html; } catch (e) { console.warn('rendering shared modal failed', e); }

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

                if (latest.toner_levels && Object.keys(latest.toner_levels).length > 0) {
                    for (const [color, level] of Object.entries(latest.toner_levels)) {
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
        } catch (e) { console.warn('lock wiring failed', e); }

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
                        // Re-open via agent helper which resolves discovered vs saved
                        if (typeof showPrinterDetails === 'function') showPrinterDetails(p.ip, source);
                    });
                } catch (err) {
                    if (statusEl) statusEl.textContent = ' Failed: ' + err;
                } finally {
                    btn.disabled = false;
                }
            });
        } catch (e) { console.warn('refresh wiring failed', e); }

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
        } catch (e) { console.warn('collect metrics wiring failed', e); }

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
                        if (!r.ok) { deleteBtn.disabled = false; deleteBtn.textContent = 'Delete Device'; try { window.__pm_shared.showToast('Delete failed', 'error'); } catch(_){}; return; }
                        deleteBtn.textContent = 'Deleted ✓';
                        try { window.__pm_shared.showToast('Device deleted successfully', 'success'); } catch(_){}
                        const cardToRemove = document.querySelector('.saved-device-card[data-serial="' + (p.serial || p.Serial) + '"]');
                        if (cardToRemove) {
                            cardToRemove.classList.add('removing');
                            setTimeout(() => { overlay.style.display = 'none'; document.body.style.overflow = ''; delete overlay.dataset.currentPrinterIp; if (typeof updatePrinters === 'function') updatePrinters(); }, 400);
                        } else {
                            overlay.style.display = 'none'; document.body.style.overflow = ''; delete overlay.dataset.currentPrinterIp; if (typeof updatePrinters === 'function') updatePrinters();
                        }
                    } catch (e) { console.error('Delete failed:', e); try { window.__pm_shared.showToast('Delete failed: ' + e.message, 'error'); } catch(_){} }
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
                        await saveDiscoveredDevice(p.IP || p.ip, true, false);
                        saveBtn.textContent = 'Saved ✓'; actionsEl.appendChild(statusLine);
                        const cardToRemove = document.querySelector('.device-card[data-ip="' + (p.IP || p.ip) + '"]'); if (cardToRemove) cardToRemove.classList.add('removing');
                        setTimeout(async () => {
                            try {
                                let dots = 0; statusLine.textContent = 'Gathering additional details';
                                const dotInterval = setInterval(() => { dots = (dots + 1) % 4; statusLine.textContent = 'Gathering additional details' + '.'.repeat(dots); }, 400);
                                const body = { ip: p.IP || p.ip, max_entries: 5000 };
                                const r = await fetch('/mib_walk', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
                                clearInterval(dotInterval);
                                if (r.ok) { const j = await r.json(); if (typeof updatePrinters === 'function') updatePrinters(); }
                                statusLine.textContent = '✓ Details updated'; statusLine.style.color = '#859900';
                                setTimeout(() => { overlay.style.display = 'none'; document.body.style.overflow = ''; delete overlay.dataset.currentPrinterIp; }, 1200);
                            } catch (e) { console.warn('Background refresh failed:', e); statusLine.textContent = '⚠ Refresh incomplete (device saved)'; statusLine.style.color = '#b58900'; setTimeout(() => { overlay.style.display = 'none'; document.body.style.overflow = ''; delete overlay.dataset.currentPrinterIp; }, 1500); }
                        }, 100);
                    } catch (e) { console.error('Save failed:', e); try { window.__pm_shared.showToast('Save failed: ' + e.message, 'error'); } catch(_){}; saveBtn.disabled = false; saveBtn.textContent = 'Save Device'; if (statusLine.parentNode) statusLine.remove(); }
                };
                actionsEl.appendChild(saveBtn);
            }
            const closeBtn = document.createElement('button'); closeBtn.textContent = 'Close'; closeBtn.onclick = () => { overlay.style.display = 'none'; document.body.style.overflow = ''; delete overlay.dataset.currentPrinterIp; }; actionsEl.appendChild(closeBtn);
        } catch (e) { console.warn('actions wiring failed', e); }

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
        } catch (e) { console.warn('cred wiring failed', e); }
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

        const saveBtn = isSaved ? '<button class="btn small" disabled>Saved</button>' : '<button class="btn primary small" onclick="saveDiscoveredDevice(\'' + ip + '\')">Save</button>';
        const proxyBtn = serial ? '<button class="btn small" onclick="window.open(\'/proxy/' + encodeURIComponent(serial) + '/\', \_blank\')">Proxy</button>' : '';

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
            console.warn('shared.showDiscoveringCard failed', e);
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

