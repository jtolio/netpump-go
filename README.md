# Netpump-Go

This is a Go reimplementation of https://github.com/soylent/netpump/, but using
SOCKSv5 and yamux.

Netpump-Go is a WebSocket tunnel proxy that routes SOCKSv5 traffic through a
browser. All client traffic flows through a single multiplexed WebSocket
connection using yamux.

## Architecture

```
Application → SOCKS5 → Client → WebSocket → Browser → WebSocket → Server → Internet
            (localhost)  (Go)     (yamux)   (Device)   (yamux)     (Go)
```

## Features

- **SOCKS5 Proxy**: Universal proxy protocol that works with any TCP application
- **Browser-based relay**: Routes all traffic through a browser-enabled device
- **Single connection**: Uses yamux to multiplex unlimited streams over one WebSocket
- **No connection limits**: Handles thousands of concurrent connections
- **Automatic reconnection**: Browser automatically reconnects if connection drops
- **Simple setup**: Just run client and open a webpage on your device
- **Traffic statistics**: Real-time display of sent/received bytes

## Installation

```bash
go build -o netpump cmd/netpump/main.go
```

## Usage

### 1. Start the server (on a VPS/cloud server)

```bash
./netpump --server --port 9999
```

### 2. Start the client (on your workstation)

```bash
./netpump --client --server-url ws://your-server.com:9999 --proxy-port 1080

# Options:
#   --host        Interface to bind web server (default: 0.0.0.0)
#   --port        Port for web interface (default: 8080)  
#   --proxy-port  SOCKS5 proxy port (default: 1080)
#   --server-url  WebSocket URL of your server (required)
```

### 3. Connect your device

1. Connect your workstation to the same network as your device (or device's hotspot)
2. Open a browser on your device
3. Navigate to `http://[laptop-ip]:8080`
4. Keep this tab open - it's relaying your traffic!

The browser will show:
- Connection status for both local client and remote server
- Real-time traffic statistics (sent/received bytes)
- SOCKS5 proxy address for configuration

### 4. Configure your applications

Configure your browser or system to use SOCKS5 proxy:
- Host: `127.0.0.1`
- Port: `1080` (or your chosen proxy-port)

#### Firefox
Settings → Network Settings → Manual proxy configuration → SOCKS Host: 127.0.0.1, Port: 1080, SOCKS v5

#### Chrome
Use command line flag: `--proxy-server="socks5://127.0.0.1:1080"`

#### macOS
System Preferences → Network → Advanced → Proxies → SOCKS Proxy

#### Linux
Set environment variables:
```bash
export all_proxy=socks5://127.0.0.1:1080
```

## How it Works

1. **Your application** connects to the local SOCKS5 proxy
2. **Client** (Go) accepts the SOCKS5 connection and opens a yamux stream
3. **Browser** (JavaScript) relays raw binary data between client and server
4. **Server** (Go) receives the yamux stream and connects to the target
5. All data flows through this single multiplexed WebSocket connection

## Building from Source

Requirements:
- Go 1.21 or later

```bash
# Clone the repository
git clone https://github.com/netpump/netpump.git
cd netpump

# Build the binary
go build -o netpump cmd/netpump/main.go

# Run tests (if available)
go test ./...
```
