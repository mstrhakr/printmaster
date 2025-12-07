const fs = require('fs');
const path = require('path');
const { JSDOM } = require('jsdom');

const loginHtml = fs.readFileSync(path.join(__dirname, '..', 'login.html'), 'utf8');

function mockLocation(win, url) {
    const parsed = new URL(url);
    const state = { lastAssigned: null };
    Object.defineProperty(win, 'location', {
        configurable: true,
        get() {
            return {
                href: parsed.href,
                origin: parsed.origin,
                pathname: parsed.pathname,
                search: parsed.search,
            };
        },
        set(value) {
            state.lastAssigned = value;
        }
    });
    return () => state.lastAssigned;
}

function mockFetchResponse(body, ok = true) {
    return {
        ok,
        json: async () => body,
        text: async () => (typeof body === 'string' ? body : JSON.stringify(body)),
    };
}

function setupDom(query = '') {
    const url = 'https://pm.local/login' + query;
    const dom = new JSDOM(loginHtml, {
        url,
        pretendToBeVisual: true,
        runScripts: 'dangerously',
    });

    global.window = dom.window;
    global.document = dom.window.document;

    const navWatcher = mockLocation(dom.window, url);
    dom.window.fetch = jest.fn();
    global.fetch = dom.window.fetch;

    jest.resetModules();
    require('../login.js');

    const triggerInit = () => {
        if (dom.window.__pmLogin && typeof dom.window.__pmLogin.init === 'function') {
            dom.window.__pmLogin.init();
        } else {
            dom.window.document.dispatchEvent(new dom.window.Event('DOMContentLoaded', { bubbles: true }));
        }
    };

    return { window: dom.window, triggerInit, navWatcher };
}

function queueFetchResponses(win, responses) {
    win.fetch.mockImplementation(() => {
        if (!responses.length) {
            return Promise.reject(new Error('No fetch response queued'));
        }
        return Promise.resolve(responses.shift());
    });
}

async function flushPromises() {
    await Promise.resolve();
    await new Promise(resolve => setTimeout(resolve, 0));
}

describe('login page behavior', () => {
    afterEach(() => {
        jest.clearAllMocks();
        jest.resetModules();
        delete global.window;
        delete global.document;
        delete global.fetch;
    });

    test('renders SSO providers and triggers start endpoint', async () => {
        const { window, triggerInit, navWatcher } = setupDom('?tenant=acme');
        queueFetchResponses(window, [mockFetchResponse({
            local_login: false,
            providers: [
                { slug: 'entra', display_name: 'Microsoft Entra', button_text: 'Entra ID', auto_login: false }
            ],
        })]);

        triggerInit();
        await flushPromises();

        const button = window.document.querySelector('#sso_buttons button');
        expect(button).not.toBeNull();
        expect(button.textContent).toContain('Entra');

        button.click();
        expect(navWatcher()).toContain('/auth/oidc/start/entra');
        expect(navWatcher()).toContain('tenant=acme');
    });

    test('auto-login provider redirects immediately when not skipped', async () => {
        const { window, triggerInit, navWatcher } = setupDom('?tenant=acme');
        queueFetchResponses(window, [mockFetchResponse({
            local_login: false,
            providers: [
                { slug: 'auto', display_name: 'Auto', auto_login: true }
            ],
        })]);

        triggerInit();
        await flushPromises();

        expect(navWatcher()).toContain('/auth/oidc/start/auto');
    });

    test('local login submits credentials and redirects to target', async () => {
        const { window, triggerInit, navWatcher } = setupDom('?redirect=%2Fapp');
        queueFetchResponses(window, [
            mockFetchResponse({ local_login: true, providers: [] }),
            { ok: true, json: async () => ({}), text: async () => '' },
        ]);

        triggerInit();
        await flushPromises();

        window.document.getElementById('login_username').value = 'admin';
        window.document.getElementById('login_password').value = 'secret';

        window.document.getElementById('login_submit').click();
        await flushPromises();

        expect(navWatcher()).toBe('/app');
    });

    test('error query parameter shows inline message', async () => {
        const { window, triggerInit } = setupDom('?error=oidc_user');
        queueFetchResponses(window, [mockFetchResponse({ local_login: true, providers: [] })]);

        triggerInit();
        await flushPromises();

        const errorEl = window.document.getElementById('login_error');
        expect(errorEl.style.display).toBe('block');
        expect(errorEl.textContent).toContain('We could not create or find a user');
    });

    test('tenant lookup stays hidden with one provider', async () => {
        const { window, triggerInit } = setupDom('');
        queueFetchResponses(window, [mockFetchResponse({
            local_login: false,
            providers: [ { slug: 'one', display_name: 'OneID' } ],
        })]);

        triggerInit();
        await flushPromises();

        const lookup = window.document.getElementById('tenant_lookup_section');
        expect(lookup.style.display).toBe('none');
    });

    test('tenant lookup shows when multiple providers available', async () => {
        const { window, triggerInit } = setupDom('');
        queueFetchResponses(window, [mockFetchResponse({
            local_login: false,
            providers: [
                { slug: 'p1', display_name: 'Provider 1' },
                { slug: 'p2', display_name: 'Provider 2' },
            ],
        })]);

        triggerInit();
        await flushPromises();

        const lookup = window.document.getElementById('tenant_lookup_section');
        expect(lookup.style.display).toBe('block');
    });
});
