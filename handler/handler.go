package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/autonoma-ai/midway/cache"
	"github.com/autonoma-ai/midway/logger"
)

type Handler struct {
	cache      *cache.DiskLRUCache
	downloader *cache.S3Downloader
}

func NewHandler(c *cache.DiskLRUCache, d *cache.S3Downloader) *Handler {
	return &Handler{
		cache:      c,
		downloader: d,
	}
}

// HandleFile handles requests for cached files: GET /{bucket}/{key...}
func (h *Handler) HandleFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract bucket/key from URL path (remove leading /)
	key := strings.TrimPrefix(r.URL.Path, "/")
	if key == "" || key == "health" || key == "stats" {
		http.NotFound(w, r)
		return
	}

	startTime := time.Now()

	// Check cache
	filePath, found := h.cache.Get(key)
	if found {
		logger.Info().Emitf("Cache hit: %s (served in %v)", key, time.Since(startTime))
		http.ServeFile(w, r, filePath)
		return
	}

	// Cache miss - download from S3
	logger.Info().Emitf("Cache miss: %s, downloading from S3...", key)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	reader, size, err := h.downloader.Download(ctx, key)
	if err != nil {
		logger.Error().Emitf("Failed to download %s: %v", key, err)
		http.Error(w, fmt.Sprintf("Failed to download: %v", err), http.StatusNotFound)
		return
	}
	defer reader.Close()

	logger.Info().Emitf("Downloaded %s (%d bytes), caching...", key, size)

	// Store in cache
	filePath, err = h.cache.Put(key, reader)
	if err != nil {
		logger.Error().Emitf("Failed to cache %s: %v", key, err)
		http.Error(w, fmt.Sprintf("Failed to cache: %v", err), http.StatusInternalServerError)
		return
	}

	logger.Info().Emitf("Cached %s in %v", key, time.Since(startTime))

	// Serve the file
	http.ServeFile(w, r, filePath)
}

// HandleHealth handles health check requests: GET /health
func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}

// HandleStats handles stats requests: GET /stats
func (h *Handler) HandleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := h.cache.GetStats()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}
