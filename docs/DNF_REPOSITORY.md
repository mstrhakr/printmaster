# DNF Repository Setup

PrintMaster provides a DNF/YUM repository hosted on GitHub Pages for easy installation on Fedora, RHEL, CentOS, and other RPM-based systems.

## For Users: Installing PrintMaster Agent

### Quick Install (One-liner)

```bash
# Add repository and install
sudo dnf config-manager addrepo --from-repofile=https://mstrhakr.github.io/printmaster/printmaster.repo
sudo dnf install printmaster-agent
```

### With GPG Verification (Recommended)

```bash
# Import GPG key first
sudo rpm --import https://mstrhakr.github.io/printmaster/gpg.key

# Add repository and install
sudo dnf config-manager addrepo --from-repofile=https://mstrhakr.github.io/printmaster/printmaster.repo
sudo dnf install printmaster-agent
```

The hosted `.repo` file already has GPG checking enabled, so importing the key first ensures verification works.

### After Installation

```bash
# Start the service
sudo systemctl start printmaster-agent

# Enable on boot
sudo systemctl enable printmaster-agent

# Check status
sudo systemctl status printmaster-agent
```

- **Web UI**: http://localhost:8000
- **Config file**: `/etc/printmaster/agent.toml`
- **Data directory**: `/var/lib/printmaster`
- **Logs**: `journalctl -u printmaster-agent`

### Updating

```bash
sudo dnf upgrade printmaster-agent
```

### Uninstalling

```bash
# Remove package (keeps config)
sudo dnf remove printmaster-agent

# Remove repository
sudo rm /etc/yum.repos.d/printmaster.repo
```

---

## Alternative: Direct RPM Install

If you prefer not to use the repository, you can download and install the RPM directly:

```bash
# Download latest release (replace VERSION with actual version)
wget https://github.com/mstrhakr/printmaster/releases/download/agent-vVERSION/printmaster-agent-VERSION-1.fc41.x86_64.rpm

# Install
sudo dnf install ./printmaster-agent-*.rpm
```

---

## For Maintainers: Repository Setup

### How It Works

1. When an `agent-v*` release is published, the `cd-agent.yml` workflow triggers
2. It builds RPM packages for each architecture using `build-rpm.sh`
3. Generates DNF repository metadata using `createrepo_c`
4. Publishes to GitHub Pages on the `gh-pages` branch alongside the APT repo

### Initial Setup

The DNF repository shares the same GitHub Pages setup as the APT repository:

1. **Enable GitHub Pages** (already done for APT):
   - Go to repository Settings → Pages
   - Source: "GitHub Actions"

2. **GPG Signing** (shares the same key as APT):
   - Uses the `APT_GPG_PRIVATE_KEY` secret
   - RPMs are signed with the same key for consistency

### Repository Structure

```
gh-pages/
├── gpg.key                    # Public GPG key (shared)
├── pool/                      # APT repository
│   └── main/
│       └── *.deb
├── dists/                     # APT metadata
│   └── stable/
│       └── main/
│           └── binary-*/
├── rpm/                       # DNF repository
│   ├── repodata/              # DNF metadata
│   │   ├── repomd.xml
│   │   ├── *-primary.xml.gz
│   │   └── *-filelists.xml.gz
│   └── packages/
│       ├── printmaster-agent-*.x86_64.rpm
│       └── printmaster-agent-*.aarch64.rpm
└── README.md
```

### Building RPMs Locally

For testing, you can build RPMs locally on Fedora:

```bash
# Install build dependencies
sudo dnf install rpm-build rpmdevtools

# Build the agent binary first
cd agent
go build -o printmaster-agent .

# Build RPM package
chmod +x build-rpm.sh
./build-rpm.sh

# Output will be in agent/dist/
ls -la dist/*.rpm
```

### Testing the Repository

After publishing, test the repository:

```bash
# Verify metadata is accessible
curl -s https://mstrhakr.github.io/printmaster/rpm/repodata/repomd.xml | head

# Test install on a fresh Fedora system
sudo dnf config-manager addrepo --from-repofile=https://mstrhakr.github.io/printmaster/printmaster.repo
sudo dnf install printmaster-agent
```

### Supported Distributions

The RPM packages should work on:
- Fedora 38+
- RHEL 9+
- CentOS Stream 9+
- Rocky Linux 9+
- AlmaLinux 9+
- openSUSE Tumbleweed (may need `zypper` instead of `dnf`)

For older distributions (RHEL 8, CentOS 8), you may need to install from the binary directly or build a compatible RPM.
