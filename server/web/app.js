// PrintMaster Server - Web UI JavaScript

// ====== Initialization ======
document.addEventListener('DOMContentLoaded', function () {
    console.log('PrintMaster Server UI loaded');
    
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
    setInterval(loadServerStatus, 30000); // Every 30 seconds
    
    // Connect to SSE for real-time updates
    connectSSE();
});

// ====== SSE Connection ======
function connectSSE() {
    const eventSource = new EventSource('/api/events');
    
    eventSource.addEventListener('connected', (e) => {
        console.log('SSE connected:', e.data);
    });
    
    eventSource.addEventListener('agent_registered', (e) => {
        try {
            const data = JSON.parse(e.data);
            console.log('Agent registered (SSE):', data);
            // Add card dynamically
            addAgentCard(data);
        } catch (err) {
            console.warn('Failed to parse agent_registered event, falling back to full reload:', err);
            loadAgents();
        }
    });
    
    eventSource.addEventListener('agent_connected', (e) => {
        try {
            const data = JSON.parse(e.data);
            console.log('Agent connected (SSE):', data);
            updateAgentConnection(data.agent_id, 'ws');
        } catch (err) {
            console.warn('Failed to parse agent_connected event, falling back to full reload:', err);
            loadAgents();
        }
    });
    
    eventSource.addEventListener('agent_disconnected', (e) => {
        try {
            const data = JSON.parse(e.data);
            console.log('Agent disconnected (SSE):', data);
            updateAgentConnection(data.agent_id, 'none');
        } catch (err) {
            console.warn('Failed to parse agent_disconnected event, falling back to full reload:', err);
            loadAgents();
        }
    });
    
    eventSource.addEventListener('agent_heartbeat', (e) => {
        try {
            const data = JSON.parse(e.data);
            // Update agent's status/last seen in-place
            updateAgentHeartbeat(data.agent_id, data.status);
        } catch (err) {
            console.log('Agent heartbeat (raw):', e.data);
        }
    });
    
    eventSource.addEventListener('device_updated', (e) => {
            try {
                const data = JSON.parse(e.data);
                console.log('Device updated (SSE):', data);
                // If devices tab is visible, update in-place, otherwise ignore
                const devicesTab = document.querySelector('[data-tab="devices"]');
                if (devicesTab && !devicesTab.classList.contains('hidden')) {
                    addOrUpdateDeviceCard(data);
                }
            } catch (err) {
                console.warn('Failed to parse device_updated event, falling back to full reload:', err);
                const devicesTab = document.querySelector('[data-tab="devices"]');
                if (devicesTab && !devicesTab.classList.contains('hidden')) {
                    loadDevices();
                }
            }
    });
    
    eventSource.onerror = (e) => {
        console.error('SSE connection error:', e);
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
                
                showAlert(message, '⚠️ Configuration Error', true, true);
            } else if (data.using_defaults) {
                // No config file found - show informational modal
                let message = 'No configuration file was found in any of these locations:\n\n';
                data.searched_paths.forEach(path => {
                    message += `• ${path}\n`;
                });
                message += '\nThe server is running with default settings.';
                
                showAlert(message, 'ℹ️ Using Default Configuration', false, true);
            }
        })
        .catch(err => {
            console.error('Failed to check config status:', err);
        });
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
                'settings': 'Settings',
                'logs': 'Logs'
            };
            label.textContent = 'Menu - ' + (tabNames[targetTab] || targetTab);
        }
        
        // Load data for specific tabs
        if (targetTab === 'devices') {
            loadDevices();
        } else if (targetTab === 'settings') {
            loadSettings();
        } else if (targetTab === 'logs') {
            // Previously called connectLogStream() which no longer exists - use loadLogs()
            loadLogs();
        }
    }
}

// ====== Toast Notification System ======
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
        }, 300);
    }, duration);
}

// ====== Confirmation Modal System ======
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

// Alert-style modal (no cancel button, just OK to dismiss)
function showAlert(message, title = 'Notice', isDangerous = false, showDontRemindCheckbox = false) {
    const modal = document.getElementById('confirm_modal');
    const titleEl = document.getElementById('confirm_modal_title');
    const messageEl = document.getElementById('confirm_modal_message');
    const confirmBtn = document.getElementById('confirm_modal_confirm');
    const cancelBtn = document.getElementById('confirm_modal_cancel');
    const closeX = document.getElementById('confirm_modal_close_x');
    
    if (!modal || !titleEl || !messageEl || !confirmBtn) {
        alert(message);
        return;
    }
    
    // Set content
    titleEl.textContent = title;
    messageEl.style.whiteSpace = 'pre-wrap'; // Preserve line breaks
    messageEl.textContent = message;
    
    // Add "Don't remind me" checkbox if requested
    let dontRemindCheckbox = null;
    if (showDontRemindCheckbox) {
        const checkboxHTML = `
            <div style="margin-top: 16px; padding-top: 16px; border-top: 1px solid var(--border);">
                <label style="display: flex; align-items: center; gap: 8px; cursor: pointer;">
                    <input type="checkbox" id="dont_remind_checkbox" style="cursor: pointer;">
                    <span style="font-size: 14px; color: var(--muted);">Don't show this again</span>
                </label>
            </div>
        `;
        messageEl.innerHTML = message.replace(/\n/g, '<br>') + checkboxHTML;
        dontRemindCheckbox = modal.querySelector('#dont_remind_checkbox');
    }
    
    // Style confirm button
    confirmBtn.textContent = 'OK';
    confirmBtn.className = isDangerous ? 
        'modal-button modal-button-danger' : 
        'modal-button modal-button-primary';
    
    // Hide cancel button for alert style
    if (cancelBtn) cancelBtn.style.display = 'none';
    
    // Show modal
    modal.style.display = 'flex';
    
    // Handle dismiss
    const onDismiss = () => {
        // Save preference if checkbox is checked
        if (dontRemindCheckbox && dontRemindCheckbox.checked) {
            localStorage.setItem('hideConfigWarning', 'true');
        }
        cleanup();
    };
    
    // Cleanup function
    const cleanup = () => {
        modal.style.display = 'none';
        if (cancelBtn) cancelBtn.style.display = ''; // Restore for future confirms
        messageEl.style.whiteSpace = ''; // Reset style
        messageEl.innerHTML = ''; // Clear HTML
        confirmBtn.textContent = 'Confirm'; // Reset text
        confirmBtn.removeEventListener('click', onDismiss);
        if (closeX) closeX.removeEventListener('click', onDismiss);
        modal.removeEventListener('click', onBackdropClick);
    };
    
    // Backdrop click closes modal
    const onBackdropClick = (e) => {
        if (e.target === modal) {
            onDismiss();
        }
    };
    
    // Attach event listeners
    confirmBtn.addEventListener('click', onDismiss);
    if (closeX) closeX.addEventListener('click', onDismiss);
    modal.addEventListener('click', onBackdropClick);
}

// ====== Server Status ======
async function loadServerStatus() {
    try {
        const response = await fetch('/api/version');
        if (!response.ok) {
            document.getElementById('server_status').innerHTML = 
                '<span style="color:var(--error);">● Error</span>';
            return;
        }
        
        const data = await response.json();
        document.getElementById('server_status').innerHTML = 
            `<span style="color:var(--success);">● Online</span> v${data.version}`;
    } catch (error) {
        console.error('Failed to load server status:', error);
        document.getElementById('server_status').innerHTML = 
            '<span style="color:var(--error);">● Error loading status</span>';
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
        console.error('Failed to load agents:', error);
        document.getElementById('agents_list').innerHTML = 
            '<div style="color:var(--error);">Failed to load agents: ' + error.message + '</div>';
    }
}

function renderAgents(agents) {
    const container = document.getElementById('agents_list');
    const statsContainer = document.getElementById('agents_stats');
    
    if (!agents || agents.length === 0) {
        container.innerHTML = '<div style="color:var(--muted);">No agents connected yet</div>';
        statsContainer.innerHTML = '<div style="color:var(--muted);">Total Agents: 0</div>';
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
                    <div class="device-card-subtitle copyable" onclick="copyToClipboard('${agent.agent_id}')" title="Click to copy Agent ID">
                        ${agent.agent_id}
                    </div>
                </div>
            </div>
            
            <div class="device-card-info">
                <div class="device-card-row">
                    <span class="device-card-label">Status</span>
                    <span class="device-card-value agent-status-value" style="color:${statusColor}">
                        ${agent.connection_type ? (function(){
                            const label = agent.connection_type === 'ws' ? 'WS' : agent.connection_type === 'http' ? 'HTTP' : 'OFF';
                            const title = `Connection: ${agent.connection_type === 'ws' ? 'WebSocket (live)' : agent.connection_type === 'http' ? 'HTTP (recent)' : 'Offline'}`;
                            return `<span class="conn-badge ${agent.connection_type}" title="${title}" aria-label="Connection status">${label}</span> `;
                        })() : ''}● ${agent.status || 'unknown'}
                    </span>
                </div>
                
                <div class="device-card-row">
                    <span class="device-card-label">IP Address</span>
                    <span class="device-card-value copyable" onclick="copyToClipboard('${agent.ip || ''}')" title="Click to copy">
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
                <button onclick="viewAgentDetails('${agent.agent_id}')">View Details</button>
                <button onclick="openAgentUI('${agent.agent_id}')" ${agent.status !== 'active' ? 'disabled title="Agent not connected via WebSocket"' : ''}>
                    Open UI
                </button>
                <button onclick="deleteAgent('${agent.agent_id}', '${agent.hostname || agent.agent_id}')" 
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
                <button onclick="openDeviceUI('${d.serial}')" ${!d.ip || !d.agent_id ? 'disabled title="Device has no IP or agent"' : ''}>
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
        console.error('Failed to load agent details:', error);
        const body = document.getElementById('agent_details_body');
        body.innerHTML = `<div style="color:var(--error);text-align:center;padding:20px;">Failed to load agent details: ${error.message}</div>`;
        showToast('Failed to load agent details', 'error');
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
                        <span class="device-card-value copyable" onclick="copyToClipboard('${agent.agent_id}')" title="Click to copy">
                            ${agent.agent_id}
                        </span>
                    </div>
                    <div class="device-card-row">
                        <span class="device-card-label">Hostname</span>
                        <span class="device-card-value">${agent.hostname || 'N/A'}</span>
                    </div>
                    <div class="device-card-row">
                        <span class="device-card-label">IP Address</span>
                        <span class="device-card-value copyable" onclick="copyToClipboard('${agent.ip || ''}')" title="Click to copy">
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
                        <span class="device-card-value copyable" onclick="copyToClipboard('${agent.git_commit}')" title="Click to copy">
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
            <button onclick="openAgentUI('${agent.agent_id}')" ${agent.status !== 'active' ? 'disabled title="Agent not connected via WebSocket"' : ''}>
                Open Agent UI
            </button>
        </div>
    `;
}

// ====== Delete Agent ======
async function deleteAgent(agentId, displayName) {
    console.log('deleteAgent called:', agentId, displayName);
    
    const confirmed = await showConfirm(
        `Are you sure you want to delete agent "${displayName}"?\n\nThis will permanently remove the agent and all its associated devices and metrics. This action cannot be undone.`,
        'Delete Agent',
        true // isDangerous
    );
    
    console.log('User confirmed:', confirmed);
    
    if (!confirmed) {
        return;
    }
    
    try {
        console.log('Sending DELETE request to:', `/api/v1/agents/${agentId}`);
        const response = await fetch(`/api/v1/agents/${agentId}`, {
            method: 'DELETE'
        });
        
        console.log('Response status:', response.status, response.statusText);
        
        if (!response.ok) {
            const errorText = await response.text();
            console.error('Delete failed:', errorText);
            throw new Error(`HTTP ${response.status}: ${errorText}`);
        }
        
        const result = await response.json();
        console.log('Delete successful:', result);
        
        showToast(`Agent "${displayName}" deleted successfully`, 'success');
        
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
        console.error('Failed to delete agent:', error);
        showToast(`Failed to delete agent: ${error.message}`, 'error');
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
        console.error('Failed to load devices:', error);
        document.getElementById('devices_cards').innerHTML = 
            '<div style="color:var(--error);">Failed to load devices</div>';
    }
}

function renderDevices(devices) {
    const container = document.getElementById('devices_cards');
    const statsContainer = document.getElementById('devices_stats');
    
    if (!devices || devices.length === 0) {
        container.innerHTML = '<div style="color:var(--muted);">No devices found</div>';
        statsContainer.innerHTML = '<div style="color:var(--muted);">Total Devices: 0</div>';
        return;
    }
    
    // Update stats
    statsContainer.innerHTML = `
        <div><strong>Total Devices:</strong> ${devices.length}</div>
    `;
    
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
                <button onclick="openDeviceUI('${device.serial}')" ${!device.ip || !device.agent_id ? 'disabled title="Device has no IP or agent"' : ''}>
                    Open Web UI
                </button>
            </div>
        </div>
        `;
    }

    container.innerHTML = devices.map(device => renderDeviceCard(device)).join('');
}

// ====== Utility Functions ======
function copyToClipboard(text) {
    if (!text) return;
    
    navigator.clipboard.writeText(text).then(() => {
        showToast('Copied to clipboard', 'success', 1500);
    }).catch(err => {
        console.error('Failed to copy:', err);
        showToast('Failed to copy to clipboard', 'error');
    });
}

// ====== Proxy Functions ======
function openAgentUI(agentId) {
    // Open agent's web UI through WebSocket proxy in a new window
    const proxyUrl = `/api/v1/proxy/agent/${agentId}/`;
    window.open(proxyUrl, `agent-ui-${agentId}`, 'width=1200,height=800');
}

function openDeviceUI(serialNumber) {
    // Open device's web UI through WebSocket proxy in a new window
    const proxyUrl = `/api/v1/proxy/device/${serialNumber}/`;
    window.open(proxyUrl, `device-ui-${serialNumber}`, 'width=1200,height=800');
}

// ====== Settings Management ======
async function loadSettings() {
    try {
        const response = await fetch('/api/settings');
        if (!response.ok) {
            showToast('Failed to load settings', 'error');
            return;
        }
        
        const settings = await response.json();
        console.log('Settings loaded:', settings);
        // TODO: Populate settings form
    } catch (error) {
        console.error('Failed to load settings:', error);
        showToast('Failed to load settings', 'error');
    }
}

async function saveSettings() {
    try {
        // TODO: Collect settings from form
        const settings = {};
        
        const response = await fetch('/api/settings', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(settings)
        });
        
        if (!response.ok) {
            showToast('Failed to save settings', 'error');
            return;
        }
        
        showToast('Settings saved successfully', 'success');
    } catch (error) {
        console.error('Failed to save settings:', error);
        showToast('Failed to save settings', 'error');
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
        console.error('Failed to load logs:', error);
        document.getElementById('logs_content').innerHTML = 
            '<div style="color:var(--error);">Failed to load logs</div>';
    }
}

function renderLogs(logs) {
    const container = document.getElementById('logs_content');
    
    if (!logs || logs.length === 0) {
        container.innerHTML = '<div style="color:var(--muted);">No logs available</div>';
        return;
    }
    
    // TODO: Implement log rendering
    container.innerHTML = '<div style="color:var(--muted);">Logs feature coming soon</div>';
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
