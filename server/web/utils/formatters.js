// PrintMaster Server - Formatting Utilities
// Extracted from app.js for modularity

/**
 * Escape HTML special characters to prevent XSS
 * @param {string} s - String to escape
 * @returns {string} Escaped string safe for HTML insertion
 */
function escapeHtml(s) {
    if (!s) return '';
    return String(s).replace(/[&<>"']/g, function(m) {
        return ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[m];
    });
}

/**
 * Format bytes into human-readable size (B, KB, MB, GB, TB)
 * @param {number} bytes - Number of bytes
 * @returns {string} Formatted string like "1.5 MB"
 */
function formatBytes(bytes) {
    let value = Number(bytes);
    if (!isFinite(value) || value <= 0) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let unitIdx = 0;
    while (value >= 1024 && unitIdx < units.length - 1) {
        value /= 1024;
        unitIdx++;
    }
    const precision = value >= 10 || unitIdx === 0 ? 0 : 1;
    return value.toFixed(precision) + ' ' + units[unitIdx];
}

/**
 * Format a date/time value to locale string
 * @param {string|Date|number} value - Date value (ISO string, Date object, or timestamp)
 * @returns {string} Formatted date/time string or "—" if invalid
 */
function formatDateTime(value) {
    if (!value) return '—';
    const d = new Date(value);
    if (isNaN(d.getTime())) return '—';
    return d.toLocaleString();
}

/**
 * Format a date/time value as relative time (e.g., "5m ago", "2h ago")
 * @param {string|Date|number} value - Date value
 * @returns {string} Relative time string or "—" if invalid
 */
function formatRelativeTime(value) {
    if (!value) return '—';
    const d = new Date(value);
    if (isNaN(d.getTime())) return '—';
    const diffMs = Date.now() - d.getTime();
    if (diffMs < 0) return 'just now';
    const minutes = Math.floor(diffMs / 60000);
    if (minutes < 1) return 'just now';
    if (minutes < 60) return minutes + 'm ago';
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return hours + 'h ago';
    const days = Math.floor(hours / 24);
    return days + 'd ago';
}

/**
 * Format a number with locale-specific formatting
 * @param {number|string} value - The value to format
 * @returns {string} Formatted number or "—" if invalid
 */
function formatNumber(value) {
    if (typeof value === 'number' && isFinite(value)) {
        return value.toLocaleString();
    }
    if (typeof value === 'string' && value.trim() !== '') {
        return value;
    }
    return '—';
}

/**
 * Format duration from milliseconds to human-readable string
 * @param {number} ms - Duration in milliseconds
 * @returns {string} Formatted duration like "5m", "2h", "3d"
 */
function formatDurationMs(ms) {
    if (ms < 60000) return `${Math.round(ms / 1000)}s`;
    if (ms < 3600000) return `${Math.round(ms / 60000)}m`;
    if (ms < 86400000) return `${Math.round(ms / 3600000)}h`;
    return `${Math.round(ms / 86400000)}d`;
}

/**
 * Format duration from seconds to human-readable string
 * @param {number} seconds - Duration in seconds
 * @returns {string} Formatted duration like "1d 5h" or "30m"
 */
function formatDurationSec(seconds) {
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

/**
 * Format a date as short date (MM/DD )
 * @param {Date} date - Date object
 * @returns {string} Formatted date like "12/20 "
 */
function formatDateShort(date) {
    if (!date || !(date instanceof Date) || isNaN(date.getTime())) return '';
    const month = String(date.getMonth() + 1).padStart(2, '0');
    const day = String(date.getDate()).padStart(2, '0');
    return `${month}/${day} `;
}

/**
 * Format a date as short time (HH:MM:SS)
 * @param {Date} date - Date object
 * @returns {string} Formatted time like "14:30:45"
 */
function formatTimeShort(date) {
    if (!date || !(date instanceof Date) || isNaN(date.getTime())) return '';
    const hours = String(date.getHours()).padStart(2, '0');
    const mins = String(date.getMinutes()).padStart(2, '0');
    const secs = String(date.getSeconds()).padStart(2, '0');
    return `${hours}:${mins}:${secs}`;
}

/**
 * Format an ISO datetime string for datetime-local input
 * @param {string} isoString - ISO 8601 date string
 * @returns {string} Formatted string for datetime-local input (YYYY-MM-DDTHH:MM)
 */
function formatDatetimeLocal(isoString) {
    if (!isoString) return '';
    const d = new Date(isoString);
    if (isNaN(d.getTime())) return '';
    const yyyy = d.getFullYear();
    const mm = String(d.getMonth() + 1).padStart(2, '0');
    const dd = String(d.getDate()).padStart(2, '0');
    const hh = String(d.getHours()).padStart(2, '0');
    const min = String(d.getMinutes()).padStart(2, '0');
    return `${yyyy}-${mm}-${dd}T${hh}:${min}`;
}

/**
 * Compare two semantic version strings.
 * @param {string} a - First version string (e.g., "0.10.18")
 * @param {string} b - Second version string
 * @returns {number} 1 if a > b, -1 if a < b, 0 if equal
 */
function compareVersions(a, b) {
    if (!a && !b) return 0;
    if (!a) return -1;
    if (!b) return 1;

    // Strip any suffix like "-dev", "-beta", etc. for comparison
    const cleanA = String(a).replace(/-.*$/, '');
    const cleanB = String(b).replace(/-.*$/, '');

    const partsA = cleanA.split('.').map(p => parseInt(p, 10) || 0);
    const partsB = cleanB.split('.').map(p => parseInt(p, 10) || 0);

    // Pad arrays to same length
    const maxLen = Math.max(partsA.length, partsB.length);
    while (partsA.length < maxLen) partsA.push(0);
    while (partsB.length < maxLen) partsB.push(0);

    for (let i = 0; i < maxLen; i++) {
        if (partsA[i] > partsB[i]) return 1;
        if (partsA[i] < partsB[i]) return -1;
    }
    return 0;
}

// Export for use in app.js (these become globals when loaded via <script>)
if (typeof window !== 'undefined') {
    window.escapeHtml = escapeHtml;
    window.formatBytes = formatBytes;
    window.formatDateTime = formatDateTime;
    window.formatRelativeTime = formatRelativeTime;
    window.formatNumber = formatNumber;
    window.formatDurationMs = formatDurationMs;
    window.formatDurationSec = formatDurationSec;
    window.formatDateShort = formatDateShort;
    window.formatTimeShort = formatTimeShort;
    window.formatDatetimeLocal = formatDatetimeLocal;
    window.compareVersions = compareVersions;
}
