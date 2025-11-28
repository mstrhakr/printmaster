// PrintMaster Server - Web UI JavaScript

const DEFAULT_ROLE_PRIORITY = { admin: 3, operator: 2, viewer: 1 };
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
            initSettingsSubTabs();
            switchSettingsView(activeSettingsView, true);
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

const SERVER_UI_STATE_KEYS = {
    ACTIVE_TAB: 'pm_server_active_tab',
    SETTINGS_VIEW: 'pm_server_settings_view',
    LOG_VIEW: 'pm_server_log_view',
    TENANTS_VIEW: 'pm_server_tenants_view',
};

const VALID_SETTINGS_VIEWS = ['server', 'sso', 'fleet', 'updates'];
const VALID_LOG_VIEWS = ['system', 'audit'];
const VALID_TENANT_VIEWS = ['directory', 'bundles'];

function getPersistedUIState(key, fallback, allowedValues) {
    try {
        if (typeof window === 'undefined' || !window.localStorage) {
            return fallback;
        }
        const value = window.localStorage.getItem(key);
        if (value === null || value === undefined || value === '') {
            return fallback;
        }
        if (Array.isArray(allowedValues) && allowedValues.length > 0 && !allowedValues.includes(value)) {
            return fallback;
        }
        return value;
    } catch (err) {
        return fallback;
    }
}

function persistUIState(key, value) {
    try {
        if (typeof window === 'undefined' || !window.localStorage) {
            return;
        }
        window.localStorage.setItem(key, value);
    } catch (err) {
        // No-op: best-effort persistence only
    }
}

let currentUser = null;
const mountedTabs = new Set();
let usersUIInitialized = false;
let tenantsUIInitialized = false;
let tenantModalInitialized = false;
let tenantsSubtabsInitialized = false;
let activeTenantsView = getPersistedUIState(SERVER_UI_STATE_KEYS.TENANTS_VIEW, 'directory', VALID_TENANT_VIEWS);
let tenantBundlesUIInitialized = false;
const tenantBundlesState = {
    selectedTenantId: '',
    cache: new Map(),
    loading: false,
};
let addAgentUIInitialized = false;
let ssoAdminInitialized = false;
let logSubtabsInitialized = false;
let settingsSubtabsInitialized = false;
let activeSettingsView = getPersistedUIState(SERVER_UI_STATE_KEYS.SETTINGS_VIEW, 'server', VALID_SETTINGS_VIEWS);
let activeLogView = getPersistedUIState(SERVER_UI_STATE_KEYS.LOG_VIEW, 'system', VALID_LOG_VIEWS);
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

const METRICS_RANGE_WINDOWS = {
    '24h': 24 * 60 * 60 * 1000,
    '7d': 7 * 24 * 60 * 60 * 1000,
    '30d': 30 * 24 * 60 * 60 * 1000,
    '90d': 90 * 24 * 60 * 60 * 1000,
    '365d': 365 * 24 * 60 * 60 * 1000,
};
const METRICS_DEFAULT_RANGE = '24h';
const FLEET_SERIES_COLORS = ['#7dd3fc', '#f472b6', '#facc15', '#34d399'];
const metricsVM = {
    range: METRICS_DEFAULT_RANGE,
    summary: null,
    aggregated: null,
    loading: false,
    lastFetched: null,
    error: null,
};

const SERVER_SETTINGS_SCHEMA = [
    {
        section: 'server',
        title: 'Network & Proxy',
        description: 'Listener ports, binding address, and proxy awareness.',
        fields: [
            { key: 'http_port', label: 'HTTP Port', type: 'number', min: 1, max: 65535, required: true, helper: 'Plain HTTP listener used for health checks or reverse proxies.', configKey: 'server.http_port' },
            { key: 'https_port', label: 'HTTPS Port', type: 'number', min: 1, max: 65535, required: true, helper: 'Direct TLS listener when not running behind a reverse proxy.', configKey: 'server.https_port' },
            { key: 'bind_address', label: 'Bind Address', type: 'text', placeholder: '0.0.0.0', helper: 'Interface to bind when accepting connections.', configKey: 'server.bind_address', fullWidth: true },
            { key: 'behind_proxy', label: 'Behind Reverse Proxy', type: 'checkbox', helper: 'Trust X-Forwarded-* headers and skip automatic TLS.', configKey: 'server.behind_proxy' },
            { key: 'proxy_use_https', label: 'Proxy Uses HTTPS', type: 'checkbox', helper: 'When behind a proxy, assume incoming traffic was HTTPS.', configKey: 'server.proxy_use_https' },
            { key: 'auto_approve_agents', label: 'Auto-Approve Agents', type: 'checkbox', helper: 'Automatically trust new agents without manual approval.', configKey: 'server.auto_approve_agents' },
            { key: 'agent_timeout_minutes', label: 'Agent Timeout (minutes)', type: 'number', min: 1, helper: 'Time window before an agent is considered offline.', configKey: 'server.agent_timeout_minutes' }
        ]
    },
    {
        section: 'security',
        title: 'Authentication & Rate Limits',
        description: 'Brute-force protection for the built-in login experience.',
        fields: [
            { key: 'rate_limit_enabled', label: 'Rate Limiting Enabled', type: 'checkbox', helper: 'Reject login attempts after repeated failures.', configKey: 'security.rate_limit_enabled' },
            { key: 'rate_limit_max_attempts', label: 'Max Attempts', type: 'number', min: 1, helper: 'Failed logins allowed before triggering a block.', configKey: 'security.rate_limit_max_attempts' },
            { key: 'rate_limit_block_minutes', label: 'Block Duration (minutes)', type: 'number', min: 1, helper: 'How long to block an IP/user after exceeding attempts.', configKey: 'security.rate_limit_block_minutes' },
            { key: 'rate_limit_window_minutes', label: 'Window (minutes)', type: 'number', min: 1, helper: 'Rolling window for counting failed attempts.', configKey: 'security.rate_limit_window_minutes' }
        ]
    },
    {
        section: 'tls',
        title: 'TLS & Certificates',
        description: 'Choose how HTTPS certificates are provisioned.',
        fields: [
            {
                key: 'mode',
                label: 'TLS Mode',
                type: 'select',
                required: true,
                options: [
                    { value: 'self-signed', label: 'Self-signed (default)' },
                    { value: 'custom', label: 'Custom certificate' },
                    { value: 'letsencrypt', label: 'Let\'s Encrypt (automatic)' }
                ],
                helper: 'Self-signed is easiest. Custom and Let\'s Encrypt require additional metadata.',
                configKey: 'tls.mode'
            },
            { key: 'domain', label: 'Primary Domain', type: 'text', placeholder: 'pm.yourdomain.com', helper: 'Common Name used for certificates and redirects.', configKey: 'tls.domain', required: true },
            { key: 'cert_path', label: 'Certificate Path', type: 'text', placeholder: 'C:/printmaster/server.crt', helper: 'Path to PEM certificate when using custom TLS.', configKey: 'tls.cert_path', fullWidth: true },
            { key: 'key_path', label: 'Key Path', type: 'text', placeholder: 'C:/printmaster/server.key', helper: 'Private key for custom TLS certificates.', configKey: 'tls.key_path', fullWidth: true },
            { key: 'letsencrypt_domain', label: 'Let\'s Encrypt Domain', type: 'text', placeholder: 'pm.yourdomain.com', helper: 'FQDN requested from Let\'s Encrypt.', configKey: 'tls.letsencrypt.domain' },
            { key: 'letsencrypt_email', label: 'Let\'s Encrypt Email', type: 'text', placeholder: 'ops@yourdomain.com', helper: 'Administrative contact for ACME registration.', configKey: 'tls.letsencrypt.email' },
            { key: 'letsencrypt_cache_dir', label: 'Let\'s Encrypt Cache Dir', type: 'text', placeholder: 'letsencrypt-cache', helper: 'Directory for cached ACME assets.', configKey: 'tls.letsencrypt.cache_dir' },
            { key: 'letsencrypt_accept_tos', label: 'Accept Let\'s Encrypt Terms', type: 'checkbox', helper: 'Required before automatic certificate issuance.', configKey: 'tls.letsencrypt.accept_tos' }
        ]
    },
    {
        section: 'logging',
        title: 'Logging Level',
        description: 'Control verbosity for new log entries.',
        fields: [
            {
                key: 'level',
                label: 'Log Level',
                type: 'select',
                required: true,
                options: [
                    { value: 'ERROR', label: 'ERROR' },
                    { value: 'WARN', label: 'WARN' },
                    { value: 'INFO', label: 'INFO' },
                    { value: 'DEBUG', label: 'DEBUG' },
                    { value: 'TRACE', label: 'TRACE' }
                ],
                helper: 'Changes apply immediately without restarting.',
                configKey: 'logging.level'
            }
        ]
    },
    {
        section: 'releases',
        title: 'Release Intake',
        description: 'Control how many GitHub releases are cached locally for auto-update and packaging.',
        fields: [
            { key: 'max_releases', label: 'Max Releases per Component', type: 'number', min: 1, required: true, helper: 'Upper bound of releases ingested for each component on every sync.', configKey: 'releases.max_releases' },
            { key: 'poll_interval_minutes', label: 'Sync Interval (minutes)', type: 'number', min: 15, required: true, helper: 'How often the server polls GitHub for new releases.', configKey: 'releases.poll_interval_minutes' }
        ]
    },
    {
        section: 'self_update',
        title: 'Server Self-Update',
        description: 'Adjust how the server checks for and stages new versions.',
        fields: [
            { key: 'enabled', label: 'Enable Self-Update', type: 'checkbox', helper: 'Allow the server to download and stage signed updates automatically.', configKey: 'server.self_update_enabled' },
            { key: 'channel', label: 'Update Channel', type: 'text', placeholder: 'stable', required: true, helper: 'Release channel to follow (e.g. stable, beta).', configKey: 'self_update.channel' },
            { key: 'max_artifacts', label: 'Max Cached Artifacts', type: 'number', min: 1, required: true, helper: 'Number of newest artifacts evaluated when picking an update candidate.', configKey: 'self_update.max_artifacts' },
            { key: 'check_interval_minutes', label: 'Check Interval (minutes)', type: 'number', min: 30, required: true, helper: 'Frequency of automatic self-update checks.', configKey: 'self_update.check_interval_minutes' }
        ]
    },
    {
        section: 'smtp',
        title: 'SMTP Notifications',
        description: 'Optional email settings for alerts and reports.',
        fields: [
            { key: 'enabled', label: 'Enable SMTP', type: 'checkbox', helper: 'Toggle email delivery for alerting.', configKey: 'smtp.enabled' },
            { key: 'host', label: 'SMTP Host', type: 'text', placeholder: 'smtp.office365.com', helper: 'Hostname or IP of your SMTP relay.', configKey: 'smtp.host' },
            { key: 'port', label: 'SMTP Port', type: 'number', min: 1, max: 65535, helper: 'Port used to connect to your SMTP server.', configKey: 'smtp.port' },
            { key: 'user', label: 'SMTP Username', type: 'text', helper: 'Leave blank if your relay allows anonymous auth.', configKey: 'smtp.user' },
            { key: 'pass', label: 'SMTP Password', type: 'password', placeholder: 'Leave blank to keep existing secret', helper: 'Value is only stored if you provide a new password.', configKey: 'smtp.pass' },
            { key: 'from', label: 'From Address', type: 'text', placeholder: 'printmaster@yourdomain.com', helper: 'Default sender for outbound email.', configKey: 'smtp.from' }
        ]
    }
];

const serverSettingsVM = {
    data: null,
    original: null,
    lockedKeys: new Set(),
    loading: false,
    saving: false,
    dirty: false,
    restartRequired: false,
    statusMessage: '',
    statusTone: 'muted',
    lastError: null,
};

function getRBAC() {
    if (typeof window === 'undefined') {
        return null;
    }
    return window.__pm_rbac || null;
}

function getRolePriorityMap() {
    const rbac = getRBAC();
    return (rbac && rbac.ROLE_PRIORITY) ? rbac.ROLE_PRIORITY : DEFAULT_ROLE_PRIORITY;
}

function normalizeRole(role) {
    const rbac = getRBAC();
    if (rbac && typeof rbac.normalizeRole === 'function') {
        return rbac.normalizeRole(role);
    }
    return (role || '').toString().toLowerCase();
}

function userHasRole(minRole) {
    if (!currentUser) return false;
    const rbac = getRBAC();
    if (rbac && typeof rbac.userHasRequiredRole === 'function') {
        return rbac.userHasRequiredRole(currentUser.role, minRole);
    }
    const priorities = getRolePriorityMap();
    const current = priorities[normalizeRole(currentUser.role)] || 0;
    const required = priorities[normalizeRole(minRole)] || 0;
    return current >= required;
}

function userCan(action) {
    if (!currentUser || !action) {
        return false;
    }
    const rbac = getRBAC();
    if (rbac && typeof rbac.canPerformAction === 'function') {
        return rbac.canPerformAction(currentUser.role, action);
    }
    if (rbac && rbac.ACTION_MIN_ROLE && rbac.ACTION_MIN_ROLE[action]) {
        return userHasRole(rbac.ACTION_MIN_ROLE[action]);
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
        restorePreferredTab();

        // Check config status and show warning if needed
        checkConfigStatus();

        // Load initial data
        loadServerStatus();
        loadAgents();
        initMetricsRangeControls();
        loadMetrics();
        window._metricsInterval = setInterval(() => {
            if (isMetricsTabActive()) {
                loadMetrics(true);
            }
        }, 60000);

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
    persistUIState(SERVER_UI_STATE_KEYS.LOG_VIEW, desired);

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

function initSettingsSubTabs() {
    if (settingsSubtabsInitialized) {
        return;
    }
    settingsSubtabsInitialized = true;

    document.querySelectorAll('.settings-subtab').forEach(btn => {
        btn.addEventListener('click', () => {
            const target = btn.dataset.settingsview || 'fleet';
            switchSettingsView(target);
        });
    });
}

function switchSettingsView(view, force = false) {
    // valid views: server | fleet | sso | updates
    let normalized = 'server';
    if (view === 'fleet') normalized = 'fleet';
    else if (view === 'sso') normalized = 'sso';
    else if (view === 'updates') normalized = 'updates';
    const previous = activeSettingsView;
    activeSettingsView = normalized;
    persistUIState(SERVER_UI_STATE_KEYS.SETTINGS_VIEW, normalized);

    document.querySelectorAll('.settings-subtab').forEach(btn => {
        const target = btn.dataset.settingsview || 'fleet';
        btn.classList.toggle('active', target === normalized);
    });

    document.querySelectorAll('[data-settingsview-panel]').forEach(panel => {
        const target = panel.dataset.settingsviewPanel || 'fleet';
        panel.classList.toggle('hidden', target !== normalized);
    });

    if (force || previous !== normalized) {
        ensureSettingsViewReady(normalized);
    }
}

function ensureSettingsViewReady(view) {
    if (view === 'sso') {
        // Initialize SSO admin panel lazily
        initSSOAdmin();
        refreshSSOProviders();
        return;
    }
    if (view === 'server') {
        // Placeholder: fetch and render server settings into server_settings_container
        loadServerSettings();
        return;
    }
    if (view === 'updates') {
        loadSelfUpdateRuns();
        return;
    }
    // fleet
    initSettingsUI();
}

async function loadServerSettings(forceRefresh = false) {
    const container = document.getElementById('server_settings_container');
    if (!container) return;
    if (serverSettingsVM.loading && !forceRefresh) {
        return;
    }
    serverSettingsVM.loading = true;
    container.innerHTML = '<div style="color:var(--muted);">Loading server settings…</div>';
    try {
        const [settingsResp, sourcesResp] = await Promise.all([
            fetchJSON('/api/v1/server/settings'),
            fetchJSON('/api/v1/server/settings/sources').catch(err => {
                window.__pm_shared.warn('Failed to load server settings lock metadata', err);
                return null;
            })
        ]);
        const normalized = normalizeServerSettings(settingsResp || {});
        serverSettingsVM.original = cloneServerSettingsData(normalized);
        serverSettingsVM.data = cloneServerSettingsData(normalized);
        const lockedKeys = (sourcesResp && Array.isArray(sourcesResp.locked_keys)) ? sourcesResp.locked_keys : [];
        serverSettingsVM.lockedKeys = new Set(lockedKeys);
        serverSettingsVM.dirty = false;
        serverSettingsVM.restartRequired = false;
        serverSettingsVM.lastError = null;
        serverSettingsVM.statusMessage = 'Fetched latest settings from server.';
        serverSettingsVM.statusTone = 'muted';
        renderServerSettingsForm();
    } catch (err) {
        serverSettingsVM.lastError = err;
        const message = err && err.message ? err.message : err;
        container.innerHTML = `<div style="color:var(--danger);">Failed to load server settings: ${message}</div>`;
        window.__pm_shared.error('Failed to load server settings', err);
    } finally {
        serverSettingsVM.loading = false;
    }
}

function normalizeServerSettings(raw) {
    const safeStr = (val) => (val === null || val === undefined) ? '' : String(val);
    const safeBool = (val) => Boolean(val);
    const scoped = raw || {};
    const serverSection = scoped.server || {};
    const securitySection = scoped.security || {};
    const tlsSection = scoped.tls || {};
    const loggingSection = scoped.logging || {};
    const smtpSection = scoped.smtp || {};
    const databaseSection = scoped.database || {};
    const releasesSection = scoped.releases || {};
    const selfUpdateSection = scoped.self_update || {};
    return {
        meta: {
            version: safeStr(scoped.version || 'unknown'),
            config_source: safeStr(scoped.config_source || 'config.toml'),
            using_defaults: Boolean(scoped.using_defaults),
            tenancy_enabled: Boolean(scoped.tenancy_enabled),
            database_path: safeStr(databaseSection.path || ''),
        },
        server: {
            http_port: safeStr(serverSection.http_port),
            https_port: safeStr(serverSection.https_port),
            bind_address: safeStr(serverSection.bind_address || ''),
            behind_proxy: safeBool(serverSection.behind_proxy),
            proxy_use_https: safeBool(serverSection.proxy_use_https),
            auto_approve_agents: safeBool(serverSection.auto_approve_agents),
            agent_timeout_minutes: safeStr(serverSection.agent_timeout_minutes),
        },
        security: {
            rate_limit_enabled: safeBool(securitySection.rate_limit_enabled),
            rate_limit_max_attempts: safeStr(securitySection.rate_limit_max_attempts),
            rate_limit_block_minutes: safeStr(securitySection.rate_limit_block_minutes),
            rate_limit_window_minutes: safeStr(securitySection.rate_limit_window_minutes),
        },
        tls: {
            mode: safeStr(tlsSection.mode || 'self-signed') || 'self-signed',
            domain: safeStr(tlsSection.domain || ''),
            cert_path: safeStr(tlsSection.cert_path || ''),
            key_path: safeStr(tlsSection.key_path || ''),
            letsencrypt_domain: safeStr(tlsSection.letsencrypt_domain || ''),
            letsencrypt_email: safeStr(tlsSection.letsencrypt_email || ''),
            letsencrypt_cache_dir: safeStr(tlsSection.letsencrypt_cache_dir || ''),
            letsencrypt_accept_tos: safeBool(tlsSection.letsencrypt_accept_tos),
        },
        logging: {
            level: safeStr(loggingSection.level || 'INFO') || 'INFO',
        },
        smtp: {
            enabled: safeBool(smtpSection.enabled),
            host: safeStr(smtpSection.host || ''),
            port: safeStr(smtpSection.port),
            user: safeStr(smtpSection.user || ''),
            pass: '',
            from: safeStr(smtpSection.from || ''),
        },
        releases: {
            max_releases: safeStr(releasesSection.max_releases),
            poll_interval_minutes: safeStr(releasesSection.poll_interval_minutes),
        },
        self_update: {
            enabled: safeBool(selfUpdateSection.enabled),
            channel: safeStr(selfUpdateSection.channel || 'stable') || 'stable',
            max_artifacts: safeStr(selfUpdateSection.max_artifacts),
            check_interval_minutes: safeStr(selfUpdateSection.check_interval_minutes),
        },
    };
}

function cloneServerSettingsData(data) {
    return JSON.parse(JSON.stringify(data || {}));
}

function renderServerSettingsForm() {
    const container = document.getElementById('server_settings_container');
    if (!container) return;
    if (!serverSettingsVM.data) {
        container.innerHTML = '<div style="color:var(--muted);">Server settings are not available.</div>';
        return;
    }
    const sectionsHtml = SERVER_SETTINGS_SCHEMA.map(section => renderServerSettingsSection(section)).join('');
    const metaCards = renderServerSettingsInfoCards();
    const lockSummary = renderServerSettingsLockSummary();
    const restartBanner = `<div id="server_settings_restart_banner" style="display:${(serverSettingsVM.restartRequired && !serverSettingsVM.dirty) ? 'flex' : 'none'};align-items:center;gap:8px;padding:8px 12px;border-radius:6px;background:rgba(255,153,0,0.15);color:var(--warn);font-size:13px;">
        <span style="font-weight:600;">Restart required</span>
        <span>Recycle the PrintMaster server service to apply TLS or network changes.</span>
    </div>`;
    container.innerHTML = `
        <div style="display:flex;flex-wrap:wrap;gap:12px;margin-bottom:16px;">
            ${metaCards}
        </div>
        ${lockSummary}
        ${restartBanner}
        ${sectionsHtml}
        <div style="display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:12px;margin-top:24px;padding:12px;border:1px solid var(--border);border-radius:10px;background:rgba(255,255,255,0.02);">
            <div id="server_settings_status" style="font-size:13px;color:var(--muted);"></div>
            <div style="display:flex;gap:10px;">
                <button id="server_settings_discard_btn" class="btn btn-secondary" type="button">Discard</button>
                <button id="server_settings_save_btn" class="btn btn-primary" type="button">Save changes</button>
            </div>
        </div>
    `;
    bindServerSettingsInputs(container);
    syncServerSettingsActionState();
}

function renderServerSettingsInfoCards() {
    const meta = (serverSettingsVM.data && serverSettingsVM.data.meta) || {};
    const cards = [
        { label: 'Version', value: meta.version || 'unknown' },
        { label: 'Config Source', value: meta.config_source || 'config.toml' },
        { label: 'Tenancy', value: meta.tenancy_enabled ? 'Enabled' : 'Disabled' },
        { label: 'Database Path', value: meta.database_path || '(default)' },
    ];
    return cards.map(card => `
        <div style="flex:1;min-width:180px;border:1px solid var(--border);border-radius:10px;padding:10px 12px;">
            <div style="font-size:12px;color:var(--muted);text-transform:uppercase;letter-spacing:0.4px;">${card.label}</div>
            <div style="font-size:15px;margin-top:4px;font-family:var(--font-code,monospace);">${escapeHtml(card.value)}</div>
        </div>
    `).join('');
}

function renderServerSettingsLockSummary() {
    if (!serverSettingsVM.lockedKeys || serverSettingsVM.lockedKeys.size === 0) {
        return '';
    }
    const keys = Array.from(serverSettingsVM.lockedKeys).sort();
    const preview = keys.slice(0, 4).map(key => `<code style="background:rgba(255,255,255,0.05);padding:2px 6px;border-radius:4px;">${escapeHtml(key)}</code>`).join(' ');
    const remainder = keys.length > 4 ? ` +${keys.length - 4} more` : '';
    return `
        <div style="border:1px solid var(--border);border-radius:10px;padding:12px;margin-bottom:16px;background:rgba(255,255,255,0.02);font-size:13px;">
            <strong>Managed keys:</strong> ${preview}${remainder}
            <div style="font-size:12px;color:var(--muted);margin-top:4px;">These values come from environment overrides and cannot be edited here.</div>
        </div>
    `;
}

function renderServerSettingsSection(sectionDef) {
    const fields = sectionDef.fields || [];
    const fieldGrid = fields.map(field => renderServerSettingsField(sectionDef.section, field)).join('');
    const title = escapeHtml(sectionDef.title || '');
    const description = sectionDef.description ? escapeHtml(sectionDef.description) : '';
    return `
        <div class="panel" style="border:1px solid var(--border);border-radius:10px;padding:16px;margin-bottom:20px;">
            <div style="display:flex;flex-direction:column;gap:4px;margin-bottom:12px;">
                <div style="font-size:16px;font-weight:600;">${title}</div>
                <div style="font-size:13px;color:var(--muted);">${description}</div>
            </div>
            <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(240px,1fr));gap:14px;">
                ${fieldGrid}
            </div>
        </div>
    `;
}

function renderServerSettingsField(sectionKey, field) {
    const sectionData = (serverSettingsVM.data && serverSettingsVM.data[sectionKey]) || {};
    let value = sectionData[field.key];
    if (value === null || value === undefined) {
        value = (field.type === 'checkbox') ? false : '';
    }
    const inputId = `server_setting_${sectionKey}_${field.key}`;
    const isLocked = field.configKey && serverSettingsVM.lockedKeys.has(field.configKey);
    const disabledAttr = isLocked ? 'disabled' : '';
    const lockBadge = isLocked ? '<span style="font-size:11px;color:var(--warn);background:rgba(255,153,0,0.15);padding:2px 6px;border-radius:4px;">Env managed</span>' : '';
    const labelText = escapeHtml(field.label || '');
    let control = '';
    if (field.type === 'checkbox') {
        const checked = value ? 'checked' : '';
        control = `
            <input data-settings-input="true" data-section="${sectionKey}" data-key="${field.key}" id="${inputId}" type="checkbox" ${checked} ${disabledAttr} />
        `;
    } else if (field.type === 'select') {
        const options = (field.options || []).map(opt => `<option value="${escapeHtml(opt.value)}" ${opt.value === value ? 'selected' : ''}>${escapeHtml(opt.label)}</option>`).join('');
        control = `
            <select data-settings-input="true" data-section="${sectionKey}" data-key="${field.key}" id="${inputId}" ${disabledAttr}>
                ${options}
            </select>
        `;
    } else {
        const typeAttr = field.type === 'password' ? 'password' : (field.type === 'number' ? 'number' : 'text');
        const minAttr = (field.type === 'number' && field.min !== undefined) ? `min="${field.min}"` : '';
        const maxAttr = (field.type === 'number' && field.max !== undefined) ? `max="${field.max}"` : '';
        const inputMode = field.type === 'number' ? 'inputmode="numeric" pattern="[0-9]*"' : '';
        control = `
            <input data-settings-input="true" data-section="${sectionKey}" data-key="${field.key}" id="${inputId}" type="${typeAttr}" ${minAttr} ${maxAttr} ${inputMode} ${disabledAttr}
                value="${escapeHtml(value)}" placeholder="${field.placeholder ? escapeHtml(field.placeholder) : ''}" />
        `;
    }
    const helper = field.helper ? `<div style="font-size:12px;color:var(--muted);">${escapeHtml(field.helper)}</div>` : '';
    return `
        <div style="display:flex;flex-direction:column;gap:6px;${field.fullWidth ? 'grid-column:1 / -1;' : ''}">
            <div style="display:flex;align-items:center;gap:8px;font-weight:600;">
                <label for="${inputId}">${labelText}</label>
                ${lockBadge}
                ${field.required ? '<span style="font-size:11px;color:var(--muted);">*</span>' : ''}
            </div>
            ${control}
            ${helper}
        </div>
    `;
}

function bindServerSettingsInputs(container) {
    if (!container) return;
    container.querySelectorAll('[data-settings-input="true"]').forEach(input => {
        const section = input.dataset.section;
        const key = input.dataset.key;
        if (!section || !key) {
            return;
        }
        if (input.type === 'checkbox') {
            input.addEventListener('change', () => handleServerSettingsInput(section, key, input.checked));
        } else if (input.tagName === 'SELECT') {
            input.addEventListener('change', () => handleServerSettingsInput(section, key, input.value));
        } else {
            input.addEventListener('input', () => handleServerSettingsInput(section, key, input.value));
        }
    });
    const saveBtn = container.querySelector('#server_settings_save_btn');
    const discardBtn = container.querySelector('#server_settings_discard_btn');
    if (saveBtn) {
        saveBtn.addEventListener('click', (e) => {
            e.preventDefault();
            saveServerSettings();
        });
    }
    if (discardBtn) {
        discardBtn.addEventListener('click', (e) => {
            e.preventDefault();
            discardServerSettingsChanges();
        });
    }
}

function handleServerSettingsInput(section, key, value) {
    if (!serverSettingsVM.data || !serverSettingsVM.data[section]) {
        return;
    }
    serverSettingsVM.data[section][key] = value;
    serverSettingsVM.dirty = true;
    serverSettingsVM.statusMessage = 'Unsaved changes';
    serverSettingsVM.statusTone = 'warn';
    serverSettingsVM.lastError = null;
    syncServerSettingsActionState();
}

function syncServerSettingsActionState() {
    const saveBtn = document.getElementById('server_settings_save_btn');
    const discardBtn = document.getElementById('server_settings_discard_btn');
    const statusEl = document.getElementById('server_settings_status');
    const restartBanner = document.getElementById('server_settings_restart_banner');
    if (saveBtn) {
        saveBtn.disabled = serverSettingsVM.saving || !serverSettingsVM.dirty;
    }
    if (discardBtn) {
        discardBtn.disabled = serverSettingsVM.saving || !serverSettingsVM.dirty;
    }
    if (restartBanner) {
        restartBanner.style.display = (serverSettingsVM.restartRequired && !serverSettingsVM.dirty) ? 'flex' : 'none';
    }
    if (statusEl) {
        let message = serverSettingsVM.statusMessage || 'All changes saved.';
        let color = 'var(--muted)';
        if (serverSettingsVM.saving) {
            message = 'Saving changes…';
            color = 'var(--highlight)';
        } else if (serverSettingsVM.lastError) {
            message = 'Save failed. Check logs for details.';
            color = 'var(--danger)';
        } else if (serverSettingsVM.dirty) {
            color = 'var(--warn)';
        } else if (serverSettingsVM.restartRequired) {
            color = 'var(--warn)';
            message = 'Changes saved. Restart required to apply network/TLS settings.';
        }
        statusEl.textContent = message;
        statusEl.style.color = color;
    }
}

function validateServerSettingsData() {
    if (!serverSettingsVM.data) {
        return { ok: false, message: 'Settings payload not ready.' };
    }
    for (const section of SERVER_SETTINGS_SCHEMA) {
        const dataSection = serverSettingsVM.data[section.section] || {};
        for (const field of section.fields || []) {
            const value = dataSection[field.key];
            if (field.required && field.type !== 'checkbox') {
                if (value === undefined || value === null || String(value).trim() === '') {
                    return { ok: false, message: `${field.label} is required.` };
                }
            }
            if (field.type === 'number' && value !== '' && value !== undefined) {
                if (isNaN(Number(value))) {
                    return { ok: false, message: `${field.label} must be a number.` };
                }
            }
        }
    }
    const tls = serverSettingsVM.data.tls || {};
    if (tls.mode === 'custom') {
        if (!tls.cert_path || !tls.key_path) {
            return { ok: false, message: 'Custom TLS mode requires both certificate and key paths.' };
        }
    }
    if (tls.mode === 'letsencrypt') {
        if (!tls.letsencrypt_domain || !tls.letsencrypt_email) {
            return { ok: false, message: 'Let\'s Encrypt mode requires domain and email.' };
        }
        if (!tls.letsencrypt_accept_tos) {
            return { ok: false, message: 'You must accept the Let\'s Encrypt terms of service.' };
        }
    }
    const releases = serverSettingsVM.data.releases || {};
    if (!releases.max_releases || isNaN(Number(releases.max_releases)) || Number(releases.max_releases) <= 0) {
        return { ok: false, message: 'Release intake max releases must be a positive number.' };
    }
    if (!releases.poll_interval_minutes || isNaN(Number(releases.poll_interval_minutes)) || Number(releases.poll_interval_minutes) <= 0) {
        return { ok: false, message: 'Release sync interval must be a positive number of minutes.' };
    }
    const selfUpdate = serverSettingsVM.data.self_update || {};
    if (!selfUpdate.channel || String(selfUpdate.channel).trim() === '') {
        return { ok: false, message: 'Self-update channel cannot be empty.' };
    }
    if (!selfUpdate.max_artifacts || isNaN(Number(selfUpdate.max_artifacts)) || Number(selfUpdate.max_artifacts) <= 0) {
        return { ok: false, message: 'Self-update max artifacts must be a positive number.' };
    }
    if (!selfUpdate.check_interval_minutes || isNaN(Number(selfUpdate.check_interval_minutes)) || Number(selfUpdate.check_interval_minutes) <= 0) {
        return { ok: false, message: 'Self-update check interval must be a positive number of minutes.' };
    }
    return { ok: true };
}

function buildServerSettingsPayload() {
    if (!serverSettingsVM.data) {
        return null;
    }
    const data = serverSettingsVM.data;
    const parseNumber = (val) => {
        if (val === undefined || val === null || String(val).trim() === '') {
            return undefined;
        }
        const parsed = parseInt(val, 10);
        return Number.isNaN(parsed) ? undefined : parsed;
    };
    const pickString = (val) => {
        if (val === undefined || val === null) {
            return undefined;
        }
        return String(val);
    };
    const payload = {
        server: {
            http_port: parseNumber(data.server.http_port),
            https_port: parseNumber(data.server.https_port),
            bind_address: pickString(data.server.bind_address),
            behind_proxy: Boolean(data.server.behind_proxy),
            proxy_use_https: Boolean(data.server.proxy_use_https),
            auto_approve_agents: Boolean(data.server.auto_approve_agents),
            agent_timeout_minutes: parseNumber(data.server.agent_timeout_minutes),
        },
        security: {
            rate_limit_enabled: Boolean(data.security.rate_limit_enabled),
            rate_limit_max_attempts: parseNumber(data.security.rate_limit_max_attempts),
            rate_limit_block_minutes: parseNumber(data.security.rate_limit_block_minutes),
            rate_limit_window_minutes: parseNumber(data.security.rate_limit_window_minutes),
        },
        tls: {
            mode: pickString(data.tls.mode) || 'self-signed',
            domain: pickString(data.tls.domain) || '',
            cert_path: pickString(data.tls.cert_path),
            key_path: pickString(data.tls.key_path),
        },
        logging: {
            level: data.logging.level || 'INFO',
        },
        smtp: {
            enabled: Boolean(data.smtp.enabled),
            host: pickString(data.smtp.host) || '',
            port: parseNumber(data.smtp.port),
            user: pickString(data.smtp.user) || '',
            from: pickString(data.smtp.from) || '',
        },
        releases: {
            max_releases: parseNumber(data.releases.max_releases),
            poll_interval_minutes: parseNumber(data.releases.poll_interval_minutes),
        },
        self_update: {
            enabled: Boolean(data.self_update.enabled),
            channel: pickString(data.self_update.channel),
            max_artifacts: parseNumber(data.self_update.max_artifacts),
            check_interval_minutes: parseNumber(data.self_update.check_interval_minutes),
        },
    };
    if (payload.tls.mode === 'letsencrypt') {
        payload.tls.letsencrypt = {
            domain: pickString(data.tls.letsencrypt_domain) || '',
            email: pickString(data.tls.letsencrypt_email) || '',
            cache_dir: pickString(data.tls.letsencrypt_cache_dir),
            accept_tos: Boolean(data.tls.letsencrypt_accept_tos),
        };
    }
    if (data.smtp.pass && data.smtp.pass.trim() !== '') {
        payload.smtp.pass = data.smtp.pass;
    }
    if (payload.smtp.port === undefined) {
        delete payload.smtp.port;
    }
    Object.keys(payload.releases).forEach(key => {
        if (payload.releases[key] === undefined) {
            delete payload.releases[key];
        }
    });
    Object.keys(payload.self_update).forEach(key => {
        if (payload.self_update[key] === undefined) {
            delete payload.self_update[key];
        }
    });
    Object.keys(payload.server).forEach(key => {
        if (payload.server[key] === undefined) {
            delete payload.server[key];
        }
    });
    Object.keys(payload.security).forEach(key => {
        if (payload.security[key] === undefined) {
            delete payload.security[key];
        }
    });
    Object.keys(payload.tls).forEach(key => {
        if (payload.tls[key] === undefined) {
            delete payload.tls[key];
        }
    });
    if (payload.tls.letsencrypt) {
        Object.keys(payload.tls.letsencrypt).forEach(key => {
            if (payload.tls.letsencrypt[key] === undefined) {
                delete payload.tls.letsencrypt[key];
            }
        });
    }
    return payload;
}

async function saveServerSettings() {
    if (!serverSettingsVM.data || serverSettingsVM.saving || !serverSettingsVM.dirty) {
        return;
    }
    const validation = validateServerSettingsData();
    if (!validation.ok) {
        window.__pm_shared.showToast(validation.message, 'warn');
        serverSettingsVM.statusMessage = validation.message;
        serverSettingsVM.statusTone = 'warn';
        syncServerSettingsActionState();
        return;
    }
    const payload = buildServerSettingsPayload();
    if (!payload) {
        window.__pm_shared.showToast('Settings payload was empty.', 'warn');
        return;
    }
    serverSettingsVM.saving = true;
    serverSettingsVM.statusMessage = 'Saving changes…';
    serverSettingsVM.statusTone = 'info';
    serverSettingsVM.lastError = null;
    syncServerSettingsActionState();
    try {
        const resp = await fetchJSON('/api/v1/server/settings', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
        });
        const nextState = normalizeServerSettings((resp && resp.settings) || {});
        serverSettingsVM.original = cloneServerSettingsData(nextState);
        serverSettingsVM.data = cloneServerSettingsData(nextState);
        serverSettingsVM.dirty = false;
        serverSettingsVM.restartRequired = Boolean(resp && resp.restart_required);
        serverSettingsVM.statusMessage = serverSettingsVM.restartRequired ? 'Saved. Restart required for some settings.' : 'Settings saved successfully.';
        serverSettingsVM.statusTone = serverSettingsVM.restartRequired ? 'warn' : 'success';
        window.__pm_shared.showToast('Server settings updated.', 'success');
        renderServerSettingsForm();
    } catch (err) {
        serverSettingsVM.lastError = err;
        serverSettingsVM.statusMessage = err && err.message ? err.message : 'Failed to save settings.';
        serverSettingsVM.statusTone = 'error';
        window.__pm_shared.error('Saving server settings failed', err);
        syncServerSettingsActionState();
        return;
    } finally {
        serverSettingsVM.saving = false;
        syncServerSettingsActionState();
    }
}

function discardServerSettingsChanges() {
    if (!serverSettingsVM.dirty || !serverSettingsVM.original) {
        return;
    }
    serverSettingsVM.data = cloneServerSettingsData(serverSettingsVM.original);
    serverSettingsVM.dirty = false;
    serverSettingsVM.statusMessage = 'Changes discarded.';
    serverSettingsVM.statusTone = 'muted';
    renderServerSettingsForm();
    window.__pm_shared.showToast('Server settings reverted.', 'info');
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
        initSettingsSubTabs();
        switchSettingsView(activeSettingsView, true);
    } else if (targetTab === 'logs') {
        initLogSubTabs();
        switchLogView(activeLogView || 'system');
    } else if (targetTab === 'tenants') {
        loadTenants();
    } else if (targetTab === 'users') {
        loadUsers();
    }

    if (targetTab && isTabSelectable(targetTab)) {
        persistUIState(SERVER_UI_STATE_KEYS.ACTIVE_TAB, targetTab);
    }
}

function isTabSelectable(targetTab) {
    if (!targetTab) {
        return false;
    }
    const panel = document.querySelector(`[data-tab="${targetTab}"]`);
    if (!panel) {
        return false;
    }
    const buttons = Array.from(document.querySelectorAll(`.tab[data-target="${targetTab}"]`));
    if (buttons.length === 0) {
        return true;
    }
    return buttons.some(btn => btn.offsetParent !== null);
}

function restorePreferredTab() {
    const stored = getPersistedUIState(SERVER_UI_STATE_KEYS.ACTIVE_TAB, null);
    if (!stored) {
        return;
    }
    if (!isTabSelectable(stored)) {
        return;
    }
    switchTab(stored);
}

// ---------------------------------------------------------------------------
// Self-Update Runs Panel
// ---------------------------------------------------------------------------
let selfUpdateRunsInitialized = false;
let selfUpdateCheckPending = false;
let releasesSyncPending = false;

async function loadSelfUpdateRuns() {
    const statusCard = document.getElementById('selfupdate_status_card');
    const runsContainer = document.getElementById('selfupdate_runs_container');

    if (!selfUpdateRunsInitialized) {
        selfUpdateRunsInitialized = true;
        const refreshBtn = document.getElementById('selfupdate_refresh_btn');
        if (refreshBtn) {
            refreshBtn.addEventListener('click', () => loadSelfUpdateRuns());
        }
        const syncBtn = document.getElementById('releases_sync_btn');
        if (syncBtn) {
            syncBtn.addEventListener('click', () => triggerReleasesSync());
        }
    }

    // Load status, runs, and artifacts in parallel
    try {
        const [statusResp, runsResp, artifactsResp] = await Promise.all([
            fetchJSON('/api/v1/selfupdate/status').catch(err => ({ error: err.message || err })),
            fetchJSON('/api/v1/selfupdate/runs').catch(err => ({ error: err.message || err })),
            fetchJSON('/api/v1/releases/artifacts').catch(err => ({ error: err.message || err }))
        ]);

        // Render status card
        if (statusCard) {
            renderSelfUpdateStatus(statusCard, statusResp);
        }

        // Render runs table
        if (runsContainer) {
            if (runsResp.error) {
                runsContainer.innerHTML = `<div style="color:var(--danger);">Failed to load history: ${runsResp.error}</div>`;
            } else {
                const runs = Array.isArray(runsResp.runs) ? runsResp.runs : [];
                renderSelfUpdateRuns(runsContainer, runs);
            }
        }

        // Render artifacts
        const artifactsContainer = document.getElementById('releases_artifacts_container');
        if (artifactsContainer) {
            if (artifactsResp.error) {
                artifactsContainer.innerHTML = `<div style="color:var(--danger);">Failed to load artifacts: ${artifactsResp.error}</div>`;
            } else {
                const artifacts = Array.isArray(artifactsResp.artifacts) ? artifactsResp.artifacts : [];
                renderReleaseArtifacts(artifactsContainer, artifacts);
            }
        }
    } catch (err) {
        const message = err && err.message ? err.message : err;
        if (statusCard) {
            statusCard.innerHTML = `<div style="color:var(--danger);">Failed to load status: ${message}</div>`;
        }
        if (runsContainer) {
            runsContainer.innerHTML = `<div style="color:var(--danger);">Failed to load history: ${message}</div>`;
        }
    }
}

function renderSelfUpdateStatus(container, status) {
    if (!status || status.error) {
        container.innerHTML = `<div style="color:var(--danger);">Failed to load status: ${status?.error || 'Unknown error'}</div>`;
        return;
    }

    const enabledBadge = status.enabled
        ? '<span style="display:inline-block;padding:2px 8px;border-radius:4px;font-size:11px;font-weight:600;background:var(--success)20;color:var(--success);">Enabled</span>'
        : '<span style="display:inline-block;padding:2px 8px;border-radius:4px;font-size:11px;font-weight:600;background:var(--danger)20;color:var(--danger);">Disabled</span>';

    const checkBtn = status.enabled
        ? `<button id="selfupdate_check_btn" class="modal-button modal-button-primary" style="padding:6px 12px;">Check for Updates</button>`
        : '';

    container.innerHTML = `
        <div style="display:flex;flex-wrap:wrap;gap:16px;justify-content:space-between;align-items:flex-start;">
            <div style="display:flex;flex-direction:column;gap:8px;">
                <div style="display:flex;align-items:center;gap:8px;">
                    <span style="font-weight:600;">Auto-Update:</span>
                    ${enabledBadge}
                </div>
                ${status.disabled_reason ? `<div style="color:var(--muted);font-size:12px;">Reason: ${status.disabled_reason}</div>` : ''}
                <div style="display:flex;flex-wrap:wrap;gap:16px;font-size:13px;color:var(--muted);">
                    <span><strong>Version:</strong> ${status.current_version || 'Unknown'}</span>
                    <span><strong>Channel:</strong> ${status.channel || 'stable'}</span>
                    <span><strong>Platform:</strong> ${status.platform || '?'}/${status.arch || '?'}</span>
                    <span><strong>Check Interval:</strong> ${status.check_interval || '?'}</span>
                </div>
            </div>
            <div style="display:flex;gap:8px;align-items:center;">
                ${checkBtn}
            </div>
        </div>
    `;

    // Bind check button
    const btn = document.getElementById('selfupdate_check_btn');
    if (btn) {
        btn.addEventListener('click', triggerSelfUpdateCheck);
    }
}

async function triggerSelfUpdateCheck() {
    if (selfUpdateCheckPending) return;
    selfUpdateCheckPending = true;

    const btn = document.getElementById('selfupdate_check_btn');
    if (btn) {
        btn.disabled = true;
        btn.textContent = 'Checking…';
    }

    try {
        const resp = await fetch('/api/v1/selfupdate/check', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            credentials: 'same-origin'
        });
        const data = await resp.json().catch(() => ({}));

        if (resp.ok) {
            showToast('Update check initiated', 'success');
            // Reload after a short delay to show results
            setTimeout(() => loadSelfUpdateRuns(), 2000);
        } else {
            showToast(data.error || 'Failed to start update check', 'error');
        }
    } catch (err) {
        showToast('Failed to start update check', 'error');
    } finally {
        selfUpdateCheckPending = false;
        if (btn) {
            btn.disabled = false;
            btn.textContent = 'Check for Updates';
        }
    }
}

function renderSelfUpdateRuns(container, runs) {
    if (!runs || runs.length === 0) {
        container.innerHTML = '<div class="muted-text">No update attempts recorded.</div>';
        return;
    }

    const statusBadge = (status) => {
        const colors = {
            'success': 'var(--success)',
            'failed': 'var(--danger)',
            'rollback': 'var(--warn)',
            'pending': 'var(--muted)',
            'in_progress': 'var(--highlight)',
        };
        const color = colors[status] || 'var(--muted)';
        return `<span style="display:inline-block;padding:2px 8px;border-radius:4px;font-size:11px;font-weight:600;background:${color}20;color:${color};">${status}</span>`;
    };

    const formatTime = (ts) => {
        if (!ts) return '—';
        const d = new Date(ts);
        return d.toLocaleString();
    };

    const rows = runs.map(run => `
        <tr>
            <td style="padding:8px;border-bottom:1px solid var(--border);">${formatTime(run.started_at)}</td>
            <td style="padding:8px;border-bottom:1px solid var(--border);">${run.from_version || '—'}</td>
            <td style="padding:8px;border-bottom:1px solid var(--border);">${run.to_version || '—'}</td>
            <td style="padding:8px;border-bottom:1px solid var(--border);">${statusBadge(run.status)}</td>
            <td style="padding:8px;border-bottom:1px solid var(--border);max-width:200px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;" title="${run.message || ''}">${run.message || '—'}</td>
            <td style="padding:8px;border-bottom:1px solid var(--border);">${formatTime(run.finished_at)}</td>
        </tr>
    `).join('');

    container.innerHTML = `
        <table style="width:100%;border-collapse:collapse;font-size:13px;">
            <thead>
                <tr style="text-align:left;color:var(--muted);border-bottom:2px solid var(--border);">
                    <th style="padding:8px;">Started</th>
                    <th style="padding:8px;">From</th>
                    <th style="padding:8px;">To</th>
                    <th style="padding:8px;">Status</th>
                    <th style="padding:8px;">Message</th>
                    <th style="padding:8px;">Finished</th>
                </tr>
            </thead>
            <tbody>
                ${rows}
            </tbody>
        </table>
    `;
}

// ---------------------------------------------------------------------------
// Release Artifacts Display
// ---------------------------------------------------------------------------

async function triggerReleasesSync() {
    if (releasesSyncPending) return;
    releasesSyncPending = true;

    const btn = document.getElementById('releases_sync_btn');
    if (btn) {
        btn.disabled = true;
        btn.textContent = 'Syncing…';
    }

    try {
        const resp = await fetch('/api/v1/releases/sync', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            credentials: 'same-origin'
        });
        const data = await resp.json().catch(() => ({}));

        if (resp.ok) {
            showToast('Release sync completed', 'success');
            // Reload to show updated artifacts
            setTimeout(() => loadSelfUpdateRuns(), 500);
        } else {
            showToast(data.error || 'Failed to sync releases', 'error');
        }
    } catch (err) {
        showToast('Failed to sync releases', 'error');
    } finally {
        releasesSyncPending = false;
        if (btn) {
            btn.disabled = false;
            btn.textContent = 'Sync from GitHub';
        }
    }
}

function renderReleaseArtifacts(container, artifacts) {
    if (!artifacts || artifacts.length === 0) {
        container.innerHTML = '<div class="muted-text">No cached artifacts. Click "Sync from GitHub" to fetch releases.</div>';
        return;
    }

    const formatSize = (bytes) => {
        if (!bytes || bytes <= 0) return '—';
        const units = ['B', 'KB', 'MB', 'GB'];
        let size = bytes;
        let unitIndex = 0;
        while (size >= 1024 && unitIndex < units.length - 1) {
            size /= 1024;
            unitIndex++;
        }
        return `${size.toFixed(1)} ${units[unitIndex]}`;
    };

    const formatTime = (ts) => {
        if (!ts || ts === '0001-01-01T00:00:00Z') return '—';
        const d = new Date(ts);
        return d.toLocaleDateString();
    };

    const componentBadge = (component) => {
        const colors = {
            'agent': 'var(--highlight)',
            'server': 'var(--success)'
        };
        const color = colors[component] || 'var(--muted)';
        return `<span style="display:inline-block;padding:2px 8px;border-radius:4px;font-size:11px;font-weight:600;background:${color}20;color:${color};">${component}</span>`;
    };

    const cachedBadge = (cached) => {
        if (cached) {
            return '<span style="color:var(--success);" title="Binary cached on disk">✓</span>';
        }
        return '<span style="color:var(--muted);" title="Not cached">—</span>';
    };

    // Group artifacts by component and version for a cleaner display
    const rows = artifacts.map(a => `
        <tr>
            <td style="padding:8px;border-bottom:1px solid var(--border);">${componentBadge(a.component)}</td>
            <td style="padding:8px;border-bottom:1px solid var(--border);font-family:monospace;">${a.version}</td>
            <td style="padding:8px;border-bottom:1px solid var(--border);">${a.platform}</td>
            <td style="padding:8px;border-bottom:1px solid var(--border);">${a.arch}</td>
            <td style="padding:8px;border-bottom:1px solid var(--border);">${a.channel || 'stable'}</td>
            <td style="padding:8px;border-bottom:1px solid var(--border);text-align:right;">${formatSize(a.size_bytes)}</td>
            <td style="padding:8px;border-bottom:1px solid var(--border);text-align:center;">${cachedBadge(a.cached)}</td>
            <td style="padding:8px;border-bottom:1px solid var(--border);">${formatTime(a.published_at)}</td>
        </tr>
    `).join('');

    container.innerHTML = `
        <table style="width:100%;border-collapse:collapse;font-size:13px;">
            <thead>
                <tr style="text-align:left;color:var(--muted);border-bottom:2px solid var(--border);">
                    <th style="padding:8px;">Component</th>
                    <th style="padding:8px;">Version</th>
                    <th style="padding:8px;">Platform</th>
                    <th style="padding:8px;">Arch</th>
                    <th style="padding:8px;">Channel</th>
                    <th style="padding:8px;text-align:right;">Size</th>
                    <th style="padding:8px;text-align:center;">Cached</th>
                    <th style="padding:8px;">Published</th>
                </tr>
            </thead>
            <tbody>
                ${rows}
            </tbody>
        </table>
    `;
}

// ---------------------------------------------------------------------------
// SSO Admin Integration
// ---------------------------------------------------------------------------

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
    initTenantsSubTabs();
    initTenantBundlesUI();
    switchTenantsView(activeTenantsView, true);
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

function initTenantsSubTabs(){
    if (tenantsSubtabsInitialized) return;
    const bar = document.getElementById('tenants_subtab_bar');
    if (!bar) return;
    tenantsSubtabsInitialized = true;
    bar.querySelectorAll('.tenants-subtab').forEach(btn => {
        btn.addEventListener('click', () => {
            const target = btn.getAttribute('data-tenantsview');
            switchTenantsView(target);
        });
    });
}

function switchTenantsView(view, force = false){
    if (!view) return;
    if (!force && view === activeTenantsView) return;
    const container = document.querySelector('[data-tab="tenants"]');
    if (container) {
        container.querySelectorAll('.tenants-subtab').forEach(btn => {
            const target = btn.getAttribute('data-tenantsview');
            if (target === view) {
                btn.classList.add('active');
            } else {
                btn.classList.remove('active');
            }
        });
        container.querySelectorAll('[data-tenantsview-panel]').forEach(panel => {
            const panelView = panel.getAttribute('data-tenantsview-panel');
            if (panelView === view) {
                panel.classList.remove('hidden');
            } else {
                panel.classList.add('hidden');
            }
        });
    }
    activeTenantsView = view;
    persistUIState(SERVER_UI_STATE_KEYS.TENANTS_VIEW, view);
    if (view === 'bundles') {
        if (tenantBundlesState.selectedTenantId) {
            loadTenantBundles(tenantBundlesState.selectedTenantId);
        } else {
            renderTenantBundlesMessage('Select a tenant to view installer bundles.');
        }
    }
}

function initTenantBundlesUI(){
    if (tenantBundlesUIInitialized) return;
    const select = document.getElementById('tenant_bundles_tenant_select');
    const table = document.getElementById('tenant_bundles_table');
    if (!select || !table) return;
    tenantBundlesUIInitialized = true;
    select.innerHTML = '<option value="">Select tenant…</option>';
    select.addEventListener('change', (event) => {
        tenantBundlesState.selectedTenantId = event.target.value;
        if (!tenantBundlesState.selectedTenantId) {
            renderTenantBundlesMessage('Select a tenant to view installer bundles.');
            return;
        }
        loadTenantBundles(tenantBundlesState.selectedTenantId);
    });
    const refreshBtn = document.getElementById('tenant_bundles_refresh_btn');
    if (refreshBtn) {
        refreshBtn.addEventListener('click', () => {
            if (!tenantBundlesState.selectedTenantId) {
                renderTenantBundlesMessage('Select a tenant to refresh bundles.');
                return;
            }
            loadTenantBundles(tenantBundlesState.selectedTenantId, { force: true });
        });
    }
    syncTenantBundlesTenantDirectory(window._tenants || []);
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
                <td class="actions-col">
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
                        <th class="actions-col">Actions</th>
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

// Cached password policy
let passwordPolicy = null;

async function loadPasswordPolicy() {
    try {
        const r = await fetch('/api/v1/users/password-policy');
        if (r.ok) {
            passwordPolicy = await r.json();
            updatePasswordHint();
        }
    } catch (err) {
        console.warn('Failed to load password policy:', err);
    }
    return passwordPolicy;
}

function updatePasswordHint() {
    const hint = document.getElementById('user_password_hint');
    if (!hint || !passwordPolicy) return;
    const parts = [`min ${passwordPolicy.min_length || 8} chars`];
    if (passwordPolicy.require_uppercase) parts.push('uppercase');
    if (passwordPolicy.require_lowercase) parts.push('lowercase');
    if (passwordPolicy.require_number) parts.push('number');
    if (passwordPolicy.require_special) parts.push('special char');
    hint.textContent = 'Requirements: ' + parts.join(', ');
}

function validatePasswordClient(password) {
    if (!passwordPolicy) return null;
    const errors = [];
    if (password.length < (passwordPolicy.min_length || 8)) {
        errors.push(`at least ${passwordPolicy.min_length || 8} characters`);
    }
    if (passwordPolicy.require_uppercase && !/[A-Z]/.test(password)) {
        errors.push('an uppercase letter');
    }
    if (passwordPolicy.require_lowercase && !/[a-z]/.test(password)) {
        errors.push('a lowercase letter');
    }
    if (passwordPolicy.require_number && !/[0-9]/.test(password)) {
        errors.push('a number');
    }
    if (passwordPolicy.require_special && !/[!@#$%^&*(),.?":{}|<>]/.test(password)) {
        errors.push('a special character');
    }
    if (errors.length > 0) {
        return 'Password must contain: ' + errors.join(', ');
    }
    return null;
}

async function openUserModal(){
    const modal = document.getElementById('user_modal');
    if(!modal) return;
    // Load password policy if not cached
    if (!passwordPolicy) await loadPasswordPolicy();
    updatePasswordHint();
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
    // Clear edit mode
    modal.removeAttribute('data-edit-id');
    document.getElementById('user_submit').textContent = 'Create';
    // reset fields
    document.getElementById('user_username').value = '';
    document.getElementById('user_email').value = '';
    document.getElementById('user_password').value = '';
    document.getElementById('user_role').value = 'user';
    document.getElementById('user_error').style.display='none';
    // Update password label for new user
    const pwLabel = document.querySelector('label[for="user_password"]');
    if (pwLabel) pwLabel.textContent = 'Password *';
    modal.style.display = 'flex';
    document.getElementById('user_username').focus();
}

async function submitCreateUser(){
    const modal = document.getElementById('user_modal');
    const username = document.getElementById('user_username').value.trim();
    const email = document.getElementById('user_email').value.trim();
    const password = document.getElementById('user_password').value;
    const role = document.getElementById('user_role').value || 'user';
    const tenant = document.getElementById('user_tenant').value || '';
    const errEl = document.getElementById('user_error');
    const editId = modal.getAttribute('data-edit-id');
    errEl.style.display='none';

    if(!username){
        errEl.textContent = 'Username is required'; errEl.style.display='block'; return;
    }

    // Password required for new users, optional for edit
    if(!editId && !password){
        errEl.textContent = 'Password is required for new users'; errEl.style.display='block'; return;
    }

    // Validate password if provided
    if(password){
        if (!passwordPolicy) await loadPasswordPolicy();
        const pwError = validatePasswordClient(password);
        if(pwError){
            errEl.textContent = pwError;
            errEl.style.display='block';
            return;
        }
    }

    try{
        const payload = { username, role };
        if(email) payload.email = email;
        if(password) payload.password = password; // Only include if provided
        if(tenant) payload.tenant_id = tenant;

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
        // Load password policy if not cached
        if (!passwordPolicy) await loadPasswordPolicy();
        updatePasswordHint();
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
        document.getElementById('user_error').style.display='none';
        // Update password label to show it's optional when editing
        const pwLabel = document.querySelector('label[for="user_password"]');
        if (pwLabel) pwLabel.textContent = 'Password (leave blank to keep current)';
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
        syncTenantBundlesTenantDirectory(data);
    }catch(err){
        el.innerHTML = '<div style="color:var(--danger)">Error loading tenants: '+(err.message||err)+'</div>';
    }
}

function syncTenantBundlesTenantDirectory(list){
    const select = document.getElementById('tenant_bundles_tenant_select');
    if (!select) return;
    const normalized = normalizeTenantList(list);
    const previous = tenantBundlesState.selectedTenantId;
    const options = ['<option value="">Select tenant…</option>'];
    const validIds = new Set();
    normalized.forEach(tenant => {
        if (!tenant.id) return;
        validIds.add(tenant.id);
        const selected = previous && tenant.id === previous ? ' selected' : '';
        options.push(`<option value="${escapeHtml(tenant.id)}"${selected}>${escapeHtml(tenant.name || tenant.id)}</option>`);
    });
    select.innerHTML = options.join('');
    tenantBundlesState.cache.forEach((_, key) => {
        if (!validIds.has(key)) {
            tenantBundlesState.cache.delete(key);
        }
    });
    if (previous && validIds.has(previous)) {
        select.value = previous;
        tenantBundlesState.selectedTenantId = previous;
        if (activeTenantsView === 'bundles' && tenantBundlesUIInitialized) {
            loadTenantBundles(previous);
        }
        return;
    }
    select.value = '';
    tenantBundlesState.selectedTenantId = '';
    if (activeTenantsView === 'bundles') {
        renderTenantBundlesMessage('Select a tenant to view installer bundles.');
    }
}

function tenantDisplayNameById(tenantId){
    if (!tenantId) return '';
    const list = Array.isArray(window._tenants) ? window._tenants : [];
    const match = list.find(t => (t.id || t.uuid || '') === tenantId);
    if (match && match.name) {
        return match.name;
    }
    return tenantId;
}

async function loadTenantBundles(tenantId, options = {}){
    const container = document.getElementById('tenant_bundles_table');
    if (!container) return;
    const normalizedId = (tenantId || '').trim();
    if (!normalizedId) {
        renderTenantBundlesMessage('Select a tenant to view installer bundles.');
        return;
    }
    if (!options.force && tenantBundlesState.cache.has(normalizedId)) {
        renderTenantBundlesTable(normalizedId, tenantBundlesState.cache.get(normalizedId));
        return;
    }
    tenantBundlesState.loading = true;
    container.innerHTML = '<div class="muted-text">Loading bundles…</div>';
    try {
        const params = new URLSearchParams({ limit: '50' });
        const response = await fetch(`/api/v1/tenants/${encodeURIComponent(normalizedId)}/bundles?${params.toString()}`);
        if (!response.ok) {
            throw new Error(await response.text());
        }
        const bundles = await response.json();
        tenantBundlesState.cache.set(normalizedId, bundles);
        renderTenantBundlesTable(normalizedId, bundles);
    } catch (err) {
        renderTenantBundlesError(err);
    } finally {
        tenantBundlesState.loading = false;
    }
}

function renderTenantBundlesTable(tenantId, bundles){
    const container = document.getElementById('tenant_bundles_table');
    if (!container) return;
    const tenantName = tenantDisplayNameById(tenantId);
    if (!Array.isArray(bundles) || bundles.length === 0) {
        container.innerHTML = `<div class="muted-text">No installer bundles for ${escapeHtml(tenantName || tenantId)} yet.</div>`;
        return;
    }
    const rows = bundles.map(bundle => {
        if (!bundle) {
            return '';
        }
        const status = bundle.expired ? '<span style="color:var(--danger);">Expired</span>' : '<span style="color:var(--success);">Active</span>';
        const created = bundle.created_at ? formatDateTime(bundle.created_at) : '—';
        const expires = bundle.expires_at ? formatDateTime(bundle.expires_at) : '—';
        const platformBits = [];
        if (bundle.platform) platformBits.push(escapeHtml(bundle.platform));
        if (bundle.arch) platformBits.push(escapeHtml(bundle.arch));
        if (bundle.format) platformBits.push(escapeHtml(bundle.format));
        const platform = platformBits.length ? platformBits.join(' • ') : '—';
        const size = typeof bundle.size_bytes === 'number' ? formatBytes(bundle.size_bytes) : '—';
        const component = escapeHtml(bundle.component || 'agent');
        const version = bundle.version ? `<div class="muted-text">v${escapeHtml(bundle.version)}</div>` : '';
        const download = bundle.download_url ? `<a href="${escapeHtml(bundle.download_url)}" target="_blank" rel="noopener">Download</a>` : '<span class="muted-text">Unavailable</span>';
        return `
            <tr>
                <td>
                    <div class="table-primary">${component}</div>
                    ${version}
                    <div class="muted-text">Bundle ID: ${escapeHtml(String(bundle.id || ''))}</div>
                </td>
                <td>${platform}</td>
                <td>${size}</td>
                <td>${created}</td>
                <td>${expires}</td>
                <td>${status}</td>
                <td>${download}</td>
            </tr>
        `;
    }).join('');
    container.innerHTML = `
        <div class="table-wrapper">
            <table class="simple-table">
                <thead>
                    <tr>
                        <th>Bundle</th>
                        <th>Platform</th>
                        <th>Size</th>
                        <th>Created</th>
                        <th>Expires</th>
                        <th>Status</th>
                        <th>Download</th>
                    </tr>
                </thead>
                <tbody>
                    ${rows}
                </tbody>
            </table>
        </div>
    `;
}

function renderTenantBundlesMessage(message){
    const container = document.getElementById('tenant_bundles_table');
    if (!container) return;
    container.innerHTML = `<div class="muted-text">${escapeHtml(message || '')}</div>`;
}

function renderTenantBundlesError(err){
    const container = document.getElementById('tenant_bundles_table');
    if (!container) return;
    const message = err && err.message ? err.message : err;
    container.innerHTML = `<div style="color:var(--danger);">Failed to load bundles: ${escapeHtml(message || 'unknown error')}</div>`;
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
                <td class="actions-col">
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
                        <th class="actions-col">Actions</th>
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
    
    let html = '<div style="max-height: 400px; overflow-y: auto;">';
    html += '<table style="width: 100%; border-collapse: collapse; font-size: 13px;">';
    html += '<thead><tr style="background: var(--bg-secondary); text-align: left;">';
    html += '<th style="padding: 8px; border-bottom: 1px solid var(--border);">ID</th>';
    html += '<th style="padding: 8px; border-bottom: 1px solid var(--border);">Type</th>';
    html += '<th style="padding: 8px; border-bottom: 1px solid var(--border);">Status</th>';
    html += '<th style="padding: 8px; border-bottom: 1px solid var(--border);">Used At</th>';
    html += '<th style="padding: 8px; border-bottom: 1px solid var(--border);">Expires</th>';
    html += '</tr></thead><tbody>';
    
    tokens.forEach(t => {
        const isRevoked = t.revoked;
        const isUsed = !!t.used_at;
        const isExpired = t.expires_at && new Date(t.expires_at) < new Date();
        
        let status = '<span style="color: var(--success);">Active</span>';
        if (isRevoked) status = '<span style="color: var(--danger);">Revoked</span>';
        else if (isUsed && t.one_time) status = '<span style="color: var(--muted);">Used</span>';
        else if (isExpired) status = '<span style="color: var(--warning);">Expired</span>';
        
        html += `<tr style="border-bottom: 1px solid var(--border);">`;
        html += `<td style="padding: 8px; font-family: monospace;">${escapeHtml(t.id)}</td>`;
        html += `<td style="padding: 8px;">${t.one_time ? 'One-time' : 'Reusable'}</td>`;
        html += `<td style="padding: 8px;">${status}</td>`;
        html += `<td style="padding: 8px;">${t.used_at ? window.__pm_shared.formatDateTime(t.used_at) : '-'}</td>`;
        html += `<td style="padding: 8px;">${t.expires_at ? window.__pm_shared.formatDateTime(t.expires_at) : 'Never'}</td>`;
        html += `</tr>`;
    });
    html += '</tbody></table></div>';
    
    // Ask user if they want to revoke a token via input modal
    window.__pm_shared.showAlert(html, 'Tokens for tenant: ' + escapeHtml(tenantID), false, false, true);
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

function formatBytes(bytes){
    let value = Number(bytes);
    if(!isFinite(value) || value <= 0) return '0 B';
    const units = ['B','KB','MB','GB','TB'];
    let unitIdx = 0;
    while(value >= 1024 && unitIdx < units.length - 1){
        value /= 1024;
        unitIdx++;
    }
    const precision = value >= 10 || unitIdx === 0 ? 0 : 1;
    return value.toFixed(precision) + ' ' + units[unitIdx];
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
            <button id="agent_check_update_btn" data-agent-id="${agent.agent_id}" ${agent.status !== 'active' ? 'disabled title="Agent not connected via WebSocket"' : ''}>
                Check for Update
            </button>
            <button data-action="open-agent" data-agent-id="${agent.agent_id}" ${agent.status !== 'active' ? 'disabled title="Agent not connected via WebSocket"' : ''}>
                Open Agent UI
            </button>
        </div>
    `;
    // Attach inline editor handlers now that DOM nodes are present
    try { _attachAgentDetailsNameEditor(agent); } catch (e) { window.__pm_shared && window.__pm_shared.warn && window.__pm_shared.warn('attach editor failed', e); }
    // Attach check for update handler
    try { _attachAgentUpdateHandler(agent); } catch (e) { window.__pm_shared && window.__pm_shared.warn && window.__pm_shared.warn('attach update handler failed', e); }
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

function _attachAgentUpdateHandler(agent) {
    try {
        const btn = document.getElementById('agent_check_update_btn');
        if (!btn) return;
        btn.addEventListener('click', async () => {
            btn.disabled = true;
            btn.textContent = 'Checking...';
            try {
                const res = await fetch(`/api/v1/agents/command/${encodeURIComponent(agent.agent_id)}`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ command: 'check_update' })
                });
                if (!res.ok) {
                    const txt = await res.text();
                    throw new Error(txt || 'Request failed');
                }
                const data = await res.json();
                if (data.success) {
                    window.__pm_shared.showToast('Update check triggered - agent will check for updates', 'success');
                } else {
                    window.__pm_shared.showToast(data.error || 'Failed to trigger update check', 'error');
                }
            } catch (err) {
                window.__pm_shared.showToast('Failed to send command: ' + (err.message || err), 'error');
            } finally {
                btn.disabled = agent.status !== 'active';
                btn.textContent = 'Check for Update';
            }
        });
    } catch (e) {
        window.__pm_shared.warn('Failed to attach agent update handler', e);
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

const DEFAULT_UPDATE_POLICY_SPEC = {
    update_check_days: 7,
    version_pin_strategy: 'minor',
    allow_major_upgrade: false,
    target_version: '',
    collect_telemetry: true,
    maintenance_window: {
        enabled: false,
        timezone: 'UTC',
        start_hour: 0,
        start_min: 0,
        end_hour: 6,
        end_min: 0,
        days_of_week: []
    },
    rollout_control: {
        staggered: true,
        max_concurrent: 0,
        batch_size: 0,
        delay_between_waves: 300,
        jitter_seconds: 60,
        emergency_abort: true
    }
};

const POLICY_VERSION_PIN_OPTIONS = [
    { value: 'major', label: 'Major (stay on v0.x)' },
    { value: 'minor', label: 'Minor (stay on v0.9.x)' },
    { value: 'patch', label: 'Patch (stay on v0.9.14)' }
];

const POLICY_DAYS_OF_WEEK = [
    { value: 0, label: 'Sun' },
    { value: 1, label: 'Mon' },
    { value: 2, label: 'Tue' },
    { value: 3, label: 'Wed' },
    { value: 4, label: 'Thu' },
    { value: 5, label: 'Fri' },
    { value: 6, label: 'Sat' }
];

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
    globalSettingsDirty: false,
    tenantList: [],
    selectedTenantId: '',
    tenantSnapshot: null,
    tenantDraft: null,
    tenantOverridesDraft: {},
    tenantDirty: false,
    tenantSettingsDirty: false,
    saving: false,
    eventsBound: false,
    lockedKeys: new Set(), // Keys locked by environment variables
    updatePolicy: {
        global: createPolicyState(),
        tenant: createPolicyState()
    }
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

function createPolicyState() {
    return {
        policy: clonePolicySpec(DEFAULT_UPDATE_POLICY_SPEC),
        originalPolicy: clonePolicySpec(DEFAULT_UPDATE_POLICY_SPEC),
        enabled: false,
        originalEnabled: false,
        dirty: false,
        loaded: false
    };
}

function clonePolicySpec(spec) {
    return JSON.parse(JSON.stringify(spec || DEFAULT_UPDATE_POLICY_SPEC));
}

function normalizePolicySpec(spec) {
    const normalized = clonePolicySpec(DEFAULT_UPDATE_POLICY_SPEC);
    if (!spec || typeof spec !== 'object') {
        return normalized;
    }
    if (Number.isFinite(Number(spec.update_check_days))) {
        normalized.update_check_days = Number(spec.update_check_days);
    }
    if (typeof spec.version_pin_strategy === 'string') {
        const value = spec.version_pin_strategy.toLowerCase();
        normalized.version_pin_strategy = POLICY_VERSION_PIN_OPTIONS.some(opt => opt.value === value) ? value : normalized.version_pin_strategy;
    }
    if (typeof spec.allow_major_upgrade === 'boolean') {
        normalized.allow_major_upgrade = spec.allow_major_upgrade;
    }
    if (typeof spec.target_version === 'string') {
        normalized.target_version = spec.target_version;
    }
    if (typeof spec.collect_telemetry === 'boolean') {
        normalized.collect_telemetry = spec.collect_telemetry;
    }
    if (spec.maintenance_window && typeof spec.maintenance_window === 'object') {
        const mw = spec.maintenance_window;
        if (typeof mw.enabled === 'boolean') normalized.maintenance_window.enabled = mw.enabled;
        if (typeof mw.timezone === 'string') normalized.maintenance_window.timezone = mw.timezone;
        if (Number.isFinite(Number(mw.start_hour))) normalized.maintenance_window.start_hour = Number(mw.start_hour);
        if (Number.isFinite(Number(mw.start_min))) normalized.maintenance_window.start_min = Number(mw.start_min);
        if (Number.isFinite(Number(mw.end_hour))) normalized.maintenance_window.end_hour = Number(mw.end_hour);
        if (Number.isFinite(Number(mw.end_min))) normalized.maintenance_window.end_min = Number(mw.end_min);
        if (Array.isArray(mw.days_of_week)) {
            normalized.maintenance_window.days_of_week = normalizePolicyDays(mw.days_of_week);
        }
    }
    if (spec.rollout_control && typeof spec.rollout_control === 'object') {
        const rc = spec.rollout_control;
        if (typeof rc.staggered === 'boolean') normalized.rollout_control.staggered = rc.staggered;
        if (Number.isFinite(Number(rc.max_concurrent))) normalized.rollout_control.max_concurrent = Number(rc.max_concurrent);
        if (Number.isFinite(Number(rc.batch_size))) normalized.rollout_control.batch_size = Number(rc.batch_size);
        if (Number.isFinite(Number(rc.delay_between_waves))) normalized.rollout_control.delay_between_waves = Number(rc.delay_between_waves);
        if (Number.isFinite(Number(rc.jitter_seconds))) normalized.rollout_control.jitter_seconds = Number(rc.jitter_seconds);
        if (typeof rc.emergency_abort === 'boolean') normalized.rollout_control.emergency_abort = rc.emergency_abort;
    }
    return normalized;
}

function normalizePolicyDays(days) {
    if (!Array.isArray(days)) {
        return [];
    }
    const normalized = Array.from(new Set(days.map(val => Number(val)).filter(val => Number.isFinite(val) && val >= 0 && val <= 6))).sort((a, b) => a - b);
    return normalized;
}

function getPolicyState(scope) {
    if (!settingsUIState.updatePolicy) {
        settingsUIState.updatePolicy = { global: createPolicyState(), tenant: createPolicyState() };
    }
    return settingsUIState.updatePolicy[scope] || null;
}

function applyPolicySnapshot(scope, enabled, policySpec) {
    const state = getPolicyState(scope);
    if (!state) return;
    state.policy = clonePolicySpec(policySpec);
    state.originalPolicy = clonePolicySpec(policySpec);
    state.enabled = !!enabled;
    state.originalEnabled = !!enabled;
    state.dirty = false;
    state.loaded = true;
    syncSettingsDirtyFlags();
}

function recomputePolicyDirty(scope) {
    const state = getPolicyState(scope);
    if (!state) return;
    const policyChanged = state.enabled && !deepEqual(state.policy, state.originalPolicy);
    const enabledChanged = state.enabled !== state.originalEnabled;
    state.dirty = policyChanged || enabledChanged;
    syncSettingsDirtyFlags();
}

function resetPolicyDraft(scope) {
    const state = getPolicyState(scope);
    if (!state) return;
    state.policy = clonePolicySpec(state.originalPolicy);
    state.enabled = state.originalEnabled;
    state.dirty = false;
    syncSettingsDirtyFlags();
}

function syncSettingsDirtyFlags() {
    const policy = settingsUIState.updatePolicy || {};
    const globalPolicyDirty = policy.global ? policy.global.dirty : false;
    const tenantPolicyDirty = policy.tenant ? policy.tenant.dirty : false;
    settingsUIState.globalDirty = !!(settingsUIState.globalSettingsDirty || globalPolicyDirty);
    settingsUIState.tenantDirty = !!(settingsUIState.tenantSettingsDirty || tenantPolicyDirty);
}

function getSettingsPayload(record) {
    if (!record) return {};
    return record.settings || record.Settings || {};
}

function getOverridesPayload(record) {
    if (!record) return {};
    return record.overrides || record.Overrides || {};
}

function getUpdatedAt(record) {
    if (!record) return null;
    return record.updated_at || record.updatedAt || record.UpdatedAt || null;
}

function getUpdatedBy(record) {
    if (!record) return '';
    return record.updated_by || record.updatedBy || record.UpdatedBy || '';
}

function getOverridesUpdatedAt(record) {
    if (!record) return null;
    return record.overrides_updated_at || record.overridesUpdatedAt || record.OverridesUpdatedAt || null;
}

function getOverridesUpdatedBy(record) {
    if (!record) return '';
    return record.overrides_updated_by || record.overridesUpdatedBy || record.OverridesUpdatedBy || '';
}

function resolveFieldValue(field, value) {
    if (value === undefined || value === null) {
        if (field && Object.prototype.hasOwnProperty.call(field, 'default')) {
            return field.default;
        }
    }
    return value;
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
        settingsUIState.tenantSettingsDirty = false;
        applyPolicySnapshot('tenant', false, DEFAULT_UPDATE_POLICY_SPEC);
        syncSettingsDirtyFlags();
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
    await loadSettingsSources(); // Fetch locked keys
    await loadGlobalSettingsSnapshot();
    await loadGlobalUpdatePolicy();
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

async function loadSettingsSources() {
    try {
        const sources = await fetchJSON('/api/v1/server/settings/sources');
        settingsUIState.lockedKeys = new Set(sources.locked_keys || []);
    } catch (err) {
        // If endpoint doesn't exist or errors, assume no locks
        settingsUIState.lockedKeys = new Set();
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
    settingsUIState.globalDraft = cloneSettings(getSettingsPayload(snapshot));
    settingsUIState.globalSettingsDirty = false;
    syncSettingsDirtyFlags();
}

async function loadGlobalUpdatePolicy() {
    const state = getPolicyState('global');
    if (!state) return;
    try {
        const resp = await fetchJSON('/api/v1/update-policies/global');
        const policy = resp && resp.policy ? normalizePolicySpec(resp.policy) : clonePolicySpec(DEFAULT_UPDATE_POLICY_SPEC);
        applyPolicySnapshot('global', true, policy);
    } catch (err) {
        if (err && err.status === 404) {
            applyPolicySnapshot('global', false, DEFAULT_UPDATE_POLICY_SPEC);
            return;
        }
        throw err;
    }
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
        settingsUIState.tenantSettingsDirty = false;
        if (settingsUIState.updatePolicy && settingsUIState.updatePolicy.tenant) {
            settingsUIState.updatePolicy.tenant = createPolicyState();
        }
        syncSettingsDirtyFlags();
        return;
    }
    const snapshot = await fetchJSON(`/api/v1/settings/tenants/${encodeURIComponent(tenantId)}`);
    settingsUIState.tenantSnapshot = snapshot;
    const baseline = getSettingsPayload(settingsUIState.globalSnapshot);
    const tenantSettings = snapshot ? getSettingsPayload(snapshot) : baseline;
    settingsUIState.tenantDraft = cloneSettings(Object.keys(tenantSettings).length ? tenantSettings : baseline);
    settingsUIState.tenantOverridesDraft = cloneSettings(getOverridesPayload(snapshot));
    settingsUIState.tenantSettingsDirty = false;
    syncSettingsDirtyFlags();
    await loadTenantUpdatePolicy(tenantId);
}

async function loadTenantUpdatePolicy(tenantId) {
    const state = getPolicyState('tenant');
    if (!state) return;
    if (!tenantId) {
        applyPolicySnapshot('tenant', false, DEFAULT_UPDATE_POLICY_SPEC);
        return;
    }
    try {
        const resp = await fetchJSON(`/api/v1/update-policies/${encodeURIComponent(tenantId)}`);
        const policy = resp && resp.policy ? normalizePolicySpec(resp.policy) : clonePolicySpec(DEFAULT_UPDATE_POLICY_SPEC);
        applyPolicySnapshot('tenant', true, policy);
    } catch (err) {
        if (err && err.status === 404) {
            applyPolicySnapshot('tenant', false, DEFAULT_UPDATE_POLICY_SPEC);
            return;
        }
        throw err;
    }
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
        formRoot.addEventListener('input', handlePolicyFieldChange);
        formRoot.addEventListener('change', handlePolicyFieldChange);
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
    refreshPolicyPanel();
}

function renderSettingsFieldRow(field, value, scope) {
    const row = document.createElement('div');
    row.className = 'settings-field-row';
    row.dataset.fieldType = (field.type || 'text').toLowerCase();
    
    // Check if this field is locked by environment variable
    const isLocked = settingsUIState.lockedKeys.has(field.path);
    
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
    // Disable input if user can't edit, no tenant selected (for tenant scope), OR locked by env
    input.disabled = !canEdit || (scope === 'tenant' && !settingsUIState.selectedTenantId) || isLocked;
    control.appendChild(element);

    // Show lock badge if locked by environment variable
    if (isLocked) {
        const lockBadge = document.createElement('span');
        lockBadge.className = 'settings-badge locked';
        lockBadge.textContent = '🔒 Locked';
        lockBadge.title = 'This setting is locked by an environment variable and cannot be changed through managed settings';
        control.appendChild(lockBadge);
    }

    if (scope === 'tenant') {
        const isOverride = hasOverride(pathToArray(field.path));
        const badge = document.createElement('span');
        badge.className = `settings-badge ${isOverride ? 'override' : 'inherited'}`;
        badge.textContent = isOverride ? 'Override' : 'Inherited';
        control.appendChild(badge);
        if (isOverride && canEdit && !isLocked) {
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

function refreshPolicyPanel() {
    const root = document.getElementById('settings_form_root');
    if (!root) return;
    const existing = document.getElementById('auto_update_policy_section');
    if (existing) {
        existing.remove();
    }
    renderUpdatePolicySection(root);
}

function renderUpdatePolicySection(root) {
    if (!root) return;
    const panel = document.createElement('div');
    panel.className = 'settings-section-panel auto-update-policy';
    panel.id = 'auto_update_policy_section';

    const header = document.createElement('div');
    header.className = 'settings-section-header';
    header.innerHTML = `<h4>Auto-Update Policy</h4><p>Control how often agents check for updates, which versions they target, and how rollouts are staged.</p>`;
    panel.appendChild(header);

    const body = document.createElement('div');
    body.className = 'settings-field-list auto-update-field-list';
    const scope = settingsUIState.scope;
    const policyState = getPolicyState(scope);
    const canEdit = userCan('settings.write');

    if (scope === 'tenant' && !settingsUIState.selectedTenantId) {
        body.innerHTML = '<div class="muted-text">Select a customer to manage auto-update overrides.</div>';
        panel.appendChild(body);
        root.appendChild(panel);
        return;
    }

    if (!policyState || !policyState.loaded) {
        body.innerHTML = '<div class="muted-text">Loading auto-update policy…</div>';
        panel.appendChild(body);
        root.appendChild(panel);
        return;
    }

    const toggleRow = document.createElement('div');
    toggleRow.className = 'settings-field-row';
    const toggleLabel = document.createElement('div');
    toggleLabel.className = 'settings-field-label';
    const toggleTitle = scope === 'global' ? 'Enforce auto-update policy' : 'Override global policy';
    const toggleDescription = scope === 'global'
        ? 'Applies to every tenant unless a specific override is configured.'
        : 'Only configure when this customer needs a different cadence than the global defaults.';
    toggleLabel.innerHTML = `<div class="field-title">${escapeHtml(toggleTitle)}</div><div class="field-description">${escapeHtml(toggleDescription)}</div>`;
    const toggleControl = document.createElement('div');
    toggleControl.className = 'settings-field-control';
    const toggle = document.createElement('label');
    toggle.className = 'mini-toggle-container settings-toggle';
    const toggleInput = document.createElement('input');
    toggleInput.type = 'checkbox';
    toggleInput.checked = !!policyState.enabled;
    toggleInput.disabled = !canEdit;
    toggleInput.dataset.policyToggle = 'enabled';
    toggleInput.dataset.policyScope = scope;
    const toggleState = document.createElement('span');
    toggleState.className = 'settings-toggle-state';
    toggleState.textContent = policyState.enabled ? 'Enabled' : 'Disabled';
    toggleInput.addEventListener('change', () => {
        toggleState.textContent = toggleInput.checked ? 'Enabled' : 'Disabled';
    });
    toggle.appendChild(toggleInput);
    toggle.appendChild(toggleState);
    toggleControl.appendChild(toggle);
    toggleRow.appendChild(toggleLabel);
    toggleRow.appendChild(toggleControl);
    body.appendChild(toggleRow);

    if (!policyState.enabled) {
        const inheritMsg = document.createElement('div');
        inheritMsg.className = 'muted-text';
        inheritMsg.textContent = scope === 'global'
            ? 'No global auto-update policy is currently enforced. Agents will rely on their local override settings.'
            : 'This customer currently inherits the global auto-update policy.';
        body.appendChild(inheritMsg);
        panel.appendChild(body);
        root.appendChild(panel);
        return;
    }

    appendPolicyInputs(body, scope, policyState, canEdit);
    panel.appendChild(body);
    root.appendChild(panel);
}

function appendPolicyInputs(container, scope, policyState, canEdit) {
    const policy = policyState.policy || DEFAULT_UPDATE_POLICY_SPEC;
    const disabled = !canEdit || !policyState.enabled;

    container.appendChild(buildPolicyRow('Check cadence (days)', 'Set to 0 to pause unattended update checks.',
        createPolicyNumberInput(scope, 'update_check_days', policy.update_check_days, disabled, 0, 365)));

    container.appendChild(buildPolicyRow('Version pin strategy', 'Controls whether agents stay on major, minor, or patch lines.',
        createPolicySelectInput(scope, 'version_pin_strategy', policy.version_pin_strategy, disabled, POLICY_VERSION_PIN_OPTIONS)));

    container.appendChild(buildPolicyRow('Allow major upgrades', 'When disabled, agents will not cross major version boundaries unless forced manually.',
        createPolicyCheckboxInput(scope, 'allow_major_upgrade', policy.allow_major_upgrade, disabled)));

    container.appendChild(buildPolicyRow('Target version (optional)', 'Provide an exact semantic version to pin the fleet. Leave blank to follow the latest allowed version.',
        createPolicyTextInput(scope, 'target_version', policy.target_version, disabled)));

    container.appendChild(buildPolicyRow('Collect telemetry during rollout', 'Allows the server to gather anonymized update metrics for dashboards.',
        createPolicyCheckboxInput(scope, 'collect_telemetry', policy.collect_telemetry, disabled)));

    container.appendChild(buildPolicySubheader('Maintenance Window'));
    const mwDisabled = disabled;
    container.appendChild(buildPolicyRow('Window enabled', 'Restrict updates to a specific time window in the tenant\'s timezone.',
        createPolicyCheckboxInput(scope, 'maintenance_window.enabled', policy.maintenance_window.enabled, mwDisabled)));

    const maintenanceInputsDisabled = mwDisabled || !policy.maintenance_window.enabled;
    container.appendChild(buildPolicyRow('Timezone', 'IANA timezone such as UTC or America/New_York.',
        createPolicyTextInput(scope, 'maintenance_window.timezone', policy.maintenance_window.timezone, maintenanceInputsDisabled)));

    const startWrapper = document.createElement('div');
    startWrapper.className = 'policy-inline-inputs';
    startWrapper.appendChild(createPolicyNumberInput(scope, 'maintenance_window.start_hour', policy.maintenance_window.start_hour, maintenanceInputsDisabled, 0, 23));
    startWrapper.appendChild(document.createTextNode(' : '));
    startWrapper.appendChild(createPolicyNumberInput(scope, 'maintenance_window.start_min', policy.maintenance_window.start_min, maintenanceInputsDisabled, 0, 59));
    container.appendChild(buildPolicyRow('Start time (HH:MM)', '24-hour format.', startWrapper));

    const endWrapper = document.createElement('div');
    endWrapper.className = 'policy-inline-inputs';
    endWrapper.appendChild(createPolicyNumberInput(scope, 'maintenance_window.end_hour', policy.maintenance_window.end_hour, maintenanceInputsDisabled, 0, 23));
    endWrapper.appendChild(document.createTextNode(' : '));
    endWrapper.appendChild(createPolicyNumberInput(scope, 'maintenance_window.end_min', policy.maintenance_window.end_min, maintenanceInputsDisabled, 0, 59));
    container.appendChild(buildPolicyRow('End time (HH:MM)', '24-hour format.', endWrapper));

    container.appendChild(buildPolicyRow('Days of week', 'Select one or more days for maintenance.',
        createPolicyDaysControl(scope, policy.maintenance_window.days_of_week, maintenanceInputsDisabled)));

    container.appendChild(buildPolicySubheader('Rollout Control'));
    const rolloutDisabled = disabled;
    container.appendChild(buildPolicyRow('Staggered rollout', 'Disabling pushes updates to all agents simultaneously.',
        createPolicyCheckboxInput(scope, 'rollout_control.staggered', policy.rollout_control.staggered, rolloutDisabled)));
    container.appendChild(buildPolicyRow('Max concurrent agents', 'Limit the number of agents updating at the same time (0 = auto).',
        createPolicyNumberInput(scope, 'rollout_control.max_concurrent', policy.rollout_control.max_concurrent, rolloutDisabled, 0, 10000)));
    container.appendChild(buildPolicyRow('Batch size', 'Number of agents per wave when staggering.',
        createPolicyNumberInput(scope, 'rollout_control.batch_size', policy.rollout_control.batch_size, rolloutDisabled, 0, 10000)));
    container.appendChild(buildPolicyRow('Delay between waves (seconds)', 'Pause between staggered batches.',
        createPolicyNumberInput(scope, 'rollout_control.delay_between_waves', policy.rollout_control.delay_between_waves, rolloutDisabled, 0, 86400)));
    container.appendChild(buildPolicyRow('Jitter (seconds)', 'Randomized delay added to reduce thundering herds.',
        createPolicyNumberInput(scope, 'rollout_control.jitter_seconds', policy.rollout_control.jitter_seconds, rolloutDisabled, 0, 3600)));
    container.appendChild(buildPolicyRow('Emergency abort available', 'Allow admins to stop an in-flight rollout from the UI.',
        createPolicyCheckboxInput(scope, 'rollout_control.emergency_abort', policy.rollout_control.emergency_abort, rolloutDisabled)));
}

function buildPolicyRow(label, description, controlElement) {
    const row = document.createElement('div');
    row.className = 'settings-field-row';
    const labelEl = document.createElement('div');
    labelEl.className = 'settings-field-label';
    labelEl.innerHTML = `<div class="field-title">${escapeHtml(label)}</div><div class="field-description">${escapeHtml(description || '')}</div>`;
    const control = document.createElement('div');
    control.className = 'settings-field-control';
    control.appendChild(controlElement);
    row.appendChild(labelEl);
    row.appendChild(control);
    return row;
}

function buildPolicySubheader(title) {
    const divider = document.createElement('div');
    divider.className = 'policy-subheader';
    divider.textContent = title;
    return divider;
}

function createPolicyNumberInput(scope, path, value, disabled, min, max) {
    const input = document.createElement('input');
    input.type = 'number';
    input.value = value === null || value === undefined ? '' : value;
    if (min !== undefined) input.min = min;
    if (max !== undefined) input.max = max;
    input.dataset.policyPath = path;
    input.dataset.policyType = 'number';
    input.dataset.policyScope = scope;
    input.disabled = !!disabled;
    return input;
}

function createPolicyTextInput(scope, path, value, disabled) {
    const input = document.createElement('input');
    input.type = 'text';
    input.value = value === null || value === undefined ? '' : value;
    input.dataset.policyPath = path;
    input.dataset.policyType = 'text';
    input.dataset.policyScope = scope;
    input.disabled = !!disabled;
    return input;
}

function createPolicySelectInput(scope, path, value, disabled, options) {
    const select = document.createElement('select');
    (options || []).forEach(option => {
        const opt = document.createElement('option');
        opt.value = option.value;
        opt.textContent = option.label;
        if (option.value === value) {
            opt.selected = true;
        }
        select.appendChild(opt);
    });
    select.dataset.policyPath = path;
    select.dataset.policyType = 'text';
    select.dataset.policyScope = scope;
    select.disabled = !!disabled;
    return select;
}

function createPolicyCheckboxInput(scope, path, checked, disabled) {
    const toggle = document.createElement('label');
    toggle.className = 'mini-toggle-container settings-toggle';
    const input = document.createElement('input');
    input.type = 'checkbox';
    input.checked = !!checked;
    input.disabled = !!disabled;
    input.dataset.policyPath = path;
    input.dataset.policyType = 'bool';
    input.dataset.policyScope = scope;
    const state = document.createElement('span');
    state.className = 'settings-toggle-state';
    state.textContent = checked ? 'Enabled' : 'Disabled';
    input.addEventListener('change', () => {
        state.textContent = input.checked ? 'Enabled' : 'Disabled';
    });
    toggle.appendChild(input);
    toggle.appendChild(state);
    return toggle;
}

function createPolicyDaysControl(scope, selectedDays, disabled) {
    const wrapper = document.createElement('div');
    wrapper.className = 'policy-days-container';
    wrapper.classList.toggle('disabled', !!disabled);
    const daySet = new Set(Array.isArray(selectedDays) ? selectedDays : []);
    POLICY_DAYS_OF_WEEK.forEach(day => {
        const chip = document.createElement('label');
        chip.className = 'policy-day-chip';
        const input = document.createElement('input');
        input.type = 'checkbox';
        input.checked = daySet.has(day.value);
        input.disabled = !!disabled;
        input.dataset.policyScope = scope;
        input.dataset.policyDay = String(day.value);
        chip.appendChild(input);
        const text = document.createElement('span');
        text.textContent = day.label;
        chip.appendChild(text);
        syncPolicyDayChipState(input);
        wrapper.appendChild(chip);
    });
    return wrapper;
}

function syncPolicyDayChipState(input) {
    if (!input) return;
    const chip = input.closest('.policy-day-chip');
    if (!chip) return;
    chip.classList.toggle('selected', !!input.checked);
    chip.classList.toggle('disabled', input.disabled);
}

function handlePolicyFieldChange(event) {
    const target = event.target;
    if (!target || !target.dataset) {
        return;
    }
    if (target.dataset.policyToggle) {
        handlePolicyToggleChange(target);
        return;
    }
    if (Object.prototype.hasOwnProperty.call(target.dataset, 'policyDay')) {
        handlePolicyDayToggle(target);
        return;
    }
    if (!target.dataset.policyPath) {
        return;
    }
    const scope = resolvePolicyScope(target.dataset.policyScope);
    const state = getPolicyState(scope);
    if (!state || !state.policy) {
        return;
    }
    const type = target.dataset.policyType || target.type || 'text';
    const value = readInputValue(target, type);
    setNestedValue(state.policy, target.dataset.policyPath, value);
    recomputePolicyDirty(scope);
    updateActionButtons();
    if (target.dataset.policyPath === 'maintenance_window.enabled') {
        refreshPolicyPanel();
    }
}

function handlePolicyToggleChange(input) {
    const scope = resolvePolicyScope(input.dataset.policyScope);
    const state = getPolicyState(scope);
    if (!state) return;
    state.enabled = !!input.checked;
    recomputePolicyDirty(scope);
    updateActionButtons();
    refreshPolicyPanel();
}

function handlePolicyDayToggle(input) {
    const scope = resolvePolicyScope(input.dataset.policyScope);
    const state = getPolicyState(scope);
    if (!state) return;
    const rawValue = Number(input.dataset.policyDay);
    if (!Number.isFinite(rawValue)) {
        return;
    }
    const current = getValueByPath(state.policy, 'maintenance_window.days_of_week');
    const next = new Set(Array.isArray(current) ? current : []);
    if (input.checked) {
        next.add(rawValue);
    } else {
        next.delete(rawValue);
    }
    setNestedValue(state.policy, 'maintenance_window.days_of_week', Array.from(next).sort((a, b) => a - b));
    recomputePolicyDirty(scope);
    updateActionButtons();
    syncPolicyDayChipState(input);
}

function resolvePolicyScope(scopeHint) {
    if (scopeHint === 'global' || scopeHint === 'tenant') {
        return scopeHint;
    }
    return settingsUIState.scope === 'tenant' ? 'tenant' : 'global';
}

function createInputForField(field, value) {
    const type = (field.type || 'text').toLowerCase();
    const resolvedValue = resolveFieldValue(field, value);
    let input;
    let element;
    if (type === 'bool') {
        input = document.createElement('input');
        input.type = 'checkbox';
        input.checked = !!resolvedValue;
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
        input.value = resolvedValue === null || resolvedValue === undefined ? '' : resolvedValue;
        if (field.min !== undefined) input.min = field.min;
        if (field.max !== undefined) input.max = field.max;
        element = input;
    } else if (type === 'select' && Array.isArray(field.enum)) {
        input = document.createElement('select');
        field.enum.forEach(optionValue => {
            const opt = document.createElement('option');
            opt.value = optionValue;
            opt.textContent = optionValue;
            if (optionValue === resolvedValue) {
                opt.selected = true;
            }
            input.appendChild(opt);
        });
        element = input;
    } else {
        input = document.createElement('input');
        input.type = 'text';
        input.value = resolvedValue === null || resolvedValue === undefined ? '' : resolvedValue;
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
    const baseline = getSettingsPayload(settingsUIState.globalSnapshot);
    settingsUIState.globalSettingsDirty = !deepEqual(settingsUIState.globalDraft, baseline);
    syncSettingsDirtyFlags();
    updateActionButtons();
}

function updateTenantDraft(path, value) {
    if (!settingsUIState.tenantDraft) {
        settingsUIState.tenantDraft = cloneSettings(settingsUIState.globalDraft);
    }
    setNestedValue(settingsUIState.tenantDraft, path, value);
    const baseValue = getValueByPath(getSettingsPayload(settingsUIState.globalSnapshot), path);
    if (valuesEqual(value, baseValue)) {
        deleteNestedValue(settingsUIState.tenantOverridesDraft, path);
    } else {
        setNestedValue(settingsUIState.tenantOverridesDraft, path, value);
    }
    const originalOverrides = getOverridesPayload(settingsUIState.tenantSnapshot);
    settingsUIState.tenantSettingsDirty = !deepEqual(settingsUIState.tenantOverridesDraft, originalOverrides);
    renderOverrideSummary();
    syncSettingsDirtyFlags();
    updateActionButtons();
}

function clearTenantOverride(path) {
    if (!settingsUIState.tenantDraft) return;
    deleteNestedValue(settingsUIState.tenantOverridesDraft, path);
    const baseValue = getValueByPath(getSettingsPayload(settingsUIState.globalSnapshot), path);
    setNestedValue(settingsUIState.tenantDraft, path, baseValue);
    const originalOverrides = getOverridesPayload(settingsUIState.tenantSnapshot);
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
    const pending = [];
    const settingsChanged = !!settingsUIState.globalSettingsDirty;
    const policyState = getPolicyState('global');
    const policyChanged = !!(policyState && policyState.dirty);
    if (settingsChanged) {
        pending.push(fetchJSON('/api/v1/settings/global', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(settingsUIState.globalDraft)
        }));
    }
    if (policyChanged) {
        pending.push(savePolicyChanges('global'));
    }
    if (!pending.length) {
        return;
    }
    await Promise.all(pending);
    if (settingsChanged) {
        await loadGlobalSettingsSnapshot();
        if (settingsUIState.selectedTenantId) {
            await loadTenantSnapshot(settingsUIState.selectedTenantId);
        }
    }
    if (policyChanged) {
        await loadGlobalUpdatePolicy();
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
    if (!settingsUIState.tenantDirty) {
        return;
    }
    const pending = [];
    const settingsChanged = !!settingsUIState.tenantSettingsDirty;
    const policyState = getPolicyState('tenant');
    const policyChanged = !!(policyState && policyState.dirty);
    if (settingsChanged) {
        const overrides = cloneSettings(settingsUIState.tenantOverridesDraft);
        if (flattenOverrides(overrides).length === 0) {
            pending.push(fetchJSON(`/api/v1/settings/tenants/${encodeURIComponent(tenantId)}`, { method: 'DELETE' }));
        } else {
            pending.push(fetchJSON(`/api/v1/settings/tenants/${encodeURIComponent(tenantId)}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(overrides)
            }));
        }
    }
    if (policyChanged) {
        pending.push(savePolicyChanges('tenant', tenantId));
    }
    if (!pending.length) {
        return;
    }
    await Promise.all(pending);
    await loadTenantSnapshot(tenantId);
    renderSettingsUI();
    window.__pm_shared.showToast('Tenant configuration saved', 'success');
}

async function savePolicyChanges(scope, tenantId) {
    const state = getPolicyState(scope);
    if (!state || !state.dirty) {
        return;
    }
    let endpoint = '/api/v1/update-policies/global';
    if (scope === 'tenant') {
        if (!tenantId) {
            throw new Error('Tenant ID is required to save tenant policy overrides');
        }
        endpoint = `/api/v1/update-policies/${encodeURIComponent(tenantId)}`;
    }
    if (!state.enabled) {
        try {
            await fetchJSON(endpoint, { method: 'DELETE' });
        } catch (err) {
            if (!err || err.status !== 404) {
                throw err;
            }
        }
        return;
    }
    await fetchJSON(endpoint, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ policy: clonePolicySpec(state.policy) })
    });
}

function handleDiscardChanges(event) {
    event.preventDefault();
    if (settingsUIState.scope === 'global') {
        settingsUIState.globalDraft = cloneSettings(getSettingsPayload(settingsUIState.globalSnapshot));
        settingsUIState.globalSettingsDirty = false;
        resetPolicyDraft('global');
    } else {
        const tenantSettings = settingsUIState.tenantSnapshot
            ? getSettingsPayload(settingsUIState.tenantSnapshot)
            : getSettingsPayload(settingsUIState.globalSnapshot);
        settingsUIState.tenantDraft = cloneSettings(tenantSettings);
        settingsUIState.tenantOverridesDraft = cloneSettings(getOverridesPayload(settingsUIState.tenantSnapshot));
        settingsUIState.tenantSettingsDirty = false;
        resetPolicyDraft('tenant');
    }
    syncSettingsDirtyFlags();
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
        const updatedAt = getUpdatedAt(snap);
        if (updatedAt) {
            text = `Updated ${formatRelativeTime(updatedAt)} by ${escapeHtml(getUpdatedBy(snap) || 'system')}`;
        }
    } else if (settingsUIState.scope === 'tenant' && settingsUIState.tenantSnapshot) {
        const snap = settingsUIState.tenantSnapshot;
        const overridesUpdatedAt = getOverridesUpdatedAt(snap);
        if (overridesUpdatedAt) {
            text = `Overrides updated ${formatRelativeTime(overridesUpdatedAt)} by ${escapeHtml(getOverridesUpdatedBy(snap) || 'system')}`;
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
async function loadMetrics(force) {
    const tab = document.querySelector('[data-tab="metrics"]');
    if (!tab) return;
    if (metricsVM.loading && !force) return;

    const since = new Date(Date.now() - getMetricsRangeWindow(metricsVM.range));
    const params = new URLSearchParams({ since: since.toISOString() });

    metricsVM.loading = true;
    renderMetricsLoading();

    try {
        const [summaryResp, aggregatedResp] = await Promise.all([
            fetch('/api/metrics'),
            fetch(`/api/metrics/aggregated?${params.toString()}`)
        ]);

        if (!summaryResp.ok) {
            throw new Error('Summary request failed: HTTP ' + summaryResp.status);
        }
        if (!aggregatedResp.ok) {
            throw new Error('Aggregated request failed: HTTP ' + aggregatedResp.status);
        }

        metricsVM.summary = await summaryResp.json();
        metricsVM.aggregated = await aggregatedResp.json();
        metricsVM.lastFetched = new Date();
        metricsVM.error = null;

        renderMetricsDashboard();
    } catch (err) {
        metricsVM.error = err;
        renderMetricsError(err);
    } finally {
        metricsVM.loading = false;
    }
}

function renderMetricsLoading() {
    const statsEl = document.getElementById('metrics_stats');
    const chartsEl = document.getElementById('metrics_chart_grid');
    const serverEl = document.getElementById('metrics_server_panel');
    const consumablesEl = document.getElementById('metrics_consumables');
    if (statsEl && !metricsVM.summary) {
        statsEl.innerHTML = '<div class="metric-card loading">Loading metrics…</div>';
    }
    if (chartsEl && !metricsVM.aggregated) {
        chartsEl.innerHTML = '<div class="metric-chart-card loading">Loading throughput data…</div>';
    }
    if (serverEl && !metricsVM.aggregated) {
        serverEl.innerHTML = '<div class="metric-card loading">Collecting server stats…</div>';
    }
    if (consumablesEl && !metricsVM.aggregated) {
        consumablesEl.innerHTML = '<div class="card-title">Consumables</div><div class="muted-text">Loading…</div>';
    }
}

function renderMetricsDashboard() {
    renderMetricsOverview(metricsVM.summary, metricsVM.aggregated);
    renderFleetCharts(metricsVM.aggregated);
    renderServerPanel(metricsVM.aggregated?.server);
    renderConsumables(metricsVM.aggregated?.fleet);
    renderMetricsActivity(metricsVM.aggregated);
    updateMetricsRangeButtons();
}

function renderMetricsOverview(summary, aggregated) {
    const container = document.getElementById('metrics_stats');
    if (!container) return;
    if (!summary || !aggregated) {
        container.innerHTML = '<div class="metric-card loading">Waiting for fleet data…</div>';
        return;
    }

    const totals = aggregated?.fleet?.totals || {};
    const statuses = aggregated?.fleet?.statuses || {};
    const history = aggregated?.fleet?.history?.total_impressions || aggregated?.fleet?.history?.TotalImpressions || [];
    const throughput = calculateThroughput(history);
    const rangeLabel = metricsRangeLabel(metricsVM.range);

    container.innerHTML = `
        <div class="metric-card">
            <div class="card-title">Agents</div>
            <div class="metric-kpi-value">${formatNumber(totals.agents || summary.agents_count || 0)}</div>
            <div class="metric-kpi-label">Connected</div>
        </div>
        <div class="metric-card">
            <div class="card-title">Devices</div>
            <div class="metric-kpi-value">${formatNumber(totals.devices || summary.devices_count || 0)}</div>
            <div class="metric-kpi-label">Managed across fleet</div>
        </div>
        <div class="metric-card">
            <div class="card-title">Throughput (${rangeLabel})</div>
            <div class="metric-kpi-value">${formatNumber(Math.round(throughput))}</div>
            <div class="metric-kpi-label">Estimated pages per hour</div>
        </div>
        <div class="metric-card">
            <div class="card-title">Alerts</div>
            ${renderMetricsStatusChips(statuses)}
            <div class="metric-footnote">${metricsVM.lastFetched ? 'Updated ' + formatRelativeTime(metricsVM.lastFetched) : ''}</div>
        </div>
    `;
}

function renderMetricsStatusChips(statuses) {
    const error = statuses?.error || 0;
    const warn = statuses?.warning || 0;
    const jam = statuses?.jam || 0;
    return `
        <div class="metric-status-chips">
            <span class="metric-chip error">Errors <strong>${formatNumber(error)}</strong></span>
            <span class="metric-chip warn">Warnings <strong>${formatNumber(warn)}</strong></span>
            <span class="metric-chip jam">Jams <strong>${formatNumber(jam)}</strong></span>
        </div>
    `;
}

function renderFleetCharts(aggregated) {
    const grid = document.getElementById('metrics_chart_grid');
    if (!grid) return;
    const history = aggregated?.fleet?.history;
    if (!history) {
        grid.innerHTML = '<div class="metric-chart-card loading">No fleet history yet.</div>';
        return;
    }

    const cards = [
        {
            id: 'fleet_total_chart',
            title: 'Total Impressions',
            series: [{ label: 'Total', color: FLEET_SERIES_COLORS[0], points: toSeriesPoints(history.total_impressions || history.TotalImpressions) }],
        },
        {
            id: 'fleet_color_mono_chart',
            title: 'Color vs Mono',
            series: [
                { label: 'Color', color: FLEET_SERIES_COLORS[1], points: toSeriesPoints(history.color_impressions || history.ColorImpressions) },
                { label: 'Mono', color: FLEET_SERIES_COLORS[2], points: toSeriesPoints(history.mono_impressions || history.MonoImpressions) },
            ],
        },
        {
            id: 'fleet_scan_chart',
            title: 'Scan Volume',
            series: [{ label: 'Scans', color: FLEET_SERIES_COLORS[3], points: toSeriesPoints(history.scan_volume || history.ScanVolume) }],
        },
    ];

    grid.innerHTML = cards.map(card => `
        <div class="metric-chart-card">
            <div class="card-title">${card.title}</div>
            <canvas id="${card.id}" class="metric-chart-canvas" height="220"></canvas>
        </div>
    `).join('');

    cards.forEach(card => {
        const canvas = document.getElementById(card.id);
        if (canvas) {
            drawFleetChart(canvas, card.series, { label: card.title });
        }
    });
}

function renderServerPanel(server) {
    const panel = document.getElementById('metrics_server_panel');
    if (!panel) return;
    if (!server) {
        panel.innerHTML = '<div class="metric-card loading">Server metrics unavailable.</div>';
        return;
    }

    const runtime = server.runtime || {};
    const memory = runtime.memory || {};
    const db = server.database || {};
    const uptime = formatDuration(server.uptime_seconds);
    const artifactsBytes = db.release_artifacts_bytes || db.release_bytes || 0;
    const bundlesBytes = db.installer_bundles_bytes || db.installer_bytes || 0;
    const cacheBytes = artifactsBytes + bundlesBytes;

    panel.innerHTML = `
        <div class="metric-card">
            <div class="card-title">Server Runtime</div>
            <div class="metric-kpi-value" style="font-size:18px;">${escapeHtml(server.hostname || 'PrintMaster')}</div>
            <div class="metric-kpi-label">Up ${uptime}, ${escapeHtml(runtime.go_version || 'Go')}</div>
            <ul style="list-style:none;padding:0;margin:12px 0 0;font-size:13px;line-height:1.6;">
                <li>Goroutines: <strong>${formatNumber(runtime.num_goroutine || 0)}</strong></li>
                <li>Heap: <strong>${formatBytes(memory.heap_alloc_bytes || memory.heap_alloc || 0)}</strong></li>
                <li>Total Alloc: <strong>${formatBytes(memory.total_alloc_bytes || memory.total_alloc || 0)}</strong></li>
            </ul>
        </div>
        <div class="metric-card">
            <div class="card-title">Database</div>
            ${db ? `
                <ul style="list-style:none;padding:0;margin:0;font-size:13px;line-height:1.6;">
                    <li>Agents: <strong>${formatNumber(db.agents || 0)}</strong></li>
                    <li>Devices: <strong>${formatNumber(db.devices || 0)}</strong></li>
                    <li>Metrics rows: <strong>${formatNumber(db.metrics_snapshots || 0)}</strong></li>
                    <li>Sessions: <strong>${formatNumber(db.sessions || 0)}</strong></li>
                    <li>Users: <strong>${formatNumber(db.users || 0)}</strong></li>
                    <li>Audit entries: <strong>${formatNumber(db.audit_entries || 0)}</strong></li>
                    <li>Artifacts: <strong>${formatNumber(db.release_artifacts || 0)}</strong> (${formatBytes(artifactsBytes)})</li>
                    <li>Installer bundles: <strong>${formatNumber(db.installer_bundles || 0)}</strong> (${formatBytes(bundlesBytes)})</li>
                    <li>Total cache size: <strong>${formatBytes(cacheBytes)}</strong></li>
                </ul>
            ` : '<div class="muted-text">No DB stats available.</div>'}
        </div>
    `;
}

function renderConsumables(fleet) {
    const card = document.getElementById('metrics_consumables');
    if (!card) return;
    card.innerHTML = '<div class="card-title">Consumables</div>';
    if (!fleet || !fleet.consumables) {
        card.innerHTML += '<div class="muted-text">No consumable data yet.</div>';
        return;
    }
    const totals = fleet.totals || {};
    const consumables = fleet.consumables;
    const totalDevices = Math.max(1, totals.devices || 1);
    const tiers = [
        { key: 'critical', label: 'Critical', value: consumables.critical || 0 },
        { key: 'low', label: 'Low', value: consumables.low || 0 },
        { key: 'medium', label: 'Medium', value: consumables.medium || 0 },
        { key: 'high', label: 'High', value: consumables.high || 0 },
        { key: 'unknown', label: 'Unknown', value: consumables.unknown || 0 },
    ];
    const bars = tiers.map(tier => {
        const pct = Math.round((tier.value / totalDevices) * 100);
        return `
            <div class="consumable-bar" data-tier="${tier.key}">
                <label><span>${tier.label}</span><span>${formatNumber(tier.value)} devices</span></label>
                <progress value="${pct}" max="100"></progress>
            </div>
        `;
    }).join('');
    card.innerHTML += `<div class="metrics-consumable-bars">${bars}</div>`;
}

function renderMetricsActivity(aggregated) {
    const card = document.getElementById('metrics_activity');
    if (!card) return;
    card.innerHTML = '<div class="card-title">Activity</div>';
    if (!aggregated || !aggregated.fleet) {
        card.innerHTML += '<div class="muted-text">No activity yet.</div>';
        return;
    }

    const totals = aggregated.fleet.totals || {};
    const history = aggregated.fleet.history?.total_impressions || [];
    const lastPoint = history[history.length - 1];
    const periodTotal = history.reduce((sum, pt) => sum + Number(pt?.value || 0), 0);
    const lastTimestamp = lastPoint ? formatDateTime(lastPoint.timestamp) : 'n/a';

    card.innerHTML += `
        <div style="display:flex;flex-direction:column;gap:8px;font-size:13px;">
            <div>Total lifetime pages: <strong>${formatNumber(totals.page_count || 0)}</strong></div>
            <div>Period pages (${metricsRangeLabel(metricsVM.range)}): <strong>${formatNumber(periodTotal)}</strong></div>
            <div>Last metric: <strong>${escapeHtml(lastTimestamp)}</strong></div>
        </div>
    `;
}

function renderMetricsError(err) {
    const statsEl = document.getElementById('metrics_stats');
    if (statsEl) {
        statsEl.innerHTML = `<div class="metric-card" style="color:var(--danger);">Failed to load metrics: ${escapeHtml(err?.message || err)}</div>`;
    }
    const chartsEl = document.getElementById('metrics_chart_grid');
    if (chartsEl) {
        chartsEl.innerHTML = '<div class="metric-chart-card" style="color:var(--danger);">Unable to render charts.</div>';
    }
}

function getMetricsRangeWindow(range) {
    return METRICS_RANGE_WINDOWS[range] || METRICS_RANGE_WINDOWS[METRICS_DEFAULT_RANGE];
}

function metricsRangeLabel(range) {
    switch (range) {
        case '24h': return '24 hours';
        case '7d': return '7 days';
        case '30d': return '30 days';
        case '90d': return '90 days';
        case '365d': return '1 year';
        default: return range;
    }
}

function initMetricsRangeControls() {
    const controls = document.getElementById('metrics_range_controls');
    if (!controls || controls._metricsBound) return;
    controls._metricsBound = true;
    controls.addEventListener('click', (evt) => {
        const btn = evt.target.closest('[data-range]');
        if (!btn) return;
        setMetricsRange(btn.getAttribute('data-range'));
    });
    updateMetricsRangeButtons();
}

function setMetricsRange(range) {
    if (!range || range === metricsVM.range) return;
    metricsVM.range = range;
    updateMetricsRangeButtons();
    loadMetrics(true);
}

function updateMetricsRangeButtons() {
    const controls = document.getElementById('metrics_range_controls');
    if (!controls) return;
    controls.querySelectorAll('[data-range]').forEach(btn => {
        if (btn.getAttribute('data-range') === metricsVM.range) {
            btn.classList.add('active');
        } else {
            btn.classList.remove('active');
        }
    });
}

function isMetricsTabActive() {
    const tab = document.querySelector('[data-tab="metrics"]');
    return tab && !tab.classList.contains('hidden');
}

function toSeriesPoints(arr) {
    if (!Array.isArray(arr)) return [];
    return arr.map(pt => {
        const timestamp = pt?.timestamp || pt?.Timestamp;
        const value = Number(pt?.value ?? pt?.Value ?? 0);
        const timeMs = timestamp ? new Date(timestamp).getTime() : NaN;
        return (Number.isFinite(timeMs)) ? { time: timeMs, value } : null;
    }).filter(Boolean);
}

function calculateThroughput(series) {
    if (!Array.isArray(series) || series.length === 0) return 0;
    const total = series.reduce((sum, pt) => sum + Number(pt?.value || pt?.Value || 0), 0);
    const hours = getMetricsRangeWindow(metricsVM.range) / (60 * 60 * 1000);
    return hours > 0 ? total / hours : 0;
}

function formatDuration(seconds) {
    if (!Number.isFinite(seconds) || seconds <= 0) {
        return 'unknown';
    }
    const sec = Math.floor(seconds);
    const days = Math.floor(sec / 86400);
    const hours = Math.floor((sec % 86400) / 3600);
    const minutes = Math.floor((sec % 3600) / 60);
    const parts = [];
    if (days) parts.push(days + 'd');
    if (hours) parts.push(hours + 'h');
    if (!days && !hours && minutes) parts.push(minutes + 'm');
    if (parts.length === 0) parts.push(sec + 's');
    return parts.join(' ');
}

function drawFleetChart(canvas, seriesList, options) {
    const ctx = canvas.getContext('2d');
    if (!ctx) return;
    const dpr = window.devicePixelRatio || 1;
    const rect = canvas.getBoundingClientRect();
    canvas.width = rect.width * dpr;
    canvas.height = rect.height * dpr;
    ctx.scale(dpr, dpr);
    ctx.clearRect(0, 0, rect.width, rect.height);

    const points = seriesList.flatMap(s => s.points || []);
    if (points.length === 0) {
        ctx.fillStyle = 'rgba(255,255,255,0.6)';
        ctx.font = '12px sans-serif';
        ctx.fillText('No data', 12, rect.height / 2);
        return;
    }

    const minTime = Math.min(...points.map(p => p.time));
    const maxTime = Math.max(...points.map(p => p.time));
    const minValue = 0;
    const maxValue = Math.max(...points.map(p => p.value)) || 1;
    const padding = { top: 20, right: 16, bottom: 26, left: 40 };
    const width = rect.width - padding.left - padding.right;
    const height = rect.height - padding.top - padding.bottom;

    const mapX = (time) => padding.left + ((time - minTime) / Math.max(1, maxTime - minTime)) * width;
    const mapY = (value) => padding.top + height - ((value - minValue) / Math.max(1, maxValue - minValue)) * height;

    ctx.strokeStyle = 'rgba(255,255,255,0.08)';
    ctx.lineWidth = 1;
    for (let i = 0; i <= 4; i++) {
        const y = padding.top + (height / 4) * i;
        ctx.beginPath();
        ctx.moveTo(padding.left, y);
        ctx.lineTo(padding.left + width, y);
        ctx.stroke();
    }

    ctx.strokeStyle = 'rgba(255,255,255,0.12)';
    ctx.beginPath();
    ctx.moveTo(padding.left, padding.top);
    ctx.lineTo(padding.left, padding.top + height);
    ctx.lineTo(padding.left + width, padding.top + height);
    ctx.stroke();

    seriesList.forEach((series, idx) => {
        const color = series.color || FLEET_SERIES_COLORS[idx % FLEET_SERIES_COLORS.length];
        const pts = (series.points || []).filter(p => Number.isFinite(p.time) && Number.isFinite(p.value));
        if (pts.length === 0) return;
        ctx.strokeStyle = color;
        ctx.lineWidth = 2;
        ctx.beginPath();
        pts.forEach((pt, i) => {
            const x = mapX(pt.time);
            const y = mapY(pt.value);
            if (i === 0) {
                ctx.moveTo(x, y);
            } else {
                ctx.lineTo(x, y);
            }
        });
        ctx.stroke();
    });

    ctx.fillStyle = 'rgba(255,255,255,0.5)';
    ctx.font = '10px monospace';
    ctx.textAlign = 'right';
    for (let i = 0; i <= 4; i++) {
        const value = minValue + ((maxValue - minValue) / 4) * i;
        const y = mapY(value);
        ctx.fillText(formatNumber(Math.round(value)), padding.left - 6, y + 3);
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
        token: null,
        mode: 'token',
        platform: 'windows',
        format: 'zip',
        arch: 'amd64'
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
        const platformSel = document.getElementById('add_agent_platform');
        const formatSel = document.getElementById('add_agent_format');
        const archSel = document.getElementById('add_agent_arch');
        const platform = platformSel ? platformSel.value : (st.platform || 'windows');
        const format = formatSel ? formatSel.value : (st.format || 'zip');
        const arch = archSel ? archSel.value : (st.arch || 'amd64');

        st.tenantID = tenantID;
        st.ttl = ttl;
        st.one_time = one_time;
        st.platform = platform;
        st.format = format;
        st.arch = arch;

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
                st.script = null;
                st.bundle = null;
                st.mode = 'token';
                st.step = 2;
                window._addAgentState = st;
                renderAddAgentStep(2);
            }catch(err){
                window.__pm_shared.showAlert('Failed to create join token: ' + (err && err.message ? err.message : err), 'Error', true, false);
            }
        } else if(action === 'script'){
            // Generate bootstrap script via server packages API
            try{
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
                st.bundle = null;
                st.token = null;
                st.mode = 'script';
                st.step = 2;
                window._addAgentState = st;
                renderAddAgentStep(2);
            }catch(err){
                window.__pm_shared.showAlert('Failed to generate bootstrap script: ' + (err && err.message ? err.message : err), 'Error', true, false);
            }
        } else if(action === 'archive'){
            try{
                const payload = {
                    tenant_id: tenantID,
                    platform: platform,
                    installer_type: 'archive',
                    ttl_minutes: ttl,
                    format: format,
                    arch: arch,
                    component: 'agent'
                };
                const r = await fetch('/api/v1/packages', { method: 'POST', headers: {'content-type':'application/json'}, body: JSON.stringify(payload) });
                if(!r.ok){
                    throw new Error(await r.text());
                }
                const data = await r.json();
                st.bundle = data;
                st.script = null;
                st.token = null;
                st.mode = 'archive';
                st.step = 2;
                window._addAgentState = st;
                renderAddAgentStep(2);
            } catch(err){
                window.__pm_shared.showAlert('Failed to build installer: ' + (err && err.message ? err.message : err), 'Error', true, false);
            }
        } else {
            window.__pm_shared.showAlert('Unsupported onboarding option selected.', 'Error', true, false);
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
    if(indicator){
        if(step === 1){
            indicator.textContent = 'Step 1/2 — Create onboarding asset';
        } else {
            const mode = (window._addAgentState && window._addAgentState.mode) ? window._addAgentState.mode : 'token';
            let label = 'Token (shown once)';
            if(mode === 'script') label = 'Bootstrap script';
            if(mode === 'archive') label = 'Installer download';
            indicator.textContent = 'Step 2/2 — ' + label;
        }
    }

    if(step === 1){
        // Tenant select
        const state = window._addAgentState || {};
        const tenants = Array.isArray(window._tenants) ? window._tenants : [];
        let tenantOptions = '';
        if(tenants.length === 0 && state.tenantID){
            tenantOptions = `<option value="${escapeHtml(state.tenantID)}">${escapeHtml(state.tenantID)}</option>`;
        } else {
            tenantOptions = tenants.map(t => {
                const value = t.id || t.uuid || t.name || '';
                const label = t.name || t.id || t.uuid || value;
                return `<option value="${escapeHtml(value)}">${escapeHtml(label)}</option>`;
            }).join('\n');
        }
        const ttlValue = state.ttl && state.ttl > 0 ? state.ttl : 60;
        const oneTimeChecked = state.one_time === undefined ? true : !!state.one_time;
        const defaultPlatform = state.platform || 'windows';
        const platformOptions = [
            { value: 'linux', label: 'Linux' },
            { value: 'windows', label: 'Windows' },
            { value: 'darwin', label: 'macOS' }
        ].map(opt => `<option value="${escapeHtml(opt.value)}" ${opt.value === defaultPlatform ? 'selected' : ''}>${escapeHtml(opt.label)}</option>`).join('\n');
        const formatOptions = [
            { value: 'zip', label: 'ZIP archive' },
            { value: 'tar.gz', label: 'TAR.GZ archive' }
        ].map(opt => `<option value="${escapeHtml(opt.value)}">${escapeHtml(opt.label)}</option>`).join('\n');
        const archOptions = [
            { value: 'amd64', label: 'x86_64 / amd64' },
            { value: 'arm64', label: 'ARM64 / Apple Silicon' }
        ].map(opt => `<option value="${escapeHtml(opt.value)}">${escapeHtml(opt.label)}</option>`).join('\n');

        content.innerHTML = `
            <div style="display:flex;flex-direction:column;gap:8px;">
                <label style="font-weight:600">Customer (tenant)</label>
                <select id="add_agent_tenant" style="padding:8px;border-radius:4px;border:1px solid var(--border);">
                    <option value="">-- select customer --</option>
                    ${tenantOptions}
                </select>

                <label style="font-weight:600">Join token TTL (minutes)</label>
                <input id="add_agent_ttl" type="number" value="${escapeHtml(String(ttlValue))}" min="1" style="padding:8px;border-radius:4px;border:1px solid var(--border);width:120px;" />

                <label style="display:flex;align-items:center;gap:8px;">
                    <input id="add_agent_one_time" type="checkbox" ${oneTimeChecked ? 'checked' : ''} />
                    <span style="color:var(--muted)">One-time (single-use) token</span>
                </label>

                <div style="margin-top:8px;">
                    <div style="font-weight:600;margin-bottom:6px;">Onboarding method</div>
                    <label style="display:flex;align-items:center;gap:8px;"><input type="radio" name="add_agent_action" value="token" ${state.mode === 'token' || !state.mode ? 'checked' : ''} /> Show raw token</label>
                    <label style="display:flex;align-items:center;gap:8px;"><input type="radio" name="add_agent_action" value="script" ${state.mode === 'script' ? 'checked' : ''} /> Generate bootstrap script</label>
                    <label style="display:flex;align-items:center;gap:8px;"><input type="radio" name="add_agent_action" value="archive" ${state.mode === 'archive' ? 'checked' : ''} /> Build installer archive</label>
                    <div id="add_agent_platform_row" style="margin-top:8px;display:none;">
                        <label style="font-weight:600">Target platform</label>
                        <select id="add_agent_platform" style="padding:8px;border-radius:4px;border:1px solid var(--border);width:180px;">
                            ${platformOptions}
                        </select>
                    </div>
                    <div id="add_agent_archive_fields" style="margin-top:12px;display:none;">
                        <label style="font-weight:600">Archive format</label>
                        <select id="add_agent_format" style="padding:8px;border-radius:4px;border:1px solid var(--border);width:200px;">
                            ${formatOptions}
                        </select>
                        <label style="font-weight:600;margin-top:10px;">Architecture</label>
                        <select id="add_agent_arch" style="padding:8px;border-radius:4px;border:1px solid var(--border);width:200px;">
                            ${archOptions}
                        </select>
                        <div style="color:var(--muted);font-size:12px;margin-top:6px;">Installer archives include tenant-specific config and expire automatically.</div>
                    </div>
                </div>

                <div style="color:var(--muted);font-size:13px">Tokens and installers inherit this TTL. Installer bundles embed the generated join token and may be downloaded from the link shown after build.</div>
            </div>
        `;

        const tenantSelect = content.querySelector('#add_agent_tenant');
        if(tenantSelect && state.tenantID){ tenantSelect.value = state.tenantID; }
        if(tenantSelect){ tenantSelect.addEventListener('change', ()=>{ state.tenantID = tenantSelect.value; }); }

        const ttlInput = content.querySelector('#add_agent_ttl');
        if(ttlInput){
            ttlInput.addEventListener('change', ()=>{
                const parsed = parseInt(ttlInput.value, 10);
                state.ttl = (isNaN(parsed) || parsed <= 0) ? 60 : parsed;
                ttlInput.value = state.ttl;
            });
        }

        const oneTimeInput = content.querySelector('#add_agent_one_time');
        if(oneTimeInput){
            oneTimeInput.checked = oneTimeChecked;
            oneTimeInput.addEventListener('change', ()=>{ state.one_time = oneTimeInput.checked; });
        }

        const platformSelect = content.querySelector('#add_agent_platform');
        if(platformSelect){
            platformSelect.value = defaultPlatform;
            state.platform = platformSelect.value;
            platformSelect.addEventListener('change', ()=>{
                state.platform = platformSelect.value;
            });
        }

        const formatSelect = content.querySelector('#add_agent_format');
        if(formatSelect){
            const selectedFormat = state.format || (state.platform === 'windows' ? 'zip' : 'tar.gz');
            formatSelect.value = selectedFormat;
            state.format = formatSelect.value;
            formatSelect.addEventListener('change', ()=>{ state.format = formatSelect.value; });
        }

        const archSelect = content.querySelector('#add_agent_arch');
        if(archSelect){
            const defaultArch = state.arch || (defaultPlatform === 'darwin' ? 'arm64' : 'amd64');
            archSelect.value = defaultArch;
            state.arch = archSelect.value;
            archSelect.addEventListener('change', ()=>{ state.arch = archSelect.value; });
        }

        const actionRadios = content.querySelectorAll('input[name="add_agent_action"]');
        const platRow = content.querySelector('#add_agent_platform_row');
        const archiveFields = content.querySelector('#add_agent_archive_fields');
        const updatePrimaryLabel = () => {
            const sel = content.querySelector('input[name="add_agent_action"]:checked');
            if(!sel || !primaryBtn) return;
            if(sel.value === 'script') primaryBtn.textContent = 'Generate Script';
            else if(sel.value === 'archive') primaryBtn.textContent = 'Build Installer';
            else primaryBtn.textContent = 'Create Token';
        };
        const updateFieldVisibility = () => {
            const sel = content.querySelector('input[name="add_agent_action"]:checked');
            const mode = sel ? sel.value : 'token';
            if(platRow) platRow.style.display = mode === 'token' ? 'none' : '';
            if(archiveFields) archiveFields.style.display = mode === 'archive' ? '' : 'none';
        };
        actionRadios.forEach(r => r.addEventListener('change', ()=>{
            state.mode = r.value;
            updatePrimaryLabel();
            updateFieldVisibility();
        }));
        updatePrimaryLabel();
        updateFieldVisibility();
    } else {
        // Step 2: show token
        const token = (window._addAgentState && window._addAgentState.token) ? window._addAgentState.token : '';
        const mode = (window._addAgentState && window._addAgentState.mode) ? window._addAgentState.mode : 'token';
        const script = (window._addAgentState && window._addAgentState.script) ? window._addAgentState.script : null;
        const filename = (window._addAgentState && window._addAgentState.scriptFilename) ? window._addAgentState.scriptFilename : 'bootstrap';
        const bundle = (window._addAgentState && window._addAgentState.bundle) ? window._addAgentState.bundle : null;
        if(mode === 'archive' && bundle){
            const expires = bundle.expires_at ? new Date(bundle.expires_at).toLocaleString() : 'n/a';
            const sizeBytes = bundle.size_bytes ? Number(bundle.size_bytes) : 0;
            const metadata = bundle.metadata || null;
            const downloadURL = bundle.download_url || '';
            content.innerHTML = `
                <div style="display:flex;flex-direction:column;gap:12px;">
                    <div class="card" style="padding:12px;">
                        <div style="font-weight:600;margin-bottom:6px;">Installer ready</div>
                        <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:8px;font-size:13px;">
                            <div><div class="muted-text">Platform</div><div>${escapeHtml(bundle.platform || '')}</div></div>
                            <div><div class="muted-text">Architecture</div><div>${escapeHtml(bundle.arch || '')}</div></div>
                            <div><div class="muted-text">Format</div><div>${escapeHtml(bundle.format || '')}</div></div>
                            <div><div class="muted-text">Size</div><div>${sizeBytes ? formatBytes(sizeBytes) : 'unknown'}</div></div>
                            <div><div class="muted-text">Expires</div><div>${escapeHtml(expires)}</div></div>
                        </div>
                    </div>
                    <div style="display:flex;gap:8px;flex-wrap:wrap;">
                        <button id="add_agent_download_bundle" class="modal-button" ${downloadURL ? '' : 'disabled'}>Download installer</button>
                        ${ downloadURL ? `<button id="add_agent_copy_bundle" class="modal-button modal-button-secondary">Copy download URL</button>` : '' }
                        ${ downloadURL ? `<a id="add_agent_open_bundle" class="modal-button" href="${escapeHtml(downloadURL)}" target="_blank" rel="noopener">Open link</a>` : '' }
                    </div>
                    ${ metadata ? `<details style="font-size:13px;" open>
                        <summary style="cursor:pointer;font-weight:600;">Embed metadata</summary>
                        <pre style="margin-top:6px;white-space:pre-wrap;word-break:break-word;background:var(--panel);padding:12px;border-radius:6px;border:1px dashed var(--border);">${escapeHtml(JSON.stringify(metadata, null, 2))}</pre>
                    </details>` : '' }
                    <div style="color:var(--muted);font-size:13px;">Installer bundles expire automatically. Share this download link with trusted recipients only.</div>
                </div>
            `;
            const downloadBtn = document.getElementById('add_agent_download_bundle');
            if(downloadBtn && downloadURL){ downloadBtn.addEventListener('click', ()=>{ window.open(downloadURL, '_blank', 'noopener'); }); }
            const copyBtn = document.getElementById('add_agent_copy_bundle');
            if(copyBtn && downloadURL){
                copyBtn.addEventListener('click', ()=>{
                    navigator.clipboard?.writeText(downloadURL).then(()=> window.__pm_shared.showToast('Download link copied','success')).catch(err=> window.__pm_shared.showAlert('Failed to copy link: ' + (err && err.message ? err.message : err), 'Error', true, false));
                });
            }
        } else if(script && mode === 'script'){
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

