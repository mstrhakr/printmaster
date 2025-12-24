# PrintMaster Agent RPM Spec File
# Usage: rpmbuild -bb printmaster-agent.spec
# Requires: PRINTMASTER_VERSION and PRINTMASTER_BINARY env vars to be set

%define _binaries_in_noarch_packages_terminate_build 0
%define debug_package %{nil}

# SELinux policy settings
%global selinux_variants targeted
%global selinux_policyver %(rpm -q --qf "%%{version}-%%{release}" selinux-policy 2>/dev/null || echo "0.0.0-0")
%global modulename printmaster_agent

Name:           printmaster-agent
Version:        %{getenv:PRINTMASTER_VERSION}
Release:        1%{?dist}
Summary:        Network printer fleet management agent

License:        MIT
URL:            https://github.com/mstrhakr/printmaster
Source0:        printmaster-agent

BuildRequires:  systemd-rpm-macros
Requires:       glibc
Requires:       policycoreutils-python-utils

# SELinux dependencies
Requires(post): policycoreutils
Requires(post): policycoreutils-python-utils
Requires(postun): policycoreutils

# The package creates its own user/group in %pre, so we provide these
# to satisfy RPM's automatic dependency generator
Provides:       user(printmaster)
Provides:       group(printmaster)

%description
PrintMaster Agent discovers and monitors network printers via SNMP,
collects metrics (page counts, toner levels, status), and optionally
reports to a central PrintMaster Server for multi-site fleet management.

Features:
- Automatic printer discovery (SNMP, mDNS, WS-Discovery)
- Real-time metrics collection
- Web UI for monitoring
- Optional central server reporting
- SELinux compatible (Fedora/RHEL/CentOS)

%install
# Create directories
install -d %{buildroot}%{_bindir}
install -d %{buildroot}%{_unitdir}
install -d %{buildroot}%{_sysconfdir}/printmaster
install -d %{buildroot}%{_sharedstatedir}/printmaster
install -d %{buildroot}%{_localstatedir}/log/printmaster

# Install binary
install -m 755 %{getenv:PRINTMASTER_BINARY} %{buildroot}%{_bindir}/printmaster-agent

# Install systemd service
cat > %{buildroot}%{_unitdir}/printmaster-agent.service << 'EOF'
[Unit]
Description=PrintMaster Agent - Network Printer Fleet Management
Documentation=https://github.com/mstrhakr/printmaster
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/bin/printmaster-agent --config /etc/printmaster/agent.toml
Restart=on-failure
RestartSec=5
User=printmaster
Group=printmaster
WorkingDirectory=/var/lib/printmaster

# Set data directory via environment variable
Environment=PRINTMASTER_DATA_DIR=/var/lib/printmaster

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/printmaster /var/log/printmaster
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF

# Install example config
cat > %{buildroot}%{_sysconfdir}/printmaster/agent.toml.example << 'EOF'
# PrintMaster Agent Configuration
# Copy to agent.toml and customize as needed

[agent]
# Name for this agent (displayed in server UI)
name = "my-site-agent"

# Web UI settings
listen = ":8080"

[discovery]
# Networks to scan (CIDR notation)
# networks = ["192.168.1.0/24", "10.0.0.0/24"]

# Scan interval (minutes)
# interval = 60

[server]
# Optional: Report to central PrintMaster Server
# url = "http://your-server:8080"
# enabled = false
EOF

%pre
# Create printmaster user/group if they don't exist
getent group printmaster >/dev/null || groupadd -r printmaster
getent passwd printmaster >/dev/null || \
    useradd -r -g printmaster -d /var/lib/printmaster -s /sbin/nologin \
    -c "PrintMaster Agent" printmaster
exit 0

%post
%systemd_post printmaster-agent.service

# ============================================================
# SELinux Configuration
# ============================================================
# We use standard SELinux types that already exist in the base policy
# This avoids needing a custom SELinux module while still being secure

if selinuxenabled 2>/dev/null; then
    echo "Configuring SELinux contexts for PrintMaster..."
    
    # Set file contexts using semanage fcontext
    # Binary: use bin_t (standard executable type)
    semanage fcontext -a -t bin_t "/usr/bin/printmaster-agent" 2>/dev/null || \
        semanage fcontext -m -t bin_t "/usr/bin/printmaster-agent" 2>/dev/null || true
    
    # Config directory: use etc_t (standard config type)
    semanage fcontext -a -t etc_t "/etc/printmaster(/.*)?" 2>/dev/null || \
        semanage fcontext -m -t etc_t "/etc/printmaster(/.*)?" 2>/dev/null || true
    
    # Data directory: use var_lib_t (standard state data type)
    semanage fcontext -a -t var_lib_t "/var/lib/printmaster(/.*)?" 2>/dev/null || \
        semanage fcontext -m -t var_lib_t "/var/lib/printmaster(/.*)?" 2>/dev/null || true
    
    # Log directory: use var_log_t (standard log type)
    semanage fcontext -a -t var_log_t "/var/log/printmaster(/.*)?" 2>/dev/null || \
        semanage fcontext -m -t var_log_t "/var/log/printmaster(/.*)?" 2>/dev/null || true
    
    # Apply the contexts
    restorecon -Rv /usr/bin/printmaster-agent 2>/dev/null || true
    restorecon -Rv /etc/printmaster 2>/dev/null || true
    restorecon -Rv /var/lib/printmaster 2>/dev/null || true
    restorecon -Rv /var/log/printmaster 2>/dev/null || true
    
    # Allow the service to bind to port 8080 (web UI)
    # Using http_port_t since 8080 is commonly an HTTP alternate port
    semanage port -a -t http_port_t -p tcp 8080 2>/dev/null || \
        semanage port -m -t http_port_t -p tcp 8080 2>/dev/null || true
    
    # Enable SELinux booleans needed for network daemon operation
    # httpd_can_network_connect: allows outbound connections (to printers, server)
    setsebool -P httpd_can_network_connect on 2>/dev/null || true
    
    echo "SELinux configuration complete."
else
    echo "SELinux not enforcing, skipping context configuration."
fi

# Set ownership of directories
chown -R printmaster:printmaster /var/lib/printmaster
chown -R printmaster:printmaster /var/log/printmaster
chmod 750 /var/lib/printmaster
chmod 750 /var/log/printmaster

# Create config from example if it doesn't exist
if [ ! -f /etc/printmaster/agent.toml ]; then
    cp /etc/printmaster/agent.toml.example /etc/printmaster/agent.toml
    chown root:printmaster /etc/printmaster/agent.toml
    chmod 640 /etc/printmaster/agent.toml
fi

# Enable and start the service (fresh install or upgrade)
systemctl daemon-reload
systemctl enable printmaster-agent.service || true
systemctl restart printmaster-agent.service || true

echo "PrintMaster Agent installed and running at http://localhost:8080"

%preun
%systemd_preun printmaster-agent.service

%postun
%systemd_postun_with_restart printmaster-agent.service

# Remove user and SELinux contexts on complete uninstall (not upgrade)
if [ $1 -eq 0 ]; then
    # Clean up SELinux file contexts
    if selinuxenabled 2>/dev/null; then
        echo "Removing SELinux contexts for PrintMaster..."
        semanage fcontext -d "/usr/bin/printmaster-agent" 2>/dev/null || true
        semanage fcontext -d "/etc/printmaster(/.*)?" 2>/dev/null || true
        semanage fcontext -d "/var/lib/printmaster(/.*)?" 2>/dev/null || true
        semanage fcontext -d "/var/log/printmaster(/.*)?" 2>/dev/null || true
        # Note: we don't remove port 8080 as other services may use it
    fi
    
    userdel printmaster 2>/dev/null || true
    groupdel printmaster 2>/dev/null || true
fi

%files
%{_bindir}/printmaster-agent
%{_unitdir}/printmaster-agent.service
%dir %{_sysconfdir}/printmaster
%config(noreplace) %{_sysconfdir}/printmaster/agent.toml.example
%dir %attr(750,printmaster,printmaster) %{_sharedstatedir}/printmaster
%dir %attr(750,printmaster,printmaster) %{_localstatedir}/log/printmaster

%changelog
* Mon Dec 23 2024 PrintMaster Team <printmaster@example.com> - %{version}-1
- Added proper SELinux context configuration
- Fixed file contexts for /etc, /var/lib, /var/log directories
- Added port labeling for web UI port 8080
* Sun Dec 22 2024 PrintMaster Team <printmaster@example.com>
- Initial automated build from release
