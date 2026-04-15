# VM Runner

VM Runner is a Go-based CTF session platform for launching QEMU-backed challenge VMs, validating submissions, and exposing sessions over WebSocket or noVNC.

## 🚀 Key Features

### 💻 Dual Display Modes
- **Serial Terminal:** PTY-based interaction using `xterm.js` over WebSockets. Ideal for CLI-centric challenges with high-speed text interaction.
- **VNC Graphical Mode:** Full graphical support via `noVNC` for GUI-based OSs, featuring absolute mouse synchronization and scaling.

### 🛡️ Submission Validation
- **Static flags:** direct comparison against stored challenge flags.
- **HMAC flags:** per-session deterministic flag generation from the session seed.
- **Dynamic Port Allocation:** Automated port searching for VNC/WS per session for better concurrency.
- **Buffered output:** terminal output is preserved so reconnects can recover the runtime stream.

### ⚡ Performance & Stability
- **Message batching:** WebSocket updates are batched to reduce terminal repaint churn.
- **History buffering:** the backend keeps a rolling output buffer for reconnects.
- **Overlay cleanup:** per-session runtime directories keep VM state isolated.

### 📋 Bidirectional Clipboard
- **Cross-Copy Support:** Integrated support for host-to-guest and guest-to-host clipboard synchronization via `qemu-vdagent` (requires `spice-vdagent` in the guest OS).

## 🏗️ Architecture

- **Backend:** Go (`net/http`, `gorilla/websocket`, `creack/pty`).
- **Frontend:** Vanilla JavaScript with `xterm.js` and `noVNC`.
- **Storage:** JSON-backed CTF/challenge configs under `data/ctfs/`.
- **Virtualization:** QEMU with optional KVM acceleration and per-session overlays.

## 🛠️ Getting Started

### Prerequisites
- Go 1.25+
- QEMU installed and in your system PATH.
- `qemu-img` available for qcow2 overlays.

### Installation
1. Clone the repository.
2. Build the server:
   ```bash
   go build -o server ./cmd/server/main.go
   ```
3. Build the VM runner CLI (for inclusion in guest images):
   ```bash
   go build -o vmrunner ./cmd/vmrunner/main.go
   ```
4. Define your CTFs in `data/ctfs/*.json`.
5. Start the server:
   ```bash
   ./server
   ```
6. Access the dashboard at `http://localhost:8080`.

### API Overview
- `GET /api/ctfs`
- `POST /api/ctfs`
- `POST /api/ctfs/:id/publish`
- `POST /api/ctfs/:id/disable`
- `POST /api/challenges`
- `POST /api/sessions`
- `GET /api/sessions/:id`
- `POST /api/sessions/:id/stop`
- `POST /api/sessions/:id/submit-answer`
- `WS /ws/session/:id`

## 🛤️ Roadmap

- [ ] **Networking Isolation:** Implement Tap/Bridge networking with firewall rules to isolate guest VMs from each other and the host network.
- [ ] **User Authentication:** Support for multiple users with secure session management and persistence.
- [ ] **Resource Quotas:** Fine-grained CPU and memory limiting per challenge session.
- [ ] **Advanced Guest Integration:** Automated resolution resizing based on browser viewport dimensions using SPICE guest agents.
