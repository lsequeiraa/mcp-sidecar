package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/lsequeiraa/mcp-sidecar/sidecar"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	cfg := sidecar.LoadConfig()
	mgr := sidecar.NewManager(cfg)

	s := server.NewMCPServer(
		"mcp-sidecar",
		"0.1.0",
		server.WithToolCapabilities(true),
	)

	sidecar.RegisterTools(s, mgr)

	// Clean up all child processes on SIGINT / SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		mgr.StopAll()
		os.Exit(0)
	}()

	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
