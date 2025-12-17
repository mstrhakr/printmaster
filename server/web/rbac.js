(function (root, factory) {
    const api = factory();
    if (typeof module === 'object' && module.exports) {
        module.exports = api;
    } else {
        root.__pm_rbac = api;
    }
})(typeof self !== 'undefined' ? self : this, function () {
    const ROLE_PRIORITY = Object.freeze({
        admin: 3,
        operator: 2,
        viewer: 1,
    });

    const ACTION_MIN_ROLE = Object.freeze({
        'tenants.read': 'admin',
        'tenants.write': 'admin',
        'join_tokens.read': 'admin',
        'join_tokens.write': 'admin',
        'packages.generate': 'admin',
        'config.read': 'viewer',
        'events.subscribe': 'viewer',
        'ui.websocket.connect': 'viewer',
        'sso.providers.read': 'admin',
        'sso.providers.write': 'admin',
        'users.read': 'admin',
        'users.write': 'admin',
        'sessions.read': 'admin',
        'sessions.write': 'admin',
        'agents.read': 'viewer',
        'agents.write': 'operator',
        'agents.delete': 'operator',
        'devices.read': 'viewer',
        'metrics.summary.read': 'viewer',
        'metrics.history.read': 'viewer',
        'proxy.agent': 'operator',
        'proxy.device': 'operator',
        'settings.read': 'viewer',   // Viewers+ can read settings (tenant-scoped if not admin)
        'settings.write': 'operator', // Operators+ can write settings (tenant-scoped if not admin)
        'settings.test_email': 'admin',
        'logs.read': 'viewer',
        'audit.logs.read': 'admin',
    });

    function normalizeRole(role) {
        return (role || '').toString().trim().toLowerCase();
    }

    function roleRank(role) {
        return ROLE_PRIORITY[normalizeRole(role)] || 0;
    }

    function userHasRequiredRole(userRole, minRole) {
        return roleRank(userRole) >= roleRank(minRole);
    }

    function visibleTabsForRole(userRole, tabDefinitions) {
        if (!tabDefinitions) {
            return [];
        }
        return Object.entries(tabDefinitions)
            .filter(([, def]) => userHasRequiredRole(userRole, (def && def.minRole) || 'viewer'))
            .map(([tabId]) => tabId);
    }

    function canAccessTenancy(role) {
        return canPerformAction(role, 'tenants.read');
    }

    function requiredRoleForAction(action) {
        if (!action) {
            return null;
        }
        return ACTION_MIN_ROLE[action] || null;
    }

    function canPerformAction(role, action) {
        const minRole = requiredRoleForAction(action);
        if (!minRole) {
            return false;
        }
        return userHasRequiredRole(role, minRole);
    }

    return {
        ROLE_PRIORITY,
        ACTION_MIN_ROLE,
        normalizeRole,
        roleRank,
        userHasRequiredRole,
        visibleTabsForRole,
        canAccessTenancy,
        canPerformAction,
        requiredRoleForAction,
    };
});
