(function(){
    if (typeof window === 'undefined') {
        return;
    }

    const ENTRA_PORTAL_URL = 'https://portal.azure.com/#view/Microsoft_AAD_RegisteredApps/CreateApplicationBlade/quickStartType~/null/isMSAApp~/false';
    const ENTRA_ICON_DATA_URI = 'data:image/svg+xml,%3Csvg xmlns%3D%22http://www.w3.org/2000/svg%22 viewBox%3D%220 0 24 24%22%3E%3Crect width%3D%2210%22 height%3D%2210%22 x%3D%221%22 y%3D%221%22 fill%3D%22%23f35325%22/%3E%3Crect width%3D%2210%22 height%3D%2210%22 x%3D%2213%22 y%3D%221%22 fill%3D%22%2381bc06%22/%3E%3Crect width%3D%2210%22 height%3D%2210%22 x%3D%221%22 y%3D%2213%22 fill%3D%22%2305a6f0%22/%3E%3Crect width%3D%2210%22 height%3D%2210%22 x%3D%2213%22 y%3D%2213%22 fill%3D%22%23ffba08%22/%3E%3C/svg%3E';

    const state = {
        initialized: false,
        providers: [],
        tenants: [],
        redirectUri: '',
        preset: 'generic',
        lastAutoSlug: '',
        lastAutoButtonText: '',
    };

    function qs(id) {
        return document.getElementById(id);
    }

    function escapeHtml(value) {
        if (value === null || value === undefined) {
            return '';
        }
        return String(value).replace(/[&<>"']/g, function(chr) {
            switch (chr) {
                case '&': return '&amp;';
                case '<': return '&lt;';
                case '>': return '&gt;';
                case '"': return '&quot;';
                case "'": return '&#39;';
                default: return chr;
            }
        });
    }

    function sharedToast(message, type) {
        try {
            if (window.__pm_shared && typeof window.__pm_shared.showToast === 'function') {
                window.__pm_shared.showToast(message, type || 'info');
            }
        } catch (err) {
            console.warn(message);
        }
    }

    function sharedAlert(message, title, isDanger) {
        if (window.__pm_shared && typeof window.__pm_shared.showAlert === 'function') {
            window.__pm_shared.showAlert(message, title || 'Notice', !!isDanger, false);
        } else {
            alert(message);
        }
    }

    function computeRedirectUri() {
        try {
            if (typeof window !== 'undefined' && window.location && window.location.origin) {
                return window.location.origin.replace(/\/+$/, '') + '/auth/oidc/callback';
            }
            if (typeof window !== 'undefined' && window.location && window.location.protocol) {
                return window.location.protocol + '//' + window.location.host + '/auth/oidc/callback';
            }
        } catch (err) {
            console.warn('Failed to compute redirect URI', err);
        }
        return '/auth/oidc/callback';
    }

    function updateRedirectHints() {
        const value = state.redirectUri || computeRedirectUri();
        const helper = qs('sso_redirect_uri_text');
        if (helper) {
            helper.textContent = value;
        }
        const modal = qs('sso_modal_redirect_uri_text');
        if (modal) {
            modal.textContent = value;
        }
    }

    function copyRedirectUri() {
        const value = state.redirectUri || computeRedirectUri();
        if (!value) {
            return;
        }
        if (typeof navigator !== 'undefined' && navigator.clipboard && typeof navigator.clipboard.writeText === 'function') {
            navigator.clipboard.writeText(value).then(() => {
                sharedToast('Redirect URI copied', 'success');
            }).catch(() => {
                if (fallbackCopyToClipboard(value)) {
                    sharedToast('Redirect URI copied', 'success');
                } else {
                    sharedAlert(value, 'Redirect URI');
                }
            });
            return;
        }
        if (fallbackCopyToClipboard(value)) {
            sharedToast('Redirect URI copied', 'success');
            return;
        }
        sharedAlert(value, 'Redirect URI');
    }

    function fallbackCopyToClipboard(text) {
        try {
            const textarea = document.createElement('textarea');
            textarea.value = text;
            textarea.setAttribute('readonly', 'readonly');
            textarea.style.position = 'absolute';
            textarea.style.left = '-9999px';
            document.body.appendChild(textarea);
            textarea.select();
            const copied = document.execCommand ? document.execCommand('copy') : false;
            document.body.removeChild(textarea);
            return copied;
        } catch (err) {
            console.warn('Copy fallback failed', err);
            return false;
        }
    }

    function setupPresetControls() {
        const templateSelect = qs('sso_template_select');
        if (templateSelect && !templateSelect.dataset.boundPreset) {
            templateSelect.dataset.boundPreset = 'true';
            templateSelect.addEventListener('change', () => {
                setPreset(templateSelect.value || 'generic');
            });
        }
        const tenantInput = qs('sso_entra_tenant');
        if (tenantInput && !tenantInput.dataset.boundTenant) {
            tenantInput.dataset.boundTenant = 'true';
            tenantInput.addEventListener('input', () => {
                updateTenantDerivedFields();
            });
        }
        const slugInput = qs('sso_slug');
        if (slugInput && !slugInput.dataset.boundSlug) {
            slugInput.dataset.boundSlug = 'true';
            slugInput.addEventListener('input', () => {
                state.lastAutoSlug = slugInput.value;
            });
        }
    }

    function setPreset(value, options) {
        const normalized = value === 'entra' ? 'entra' : 'generic';
        state.preset = normalized;
        const templateSelect = qs('sso_template_select');
        if (templateSelect && templateSelect.value !== normalized) {
            templateSelect.value = normalized;
        }
        togglePresetSections();
        if (normalized === 'entra' && (!options || !options.skipDefaults)) {
            applyEntraDefaults(options && options.forceDefaults);
        }
    }

    function togglePresetSections() {
        const isEntra = state.preset === 'entra';
        document.querySelectorAll('.preset-generic').forEach((el) => {
            setPresetVisibility(el, !isEntra);
        });
        document.querySelectorAll('.preset-entra').forEach((el) => {
            setPresetVisibility(el, isEntra);
        });
    }

    function setPresetVisibility(element, shouldShow) {
        if (!element) {
            return;
        }
        if (shouldShow) {
            const target = element.dataset.display || '';
            element.style.display = target || '';
            return;
        }
        if (!element.dataset.display) {
            const computed = (window.getComputedStyle ? window.getComputedStyle(element).display : '') || '';
            if (computed && computed !== 'none') {
                element.dataset.display = computed;
            }
        }
        element.style.display = 'none';
    }

    function applyEntraDefaults(forceDefaults) {
        const displayInput = qs('sso_display_name');
        if (displayInput && (!displayInput.value || forceDefaults)) {
            displayInput.value = 'Microsoft Entra ID';
        }
        const buttonTextInput = qs('sso_button_text');
        const defaultButtonText = 'Sign in with Microsoft';
        if (buttonTextInput && (!buttonTextInput.value || buttonTextInput.value === state.lastAutoButtonText || forceDefaults)) {
            buttonTextInput.value = defaultButtonText;
            state.lastAutoButtonText = defaultButtonText;
        }
        const buttonStyleInput = qs('sso_button_style');
        if (buttonStyleInput && (!buttonStyleInput.value || forceDefaults)) {
            buttonStyleInput.value = 'btn-entra';
        }
        const iconInput = qs('sso_icon');
        if (iconInput && (!iconInput.value || forceDefaults)) {
            iconInput.value = ENTRA_ICON_DATA_URI;
        }
        ensureScopesIncludeOffline();
        updateTenantDerivedFields({ allowEmptyTenant: true, forceSlugUpdate: !!forceDefaults });
    }

    function ensureScopesIncludeOffline() {
        const scopesInput = qs('sso_scopes');
        if (!scopesInput) {
            return;
        }
        const scopes = scopesInput.value ? scopesInput.value.split(/\s+/).filter(Boolean) : [];
        if (scopes.indexOf('openid') === -1) {
            scopes.unshift('openid');
        }
        if (scopes.indexOf('profile') === -1) {
            scopes.push('profile');
        }
        if (scopes.indexOf('email') === -1) {
            scopes.push('email');
        }
        if (scopes.indexOf('offline_access') === -1) {
            scopes.push('offline_access');
        }
        scopesInput.value = Array.from(new Set(scopes)).join(' ');
    }

    function updateTenantDerivedFields(options) {
        if (state.preset !== 'entra') {
            return;
        }
        const tenantInput = qs('sso_entra_tenant');
        const tenantId = ((tenantInput && tenantInput.value) || '').trim();
        const issuerInput = qs('sso_issuer');
        if (issuerInput) {
            issuerInput.value = tenantId ? buildEntraIssuer(tenantId) : '';
        }
        if (tenantId) {
            maybeAutofillSlug(tenantId, options && options.forceSlugUpdate);
        } else if (options && options.allowEmptyTenant) {
            maybeAutofillSlug('', true);
        }
    }

    function maybeAutofillSlug(tenantId, force) {
        const slugInput = qs('sso_slug');
        if (!slugInput || slugInput.disabled) {
            return;
        }
        const generated = tenantId ? buildEntraSlug(tenantId) : 'entra';
        if (force || !slugInput.value || slugInput.value === state.lastAutoSlug) {
            slugInput.value = generated;
            state.lastAutoSlug = generated;
        }
    }

    function detectPresetFromProvider(provider) {
        if (!provider) {
            return 'generic';
        }
        const issuer = provider.issuer || '';
        if (/login\.microsoftonline\.com\/[^\s]+/i.test(issuer)) {
            return 'entra';
        }
        return 'generic';
    }

    function extractTenantFromIssuer(issuer) {
        if (!issuer) {
            return '';
        }
        const match = issuer.match(/login\.microsoftonline\.com\/([^/]+)\//i);
        return match ? match[1] : '';
    }

    function resetPresetState() {
        state.preset = 'generic';
        state.lastAutoSlug = '';
        state.lastAutoButtonText = '';
        const tenantInput = qs('sso_entra_tenant');
        if (tenantInput) {
            tenantInput.value = '';
        }
    }
    function buildEntraIssuer(tenantId) {
        const cleaned = (tenantId || '').trim();
        return 'https://login.microsoftonline.com/' + cleaned + '/v2.0';
    }

    function buildEntraSlug(tenantId) {
        let cleaned = (tenantId || '').toLowerCase().replace(/[^a-z0-9]+/g, '-');
        cleaned = cleaned.replace(/^-+|-+$/g, '');
        if (!cleaned) {
            cleaned = 'entra';
        }
        const suffix = cleaned.length > 24 ? cleaned.slice(0, 24) : cleaned;
        let slug = 'entra-' + suffix;
        slug = slug.replace(/-+/g, '-');
        if (slug.length > 64) {
            slug = slug.slice(0, 64);
        }
        return slug;
    }

    async function init() {
        if (state.initialized) {
            return;
        }
        state.initialized = true;

        const addBtn = qs('sso_add_provider_btn');
        if (addBtn) {
            addBtn.addEventListener('click', () => openModal());
        }

        const modal = qs('sso_modal');
        if (modal) {
            modal.addEventListener('click', (evt) => {
                if (evt.target === modal) {
                    closeModal();
                }
            });
        }

        const closeBtn = qs('sso_modal_close');
        if (closeBtn) {
            closeBtn.addEventListener('click', closeModal);
        }

        const cancelBtn = qs('sso_modal_cancel');
        if (cancelBtn) {
            cancelBtn.addEventListener('click', (evt) => {
                evt.preventDefault();
                closeModal();
            });
        }

        const form = qs('sso_modal_form');
        if (form) {
            form.addEventListener('submit', (evt) => {
                evt.preventDefault();
                submitProvider();
            });
        }

        state.redirectUri = computeRedirectUri();
        updateRedirectHints();

        const modalCopyBtn = qs('sso_modal_copy_redirect');
        if (modalCopyBtn) {
            modalCopyBtn.addEventListener('click', (evt) => {
                evt.preventDefault();
                copyRedirectUri();
            });
        }

        setupPresetControls();
        setPreset('generic', { skipDefaults: true });

        document.querySelectorAll('.sso-entra-portal-link').forEach((link) => {
            if (link) {
                link.href = ENTRA_PORTAL_URL;
            }
        });

        await loadProviders();
    }

    function syncTenants(list) {
        if (!Array.isArray(list)) {
            return;
        }
        state.tenants = list.slice();
    }

    async function loadProviders() {
        const container = qs('sso_providers_list');
        if (container) {
            container.innerHTML = '<div class="muted-text">Loading identity providers…</div>';
        }
        try {
            if (typeof fetch !== 'function') {
                throw new Error('fetch not available');
            }
            const resp = await fetch('/api/v1/sso/providers');
            if (!resp.ok) {
                throw new Error(await resp.text() || 'Failed to load providers');
            }
            state.providers = await resp.json();
            renderProviders();
        } catch (err) {
            if (container) {
                container.innerHTML = '<div class="error-text">' + escapeHtml(err && err.message ? err.message : 'Unable to load providers') + '</div>';
            }
        }
    }

    function renderProviders(list) {
        if (Array.isArray(list)) {
            state.providers = list.slice();
        }
        const container = qs('sso_providers_list');
        if (!container) {
            return;
        }
        if (!state.providers || state.providers.length === 0) {
            container.innerHTML = '<div class="muted-text">No single sign-on providers configured. Click "Add Provider" to connect your identity provider.</div>';
            return;
        }
        const rows = state.providers.map((p) => {
            const autoBadge = p.auto_login ? '<span class="badge badge-success">Auto</span>' : '<span class="badge badge-muted">Manual</span>';
            const scopePreview = (Array.isArray(p.scopes) && p.scopes.length > 0) ? escapeHtml(p.scopes.join(' ')) : 'openid profile email';
            const tenantName = p.tenant_id ? (window._tenants && window._tenants.find(t => (t.id || t.uuid || t.tenant_id) === p.tenant_id)?.name || p.tenant_id) : '';
            const tenantLabel = p.tenant_id ? ('Tenant: ' + escapeHtml(tenantName)) : 'Global';
            const buttonText = p.button_text || p.display_name || p.slug;
            return '<tr>' +
                '<td>' +
                    '<div class="sso-provider-name">' + escapeHtml(p.display_name || p.slug) + '</div>' +
                    '<div class="sso-provider-meta">Slug: ' + escapeHtml(p.slug) + ' • ' + escapeHtml(tenantLabel) + '</div>' +
                '</td>' +
                '<td>' + escapeHtml(p.issuer || '—') + '<div class="sso-provider-meta">Scopes: ' + scopePreview + '</div></td>' +
                '<td>' + escapeHtml(buttonText || 'Sign in') + '<div class="sso-provider-meta">Role: ' + escapeHtml(p.default_role || 'user') + '</div></td>' +
                '<td style="text-align:center;">' + autoBadge + '</td>' +
                '<td class="sso-actions">' +
                    '<button type="button" class="link-button" data-action="edit" data-slug="' + escapeHtml(p.slug) + '">Edit</button>' +
                    '<button type="button" class="link-button danger" data-action="delete" data-slug="' + escapeHtml(p.slug) + '">Delete</button>' +
                '</td>' +
            '</tr>';
        }).join('');
        container.innerHTML = '<div class="table-wrapper"><table class="simple-table"><thead><tr><th>Provider</th><th>Issuer & Scopes</th><th>Button & Role</th><th style="width:80px;text-align:center;">Auto</th><th style="width:160px;">Actions</th></tr></thead><tbody>' + rows + '</tbody></table></div>';
        container.querySelectorAll('[data-action="edit"]').forEach((btn) => {
            btn.addEventListener('click', () => {
                const slug = btn.getAttribute('data-slug');
                const provider = state.providers.find((p) => p.slug === slug);
                openModal(provider || null);
            });
        });
        container.querySelectorAll('[data-action="delete"]').forEach((btn) => {
            btn.addEventListener('click', () => {
                const slug = btn.getAttribute('data-slug');
                if (slug) {
                    deleteProvider(slug);
                }
            });
        });
    }

    async function ensureTenantsLoaded() {
        if (state.tenants && state.tenants.length > 0) {
            return state.tenants;
        }
        try {
            if (typeof fetch !== 'function') {
                return [];
            }
            const resp = await fetch('/api/v1/tenants');
            if (!resp.ok) {
                return [];
            }
            const data = await resp.json();
            state.tenants = Array.isArray(data) ? data : [];
            return state.tenants;
        } catch (err) {
            return [];
        }
    }

    async function populateTenantSelect(selectedValue) {
        const select = qs('sso_tenant');
        if (!select) {
            return;
        }
        const tenants = await ensureTenantsLoaded();
        select.innerHTML = '<option value="">Server default (global)</option>';
        tenants.forEach((tenant) => {
            const id = tenant.id || tenant.uuid;
            if (!id) {
                return;
            }
            const option = document.createElement('option');
            option.value = id;
            option.textContent = tenant.name ? tenant.name + ' (' + id + ')' : id;
            select.appendChild(option);
        });
        if (selectedValue) {
            select.value = selectedValue;
        }
    }

    function openModal(provider) {
        const modal = qs('sso_modal');
        if (!modal) {
            return;
        }
        const title = qs('sso_modal_title');
        const slugInput = qs('sso_slug');
        const displayInput = qs('sso_display_name');
        const issuerInput = qs('sso_issuer');
        const clientIdInput = qs('sso_client_id');
        const clientSecretInput = qs('sso_client_secret');
        const scopesInput = qs('sso_scopes');
        const buttonTextInput = qs('sso_button_text');
        const buttonStyleInput = qs('sso_button_style');
        const iconInput = qs('sso_icon');
        const autoCheckbox = qs('sso_auto_login');
        const roleSelect = qs('sso_default_role');
        const tenantSelect = qs('sso_tenant');
        const modalError = qs('sso_modal_error');
        if (modalError) {
            modalError.style.display = 'none';
        }

        resetPresetState();

        if (provider) {
            if (title) title.textContent = 'Edit OIDC Provider';
            slugInput.value = provider.slug || '';
            slugInput.disabled = true;
            displayInput.value = provider.display_name || provider.slug || '';
            issuerInput.value = provider.issuer || '';
            clientIdInput.value = provider.client_id || '';
            clientSecretInput.value = '';
            scopesInput.value = Array.isArray(provider.scopes) ? provider.scopes.join(' ') : '';
            buttonTextInput.value = provider.button_text || '';
            buttonStyleInput.value = provider.button_style || '';
            iconInput.value = provider.icon || '';
            autoCheckbox.checked = !!provider.auto_login;
            roleSelect.value = provider.default_role || 'user';
            modal.setAttribute('data-edit-slug', provider.slug || '');
            populateTenantSelect(provider.tenant_id || '');
        } else {
            if (title) title.textContent = 'Add OIDC Provider';
            slugInput.disabled = false;
            slugInput.value = '';
            displayInput.value = '';
            issuerInput.value = '';
            clientIdInput.value = '';
            clientSecretInput.value = '';
            scopesInput.value = 'openid profile email';
            buttonTextInput.value = '';
            buttonStyleInput.value = '';
            iconInput.value = '';
            autoCheckbox.checked = false;
            roleSelect.value = 'user';
            modal.removeAttribute('data-edit-slug');
            populateTenantSelect('');
        }

        const inferredPreset = detectPresetFromProvider(provider);
        const tenantInput = qs('sso_entra_tenant');
        if (provider && inferredPreset === 'entra' && tenantInput) {
            tenantInput.value = extractTenantFromIssuer(provider.issuer || '');
        }
        setPreset(inferredPreset, { skipDefaults: true });
        if (inferredPreset === 'entra') {
            updateTenantDerivedFields();
        }

        updateRedirectHints();
        modal.style.display = 'flex';
        displayInput.focus();
    }

    function closeModal() {
        const modal = qs('sso_modal');
        if (!modal) {
            return;
        }
        setModalSaving(false);
        modal.removeAttribute('data-edit-slug');
        modal.style.display = 'none';
        resetPresetState();
        setPreset('generic', { skipDefaults: true });
    }

    function parseScopes(value) {
        if (!value) {
            return ['openid', 'profile', 'email'];
        }
        const scopes = value.split(/[\s,]+/).map((entry) => entry.trim()).filter(Boolean);
        if (scopes.indexOf('openid') === -1) {
            scopes.unshift('openid');
        }
        return Array.from(new Set(scopes));
    }

    function showModalError(message) {
        const errorEl = qs('sso_modal_error');
        if (!errorEl) {
            return;
        }
        errorEl.textContent = message;
        errorEl.style.display = 'block';
    }

    function setModalSaving(isSaving) {
        const submitBtn = qs('sso_modal_submit');
        if (submitBtn) {
            submitBtn.disabled = !!isSaving;
            submitBtn.textContent = isSaving ? 'Saving…' : 'Save Provider';
        }
    }

    async function submitProvider() {
        const modal = qs('sso_modal');
        if (!modal) {
            return;
        }
        const slugInput = qs('sso_slug');
        const payload = {
            slug: (slugInput.value || '').trim(),
            display_name: (qs('sso_display_name').value || '').trim(),
            issuer: (qs('sso_issuer').value || '').trim(),
            client_id: (qs('sso_client_id').value || '').trim(),
            client_secret: (qs('sso_client_secret').value || '').trim(),
            scopes: parseScopes(qs('sso_scopes').value),
            button_text: (qs('sso_button_text').value || '').trim(),
            button_style: (qs('sso_button_style').value || '').trim(),
            icon: (qs('sso_icon').value || '').trim(),
            auto_login: !!qs('sso_auto_login').checked,
            tenant_id: (qs('sso_tenant').value || '').trim(),
            default_role: (qs('sso_default_role').value || 'user').trim(),
        };

        if (state.preset === 'entra') {
            const tenantInput = qs('sso_entra_tenant');
            const tenantId = ((tenantInput && tenantInput.value) || '').trim();
            if (!tenantId) {
                showModalError('Tenant (Directory) ID is required for Microsoft Entra ID presets.');
                if (tenantInput) {
                    tenantInput.focus();
                }
                return;
            }
            payload.issuer = buildEntraIssuer(tenantId);
        }

        if (!payload.display_name) {
            showModalError('Display name is required.');
            return;
        }
        if (!payload.slug) {
            showModalError('Slug is required.');
            return;
        }
        if (!payload.issuer) {
            showModalError('Issuer URL is required.');
            return;
        }
        if (!payload.client_id) {
            showModalError('Client ID is required.');
            return;
        }

        if (!payload.client_secret) {
            delete payload.client_secret;
        }

        const existingSlug = modal.getAttribute('data-edit-slug');
        const method = existingSlug ? 'PUT' : 'POST';
        const url = existingSlug ? ('/api/v1/sso/providers/' + encodeURIComponent(existingSlug)) : '/api/v1/sso/providers';

        try {
            if (typeof fetch !== 'function') {
                throw new Error('fetch not available');
            }
            setModalSaving(true);
            const resp = await fetch(url, {
                method,
                headers: { 'content-type': 'application/json' },
                body: JSON.stringify(payload),
            });
            if (!resp.ok) {
                const text = await resp.text();
                throw new Error(text || 'Request failed');
            }
            closeModal();
            await loadProviders();
            sharedToast(existingSlug ? 'Identity provider updated' : 'Identity provider created', 'success');
        } catch (err) {
            showModalError(err && err.message ? err.message : 'Failed to save provider');
            setModalSaving(false);
        }
    }

    async function deleteProvider(slug) {
        if (!slug) {
            return;
        }
        let confirmed = true;
        if (window.__pm_shared && typeof window.__pm_shared.showConfirm === 'function') {
            confirmed = await window.__pm_shared.showConfirm('Delete identity provider "' + slug + '"? Users will no longer be able to sign in with this provider.', 'Delete Provider', true);
        } else {
            confirmed = window.confirm('Delete provider ' + slug + '?');
        }
        if (!confirmed) {
            return;
        }
        try {
            if (typeof fetch !== 'function') {
                throw new Error('fetch not available');
            }
            const resp = await fetch('/api/v1/sso/providers/' + encodeURIComponent(slug), { method: 'DELETE' });
            if (!resp.ok) {
                const text = await resp.text();
                throw new Error(text || 'Failed to delete provider');
            }
            sharedToast('Deleted provider ' + slug, 'success');
            await loadProviders();
        } catch (err) {
            sharedAlert(err && err.message ? err.message : 'Failed to delete provider', 'Delete failed', true);
        }
    }

    window.__pmSSO = Object.assign(window.__pmSSO || {}, {
        init,
        loadProviders,
        renderProviders,
        openModal,
        syncTenants,
    });
})();
