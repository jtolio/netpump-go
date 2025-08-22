package server

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
)

type Server struct {
	host     string
	port     int
	log      *slog.Logger
	upgrader websocket.Upgrader
	server   *http.Server
}

func New(host string, port int) *Server {
	return &Server{
		host: host,
		port: port,
		log:  slog.Default().With("component", "server"),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}
}

func (s *Server) Start() error {
	s.log.Info("netpump server starting", "host", s.host, "port", s.port)

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleHealth)
	mux.HandleFunc("/ws", s.handleWebSocket)

	s.server = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", s.host, s.port),
		Handler: mux,
	}

	return s.server.ListenAndServe()
}

func (s *Server) Stop() error {
	if s.server != nil {
		return s.server.Close()
	}
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "netpump server v2.0.0\n")
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	ws, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Error("websocket upgrade failed", "error", err)
		return
	}
	defer ws.Close()

	clientIP := s.getClientIP(r)
	s.log.Info("client connected", "ip", clientIP)

	// Setup yamux session
	conn := &wsAdapter{ws: ws}
	session, err := yamux.Server(conn, nil)
	if err != nil {
		s.log.Error("yamux setup failed", "error", err)
		return
	}
	defer session.Close()

	// Accept streams
	for {
		stream, err := session.Accept()
		if err != nil {
			if err == io.EOF {
				s.log.Info("client disconnected", "ip", clientIP)
			} else {
				s.log.Error("stream accept error", "error", err)
			}
			return
		}

		go s.handleStream(stream)
	}
}

func (s *Server) handleStream(stream net.Conn) {
	defer stream.Close()

	// Read target address length
	lenBuf := make([]byte, 1)
	if _, err := io.ReadFull(stream, lenBuf); err != nil {
		s.log.Error("failed to read address length", "error", err)
		return
	}

	// Read target address
	addrLen := int(lenBuf[0])
	addrBuf := make([]byte, addrLen)
	if _, err := io.ReadFull(stream, addrBuf); err != nil {
		s.log.Error("failed to read address", "error", err)
		return
	}

	target := string(addrBuf)

	// Connect to target
	conn, err := net.DialTimeout("tcp", target, 10*time.Second)
	if err != nil {
		s.log.Error("connection failed", "target", target, "error", err)
		stream.Write([]byte{0x01}) // Send failure
		return
	}
	defer conn.Close()

	// Send success
	stream.Write([]byte{0x00})

	s.log.Info("proxying", "target", target)

	// Relay data
	done := make(chan struct{}, 2)

	go func() {
		io.Copy(conn, stream)
		done <- struct{}{}
	}()

	go func() {
		io.Copy(stream, conn)
		done <- struct{}{}
	}()

	<-done
	s.log.Info("connection closed", "target", target)
}

func (s *Server) getClientIP(r *http.Request) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		return xff
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
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
