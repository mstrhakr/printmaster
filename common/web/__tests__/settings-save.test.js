/** @jest-environment jsdom */

// Load focused helper so we can validate the interval clamping without
// pulling in the entire agent bundle inside the test runner.
const { getMetricsRescanIntervalFromDom } = require('../../../agent/web/save_helpers.js');

describe('saveAllSettings payload', () => {
    beforeEach(() => {
        document.body.innerHTML = '';
        // Create minimal set of elements referenced by saveAllSettings
        const ids = [
            'dev_debug_logging','dev_dump_parse_debug','dev_snmp_community',
            'dev_snmp_timeout','dev_snmp_retries','dev_discover_concurrency','dev_asset_id_regex',
            'scan_local_subnet_enabled','manual_ranges_enabled','discovery_arp_enabled','discovery_icmp_enabled',
            'discovery_tcp_enabled','discovery_mdns_enabled','discovery_snmp_enabled','discovery_live_mdns_enabled',
            'discovery_live_wsd_enabled','discovery_live_ssdp_enabled','discovery_live_snmptrap_enabled','discovery_live_llmnr_enabled',
            'metrics_rescan_enabled','metrics_rescan_interval','auto_discover_checkbox','autosave_checkbox',
            'show_discover_button_anyway','show_discovered_devices_anyway','passive_discovery_enabled','ip_scanning_enabled',
            'enable_saved_credentials','enable_http','http_port','enable_https','https_port','redirect_http_to_https',
            'custom_cert_path','custom_key_path'
        ];
        ids.forEach(id => {
            const el = document.createElement('input');
            el.id = id;
            // default types and values
            if (id === 'dev_debug_logging') el.value = 'info';
            if (id === 'metrics_rescan_interval') el.value = '15';
            if (id.endsWith('_enabled') || id === 'dev_dump_parse_debug') el.type = 'checkbox';
            document.body.appendChild(el);
        });
        // Ensure checkboxes have .checked property
        document.getElementById('metrics_rescan_enabled').checked = true;
        document.getElementById('dev_dump_parse_debug').checked = false;

    });

    test('metrics_rescan_interval_minutes is integer in payload', async () => {
    // Validate the helper computes an integer and clamps correctly
    const iv = getMetricsRescanIntervalFromDom();
    expect(Number.isInteger(iv)).toBe(true);
    expect(iv).toBe(15);
    });
});
