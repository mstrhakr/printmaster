(function(){
  const doc = document;
  const form = doc.getElementById('login_form');
  const usernameInput = doc.getElementById('login_username');
  const passwordInput = doc.getElementById('login_password');
  const submitBtn = doc.getElementById('login_submit');
  const statusEl = doc.getElementById('login_status');
  const loopbackNotice = doc.getElementById('loopback_notice');
  const serverWrap = doc.getElementById('server_login_wrap');
  const serverLink = doc.getElementById('server_login_link');

  const qs = new URLSearchParams(window.location.search || '');
  const returnTo = sanitizeReturnPath(qs.get('return_to'));
  let submitting = false;

  function sanitizeReturnPath(value) {
    if (!value || typeof value !== 'string') return '/';
    try {
      const trimmed = value.trim();
      if (!trimmed.startsWith('/')) return '/';
      const url = new URL(trimmed, window.location.origin);
      return (url.pathname || '/') + (url.search || '');
    } catch (e) {
      return '/';
    }
  }

  function setStatus(message, tone) {
    if (!statusEl) return;
    if (!message) {
      statusEl.textContent = '';
      statusEl.classList.add('hidden');
      statusEl.classList.remove('is-error', 'is-warn', 'is-info');
      return;
    }
    statusEl.textContent = message;
    statusEl.classList.remove('hidden');
    statusEl.classList.remove('is-error', 'is-warn', 'is-info');
    if (tone) {
      statusEl.classList.add('is-' + tone);
    }
  }

  function disableForm(disabled) {
    if (!form) return;
    form.classList.toggle('is-disabled', !!disabled);
    [usernameInput, passwordInput, submitBtn].forEach(function(el){
      if (el) el.disabled = !!disabled;
    });
  }

  function buildServerLoginURL(base) {
    try {
      if (!base) return '#';
      const trimmed = base.replace(/\/$/, '');
      return trimmed + '/login?return_to=' + encodeURIComponent(returnTo || '/');
    } catch (e) {
      return base;
    }
  }

  async function loadOptions() {
    try {
      const resp = await fetch('/api/v1/auth/options', { credentials: 'same-origin' });
      if (!resp.ok) throw new Error('options failed');
      const data = await resp.json();
      applyOptions(data);
    } catch (err) {
      setStatus('Unable to load authentication options. Remote login may be disabled.', 'warn');
    }
  }

  function applyOptions(opts) {
    if (!opts) return;
    if (!opts.login_supported) {
      setStatus('Remote logins are disabled for this agent. Access it locally or connect it to the server.', 'warn');
      disableForm(true);
    }
    if (opts.allow_local_admin && loopbackNotice) {
      loopbackNotice.classList.remove('hidden');
    }
    if (opts.server_url && serverWrap && serverLink) {
      serverLink.href = buildServerLoginURL(opts.server_url);
      serverWrap.classList.remove('hidden');
    }
  }

  async function handleSubmit(evt) {
    evt.preventDefault();
    if (submitting) return;
    const username = (usernameInput && usernameInput.value.trim()) || '';
    const password = (passwordInput && passwordInput.value) || '';
    if (!username || !password) {
      setStatus('Enter both username and password.', 'error');
      return;
    }
    submitting = true;
    setStatus('');
    if (submitBtn) {
      submitBtn.disabled = true;
      submitBtn.textContent = 'Signing in…';
    }
    try {
      const resp = await fetch('/api/v1/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'same-origin',
        body: JSON.stringify({ username: username, password: password })
      });
      if (resp.ok) {
        window.location.href = returnTo || '/';
        return;
      }
      if (resp.status === 401) {
        setStatus('Invalid username or password.', 'error');
      } else if (resp.status === 503) {
        setStatus('Authentication is unavailable on this agent.', 'error');
      } else {
        const text = await resp.text();
        setStatus(text || 'Login failed. Try again.', 'error');
      }
    } catch (err) {
      setStatus('Unable to reach the agent. Check your network connection.', 'error');
    } finally {
      submitting = false;
      if (submitBtn) {
        submitBtn.disabled = false;
        submitBtn.textContent = 'Sign In';
      }
    }
  }

  if (form) {
    form.addEventListener('submit', handleSubmit);
  }
  if (usernameInput) {
    usernameInput.focus();
  }
  loadOptions();
})();
