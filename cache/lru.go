package cache

import (
	"container/list"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry represents a single cached file with its metadata.
type Entry struct {
	Key        string    `json:"key"`        // bucket/path (e.g., "bucket/folder/file")
	Filename   string    `json:"filename"`   // local filename
	Size       int64     `json:"size"`       // file size in bytes
	AccessTime time.Time `json:"accessTime"` // last access time
	CreateTime time.Time `json:"createTime"` // when file was cached
}

// Stats contains cache performance metrics and current state information.
type Stats struct {
	Hits       int64  `json:"hits"`
	Misses     int64  `json:"misses"`
	Evictions  int64  `json:"evictions"`
	TotalBytes int64  `json:"totalBytes"`
	MaxBytes   int64  `json:"maxBytes"`
	EntryCount int    `json:"entryCount"`
	CacheDir   string `json:"cacheDir"`
}

// DiskLRUCache is a disk-backed LRU cache for storing files locally.
// It automatically evicts least recently used entries when the cache
// exceeds its configured maximum size.
type DiskLRUCache struct {
	mu           sync.RWMutex
	cacheDir     string
	filesDir     string
	maxSizeBytes int64
	currentSize  int64
	entries      map[string]*Entry        // key -> entry
	accessOrder  *list.List               // LRU tracking (front = most recent)
	accessMap    map[string]*list.Element // key -> list element
	stats        Stats
}

// NewDiskLRUCache creates a new disk-backed LRU cache at the specified directory
// with a maximum size limit in gigabytes. It loads any existing cached entries
// from disk on initialization.
func NewDiskLRUCache(cacheDir string, maxSizeGB int64) (*DiskLRUCache, error) {
	filesDir := filepath.Join(cacheDir, "files")
	if err := os.MkdirAll(filesDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	cache := &DiskLRUCache{
		cacheDir:     cacheDir,
		filesDir:     filesDir,
		maxSizeBytes: maxSizeGB * 1024 * 1024 * 1024, // GB to bytes
		entries:      make(map[string]*Entry),
		accessOrder:  list.New(),
		accessMap:    make(map[string]*list.Element),
		stats: Stats{
			MaxBytes: maxSizeGB * 1024 * 1024 * 1024,
			CacheDir: cacheDir,
		},
	}

	if err := cache.loadFromDisk(); err != nil {
		// Log warning but continue - cache will rebuild
		fmt.Printf("Warning: failed to load cache metadata: %v\n", err)
	}

	return cache, nil
}

// Get retrieves the local file path for a cached entry by its key.
// It updates the entry's access time and moves it to the front of the LRU list.
// Returns the file path and true if found, or an empty string and false if not.
func (c *DiskLRUCache) Get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, exists := c.entries[key]
	if !exists {
		c.stats.Misses++
		return "", false
	}

	// Verify file still exists
	filePath := filepath.Join(c.filesDir, entry.Filename)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// File was deleted externally, remove from cache
		c.removeEntry(key)
		c.stats.Misses++
		return "", false
	}

	// Update access time and move to front of LRU
	entry.AccessTime = time.Now()
	if elem, ok := c.accessMap[key]; ok {
		c.accessOrder.MoveToFront(elem)
	}

	c.stats.Hits++
	return filePath, true
}

// Put stores a file in the cache by reading from the provided io.Reader.
// If the key already exists, the old entry is replaced. The cache will
// automatically evict least recently used entries if needed to make room.
// Returns the local file path where the data was stored.
func (c *DiskLRUCache) Put(key string, data io.Reader) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If key already exists, remove old entry
	if _, exists := c.entries[key]; exists {
		c.removeEntry(key)
	}

	// Create a safe filename from the key
	filename := sanitizeFilename(key)
	filePath := filepath.Join(c.filesDir, filename)

	// Write to temp file first, then rename (atomic)
	tmpPath := filePath + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}

	size, err := io.Copy(file, data)
	file.Close()
	if err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	// Evict entries if needed to make room
	if err := c.evictIfNeeded(size); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to evict entries: %w", err)
	}

	// Rename temp file to final path
	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to rename temp file: %w", err)
	}

	// Create entry
	entry := &Entry{
		Key:        key,
		Filename:   filename,
		Size:       size,
		AccessTime: time.Now(),
		CreateTime: time.Now(),
	}

	c.entries[key] = entry
	elem := c.accessOrder.PushFront(key)
	c.accessMap[key] = elem
	c.currentSize += size
	c.stats.TotalBytes = c.currentSize
	c.stats.EntryCount = len(c.entries)

	// Persist metadata
	c.saveMetadata()

	return filePath, nil
}

// GetStats returns a snapshot of current cache statistics including
// hit/miss counts, eviction count, total size, and entry count.
func (c *DiskLRUCache) GetStats() Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := c.stats
	stats.TotalBytes = c.currentSize
	stats.EntryCount = len(c.entries)
	return stats
}

// evictIfNeeded removes least recently used entries until there's room for newSize
func (c *DiskLRUCache) evictIfNeeded(newSize int64) error {
	for c.currentSize+newSize > c.maxSizeBytes && c.accessOrder.Len() > 0 {
		// Get least recently used (back of list)
		elem := c.accessOrder.Back()
		if elem == nil {
			break
		}

		key := elem.Value.(string)
		c.removeEntry(key)
		c.stats.Evictions++
	}

	return nil
}

// removeEntry removes an entry from the cache (must be called with lock held)
func (c *DiskLRUCache) removeEntry(key string) {
	entry, exists := c.entries[key]
	if !exists {
		return
	}

	// Remove file
	filePath := filepath.Join(c.filesDir, entry.Filename)
	os.Remove(filePath)

	// Remove from data structures
	if elem, ok := c.accessMap[key]; ok {
		c.accessOrder.Remove(elem)
		delete(c.accessMap, key)
	}
	delete(c.entries, key)
	c.currentSize -= entry.Size
}

// loadFromDisk rebuilds cache state from existing files and metadata
func (c *DiskLRUCache) loadFromDisk() error {
	metadataPath := filepath.Join(c.cacheDir, "metadata.json")
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No metadata yet, fresh cache
		}
		return err
	}

	var entries []*Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}

	// Rebuild cache from metadata, verifying files exist
	for _, entry := range entries {
		filePath := filepath.Join(c.filesDir, entry.Filename)
		info, err := os.Stat(filePath)
		if err != nil {
			continue // File doesn't exist, skip
		}

		// Update size in case it changed
		entry.Size = info.Size()

		c.entries[entry.Key] = entry
		elem := c.accessOrder.PushBack(entry.Key) // Back = older
		c.accessMap[entry.Key] = elem
		c.currentSize += entry.Size
	}

	// Sort by access time (most recent to front)
	// Simple approach: rebuild list in sorted order
	type entryWithTime struct {
		key  string
		time time.Time
	}
	sorted := make([]entryWithTime, 0, len(c.entries))
	for key, entry := range c.entries {
		sorted = append(sorted, entryWithTime{key, entry.AccessTime})
	}
	// Sort by access time descending
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i].time.Before(sorted[j].time) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// Rebuild access order list
	c.accessOrder = list.New()
	c.accessMap = make(map[string]*list.Element)
	for _, e := range sorted {
		elem := c.accessOrder.PushBack(e.key)
		c.accessMap[e.key] = elem
	}

	c.stats.TotalBytes = c.currentSize
	c.stats.EntryCount = len(c.entries)

	return nil
}

// saveMetadata persists cache metadata to disk
func (c *DiskLRUCache) saveMetadata() error {
	entries := make([]*Entry, 0, len(c.entries))
	for _, entry := range c.entries {
		entries = append(entries, entry)
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}

	metadataPath := filepath.Join(c.cacheDir, "metadata.json")
	return os.WriteFile(metadataPath, data, 0644)
}

// sanitizeFilename creates a safe filename from a cache key
func sanitizeFilename(key string) string {
	// Replace path separators with underscores, keep the extension
	ext := filepath.Ext(key)
	base := key[:len(key)-len(ext)]

	// Replace / with _ and remove any other unsafe characters
	safe := ""
	for _, r := range base {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			safe += string(r)
		} else if r == '/' {
			safe += "_"
		}
	}

	return safe + ext
}
