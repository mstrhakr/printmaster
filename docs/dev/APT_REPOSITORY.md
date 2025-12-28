# APT Repository Setup

PrintMaster provides an APT repository hosted on GitHub Pages for easy installation on Debian/Ubuntu systems.

## For Users: Installing PrintMaster Agent

### Quick Install (Unsigned)

```bash
# Add repository
echo "deb [trusted=yes] https://mstrhakr.github.io/printmaster stable main" | sudo tee /etc/apt/sources.list.d/printmaster.list

# Update and install
sudo apt-get update
sudo apt-get install printmaster-agent
```

### Install with GPG Verification (Recommended)

```bash
# Download and add GPG key
curl -fsSL https://mstrhakr.github.io/printmaster/gpg.key | sudo gpg --dearmor -o /usr/share/keyrings/printmaster.gpg

# Add repository with signed-by
echo "deb [signed-by=/usr/share/keyrings/printmaster.gpg] https://mstrhakr.github.io/printmaster stable main" | sudo tee /etc/apt/sources.list.d/printmaster.list

# Update and install
sudo apt-get update
sudo apt-get install printmaster-agent
```

### After Installation

```bash
# Start the service
sudo systemctl start printmaster-agent

# Enable on boot
sudo systemctl enable printmaster-agent

# Check status
sudo systemctl status printmaster-agent
```

- **Web UI**: http://localhost:8080
- **Config file**: `/etc/printmaster/agent.toml`
- **Data directory**: `/var/lib/printmaster`
- **Logs**: `journalctl -u printmaster-agent`

### Updating

```bash
sudo apt-get update
sudo apt-get upgrade printmaster-agent
```

### Uninstalling

```bash
# Remove package (keeps config)
sudo apt-get remove printmaster-agent

# Remove package and all data
sudo apt-get purge printmaster-agent
```

---

## For Maintainers: Repository Setup

### How It Works

1. When an `agent-v*` release is published, the `update-apt-repo.yml` workflow triggers
2. It downloads all `.deb` files from recent releases
3. Generates APT repository metadata (`Packages`, `Release`)
4. Publishes to GitHub Pages on the `gh-pages` branch

### Initial Setup

1. **Enable GitHub Pages**:
   - Go to repository Settings → Pages
   - Source: "GitHub Actions"

2. **(Optional) Set up GPG signing**:
   
   Generate a GPG key for signing:
   ```bash
   # Generate key (use a dedicated email, no passphrase for CI, or use passphrase with secret)
   gpg --batch --gen-key <<EOF
   Key-Type: RSA
   Key-Length: 4096
   Name-Real: PrintMaster APT Signing Key
   Name-Email: printmaster-apt@example.com
   Expire-Date: 0
   %no-protection
   EOF
   
   # Export private key
   gpg --armor --export-secret-keys printmaster-apt@example.com > private.key
   
   # Export public key
   gpg --armor --export printmaster-apt@example.com > public.key
   ```

3. **Add secrets to GitHub**:
   - `APT_GPG_PRIVATE_KEY`: Contents of `private.key`
   - `APT_GPG_PASSPHRASE`: Passphrase (if you used one)

4. **Upload public key**:
   - Add `public.key` as `gpg.key` to the `gh-pages` branch
   - Or configure the workflow to export it automatically

### Manual Repository Update

Trigger the workflow manually:
- Go to Actions → "Update APT Repository" → "Run workflow"

### Repository Structure

```
gh-pages branch:
├── index.html              # Landing page with install instructions
├── gpg.key                 # Public GPG key (optional)
├── pool/
│   └── main/
│       ├── printmaster-agent_0.16.0_amd64.deb
│       └── printmaster-agent_0.16.0_arm64.deb
└── dists/
    └── stable/
        ├── Release         # Repository metadata
        ├── Release.gpg     # Detached signature (if signed)
        ├── InRelease       # Inline signed release (if signed)
        └── main/
            ├── binary-amd64/
            │   ├── Packages
            │   └── Packages.gz
            └── binary-arm64/
                ├── Packages
                └── Packages.gz
```

### Troubleshooting

**Packages not showing up?**
- Check that the release has `.deb` files attached
- Verify the workflow ran successfully
- Check the `gh-pages` branch for the pool directory

**GPG signature errors?**
- Ensure the GPG key is properly imported
- Check the passphrase secret is correct
- Users can use `[trusted=yes]` to skip verification

**404 on apt update?**
- Verify GitHub Pages is enabled
- Check the repository URL matches your GitHub username/org
