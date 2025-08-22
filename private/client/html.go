package client

import (
	"fmt"
	"net/http"
)

func (c *Client) serveHTML(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<!doctype html>
<html>
<head>
  <meta charset="utf-8" />
  <title>netpump-go</title>
  <style>
    body {
      font-family: sans-serif;
      display: flex;
      align-items: center;
      justify-content: center;
      height: 100vh;
      margin: 0;
    }
    .container { text-align: center; }
    h1 { font-size: 3em; margin: 0.5em 0; }
    .status { font-size: 1.2em; margin: 1em 0; }
    .info { margin: 2em 0; line-height: 1.8; }
    .connected { color: #4f4; }
    .disconnected { color: #f44; }
  </style>
</head>
<body>
  <div class="container">
    <h1>netpump-go</h1>
    <div class="status">
      Local: <span id="localStatus" class="disconnected">Connecting...</span><br>
      Server: <span id="serverStatus" class="disconnected">Waiting...</span>
    </div>
    <div class="info">
      <div>SOCKS5: 127.0.0.1:%d</div>
      <div>Sent: <span id="bytesSent">0 B</span></div>
      <div>Received: <span id="bytesReceived">0 B</span></div>
      <div>Total: <span id="bytesTotal">0 B</span></div>
    </div>
  </div>

  <script>
    const serverURL = '%s';
    let localWS = null;
    let serverWS = null;
    let bytesSent = 0;
    let bytesReceived = 0;

    function formatBytes(bytes) {
      if (bytes === 0) return '0 B';
      const k = 1024;
      const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
      const i = Math.floor(Math.log(bytes) / Math.log(k));
      return (bytes / Math.pow(k, i)).toFixed(2) + ' ' + sizes[i];
    }

    function updateBytes() {
      document.getElementById('bytesSent').textContent = formatBytes(bytesSent);
      document.getElementById('bytesReceived').textContent = formatBytes(bytesReceived);
      document.getElementById('bytesTotal').textContent = formatBytes(bytesSent + bytesReceived);
    }

    function updateStatus(element, connected) {
      element.textContent = connected ? 'Connected' : 'Disconnected';
      element.className = connected ? 'connected' : 'disconnected';
    }

    function connect() {
      // Connect to local client
      localWS = new WebSocket('ws://' + location.host + '/ws/local');
      localWS.binaryType = 'arraybuffer';

      localWS.onopen = function() {
        console.log('[+] Connected to local client');
        updateStatus(document.getElementById('localStatus'), true);

        // Connect to server
        serverWS = new WebSocket(serverURL + '/ws');
        serverWS.binaryType = 'arraybuffer';

        serverWS.onopen = function() {
          console.log('[+] Connected to server');
          updateStatus(document.getElementById('serverStatus'), true);

          // Relay all data between connections
          localWS.onmessage = function(event) {
            if (serverWS.readyState === WebSocket.OPEN) {
              serverWS.send(event.data);
              // Data from local client going to server (upload/sent)
              bytesSent += event.data.byteLength || event.data.length || 0;
              updateBytes();
            }
          };

          serverWS.onmessage = function(event) {
            if (localWS.readyState === WebSocket.OPEN) {
              localWS.send(event.data);
              // Data from server going to local client (download/received)
              bytesReceived += event.data.byteLength || event.data.length || 0;
              updateBytes();
            }
          };
        };

        serverWS.onerror = function(error) {
          console.error('[!] Server error:', error);
          updateStatus(document.getElementById('serverStatus'), false);
        };

        serverWS.onclose = function() {
          console.log('[-] Server disconnected');
          updateStatus(document.getElementById('serverStatus'), false);
          if (localWS.readyState === WebSocket.OPEN) {
            localWS.close();
          }
        };
      };

      localWS.onerror = function(error) {
        console.error('[!] Local error:', error);
        updateStatus(document.getElementById('localStatus'), false);
      };

      localWS.onclose = function() {
        console.log('[-] Local disconnected');
        updateStatus(document.getElementById('localStatus'), false);
        updateStatus(document.getElementById('serverStatus'), false);
        // Reconnect after 1 second
        setTimeout(connect, 1000);
      };
    }

    // Start connection
    connect();
  </script>
</body>
</html>`, c.proxyPort, c.serverURL)
}
