(function(){
    const code = (document.body.dataset.deviceCode || '').trim();
    const statusBadge = document.getElementById('status_badge');
    const codeBadge = document.getElementById('code_badge');
    const tenantSelect = document.getElementById('tenant_select');
    const agentNameInput = document.getElementById('agent_name_input');
    const pageMessage = document.getElementById('page_message');
    const approveForm = document.getElementById('approve_form');
    const approveBtn = document.getElementById('approve_btn');
    const rejectBtn = document.getElementById('reject_btn');
    const refreshBtn = document.getElementById('refresh_btn');
    const closeHint = document.getElementById('close_hint');
    const metaFields = {
        name: document.getElementById('agent_name'),
        id: document.getElementById('agent_id'),
        version: document.getElementById('agent_version'),
        host: document.getElementById('agent_host'),
        platform: document.getElementById('agent_platform'),
        ip: document.getElementById('agent_ip')
    };

    function setStatus(status, message) {
        if (!statusBadge) return;
        const map = {
            pending: { text: 'Pending', class: 'status-pending' },
            approved: { text: 'Approved', class: 'status-approved' },
            rejected: { text: 'Rejected', class: 'status-rejected' },
            expired: { text: 'Expired', class: 'status-expired' }
        };
        const meta = map[status] || map.pending;
        statusBadge.textContent = meta.text;
        statusBadge.className = 'status-badge ' + meta.class;
        if (status !== 'pending') {
            approveBtn && (approveBtn.disabled = true);
            rejectBtn && (rejectBtn.disabled = true);
            refreshBtn && (refreshBtn.disabled = true);
            tenantSelect && (tenantSelect.disabled = true);
            agentNameInput && (agentNameInput.disabled = true);
            approveForm && approveForm.classList.add('disabled');
            closeHint && (closeHint.style.display = 'block');
        }
        showMessage(message || '', status === 'approved' ? 'success' : status === 'rejected' || status === 'expired' ? 'error' : 'info');
        if (status === 'approved') {
            setTimeout(() => {
                try { window.close(); } catch (e) {}
            }, 1500);
        }
    }

    function showMessage(text, kind) {
        if (!pageMessage) return;
        if (!text) {
            pageMessage.style.display = 'none';
            pageMessage.textContent = '';
            pageMessage.className = '';
            return;
        }
        pageMessage.textContent = text;
        pageMessage.className = kind === 'error' ? 'error' : (kind === 'success' ? 'success' : '');
        pageMessage.style.display = 'block';
    }

    async function fetchDetails() {
        if (!code) return;
        try {
            showMessage('Loading request details…', 'info');
            const resp = await fetch(`/api/v1/device-auth/requests/${encodeURIComponent(code)}`);
            if (!resp.ok) {
                throw new Error(`Request not found (${resp.status})`);
            }
            const data = await resp.json();
            applyDetails(data);
            showMessage('', '');
        } catch (err) {
            showMessage(err.message || 'Failed to load request', 'error');
        }
    }

    function applyDetails(data) {
        if (!data) return;
        if (codeBadge && data.code) codeBadge.textContent = data.code;
        if (metaFields.name) metaFields.name.textContent = data.agent?.name || '—';
        if (metaFields.id) metaFields.id.textContent = data.agent?.id || '—';
        if (metaFields.version) metaFields.version.textContent = data.agent?.version || '—';
        if (metaFields.host) metaFields.host.textContent = data.agent?.hostname || '—';
        if (metaFields.platform) metaFields.platform.textContent = data.agent?.platform || '—';
        if (metaFields.ip) metaFields.ip.textContent = data.agent?.client_ip || '—';
        if (Array.isArray(data.tenants) && tenantSelect) {
            tenantSelect.innerHTML = '<option value="" disabled>Select a tenant…</option>';
            data.tenants.forEach(t => {
                const opt = document.createElement('option');
                opt.value = t.id;
                opt.textContent = t.name || t.id;
                tenantSelect.appendChild(opt);
            });
            if (data.tenant_id) {
                tenantSelect.value = data.tenant_id;
            }
        }
        if (agentNameInput && data.assigned_name) {
            agentNameInput.value = data.assigned_name;
        }
        setStatus(data.status || 'pending', data.message);
    }

    async function approve(e) {
        e.preventDefault();
        const tenantId = tenantSelect?.value;
        if (!tenantId) {
            showMessage('Select a tenant before approving.', 'error');
            return;
        }
        approveBtn && (approveBtn.disabled = true);
        try {
            const body = {
                tenant_id: tenantId,
                agent_name: agentNameInput?.value || ''
            };
            const resp = await fetch(`/api/v1/device-auth/requests/${encodeURIComponent(code)}/approve`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body)
            });
            if (!resp.ok) {
                throw new Error(await resp.text() || 'Approve failed');
            }
            showMessage('Approved. Notifying agent…', 'success');
            await fetchDetails();
        } catch (err) {
            showMessage(err.message || 'Failed to approve request', 'error');
        } finally {
            approveBtn && (approveBtn.disabled = false);
        }
    }

    async function reject() {
        if (!code) return;
        const reason = await window.__pm_shared.showPrompt?.('Provide a reason for rejection (optional):', '', 'Reject request');
        rejectBtn && (rejectBtn.disabled = true);
        try {
            const resp = await fetch(`/api/v1/device-auth/requests/${encodeURIComponent(code)}/reject`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ reason: reason || '' })
            });
            if (!resp.ok) {
                throw new Error(await resp.text() || 'Reject failed');
            }
            showMessage('Request rejected.', 'error');
            await fetchDetails();
        } catch (err) {
            showMessage(err.message || 'Failed to reject request', 'error');
        } finally {
            rejectBtn && (rejectBtn.disabled = false);
        }
    }

    document.addEventListener('DOMContentLoaded', () => {
        approveForm && approveForm.addEventListener('submit', approve);
        rejectBtn && rejectBtn.addEventListener('click', reject);
        refreshBtn && refreshBtn.addEventListener('click', fetchDetails);
        fetchDetails();
    });
})();
