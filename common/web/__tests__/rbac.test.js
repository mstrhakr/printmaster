const path = require('path');

const rbac = require(path.resolve(__dirname, '../../../server/web/rbac.js'));

describe('RBAC helper module', () => {
  test('normalizeRole lowercases and trims input', () => {
    expect(rbac.normalizeRole(' Admin ')).toBe('admin');
    expect(rbac.normalizeRole('VIEWER')).toBe('viewer');
  });

  test('userHasRequiredRole enforces hierarchy', () => {
    expect(rbac.userHasRequiredRole('admin', 'viewer')).toBe(true);
    expect(rbac.userHasRequiredRole('operator', 'viewer')).toBe(true);
    expect(rbac.userHasRequiredRole('viewer', 'operator')).toBe(false);
    expect(rbac.userHasRequiredRole('viewer', 'admin')).toBe(false);
  });

  test('visibleTabsForRole filters based on minRole', () => {
    const tabs = {
      tenants: { minRole: 'admin' },
      devices: { minRole: 'viewer' },
      logs: { minRole: 'operator' },
    };
    expect(rbac.visibleTabsForRole('admin', tabs)).toEqual(['tenants', 'devices', 'logs']);
    expect(rbac.visibleTabsForRole('operator', tabs)).toEqual(['devices', 'logs']);
    expect(rbac.visibleTabsForRole('viewer', tabs)).toEqual(['devices']);
  });

  test('canAccessTenancy only grants admins', () => {
    expect(rbac.canAccessTenancy('admin')).toBe(true);
    expect(rbac.canAccessTenancy('operator')).toBe(false);
  });
});
