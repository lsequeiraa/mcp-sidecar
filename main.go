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

	sec, err := sidecar.LoadSecurityConfig()
	if err != nil {
		log.Fatalf("security config: %v", err)
	}

	audit, err := sidecar.NewAuditLogger(os.Getenv("SIDECAR_AUDIT_LOG"))
	if err != nil {
		log.Fatalf("audit logger: %v", err)
	}

	mgr := sidecar.NewManager(cfg, sec, audit)

	defer func() {
		mgr.StopAll()
		if audit != nil {
			audit.Close()
		}
	}()

	s := server.NewMCPServer(
		"mcp-sidecar",
		"0.1.0",
		server.WithToolCapabilities(true),
	)

	sidecar.RegisterTools(s, mgr)

	// Clean up all child processes on SIGINT / SIGTERM.
	// Note: os.Exit does not run deferred functions, so we must
	// clean up explicitly here. The defer above covers the normal
	// exit path (when ServeStdio returns).
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		mgr.StopAll()
		if audit != nil {
			audit.Close()
		}
		os.Exit(0)
	}()

	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
