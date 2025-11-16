(function(){
    if (typeof window === 'undefined' || typeof document === 'undefined') {
        return;
    }

    const params = new URLSearchParams(window.location.search);
    const tenantParam = (params.get('tenant') || '').trim();
    const redirectTarget = params.get('redirect') || '/';
    const skipAuto = ['1','true','yes','on'].includes((params.get('no_auto') || '').toLowerCase());
    const errorCode = params.get('error') || '';

    const errorMessages = {
        oidc_invalid: 'The sign-in response was incomplete. Please try again.',
        oidc_state: 'The sign-in request expired. Start again from this page.',
        oidc_provider: 'The selected identity provider is no longer available.',
        oidc_discovery: 'Unable to reach the identity provider. Please try again.',
        oidc_exchange: 'Failed to exchange the sign-in code. Try again or use another method.',
        oidc_token: 'The identity provider did not return a valid token.',
        oidc_verify: 'The identity token could not be verified.',
        oidc_nonce: 'The sign-in session is no longer valid. Try again.',
        oidc_claims: 'Could not read your profile information from the identity provider.',
        oidc_user: 'We could not create or find a user for this identity.',
        oidc_session: 'We could not establish a session. Please try again.',
    };

    const elementIds = {
        error: 'login_error',
        loading: 'auth_loading',
        localSection: 'local_login_section',
        ssoSection: 'sso_section',
        ssoButtons: 'sso_buttons',
        noOptions: 'no_options_message',
        autoHint: 'auto_login_hint',
        skipAutoLink: 'skip_auto_link',
    };

    const elements = new Proxy({}, {
        get(_, prop) {
            const id = elementIds[prop];
            if (!id || typeof document === 'undefined') {
                return null;
            }
            return document.getElementById(id);
        }
    });

    function showError(message){
        if(!elements.error) return;
        elements.error.textContent = message;
        elements.error.style.display = 'block';
    }

    async function loadAuthOptions(){
        const query = new URLSearchParams();
        if(tenantParam){
            query.set('tenant', tenantParam);
        }
        try{
            const res = await fetch('/api/v1/auth/options' + (query.toString() ? ('?' + query.toString()) : ''));
            if(!res.ok){
                throw new Error('Unable to load sign-in options.');
            }
            const data = await res.json();
            renderOptions(data || {});
        }catch(err){
            showError(err && err.message ? err.message : 'Unable to load sign-in options.');
            if(elements.noOptions){
                elements.noOptions.style.display = 'block';
            }
        }finally{
            if(elements.loading){
                elements.loading.style.display = 'none';
            }
        }
    }

    function renderOptions(opts){
        const providers = Array.isArray(opts.providers) ? opts.providers : [];
        const localEnabled = !!opts.local_login;

        if(localEnabled && elements.localSection){
            elements.localSection.style.display = 'block';
        }

        if(providers.length > 0 && elements.ssoSection && elements.ssoButtons){
            elements.ssoSection.style.display = 'block';
            elements.ssoButtons.innerHTML = '';
            providers.forEach(p => elements.ssoButtons.appendChild(createProviderButton(p)));
        }

        if(!localEnabled && providers.length === 0 && elements.noOptions){
            elements.noOptions.style.display = 'block';
        }

        maybeAutoLogin(providers);
    }

    function createProviderButton(provider){
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'modal-button modal-button-primary';
        btn.style.display = 'flex';
        btn.style.alignItems = 'center';
        btn.style.justifyContent = 'center';
        btn.style.gap = '6px';
        btn.style.fontWeight = '600';
        btn.dataset.slug = provider.slug;
        btn.textContent = provider.button_text || provider.display_name || 'Continue';

        if(provider.button_style){
            btn.className += ' ' + provider.button_style;
        }

        if(provider.icon){
            const icon = document.createElement('img');
            icon.src = provider.icon;
            icon.alt = '';
            icon.style.width = '18px';
            icon.style.height = '18px';
            icon.style.objectFit = 'contain';
            btn.prepend(icon);
        }

        btn.addEventListener('click', () => startOIDC(provider.slug));
        return btn;
    }

    function maybeAutoLogin(providers){
        if(!providers || providers.length === 0){
            return;
        }
        const autoProvider = providers.find(p => p && p.auto_login);
        if(!autoProvider || skipAuto || errorCode){
            return;
        }
        if(elements.autoHint){
            elements.autoHint.style.display = 'block';
            if(elements.skipAutoLink){
                const qp = new URLSearchParams(window.location.search);
                qp.set('no_auto', '1');
                elements.skipAutoLink.href = window.location.pathname + '?' + qp.toString();
            }
        }
        startOIDC(autoProvider.slug);
    }

    function startOIDC(slug){
        if(!slug) return;
        disableInputs();
        const qs = new URLSearchParams();
        if(tenantParam){
            qs.set('tenant', tenantParam);
        }
        if(redirectTarget){
            qs.set('redirect', redirectTarget);
        }
        window.location = '/auth/oidc/start/' + encodeURIComponent(slug) + (qs.toString() ? ('?' + qs.toString()) : '');
    }

    function disableInputs(){
        ['login_submit','login_cancel'].forEach(id => {
            const el = document.getElementById(id);
            if(el){
                el.disabled = true;
            }
        });
        if(elements.ssoButtons){
            Array.from(elements.ssoButtons.querySelectorAll('button')).forEach(btn => {
                btn.disabled = true;
            });
        }
    }

    async function doLogin(){
        const errEl = elements.error;
        if(errEl){
            errEl.style.display = 'none';
        }
        const uEl = document.getElementById('login_username');
        const pEl = document.getElementById('login_password');
        const username = uEl ? (uEl.value || '') : '';
        const password = pEl ? (pEl.value || '') : '';
        try{
            const r = await fetch('/api/v1/auth/login', {method:'POST', headers:{'content-type':'application/json'}, body: JSON.stringify({username, password})});
            if(!r.ok){
                const text = await r.text();
                showError(text || 'Invalid credentials');
                return;
            }
            window.location = redirectTarget || '/';
        }catch(e){
            showError(e && e.message ? e.message : 'Login failed');
        }
    }

    function initializeLoginPage(){
        if(initializeLoginPage._ran){
            return;
        }
        initializeLoginPage._ran = true;
        if(errorCode){
            showError(errorMessages[errorCode] || 'Sign-in failed. Please try again.');
        }
        loadAuthOptions();

        const submit = document.getElementById('login_submit');
        if(submit){
            submit.onclick = doLogin;
        }
        const cancel = document.getElementById('login_cancel');
        if(cancel){
            cancel.onclick = function(){ window.location = redirectTarget || '/'; };
        }
        const pwd = document.getElementById('login_password');
        if(pwd){
            pwd.addEventListener('keypress', function(e){ if(e.key==='Enter'){ doLogin(); } });
        }
    }

    document.addEventListener('DOMContentLoaded', initializeLoginPage);
    if(document.readyState === 'interactive' || document.readyState === 'complete'){
        initializeLoginPage();
    }

    window.__pmLogin = window.__pmLogin || {};
    window.__pmLogin.init = initializeLoginPage;
})();
