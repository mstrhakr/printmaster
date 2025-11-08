// PrintMaster Shared JavaScript - Common utilities for Agent and Server UIs

// Load shared vendor assets (flatpickr) from server-hosted copy if available,
// otherwise fall back to CDN. This centralizes the import so agent and server
// UIs get the same vendor script without duplicating <script> tags in each
// HTML file.
(function loadFlatpickr() {
    const serverCss = '/static/vendor/flatpickr/flatpickr.min.css';
    const serverJs = '/static/vendor/flatpickr/flatpickr.min.js';
    const cdnCss = 'https://cdn.jsdelivr.net/npm/flatpickr/dist/flatpickr.min.css';
    const cdnJs = 'https://cdn.jsdelivr.net/npm/flatpickr';

    // Helper to insert a stylesheet
    const insertCss = (href) => {
        try {
            const link = document.createElement('link');
            link.rel = 'stylesheet';
            link.href = href;
            document.head.appendChild(link);
        } catch (e) {
            console.error('Failed to insert flatpickr CSS:', e);
        }
    };

    // Helper to insert a script
    const insertJs = (src) => {
        try {
            const s = document.createElement('script');
            s.src = src;
            s.defer = true;
            document.head.appendChild(s);
        } catch (e) {
            console.error('Failed to insert flatpickr JS:', e);
        }
    };

    // Try server-hosted CSS/JS first (HEAD request), fall back to CDN on error.
    fetch(serverCss, { method: 'HEAD' }).then(r => {
        if (r.ok) insertCss(serverCss); else insertCss(cdnCss);
    }).catch(() => insertCss(cdnCss));

    fetch(serverJs, { method: 'HEAD' }).then(r => {
        if (r.ok) insertJs(serverJs); else insertJs(cdnJs);
    }).catch(() => insertJs(cdnJs));
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
function copyToClipboard(text) {
    if (!text) return;
    
    navigator.clipboard.writeText(text).then(() => {
        showToast('Copied to clipboard', 'success', 1500);
    }).catch(err => {
        console.error('Failed to copy:', err);
        showToast('Failed to copy to clipboard', 'error');
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

// Export to global scope for backward compatibility
window.showToast = showToast;
window.showConfirm = showConfirm;
window.showAlert = showAlert;
window.copyToClipboard = copyToClipboard;
window.formatDateTime = formatDateTime;
window.formatRelativeTime = formatRelativeTime;
window.formatNumber = formatNumber;
window.formatBytes = formatBytes;
window.debounce = debounce;
