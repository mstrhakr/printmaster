// Testable shared helper module (CommonJS) - exports pure functions used by shared.js

function formatDateTime(dateString) {
    if (!dateString) return 'Never';
    try {
        const date = new Date(dateString);
        if (isNaN(date.getTime())) return 'Invalid date';
        return date.toLocaleString('en-US', {
            month: 'short',
            day: 'numeric',
            year: 'numeric',
            hour: 'numeric',
            minute: '2-digit',
            hour12: true
        });
    } catch (err) {
        return dateString;
    }
}

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
        return dateString;
    }
}

function formatNumber(num) {
    if (num === null || num === undefined) return '0';
    return num.toLocaleString('en-US');
}

function formatBytes(bytes, decimals = 2) {
    if (bytes === 0) return '0 Bytes';
    const k = 1024;
    const dm = decimals < 0 ? 0 : decimals;
    const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + ' ' + sizes[i];
}

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

module.exports = {
    formatDateTime,
    formatRelativeTime,
    formatNumber,
    formatBytes,
    debounce
};
