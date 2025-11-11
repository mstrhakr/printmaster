// PrintMaster Shared JavaScript - Common utilities for Agent and Server UIs

// Load shared vendor assets (flatpickr) from server-hosted copy if available,
// otherwise fall back to CDN. This centralizes the import so agent and server
// UIs get the same vendor script without duplicating <script> tags in each
// HTML file.
// Load flatpickr from the CDN (simpler, reliable). If you prefer vendoring,
// reintroduce local files and update the server embed accordingly.
(function loadFlatpickrFromCdn() {
    const cdnCss = 'https://cdn.jsdelivr.net/npm/flatpickr/dist/flatpickr.min.css';
    const cdnJs = 'https://cdn.jsdelivr.net/npm/flatpickr';

    const link = document.createElement('link');
    link.rel = 'stylesheet';
    link.href = cdnCss;
    document.head.appendChild(link);

    const script = document.createElement('script');
    script.src = cdnJs;
    script.defer = true;
    document.head.appendChild(script);
})();

// ============================================================================
// TOAST NOTIFICATION SYSTEM
// ============================================================================

/**
 * Display a toast notification
 * @param {string} message - Message to display
 * @param {string} type - 'success', 'error', or 'info'
 * @param {number} duration - Duration in milliseconds (default 3000)
 */
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
        }, 300); // Match CSS animation duration
    }, duration);
}

// ============================================================================
// MODAL SYSTEM
// ============================================================================

/**
 * Show a confirmation modal dialog
 * @param {string} message - Message to display
 * @param {string} title - Modal title (default 'Confirm Action')
 * @param {boolean} isDangerous - Whether this is a destructive action (affects button color)
 * @returns {Promise<boolean>} - Resolves to true if confirmed, false if cancelled
 */
function showConfirm(message, title = 'Confirm Action', isDangerous = false) {
    return new Promise((resolve) => {
        // Prevent re-entrancy / recursion: if a confirm modal is already open,
        // avoid opening another one. This guards against accidental re-binding
        // or nested calls that caused "too much recursion" in some pages.
        if (window.__pm_confirm_open) {
            // Fallback: use browser confirm as a safe synchronous fallback
            try {
                resolve(window.confirm(message));
            } catch (e) {
                resolve(false);
            }
            return;
        }

        window.__pm_confirm_open = true;

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
            // Clear guard
            window.__pm_confirm_open = false;
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

/**
 * Show an alert-style modal (no cancel button, just OK to dismiss)
 * @param {string} message - Message to display
 * @param {string} title - Modal title (default 'Notice')
 * @param {boolean} isDangerous - Whether to style as danger/warning
 * @param {boolean} showDontRemindCheckbox - Show "don't remind me" checkbox
 */
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

// ============================================================================
// CLIPBOARD UTILITIES
// ============================================================================

/**
 * Copy text to clipboard with user feedback
 * @param {string} text - Text to copy
 */
function copyToClipboard(text, callback, successMessage) {
    // callback (optional) will be called with a boolean success flag
    if (!text) {
        if (typeof callback === 'function') callback(false);
        return;
    }

    navigator.clipboard.writeText(text).then(() => {
        try { showToast(successMessage || 'Copied to clipboard', 'success', 1500); } catch (e) {}
        if (typeof callback === 'function') callback(true);
    }).catch(err => {
        console.error('Failed to copy:', err);
        try { showToast('Failed to copy to clipboard', 'error'); } catch (e) {}
        if (typeof callback === 'function') callback(false);
    });
}

// ============================================================================
// DATE/TIME FORMATTING
// ============================================================================

/**
 * Format a date string to local time display
 * @param {string} dateString - ISO date string
 * @returns {string} - Formatted date/time
 */
function formatDateTime(dateString) {
    if (!dateString) return 'Never';
    
    try {
        const date = new Date(dateString);
        if (isNaN(date.getTime())) return 'Invalid date';
        
        // Format as: "Nov 8, 2025 3:45 PM"
        return date.toLocaleString('en-US', {
            month: 'short',
            day: 'numeric',
            year: 'numeric',
            hour: 'numeric',
            minute: '2-digit',
            hour12: true
        });
    } catch (err) {
        console.error('Date formatting error:', err);
        return dateString;
    }
}

/**
 * Show a prompt modal dialog and return the entered value or null if cancelled.
 * @param {string} message - Message to display
 * @param {string} defaultValue - Default input value
 * @param {string} title - Modal title
 * @returns {Promise<string|null>}
 */
function showPrompt(message, defaultValue = '', title = 'Input') {
    return new Promise((resolve) => {
        // Create modal dynamically if missing
        let modal = document.getElementById('prompt_modal');
        if (!modal) {
            modal = document.createElement('div');
            modal.id = 'prompt_modal';
            modal.className = 'modal-overlay';
            modal.style.display = 'none';
            modal.innerHTML = `
                <div class="modal-content" style="max-width:480px;">
                    <div class="modal-header">
                        <h3 class="modal-title" id="prompt_modal_title">${title}</h3>
                        <button class="modal-close-x" id="prompt_modal_close_x" title="Close">&times;</button>
                    </div>
                    <div class="modal-body">
                        <p id="prompt_modal_message">${message}</p>
                        <input id="prompt_modal_input" class="modal-input" style="width:100%;padding:8px;margin-top:8px;" />
                    </div>
                    <div class="modal-footer">
                        <button class="modal-button modal-button-secondary" id="prompt_modal_cancel">Cancel</button>
                        <button class="modal-button modal-button-primary" id="prompt_modal_ok">OK</button>
                    </div>
                </div>
            `;
            document.body.appendChild(modal);

            // Close handlers
            modal.querySelector('#prompt_modal_close_x').addEventListener('click', () => { modal.style.display = 'none'; resolve(null); });
            modal.querySelector('#prompt_modal_cancel').addEventListener('click', () => { modal.style.display = 'none'; resolve(null); });
            modal.addEventListener('click', (e) => { if (e.target === modal) { modal.style.display = 'none'; resolve(null); } });
        }

        const titleEl = document.getElementById('prompt_modal_title');
        const messageEl = document.getElementById('prompt_modal_message');
        const inputEl = document.getElementById('prompt_modal_input');
        const okBtn = document.getElementById('prompt_modal_ok');

        titleEl.textContent = title;
        messageEl.textContent = message;
        inputEl.value = defaultValue || '';

        // OK handler
        const onOk = () => {
            const val = inputEl.value;
            cleanup();
            resolve(val);
        };

        function cleanup() {
            try { okBtn.removeEventListener('click', onOk); } catch (e) {}
        }

        okBtn.addEventListener('click', onOk);

        // Show modal and focus input
        modal.style.display = 'flex';
        setTimeout(() => { try { inputEl.focus(); inputEl.select(); } catch (e) {} }, 10);
    });
}

/**
 * Format a date string as relative time (e.g., "2 minutes ago")
 * @param {string} dateString - ISO date string
 * @returns {string} - Relative time string
 */
function formatRelativeTime(dateString) {
    if (!dateString) return 'Never';
    
    try {
        const date = new Date(dateString);
        if (isNaN(date.getTime())) return 'Invalid date';
        
        const now = new Date();
        const seconds = Math.floor((now - date) / 1000);
        
        if (seconds < 60) return 'Just now';
        const minutes = Math.floor(seconds / 60);
        if (minutes < 60) return `${minutes} minute${minutes !== 1 ? 's' : ''} ago`;
        const hours = Math.floor(minutes / 60);
        if (hours < 24) return `${hours} hour${hours !== 1 ? 's' : ''} ago`;
        const days = Math.floor(hours / 24);
        if (days < 30) return `${days} day${days !== 1 ? 's' : ''} ago`;
        const months = Math.floor(days / 30);
        if (months < 12) return `${months} month${months !== 1 ? 's' : ''} ago`;
        const years = Math.floor(months / 12);
        return `${years} year${years !== 1 ? 's' : ''} ago`;
    } catch (err) {
        console.error('Relative time formatting error:', err);
        return dateString;
    }
}

// ============================================================================
// NUMBER FORMATTING
// ============================================================================

/**
 * Format a number with thousand separators
 * @param {number} num - Number to format
 * @returns {string} - Formatted number (e.g., "1,234,567")
 */
function formatNumber(num) {
    if (num === null || num === undefined) return '0';
    return num.toLocaleString('en-US');
}

/**
 * Format bytes as human-readable size
 * @param {number} bytes - Number of bytes
 * @param {number} decimals - Number of decimal places (default 2)
 * @returns {string} - Formatted size (e.g., "1.5 MB")
 */
function formatBytes(bytes, decimals = 2) {
    if (bytes === 0) return '0 Bytes';
    
    const k = 1024;
    const dm = decimals < 0 ? 0 : decimals;
    const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB'];
    
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    
    return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + ' ' + sizes[i];
}

// ============================================================================
// DEBOUNCE UTILITY
// ============================================================================

/**
 * Debounce a function call
 * @param {Function} func - Function to debounce
 * @param {number} wait - Delay in milliseconds
 * @returns {Function} - Debounced function
 */
function debounce(func, wait) {
    let timeout;
    return function executedFunction(...args) {
        const later = () => {
            clearTimeout(timeout);
            func(...args);
        };
        clearTimeout(timeout);
        timeout = setTimeout(later, wait);
    };
}

// Export shared helpers under a single namespaced object to avoid global
// symbol collisions when agent and server bundles are loaded together (proxied).
// Consumers should prefer `window.__pm_shared.<fn>` instead of global functions.
(function exportSharedNamespace() {
    try {
        if (typeof window === 'undefined') return;
        window.__pm_shared = window.__pm_shared || {};
        // Export commonly used helpers
        window.__pm_shared.showToast = window.__pm_shared.showToast || showToast;
        window.__pm_shared.showConfirm = window.__pm_shared.showConfirm || showConfirm;
        window.__pm_shared.showAlert = window.__pm_shared.showAlert || showAlert;
        window.__pm_shared.copyToClipboard = window.__pm_shared.copyToClipboard || copyToClipboard;
        window.__pm_shared.formatDateTime = window.__pm_shared.formatDateTime || formatDateTime;
        window.__pm_shared.formatRelativeTime = window.__pm_shared.formatRelativeTime || formatRelativeTime;
        window.__pm_shared.formatNumber = window.__pm_shared.formatNumber || formatNumber;
        window.__pm_shared.formatBytes = window.__pm_shared.formatBytes || formatBytes;
        window.__pm_shared.debounce = window.__pm_shared.debounce || debounce;
    window.__pm_shared.showPrompt = window.__pm_shared.showPrompt || showPrompt;
        // Note: we intentionally do NOT overwrite global `window.showToast` etc.
        // This avoids clobbering other bundles and makes callers opt-in to the
        // shared namespace for safer cross-bundle interaction.
    } catch (e) {
        // Non-fatal: best-effort namespacing
        try { console.warn('Failed to export __pm_shared namespace', e); } catch (e2) {}
    }
})();

// Backwards-compatible global exports removed. Consumers should use the
// namespaced `window.__pm_shared` API (e.g. window.__pm_shared.showToast)
// to avoid global symbol collisions when agent and server bundles are
// loaded together via proxy.

// ============================================================================
// SHARED METRICS MODAL
// ============================================================================
/**
 * Show a shared metrics modal.
 * Options:
 *  - serial: device serial (optional)
 *  - onFetch: async function({serial, from, to}) -> { data: [...] }
 */
window.showMetricsModal = async function (opts = {}) {
    opts = opts || {};
    const serial = opts.serial || null;
    const onFetch = opts.onFetch || (async ({serial, from, to}) => {
        // default fetch uses server API if available
        const qs = [];
        if (serial) qs.push('serial=' + encodeURIComponent(serial));
        if (from) qs.push('from=' + encodeURIComponent(new Date(from).toISOString()));
        if (to) qs.push('to=' + encodeURIComponent(new Date(to).toISOString()));
        const url = '/api/devices/metrics/history' + (qs.length ? ('?' + qs.join('&')) : '');
        const res = await fetch(url);
        if (!res.ok) throw new Error('Failed to fetch metrics');
        return res.json();
    });

    // If the richer metrics UI is available (shared metrics bundle), prefer it
    if (typeof window.loadDeviceMetrics === 'function' || (window.__pm_shared_metrics && typeof window.__pm_shared_metrics.loadDeviceMetrics === 'function')) {
        // Create a modal container that the shared metrics loader understands
        let modal = document.getElementById('metrics_modal');
        if (!modal) {
            modal = document.createElement('div');
            modal.id = 'metrics_modal';
            modal.className = 'modal';
            modal.style.display = 'none';
            modal.innerHTML = `
                <div class="modal-content" style="max-width:900px;">
                    <div class="modal-header">
                        <span class="modal-title" id="metrics_modal_title">Metrics</span>
                        <span class="modal-close" id="metrics_modal_close_x">&times;</span>
                    </div>
                    <div class="modal-body" id="metrics_modal_body" style="max-height:60vh;overflow:auto;padding:16px;">
                        <div id="metrics_modal_content" style="font-size:13px;color:var(--muted)">Loading...</div>
                    </div>
                    <div class="modal-footer">
                        <button class="modal-button modal-button-secondary" id="metrics_modal_close">Close</button>
                    </div>
                </div>
            `;
            document.body.appendChild(modal);

            // close handlers
            modal.querySelector('#metrics_modal_close_x').addEventListener('click', () => modal.style.display = 'none');
            modal.querySelector('#metrics_modal_close').addEventListener('click', () => modal.style.display = 'none');
            modal.addEventListener('click', (e) => { if (e.target === modal) modal.style.display = 'none'; });
        }

        const titleEl = document.getElementById('metrics_modal_title');
        const contentEl = document.getElementById('metrics_modal_content');
        titleEl.textContent = serial ? `Metrics: ${serial}` : 'Metrics';
        contentEl.innerHTML = '<div style="color:var(--muted);">Loading metrics...</div>';

        // Show modal then delegate rendering to the shared metrics loader
        modal.style.display = 'flex';
        try {
            // Prefer the explicit exported shared implementation if present
            const loader = (window.__pm_shared_metrics && window.__pm_shared_metrics.loadDeviceMetrics) ? window.__pm_shared_metrics.loadDeviceMetrics : window.loadDeviceMetrics;
            // Call loader to render the full metrics UI into the modal content
            // Use a short timeout so the modal becomes visible before heavy work
            setTimeout(() => {
                try { loader(serial, 'metrics_modal_content'); } catch (e) { console.warn('metrics loader failed', e); }
            }, 60);
        } catch (e) {
            console.warn('Failed to invoke shared metrics loader', e);
        }

        return; // done
    }

    // Initialize flatpickr on range input if available
    try {
        if (typeof flatpickr === 'function') {
            // If already initialized, destroy first (safety)
            if (rangeInput._flatpickr) rangeInput._flatpickr.destroy();
            const fpInstance = flatpickr(rangeInput, {
                mode: 'range',
                enableTime: true,
                dateFormat: 'Y-m-d H:i',
                defaultDate: [new Date(Date.now() - 7 * 24 * 3600 * 1000), new Date()],
            });
            // Expose a lightweight handle for callers that expect a global reference
            window.metricsDataRange = window.metricsDataRange || { min: null, max: null, serial: serial, flatpickr: null };
            try { window.metricsDataRange.flatpickr = fpInstance; } catch (e) { /* ignore */ }

            // Honor preset option (e.g., '7day') by setting a sensible selection
            if (opts && opts.preset === '7day') {
                try {
                    const maxTime = new Date();
                    const start = new Date(maxTime.getTime() - 7 * 24 * 60 * 60 * 1000);
                    if (typeof fpInstance.setDate === 'function') fpInstance.setDate([start, maxTime], true);
                } catch (e) { /* ignore preset failure */ }
            }
        }
    } catch (e) {
        console.warn('flatpickr init failed in shared metrics modal', e);
    }

    async function doLoad() {
        contentEl.innerHTML = '<div style="color:var(--muted);">Loading metrics...</div>';
        let from = null, to = null;
        try {
            const val = rangeInput.value || '';
            if (val.includes(' to ')) {
                const parts = val.split(' to ');
                from = new Date(parts[0]);
                to = new Date(parts[1]);
            } else if (val) {
                from = new Date(val);
                to = new Date();
            }
        } catch (e) { /* ignore parse errors */ }

        try {
            const data = await onFetch({ serial, from, to });
            // If API returned array directly, use it
            let items = data;
            if (data && data.history) items = data.history;
            if (!items || items.length === 0) {
                contentEl.innerHTML = '<div style="color:var(--muted);">No metrics available for selected range.</div>';
                return;
            }

            // Render simple list
            let html = '<div style="display:flex;flex-direction:column;gap:8px;">';
            items.forEach(it => {
                const t = it.timestamp ? new Date(it.timestamp).toLocaleString() : 'N/A';
                html += `<div style="padding:8px;background:rgba(0,0,0,0.03);border-radius:6px;"><strong>${it.serial || it.Serial || ''}</strong> — ${t} — pages: ${it.page_count || it.pageCount || 'n/a'}</div>`;
            });
            html += '</div>';
            contentEl.innerHTML = html;
        } catch (err) {
            console.error('Metrics fetch failed', err);
            contentEl.innerHTML = `<div style="color:var(--error);">Failed to load metrics: ${err.message || err}</div>`;
        }
    }

    refreshBtn.addEventListener('click', doLoad);

    // show modal and trigger initial load
    modal.style.display = 'flex';
    setTimeout(doLoad, 50);
};

