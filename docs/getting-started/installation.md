# Installation & Verification

This guide covers installing `cloakenv`, preparing your operating system's credential store, verifying pre-compiled release artifacts, and enabling shell autocompletion.

---

## Prerequisites

### Go Runtime (When Building or Installing from Source)
- **Go 1.26.6** or higher ([go.dev](https://go.dev/)).

### Operating System Credential Store
`cloakenv` uses your OS credential store to store vault unlock passwords (via `cloakenv auth login`), encryption keys for disk caching (`cache_disk_token`), and entries for the built-in `keyring` provider:

| Platform | Credential Store | Notes |
|---|---|---|
| **macOS** | Keychain Services | Built-in; no extra setup required. |
| **Linux** | D-Bus Secret Service | Requires a running Secret Service daemon (`gnome-keyring`, `ksecretsservice`, or KeePassXC with Secret Service integration enabled) and an unlocked session D-Bus (`DBUS_SESSION_BUS_ADDRESS`). |
| **Windows** | Windows Credential Manager | Built-in; no extra setup required. |

> [!TIP]
> For headless Linux environments (CI/CD runners or containers without D-Bus), see the [Headless & CI/CD Guide](../guides/cicd-and-headless.md) for authentication options that do not require an OS keyring.

### Optional Backend Dependencies
- **`pass` provider**: Requires `gpg` in your `PATH` and an accessible GPG keyring/agent.

---

## Installation Methods

### 1. Using `go install` (Recommended for Go Users)

```bash
go install github.com/warpcode/cloakenv@latest
```

Ensure `$(go env GOPATH)/bin` (or `$GOBIN`) is on your `PATH`.

### 2. Direct Execution with `go run`

```bash
go run github.com/warpcode/cloakenv@latest --help
```

### 3. Building from Source

```bash
git clone https://github.com/warpcode/cloakenv.git
cd cloakenv
make build
# Binary is compiled to bin/cloakenv
```

To install into `$GOBIN` / `$GOPATH/bin`:

```bash
make install
```

### 4. Pre-Compiled Release Binaries

Download pre-compiled `.tar.gz` (Linux/macOS) or `.zip` (Windows) archives from the [GitHub Releases](https://github.com/warpcode/cloakenv/releases) page.

---

## Cryptographic Release Verification

Every official `cloakenv` release includes:
- `checksums.txt` (SHA-256 digests)
- `checksums.txt.sigstore.json` (keyless Sigstore/Cosign signature bundle signed via GitHub Actions OIDC)
- SLSA Level 3 build provenance attestations

### Step 1: Verify `checksums.txt` with Cosign

```bash
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp "^https://github.com/warpcode/cloakenv/" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  checksums.txt
```

### Step 2: Verify the Archive Checksum

```bash
sha256sum --ignore-missing -c checksums.txt
```

### Step 3: Verify SLSA Level 3 Provenance with GitHub CLI

```bash
gh attestation verify cloakenv-linux-amd64.tar.gz --owner warpcode
```

---

## Shell Autocompletion

`cloakenv` generates native autocompletion scripts for `bash`, `zsh`, `fish`, and `powershell` (including dynamic vault-name completion for `auth` via `cloakenv internal list-vaults`).

### Bash
```bash
# Load for current session
source <(cloakenv completion bash)

# Persist in ~/.bashrc
echo 'source <(cloakenv completion bash)' >> ~/.bashrc
```

### Zsh
```zsh
# Ensure a directory in $fpath exists, then write _cloakenv
mkdir -p ~/.zsh/completions
cloakenv completion zsh > ~/.zsh/completions/_cloakenv
# Add to ~/.zshrc if not already present:
# fpath=(~/.zsh/completions $fpath)
# autoload -Uz compinit && compinit
```

### Fish
```fish
cloakenv completion fish > ~/.config/fish/completions/cloakenv.fish
```

### PowerShell
```powershell
cloakenv completion powershell | Out-String | Invoke-Expression
```

---

## Verify Your Installation

```bash
cloakenv version
cloakenv doctor
```

Next: Follow the [Quick Start](quickstart.md) to configure your first vault and run a command.
