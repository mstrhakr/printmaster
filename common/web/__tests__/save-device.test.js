/** @jest-environment jsdom */

require('../shared.js');

describe('saveDiscoveredDevice', () => {
    beforeEach(() => {
        document.body.innerHTML = '';
        // create expected inputs
        document.body.innerHTML = `
            <input id="save_device_name" />
            <input id="save_device_serial" />
            <input id="save_device_addr" />
            <button id="save_device_button"></button>
        `;
    });

    test('sends POST to /devices/save for serial', async () => {
        // mock fetch for POST
        global.fetch = jest.fn().mockResolvedValue({ ok: true, status: 200, text: async () => '' });
        // Provide an agent implementation so the shared delegator can call into it
        window.__agent_saveDiscoveredDevice = async (ipOrSerial, autosave, updateUI) => {
            const resp = await fetch('/devices/save', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ serial: ipOrSerial }) });
            if (!resp.ok) throw new Error('Failed to save device');
            return;
        };
        await expect(window.__pm_shared.saveDiscoveredDevice('SN123', false, false)).resolves.toBeUndefined();
        expect(global.fetch).toHaveBeenCalledWith('/devices/save', expect.objectContaining({ method: 'POST' }));
    });

    test('throws on failed save', async () => {
        // Suppress expected error logging during this test
        const origError = window.__pm_shared.error;
        window.__pm_shared.error = jest.fn();

        global.fetch = jest.fn().mockResolvedValue({ ok: false, status: 500, text: async () => 'boom' });
        window.__agent_saveDiscoveredDevice = async (ipOrSerial, autosave, updateUI) => {
            const resp = await fetch('/devices/save', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ serial: ipOrSerial }) });
            if (!resp.ok) {
                const t = await resp.text();
                throw new Error('Failed to save device: ' + t + ' (status ' + resp.status + ')');
            }
            return;
        };
        await expect(window.__pm_shared.saveDiscoveredDevice('SN123', false, false)).rejects.toThrow(/Failed to save device/);

        window.__pm_shared.error = origError;
    });
});
