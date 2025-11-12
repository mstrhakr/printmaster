const helpers = require('../shared_helpers');

describe('formatters', () => {
    test('formatDateTime: invalid returns Invalid date', () => {
        expect(helpers.formatDateTime('not-a-date')).toBe('Invalid date');
    });

    test('formatDateTime: empty returns Never', () => {
        expect(helpers.formatDateTime('')).toBe('Never');
    });

    test('formatNumber: formats large numbers', () => {
        expect(helpers.formatNumber(1234567)).toBe('1,234,567');
    });

    test('formatBytes: 1536 -> 1.5 KB approx', () => {
        const out = helpers.formatBytes(1536,1);
        expect(out).toMatch(/1\.5\sKB/);
    });

    test('formatRelativeTime: just now for recent', () => {
        const now = new Date().toISOString();
        expect(helpers.formatRelativeTime(now)).toBe('Just now');
    });
});
