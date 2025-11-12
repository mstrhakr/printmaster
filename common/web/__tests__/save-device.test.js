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
        await expect(window.__pm_shared.saveDiscoveredDevice('SN123', false, false)).resolves.toBeUndefined();
        expect(global.fetch).toHaveBeenCalledWith('/devices/save', expect.objectContaining({ method: 'POST' }));
    });

    test('throws on failed save', async () => {
        global.fetch = jest.fn().mockResolvedValue({ ok: false, status: 500, text: async () => 'boom' });
        await expect(window.__pm_shared.saveDiscoveredDevice('SN123', false, false)).rejects.toThrow(/Failed to save device/);
    });
});
