/** @jest-environment jsdom */

require('../shared.js'); // load shared.js which attaches to window

describe('DOM shims (showToast, showConfirm, showPrompt, toggleDatabaseFields)', () => {
    beforeEach(() => {
        document.body.innerHTML = '';
        // ensure no lingering flags
        window.__pm_confirm_open = false;
    });

    test('showToast appends and removes toast', () => {
        jest.useFakeTimers();
        const container = document.createElement('div');
        container.id = 'toast_container';
        document.body.appendChild(container);

        // call via namespaced API
        window.__pm_shared.showToast('hello', 'success', 500);
        const toast = container.querySelector('.toast');
        expect(toast).not.toBeNull();
        expect(toast.querySelector('.toast-message').textContent).toBe('hello');

        // advance time to let it auto-remove
        jest.advanceTimersByTime(500 + 400); // include animation delay
        expect(container.querySelector('.toast')).toBeNull();
        jest.useRealTimers();
    });

    test('showConfirm resolves true on confirm click', async () => {
        // create DOM elements expected by showConfirm
        document.body.innerHTML = `
            <div id="confirm_modal" style="display:none">
                <div id="confirm_modal_title"></div>
                <div id="confirm_modal_message"></div>
                <button id="confirm_modal_confirm">OK</button>
                <button id="confirm_modal_cancel">Cancel</button>
                <button id="confirm_modal_close_x">X</button>
            </div>
        `;

        const p = window.__pm_shared.showConfirm('Are you sure?', 'Please');
        // modal should be visible
        const modal = document.getElementById('confirm_modal');
        expect(modal.style.display).toBe('flex');

        // simulate click confirm
        document.getElementById('confirm_modal_confirm').click();
        const res = await p;
        expect(res).toBe(true);
    });

    test('showPrompt creates modal and resolves input', async () => {
        // No prompt exists; function should create one
        const promise = window.__pm_shared.showPrompt('Enter name', 'bob', 'Title');
        // prompt modal should exist now
        const input = document.getElementById('prompt_modal_input');
        expect(input).not.toBeNull();
        input.value = 'alice';
        document.getElementById('prompt_modal_ok').click();
        const val = await promise;
        expect(val).toBe('alice');
    });

    test('toggleDatabaseFields shows and hides appropriate groups', () => {
        // create selector and groups
        document.body.innerHTML = `
            <select id="db_backend_type"><option value="sqlite">sqlite</option><option value="postgresql">postgresql</option></select>
            <div id="db_sqlite_fields"></div>
            <div id="db_postgresql_fields"></div>
            <div id="clear_db_section"></div>
        `;
        const sel = document.getElementById('db_backend_type');
        sel.value = 'postgresql';
        window.__pm_shared.toggleDatabaseFields();
        expect(document.getElementById('db_sqlite_fields').style.display).toBe('none');
        expect(document.getElementById('db_postgresql_fields').style.display).toBe('flex');
        expect(document.getElementById('clear_db_section').style.display).toBe('none');

        sel.value = 'sqlite';
        window.__pm_shared.toggleDatabaseFields();
        expect(document.getElementById('clear_db_section').style.display).toBe('block');
    });
});
