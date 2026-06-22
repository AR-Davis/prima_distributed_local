// mycelium-api is the Ollama-compatible API gateway for the Mycelium
// distributed inference network. It routes requests through the
// Three Ravens: Huginn (fast local), Muninn (deep remote), Skald (precise).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aaronrdavis/mycelium-api/internal/api"
	"github.com/aaronrdavis/mycelium-api/internal/config"
	"github.com/aaronrdavis/mycelium-api/internal/node"
	"github.com/aaronrdavis/mycelium-api/internal/routing"
)

func main() {
	configPath := flag.String("config", "", "Path to config YAML (empty = defaults)")
	port := flag.Int("port", 0, "Override listen port")
	host := flag.String("host", "", "Override listen host")
	flag.Parse()

	var cfg *config.Config
	if *configPath != "" {
		loaded, err := config.Load(*configPath)
		if err != nil {
			log.Fatalf("Failed to load config from %s: %v", *configPath, err)
		}
		cfg = loaded
		log.Printf("[mycelium] Loaded config from %s", *configPath)
	} else {
		cfg = config.DefaultConfig()
		log.Println("[mycelium] Using default configuration")
	}

	if *port != 0 {
		cfg.Server.Port = *port
	}
	if *host != "" {
		cfg.Server.Host = *host
	}

	mgr := node.NewManager(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mgr.StartHealthChecks(ctx, 30*time.Second)

	router := routing.NewRouter(cfg, mgr)
	server := api.NewServer(cfg, router, mgr)

	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	fmt.Println()
	fmt.Println("  __  __  __  __  __  __  __  __  __  __  __")
	fmt.Println(" /  \\/  \\/  \\/  \\/  \\/  \\/  \\/  \\/  \\/  \\/  \\")
	fmt.Println("|  M  y  c  e  l  i  u  m    A  P  I  |")
	fmt.Println(" \\__/\\__/\\__/\\__/\\__/\\__/\\__/\\__/\\__/\\__/\\__/")
	fmt.Println()
	fmt.Printf("  Listening:  http://%s\n", addr)
	fmt.Printf("  Generate:   http://%s/api/generate\n", addr)
	fmt.Printf("  Chat:       http://%s/api/chat\n", addr)
	fmt.Printf("  Status:     http://%s/api/status\n", addr)
	fmt.Printf("  Routes:     http://%s/api/routes\n", addr)
	fmt.Println()
	fmt.Println("  Three Ravens:")
	fmt.Printf("    Huginn -> %v (fast, local)\n", cfg.Routing.Huginn.Pools)
	fmt.Printf("    Muninn -> %v (deep, remote)\n", cfg.Routing.Muninn.Pools)
	fmt.Printf("    Skald  -> %v (precise)\n", cfg.Routing.Skald.Pools)
	fmt.Printf("    Default: %s\n", cfg.Routing.Default)
	fmt.Printf("    Fallback: %s\n", cfg.Routing.FallbackLocal)
	fmt.Println()
	fmt.Println("  Nodes:")
	for _, n := range cfg.Nodes {
		fmt.Printf("    %-10s %s (pool=%s, weight=%d)\n", n.Name, n.Host, n.Pool, n.Weight)
	}
	fmt.Println()

	httpServer := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  300 * time.Second,
		WriteTimeout: 300 * time.Second,
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("[mycelium] Shutting down...")
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		httpServer.Shutdown(shutdownCtx)
	}()

	log.Fatal(httpServer.ListenAndServe())
}
