# VM-Runner: A Modern CTF Virtualization Platform

VM-Runner is a high-performance, web-based platform for hosting and managing virtualized Capture The Flag (CTF) challenges. Built with Go and QEMU, it provides a seamless experience for both CLI-based terminal challenges and full graphical (GUI) operating system environments.

## 🚀 Key Features

### 💻 Dual Display Modes
- **Serial Terminal:** PTY-based interaction using `xterm.js` over WebSockets. Ideal for CLI-centric challenges with high-speed text interaction.
- **VNC Graphical Mode:** Full graphical support via `noVNC` for GUI-based OSs, featuring absolute mouse synchronization and scaling.

### 🛡️ Automated Flag Detection
- **Real-time Scanning:** Background monitoring of VM serial output (`mon:stdio`) using a sliding window buffer to detect `flag{...}` patterns instantly.
- **Automatic Question Completion:** Automatically updates the challenge status upon detection, providing immediate feedback to the user.

### ⚡ Performance & Stability
- **Message Batching:** WebSocket updates are batched (50ms) to significantly reduce browser UI reflows and prevent "word-by-word" rendering lag.
- **History Buffering:** 100KB backend buffer ensures that terminal output is preserved across page reloads and reconnections.
- **Resource Management:** Frontend log capping and clean terminal state resets (using `term.reset()`) maintain a snappy and bug-free user interface.

### 📋 Bidirectional Clipboard
- **Cross-Copy Support:** Integrated support for host-to-guest and guest-to-host clipboard synchronization via `qemu-vdagent` (requires `spice-vdagent` in the guest OS).

## 🏗️ Architecture

- **Backend:** Go (Standard library `net/http`, `gorilla/websocket` for real-time proxying, `creack/pty` for terminal management).
- **Frontend:** Vanilla JavaScript with `xterm.js` for terminal rendering and `noVNC` for graphical framebuffer display.
- **Virtualization:** QEMU with KVM acceleration (where supported) for near-native performance.

## 🛠️ Getting Started

### Prerequisites
- Go 1.25+
- QEMU installed and in your system PATH.
- (Optional) `spice-vdagent` installed on guest images for clipboard features.

### Installation
1. Clone the repository.
2. Build the server:
   ```bash
   go build -o server ./cmd/server/main.go
   ```
3. Define your challenges in `data/challenges/` using the `config.json` schema.
4. Start the server:
   ```bash
   ./server
   ```
5. Access the dashboard at `http://localhost:8080`.

## 🛤️ Roadmap

- [ ] **Dynamic Port Allocation:** Move away from hardcoded VNC/WS ports to a dynamic pooling system for concurrent sessions.
- [ ] **Networking Isolation:** Implement Tap/Bridge networking with firewall rules to isolate guest VMs from each other and the host network.
- [ ] **User Authentication:** Support for multiple users with secure session management and persistence.
- [ ] **Resource Quotas:** Fine-grained CPU and memory limiting per challenge session.
- [ ] **Advanced Guest Integration:** Automated resolution resizing based on browser viewport dimensions using SPICE guest agents.
