/**
 * PrintMaster Context Menu System
 * 
 * Provides right-click context menus for agents and devices tables/cards.
 * Replaces the actions column with a cleaner contextual interface.
 */

(function() {
    'use strict';

    // Track active menu for cleanup
    let activeMenu = null;

    /**
     * Context menu configuration for agents
     */
    const AGENT_MENU_ITEMS = [
        {
            id: 'view-details',
            label: 'View Details',
            icon: '📋',
            action: 'view-agent',
            always: true
        },
        {
            id: 'settings',
            label: 'Agent Settings',
            icon: '⚙️',
            action: 'agent-settings'
        },
        { divider: true },
        {
            id: 'open-ui',
            label: 'Open Agent UI',
            icon: '🌐',
            action: 'open-agent',
            requiresWs: true
        },
        {
            id: 'update',
            label: 'Update Agent',
            icon: '⬆️',
            action: 'update-agent',
            requiresWs: true,
            requiresUpdate: true
        },
        {
            id: 'restart',
            label: 'Restart Agent',
            icon: '🔄',
            action: 'restart-agent',
            requiresWs: true
        },
        { divider: true },
        {
            id: 'copy-id',
            label: 'Copy Agent ID',
            icon: '📋',
            action: 'copy-agent-id'
        },
        {
            id: 'copy-ip',
            label: 'Copy IP Address',
            icon: '📋',
            action: 'copy-agent-ip'
        },
        { divider: true },
        {
            id: 'delete',
            label: 'Delete Agent',
            icon: '🗑️',
            action: 'delete-agent',
            danger: true
        }
    ];

    /**
     * Context menu configuration for devices
     */
    const DEVICE_MENU_ITEMS = [
        {
            id: 'view-details',
            label: 'View Details',
            icon: '📋',
            action: 'show-printer-details',
            always: true
        },
        { divider: true },
        {
            id: 'open-webui',
            label: 'Open Web UI',
            icon: '🌐',
            action: 'open-device',
            requiresAccess: true
        },
        {
            id: 'view-metrics',
            label: 'View Metrics',
            icon: '📊',
            action: 'view-metrics',
            requiresSerial: true
        },
        { divider: true },
        {
            id: 'copy-serial',
            label: 'Copy Serial Number',
            icon: '📋',
            action: 'copy-serial'
        },
        {
            id: 'copy-ip',
            label: 'Copy IP Address',
            icon: '📋',
            action: 'copy-device-ip'
        },
        {
            id: 'copy-mac',
            label: 'Copy MAC Address',
            icon: '📋',
            action: 'copy-device-mac'
        },
        { divider: true },
        {
            id: 'delete-device',
            label: 'Delete Device',
            icon: '🗑️',
            action: 'delete-device',
            requiresSerial: true,
            danger: true
        }
    ];

    /**
     * Create and show a context menu
     */
    function showContextMenu(event, items, context) {
        event.preventDefault();
        event.stopPropagation();

        // Close any existing menu
        closeContextMenu();

        const menu = document.createElement('div');
        menu.className = 'pm-context-menu';
        menu.setAttribute('role', 'menu');

        // Build menu items
        items.forEach((item, index) => {
            if (item.divider) {
                const divider = document.createElement('div');
                divider.className = 'pm-context-menu-divider';
                menu.appendChild(divider);
                return;
            }

            // Check item visibility conditions
            if (item.requiresWs && !context.hasWsConnection) return;
            if (item.requiresUpdate && !context.hasUpdate) return;
            if (item.requiresAccess && !context.hasAccess) return;
            if (item.requiresSerial && !context.serial) return;

            const menuItem = document.createElement('button');
            menuItem.className = 'pm-context-menu-item';
            if (item.danger) menuItem.classList.add('danger');
            if (item.disabled) {
                menuItem.classList.add('disabled');
                menuItem.disabled = true;
            }
            menuItem.setAttribute('role', 'menuitem');
            menuItem.setAttribute('data-action', item.action);

            // Copy context data to button
            Object.keys(context).forEach(key => {
                if (context[key]) {
                    menuItem.setAttribute('data-' + key.replace(/([A-Z])/g, '-$1').toLowerCase(), context[key]);
                }
            });

            menuItem.innerHTML = `
                <span class="pm-context-menu-icon">${item.icon}</span>
                <span class="pm-context-menu-label">${escapeHtml(item.label)}</span>
            `;

            // Handle click (skip for disabled items)
            if (!item.disabled) {
                menuItem.addEventListener('click', () => {
                    handleMenuAction(item.action, context);
                    closeContextMenu();
                });
            }

            menu.appendChild(menuItem);
        });

        // Position the menu
        document.body.appendChild(menu);
        
        // Calculate position (avoid going off screen)
        const menuRect = menu.getBoundingClientRect();
        let x = event.clientX;
        let y = event.clientY;

        if (x + menuRect.width > window.innerWidth) {
            x = window.innerWidth - menuRect.width - 10;
        }
        if (y + menuRect.height > window.innerHeight) {
            y = window.innerHeight - menuRect.height - 10;
        }

        menu.style.left = `${x}px`;
        menu.style.top = `${y}px`;

        // Store reference and set up close handlers
        activeMenu = menu;

        // Close on outside click (with slight delay to avoid immediate close)
        setTimeout(() => {
            document.addEventListener('click', handleOutsideClick);
            document.addEventListener('contextmenu', handleOutsideClick);
        }, 10);

        // Close on escape
        document.addEventListener('keydown', handleEscapeKey);

        // Close on scroll
        document.addEventListener('scroll', closeContextMenu, true);

        // Focus first item for accessibility
        const firstItem = menu.querySelector('.pm-context-menu-item');
        if (firstItem) firstItem.focus();
    }

    /**
     * Close the active context menu
     */
    function closeContextMenu() {
        if (activeMenu) {
            activeMenu.remove();
            activeMenu = null;
        }
        document.removeEventListener('click', handleOutsideClick);
        document.removeEventListener('contextmenu', handleOutsideClick);
        document.removeEventListener('keydown', handleEscapeKey);
        document.removeEventListener('scroll', closeContextMenu, true);
    }

    function handleOutsideClick(event) {
        if (activeMenu && !activeMenu.contains(event.target)) {
            closeContextMenu();
        }
    }

    function handleEscapeKey(event) {
        if (event.key === 'Escape') {
            closeContextMenu();
        }
    }

    /**
     * Handle menu action clicks
     */
    function handleMenuAction(action, context) {
        const shared = window.__pm_shared || {};

        switch (action) {
            // Agent actions
            case 'view-agent':
                if (shared.viewAgentDetails) {
                    shared.viewAgentDetails(context.agentId);
                }
                break;

            case 'agent-settings':
                if (shared.openFleetSettingsForAgent) {
                    shared.openFleetSettingsForAgent(context.agentId);
                }
                break;

            case 'open-agent':
                if (shared.openAgentUI) {
                    shared.openAgentUI(context.agentId);
                }
                break;

            case 'update-agent':
                if (shared.updateAgent) {
                    shared.updateAgent(context.agentId);
                }
                break;

            case 'restart-agent':
                if (shared.restartAgent) {
                    shared.restartAgent(context.agentId, context.agentName);
                }
                break;

            case 'delete-agent':
                if (shared.deleteAgent) {
                    shared.deleteAgent(context.agentId, context.agentName);
                }
                break;

            case 'copy-agent-id':
                copyToClipboard(context.agentId, 'Agent ID');
                break;

            case 'copy-agent-ip':
                copyToClipboard(context.ip, 'IP Address');
                break;

            // Device actions
            case 'show-printer-details':
                if (shared.showPrinterDetails) {
                    shared.showPrinterDetails(context.serial || context.ip, 'saved');
                }
                break;

            case 'open-device':
                if (shared.openDeviceUI) {
                    shared.openDeviceUI(context.serial);
                }
                break;

            case 'view-metrics':
                if (shared.openDeviceMetrics) {
                    shared.openDeviceMetrics(context.serial);
                }
                break;

            case 'copy-serial':
                copyToClipboard(context.serial, 'Serial Number');
                break;

            case 'copy-device-ip':
                copyToClipboard(context.ip, 'IP Address');
                break;

            case 'copy-device-mac':
                copyToClipboard(context.mac, 'MAC Address');
                break;

            case 'delete-device':
                if (shared.deleteDevice) {
                    shared.deleteDevice(context.serial, context.agentId);
                }
                break;

            // Multi-select actions
            case 'delete-agents':
                if (context.selectedIds && context.selectedIds.length > 0) {
                    deleteMultipleAgents(context.selectedIds);
                }
                break;

            case 'delete-devices':
                if (context.selectedIds && context.selectedIds.length > 0) {
                    deleteMultipleDevices(context.selectedIds);
                }
                break;

            case 'none':
                // Do nothing - used for info items
                break;

            default:
                console.warn('Unknown context menu action:', action);
        }
    }

    /**
     * Delete multiple agents with confirmation
     */
    async function deleteMultipleAgents(agentIds) {
        const shared = window.__pm_shared || {};
        const count = agentIds.length;
        
        // Get confirmation
        const confirmed = await (shared.confirm ? 
            shared.confirm(`Delete ${count} agents?`, `This will permanently remove ${count} agents from the server. This action cannot be undone.`) :
            confirm(`Delete ${count} agents? This action cannot be undone.`));
        
        if (!confirmed) return;

        let successCount = 0;
        let failCount = 0;

        for (const agentId of agentIds) {
            try {
                const response = await fetch(`/api/v1/agents/${encodeURIComponent(agentId)}`, {
                    method: 'DELETE'
                });
                if (response.ok) {
                    successCount++;
                } else {
                    failCount++;
                }
            } catch (e) {
                console.error('Failed to delete agent:', agentId, e);
                failCount++;
            }
        }

        // Clear selection and refresh
        if (window.agentsVM?.selection) {
            window.agentsVM.selection.selectedIds.clear();
            window.agentsVM.selection.lastSelected = null;
        }

        // Refresh agents list
        if (shared.fetchAgents) {
            shared.fetchAgents();
        }

        // Show result
        if (shared.showToast) {
            if (failCount === 0) {
                shared.showToast(`Deleted ${successCount} agents`, 'success');
            } else {
                shared.showToast(`Deleted ${successCount} agents, ${failCount} failed`, 'warning');
            }
        }
    }

    /**
     * Delete multiple devices with confirmation
     */
    async function deleteMultipleDevices(deviceIds) {
        const shared = window.__pm_shared || {};
        const count = deviceIds.length;
        
        // Get confirmation
        const confirmed = await (shared.confirm ? 
            shared.confirm(`Delete ${count} devices?`, `This will permanently remove ${count} devices from the server. This action cannot be undone.`) :
            confirm(`Delete ${count} devices? This action cannot be undone.`));
        
        if (!confirmed) return;

        let successCount = 0;
        let failCount = 0;

        for (const deviceId of deviceIds) {
            try {
                const response = await fetch(`/api/v1/devices/${encodeURIComponent(deviceId)}`, {
                    method: 'DELETE'
                });
                if (response.ok) {
                    successCount++;
                } else {
                    failCount++;
                }
            } catch (e) {
                console.error('Failed to delete device:', deviceId, e);
                failCount++;
            }
        }

        // Clear selection and refresh
        if (window.devicesVM?.selection) {
            window.devicesVM.selection.selectedIds.clear();
            window.devicesVM.selection.lastSelected = null;
        }

        // Refresh devices list
        if (shared.fetchDevices) {
            shared.fetchDevices();
        }

        // Show result
        if (shared.showToast) {
            if (failCount === 0) {
                shared.showToast(`Deleted ${successCount} devices`, 'success');
            } else {
                shared.showToast(`Deleted ${successCount} devices, ${failCount} failed`, 'warning');
            }
        }
    }

    /**
     * Copy text to clipboard and show toast
     */
    function copyToClipboard(text, label) {
        if (!text) {
            if (window.__pm_shared?.showToast) {
                window.__pm_shared.showToast(`No ${label} to copy`, 'warning');
            }
            return;
        }

        navigator.clipboard.writeText(text).then(() => {
            if (window.__pm_shared?.showToast) {
                window.__pm_shared.showToast(`${label} copied to clipboard`, 'success');
            }
        }).catch(err => {
            console.error('Failed to copy:', err);
            if (window.__pm_shared?.showToast) {
                window.__pm_shared.showToast('Failed to copy to clipboard', 'error');
            }
        });
    }

    /**
     * Get menu items for multi-selected agents
     */
    function getAgentMultiSelectMenuItems(context) {
        return [
            {
                id: 'multi-info',
                label: `${context.selectedCount} agents selected`,
                icon: '📋',
                action: 'none',
                disabled: true
            },
            { divider: true },
            {
                id: 'delete-selected',
                label: `Delete ${context.selectedCount} Agents`,
                icon: '🗑️',
                action: 'delete-agents',
                danger: true
            }
        ];
    }

    /**
     * Get menu items for multi-selected devices
     */
    function getDeviceMultiSelectMenuItems(context) {
        return [
            {
                id: 'multi-info',
                label: `${context.selectedCount} devices selected`,
                icon: '📋',
                action: 'none',
                disabled: true
            },
            { divider: true },
            {
                id: 'delete-selected',
                label: `Delete ${context.selectedCount} Devices`,
                icon: '🗑️',
                action: 'delete-devices',
                danger: true
            }
        ];
    }

    /**
     * Initialize context menu handlers for agents table/cards
     */
    function initAgentContextMenu(container) {
        if (!container || container.dataset.contextMenuBound) return;
        container.dataset.contextMenuBound = 'true';

        container.addEventListener('contextmenu', (event) => {
            // Find the agent row or card
            const row = event.target.closest('tr[data-agent-id]');
            const card = event.target.closest('[data-agent-id]');
            const target = row || card;

            if (!target) return;

            const agentId = target.getAttribute('data-agent-id');
            if (!agentId) return;

            // Get agent data from the VM if available
            const agent = getAgentById(agentId);
            const meta = agent?.__meta || {};

            // Check for multi-selection
            const selection = window.agentsVM?.selection;
            const selectedIds = selection?.selectedIds || new Set();
            const isMultiSelect = selectedIds.size > 1 && selectedIds.has(agentId);

            // WebSocket connection check - uses connection_type field
            const connectionType = (agent?.connection_type || '').toLowerCase();
            const hasWs = connectionType === 'ws';

            const context = {
                agentId: agentId,
                agentName: target.getAttribute('data-agent-name') || 
                           (agent && getAgentDisplayName(agent)) || 
                           agentId,
                ip: agent?.ip || '',
                hasWsConnection: hasWs,
                hasUpdate: hasAgentUpdate(agentId),
                isMultiSelect: isMultiSelect,
                selectedCount: isMultiSelect ? selectedIds.size : 1,
                selectedIds: isMultiSelect ? Array.from(selectedIds) : [agentId]
            };

            // Use multi-select menu items if applicable
            const menuItems = isMultiSelect ? getAgentMultiSelectMenuItems(context) : AGENT_MENU_ITEMS;
            showContextMenu(event, menuItems, context);
        });
    }

    /**
     * Initialize context menu handlers for devices table/cards
     */
    function initDeviceContextMenu(container) {
        if (!container || container.dataset.contextMenuBound) return;
        container.dataset.contextMenuBound = 'true';

        container.addEventListener('contextmenu', (event) => {
            // Find the device row or card
            const row = event.target.closest('tr[data-serial]');
            const card = event.target.closest('.device-card[data-serial]');
            const target = row || card;

            if (!target) return;

            const serial = target.getAttribute('data-serial');
            const ip = target.getAttribute('data-ip');
            const agentId = target.getAttribute('data-agent-id');
            const deviceId = serial || ip;

            // Get device data from the VM if available
            const device = getDeviceBySerial(serial);

            // Check for multi-selection
            const selection = window.devicesVM?.selection;
            const selectedIds = selection?.selectedIds || new Set();
            const isMultiSelect = selectedIds.size > 1 && selectedIds.has(deviceId);

            const context = {
                serial: serial,
                ip: ip || device?.ip || '',
                mac: device?.mac || '',
                agentId: agentId || device?.agent_id || '',
                hasAccess: !!(ip && agentId),
                isMultiSelect: isMultiSelect,
                selectedCount: isMultiSelect ? selectedIds.size : 1,
                selectedIds: isMultiSelect ? Array.from(selectedIds) : [deviceId]
            };

            // Use multi-select menu items if applicable
            const menuItems = isMultiSelect ? getDeviceMultiSelectMenuItems(context) : DEVICE_MENU_ITEMS;
            showContextMenu(event, menuItems, context);
        });
    }

    /**
     * Helper to get agent by ID from the view model
     */
    function getAgentById(agentId) {
        try {
            if (window.agentsVM && Array.isArray(window.agentsVM.items)) {
                return window.agentsVM.items.find(a => a.agent_id === agentId);
            }
        } catch (e) {}
        return null;
    }

    /**
     * Helper to get agent display name
     */
    function getAgentDisplayName(agent) {
        if (!agent) return 'Unknown';
        if (typeof window.getAgentDisplayName === 'function') {
            return window.getAgentDisplayName(agent);
        }
        return agent.name || agent.hostname || agent.agent_id || 'Unknown';
    }

    /**
     * Helper to check if agent has an available update
     */
    function hasAgentUpdate(agentId) {
        try {
            if (window.agentsVM) {
                const agent = getAgentById(agentId);
                const latestVersion = window.agentsVM.latestVersion;
                const currentVersion = agent?.version;
                if (latestVersion && currentVersion && currentVersion !== latestVersion) {
                    // Simple version comparison
                    return true;
                }
            }
        } catch (e) {}
        return false;
    }

    /**
     * Helper to get device by serial from the view model
     */
    function getDeviceBySerial(serial) {
        try {
            if (window.devicesVM && Array.isArray(window.devicesVM.items)) {
                return window.devicesVM.items.find(d => d.serial === serial);
            }
        } catch (e) {}
        return null;
    }

    /**
     * HTML escape helper
     */
    function escapeHtml(str) {
        if (str === null || str === undefined) return '';
        return String(str)
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;')
            .replace(/'/g, '&#39;');
    }

    // Export to global scope
    window.PMContextMenu = {
        showContextMenu,
        closeContextMenu,
        initAgentContextMenu,
        initDeviceContextMenu,
        AGENT_MENU_ITEMS,
        DEVICE_MENU_ITEMS
    };

})();
