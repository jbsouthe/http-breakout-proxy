# HTTP Breakout Proxy

**HTTP Breakout Proxy** is a lightweight, cross-platform HTTP/HTTPS interception proxy written in **Go**, with a built-in real-time **web interface** for analyzing and interacting with captured traffic.

It allows you to inspect all HTTP requests and responses exchanged between any two applications — ideal for debugging APIs, observing integrations, or reverse-engineering network behavior.

---

## ✨ Features

- **Full HTTP/HTTPS proxy support** (with automatic MITM certificate generation).
- **Embedded Web UI** – browse and filter captured traffic in real-time.
- **Capture persistence** – store captures on disk and reload at startup.
- **Live updates via SSE** – new requests appear instantly in the UI.
- **Request/Response inspection** – view headers and formatted JSON bodies.
- **Filtering and searching** – regex or field-based filters.
- **Pause/Resume capture** from the UI.
- **Capture management**
  - Delete individual captures
  - Clear all captures
  - Rename captures
- **Export capabilities**
  - Copy as `curl` command
  - Copy as Python `requests` code
  - Download raw response body

Everything is served from a **single binary** with no external dependencies.

---

## 🚀 Quick Start

### Prerequisites
- Go 1.21 or later for building
- macOS, Linux, or Windows runtime
- For HTTPS interception: ability to install the generated CA certificate in your client’s trust store.

### Building from Source

```bash
git clone https://github.com/jbsouthe/http-breakout-proxy.git
cd http-breakout-proxy
go build -o http-breakout-proxy
