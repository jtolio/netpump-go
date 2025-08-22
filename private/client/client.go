package client

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/armon/go-socks5"
	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
)

type Client struct {
	host        string
	port        int
	proxyPort   int
	serverURL   string
	log         *slog.Logger
	server      *http.Server
	socksServer *socks5.Server
	ctx         context.Context
	cancel      context.CancelFunc

	// Multiplexing
	muxSession *yamux.Session
	muxMu      sync.Mutex
	wsConn     *websocket.Conn
}

func New(host string, port int, proxyPort int, serverURL string) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		host:      host,
		port:      port,
		proxyPort: proxyPort,
		serverURL: serverURL,
		log:       slog.Default().With("component", "client"),
		ctx:       ctx,
		cancel:    cancel,
	}
}

func (c *Client) Start() error {
	c.log.Info("netpump client starting")

	// Configure SOCKS5 server with custom dialer
	conf := &socks5.Config{
		Dial: c.dialThroughTunnel,
	}

	socksServer, err := socks5.New(conf)
	if err != nil {
		return fmt.Errorf("failed to create SOCKS5 server: %w", err)
	}
	c.socksServer = socksServer

	// Start SOCKS5 proxy
	proxyAddr := fmt.Sprintf("127.0.0.1:%d", c.proxyPort)
	go func() {
		c.log.Info("SOCKS5 proxy ready", "addr", proxyAddr)
		if err := c.socksServer.ListenAndServe("tcp", proxyAddr); err != nil {
			c.log.Error("SOCKS5 server error", "error", err)
		}
	}()

	// Start web interface (browser will connect to server)
	if err := c.startWebInterface(); err != nil {
		return fmt.Errorf("failed to start web interface: %w", err)
	}

	<-c.ctx.Done()
	return nil
}

func (c *Client) Stop() {
	c.cancel()
	if c.muxSession != nil {
		c.muxSession.Close()
	}
	if c.wsConn != nil {
		c.wsConn.Close()
	}
	if c.server != nil {
		c.server.Close()
	}
}

// dialThroughTunnel is called by the SOCKS5 server for each connection
func (c *Client) dialThroughTunnel(ctx context.Context, network, addr string) (net.Conn, error) {
	// Wait for mux session if not ready (browser not connected yet)
	var stream net.Conn
	var err error
	for retries := 0; retries < 30; retries++ { // Wait up to 30 seconds
		c.muxMu.Lock()
		if c.muxSession != nil {
			stream, err = c.muxSession.Open()
			c.muxMu.Unlock()
			if err == nil {
				break
			}
			return nil, fmt.Errorf("failed to open stream: %w", err)
		}
		c.muxMu.Unlock()

		if retries == 0 {
			c.log.Info("waiting for browser connection...")
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}

	if stream == nil {
		return nil, fmt.Errorf("timeout waiting for browser connection")
	}

	// Send target address
	header := []byte{byte(len(addr))}
	header = append(header, []byte(addr)...)
	if _, err := stream.Write(header); err != nil {
		stream.Close()
		return nil, fmt.Errorf("failed to send target: %w", err)
	}

	// Read connection status
	status := make([]byte, 1)
	if _, err := io.ReadFull(stream, status); err != nil {
		stream.Close()
		return nil, fmt.Errorf("failed to read status: %w", err)
	}

	if status[0] != 0x00 {
		stream.Close()
		return nil, fmt.Errorf("server failed to connect to %s", addr)
	}

	c.log.Info("connected", "target", addr)
	return stream, nil
}

func (c *Client) startWebInterface() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", c.serveHTML)
	mux.HandleFunc("/ws/local", c.handleLocalWebSocket)

	c.server = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", c.host, c.port),
		Handler: mux,
	}

	go func() {
		c.log.Info("web interface ready", "url", fmt.Sprintf("http://%s:%d", c.host, c.port))
		if err := c.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			c.log.Error("web server error", "error", err)
		}
	}()

	return nil
}

func (c *Client) handleLocalWebSocket(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		c.log.Error("websocket upgrade failed", "error", err)
		return
	}
	defer ws.Close()

	c.log.Info("browser connected")

	// Store the websocket connection for yamux
	c.muxMu.Lock()
	if c.wsConn != nil {
		c.wsConn.Close()
	}
	c.wsConn = ws

	// Setup yamux session
	conn := &wsAdapter{ws: ws}
	session, err := yamux.Server(conn, nil) // Server side of yamux since browser is client
	if err != nil {
		c.muxMu.Unlock()
		c.log.Error("yamux setup failed", "error", err)
		return
	}
	c.muxSession = session
	c.muxMu.Unlock()

	c.log.Info("yamux session established with browser")

	// Keep connection alive
	<-session.CloseChan()

	c.muxMu.Lock()
	c.muxSession = nil
	c.wsConn = nil
	c.muxMu.Unlock()

	c.log.Info("browser disconnected")
}

// wsAdapter adapts websocket to net.Conn for yamux
type wsAdapter struct {
	ws     *websocket.Conn
	reader io.Reader
	mu     sync.Mutex
}

func (w *wsAdapter) Read(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.reader == nil {
		_, r, err := w.ws.NextReader()
		if err != nil {
			return 0, err
		}
		w.reader = r
	}

	n, err := w.reader.Read(b)
	if err == io.EOF {
		w.reader = nil
		return n, nil
	}
	return n, err
}

func (w *wsAdapter) Write(b []byte) (int, error) {
	err := w.ws.WriteMessage(websocket.BinaryMessage, b)
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

func (w *wsAdapter) Close() error {
	return w.ws.Close()
}

func (w *wsAdapter) LocalAddr() net.Addr {
	return w.ws.LocalAddr()
}

func (w *wsAdapter) RemoteAddr() net.Addr {
	return w.ws.RemoteAddr()
}
