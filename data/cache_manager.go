package data

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
)

// GlobalCacheManager manages all trade caches globally
type GlobalCacheManager struct {
	mu       sync.RWMutex
	cacheDir string
	caches   map[string]*TradeCache // key: cacheDir -> TradeCache
}

var (
	globalCacheManager *GlobalCacheManager
	cacheOnce          sync.Once
)

// GetGlobalCacheManager returns the singleton cache manager
func GetGlobalCacheManager() *GlobalCacheManager {
	cacheOnce.Do(func() {
		globalCacheManager = &GlobalCacheManager{
			cacheDir: "/tmp/banbot_cache",
			caches:   make(map[string]*TradeCache),
		}
	})
	return globalCacheManager
}

// RegisterCache registers a trade cache
func (m *GlobalCacheManager) RegisterCache(cache *TradeCache) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.caches[cache.cacheDir] = cache
}

// ClearAllCaches clears all registered caches
func (m *GlobalCacheManager) ClearAllCaches() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	var lastErr error
	for _, cache := range m.caches {
		if err := cache.ClearCache(); err != nil {
			lastErr = err
			log.Error("failed to clear cache", zap.String("dir", cache.cacheDir), zap.Error(err))
		}
	}
	
	// Also clear the default cache directory
	defaultCacheDir := filepath.Join(m.cacheDir, "trades")
	if _, err := os.Stat(defaultCacheDir); err == nil {
		if err := os.RemoveAll(defaultCacheDir); err != nil {
			lastErr = err
			log.Error("failed to clear default cache", zap.String("dir", defaultCacheDir), zap.Error(err))
		} else {
			log.Info("cleared default cache", zap.String("dir", defaultCacheDir))
		}
	}
	
	return lastErr
}

// ClearOldCaches removes cache files older than specified days
func (m *GlobalCacheManager) ClearOldCaches(days int) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	var lastErr error
	for _, cache := range m.caches {
		if err := cache.ClearOldCache(days); err != nil {
			lastErr = err
			log.Error("failed to clear old cache", zap.String("dir", cache.cacheDir), zap.Error(err))
		}
	}
	
	return lastErr
}

// GetGlobalCacheStats returns statistics for all caches
func (m *GlobalCacheManager) GetGlobalCacheStats() (totalFiles int, totalSize int64, err error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	for _, cache := range m.caches {
		files, size, _ := cache.GetCacheStats()
		totalFiles += files
		totalSize += size
	}
	
	// Also check default cache directory
	defaultCacheDir := filepath.Join(m.cacheDir, "trades")
	filepath.Walk(defaultCacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			totalFiles++
			totalSize += info.Size()
		}
		return nil
	})
	
	return totalFiles, totalSize, nil
}

// GetCacheSummary returns a human-readable summary of cache status
func (m *GlobalCacheManager) GetCacheSummary() string {
	files, size, _ := m.GetGlobalCacheStats()
	
	// Convert size to human-readable format
	var sizeStr string
	if size < 1024 {
		sizeStr = fmt.Sprintf("%d B", size)
	} else if size < 1024*1024 {
		sizeStr = fmt.Sprintf("%.2f KB", float64(size)/1024)
	} else if size < 1024*1024*1024 {
		sizeStr = fmt.Sprintf("%.2f MB", float64(size)/(1024*1024))
	} else {
		sizeStr = fmt.Sprintf("%.2f GB", float64(size)/(1024*1024*1024))
	}
	
	return fmt.Sprintf("Trade Cache: %d files, %s", files, sizeStr)
}

// ClearTradeCaches is a convenience function to clear all trade caches
func ClearTradeCaches() error {
	return GetGlobalCacheManager().ClearAllCaches()
}

// ClearOldTradeCaches is a convenience function to clear old trade caches
func ClearOldTradeCaches(days int) error {
	return GetGlobalCacheManager().ClearOldCaches(days)
}

// GetTradeCacheSummary is a convenience function to get cache summary
func GetTradeCacheSummary() string {
	return GetGlobalCacheManager().GetCacheSummary()
}