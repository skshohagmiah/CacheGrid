package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	cachegrid "github.com/skshohagmiah/cachegrid"
	"github.com/skshohagmiah/cachegrid/internal/server"
)

func main() {
	logger := log.New(os.Stdout, "[cachegrid] ", log.LstdFlags|log.Lmsgprefix)

	config := cachegrid.DefaultConfig()
	config.NumShards = envInt("CACHEGRID_SHARDS", 256)
	config.MaxMemoryMB = int64(envInt("CACHEGRID_MAX_MEMORY_MB", 0))
	config.DefaultTTL = envDuration("CACHEGRID_DEFAULT_TTL", 0)
	config.SweeperInterval = envDuration("CACHEGRID_SWEEPER_INTERVAL", time.Second)
	config.NodeName = envStr("CACHEGRID_NODE_NAME", "")
	config.ListenAddr = envStr("CACHEGRID_LISTEN_ADDR", "")
	config.GRPCPort = envInt("CACHEGRID_RPC_PORT", 7947)
	config.HTTPPort = envInt("CACHEGRID_HTTP_PORT", 6380)
	config.VirtualNodes = envInt("CACHEGRID_VIRTUAL_NODES", 150)

	// Storage mode
	switch envStr("CACHEGRID_STORAGE_MODE", "memory") {
	case "disk":
		config.StorageMode = cachegrid.Disk
		config.DiskPath = envStr("CACHEGRID_DISK_PATH", "./cachegrid-data")
	default:
		config.StorageMode = cachegrid.Memory
	}

	if seeds := envStr("CACHEGRID_SEEDS", ""); seeds != "" {
		config.Peers = strings.Split(seeds, ",")
	}

	switch envStr("CACHEGRID_MODE", "partitioned") {
	case "replicated":
		config.Mode = cachegrid.Replicated
	case "nearcache":
		config.Mode = cachegrid.NearCache
	default:
		config.Mode = cachegrid.Partitioned
	}

	cache, err := cachegrid.New(config)
	if err != nil {
		logger.Fatalf("failed to create cache: %v", err)
	}

	httpAddr := fmt.Sprintf(":%d", config.HTTPPort)
	srv := server.New(cache, httpAddr, logger)

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		logger.Printf("received signal %v, shutting down...", sig)
		if err := srv.Stop(10 * time.Second); err != nil {
			logger.Printf("HTTP server shutdown error: %v", err)
		}
		if err := cache.Shutdown(); err != nil {
			logger.Printf("cache shutdown error: %v", err)
		}
		os.Exit(0)
	}()

	logger.Printf("CacheGrid starting (mode=%s, storage=%s, http=%s)",
		modeName(config.Mode), storageModeName(config.StorageMode), httpAddr)

	if err := srv.Start(); err != nil {
		logger.Fatalf("HTTP server error: %v", err)
	}
}

func modeName(m cachegrid.Mode) string {
	switch m {
	case cachegrid.Replicated:
		return "replicated"
	case cachegrid.NearCache:
		return "nearcache"
	default:
		return "partitioned"
	}
}

func storageModeName(m cachegrid.StorageMode) string {
	switch m {
	case cachegrid.Disk:
		return "disk"
	default:
		return "memory"
	}
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
