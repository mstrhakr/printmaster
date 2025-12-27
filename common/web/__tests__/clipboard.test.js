/** @jest-environment jsdom */

require('../shared.js');

describe('clipboard helpers', () => {
    beforeEach(() => {
        document.body.innerHTML = '';
        window.navigator.clipboard = { writeText: jest.fn().mockResolvedValue(true) };
    });

    test('copyToClipboard writes text and returns true', async () => {
        const res = await window.__pm_shared.copyToClipboard('abc');
        expect(window.navigator.clipboard.writeText).toHaveBeenCalledWith('abc');
        expect(res).toBe(true);
    });

    test('copyToClipboard handles rejection and returns false', async () => {
        // Suppress expected error logging during this test
        const origError = window.__pm_shared.error;
        window.__pm_shared.error = jest.fn();
        
        window.navigator.clipboard.writeText.mockRejectedValueOnce(new Error('fail'));
        const res = await window.__pm_shared.copyToClipboard('abc');
        expect(res).toBe(false);
        
        window.__pm_shared.error = origError;
    });
});
