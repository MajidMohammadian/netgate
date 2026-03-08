# NetGate

NetGate is a small Go application for Ubuntu that installs VPN stacks (e.g. L2TP/IPsec) and provides a web UI to manage VPN configs and connect/disconnect. It can also install the [3x-ui](https://github.com/MHSanaei/3x-ui) panel for Xray.

## Features

- **Install / Uninstall VPN stacks**  
  Choose protocols (e.g. L2TP), pick an APT mirror (with optional Iran mirrors and reachability check), then run install or uninstall. UFW rules for L2TP (UDP 500, 4500, 1701) are added on install and removed on uninstall.
- **Web UI**  
  Served on a configurable port (default `8080`), with:
  - **Install** – Mirror list (add, check, select), protocol selection, install/uninstall with live log and cancel.
  - **Accounts** – Per-protocol configs: add, edit, delete; connect/disconnect with step-by-step progress.
  - **Panel** – Install or uninstall the 3x-ui panel (optional proxy for GitHub).
- **Driver-based design**  
  Protocols are pluggable; L2TP is included. Configs are stored per driver (e.g. `l2tp/config.json`).

## Requirements

- **Ubuntu** (tested on 22.04).
- **Root** for: one-shot CLI install, and for install/uninstall/connect from the UI and for 3x-ui panel install.

## Build

```bash
# Local binary (current OS)
make build
# → netgate (or netgate.exe on Windows)

# Linux binary (e.g. for deployment)
make build-linux
# → bin/netgate
```

On Windows, `make build-linux` runs `build-linux.ps1` (PowerShell). If scripts are restricted, use:

```powershell
.\build-linux.ps1
```

## Run

**Web UI (recommended)**

```bash
./netgate serve [ -data <dir> ] [ -port <port> ]
```

- `-data` – Data directory for mirrors and driver configs (default: current directory).
- `-port` – HTTP port (default: `8080`).

Example:

```bash
./netgate serve -data /opt/netgate-data -port 8080
```

Then open `http://<server>:8080`. The app listens on `0.0.0.0`.

**One-shot install (no UI)**  
Installs the default stack (e.g. L2TP) using the built-in mirror. Must be run as root:

```bash
sudo ./netgate
```

## Data directory

When using `serve`, the data directory holds:

- `mirrors.json` – Saved mirror URLs for install.
- `<driver>/config.json` – Stored configs per protocol (e.g. `l2tp/config.json`).

Do not commit these if they contain secrets; they are listed in `.gitignore` when under the project root.

## 3x-ui panel and proxy

From the **Panel** tab you can install or uninstall the 3x-ui panel. If the server cannot reach GitHub, open **Proxy** in that tab and set a proxy URL (e.g. `http://127.0.0.1:7890`); the install script will use it.
