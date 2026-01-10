package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/autonoma-ai/midway/cache"
	"github.com/autonoma-ai/midway/handler"
	"github.com/autonoma-ai/midway/logger"
	"github.com/aws/aws-sdk-go-v2/config"
)

func main() {
	ctx := context.Background()

	logger.Init(ctx)

	// Load configuration from environment
	port := getEnv("PORT", "8900")
	cacheDir := getEnv("CACHE_DIR", defaultCacheDir())
	maxSizeGB := getEnvInt("CACHE_MAX_SIZE_GB", 50)

	logger.Info().Emitf("Starting midway service on port %s", port)
	logger.Info().Emitf("Cache directory: %s", cacheDir)
	logger.Info().Emitf("Max cache size: %d GB", maxSizeGB)

	// Initialize AWS config
	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(getEnv("AWS_REGION", "us-east-1")),
	)
	if err != nil {
		logger.Fatal().Emitf("Failed to load AWS config: %v", err)
		os.Exit(1)
	}

	// Initialize cache
	diskCache, err := cache.NewDiskLRUCache(cacheDir, int64(maxSizeGB))
	if err != nil {
		logger.Fatal().Emitf("Failed to initialize cache: %v", err)
		os.Exit(1)
	}

	stats := diskCache.GetStats()
	logger.Info().Emitf("Cache loaded: %d entries, %.2f MB", stats.EntryCount, float64(stats.TotalBytes)/(1024*1024))

	// Initialize S3 downloader
	downloader := cache.NewS3Downloader(awsCfg)

	// Initialize handler
	h := handler.NewHandler(diskCache, downloader)

	// Setup routes
	mux := http.NewServeMux()
	mux.HandleFunc("/health", h.HandleHealth)
	mux.HandleFunc("/stats", h.HandleStats)
	mux.HandleFunc("/", h.HandleFile) // Catch-all for file requests

	// Start server
	server := &http.Server{
		Addr:         fmt.Sprintf(":%s", port),
		Handler:      mux,
		ReadTimeout:  10 * time.Minute,
		WriteTimeout: 10 * time.Minute,
		IdleTimeout:  60 * time.Second,
	}

	logger.Info().Emitf("midway service started on :%s", port)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Fatal().Emitf("Server failed: %v", err)
		os.Exit(1)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func defaultCacheDir() string {
	if cacheDir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(cacheDir, "midway")
	}
	return filepath.Join(os.Getenv("HOME"), ".cache", "midway")
}
