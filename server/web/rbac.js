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
        return userHasRequiredRole(role, 'admin');
    }

    return {
        ROLE_PRIORITY,
        normalizeRole,
        roleRank,
        userHasRequiredRole,
        visibleTabsForRole,
        canAccessTenancy,
    };
});
