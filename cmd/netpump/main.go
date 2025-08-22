package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/jtolio/netpump-go/private/client"
	"github.com/jtolio/netpump-go/private/server"
)

func main() {
	isClient := flag.Bool("client", false, "run as client")
	isServer := flag.Bool("server", false, "run as server")
	host := flag.String("host", "0.0.0.0", "host to listen on")
	port := flag.Int("port", 8080, "port for web interface (client) or websocket (server)")
	proxyPort := flag.Int("proxy-port", 1080, "SOCKS5 proxy port (client only)")
	serverURL := flag.String("server-url", "", "websocket server URL (client only)")
	flag.Parse()

	if (!*isClient && !*isServer) || (*isClient && *isServer) {
		fmt.Println("Usage: netpump --client or --server")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if *isClient && *serverURL == "" {
		fmt.Println("Error: --server-url is required for client mode")
		os.Exit(1)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	if *isServer {
		s := server.New(*host, *port)
		go func() {
			<-sigChan
			log.Println("Shutting down server...")
			s.Stop()
			os.Exit(0)
		}()
		if err := s.Start(); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}

	if *isClient {
		c := client.New(*host, *port, *proxyPort, *serverURL)
		go func() {
			<-sigChan
			log.Println("Shutting down client...")
			c.Stop()
			os.Exit(0)
		}()
		if err := c.Start(); err != nil {
			log.Fatalf("Client error: %v", err)
		}
	}
}
