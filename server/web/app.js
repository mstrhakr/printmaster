// PrintMaster Server - Web UI JavaScript

// ====== Initialization ======
document.addEventListener('DOMContentLoaded', function () {
    window.__pm_shared.log('PrintMaster Server UI loaded');
    
    // Initialize theme toggle
    initThemeToggle();
    
    // Initialize tabs
    initTabs();
    
    // Check config status and show warning if needed
    checkConfigStatus();
    
    // Load initial data
    loadServerStatus();
    loadAgents();
    
    // Set up periodic refresh for server status only
    // Keep the interval ID so we can cancel polling when WebSocket is active
    window._serverStatusInterval = setInterval(loadServerStatus, 30000); // Every 30 seconds

    // Try WebSocket first for low-latency liveness; fallback to SSE if WS not available
    connectWS();
    // Also keep SSE as a fallback
    connectSSE();
});

// ====== WebSocket Connection (UI liveness channel) ======
function connectWS() {
    try {
        const protocol = (location.protocol === 'https:') ? 'wss' : 'ws';
        const wsURL = protocol + '://' + location.host + '/api/ws/ui';
        const socket = new WebSocket(wsURL);

        socket.addEventListener('open', () => {
            window.__pm_shared.log('UI WebSocket connected, disabling /api/version polling');
            if (window._serverStatusInterval) {
                clearInterval(window._serverStatusInterval);
                window._serverStatusInterval = null;
            }
        });

        socket.addEventListener('message', (ev) => {
            try {
                const msg = JSON.parse(ev.data);
                // Handle version message specially
                if (msg.type === 'version') {
                    // Optionally update version badge in UI
                    if (msg.data && msg.data.version) {
                        const verEl = document.getElementById('server_version');
                        if (verEl) verEl.textContent = msg.data.version;
                    }
                }
                // Additional messages may be forwarded to existing handlers in future
            } catch (e) {
                window.__pm_shared.warn('Failed to parse WS message', e);
            }
        });

        socket.addEventListener('close', (e) => {
            window.__pm_shared.warn('UI WebSocket closed, falling back to polling and SSE', e);
            // Restart polling if not already running
            if (!window._serverStatusInterval) {
                window._serverStatusInterval = setInterval(loadServerStatus, 30000);
            }
            // Optionally try to reconnect after a delay
            setTimeout(connectWS, 5000);
        });

        socket.addEventListener('error', (e) => {
            window.__pm_shared.error('UI WebSocket error', e);
            // Let close handler restart fallback
        });
    } catch (e) {
        window.__pm_shared.warn('WebSocket not available, continuing with SSE/polling', e);
    }
}

// ====== SSE Connection ======
function connectSSE() {
    const eventSource = new EventSource('/api/events');
    eventSource.onopen = (e) => {
        window.__pm_shared.log('SSE onopen, readyState=', eventSource.readyState);
    };
    
    eventSource.addEventListener('connected', (e) => {
        window.__pm_shared.log('SSE connected:', e.data);
    });
    
    eventSource.addEventListener('agent_registered', (e) => {
        try {
            const data = JSON.parse(e.data);
            window.__pm_shared.log('Agent registered (SSE):', data);
            // Add card dynamically
            addAgentCard(data);
        } catch (err) {
            window.__pm_shared.warn('Failed to parse agent_registered event, falling back to full reload:', err);
            loadAgents();
        }
    });
    
    eventSource.addEventListener('agent_connected', (e) => {
        try {
            const data = JSON.parse(e.data);
            window.__pm_shared.log('Agent connected (SSE):', data);
            updateAgentConnection(data.agent_id, 'ws');
        } catch (err) {
            window.__pm_shared.warn('Failed to parse agent_connected event, falling back to full reload:', err);
            loadAgents();
        }
    });
    
    eventSource.addEventListener('agent_disconnected', (e) => {
        try {
            const data = JSON.parse(e.data);
            window.__pm_shared.log('Agent disconnected (SSE):', data);
            updateAgentConnection(data.agent_id, 'none');
        } catch (err) {
            window.__pm_shared.warn('Failed to parse agent_disconnected event, falling back to full reload:', err);
            loadAgents();
        }
    });
    
    eventSource.addEventListener('agent_heartbeat', (e) => {
        try {
            const data = JSON.parse(e.data);
            // Update agent's status/last seen in-place
            updateAgentHeartbeat(data.agent_id, data.status);
        } catch (err) {
            window.__pm_shared.log('Agent heartbeat (raw):', e.data);
        }
    });
    
    eventSource.addEventListener('device_updated', (e) => {
            try {
                const data = JSON.parse(e.data);
                window.__pm_shared.log('Device updated (SSE):', data);
                // If devices tab is visible, update in-place, otherwise ignore
                const devicesTab = document.querySelector('[data-tab="devices"]');
                if (devicesTab && !devicesTab.classList.contains('hidden')) {
                    addOrUpdateDeviceCard(data);
                }
            } catch (err) {
                window.__pm_shared.warn('Failed to parse device_updated event, falling back to full reload:', err);
                const devicesTab = document.querySelector('[data-tab="devices"]');
                if (devicesTab && !devicesTab.classList.contains('hidden')) {
                    loadDevices();
                }
            }
    });
    
    eventSource.onerror = (e) => {
        // EventSource provides automatic reconnects, but log useful state
        try {
            window.__pm_shared.error('SSE connection error:', e, 'readyState=', eventSource.readyState);
        } catch (ex) {
            window.__pm_shared.error('SSE connection error and failed to read readyState', ex);
        }
        // EventSource will automatically try to reconnect
    };
}

// ====== Config Status Check ======
function checkConfigStatus() {
    // Check if user dismissed this warning
    if (localStorage.getItem('hideConfigWarning') === 'true') {
        return;
    }
    
    fetch('/api/config/status')
        .then(res => res.json())
        .then(data => {
            if (data.errors && data.errors.length > 0) {
                // Config errors found - show modal with details
                let message = 'The server configuration file(s) failed to load:\n\n';
                data.errors.forEach(err => {
                    message += `• ${err}\n`;
                });
                message += '\nThe server is running with default settings. Please check your config.toml file.';
                
                window.__pm_shared.showAlert(message, '⚠️ Configuration Error', true, true);
            } else if (data.using_defaults) {
                // No config file found - show informational modal
                let message = 'No configuration file was found in any of these locations:\n\n';
                data.searched_paths.forEach(path => {
                    message += `• ${path}\n`;
                });
                message += '\nThe server is running with default settings.';
                
                window.__pm_shared.showAlert(message, 'ℹ️ Using Default Configuration', false, true);
            }
        })
        .catch(err => {
            window.__pm_shared.error('Failed to check config status:', err);
        });
}

// Metrics modal (delegate to shared implementation)
function showDeviceMetricsModal(serial, preset) {
    if (!serial) return;
    if (typeof window !== 'undefined' && typeof window.showMetricsModal === 'function') {
        try {
            window.showMetricsModal({ serial, preset });
            return;
        } catch (e) {
            window.__pm_shared.warn('shared.showMetricsModal failed', e);
        }
    }
    // Fallback: minimal alert
    window.__pm_shared.showAlert('Metrics UI not available for ' + serial, 'Metrics', false, false);
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
        window.__pm_shared.warn('toggleMetricsTimeSelector failed', e);
    }
}

// ====== Theme Toggle ======
function initThemeToggle() {
    const toggle = document.getElementById('theme-toggle-checkbox');
    const savedTheme = localStorage.getItem('theme') || 'dark';
    
    if (savedTheme === 'light') {
        toggle.checked = true;
        document.body.classList.add('light-mode');
    }
    
    toggle.addEventListener('change', function () {
        if (this.checked) {
            document.body.classList.add('light-mode');
            localStorage.setItem('theme', 'light');
        } else {
            document.body.classList.remove('light-mode');
            localStorage.setItem('theme', 'dark');
        }
    });
}

// ====== Tab Management ======
function initTabs() {
    const tabs = document.querySelectorAll('.tab');
    const mobileTabs = document.querySelectorAll('.mobile-nav .tab');
    const hamburger = document.querySelector('.hamburger-icon');
    const mobileNav = document.getElementById('mobile_nav');
    
    // Desktop tabs
    tabs.forEach(tab => {
        tab.addEventListener('click', () => switchTab(tab.dataset.target));
    });
    
    // Mobile tabs
    mobileTabs.forEach(tab => {
        tab.addEventListener('click', () => {
            switchTab(tab.dataset.target);
            mobileNav.classList.remove('active');
        });
    });
    
    // Hamburger menu
    if (hamburger) {
        hamburger.addEventListener('click', () => {
            mobileNav.classList.toggle('active');
        });
    }
    
    function switchTab(targetTab) {
        // Hide all tabs
        document.querySelectorAll('[data-tab]').forEach(tab => {
            tab.classList.add('hidden');
        });
        
        // Remove active class from all tab buttons
        document.querySelectorAll('.tab').forEach(tab => {
            tab.classList.remove('active');
        });
        
        // Show target tab
        const target = document.querySelector(`[data-tab="${targetTab}"]`);
        if (target) {
            target.classList.remove('hidden');
        }
        
        // Add active class to clicked tab buttons
        document.querySelectorAll(`.tab[data-target="${targetTab}"]`).forEach(tab => {
            tab.classList.add('active');
        });
        
        // Update mobile menu label
        const label = document.getElementById('current_tab_label');
        if (label) {
            const tabNames = {
                'agents': 'Agents',
                'devices': 'Devices',
                'metrics': 'Metrics',
                'settings': 'Settings',
                'logs': 'Logs'
            };
            label.textContent = 'Menu - ' + (tabNames[targetTab] || targetTab);
        }
        
        // Load data for specific tabs
        if (targetTab === 'devices') {
            loadDevices();
        } else if (targetTab === 'metrics') {
            loadMetrics();
        } else if (targetTab === 'settings') {
            loadSettings();
        } else if (targetTab === 'logs') {
            // Previously called connectLogStream() which no longer exists - use loadLogs()
            loadLogs();
        }
    }
}

// Wrapper functions removed: call sites should use window.__pm_shared.showToast / showConfirm / showAlert directly.

// ====== Server Status ======
async function loadServerStatus() {
    try {
        const response = await fetch('/api/version');
        if (!response.ok) {
            const el = document.getElementById('server_status');
            if (el) el.innerHTML = '<span style="color:var(--error);">● Error</span>';
            else window.__pm_shared.warn('server_status element not found in DOM');
            return;
        }
        
        const data = await response.json();
        const el = document.getElementById('server_status');
        if (el) el.innerHTML = `<span style="color:var(--success);">● Online</span> v${data.version}`;
        else window.__pm_shared.warn('server_status element not found in DOM');
    } catch (error) {
        window.__pm_shared.error('Failed to load server status:', error);
        const errEl = document.getElementById('server_status');
        if (errEl) errEl.innerHTML = '<span style="color:var(--error);">● Error loading status</span>';
        else window.__pm_shared.warn('server_status element not found in DOM while handling error');
    }
}

// ====== Agents Management ======
async function loadAgents() {
    try {
        // TODO: This endpoint needs to be created in server/main.go
        const response = await fetch('/api/v1/agents/list');
        if (!response.ok) {
            throw new Error(`HTTP ${response.status}`);
        }
        
        const agents = await response.json();
        renderAgents(agents);
    } catch (error) {
        window.__pm_shared.error('Failed to load agents:', error);
        const listEl = document.getElementById('agents_list');
        if (listEl) {
            listEl.innerHTML = '<div style="color:var(--error);">Failed to load agents: ' + error.message + '</div>';
        } else {
            window.__pm_shared.warn('agents_list element not found in DOM while handling loadAgents error');
        }
    }
}

function renderAgents(agents) {
    const container = document.getElementById('agents_list');
    const statsContainer = document.getElementById('agents_stats');

    if (!container) {
        window.__pm_shared.warn('renderAgents: agents_list element not found - aborting render');
        return;
    }

    if (!agents || agents.length === 0) {
        container.innerHTML = '<div style="color:var(--muted);">No agents connected yet</div>';
        if (statsContainer) statsContainer.innerHTML = '<div style="color:var(--muted);">Total Agents: 0</div>';
        return;
    }
    
    // Update stats
    const activeCount = agents.filter(a => a.status === 'active').length;
    const inactiveCount = agents.filter(a => a.status === 'inactive').length;
    const offlineCount = agents.filter(a => a.status === 'offline').length;
    
    statsContainer.innerHTML = `
        <div><strong>Total Agents:</strong> ${agents.length}</div>
        <div><strong>Active:</strong> <span style="color:var(--success)">${activeCount}</span></div>
        <div><strong>Inactive:</strong> <span style="color:var(--muted)">${inactiveCount}</span></div>
        <div><strong>Offline:</strong> <span style="color:var(--error)">${offlineCount}</span></div>
    `;
    
    // Render agent cards
    container.innerHTML = '<div class="device-cards-container">' + 
        agents.map(agent => renderAgentCard(agent)).join('') + 
        '</div>';
}

function renderAgentCard(agent) {
    const statusColors = {
        'active': 'var(--success)',
        'inactive': 'var(--muted)',
        'offline': 'var(--error)'
    };
    
    const statusColor = statusColors[agent.status] || 'var(--muted)';
    const lastSeenDate = agent.last_seen ? new Date(agent.last_seen) : null;
    const registeredDate = agent.registered_at ? new Date(agent.registered_at) : null;
    
    // Calculate time since last seen
    let lastSeenText = 'Never';
    if (lastSeenDate) {
        const now = new Date();
        const diff = now - lastSeenDate;
        const seconds = Math.floor(diff / 1000);
        const minutes = Math.floor(seconds / 60);
        const hours = Math.floor(minutes / 60);
        const days = Math.floor(hours / 24);
        
        if (days > 0) lastSeenText = `${days}d ago`;
        else if (hours > 0) lastSeenText = `${hours}h ago`;
        else if (minutes > 0) lastSeenText = `${minutes}m ago`;
        else lastSeenText = 'Just now';
    }
    
    return `
        <div class="device-card" data-agent-id="${agent.agent_id}">
            <div class="device-card-header">
                <div>
                    <div style="display:flex;align-items:center;gap:8px">
                        <div class="device-card-title">${agent.hostname || agent.agent_id}</div>
                    </div>
                    <div class="device-card-subtitle copyable" data-copy="${agent.agent_id}" title="Click to copy Agent ID">
                        ${agent.agent_id}
                    </div>
                </div>
            </div>
            
            <div class="device-card-info">
                <div class="device-card-row">
                    <span class="device-card-label">Status</span>
                    <span class="device-card-value agent-status-value" style="color:${statusColor}">
                        ● ${agent.status || 'unknown'}
                    </span>
                </div>

                <div class="device-card-row">
                    <span class="device-card-label">WebSockets</span>
                    <span class="device-card-value">
                        ${ (function(){
                            let label = '';
                            let title = '';
                            let cls = 'none';
                            if (agent.connection_type === 'ws') {
                                label = 'Live';
                                title = 'WebSocket (live)';
                                cls = 'ws';
                            } else if (agent.connection_type === 'http') {
                                label = 'HTTP(s) Fallback';
                                title = 'HTTP(s) recent fallback';
                                cls = 'http';
                            } else {
                                label = 'Disconnected';
                                title = 'Disconnected';
                                cls = 'none';
                            }
                            // Include optional websocket subsystem version if provided
                            const ver = agent.ws_version ? ` <span class="ws-version">v${agent.ws_version}</span>` : '';
                            return `<span class="conn-badge ${cls}" title="${title}" aria-label="WEBSOCKETS: ${label}">WEBSOCKETS: ${label}</span>${ver}`;
                        })() }
                    </span>
                </div>
                
                <div class="device-card-row">
                    <span class="device-card-label">IP Address</span>
                    <span class="device-card-value copyable" data-copy="${agent.ip || ''}" title="Click to copy">
                        ${agent.ip || 'N/A'}
                    </span>
                </div>
                
                <div class="device-card-row">
                    <span class="device-card-label">Platform</span>
                    <span class="device-card-value">${agent.platform || 'Unknown'}</span>
                </div>
                
                <div class="device-card-row">
                    <span class="device-card-label">Version</span>
                    <span class="device-card-value">${agent.version || 'N/A'}</span>
                </div>
                
                <div class="device-card-row">
                    <span class="device-card-label">Last Seen</span>
                    <span class="device-card-value agent-last-seen" title="${lastSeenDate ? lastSeenDate.toLocaleString() : 'Never'}">
                        ${lastSeenText}
                    </span>
                </div>
                
                ${registeredDate ? `
                <div class="device-card-row">
                    <span class="device-card-label">Registered</span>
                    <span class="device-card-value" title="${registeredDate.toLocaleString()}">
                        ${registeredDate.toLocaleDateString()}
                    </span>
                </div>
                ` : ''}
            </div>
            
            <div class="device-card-actions">
                <button data-action="view-agent" data-agent-id="${agent.agent_id}">View Details</button>
                <button data-action="open-agent" data-agent-id="${agent.agent_id}" ${agent.status !== 'active' ? 'disabled title="Agent not connected via WebSocket"' : ''}>
                    Open UI
                </button>
                <button data-action="delete-agent" data-agent-id="${agent.agent_id}" data-agent-name="${agent.hostname || agent.agent_id}" 
                    style="background: var(--btn-delete-bg); color: var(--btn-delete-text); border: 1px solid var(--btn-delete-border);">
                    Delete
                </button>
            </div>
        </div>
    `;
}

// DOM helpers for incremental updates
function findAgentsContainer() {
    const container = document.getElementById('agents_list');
    if (!container) return null;
    let cards = container.querySelector('.device-cards-container');
    if (!cards) {
        cards = document.createElement('div');
        cards.className = 'device-cards-container';
        container.innerHTML = '';
        container.appendChild(cards);
    }
    return cards;
}

function addAgentCard(agent) {
    const cards = findAgentsContainer();
    if (!cards) return;
    // If card exists, replace it
    const existing = cards.querySelector(`[data-agent-id="${agent.agent_id}"]`);
    if (existing) {
        existing.outerHTML = renderAgentCard(agent);
        return;
    }
    // Insert at top
    cards.insertAdjacentHTML('afterbegin', renderAgentCard(agent));
}

function removeAgentCard(agentId) {
    const cards = findAgentsContainer();
    if (!cards) return;
    const existing = cards.querySelector(`[data-agent-id="${agentId}"]`);
    if (existing) {
        existing.classList.add('removing');
        setTimeout(() => existing.remove(), 300);
    }
}

function updateAgentConnection(agentId, connType) {
    const cards = findAgentsContainer();
    if (!cards) return;
    const card = cards.querySelector(`[data-agent-id="${agentId}"]`);
    if (!card) {
        // If card missing, fetch single agent and add
        fetch(`/api/v1/agents/${agentId}`).then(r => r.json()).then(a => addAgentCard(a)).catch(() => {});
        return;
    }
    // Update connection badge
    const badge = card.querySelector('.conn-badge');
    if (badge) {
        badge.classList.remove('ws','http','none');
        badge.classList.add(connType);
        const aria = connType === 'ws' ? 'Connection: WebSocket (live)' : connType === 'http' ? 'Connection: HTTP (recent)' : 'Connection: Offline';
        badge.setAttribute('aria-label', aria);
        badge.setAttribute('title', aria);
    }
    // Update Open UI button enablement
    const openBtn = card.querySelector('.device-card-actions button[onclick^="openAgentUI"]');
    if (openBtn) {
        if (connType === 'ws') {
            openBtn.removeAttribute('disabled');
            openBtn.removeAttribute('title');
        } else {
            openBtn.setAttribute('disabled', 'disabled');
            openBtn.setAttribute('title', 'Agent not connected via WebSocket');
        }
    }
}

function updateAgentHeartbeat(agentId, status) {
    const cards = findAgentsContainer();
    if (!cards) return;
    const card = cards.querySelector(`[data-agent-id="${agentId}"]`);
    if (!card) return;
    const statusEl = card.querySelector('.agent-status-value');
    if (statusEl) {
        statusEl.textContent = '● ' + (status || 'unknown');
        const colors = { 'active': 'var(--success)', 'inactive': 'var(--muted)', 'offline': 'var(--error)' };
        statusEl.style.color = colors[status] || 'var(--muted)';
    }
    // Refresh last seen by fetching agent details
    fetch(`/api/v1/agents/${agentId}`).then(r => r.json()).then(a => {
        const lastSeenEl = card.querySelector('.agent-last-seen');
        if (lastSeenEl && a.last_seen) {
            const dt = new Date(a.last_seen);
            const now = new Date();
            const diff = now - dt;
            const seconds = Math.floor(diff/1000);
            const minutes = Math.floor(seconds/60);
            const hours = Math.floor(minutes/60);
            let text = 'Never';
            if (hours > 0) text = `${hours}h ago`;
            else if (minutes > 0) text = `${minutes}m ago`;
            else if (seconds > 0) text = 'Just now';
            lastSeenEl.textContent = text;
            lastSeenEl.title = dt.toLocaleString();
        }
    }).catch(() => {});
}

// Device helpers for server UI
function addOrUpdateDeviceCard(device) {
    const container = document.getElementById('devices_cards');
    if (!container) return;
    const serial = device.serial || '';
    if (!serial) {
        // fallback: reload full devices
        loadDevices();
        return;
    }
    const existing = container.querySelector(`[data-serial="${serial}"]`);
    const cardHtml = (function(d) {
        return `
        <div class="device-card" data-serial="${d.serial || ''}" data-agent-id="${d.agent_id || ''}">
            <div class="device-card-header">
                <div>
                    <div class="device-card-title">${d.manufacturer || 'Unknown'} ${d.model || ''}</div>
                    <div class="device-card-subtitle">${d.ip || 'N/A'}</div>
                </div>
            </div>
            <div class="device-card-info">
                <div class="device-card-row">
                    <span class="device-card-label">Serial</span>
                    <span class="device-card-value device-serial">${d.serial || 'N/A'}</span>
                </div>
                <div class="device-card-row">
                    <span class="device-card-label">Agent</span>
                    <span class="device-card-value device-agent-id">${d.agent_id || 'N/A'}</span>
                </div>
            </div>
            <div class="device-card-actions">
                <button data-action="open-device" data-serial="${d.serial}" data-agent-id="${d.agent_id}" ${!d.ip || !d.agent_id ? 'disabled title="Device has no IP or agent"' : ''}>
                    Open Web UI
                </button>
            </div>
        </div>
        `;
    })(device);
    if (existing) {
        existing.outerHTML = cardHtml;
    } else {
        // insert at top
        container.insertAdjacentHTML('afterbegin', cardHtml);
    }
}

// ====== Agent Details ======
async function viewAgentDetails(agentId) {
    try {
        // Show modal immediately with loading state
        const modal = document.getElementById('agent_details_modal');
        const body = document.getElementById('agent_details_body');
        const title = document.getElementById('agent_details_title');
        
        modal.style.display = 'flex';
        body.innerHTML = '<div style="color:var(--muted);text-align:center;padding:20px;">Loading agent details...</div>';
        title.textContent = 'Agent Details';
        
        const response = await fetch(`/api/v1/agents/${agentId}`);
        if (!response.ok) {
            throw new Error(`HTTP ${response.status}`);
        }
        
        const agent = await response.json();
        renderAgentDetailsModal(agent);
    } catch (error) {
        window.__pm_shared.error('Failed to load agent details:', error);
        const body = document.getElementById('agent_details_body');
        body.innerHTML = `<div style="color:var(--error);text-align:center;padding:20px;">Failed to load agent details: ${error.message}</div>`;
        window.__pm_shared.showToast('Failed to load agent details', 'error');
    }
}

function renderAgentDetailsModal(agent) {
    const title = document.getElementById('agent_details_title');
    const body = document.getElementById('agent_details_body');
    
    title.textContent = `Agent: ${agent.hostname || agent.agent_id}`;
    
    const lastSeenDate = agent.last_seen ? new Date(agent.last_seen) : null;
    const registeredDate = agent.registered_at ? new Date(agent.registered_at) : null;
    const lastHeartbeatDate = agent.last_heartbeat ? new Date(agent.last_heartbeat) : null;
    const lastDeviceSyncDate = agent.last_device_sync ? new Date(agent.last_device_sync) : null;
    const lastMetricsSyncDate = agent.last_metrics_sync ? new Date(agent.last_metrics_sync) : null;
    
    // Calculate uptime
    let uptimeText = 'N/A';
    if (registeredDate && lastSeenDate) {
        const uptimeMs = lastSeenDate - registeredDate;
        const days = Math.floor(uptimeMs / (1000 * 60 * 60 * 24));
        const hours = Math.floor((uptimeMs % (1000 * 60 * 60 * 24)) / (1000 * 60 * 60));
        uptimeText = `${days}d ${hours}h`;
    }
    
    const statusColors = {
        'active': 'var(--success)',
        'inactive': 'var(--muted)',
        'offline': 'var(--error)'
    };
    const statusColor = statusColors[agent.status] || 'var(--muted)';
    
    body.innerHTML = `
        <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 20px;">
            <!-- Basic Info -->
            <div class="panel">
                <h4 style="margin-top:0;color:var(--highlight);font-size:14px;">Basic Information</h4>
                <div style="display:flex;flex-direction:column;gap:8px;font-size:13px;">
                    <div class="device-card-row">
                        <span class="device-card-label">Agent ID</span>
                        <span class="device-card-value copyable" data-copy="${agent.agent_id}" title="Click to copy">
                            ${agent.agent_id}
                        </span>
                    </div>
                    <div class="device-card-row">
                        <span class="device-card-label">Hostname</span>
                        <span class="device-card-value">${agent.hostname || 'N/A'}</span>
                    </div>
                    <div class="device-card-row">
                        <span class="device-card-label">IP Address</span>
                        <span class="device-card-value copyable" data-copy="${agent.ip || ''}" title="Click to copy">
                            ${agent.ip || 'N/A'}
                        </span>
                    </div>
                    <div class="device-card-row">
                        <span class="device-card-label">Status</span>
                        <span class="device-card-value" style="color:${statusColor}">
                            ● ${agent.status || 'unknown'}
                        </span>
                    </div>
                </div>
            </div>
            
            <!-- System Info -->
            <div class="panel">
                <h4 style="margin-top:0;color:var(--highlight);font-size:14px;">System Information</h4>
                <div style="display:flex;flex-direction:column;gap:8px;font-size:13px;">
                    <div class="device-card-row">
                        <span class="device-card-label">Platform</span>
                        <span class="device-card-value">${agent.platform || 'Unknown'}</span>
                    </div>
                    ${agent.os_version ? `
                    <div class="device-card-row">
                        <span class="device-card-label">OS Version</span>
                        <span class="device-card-value">${agent.os_version}</span>
                    </div>
                    ` : ''}
                    ${agent.architecture ? `
                    <div class="device-card-row">
                        <span class="device-card-label">Architecture</span>
                        <span class="device-card-value">${agent.architecture}</span>
                    </div>
                    ` : ''}
                    ${agent.num_cpu ? `
                    <div class="device-card-row">
                        <span class="device-card-label">CPUs</span>
                        <span class="device-card-value">${agent.num_cpu}</span>
                    </div>
                    ` : ''}
                    ${agent.total_memory_mb ? `
                    <div class="device-card-row">
                        <span class="device-card-label">Memory</span>
                        <span class="device-card-value">${(agent.total_memory_mb / 1024).toFixed(2)} GB</span>
                    </div>
                    ` : ''}
                </div>
            </div>
            
            <!-- Version Info -->
            <div class="panel">
                <h4 style="margin-top:0;color:var(--highlight);font-size:14px;">Version Information</h4>
                <div style="display:flex;flex-direction:column;gap:8px;font-size:13px;">
                    <div class="device-card-row">
                        <span class="device-card-label">Agent Version</span>
                        <span class="device-card-value">${agent.version || 'N/A'}</span>
                    </div>
                    <div class="device-card-row">
                        <span class="device-card-label">Protocol Version</span>
                        <span class="device-card-value">${agent.protocol_version || 'N/A'}</span>
                    </div>
                    ${agent.go_version ? `
                    <div class="device-card-row">
                        <span class="device-card-label">Go Version</span>
                        <span class="device-card-value">${agent.go_version}</span>
                    </div>
                    ` : ''}
                    ${agent.build_type ? `
                    <div class="device-card-row">
                        <span class="device-card-label">Build Type</span>
                        <span class="device-card-value">${agent.build_type}</span>
                    </div>
                    ` : ''}
                    ${agent.git_commit && agent.git_commit !== 'unknown' ? `
                    <div class="device-card-row">
                        <span class="device-card-label">Git Commit</span>
                        <span class="device-card-value copyable" data-copy="${agent.git_commit}" title="Click to copy">
                            ${agent.git_commit.substring(0, 8)}...
                        </span>
                    </div>
                    ` : ''}
                </div>
            </div>
            
            <!-- Activity -->
            <div class="panel">
                <h4 style="margin-top:0;color:var(--highlight);font-size:14px;">Activity</h4>
                <div style="display:flex;flex-direction:column;gap:8px;font-size:13px;">
                    ${registeredDate ? `
                    <div class="device-card-row">
                        <span class="device-card-label">Registered</span>
                        <span class="device-card-value" title="${registeredDate.toLocaleString()}">
                            ${registeredDate.toLocaleDateString()}
                        </span>
                    </div>
                    ` : ''}
                    ${lastSeenDate ? `
                    <div class="device-card-row">
                        <span class="device-card-label">Last Seen</span>
                        <span class="device-card-value" title="${lastSeenDate.toLocaleString()}">
                            ${lastSeenDate.toLocaleString()}
                        </span>
                    </div>
                    ` : ''}
                    ${lastHeartbeatDate ? `
                    <div class="device-card-row">
                        <span class="device-card-label">Last Heartbeat</span>
                        <span class="device-card-value" title="${lastHeartbeatDate.toLocaleString()}">
                            ${lastHeartbeatDate.toLocaleString()}
                        </span>
                    </div>
                    ` : ''}
                    <div class="device-card-row">
                        <span class="device-card-label">Uptime</span>
                        <span class="device-card-value">${uptimeText}</span>
                    </div>
                </div>
            </div>
            
            <!-- Data Sync -->
            <div class="panel">
                <h4 style="margin-top:0;color:var(--highlight);font-size:14px;">Data Synchronization</h4>
                <div style="display:flex;flex-direction:column;gap:8px;font-size:13px;">
                    <div class="device-card-row">
                        <span class="device-card-label">Devices</span>
                        <span class="device-card-value">${agent.device_count || 0}</span>
                    </div>
                    ${lastDeviceSyncDate ? `
                    <div class="device-card-row">
                        <span class="device-card-label">Last Device Sync</span>
                        <span class="device-card-value" title="${lastDeviceSyncDate.toLocaleString()}">
                            ${lastDeviceSyncDate.toLocaleString()}
                        </span>
                    </div>
                    ` : `
                    <div class="device-card-row">
                        <span class="device-card-label">Last Device Sync</span>
                        <span class="device-card-value" style="color:var(--muted);">Never</span>
                    </div>
                    `}
                    ${lastMetricsSyncDate ? `
                    <div class="device-card-row">
                        <span class="device-card-label">Last Metrics Sync</span>
                        <span class="device-card-value" title="${lastMetricsSyncDate.toLocaleString()}">
                            ${lastMetricsSyncDate.toLocaleString()}
                        </span>
                    </div>
                    ` : `
                    <div class="device-card-row">
                        <span class="device-card-label">Last Metrics Sync</span>
                        <span class="device-card-value" style="color:var(--muted);">Never</span>
                    </div>
                    `}
                </div>
            </div>
        </div>
        
        <!-- WS Diagnostics -->
        <div style="margin-top:16px;">
            <div class="panel">
                <h4 style="margin-top:0;color:var(--highlight);font-size:14px;">WebSocket Diagnostics</h4>
                <div style="display:flex;flex-direction:column;gap:8px;font-size:13px;">
                    <div class="device-card-row">
                        <span class="device-card-label">Ping Failures</span>
                        <span class="device-card-value">${agent.ws_ping_failures || 0}</span>
                    </div>
                    <div class="device-card-row">
                        <span class="device-card-label">Disconnect Events</span>
                        <span class="device-card-value">${agent.ws_disconnect_events || 0}</span>
                    </div>
                    <div style="color:var(--muted);font-size:12px;">These counts are diagnostics from the server's WebSocket subsystem. They help indicate flaky connections or network issues.</div>
                </div>
            </div>
        </div>
        <!-- Action Buttons -->
        <div style="margin-top: 20px; display: flex; gap: 10px; justify-content: flex-end;">
            <button data-action="open-agent" data-agent-id="${agent.agent_id}" ${agent.status !== 'active' ? 'disabled title="Agent not connected via WebSocket"' : ''}>
                Open Agent UI
            </button>
        </div>
    `;
}

// ====== Delete Agent ======
async function deleteAgent(agentId, displayName) {
    window.__pm_shared.log('deleteAgent called:', agentId, displayName);
    
    const confirmed = await window.__pm_shared.showConfirm(
        `Are you sure you want to delete agent "${displayName}"?\n\nThis will permanently remove the agent and all its associated devices and metrics. This action cannot be undone.`,
        'Delete Agent',
        true
    );
    
    window.__pm_shared.log('User confirmed:', confirmed);
    
    if (!confirmed) {
        return;
    }
    
    try {
        window.__pm_shared.log('Sending DELETE request to:', `/api/v1/agents/${agentId}`);
        const response = await fetch(`/api/v1/agents/${agentId}`, {
            method: 'DELETE'
        });
        
        window.__pm_shared.log('Response status:', response.status, response.statusText);
        
        if (!response.ok) {
            const errorText = await response.text();
            window.__pm_shared.error('Delete failed:', errorText);
            throw new Error(`HTTP ${response.status}: ${errorText}`);
        }
        
        const result = await response.json();
        window.__pm_shared.log('Delete successful:', result);
        
    window.__pm_shared.showToast(`Agent "${displayName}" deleted successfully`, 'success');
        
        // Remove agent card with animation
        const card = document.querySelector(`[data-agent-id="${agentId}"]`);
        if (card) {
            card.classList.add('removing');
            setTimeout(() => {
                // Reload agents list
                loadAgents();
            }, 400); // Match animation duration
        } else {
            // Card not found, just reload
            loadAgents();
        }
    } catch (error) {
        window.__pm_shared.error('Failed to delete agent:', error);
        window.__pm_shared.showToast(`Failed to delete agent: ${error.message}`, 'error');
    }
}

// ====== Devices Management ======
async function loadDevices() {
    try {
        const response = await fetch('/api/v1/devices/list');
        if (!response.ok) {
            throw new Error(`HTTP ${response.status}`);
        }
        
        const devices = await response.json();
        renderDevices(devices);
    } catch (error) {
        window.__pm_shared.error('Failed to load devices:', error);
        const cardsEl = document.getElementById('devices_cards');
        if (cardsEl) {
            cardsEl.innerHTML = '<div style="color:var(--error);">Failed to load devices</div>';
        } else {
            window.__pm_shared.warn('loadDevices: devices_cards element not found in DOM while handling error');
        }
    }
}

function renderDevices(devices) {
    const container = document.getElementById('devices_cards');
    const statsContainer = document.getElementById('devices_stats');

    if (!container) {
        window.__pm_shared.warn('renderDevices: devices_cards element not found - aborting render');
        return;
    }

    if (!devices || devices.length === 0) {
        container.innerHTML = '<div style="color:var(--muted);">No devices found</div>';
        if (statsContainer) statsContainer.innerHTML = '<div style="color:var(--muted);">Total Devices: 0</div>';
        return;
    }

    // Update stats
    if (statsContainer) {
        statsContainer.innerHTML = `
        <div><strong>Total Devices:</strong> ${devices.length}</div>
    `;
    }
    
    // Render device cards (simplified for now) with data-serial for targeted updates
    function renderDeviceCard(device) {
        return `
        <div class="device-card" data-serial="${device.serial || ''}" data-agent-id="${device.agent_id || ''}">
            <div class="device-card-header">
                <div>
                    <div class="device-card-title">${device.manufacturer || 'Unknown'} ${device.model || ''}</div>
                    <div class="device-card-subtitle">${device.ip || 'N/A'}</div>
                </div>
            </div>
            <div class="device-card-info">
                <div class="device-card-row">
                    <span class="device-card-label">Serial</span>
                    <span class="device-card-value device-serial">${device.serial || 'N/A'}</span>
                </div>
                <div class="device-card-row">
                    <span class="device-card-label">Agent</span>
                    <span class="device-card-value device-agent-id">${device.agent_id || 'N/A'}</span>
                </div>
            </div>
            <div class="device-card-actions">
                <button data-action="open-device" data-serial="${device.serial}" data-agent-id="${device.agent_id}" ${!device.ip || !device.agent_id ? 'disabled title="Device has no IP or agent"' : ''}>
                    Open Web UI
                </button>
                <button data-action="view-metrics" data-serial="${device.serial}" ${!device.serial ? 'disabled title="No serial"' : ''}>
                    View Metrics
                </button>
                <button data-action="show-printer-details" data-ip="${device.ip||''}" data-source="saved" ${!device.ip ? 'disabled title="No IP"' : ''}>
                    Details
                </button>
            </div>
        </div>
        `;
    }

    container.innerHTML = devices.map(device => renderDeviceCard(device)).join('');
}

// Show printer details modal by finding the device in the server device list
async function showPrinterDetails(ip, source) {
    if (!ip) return;
    source = source || 'saved';
    try {
        const res = await fetch('/api/v1/devices/list');
        if (!res.ok) throw new Error('Failed to fetch devices');
        const devices = await res.json();
        if (!Array.isArray(devices)) throw new Error('Invalid device list');

        // Try to find by ip or serial
        let found = devices.find(d => (d.ip && d.ip === ip) || (d.serial && d.serial === ip) || (d.printer_info && (d.printer_info.ip === ip || d.printer_info.serial === ip)));
        if (!found && devices.length === 1 && devices[0].ip === ip) found = devices[0];

        if (!found) {
            window.__pm_shared.showToast('Device not found', 'error');
            return;
        }

        // Normalize shape: server devices might embed printer_info under different keys
        const deviceObj = found.printer_info ? Object.assign({}, found.printer_info, { serial: found.serial || (found.printer_info && found.printer_info.serial) }) : found;

        window.__pm_shared_cards.showPrinterDetailsData(deviceObj, source, null);
    } catch (err) {
        window.__pm_shared.error('showPrinterDetails failed', err);
        window.__pm_shared.showToast('Failed to load device details', 'error');
    }
}

// ====== Utility Functions ======
function copyToClipboard(text) {
    if (!text) return;
    
    navigator.clipboard.writeText(text).then(() => {
        window.__pm_shared.showToast('Copied to clipboard', 'success', 1500);
    }).catch(err => {
        window.__pm_shared.error('Failed to copy:', err);
        window.__pm_shared.showToast('Failed to copy to clipboard', 'error');
    });
}

// ====== Proxy Functions ======
function openAgentUI(agentId) {
    // Open agent's web UI through WebSocket proxy in a new window
    // Ensure agentId is URL-encoded to avoid embedding spaces or unsafe chars
    const proxyUrl = `/api/v1/proxy/agent/${encodeURIComponent(agentId)}/`;
    window.open(proxyUrl, `agent-ui-${encodeURIComponent(agentId)}`, 'width=1200,height=800');
}

function openDeviceUI(serialNumber) {
    // Open device's web UI through WebSocket proxy in a new window
    const proxyUrl = `/api/v1/proxy/device/${encodeURIComponent(serialNumber)}/`;
    window.open(proxyUrl, `device-ui-${encodeURIComponent(serialNumber)}`, 'width=1200,height=800');
}

// Open the shared metrics modal for a device
function openDeviceMetrics(serial) {
    if (!serial) return;
    if (typeof window.showMetricsModal === 'function') {
        window.showMetricsModal({ serial });
    } else {
        // Fallback: navigate to devices list or show a toast
        window.__pm_shared.showToast('Metrics UI not available', 'error');
    }
}

// ====== Settings Management ======
async function loadSettings() {
    try {
        // There's no complex settings endpoint yet; reuse config status as a safe probe
        const response = await fetch('/api/config/status');
        if (!response.ok) {
            window.__pm_shared.showToast('Failed to load settings', 'error');
            return;
        }
        
        const settings = await response.json();
        window.__pm_shared.log('Settings loaded:', settings);
        // TODO: Populate settings form

        // Show printer details modal by delegating to the shared renderer.
    } catch (error) {
        window.__pm_shared.error('Failed to load settings:', error);
        window.__pm_shared.showToast('Failed to load settings', 'error');
    }
}

async function saveSettings() {
    try {
        // TODO: Collect settings from form
        const settings = {};
        // Post to /api/settings if available; server provides a minimal handler
        const response = await fetch('/api/settings', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(settings)
        });
        
        if (!response.ok) {
            window.__pm_shared.showToast('Failed to save settings', 'error');
            return;
        }

        window.__pm_shared.showToast('Settings saved successfully', 'success');
    } catch (error) {
        window.__pm_shared.error('Failed to save settings:', error);
    window.__pm_shared.showToast('Failed to save settings', 'error');
    }
}

// ====== Logs Management ======
async function loadLogs() {
    try {
        const response = await fetch('/api/logs');
        if (!response.ok) {
            throw new Error(`HTTP ${response.status}`);
        }
        
        const logs = await response.json();
        renderLogs(logs);
    } catch (error) {
        window.__pm_shared.error('Failed to load logs:', error);
        const logEl = document.getElementById('log');
        if (logEl) {
            logEl.textContent = 'Failed to load logs: ' + error.message;
        }
    }
}

// ====== Metrics ======
async function loadMetrics() {
    try {
        const resp = await fetch('/api/metrics');
        if (!resp.ok) throw new Error('HTTP ' + resp.status);
        const data = await resp.json();

        const statsEl = document.getElementById('metrics_stats');
        const contentEl = document.getElementById('metrics_content');

        if (statsEl) {
            statsEl.innerHTML = `
                <div><strong>Agents:</strong> ${data.agents_count}</div>
                <div><strong>Devices:</strong> ${data.devices_count}</div>
                <div><strong>Devices with recent metrics (24h):</strong> ${data.devices_with_metrics_24h}</div>
            `;
        }

        if (contentEl) {
            // Simple rendering: list latest metrics timestamps for sampled devices
            if (data.recent && Array.isArray(data.recent) && data.recent.length > 0) {
                let html = '<div style="display:flex;flex-direction:column;gap:8px;">';
                data.recent.forEach(r => {
                    const t = r.timestamp ? new Date(r.timestamp).toLocaleString() : 'N/A';
                    html += `<div style="padding:8px;background:rgba(0,0,0,0.03);border-radius:6px;"><strong>${r.serial}</strong> — ${t} — pages: ${r.page_count || 'n/a'}</div>`;
                });
                html += '</div>';
                contentEl.innerHTML = html;
            } else {
                contentEl.innerHTML = '<div style="color:var(--muted);padding:12px">No recent metrics available.</div>';
            }
        }
    } catch (err) {
        window.__pm_shared.error('Failed to load metrics:', err);
        const contentEl = document.getElementById('metrics_content');
        if (contentEl) contentEl.innerHTML = '<div style="color:var(--error);">Failed to load metrics</div>';
    }
}

function renderLogs(logs) {
    const container = document.getElementById('log');
    if (!container) {
        window.__pm_shared.warn('renderLogs: log element not found');
        return;
    }

    if (!logs || (Array.isArray(logs) && logs.length === 0)) {
        container.textContent = 'No logs available';
        return;
    }

    // If server returned an object like { logs: [...] }, normalize
    let lines = logs;
    if (logs && logs.logs && Array.isArray(logs.logs)) lines = logs.logs;

    if (Array.isArray(lines)) {
        container.textContent = lines.join('\n');
    } else if (typeof logs === 'string') {
        container.textContent = logs;
    } else {
        container.textContent = JSON.stringify(logs, null, 2);
    }
}

// ====== Modal Handlers ======
// Agent Details Modal
document.getElementById('agent_details_close_x')?.addEventListener('click', () => {
    document.getElementById('agent_details_modal').style.display = 'none';
});
document.getElementById('agent_details_close')?.addEventListener('click', () => {
    document.getElementById('agent_details_modal').style.display = 'none';
});

// Click outside modal to close
window.addEventListener('click', (event) => {
    const agentModal = document.getElementById('agent_details_modal');
    const confirmModal = document.getElementById('confirm_modal');
    
    if (event.target === agentModal) {
        agentModal.style.display = 'none';
    }
    if (event.target === confirmModal) {
        confirmModal.style.display = 'none';
    }
});

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
        window.__pm_shared.error('toggleAdvancedSettings failed', e);
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

// Wire up printer details modal close buttons and backdrop
(function wirePrinterModal() {
    const detailsOverlay = document.getElementById('printer_details_overlay');
    const modalCloseBtn = document.querySelector('#printer_details_actions button');
    const printerDetailsCloseX = document.getElementById('printer_details_close_x');

    function closePrinterDetailsModal() {
        if (detailsOverlay) {
            detailsOverlay.style.display = 'none';
            document.body.style.overflow = '';
            try { delete detailsOverlay.dataset.currentPrinterIp; } catch (e) {}
        }
    }

    if (modalCloseBtn) modalCloseBtn.addEventListener('click', closePrinterDetailsModal);
    if (printerDetailsCloseX) printerDetailsCloseX.addEventListener('click', closePrinterDetailsModal);
    if (detailsOverlay) {
        detailsOverlay.addEventListener('click', function (e) {
            if (e.target === detailsOverlay) closePrinterDetailsModal();
        });
    }
})();

