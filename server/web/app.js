// PrintMaster Server - Web UI JavaScript

const RBAC = (typeof window !== 'undefined' && window.__pm_rbac) ? window.__pm_rbac : null;
const ROLE_PRIORITY = RBAC && RBAC.ROLE_PRIORITY ? RBAC.ROLE_PRIORITY : { admin: 3, operator: 2, viewer: 1 };
const BASE_TAB_LABELS = {
    agents: 'Agents',
    devices: 'Devices',
    metrics: 'Metrics',
    logs: 'Logs'
};
const TAB_DEFINITIONS = {
    tenants: {
        label: 'Customers',
        requiredAction: 'tenants.read',
        templateId: 'tab-template-tenants',
        onMount: () => initTenantsUI()
    },
    users: {
        label: 'Users',
        requiredAction: 'users.read',
        templateId: 'tab-template-users',
        onMount: () => initUsersUI()
    },
    settings: {
        label: 'Settings',
        requiredAction: 'settings.read',
        templateId: 'tab-template-settings',
        onMount: () => {
            initSSOAdmin();
        }
    },
    logs: {
        label: 'Logs',
        requiredAction: 'logs.read',
        templateId: 'tab-template-logs',
        onMount: () => {
            initLogSubTabs();
            switchLogView(activeLogView || 'system');
        }
    }
};

let currentUser = null;
const mountedTabs = new Set();
let usersUIInitialized = false;
let tenantsUIInitialized = false;
let tenantModalInitialized = false;
let addAgentUIInitialized = false;
let ssoAdminInitialized = false;
let logSubtabsInitialized = false;
let activeLogView = 'system';
const AUDIT_SEVERITY_VALUES = ['error', 'warn', 'info'];
const AUDIT_AUTO_REFRESH_INTERVAL_MS = 15000;
let auditLogEntries = [];
let auditFilterState = {
    search: '',
    action: '',
    tenant: '',
    severities: new Set(AUDIT_SEVERITY_VALUES),
};
let auditFiltersInitialized = false;
let auditAutoRefreshHandle = null;
let auditLastUpdated = null;
let auditLiveRequested = false;
let auditDataLoaded = false;

function normalizeRole(role) {
    if (RBAC && typeof RBAC.normalizeRole === 'function') {
        return RBAC.normalizeRole(role);
    }
    return (role || '').toString().toLowerCase();
}

function userHasRole(minRole) {
    if (!currentUser) return false;
    if (RBAC && typeof RBAC.userHasRequiredRole === 'function') {
        return RBAC.userHasRequiredRole(currentUser.role, minRole);
    }
    const current = ROLE_PRIORITY[normalizeRole(currentUser.role)] || 0;
    const required = ROLE_PRIORITY[normalizeRole(minRole)] || 0;
    return current >= required;
}

function userCan(action) {
    if (!currentUser || !action) {
        return false;
    }
    if (RBAC && typeof RBAC.canPerformAction === 'function') {
        return RBAC.canPerformAction(currentUser.role, action);
    }
    if (RBAC && RBAC.ACTION_MIN_ROLE && RBAC.ACTION_MIN_ROLE[action]) {
        return userHasRole(RBAC.ACTION_MIN_ROLE[action]);
    }
    return false;
}

function debounce(fn, wait = 250) {
    let timeout;
    return (...args) => {
        clearTimeout(timeout);
        timeout = setTimeout(() => fn.apply(null, args), wait);
    };
}

function buildDynamicTabs() {
    Object.entries(TAB_DEFINITIONS).forEach(([tabId, config]) => {
        const requiredAction = config && config.requiredAction;
        const canShow = requiredAction ? userCan(requiredAction) : userHasRole((config && config.minRole) || 'viewer');
        if (canShow) {
            mountTab(tabId, config);
        }
    });
}

function mountTab(tabId, config) {
    if (mountedTabs.has(tabId)) {
        return;
    }
    createTabButtons(tabId, config.label);
    ensureTabPanel(tabId, config.templateId);
    mountedTabs.add(tabId);
    if (typeof config.onMount === 'function') {
        config.onMount();
    }
}

function createTabButtons(tabId, label) {
    const desktop = document.getElementById('desktop_tabs');
    if (desktop && !desktop.querySelector(`.tab[data-target="${tabId}"]`)) {
        const btn = document.createElement('button');
        btn.className = 'tab';
        btn.dataset.target = tabId;
        btn.textContent = label;
        desktop.appendChild(btn);
        registerTabButton(btn);
    }
    const mobile = document.getElementById('mobile_nav');
    if (mobile && !mobile.querySelector(`.tab[data-target="${tabId}"]`)) {
        const btn = document.createElement('button');
        btn.className = 'tab';
        btn.dataset.target = tabId;
        btn.textContent = label;
        mobile.appendChild(btn);
        registerTabButton(btn);
    }
}

function ensureTabPanel(tabId, templateId) {
    if (document.querySelector(`[data-tab="${tabId}"]`)) {
        return;
    }
    const tpl = document.getElementById(templateId);
    if (!tpl || !tpl.content) {
        return;
    }
    const container = document.querySelector('.content-container');
    if (!container) {
        return;
    }
    container.appendChild(tpl.content.cloneNode(true));
}

function applyRBACVisibility() {
    buildDynamicTabs();
    configureRBACActions();
}

function configureRBACActions() {
    const joinBtn = document.getElementById('join_token_btn');
    if (joinBtn) {
        if (userCan('join_tokens.write')) {
            joinBtn.style.display = '';
            initAddAgentUI();
        } else {
            joinBtn.style.display = 'none';
        }
    }
}

function registerTabButton(tab) {
    if (!tab || tab.dataset.tabRegistered === 'true') {
        return;
    }
    tab.dataset.tabRegistered = 'true';
    tab.addEventListener('click', () => {
        switchTab(tab.dataset.target);
        const mobileNav = document.getElementById('mobile_nav');
        if (mobileNav) {
            mobileNav.classList.remove('active');
        }
    });
}

function getTabLabel(targetTab) {
    if (TAB_DEFINITIONS[targetTab]) {
        return TAB_DEFINITIONS[targetTab].label;
    }
    return BASE_TAB_LABELS[targetTab] || targetTab;
}

// ====== Initialization ======
document.addEventListener('DOMContentLoaded', function () {
    window.__pm_shared.log('PrintMaster Server UI loaded');

    // Before initializing the UI, ensure user is authenticated (shared auth util)
    window.__pm_auth.ensureAuth().then(user => {
        if (!user) {
            // ensureAuthenticated will redirect to login for us
            return;
        }

        currentUser = user;
        applyRBACVisibility();

        // Initialize theme toggle
        initThemeToggle();

        // Initialize tabs (after dynamic tabs injected)
        initTabs();
        initLogSubTabs();

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
        // Update auth-related UI (logout button)
        updateAuthUI();
    }).catch(err=>{
        window.__pm_shared.error('Auth initialization failed', err);
    });
});

// Ensure user is authenticated, show login modal if not. Resolves true once authenticated.
// ensureAuthenticated replaced by shared utility window.__pm_auth.ensureAuth()

function showLoginModal(){
    const modal = document.getElementById('login_modal');
    if(!modal) return;
    modal.style.display = 'flex';
    document.getElementById('login_username').focus();

    const submit = document.getElementById('login_submit');
    const cancel = document.getElementById('login_cancel');
    const errEl = document.getElementById('login_error');

    const doSubmit = async ()=>{
        errEl.style.display='none';
        const u = document.getElementById('login_username').value || '';
        const p = document.getElementById('login_password').value || '';
        try{
            const r = await fetch('/api/v1/auth/login', {method:'POST', headers:{'content-type':'application/json'}, body: JSON.stringify({username:u,password:p})});
            if(!r.ok){
                const text = await r.text();
                errEl.textContent = text || 'Invalid credentials';
                errEl.style.display='block';
                return;
            }
            // success - hide modal and re-init UI
            modal.style.display='none';
            window.location.reload();
        }catch(ex){
            errEl.textContent = ex && ex.message ? ex.message : 'Login failed';
            errEl.style.display='block';
        }
    };

    submit.onclick = doSubmit;
    cancel.onclick = ()=>{ modal.style.display='none'; };
    document.getElementById('login_password').addEventListener('keypress', function(e){ if(e.key==='Enter'){ doSubmit(); } });
}

// Log out the current user and show login modal
async function logout() {
    try {
        const r = await fetch('/api/v1/auth/logout', { method: 'POST' });
        if (!r.ok) {
            // still attempt to clear UI
            window.location = '/login';
            window.__pm_shared.showToast('Logged out (server responded ' + r.status + ')', 'info');
            document.getElementById('logout_btn').style.display = 'none';
            return;
        }
        document.getElementById('logout_btn').style.display = 'none';
        window.location = '/login';
        window.__pm_shared.showToast('Logged out', 'success');
    } catch (err) {
        window.__pm_shared.error('Logout failed', err);
        window.location = '/login';
    }
}

// Show or hide logout button based on current auth state
function updateAuthUI() {
    const btn = document.getElementById('logout_btn');
    if (!btn) return;
    if (currentUser) {
        btn.style.display = 'inline-block';
        btn.onclick = logout;
    } else {
        btn.style.display = 'none';
    }
}

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
            // Show joined bubble when registration event received
            try { setAgentJoined(data.agent_id, true); } catch (ex) {}
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
    const allTabs = document.querySelectorAll('.tabbar .tab');
    const hamburger = document.querySelector('.hamburger-icon');
    const mobileNav = document.getElementById('mobile_nav');
    
    allTabs.forEach(registerTabButton);
    
    // Hamburger menu
    if (hamburger) {
        hamburger.addEventListener('click', () => {
            mobileNav.classList.toggle('active');
        });
    }
}

function initLogSubTabs() {
    if (logSubtabsInitialized) {
        return;
    }
    logSubtabsInitialized = true;

    const subtabButtons = document.querySelectorAll('.log-subtab');
    subtabButtons.forEach(btn => {
        btn.addEventListener('click', () => {
            const target = btn.dataset.logview || 'system';
            switchLogView(target);
        });
    });

    const refreshBtn = document.getElementById('refresh_audit_logs_btn');
    if (refreshBtn) {
        refreshBtn.addEventListener('click', () => loadAuditLogs());
    }

    const timeFilter = document.getElementById('audit_time_filter');
    if (timeFilter) {
        timeFilter.addEventListener('change', () => loadAuditLogs());
    }

    const actorFilter = document.getElementById('audit_actor_filter');
    if (actorFilter) {
        actorFilter.addEventListener('keyup', (ev) => {
            if (ev.key === 'Enter') {
                loadAuditLogs();
            }
        });
    }

    initAuditFilterControls();
}

function switchLogView(view) {
    const canViewAudit = userCan('audit.logs.read');
    const desired = view === 'audit' && canViewAudit ? 'audit' : 'system';
    activeLogView = desired;

    document.querySelectorAll('.log-subtab').forEach(btn => {
        const target = btn.dataset.logview || 'system';
        const isAudit = target === 'audit';
        if (isAudit) {
            btn.style.display = canViewAudit ? '' : 'none';
        }
        if (target === desired) {
            btn.classList.add('active');
        } else {
            btn.classList.remove('active');
        }
    });

    document.querySelectorAll('[data-logview-panel]').forEach(panel => {
        const target = panel.dataset.logviewPanel || 'system';
        if (target === desired) {
            panel.classList.remove('hidden');
        } else {
            panel.classList.add('hidden');
        }
    });

    if (desired === 'audit') {
        loadAuditLogs();
    } else {
        loadLogs();
    }

    syncAuditLiveTimer();
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
        label.textContent = 'Menu - ' + getTabLabel(targetTab);
    }
    
    // Load data for specific tabs
    if (targetTab === 'devices') {
        loadDevices();
    } else if (targetTab === 'metrics') {
        loadMetrics();
    } else if (targetTab === 'settings') {
        initSettingsUI();
        refreshSSOProviders();
    } else if (targetTab === 'logs') {
        initLogSubTabs();
        switchLogView(activeLogView || 'system');
    } else if (targetTab === 'tenants') {
        loadTenants();
    } else if (targetTab === 'users') {
        loadUsers();
    }
}

function logSSOWarning(message, err) {
    if (window.__pm_shared && typeof window.__pm_shared.warn === 'function') {
        window.__pm_shared.warn(message, err);
    } else {
        console.warn(message, err);
    }
}

function invokeSSOMethod(method, ...args) {
    if (!window.__pmSSO || typeof window.__pmSSO[method] !== 'function') {
        return;
    }
    try {
        window.__pmSSO[method](...args);
    } catch (err) {
        logSSOWarning('SSO admin call failed: ' + method, err);
    }
}

function initSSOAdmin() {
    if (ssoAdminInitialized) return;
    ssoAdminInitialized = true;
    invokeSSOMethod('init');
}

function refreshSSOProviders() {
    invokeSSOMethod('loadProviders');
}

function syncSSOTenants(list) {
    invokeSSOMethod('syncTenants', Array.isArray(list) ? list : []);
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

// ====== Tenants / Customers UI ======
function initTenantsUI(){
    if (tenantsUIInitialized) return;
    tenantsUIInitialized = true;
    initTenantModal();
    const btn = document.getElementById('new_tenant_btn');
    if(btn){
        btn.addEventListener('click', ()=> openTenantModal());
    }
    loadTenants();
}

function initTenantModal(){
    const modal = document.getElementById('tenant_modal');
    if (!modal || tenantModalInitialized) return;
    tenantModalInitialized = true;
    const closeBtn = document.getElementById('tenant_modal_close_x');
    const cancelBtn = document.getElementById('tenant_cancel');
    const saveBtn = document.getElementById('tenant_save');
    if (closeBtn) closeBtn.addEventListener('click', closeTenantModal);
    if (cancelBtn) cancelBtn.addEventListener('click', closeTenantModal);
    if (saveBtn) saveBtn.addEventListener('click', submitTenantForm);
    modal.addEventListener('click', (e)=>{
        if (e.target === modal) closeTenantModal();
    });
}

function openTenantModal(tenant){
    const modal = document.getElementById('tenant_modal');
    if (!modal) return;
    if (tenant && tenant.id) {
        modal.setAttribute('data-edit-id', tenant.id);
        document.getElementById('tenant_modal_title').textContent = 'Edit Customer';
        document.getElementById('tenant_save').textContent = 'Save';
    } else {
        modal.removeAttribute('data-edit-id');
        document.getElementById('tenant_modal_title').textContent = 'New Customer';
        document.getElementById('tenant_save').textContent = 'Create';
    }
    const safe = (key) => (tenant && tenant[key]) ? tenant[key] : '';
    document.getElementById('tenant_name').value = safe('name');
    document.getElementById('tenant_business_unit').value = safe('business_unit');
    document.getElementById('tenant_contact_name').value = safe('contact_name');
    document.getElementById('tenant_contact_email').value = safe('contact_email');
    document.getElementById('tenant_contact_phone').value = safe('contact_phone');
    document.getElementById('tenant_billing_code').value = safe('billing_code');
    document.getElementById('tenant_address').value = safe('address');
    document.getElementById('tenant_description').value = safe('description');
    const errEl = document.getElementById('tenant_error');
    if (errEl) errEl.style.display = 'none';
    modal.style.display = 'flex';
    setTimeout(()=>{
        try { document.getElementById('tenant_name').focus(); } catch (e) {}
    }, 10);
}

function closeTenantModal(){
    const modal = document.getElementById('tenant_modal');
    if (!modal) return;
    modal.style.display = 'none';
    modal.removeAttribute('data-edit-id');
    const errEl = document.getElementById('tenant_error');
    if (errEl) errEl.style.display = 'none';
}

function collectTenantFormData(){
    return {
        name: (document.getElementById('tenant_name').value || '').trim(),
        description: (document.getElementById('tenant_description').value || '').trim(),
        business_unit: (document.getElementById('tenant_business_unit').value || '').trim(),
        contact_name: (document.getElementById('tenant_contact_name').value || '').trim(),
        contact_email: (document.getElementById('tenant_contact_email').value || '').trim(),
        contact_phone: (document.getElementById('tenant_contact_phone').value || '').trim(),
        billing_code: (document.getElementById('tenant_billing_code').value || '').trim(),
        address: (document.getElementById('tenant_address').value || '').trim()
    };
}

async function submitTenantForm(){
    const modal = document.getElementById('tenant_modal');
    const errEl = document.getElementById('tenant_error');
    if (errEl) errEl.style.display = 'none';
    const payload = collectTenantFormData();
    if (!payload.name) {
        if (errEl) {
            errEl.textContent = 'Name is required';
            errEl.style.display = 'block';
        }
        return;
    }
    const editId = modal ? modal.getAttribute('data-edit-id') : '';
    try {
        if (editId) {
            await updateTenant(editId, payload);
            window.__pm_shared.showToast('Customer updated', 'success');
        } else {
            await createTenant(payload);
            window.__pm_shared.showToast('Customer created', 'success');
        }
        closeTenantModal();
        loadTenants();
    } catch (err) {
        const message = (err && err.message) ? err.message : 'Failed to save tenant';
        if (errEl) {
            errEl.textContent = message;
            errEl.style.display = 'block';
        }
    }
}

// ====== Users UI ======
function initUsersUI(){
    if (usersUIInitialized) return;
    usersUIInitialized = true;
    const btn = document.getElementById('new_user_btn');
    if(btn){
        btn.addEventListener('click', ()=>{
            openUserModal();
        });
    }
    // Wire modal close/buttons
    const userModal = document.getElementById('user_modal');
    if(userModal){
        document.getElementById('user_modal_close_x').addEventListener('click', ()=> userModal.style.display='none');
        document.getElementById('user_cancel').addEventListener('click', ()=> userModal.style.display='none');
        document.getElementById('user_submit').addEventListener('click', submitCreateUser);
    }

    loadUsers();
}

async function loadUsers(){
    const el = document.getElementById('users_list');
    if(!el) return;
    el.innerHTML = '<div style="color:var(--muted)">Loading users...</div>';
    try{
        const r = await fetch('/api/v1/users');
        if(!r.ok) throw new Error(await r.text());
        const users = await r.json();
        renderUsers(users);
    }catch(err){
        el.innerHTML = '<div style="color:var(--danger)">Error loading users: '+(err.message||err)+'</div>';
    }
}

function renderUsers(list){
    const el = document.getElementById('users_list');
    if(!el) return;
    if(!Array.isArray(list) || list.length===0){
        el.innerHTML = '<div class="muted-text">No users found.</div>';
        return;
    }

    const rows = list.map(u=>{
        const username = escapeHtml(u.username || '—');
        const email = escapeHtml(u.email || '(no email)');
        const role = escapeHtml(u.role || 'user');
        const tenantMarkup = u.tenant_id ? escapeHtml(u.tenant_id) : '<span class="muted-text">(global)</span>';
        const idAttr = escapeHtml(u.id || '');
        const usernameAttr = escapeHtml(u.username || '');
        return `
            <tr>
                <td>
                    <div class="table-primary">${username}</div>
                    <div class="muted-text">${email}</div>
                </td>
                <td>${role}</td>
                <td>${tenantMarkup}</td>
                <td>
                    <div class="table-actions">
                        <button data-action="user-sessions" data-id="${idAttr}" data-username="${usernameAttr}">Sessions</button>
                        <button data-action="edit-user" data-id="${idAttr}">Edit</button>
                        <button data-action="delete-user" data-id="${idAttr}">Delete</button>
                    </div>
                </td>
            </tr>
        `;
    }).join('\n');

    el.innerHTML = `
        <div class="table-wrapper">
            <table class="simple-table">
                <thead>
                    <tr>
                        <th>User</th>
                        <th>Role</th>
                        <th>Tenant</th>
                        <th style="width:1%">Actions</th>
                    </tr>
                </thead>
                <tbody>
                    ${rows}
                </tbody>
            </table>
        </div>
    `;

    // Attach handlers for edit/delete
    el.querySelectorAll('button[data-action="edit-user"]').forEach(b=>{
        b.addEventListener('click', async ()=>{
            const id = b.getAttribute('data-id');
            openUserEditModal(id);
        });
    });
    el.querySelectorAll('button[data-action="delete-user"]').forEach(b=>{
        b.addEventListener('click', async ()=>{
            const id = b.getAttribute('data-id');
            if(!confirm('Delete user ID '+id+'? This cannot be undone.')) return;
            try{
                const r = await fetch('/api/v1/users/'+encodeURIComponent(id), { method: 'DELETE' });
                if(!r.ok) throw new Error(await r.text());
                window.__pm_shared.showToast('User deleted', 'success');
                loadUsers();
            }catch(err){
                window.__pm_shared.showAlert('Failed to delete user: '+(err.message||err), 'Error', true, false);
            }
        });
    });
    el.querySelectorAll('button[data-action="user-sessions"]').forEach(b=>{
        b.addEventListener('click', async ()=>{
            const id = b.getAttribute('data-id');
            const username = b.getAttribute('data-username') || '';
            await loadUserSessions(id, username);
        });
    });
}

async function loadUserSessions(userId, username){
    try{
        const r = await fetch('/api/v1/sessions?user_id='+encodeURIComponent(userId));
        if(!r.ok) throw new Error(await r.text());
        const sessions = await r.json();
        showSessionsModal(sessions, username);
    }catch(err){
        window.__pm_shared.showAlert('Failed to load sessions: '+(err.message||err), 'Error', true, false);
    }
}

function showSessionsModal(sessions, username){
    // create simple modal
    const modal = document.createElement('div');
    modal.style.position = 'fixed'; modal.style.left='0'; modal.style.top='0'; modal.style.right='0'; modal.style.bottom='0';
    modal.style.background = 'rgba(0,0,0,0.5)'; modal.style.display='flex'; modal.style.alignItems='center'; modal.style.justifyContent='center';
    modal.style.zIndex = 9999;
    const box = document.createElement('div');
    box.style.background = 'var(--bg)'; box.style.color='var(--fg)'; box.style.padding='16px'; box.style.borderRadius='6px'; box.style.width='600px'; box.style.maxHeight='80vh'; box.style.overflow='auto';
    const title = document.createElement('div'); title.style.display='flex'; title.style.justifyContent='space-between'; title.style.alignItems='center';
    const h = document.createElement('div'); h.style.fontWeight='600'; h.textContent = 'Sessions for '+(username||'user');
    const close = document.createElement('button'); close.textContent='Close'; close.addEventListener('click', ()=> document.body.removeChild(modal));
    title.appendChild(h); title.appendChild(close);
    box.appendChild(title);
    const list = document.createElement('div'); list.style.marginTop='12px';
    if(!Array.isArray(sessions) || sessions.length===0){
        list.innerHTML = '<div style="color:var(--muted)">No sessions found.</div>';
    } else {
        const rows = sessions.map(s => {
            const created = s.created_at ? new Date(s.created_at).toLocaleString() : '';
            const expires = s.expires_at ? new Date(s.expires_at).toLocaleString() : '';
            const user = s.username || '';
            return `<div style="display:flex;justify-content:space-between;align-items:center;padding:8px 0;border-bottom:1px solid var(--muted)">
                <div style="font-size:13px">
                    <div><strong>${escapeHtml(user)}</strong></div>
                    <div style="color:var(--muted);font-size:12px">Created: ${escapeHtml(created)} &nbsp; Expires: ${escapeHtml(expires)}</div>
                </div>
                <div>
                    <button data-action="revoke-session" data-key="${escapeHtml(s.token || '')}">Revoke</button>
                </div>
            </div>`;
        }).join('\n');
        list.innerHTML = rows;
    }
    box.appendChild(list);
    modal.appendChild(box);
    document.body.appendChild(modal);

    // Attach revoke handlers
    modal.querySelectorAll('button[data-action="revoke-session"]').forEach(b=>{
        b.addEventListener('click', async ()=>{
            const key = b.getAttribute('data-key');
            if(!confirm('Revoke session?')) return;
            try{
                const r = await fetch('/api/v1/sessions/'+encodeURIComponent(key), { method: 'DELETE' });
                if(!r.ok) throw new Error(await r.text());
                window.__pm_shared.showToast('Session revoked', 'success');
                // refresh list
                const id = sessions.length>0 ? sessions[0].user_id : null;
                if(id) await loadUserSessions(id, username);
            }catch(err){
                window.__pm_shared.showAlert('Failed to revoke session: '+(err.message||err), 'Error', true, false);
            }
        });
    });
}

function openUserModal(){
    const modal = document.getElementById('user_modal');
    if(!modal) return;
    // populate tenant select from cached tenants
    const sel = document.getElementById('user_tenant');
    if(sel){
        sel.innerHTML = '<option value="">(Global / Server)</option>';
        if(window._tenants && Array.isArray(window._tenants)){
            window._tenants.forEach(t=>{
                const opt = document.createElement('option');
                opt.value = t.id || t.uuid || '';
                opt.textContent = t.name || opt.value;
                sel.appendChild(opt);
            });
        }
    }
    // reset fields
    document.getElementById('user_username').value = '';
    document.getElementById('user_email').value = '';
    document.getElementById('user_password').value = '';
    document.getElementById('user_role').value = 'user';
    document.getElementById('user_error').style.display='none';
    modal.style.display = 'flex';
    document.getElementById('user_username').focus();
}

async function submitCreateUser(){
    const modal = document.getElementById('user_modal');
    const username = document.getElementById('user_username').value || '';
    const email = document.getElementById('user_email').value || '';
    const password = document.getElementById('user_password').value || '';
    const role = document.getElementById('user_role').value || 'user';
    const tenant = document.getElementById('user_tenant').value || '';
    const errEl = document.getElementById('user_error');
    errEl.style.display='none';
    if(!username || !password){
        errEl.textContent = 'Username and password required'; errEl.style.display='block'; return;
    }
    try{
        const payload = { username, password, role };
        if(email) payload.email = email;
        if(tenant) payload.tenant_id = tenant;
        const editId = modal.getAttribute('data-edit-id');
        let r;
        if(editId) {
            // update existing
            r = await fetch('/api/v1/users/'+encodeURIComponent(editId), { method: 'PUT', headers: {'content-type':'application/json'}, body: JSON.stringify(payload) });
        } else {
            r = await fetch('/api/v1/users', { method: 'POST', headers: {'content-type':'application/json'}, body: JSON.stringify(payload) });
        }
        if(!r.ok){
            const txt = await r.text();
            throw new Error(txt || 'Request failed');
        }
        const created = await r.json();
        modal.style.display='none';
        modal.removeAttribute('data-edit-id');
        document.getElementById('user_submit').textContent = 'Create';
        window.__pm_shared.showToast(editId ? 'User updated' : 'User created', 'success');
        loadUsers();
    }catch(err){
        errEl.textContent = (err && err.message) ? err.message : 'Failed to create user'; errEl.style.display='block';
    }
}

// Open modal for editing an existing user
async function openUserEditModal(id){
    try{
        const r = await fetch('/api/v1/users/'+encodeURIComponent(id));
        if(!r.ok) throw new Error(await r.text());
        const u = await r.json();
        const modal = document.getElementById('user_modal');
        document.getElementById('user_username').value = u.username || '';
        document.getElementById('user_email').value = u.email || '';
        document.getElementById('user_password').value = '';
        document.getElementById('user_role').value = u.role || 'user';
        document.getElementById('user_tenant').value = u.tenant_id || '';
        // store editing id on modal
        modal.setAttribute('data-edit-id', id);
        document.getElementById('user_submit').textContent = 'Save';
        modal.style.display = 'flex';
    }catch(err){
        window.__pm_shared.showAlert('Failed to load user: '+(err.message||err), 'Error', true, false);
    }
}

async function loadTenants(){
    const el = document.getElementById('tenants_list');
    if(!el) return;
    el.innerHTML = '<div style="color:var(--muted)">Loading tenants...</div>';
    try{
        const r = await fetch('/api/v1/tenants');
        if(!r.ok) throw new Error(await r.text());
        const data = await r.json();
        // Cache tenants for use in other UI flows (e.g. add-agent modal)
        window._tenants = data;
        syncSSOTenants(data);
        renderTenants(data);
        notifyManagedSettingsTenantDirectory(data);
    }catch(err){
        el.innerHTML = '<div style="color:var(--danger)">Error loading tenants: '+(err.message||err)+'</div>';
    }
}

function renderTenants(list){
    const el = document.getElementById('tenants_list');
    if(!el) return;
    if(!Array.isArray(list) || list.length===0){
        el.innerHTML = '<div class="muted-text">No tenants yet. Click New Tenant to add one.</div>';
        return;
    }
    const rows = list.map(t => {
        const rawId = t.id || t.uuid || '';
        const idAttr = escapeHtml(rawId);
        const idDisplay = rawId ? idAttr : '<span class="muted-text">(none)</span>';
        const businessLines = [
            `<div class="table-primary">${escapeHtml(t.name || '—')}</div>`,
            t.business_unit ? `<div class="muted-text">${escapeHtml(t.business_unit)}</div>` : '',
            t.description ? `<div class="muted-text">${escapeHtml(t.description)}</div>` : ''
        ].join('');
        const contactEmail = t.contact_email ? `<a href="mailto:${encodeURIComponent(t.contact_email)}">${escapeHtml(t.contact_email)}</a>` : '';
        const contactLines = [
            t.contact_name ? `<div>${escapeHtml(t.contact_name)}</div>` : '',
            contactEmail ? `<div>${contactEmail}</div>` : '',
            t.contact_phone ? `<div class="muted-text">${escapeHtml(t.contact_phone)}</div>` : ''
        ].join('');
        const metaLines = [
            `<div>Tenant ID: ${idDisplay}</div>`,
            t.billing_code ? `<div class="muted-text">Billing: ${escapeHtml(t.billing_code)}</div>` : '',
            t.address ? `<div class="muted-text" style="white-space:pre-line;">${escapeHtml(t.address)}</div>` : '',
            t.created_at ? `<div class="muted-text">Created ${escapeHtml(formatDateTime(t.created_at))}</div>` : ''
        ].join('');
        return `
            <tr>
                <td>${businessLines}</td>
                <td>${contactLines || '<span class="muted-text">No contact info</span>'}</td>
                <td>${metaLines}</td>
                <td>
                    <div class="table-actions">
                        <button data-action="create-token" data-tenant="${idAttr}">Create Token</button>
                        <button data-action="view-tokens" data-tenant="${idAttr}">Tokens</button>
                        <button data-action="edit-tenant" data-tenant="${idAttr}">Edit</button>
                    </div>
                </td>
            </tr>
        `;
    }).join('\n');

    el.innerHTML = `
        <div class="table-wrapper">
            <table class="simple-table">
                <thead>
                    <tr>
                        <th>Customer</th>
                        <th>Contact</th>
                        <th>Details</th>
                        <th style="width:1%">Actions</th>
                    </tr>
                </thead>
                <tbody>
                    ${rows}
                </tbody>
            </table>
        </div>
    `;

    el.querySelectorAll('button[data-action="create-token"]').forEach(b=>{
        b.addEventListener('click', async ()=>{
            const tenant = b.getAttribute('data-tenant');
            await handleCreateToken(tenant);
        });
    });
    el.querySelectorAll('button[data-action="view-tokens"]').forEach(b=>{
        b.addEventListener('click', async ()=>{
            const tenant = b.getAttribute('data-tenant');
            await showTokensList(tenant);
        });
    });
    el.querySelectorAll('button[data-action="edit-tenant"]').forEach(b=>{
        b.addEventListener('click', ()=>{
            const tenantId = b.getAttribute('data-tenant') || '';
            const tenant = (window._tenants || []).find(t => (t.id || t.uuid || '') === tenantId);
            openTenantModal(tenant || null);
        });
    });
}

async function createTenant(body){
    const r = await fetch('/api/v1/tenants', {method:'POST', headers:{'content-type':'application/json'}, body: JSON.stringify(body)});
    if(!r.ok) throw new Error(await r.text());
    return r.json();
}

async function updateTenant(id, body){
    const r = await fetch('/api/v1/tenants/'+encodeURIComponent(id), {method:'PUT', headers:{'content-type':'application/json'}, body: JSON.stringify(body)});
    if(!r.ok) throw new Error(await r.text());
    return r.json();
}

async function handleCreateToken(tenantID){
    // Open the unified Add Agent modal and preselect the tenant
    try {
        openAddAgentModal({ tenantID });
    } catch (err) {
        window.__pm_shared.showAlert('Failed to open Add Agent modal: ' + (err && err.message ? err.message : err), 'Error', true, false);
    }
}

async function showTokensList(tenantID){
    try{
        const r = await fetch('/api/v1/join-tokens?tenant_id='+encodeURIComponent(tenantID));
        if(!r.ok) throw new Error(await r.text());
        const tokens = await r.json();
        renderTokenModal(tenantID, tokens);
    }catch(err){
        window.__pm_shared.showAlert('Failed to load tokens: '+(err.message||err), 'Error', true, false);
    }
}

function renderTokenModal(tenantID, tokens){
    if(!Array.isArray(tokens) || tokens.length===0){
        window.__pm_shared.showAlert('No tokens for tenant: ' + escapeHtml(tenantID), 'Tokens', false, false);
        return;
    }
    let html = '<div style="font-family:monospace; white-space:pre-wrap;">';
    tokens.forEach(t => {
        html += `ID: ${t.id} | one_time: ${t.one_time} | revoked: ${t.revoked?1:0} | used_at: ${t.used_at||'-'} | expires: ${t.expires_at||'-'}\n`;
    });
    html += '</div>';
    // Ask user if they want to revoke a token via input modal
    window.__pm_shared.showAlert(html, 'Tokens for tenant: ' + escapeHtml(tenantID), false, false);
    showInputModal('Revoke token', 'Enter the token ID to revoke (leave empty to cancel)', '').then(id => {
        if (!id) return;
        revokeToken(id.trim()).then(()=>{
            window.__pm_shared.showToast('Revoked ' + id.trim(), 'success');
            showTokensList(tenantID);
        }).catch(err=>{
            window.__pm_shared.showAlert('Failed to revoke: '+(err.message||err), 'Error', true, false);
        });
    }).catch(()=>{});
}

async function revokeToken(id){
    const r = await fetch('/api/v1/join-token/revoke', {method:'POST', headers:{'content-type':'application/json'}, body: JSON.stringify({id})});
    if(!r.ok) throw new Error(await r.text());
    return r.json();
}

function escapeHtml(s){
    if(!s) return '';
    return String(s).replace(/[&<>"']/g, function(m){ return ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;','\'':"&#39;"})[m]; });
}

function formatDateTime(value){
    if(!value) return '—';
    const d = new Date(value);
    if(isNaN(d.getTime())) return '—';
    return d.toLocaleString();
}

function formatRelativeTime(value){
    if(!value) return '—';
    const d = new Date(value);
    if(isNaN(d.getTime())) return '—';
    const diffMs = Date.now() - d.getTime();
    if(diffMs < 0) return 'just now';
    const minutes = Math.floor(diffMs / 60000);
    if(minutes < 1) return 'just now';
    if(minutes < 60) return minutes + 'm ago';
    const hours = Math.floor(minutes / 60);
    if(hours < 24) return hours + 'h ago';
    const days = Math.floor(hours / 24);
    return days + 'd ago';
}

function formatNumber(value){
    if(typeof value === 'number' && isFinite(value)){
        return value.toLocaleString();
    }
    if(typeof value === 'string' && value.trim() !== ''){
        return value;
    }
    return '—';
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
                        <div class="device-card-title">${agent.name || agent.hostname || agent.agent_id}</div>
                        <span class="agent-joined-bubble" style="margin-left:8px;display:${registeredDate? 'inline-flex':'none'};align-items:center;padding:2px 6px;border-radius:12px;background:var(--panel);font-size:12px;color:var(--muted);border:1px solid var(--border);">${registeredDate? 'Joined' : ''}</span>
                    </div>
                    <div class="device-card-subtitle">
                        <span class="copyable" data-copy="${agent.hostname || ''}" title="Click to copy Hostname">${agent.hostname || 'N/A'}</span>
                        <span style="margin-left:8px;color:var(--muted);font-size:12px;" class="copyable" data-copy="${agent.agent_id}" title="Click to copy Agent ID">${agent.agent_id}</span>
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
                                title = 'Live';
                                cls = 'ws';
                            } else if (agent.connection_type === 'http') {
                                label = 'HTTP(s) Fallback';
                                title = 'HTTP(s) Fallback';
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
                <button data-action="delete-agent" data-agent-id="${agent.agent_id}" data-agent-name="${agent.name || agent.hostname || agent.agent_id}" 
                    style="background: var(--btn-delete-bg); color: var(--btn-delete-text); border: 1px solid var(--btn-delete-border);">
                    Delete
                </button>
            </div>
        </div>
    `;
}

// Toggle the small 'Joined' bubble on an agent card
function setAgentJoined(agentId, joined) {
    const cards = findAgentsContainer();
    if (!cards) return;
    const card = cards.querySelector(`[data-agent-id="${agentId}"]`);
    if (!card) return;
    const bubble = card.querySelector('.agent-joined-bubble');
    if (!bubble) return;
    if (joined) {
        bubble.style.display = 'inline-flex';
        bubble.textContent = 'Joined';
    } else {
        bubble.style.display = 'none';
        bubble.textContent = '';
    }
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
    const cardHtml = renderServerDeviceCard(device);
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
    
    title.textContent = `Agent: ${agent.name || agent.hostname || agent.agent_id}`;
    
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
                        <span class="device-card-label">Name</span>
                        <span class="device-card-value" id="agent_details_name_display">${agent.name || ''}</span>
                        <span style="margin-left:8px;"><button id="agent_details_edit_name_btn">Edit</button></span>
                    </div>
                    <div class="device-card-row">
                        <span class="device-card-label">Hostname</span>
                        <span class="device-card-value copyable" data-copy="${agent.hostname || ''}" title="Click to copy">
                            ${agent.hostname || 'N/A'}
                        </span>
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
    // Attach inline editor handlers now that DOM nodes are present
    try { _attachAgentDetailsNameEditor(agent); } catch (e) { window.__pm_shared && window.__pm_shared.warn && window.__pm_shared.warn('attach editor failed', e); }
}

// After rendering the agent details modal we attach a small inline handler
// to allow editing the agent's user-friendly name. This toggles an input
// in the modal and sends a POST to update the name on the server, then
// updates the UI card in-place.
function _attachAgentDetailsNameEditor(agent) {
    try {
        const editBtn = document.getElementById('agent_details_edit_name_btn');
        if (!editBtn) return;
        editBtn.addEventListener('click', () => {
            const displayEl = document.getElementById('agent_details_name_display');
            if (!displayEl) return;
            const current = displayEl.textContent || '';
            displayEl.innerHTML = ` <input id="agent_details_name_input" value="${(agent.name||'').replace(/"/g,'&quot;')}" style="width:70%" /> <button id="agent_details_save_name">Save</button> <button id="agent_details_cancel_name">Cancel</button>`;

            const saveBtn = document.getElementById('agent_details_save_name');
            const cancelBtn = document.getElementById('agent_details_cancel_name');
            if (cancelBtn) cancelBtn.addEventListener('click', () => { displayEl.textContent = current; });

            if (saveBtn) saveBtn.addEventListener('click', async () => {
                const input = document.getElementById('agent_details_name_input');
                if (!input) return;
                const newName = input.value.trim();
                try {
                    const res = await fetch(`/api/v1/agents/${encodeURIComponent(agent.agent_id)}`, {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ name: newName })
                    });
                    if (!res.ok) throw new Error('HTTP ' + res.status);
                    const updated = await res.json();
                    // Update modal display
                    const title = document.getElementById('agent_details_title');
                    const nameDisplay = document.getElementById('agent_details_name_display');
                    if (nameDisplay) nameDisplay.textContent = updated.name || '';
                    if (title) title.textContent = `Agent: ${updated.name || updated.hostname || updated.agent_id}`;
                    // Update agent card in list
                    try { addAgentCard(updated); } catch (e) {}
                    window.__pm_shared.showToast('Agent name updated', 'success');
                } catch (err) {
                    window.__pm_shared.showToast('Failed to update agent name', 'error');
                    // restore display
                    displayEl.textContent = current;
                }
            });
        });
    } catch (e) {
        window.__pm_shared.warn('Failed to attach agent details name editor', e);
    }
}

// Expose server-specific agent UI helpers to the shared namespace so the
// delegated card handlers (loaded earlier) call the rich renderer instead
// of the generic fallback in `common/web/shared.js` which shows raw JSON.
try {
    window.__pm_shared = window.__pm_shared || {};
    window.__pm_shared.viewAgentDetails = viewAgentDetails;
    window.__pm_shared.renderAgentDetailsModal = renderAgentDetailsModal;
    // Also expose delete/open helpers if present so shared callers use server implementations
    window.__pm_shared.deleteAgent = window.__pm_shared.deleteAgent || deleteAgent;
    window.__pm_shared.openAgentUI = window.__pm_shared.openAgentUI || openAgentUI;
    // Always override device helpers so cards and shared UI use the server proxy endpoint
    window.__pm_shared.openDeviceUI = openDeviceUI;
    window.__pm_shared.openDeviceMetrics = window.__pm_shared.openDeviceMetrics || openDeviceMetrics;
} catch (e) { console.warn('Failed to expose server UI helpers to shared namespace', e); }

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

function renderServerDeviceCard(device) {
    const serial = device.serial || '';
    const agentId = device.agent_id || '';
    const hasIp = !!device.ip;
    const hasAgent = !!agentId;
    const hasSerial = !!serial;

    return `
    <div class="device-card" data-serial="${serial}" data-agent-id="${agentId}">
        <div class="device-card-header">
            <div>
                <div class="device-card-title">${device.manufacturer || 'Unknown'} ${device.model || ''}</div>
                <div class="device-card-subtitle">${device.ip || 'N/A'}</div>
            </div>
        </div>
        <div class="device-card-info">
            <div class="device-card-row">
                <span class="device-card-label">Serial</span>
                <span class="device-card-value device-serial">${serial || 'N/A'}</span>
            </div>
            <div class="device-card-row">
                <span class="device-card-label">Agent</span>
                <span class="device-card-value device-agent-id">${agentId || 'N/A'}</span>
            </div>
        </div>
        <div class="device-card-actions">
            <button data-action="open-device" data-serial="${serial}" data-agent-id="${agentId}" ${!hasIp || !hasAgent ? 'disabled title="Device has no IP or agent"' : ''}>
                Open Web UI
            </button>
            <button data-action="view-metrics" data-serial="${serial}" ${!hasSerial ? 'disabled title="No serial"' : ''}>
                View Metrics
            </button>
            <button data-action="show-printer-details" data-ip="${device.ip||''}" data-serial="${serial}" data-source="saved" ${!hasIp ? 'disabled title="No IP"' : ''}>
                Details
            </button>
        </div>
    </div>
    `;
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
    
    container.innerHTML = devices.map(device => renderServerDeviceCard(device)).join('');
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

// ====== Managed Settings UI ======
const SETTINGS_SECTION_LABELS = {
    discovery: 'Discovery',
    developer: 'Developer',
    security: 'Security'
};
const SETTINGS_SECTION_ORDER = ['discovery', 'developer', 'security'];

const settingsUIState = {
    initialized: false,
    loading: false,
    loadingPromise: null,
    scope: 'global',
    schema: null,
    groupedFields: {},
    globalSnapshot: null,
    globalDraft: null,
    globalDirty: false,
    tenantList: [],
    selectedTenantId: '',
    tenantSnapshot: null,
    tenantDraft: null,
    tenantOverridesDraft: {},
    tenantDirty: false,
    saving: false,
    eventsBound: false
};

function resolveTenantId(record) {
    if (!record) return '';
    return record.id || record.uuid || record.tenant_id || '';
}

function normalizeTenantList(list) {
    if (!Array.isArray(list)) return [];
    const normalized = [];
    list.forEach(item => {
        const id = resolveTenantId(item);
        if (!id) {
            return;
        }
        normalized.push({ ...item, id });
    });
    return normalized;
}

function updateSettingsTenantDirectory(rawList) {
    const normalized = normalizeTenantList(rawList);
    const previousSelection = settingsUIState.selectedTenantId;
    settingsUIState.tenantList = normalized;
    const selectionStillValid = previousSelection && normalized.some(t => t.id === previousSelection);
    if (!selectionStillValid) {
        settingsUIState.selectedTenantId = normalized.length ? normalized[0].id : '';
    }
    if (!settingsUIState.selectedTenantId) {
        settingsUIState.tenantSnapshot = null;
        settingsUIState.tenantDraft = null;
        settingsUIState.tenantOverridesDraft = {};
        settingsUIState.tenantDirty = false;
    }
    if (!settingsUIState.initialized) {
        return;
    }
    if (settingsUIState.scope === 'tenant' && !selectionStillValid && settingsUIState.selectedTenantId) {
        loadTenantSnapshot(settingsUIState.selectedTenantId)
            .then(() => renderSettingsUI())
            .catch(err => reportSettingsError('Failed to refresh tenant overrides', err));
        return;
    }
    renderSettingsUI();
}

function notifyManagedSettingsTenantDirectory(list) {
    updateSettingsTenantDirectory(list);
}

async function initSettingsUI() {
    const panel = document.getElementById('managed_settings_panel');
    if (!panel) return;
    if (settingsUIState.loading) {
        return settingsUIState.loadingPromise;
    }
    if (settingsUIState.initialized) {
        renderSettingsUI();
        return;
    }
    settingsUIState.loading = true;
    settingsUIState.loadingPromise = (async () => {
        try {
            await bootstrapSettingsUI();
            settingsUIState.initialized = true;
            renderSettingsUI();
        } catch (err) {
            renderSettingsError(err);
        } finally {
            settingsUIState.loading = false;
        }
    })();
    return settingsUIState.loadingPromise;
}

async function bootstrapSettingsUI() {
    await loadSettingsSchema();
    await loadGlobalSettingsSnapshot();
    await loadTenantDirectory();
    if (settingsUIState.tenantList.length > 0) {
        settingsUIState.selectedTenantId = settingsUIState.tenantList[0].id;
        if (settingsUIState.selectedTenantId) {
            await loadTenantSnapshot(settingsUIState.selectedTenantId);
        }
    }
}

async function loadSettingsSchema() {
    try {
        const schema = await fetchJSON('/api/v1/settings/schema');
        settingsUIState.schema = schema;
        settingsUIState.groupedFields = groupSchemaFields(schema && Array.isArray(schema.fields) ? schema.fields : []);
    } catch (err) {
        if (err && err.status === 404) {
            throw new Error('Managed settings are disabled on this server build. Enable tenancy/features to use this tab.');
        }
        throw err;
    }
}

function groupSchemaFields(fields) {
    const groups = {};
    fields.forEach(field => {
        if (!field || !field.path) return;
        const scope = (field.scope || '').toLowerCase();
        if (scope === 'agent') {
            return;
        }
        const section = field.path.split('.')[0];
        if (!groups[section]) {
            groups[section] = [];
        }
        groups[section].push(field);
    });
    return groups;
}

function orderedSettingsSections() {
    const sections = [];
    const seen = new Set();
    SETTINGS_SECTION_ORDER.forEach(sectionKey => {
        const group = settingsUIState.groupedFields[sectionKey];
        if (group && group.length) {
            sections.push(sectionKey);
            seen.add(sectionKey);
        }
    });
    Object.keys(settingsUIState.groupedFields).sort().forEach(sectionKey => {
        const group = settingsUIState.groupedFields[sectionKey];
        if (!seen.has(sectionKey) && group && group.length) {
            sections.push(sectionKey);
        }
    });
    return sections;
}

async function loadGlobalSettingsSnapshot() {
    const snapshot = await fetchJSON('/api/v1/settings/global');
    settingsUIState.globalSnapshot = snapshot;
    settingsUIState.globalDraft = cloneSettings(snapshot ? snapshot.Settings : {});
    settingsUIState.globalDirty = false;
}

async function loadTenantDirectory() {
    try {
        const tenants = await fetchJSON('/api/v1/tenants');
        updateSettingsTenantDirectory(tenants);
    } catch (err) {
        if (err && (err.status === 403 || err.status === 404)) {
            settingsUIState.tenantList = [];
            return;
        }
        throw err;
    }
}

async function loadTenantSnapshot(tenantId) {
    if (!tenantId) {
        settingsUIState.tenantSnapshot = null;
        settingsUIState.tenantDraft = null;
        settingsUIState.tenantOverridesDraft = {};
        settingsUIState.tenantDirty = false;
        return;
    }
    const snapshot = await fetchJSON(`/api/v1/settings/tenants/${encodeURIComponent(tenantId)}`);
    settingsUIState.tenantSnapshot = snapshot;
    settingsUIState.tenantDraft = cloneSettings(snapshot ? snapshot.Settings : settingsUIState.globalSnapshot.Settings);
    settingsUIState.tenantOverridesDraft = cloneSettings(snapshot && snapshot.Overrides ? snapshot.Overrides : {});
    settingsUIState.tenantDirty = false;
}

function renderSettingsUI() {
    bindSettingsEvents();
    renderScopeButtons();
    updateTenantSelect();
    renderSettingsForm();
    renderOverrideSummary();
    updateActionButtons();
    updateLastUpdatedMeta();
}

function renderSettingsError(err) {
    const root = document.getElementById('settings_form_root');
    if (root) {
        let message = 'Managed settings are unavailable.';
        if (err) {
            if (err.status === 403) {
                message = 'You do not have permission to view managed settings.';
            } else if (err.status === 404) {
                message = 'Managed settings are disabled on this server build.';
            } else if (err.message) {
                message = err.message;
            }
        }
        root.innerHTML = `<div class="error-text">${escapeHtml(message)}</div>`;
        window.__pm_shared.showToast(message, 'error', 5000);
    }
    const actions = document.querySelector('.settings-actions');
    if (actions) {
        actions.style.display = 'none';
    }
}

function bindSettingsEvents() {
    if (settingsUIState.eventsBound) return;
    const formRoot = document.getElementById('settings_form_root');
    if (formRoot) {
        formRoot.addEventListener('input', handleSettingsFieldChange);
        formRoot.addEventListener('change', handleSettingsFieldChange);
        formRoot.addEventListener('click', handleSettingsFieldClick);
    }
    const saveBtn = document.getElementById('settings_save_btn');
    if (saveBtn) saveBtn.addEventListener('click', handleSettingsSave);
    const discardBtn = document.getElementById('settings_discard_btn');
    if (discardBtn) discardBtn.addEventListener('click', handleDiscardChanges);
    const resetBtn = document.getElementById('settings_reset_overrides_btn');
    if (resetBtn) resetBtn.addEventListener('click', resetTenantOverrides);
    document.querySelectorAll('.settings-scope-btn').forEach(btn => {
        btn.addEventListener('click', () => handleSettingsScopeChange(btn.dataset.scope));
    });
    const tenantSelect = document.getElementById('settings_tenant_select');
    if (tenantSelect) tenantSelect.addEventListener('change', handleTenantSelect);
    settingsUIState.eventsBound = true;
}

function renderScopeButtons() {
    document.querySelectorAll('.settings-scope-btn').forEach(btn => {
        const scope = btn.dataset.scope || 'global';
        btn.classList.toggle('active', scope === settingsUIState.scope);
    });
}

function updateTenantSelect() {
    const select = document.getElementById('settings_tenant_select');
    if (!select) return;
    select.innerHTML = '';
    if (!settingsUIState.tenantList.length) {
        const opt = document.createElement('option');
        opt.value = '';
        opt.textContent = 'No tenants available';
        select.appendChild(opt);
        select.disabled = true;
        return;
    }
    select.disabled = false;
    settingsUIState.tenantList.forEach(tenant => {
        const opt = document.createElement('option');
        const tenantId = resolveTenantId(tenant);
        opt.value = tenantId;
        opt.textContent = tenant.name || tenantId;
        if (tenantId === settingsUIState.selectedTenantId) {
            opt.selected = true;
        }
        select.appendChild(opt);
    });
}

function renderSettingsForm() {
    const root = document.getElementById('settings_form_root');
    if (!root) return;
    if (!settingsUIState.schema || !settingsUIState.globalDraft) {
        root.innerHTML = '<div class="muted-text">Managed settings are initializing…</div>';
        return;
    }
    const scope = settingsUIState.scope;
    const draft = scope === 'global'
        ? settingsUIState.globalDraft
        : (settingsUIState.tenantDraft || settingsUIState.globalDraft);
    root.innerHTML = '';
    orderedSettingsSections().forEach(sectionKey => {
        const fields = settingsUIState.groupedFields[sectionKey];
        if (!fields || !fields.length) {
            return;
        }
        const sectionEl = document.createElement('div');
        sectionEl.className = 'settings-section-panel';
        const header = document.createElement('div');
        header.className = 'settings-section-header';
        header.innerHTML = `<h4>${escapeHtml(SETTINGS_SECTION_LABELS[sectionKey] || sectionKey)}</h4>`;
        sectionEl.appendChild(header);

        const list = document.createElement('div');
        list.className = 'settings-field-list';
        fields.forEach(field => {
            const value = getValueByPath(draft, field.path);
            const row = renderSettingsFieldRow(field, value, scope);
            if (row) {
                list.appendChild(row);
            }
        });
        sectionEl.appendChild(list);
        root.appendChild(sectionEl);
    });
    if (!root.children.length) {
        root.innerHTML = '<div class="muted-text">No server-managed settings are available in this build.</div>';
    }
}

function renderSettingsFieldRow(field, value, scope) {
    const row = document.createElement('div');
    row.className = 'settings-field-row';
    row.dataset.fieldType = (field.type || 'text').toLowerCase();
    const label = document.createElement('div');
    label.className = 'settings-field-label';
    label.innerHTML = `
        <div class="field-title">${escapeHtml(field.title || field.path)}</div>
        <div class="field-description">${escapeHtml(field.description || '')}</div>
    `;

    const control = document.createElement('div');
    control.className = 'settings-field-control';
    const inputFragment = createInputForField(field, value);
    if (!inputFragment || !inputFragment.input || !inputFragment.element) {
        return null;
    }
    const { input, element } = inputFragment;
    const canEdit = userCan('settings.write');
    input.disabled = !canEdit || (scope === 'tenant' && !settingsUIState.selectedTenantId);
    control.appendChild(element);

    if (scope === 'tenant') {
        const isOverride = hasOverride(pathToArray(field.path));
        const badge = document.createElement('span');
        badge.className = `settings-badge ${isOverride ? 'override' : 'inherited'}`;
        badge.textContent = isOverride ? 'Override' : 'Inherited';
        control.appendChild(badge);
        if (isOverride && canEdit) {
            const inheritBtn = document.createElement('button');
            inheritBtn.type = 'button';
            inheritBtn.className = 'ghost-btn inherit-btn';
            inheritBtn.textContent = 'Inherit';
            inheritBtn.dataset.inheritPath = field.path;
            control.appendChild(inheritBtn);
        }
    }

    row.appendChild(label);
    row.appendChild(control);
    return row;
}

function createInputForField(field, value) {
    const type = (field.type || 'text').toLowerCase();
    let input;
    let element;
    if (type === 'bool') {
        input = document.createElement('input');
        input.type = 'checkbox';
        input.checked = !!value;
        const toggle = document.createElement('label');
        toggle.className = 'mini-toggle-container settings-toggle';
        toggle.title = field.title || field.path;
        const state = document.createElement('span');
        state.className = 'settings-toggle-state';
        state.textContent = input.checked ? 'Enabled' : 'Disabled';
        input.addEventListener('change', () => {
            state.textContent = input.checked ? 'Enabled' : 'Disabled';
        });
        toggle.appendChild(input);
        toggle.appendChild(state);
        element = toggle;
    } else if (type === 'number') {
        input = document.createElement('input');
        input.type = 'number';
        input.value = value === null || value === undefined ? '' : value;
        if (field.min !== undefined) input.min = field.min;
        if (field.max !== undefined) input.max = field.max;
        element = input;
    } else if (type === 'select' && Array.isArray(field.enum)) {
        input = document.createElement('select');
        field.enum.forEach(optionValue => {
            const opt = document.createElement('option');
            opt.value = optionValue;
            opt.textContent = optionValue;
            if (optionValue === value) {
                opt.selected = true;
            }
            input.appendChild(opt);
        });
        element = input;
    } else {
        input = document.createElement('input');
        input.type = 'text';
        input.value = value === null || value === undefined ? '' : value;
        element = input;
    }
    input.dataset.settingsPath = field.path;
    input.dataset.fieldType = (field.type || 'text').toLowerCase();
    return { input, element };
}

function handleSettingsFieldChange(event) {
    const target = event.target;
    if (!target || !target.dataset || !target.dataset.settingsPath) {
        return;
    }
    const path = target.dataset.settingsPath;
    const fieldType = target.dataset.fieldType || 'text';
    const newValue = readInputValue(target, fieldType);
    if (settingsUIState.scope === 'global') {
        updateGlobalDraft(path, newValue);
    } else {
        updateTenantDraft(path, newValue);
    }
}

function handleSettingsFieldClick(event) {
    const target = event.target;
    if (target && target.dataset && target.dataset.inheritPath) {
        event.preventDefault();
        clearTenantOverride(target.dataset.inheritPath);
    }
}

function readInputValue(input, fieldType) {
    switch (fieldType) {
        case 'bool':
            return !!input.checked;
        case 'number':
            return input.value === '' ? null : Number(input.value);
        default:
            return input.value;
    }
}

function updateGlobalDraft(path, value) {
    setNestedValue(settingsUIState.globalDraft, path, value);
    const baseline = settingsUIState.globalSnapshot ? settingsUIState.globalSnapshot.Settings : {};
    settingsUIState.globalDirty = !deepEqual(settingsUIState.globalDraft, baseline);
    updateActionButtons();
}

function updateTenantDraft(path, value) {
    if (!settingsUIState.tenantDraft) {
        settingsUIState.tenantDraft = cloneSettings(settingsUIState.globalDraft);
    }
    setNestedValue(settingsUIState.tenantDraft, path, value);
    const baseValue = getValueByPath(settingsUIState.globalSnapshot ? settingsUIState.globalSnapshot.Settings : {}, path);
    if (valuesEqual(value, baseValue)) {
        deleteNestedValue(settingsUIState.tenantOverridesDraft, path);
    } else {
        setNestedValue(settingsUIState.tenantOverridesDraft, path, value);
    }
    const originalOverrides = settingsUIState.tenantSnapshot && settingsUIState.tenantSnapshot.Overrides
        ? settingsUIState.tenantSnapshot.Overrides
        : {};
    settingsUIState.tenantDirty = !deepEqual(settingsUIState.tenantOverridesDraft, originalOverrides);
    renderOverrideSummary();
    updateActionButtons();
}

function clearTenantOverride(path) {
    if (!settingsUIState.tenantDraft) return;
    deleteNestedValue(settingsUIState.tenantOverridesDraft, path);
    const baseValue = getValueByPath(settingsUIState.globalSnapshot ? settingsUIState.globalSnapshot.Settings : {}, path);
    setNestedValue(settingsUIState.tenantDraft, path, baseValue);
    const originalOverrides = settingsUIState.tenantSnapshot && settingsUIState.tenantSnapshot.Overrides
        ? settingsUIState.tenantSnapshot.Overrides
        : {};
    settingsUIState.tenantDirty = !deepEqual(settingsUIState.tenantOverridesDraft, originalOverrides);
    renderSettingsForm();
    renderOverrideSummary();
    updateActionButtons();
}

function handleSettingsScopeChange(scope) {
    if (!scope || scope === settingsUIState.scope) {
        return;
    }
    settingsUIState.scope = scope;
    renderSettingsUI();
}

function handleTenantSelect(event) {
    const tenantId = event.target.value;
    settingsUIState.selectedTenantId = tenantId;
    loadTenantSnapshot(tenantId).then(() => {
        renderSettingsUI();
    }).catch(err => {
        reportSettingsError('Failed to load tenant settings', err);
    });
}

async function handleSettingsSave(event) {
    event.preventDefault();
    if (!userCan('settings.write')) {
        window.__pm_shared.showToast('You do not have permission to update settings', 'error');
        return;
    }
    settingsUIState.saving = true;
    updateActionButtons();
    try {
        if (settingsUIState.scope === 'global') {
            await saveGlobalSettings();
        } else {
            await saveTenantSettings();
        }
    } catch (err) {
        reportSettingsError('Failed to save settings', err);
    } finally {
        settingsUIState.saving = false;
        updateActionButtons();
    }
}

async function saveGlobalSettings() {
    if (!settingsUIState.globalDirty) {
        return;
    }
    await fetchJSON('/api/v1/settings/global', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(settingsUIState.globalDraft)
    });
    await loadGlobalSettingsSnapshot();
    if (settingsUIState.selectedTenantId) {
        await loadTenantSnapshot(settingsUIState.selectedTenantId);
    }
    renderSettingsUI();
    window.__pm_shared.showToast('Global settings saved', 'success');
}

async function saveTenantSettings() {
    if (!settingsUIState.selectedTenantId) {
        window.__pm_shared.showToast('Select a tenant to edit overrides', 'error');
        return;
    }
    const tenantId = settingsUIState.selectedTenantId;
    const overrides = cloneSettings(settingsUIState.tenantOverridesDraft);
    if (flattenOverrides(overrides).length === 0) {
        await fetchJSON(`/api/v1/settings/tenants/${encodeURIComponent(tenantId)}`, { method: 'DELETE' });
    } else {
        await fetchJSON(`/api/v1/settings/tenants/${encodeURIComponent(tenantId)}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(overrides)
        });
    }
    await loadTenantSnapshot(tenantId);
    renderSettingsUI();
    window.__pm_shared.showToast('Tenant overrides saved', 'success');
}

function handleDiscardChanges(event) {
    event.preventDefault();
    if (settingsUIState.scope === 'global') {
        settingsUIState.globalDraft = cloneSettings(settingsUIState.globalSnapshot ? settingsUIState.globalSnapshot.Settings : {});
        settingsUIState.globalDirty = false;
    } else {
        settingsUIState.tenantDraft = cloneSettings(settingsUIState.tenantSnapshot ? settingsUIState.tenantSnapshot.Settings : settingsUIState.globalSnapshot.Settings);
        settingsUIState.tenantOverridesDraft = cloneSettings(settingsUIState.tenantSnapshot && settingsUIState.tenantSnapshot.Overrides ? settingsUIState.tenantSnapshot.Overrides : {});
        settingsUIState.tenantDirty = false;
    }
    renderSettingsUI();
}

async function resetTenantOverrides(event) {
    event.preventDefault();
    if (!settingsUIState.selectedTenantId) {
        return;
    }
    if (!confirm('Clear all overrides for this tenant?')) {
        return;
    }
    try {
        await fetchJSON(`/api/v1/settings/tenants/${encodeURIComponent(settingsUIState.selectedTenantId)}`, { method: 'DELETE' });
        await loadTenantSnapshot(settingsUIState.selectedTenantId);
        renderSettingsUI();
        window.__pm_shared.showToast('Tenant now inherits global defaults', 'success');
    } catch (err) {
        reportSettingsError('Failed to clear tenant overrides', err);
    }
}

function renderOverrideSummary() {
    const container = document.getElementById('settings_override_list');
    if (!container) return;
    if (settingsUIState.scope !== 'tenant' || !settingsUIState.selectedTenantId) {
        container.textContent = 'Select a tenant to view override details.';
        return;
    }
    const overrides = flattenOverrides(settingsUIState.tenantOverridesDraft);
    if (!overrides.length) {
        container.innerHTML = '<div class="muted-text">No overrides. This tenant inherits all global defaults.</div>';
        return;
    }
    const list = overrides.map(item => {
        return `<li><strong>${escapeHtml(item.path)}</strong><div>${escapeHtml(String(item.value))}</div></li>`;
    }).join('');
    container.innerHTML = `<ul class="settings-override-list">${list}</ul>`;
}

function updateActionButtons() {
    const saveBtn = document.getElementById('settings_save_btn');
    const discardBtn = document.getElementById('settings_discard_btn');
    const resetBtn = document.getElementById('settings_reset_overrides_btn');
    const status = document.getElementById('settings_status');
    const canEdit = userCan('settings.write');
    const dirty = settingsUIState.scope === 'global' ? settingsUIState.globalDirty : settingsUIState.tenantDirty;
    if (saveBtn) {
        saveBtn.disabled = !canEdit || settingsUIState.saving || !dirty;
    }
    if (discardBtn) {
        discardBtn.disabled = !dirty;
    }
    if (resetBtn) {
        const hasOverrides = flattenOverrides(settingsUIState.tenantOverridesDraft).length > 0;
        resetBtn.classList.toggle('hidden', settingsUIState.scope !== 'tenant');
        resetBtn.disabled = !canEdit || !hasOverrides || settingsUIState.saving;
    }
    const tenantControls = document.getElementById('settings_tenant_controls');
    if (tenantControls) {
        tenantControls.classList.toggle('hidden', settingsUIState.scope !== 'tenant' || settingsUIState.tenantList.length === 0);
    }
    if (status) {
        if (settingsUIState.saving) {
            status.textContent = 'Saving…';
        } else if (dirty) {
            status.textContent = 'Unsaved changes';
        } else {
            status.textContent = '';
        }
    }
}

function updateLastUpdatedMeta() {
    const el = document.getElementById('settings_last_updated');
    if (!el) return;
    let text = '';
    if (settingsUIState.scope === 'global' && settingsUIState.globalSnapshot) {
        const snap = settingsUIState.globalSnapshot;
        if (snap.UpdatedAt) {
            text = `Updated ${formatRelativeTime(snap.UpdatedAt)} by ${escapeHtml(snap.UpdatedBy || 'system')}`;
        }
    } else if (settingsUIState.scope === 'tenant' && settingsUIState.tenantSnapshot) {
        const snap = settingsUIState.tenantSnapshot;
        if (snap.OverridesUpdatedAt) {
            text = `Overrides updated ${formatRelativeTime(snap.OverridesUpdatedAt)} by ${escapeHtml(snap.OverridesUpdatedBy || 'system')}`;
        } else {
            text = 'Inheriting global defaults';
        }
    }
    el.textContent = text;
}

function flattenOverrides(overrides, prefix = '', acc = []) {
    if (!overrides || typeof overrides !== 'object') {
        return acc;
    }
    Object.keys(overrides).forEach(key => {
        const path = prefix ? `${prefix}.${key}` : key;
        const value = overrides[key];
        if (value && typeof value === 'object' && !Array.isArray(value)) {
            flattenOverrides(value, path, acc);
        } else {
            acc.push({ path, value });
        }
    });
    return acc;
}

function hasOverride(pathParts) {
    let cursor = settingsUIState.tenantOverridesDraft;
    for (let i = 0; i < pathParts.length; i++) {
        const part = pathParts[i];
        if (!cursor || typeof cursor !== 'object' || !(part in cursor)) {
            return false;
        }
        cursor = cursor[part];
    }
    return true;
}

function pathToArray(path) {
    return (path || '').split('.');
}

function readNested(obj, parts) {
    let cursor = obj;
    for (let i = 0; i < parts.length; i++) {
        if (!cursor) return undefined;
        cursor = cursor[parts[i]];
    }
    return cursor;
}

function setNestedValue(obj, path, value) {
    if (!obj) return;
    const parts = pathToArray(path);
    let cursor = obj;
    for (let i = 0; i < parts.length - 1; i++) {
        const key = parts[i];
        if (typeof cursor[key] !== 'object' || cursor[key] === null) {
            cursor[key] = {};
        }
        cursor = cursor[key];
    }
    cursor[parts[parts.length - 1]] = value;
}

function deleteNestedValue(obj, path) {
    if (!obj) return;
    const parts = pathToArray(path);
    const stack = [];
    let cursor = obj;
    for (let i = 0; i < parts.length - 1; i++) {
        const key = parts[i];
        if (typeof cursor[key] !== 'object' || cursor[key] === null) {
            return;
        }
        stack.push([cursor, key]);
        cursor = cursor[key];
    }
    delete cursor[parts[parts.length - 1]];
    for (let i = stack.length - 1; i >= 0; i--) {
        const [parent, key] = stack[i];
        if (parent[key] && Object.keys(parent[key]).length === 0) {
            delete parent[key];
        }
    }
}

function getValueByPath(obj, path) {
    return readNested(obj, pathToArray(path));
}

function valuesEqual(a, b) {
    if (typeof a === 'number' && typeof b === 'number') {
        return Number(a) === Number(b);
    }
    if (typeof a === 'boolean' || typeof b === 'boolean') {
        return !!a === !!b;
    }
    return a === b;
}

function cloneSettings(obj) {
    return obj ? JSON.parse(JSON.stringify(obj)) : {};
}

function deepEqual(a, b) {
    if (a === b) {
        return true;
    }
    if (Number.isNaN(a) && Number.isNaN(b)) {
        return true;
    }
    if (Array.isArray(a) || Array.isArray(b)) {
        if (!Array.isArray(a) || !Array.isArray(b) || a.length !== b.length) {
            return false;
        }
        for (let i = 0; i < a.length; i++) {
            if (!deepEqual(a[i], b[i])) {
                return false;
            }
        }
        return true;
    }
    if (a && b && typeof a === 'object' && typeof b === 'object') {
        const keysA = Object.keys(a);
        const keysB = Object.keys(b);
        if (keysA.length !== keysB.length) {
            return false;
        }
        for (const key of keysA) {
            if (!deepEqual(a[key], b[key])) {
                return false;
            }
        }
        return true;
    }
    return false;
}

async function fetchJSON(url, options = {}) {
    const response = await fetch(url, options);
    if (!response.ok) {
        const err = new Error(`HTTP ${response.status}`);
        err.status = response.status;
        try {
            err.body = await response.text();
        } catch (_) {
            err.body = '';
        }
        throw err;
    }
    if (response.status === 204) {
        return null;
    }
    const text = await response.text();
    return text ? JSON.parse(text) : null;
}

function reportSettingsError(message, err) {
    window.__pm_shared.error(message, err);
    let detail = '';
    if (err) {
        const extra = err.body || err.message;
        if (extra) {
            detail = ': ' + String(extra).slice(0, 200);
        }
    }
    window.__pm_shared.showToast(message + detail, 'error', 5000);
}

// ====== Logs Management ======
function initAuditFilterControls() {
    if (auditFiltersInitialized) {
        return;
    }
    auditFiltersInitialized = true;

    const searchInput = document.getElementById('audit_search_filter');
    if (searchInput) {
        const handler = debounce(() => {
            auditFilterState.search = (searchInput.value || '').trim().toLowerCase();
            applyAuditFilters();
        }, 200);
        searchInput.addEventListener('input', handler);
    }

    const actionInput = document.getElementById('audit_action_filter');
    if (actionInput) {
        const handler = debounce(() => {
            auditFilterState.action = (actionInput.value || '').trim().toLowerCase();
            applyAuditFilters();
        }, 200);
        actionInput.addEventListener('input', handler);
    }

    const tenantInput = document.getElementById('audit_tenant_filter');
    if (tenantInput) {
        const handler = debounce(() => {
            auditFilterState.tenant = (tenantInput.value || '').trim().toLowerCase();
            applyAuditFilters();
        }, 200);
        tenantInput.addEventListener('input', handler);
    }

    const severityContainer = document.getElementById('audit_severity_filter');
    if (severityContainer) {
        const checkboxes = Array.from(severityContainer.querySelectorAll('.audit-severity-option'));
        const update = () => updateAuditSeverityState(checkboxes);
        checkboxes.forEach(cb => {
            toggleSeverityPillState(cb);
            cb.addEventListener('change', update);
        });
    }

    const resetBtn = document.getElementById('audit_clear_filters_btn');
    if (resetBtn) {
        resetBtn.addEventListener('click', (ev) => {
            ev.preventDefault();
            resetAuditFilters();
        });
    }

    const liveToggle = document.getElementById('audit_live_toggle');
    if (liveToggle) {
        liveToggle.addEventListener('change', () => {
            toggleAuditLiveUpdates(Boolean(liveToggle.checked));
        });
    }
}

function updateAuditSeverityState(checkboxes) {
    const selected = new Set();
    checkboxes.forEach(cb => {
        toggleSeverityPillState(cb);
        if (cb.checked) {
            selected.add((cb.value || '').toLowerCase());
        }
    });
    if (selected.size === 0) {
        checkboxes.forEach(cb => {
            cb.checked = true;
            toggleSeverityPillState(cb);
            selected.add((cb.value || '').toLowerCase());
        });
        if (window.__pm_shared && typeof window.__pm_shared.showToast === 'function') {
            window.__pm_shared.showToast('Select at least one severity to filter', 'info');
        }
    }
    auditFilterState.severities = selected;
    applyAuditFilters();
}

function toggleSeverityPillState(checkbox) {
    if (!checkbox) return;
    const pill = checkbox.closest('.audit-severity-pill');
    if (pill) {
        pill.classList.toggle('active', checkbox.checked);
    }
}

function resetAuditFilters() {
    const actionInput = document.getElementById('audit_action_filter');
    const tenantInput = document.getElementById('audit_tenant_filter');
    const searchInput = document.getElementById('audit_search_filter');
    if (actionInput) actionInput.value = '';
    if (tenantInput) tenantInput.value = '';
    if (searchInput) searchInput.value = '';
    auditFilterState.action = '';
    auditFilterState.tenant = '';
    auditFilterState.search = '';
    const severityCheckboxes = document.querySelectorAll('.audit-severity-option');
    severityCheckboxes.forEach(cb => {
        cb.checked = true;
        toggleSeverityPillState(cb);
    });
    auditFilterState.severities = new Set(AUDIT_SEVERITY_VALUES);
    applyAuditFilters();
}

function setAuditEntries(entries) {
    auditDataLoaded = true;
    auditLogEntries = Array.isArray(entries) ? entries : [];
    updateAuditActionSuggestions(auditLogEntries);
    applyAuditFilters();
}

function hasActiveAuditFilters() {
    const severities = auditFilterState.severities instanceof Set ? auditFilterState.severities : new Set(AUDIT_SEVERITY_VALUES);
    const allSeveritiesSelected = severities.size === AUDIT_SEVERITY_VALUES.length;
    return Boolean(auditFilterState.search || auditFilterState.action || auditFilterState.tenant || !allSeveritiesSelected);
}

function applyAuditFilters() {
    const entries = Array.isArray(auditLogEntries) ? auditLogEntries : [];
    const severitySet = auditFilterState.severities instanceof Set && auditFilterState.severities.size > 0
        ? auditFilterState.severities
        : new Set(AUDIT_SEVERITY_VALUES);
    const actionQuery = auditFilterState.action;
    const tenantQuery = auditFilterState.tenant;
    const searchTokens = auditFilterState.search ? auditFilterState.search.split(/\s+/).filter(Boolean) : [];

    const filtered = entries.filter(entry => {
        const severity = String(entry && entry.severity ? entry.severity : 'info').toLowerCase();
        if (severitySet.size > 0 && !severitySet.has(severity)) {
            return false;
        }

        if (actionQuery && !(String(entry.action || '').toLowerCase().includes(actionQuery))) {
            return false;
        }

        if (tenantQuery) {
            const tenantMatches = [
                entry.tenant_id,
                entry.metadata && (entry.metadata.tenant_name || entry.metadata.tenant_display || entry.metadata.tenant),
            ].filter(Boolean).map(v => String(v).toLowerCase());
            if (!tenantMatches.some(val => val.includes(tenantQuery))) {
                return false;
            }
        }

        if (searchTokens.length > 0) {
            const haystack = buildAuditSearchHaystack(entry);
            if (!searchTokens.every(token => haystack.includes(token))) {
                return false;
            }
        }

        return true;
    });

    renderAuditLogs(filtered, { filtersActive: hasActiveAuditFilters() });
    updateAuditSummary(entries.length, filtered.length);
}

function buildAuditSearchHaystack(entry) {
    if (!entry) return '';
    let metadataBlob = '';
    if (entry.metadata) {
        try {
            metadataBlob = JSON.stringify(entry.metadata);
        } catch (err) {
            metadataBlob = '';
        }
    }
    return [
        entry.severity,
        entry.actor_name,
        entry.actor_id,
        entry.actor_type,
        entry.action,
        entry.target_type,
        entry.target_id,
        entry.tenant_id,
        entry.details,
        entry.ip_address,
        entry.user_agent,
        entry.request_id,
        metadataBlob,
    ].filter(Boolean).join(' ').toLowerCase();
}

function updateAuditSummary(total, filtered) {
    const summary = document.getElementById('audit_summary');
    if (!summary) return;
    if (!auditDataLoaded) {
        summary.setAttribute('hidden', 'hidden');
        return;
    }
    const countsEl = document.getElementById('audit_summary_counts');
    if (countsEl) {
        if (total === filtered) {
            countsEl.innerHTML = `<strong>${filtered}</strong> ${filtered === 1 ? 'entry' : 'entries'}`;
        } else {
            countsEl.innerHTML = `<strong>${filtered}</strong> of ${total} entries`;
        }
    }
    summary.removeAttribute('hidden');
}

function setAuditLastUpdated(date = new Date()) {
    auditLastUpdated = date;
    const updatedEl = document.getElementById('audit_summary_updated');
    if (!updatedEl) return;
    const isoValue = date instanceof Date ? date.toISOString() : date;
    const relative = formatRelativeTime(isoValue);
    const exact = date instanceof Date ? date.toLocaleTimeString() : String(date);
    updatedEl.textContent = `Updated ${relative} (${exact})`;
}

function updateAuditActionSuggestions(entries) {
    const dataList = document.getElementById('audit_action_suggestions');
    if (!dataList) return;
    dataList.innerHTML = '';
    const unique = new Set();
    entries.forEach(entry => {
        if (entry && entry.action) {
            unique.add(entry.action);
        }
    });
    Array.from(unique).sort().slice(0, 50).forEach(action => {
        const option = document.createElement('option');
        option.value = action;
        dataList.appendChild(option);
    });
}

function toggleAuditLiveUpdates(enabled) {
    auditLiveRequested = enabled;
    const toggle = document.getElementById('audit_live_toggle');
    if (toggle && toggle.checked !== enabled) {
        toggle.checked = enabled;
    }
    syncAuditLiveTimer();
    if (enabled && activeLogView === 'audit') {
        loadAuditLogs({ silent: true });
    }
}

function syncAuditLiveTimer() {
    if (auditAutoRefreshHandle) {
        clearInterval(auditAutoRefreshHandle);
        auditAutoRefreshHandle = null;
    }
    if (auditLiveRequested && activeLogView === 'audit') {
        auditAutoRefreshHandle = setInterval(() => {
            loadAuditLogs({ silent: true });
        }, AUDIT_AUTO_REFRESH_INTERVAL_MS);
    }
    updateAuditLiveStatus();
}

function updateAuditLiveStatus() {
    const statusEl = document.getElementById('audit_live_status');
    if (!statusEl) return;
    if (!auditLiveRequested) {
        statusEl.textContent = 'Auto-refresh off';
        return;
    }
    if (activeLogView !== 'audit') {
        statusEl.textContent = 'Auto-refresh paused';
        return;
    }
    statusEl.textContent = auditAutoRefreshHandle ? 'Auto-refresh on' : 'Auto-refresh ready';
}

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

async function loadAuditLogs(options) {
    if (!userCan('audit.logs.read')) {
        return;
    }

    const opts = options || {};
    const silent = Boolean(opts.silent);

    const container = document.getElementById('audit_logs_table');
    if (!container) {
        window.__pm_shared.warn('loadAuditLogs: container not found');
        return;
    }

    const params = new URLSearchParams();
    const timeFilter = document.getElementById('audit_time_filter');
    const hours = timeFilter ? parseInt(timeFilter.value, 10) : 24;
    if (hours && hours > 0) {
        params.set('hours', String(hours));
    }

    const actorFilter = document.getElementById('audit_actor_filter');
    const actorValue = actorFilter ? actorFilter.value.trim() : '';
    if (actorValue) {
        params.set('actor_id', actorValue);
    }

    if (!silent) {
        container.innerHTML = '<div class="muted-text">Loading audit log...</div>';
    }

    const queryString = params.toString();
    const endpoint = queryString ? `/api/audit/logs?${queryString}` : '/api/audit/logs';

    try {
        const response = await fetch(endpoint);
        if (!response.ok) {
            throw new Error(`HTTP ${response.status}`);
        }

        const payload = await response.json();
        let entries = [];
        if (payload && Array.isArray(payload.entries)) {
            entries = payload.entries;
        } else if (Array.isArray(payload)) {
            entries = payload;
        }
        setAuditEntries(entries);
        setAuditLastUpdated(new Date());
    } catch (error) {
        window.__pm_shared.error('Failed to load audit logs:', error);
        if (!silent) {
            const message = escapeHtml(error && error.message ? error.message : String(error));
            container.innerHTML = `<div class="error-text">Failed to load audit logs: ${message}</div>`;
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
            if (data.recent && Array.isArray(data.recent) && data.recent.length > 0) {
                const rows = data.recent.map(entry => {
                    const serial = escapeHtml(entry.serial || '—');
                    const timestamp = escapeHtml(formatDateTime(entry.timestamp));
                    const relative = escapeHtml(formatRelativeTime(entry.timestamp));
                    const pageCount = escapeHtml(formatNumber(entry.page_count));
                    return `
                        <tr>
                            <td>${serial}</td>
                            <td>
                                <div class="table-primary">${timestamp}</div>
                                <div class="muted-text">${relative}</div>
                            </td>
                            <td>${pageCount}</td>
                        </tr>
                    `;
                }).join('\n');

                contentEl.innerHTML = `
                    <div class="table-wrapper">
                        <table class="simple-table">
                            <thead>
                                <tr>
                                    <th>Device Serial</th>
                                    <th>Last Metric</th>
                                    <th>Page Count</th>
                                </tr>
                            </thead>
                            <tbody>
                                ${rows}
                            </tbody>
                        </table>
                    </div>
                `;
            } else {
                contentEl.innerHTML = '<div class="muted-text" style="padding:12px">No recent metrics available.</div>';
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

function renderAuditLogs(entries, options = {}) {
    const container = document.getElementById('audit_logs_table');
    if (!container) {
        window.__pm_shared.warn('renderAuditLogs: container not found');
        return;
    }

    if (!Array.isArray(entries) || entries.length === 0) {
        const message = options.filtersActive
            ? 'No audit entries match the current filters.'
            : 'No audit entries in this window.';
        container.innerHTML = `<div class="muted-text" style="padding:12px;">${message}</div>`;
        return;
    }

    const rows = entries.map(entry => {
        const ts = escapeHtml(formatDateTime(entry.timestamp));
        const rel = escapeHtml(formatRelativeTime(entry.timestamp));
        const actorName = entry.actor_name || entry.actor_id || '—';
        const actorMetaParts = [];
        if (entry.actor_type) {
            actorMetaParts.push((entry.actor_type || '').toUpperCase());
        }
        if (entry.actor_id) {
            actorMetaParts.push(entry.actor_id);
        }
        const actorMeta = actorMetaParts.length ? `<div class="audit-actor-meta">${escapeHtml(actorMetaParts.join(' • '))}</div>` : '';

        const severity = String(entry.severity || 'info').toLowerCase();
        const severityBadge = `<span class="badge ${getSeverityBadgeClass(severity)}">${escapeHtml(severity.toUpperCase())}</span>`;

        const actionLabel = escapeHtml(entry.action || '—');

        const targetPrimaryParts = [];
        if (entry.target_type) targetPrimaryParts.push(entry.target_type);
        if (entry.target_id) targetPrimaryParts.push(entry.target_id);
        const targetPrimary = targetPrimaryParts.length ? escapeHtml(targetPrimaryParts.join(' • ')) : '—';
        const tenantTag = entry.tenant_id ? `<div class="audit-target-meta">Tenant: ${escapeHtml(entry.tenant_id)}</div>` : '';

        const detailLines = [];
        if (entry.details) {
            detailLines.push(entry.details);
        }
        if (entry.ip_address) {
            detailLines.push(`IP: ${entry.ip_address}`);
        }
        if (entry.user_agent) {
            detailLines.push(`User-Agent: ${entry.user_agent}`);
        }
        if (entry.request_id) {
            detailLines.push(`Request: ${entry.request_id}`);
        }
        const metadataText = formatAuditMetadata(entry.metadata);
        const detailText = detailLines.length ? escapeHtml(detailLines.join('\n')) : '—';
        const metadataBlock = metadataText ? `<pre class="audit-metadata">${escapeHtml(metadataText)}</pre>` : '';

        return `
            <tr>
                <td>
                    <div class="table-primary">${ts}</div>
                    <div class="muted-text">${rel}</div>
                </td>
                <td>
                    <div class="table-primary">${escapeHtml(actorName)}</div>
                    ${actorMeta}
                </td>
                <td>
                    <div class="table-primary">${actionLabel}</div>
                    <div class="audit-detail-meta">${severityBadge}</div>
                </td>
                <td>
                    <div class="table-primary">${targetPrimary}</div>
                    ${tenantTag}
                </td>
                <td>
                    <div class="audit-details">${detailText}</div>
                    ${metadataBlock}
                </td>
            </tr>
        `;
    }).join('');

    container.innerHTML = `
        <table class="simple-table">
            <thead>
                <tr>
                    <th>Timestamp</th>
                    <th>Actor</th>
                    <th>Action</th>
                    <th>Target</th>
                    <th>Details</th>
                </tr>
            </thead>
            <tbody>
                ${rows}
            </tbody>
        </table>
    `;
}

function getSeverityBadgeClass(severity) {
    switch (severity) {
        case 'error':
            return 'badge-error';
        case 'warn':
        case 'warning':
            return 'badge-warn';
        default:
            return 'badge-info';
    }
}

function formatAuditMetadata(metadata) {
    if (!metadata || typeof metadata !== 'object') {
        return '';
    }
    try {
        return JSON.stringify(metadata, null, 2);
    } catch (err) {
        window.__pm_shared.warn('formatAuditMetadata failed', err);
        return '';
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

// ====== Add Agent Modal UI ======
function initAddAgentUI(){
    if (addAgentUIInitialized) return;
    addAgentUIInitialized = true;
    // Wire header Join button if present
    const joinBtn = document.getElementById('join_token_btn');
    if(joinBtn) joinBtn.addEventListener('click', ()=> openAddAgentModal({}));

    // Wire modal chrome
    const closeX = document.getElementById('add_agent_close_x');
    const cancelBtn = document.getElementById('add_agent_cancel');
    const primaryBtn = document.getElementById('add_agent_primary');

    if(closeX) closeX.addEventListener('click', closeAddAgentModal);
    if(cancelBtn) cancelBtn.addEventListener('click', closeAddAgentModal);
    if(primaryBtn) primaryBtn.addEventListener('click', handleAddAgentPrimary);
}

function openAddAgentModal(opts){
    // opts: { tenantID?: string }
    window._addAgentState = {
        step: 1,
        tenantID: opts && opts.tenantID ? opts.tenantID : null,
        ttl: 60,
        one_time: true,
        token: null
    };
    renderAddAgentStep(1);
    const modal = document.getElementById('add_agent_modal');
    if(modal){ modal.style.display = 'flex'; document.body.style.overflow = 'hidden'; }
}

function closeAddAgentModal(){
    const modal = document.getElementById('add_agent_modal');
    if(modal){ modal.style.display = 'none'; document.body.style.overflow = ''; }
    try{ delete window._addAgentState; }catch(e){}
}

async function handleAddAgentPrimary(){
    const st = window._addAgentState || {step:1};
    if(st.step === 1){
        // Read form values
        const tenantSel = document.getElementById('add_agent_tenant');
        const ttlEl = document.getElementById('add_agent_ttl');
        const oneTimeEl = document.getElementById('add_agent_one_time');
        const tenantID = tenantSel ? tenantSel.value : (st.tenantID || null);
        const ttl = ttlEl ? parseInt(ttlEl.value,10) || 60 : 60;
        const one_time = oneTimeEl ? oneTimeEl.checked : true;

        // Basic validation
        if(!tenantID){ window.__pm_shared.showAlert('Please select a tenant (customer) to assign this token to.', 'Missing tenant', true, false); return; }

        // Decide whether user wants a raw token or a generated bootstrap script
        const selectedActionEl = document.querySelector('input[name="add_agent_action"]:checked');
        const action = selectedActionEl ? selectedActionEl.value : 'token';
        if(action === 'token'){
            // Create join token
            try{
                const payload = { tenant_id: tenantID, ttl_minutes: ttl, one_time: one_time };
                const r = await fetch('/api/v1/join-token', { method: 'POST', headers: {'content-type':'application/json'}, body: JSON.stringify(payload) });
                if(!r.ok) throw new Error(await r.text());
                const data = await r.json();
                st.token = data.token;
                st.step = 2;
                st.tenantID = tenantID;
                window._addAgentState = st;
                renderAddAgentStep(2);
            }catch(err){
                window.__pm_shared.showAlert('Failed to create join token: ' + (err && err.message ? err.message : err), 'Error', true, false);
            }
        } else {
            // Generate bootstrap script via server packages API
            try{
                const platformSel = document.getElementById('add_agent_platform');
                const platform = platformSel ? platformSel.value : 'linux';
                const payload = { tenant_id: tenantID, platform: platform, installer_type: 'script', ttl_minutes: ttl };
                const r = await fetch('/api/v1/packages', { method: 'POST', headers: {'content-type':'application/json'}, body: JSON.stringify(payload) });
                if(!r.ok) {
                    throw new Error(await r.text());
                }
                // Handle JSON or plain text responses
                const ct = (r.headers.get('content-type') || '').toLowerCase();
                let scriptText = '';
                let filename = 'bootstrap.sh';
                let downloadURL = null;
                let oneLiner = null;
                if(ct.includes('application/json')){
                    const data = await r.json();
                    scriptText = data.script || '';
                    filename = data.filename || filename;
                    downloadURL = data.download_url || null;
                    oneLiner = data.one_liner || null;
                } else {
                    scriptText = await r.text();
                    const cd = r.headers.get('content-disposition');
                    if(cd){
                        const m = cd.match(/filename="?([^";]+)"?/);
                        if(m && m[1]) filename = m[1];
                    } else if(platform === 'windows') filename = 'bootstrap.ps1';
                    else if(platform === 'darwin') filename = 'bootstrap.sh';
                }

                st.script = scriptText;
                st.scriptFilename = filename;
                st.scriptDownloadURL = downloadURL;
                st.oneLiner = oneLiner;
                st.step = 2;
                st.tenantID = tenantID;
                window._addAgentState = st;
                renderAddAgentStep(2);
            }catch(err){
                window.__pm_shared.showAlert('Failed to generate bootstrap script: ' + (err && err.message ? err.message : err), 'Error', true, false);
            }
        }
    } else if(st.step === 2){
        // Done
        closeAddAgentModal();
        // Optionally refresh tokens/tenants list
        try{ loadTenants(); }catch(e){}
    }
}

function renderAddAgentStep(step){
    const indicator = document.getElementById('add_agent_step_indicator');
    const content = document.getElementById('add_agent_content');
    const primaryBtn = document.getElementById('add_agent_primary');
    if(!content) return;
    if(indicator) indicator.textContent = step === 1 ? 'Step 1/2 — Create join token' : 'Step 2/2 — Token (shown once)';

    if(step === 1){
        // Tenant select
        let tenantOptions = '';
        const tenants = Array.isArray(window._tenants) ? window._tenants : [];
        if(tenants.length === 0 && window._addAgentState && window._addAgentState.tenantID){
            tenantOptions = `<option value="${escapeHtml(window._addAgentState.tenantID)}">${escapeHtml(window._addAgentState.tenantID)}</option>`;
        } else {
            tenantOptions = tenants.map(t => `<option value="${escapeHtml(t.id||t.uuid||t.name)}">${escapeHtml(t.name||t.id||t.uuid||t.name)}</option>`).join('\n');
        }

        content.innerHTML = `
            <div style="display:flex;flex-direction:column;gap:8px;">
                <label style="font-weight:600">Customer (tenant)</label>
                <select id="add_agent_tenant" style="padding:8px;border-radius:4px;border:1px solid var(--border);">
                    <option value="">-- select customer --</option>
                    ${tenantOptions}
                </select>

                <label style="font-weight:600">Token TTL (minutes)</label>
                <input id="add_agent_ttl" type="number" value="60" style="padding:8px;border-radius:4px;border:1px solid var(--border);width:120px;" />

                <label style="display:flex;align-items:center;gap:8px;">
                    <input id="add_agent_one_time" type="checkbox" checked />
                    <span style="color:var(--muted)">One-time (single-use) token</span>
                </label>

                <div style="margin-top:8px;">
                    <div style="font-weight:600;margin-bottom:6px;">Onboarding method</div>
                    <label style="display:flex;align-items:center;gap:8px;"><input type="radio" name="add_agent_action" value="token" checked /> Show raw token</label>
                    <label style="display:flex;align-items:center;gap:8px;"><input type="radio" name="add_agent_action" value="script" /> Generate bootstrap script</label>
                    <div id="add_agent_platform_row" style="margin-top:8px;display:none;">
                        <label style="font-weight:600">Target platform</label>
                        <select id="add_agent_platform" style="padding:8px;border-radius:4px;border:1px solid var(--border);width:180px;">
                            <option value="linux">Linux</option>
                            <option value="windows" selected>Windows</option>
                            <option value="darwin">macOS</option>
                        </select>
                    </div>
                </div>

                <div style="color:var(--muted);font-size:13px">This will create a join token that an agent can use to register with the server. The token will be shown once — copy it and deliver it to the agent (or use agent onboarding).</div>
            </div>
        `;
        // Preselect tenant if supplied
        if(window._addAgentState && window._addAgentState.tenantID){
            const sel = content.querySelector('#add_agent_tenant');
            if(sel) sel.value = window._addAgentState.tenantID;
        }
        // Wire action radio toggles to show/hide platform and update primary button label
        const actionRadios = content.querySelectorAll('input[name="add_agent_action"]');
        const platRow = content.querySelector('#add_agent_platform_row');
        const updatePrimaryLabel = () => {
            const sel = content.querySelector('input[name="add_agent_action"]:checked');
            if(!sel) return;
            if(primaryBtn) primaryBtn.textContent = (sel.value === 'script') ? 'Create Script' : 'Create Token';
        };
        actionRadios.forEach(r => r.addEventListener('change', ()=>{
            if(platRow) platRow.style.display = (r.value === 'script' && r.checked) ? '' : 'none';
            updatePrimaryLabel();
        }));
        // Initialize primary button label based on default selection
        updatePrimaryLabel();
    } else {
        // Step 2: show token
        const token = (window._addAgentState && window._addAgentState.token) ? window._addAgentState.token : '';
        // If we have a generated script, show it instead of raw token
        const script = (window._addAgentState && window._addAgentState.script) ? window._addAgentState.script : null;
        const filename = (window._addAgentState && window._addAgentState.scriptFilename) ? window._addAgentState.scriptFilename : 'bootstrap';
        if(script){
            const oneLiner = (window._addAgentState && window._addAgentState.oneLiner) ? window._addAgentState.oneLiner : null;
            content.innerHTML = `
                <div style="display:flex;flex-direction:column;gap:12px;">
                    ${ oneLiner ? `<div style="font-family:monospace;padding:12px;background:var(--panel);border-radius:6px;border:1px dashed var(--border);">${escapeHtml(oneLiner)}</div>` : `<div style="font-family:monospace;white-space:pre-wrap;padding:12px;background:var(--panel);border-radius:6px;border:1px dashed var(--border);">${escapeHtml(script)}</div>` }
                    <div style="display:flex;gap:8px;">
                        <button id="add_agent_copy" class="modal-button modal-button-secondary">${ oneLiner ? 'Copy one-liner' : 'Copy script' }</button>
                        <button id="add_agent_download" class="modal-button">Download script</button>
                        ${ (window._addAgentState && window._addAgentState.scriptDownloadURL) ? `<a id="add_agent_download_url" class="modal-button" href="${escapeHtml(window._addAgentState.scriptDownloadURL)}" target="_blank">Open hosted URL</a>` : '' }
                    </div>
                    ${ oneLiner ? `<button id="add_agent_show_full" class="modal-button modal-button-secondary">Show full script</button>` : '' }
                    <div style="color:var(--muted);font-size:13px">This script was generated for the selected platform. Download or copy it and execute it on the target machine to install and register the agent.</div>
                    ${ oneLiner ? `<div id="add_agent_full_script" style="display:none;margin-top:8px;font-family:monospace;white-space:pre-wrap;padding:12px;background:var(--panel);border-radius:6px;border:1px dashed var(--border);">${escapeHtml(script)}</div>` : '' }
                </div>
            `;
            const copyBtn = document.getElementById('add_agent_copy');
            if(copyBtn) copyBtn.addEventListener('click', ()=>{
                const textToCopy = oneLiner ? oneLiner : script;
                navigator.clipboard?.writeText(textToCopy).then(()=>{ window.__pm_shared.showToast((oneLiner ? 'One-liner' : 'Script') + ' copied to clipboard','success'); }).catch(err=>{ window.__pm_shared.showAlert('Failed to copy: ' + (err && err.message ? err.message : err), 'Error', true, false); });
            });
            const dlBtn = document.getElementById('add_agent_download');
            if(dlBtn) dlBtn.addEventListener('click', ()=>{
                const blob = new Blob([script], {type:'application/octet-stream'});
                const url = URL.createObjectURL(blob);
                const a = document.createElement('a'); a.href = url; a.download = filename; document.body.appendChild(a); a.click(); a.remove(); URL.revokeObjectURL(url);
            });
            const showFull = document.getElementById('add_agent_show_full');
            if(showFull){
                showFull.addEventListener('click', ()=>{
                    const full = document.getElementById('add_agent_full_script');
                    if(!full) return;
                    if(full.style.display === 'none'){
                        full.style.display = 'block'; showFull.textContent = 'Hide full script';
                    } else { full.style.display = 'none'; showFull.textContent = 'Show full script'; }
                });
            }
        } else {
            content.innerHTML = `
                <div style="display:flex;flex-direction:column;gap:12px;">
                    <div style="font-family:monospace;white-space:pre-wrap;padding:12px;background:var(--panel);border-radius:6px;border:1px dashed var(--border);">${escapeHtml(token)}</div>
                    <div style="display:flex;gap:8px;">
                        <button id="add_agent_copy" class="modal-button modal-button-secondary">Copy token</button>
                        <button id="add_agent_download" class="modal-button">Download token</button>
                    </div>
                    <div style="color:var(--muted);font-size:13px">This token is shown only once. After you close this modal the raw token cannot be retrieved again from the server.</div>
                </div>
            `;
            // Wire copy/download for token
            const copyBtn = document.getElementById('add_agent_copy');
            if(copyBtn) copyBtn.addEventListener('click', copyTokenToClipboard);
            const dlBtn = document.getElementById('add_agent_download');
            if(dlBtn) dlBtn.addEventListener('click', ()=>{
                const blob = new Blob([token], {type:'text/plain'});
                const url = URL.createObjectURL(blob);
                const a = document.createElement('a');
                a.href = url; a.download = 'join-token.txt'; document.body.appendChild(a); a.click(); a.remove(); URL.revokeObjectURL(url);
            });
        }
        if(primaryBtn) primaryBtn.textContent = 'Done';
    }
}

function copyTokenToClipboard(){
    const token = (window._addAgentState && window._addAgentState.token) ? window._addAgentState.token : '';
    if(!token) return;
    navigator.clipboard?.writeText(token).then(()=>{
        window.__pm_shared.showToast('Token copied to clipboard', 'success');
    }).catch(err=>{
        window.__pm_shared.showAlert('Failed to copy token: ' + (err && err.message ? err.message : err), 'Error', true, false);
    });
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

