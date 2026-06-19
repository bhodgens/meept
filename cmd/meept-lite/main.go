// Command meept-lite is a minimalistic console client for meept.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/caimlas/meept/internal/sharedclient"
	"github.com/caimlas/meept/internal/transport"
)

var (
	socketPath    string
	sessionName   string
	transportFlag string
	httpURLFlag   string
)

func main() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "~"
	}

	defaultSocket := filepath.Join(homeDir, ".meept", "meept.sock")
	defaultHTTP := "http://localhost:8081"

	flag.StringVar(&socketPath, "socket", defaultSocket, "Unix socket path (for RPC)")
	flag.StringVar(&socketPath, "s", defaultSocket, "Unix socket path (shorthand)")
	flag.StringVar(&sessionName, "session", "", "Session name (default: most recent or 'default')")
	flag.StringVar(&transportFlag, "transport", "rpc", "Transport: rpc or http")
	flag.StringVar(&httpURLFlag, "http-url", defaultHTTP, "HTTP base URL for daemon")

	flag.Parse()

	// Create transport client
	cfg := &transport.Config{
		Transport:   transportFlag,
		SocketPath:  socketPath,
		HTTPBaseURL: httpURLFlag,
	}

	client, err := transport.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create transport: %v\n", err)
		os.Exit(1)
	}

	if err := client.Connect(); err != nil {
		host := "localhost:8081"
		if transportFlag == "http" && httpURLFlag != "http://localhost:8081" {
			host = httpURLFlag
		}
		fmt.Fprintf(os.Stderr, "failed to connect to daemon (%v)\n", err)
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "make sure the daemon is running:")
		if transportFlag == "http" {
			fmt.Fprintf(os.Stderr, "  meept daemon start --http-listen %s\n\n", host)
		} else {
			fmt.Fprintln(os.Stderr, "  meept daemon start")
		}
		fmt.Fprintln(os.Stderr, "or use --socket/-s or --http-url to specify the address")
		os.Exit(1)
	}
	defer client.Close()

	// Create session manager
	sessionMgr := sharedclient.NewSessionManager(client, "default")
	if err := sessionMgr.LoadOrCreateSession(context.TODO(), sessionName); err != nil {
		fmt.Fprintf(os.Stderr, "failed to load session: %v\n", err)
		os.Exit(1)
	}

	// Start the TUI
	tui := NewTUI(client, sessionMgr)
	if err := tui.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
}
