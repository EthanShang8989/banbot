package data

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/strat"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	utils2 "github.com/banbox/banexg/utils"
	"go.uber.org/zap"
)

var _ IHistWSFeeder = (*TradeFeeder)(nil)
var _ IHistFeeder = (*TradeFeeder)(nil)

// TradeFeeder feeds trade data from files for backtesting
type TradeFeeder struct {
	// Basic info
	symbol     string
	dataDir    string
	market     string // "spot" or "linear" for futures
	downloader *BinanceDataDownloader

	// Memory cache system
	fileCache   map[string][]*banexg.Trade // Date -> trades mapping
	cacheSize   int                        // Cache days (default 3)
	cache       *TradeCache                // Hourly cache manager
	cacheReady  map[string]bool            // Track which dates have been processed

	// Hour-level memory cache for fast access
	hourCache    map[string][]*banexg.Trade // "date-hour" -> trades
	lastLoadHour int                        // Last loaded hour to track changes

	// Time management
	startMS     int64 // Backtest start time
	endMS       int64 // Backtest end time
	nextBarMS   int64 // Next bar end time
	timeFrameMS int64 // Time frame in milliseconds

	// Current processing window
	tradesToFire []*banexg.Trade // Trades in current time window

	// Concurrency safety
	mu      sync.RWMutex
	stopped bool
}

// NewTradeFeeder creates a new trade data feeder
func NewTradeFeeder(symbol, dataDir string, startMS, endMS int64, timeframe string, market string) *TradeFeeder {
	tfSecs := utils2.TFToSecs(timeframe)

	// Calculate cache size based on time range and configuration
	dayCount := int((endMS-startMS)/(24*60*60*1000)) + 1
	// Use configured cache hours to determine reasonable day limit
	maxCacheDays := config.Data.TradeCacheHours/24 + 1
	if maxCacheDays <= 0 {
		maxCacheDays = 3 // Default fallback
	}
	cacheSize := min(dayCount, maxCacheDays)

	return &TradeFeeder{
		symbol:       symbol,
		dataDir:      dataDir,
		market:       market,
		downloader:   NewBinanceDataDownloader(dataDir, market),
		fileCache:    make(map[string][]*banexg.Trade),
		cacheSize:    cacheSize,
		cache:        NewTradeCache(""), // Use default /tmp/banbot_cache
		cacheReady:   make(map[string]bool),
		hourCache:    make(map[string][]*banexg.Trade),
		lastLoadHour: -1,
		startMS:      startMS,
		endMS:        endMS,
		timeFrameMS:  int64(tfSecs) * 1000,
		nextBarMS:    startMS + int64(tfSecs)*1000,
	}
}

// IHistWSFeeder interface implementation

func (f *TradeFeeder) getSymbol() string {
	return f.symbol
}

func (f *TradeFeeder) Warmup(curMS, startMS int64, pBar *utils.PrgBar) *errs.Error {
	// Trade data doesn't need traditional warmup
	// Data is loaded on demand from files
	return nil
}

// IHistFeeder interface implementation

func (f *TradeFeeder) getNextMS() int64 {
	return f.nextBarMS
}

func (f *TradeFeeder) SetSeek(since int64) {
	f.startMS = since
	f.nextBarMS = since + f.timeFrameMS
	// Don't load data here, it will be loaded when needed in CallNext
}

func (f *TradeFeeder) SetEndMS(ms int64) {
	f.endMS = ms
}

func (f *TradeFeeder) GetBatch() Batch {
	// Check if we've reached the end
	if f.nextBarMS > f.endMS+f.timeFrameMS {
		return nil
	}

	// Ensure trades are loaded for current time window
	if f.tradesToFire == nil {
		f.loadNextMinuteTrades()
	}

	// Create batch for current time window
	startMS := f.nextBarMS - f.timeFrameMS
	batch := NewTradeBatch(f.symbol, startMS, f.nextBarMS)

	// Add trades in this time window
	for _, trade := range f.tradesToFire {
		batch.AddTrade(trade)
	}

	return batch
}

func (f *TradeFeeder) RunBatch(batch Batch) *errs.Error {
	if tradeBatch, ok := batch.(*TradeBatch); ok {
		if len(tradeBatch.Trades) == 0 {
			return nil
		}

		// Set virtual time to batch start
		btime.CurTimeMS = batch.StartTime()

		// Fire trades through strategy callbacks
		f.fireTrades(tradeBatch.Trades)

		// Set virtual time to batch end
		btime.CurTimeMS = batch.EndTime()
	}

	return nil
}

func (f *TradeFeeder) CallNext() {
	f.nextBarMS += f.timeFrameMS
	if f.nextBarMS <= f.endMS {
		f.loadNextMinuteTrades()
	}
}

// Internal methods

func (f *TradeFeeder) preloadFiles() *errs.Error {
	// Just ensure zip files are processed to hourly cache
	// Don't load all data into memory
	startTime := time.UnixMilli(f.startMS)
	endTime := time.UnixMilli(f.endMS)

	// Calculate days to process
	days := int((f.endMS-f.startMS)/(24*60*60*1000)) + 1

	log.Info("🚀 Initializing trade data cache",
		zap.String("symbol", f.symbol),
		zap.String("market", f.market),
		zap.Int("days_to_process", days),
		zap.String("cache_dir", f.cache.cacheDir),
		zap.String("start_date", startTime.Format("2006-01-02")),
		zap.String("end_date", endTime.Format("2006-01-02")))

	processedCount := 0
	skippedCount := 0

	for i := 0; i < days && i < f.cacheSize; i++ {
		date := startTime.AddDate(0, 0, i).Format("2006-01-02")

		// Check if date is within our range
		dayStart := startTime.AddDate(0, 0, i)
		if dayStart.After(endTime) {
			break
		}

		// Ensure zip is processed to cache
		zipPath := f.downloader.GetFilePath(f.symbol, date)
		if _, err := os.Stat(zipPath); err != nil {
			// Zip file doesn't exist, skip
			log.Debug("ZIP file not found, skipping",
				zap.String("symbol", f.symbol),
				zap.String("date", date),
				zap.String("path", zipPath))
			skippedCount++
			continue
		}

		log.Debug("Processing date",
			zap.String("symbol", f.symbol),
			zap.String("date", date),
			zap.String("zip_path", zipPath))

		// Use thread-safe check to prevent duplicate processing
		f.mu.Lock()
		needProcess := !f.cacheReady[date]
		if needProcess {
			f.cacheReady[date] = true // Mark immediately
		}
		f.mu.Unlock()
		
		if needProcess {
			err := f.cache.ProcessAndCacheZipFile(zipPath, f.market, f.symbol, date)
			if err != nil {
				log.Warn("Failed to process cache",
					zap.String("symbol", f.symbol),
					zap.String("date", date),
					zap.Error(err))
				// Reset flag on error
				f.mu.Lock()
				f.cacheReady[date] = false
				f.mu.Unlock()
				continue
			}
		}
		processedCount++
	}

	log.Info("📊 Trade cache initialization complete",
		zap.String("symbol", f.symbol),
		zap.Int("days_processed", processedCount),
		zap.Int("days_skipped", skippedCount),
		zap.String("cache_summary", GetTradeCacheSummary()))

	return nil
}

// Removed loadFullDayTrades - replaced by 3-hour sliding window approach

// Removed parseTradeRecord - functionality moved to trade_cache.go parseTradeFields

func (f *TradeFeeder) loadNextMinuteTrades() {
	startMS := f.nextBarMS - f.timeFrameMS
	endMS := f.nextBarMS

	// Get date and hour for the time window
	startTime := time.UnixMilli(startMS)
	endTime := time.UnixMilli(endMS)
	currentHour := startTime.Hour()

	// Check if we need to update the cache window
	if f.lastLoadHour != currentHour {
		f.preloadHourWindow(startTime)
		f.lastLoadHour = currentHour
	}

	// Filter trades from memory cache
	var trades []*banexg.Trade

	// If crossing day boundary, handle separately
	if startTime.Day() != endTime.Day() {
		// Load from current day
		dateStr := startTime.Format("2006-01-02")
		for hour := startTime.Hour(); hour <= 23; hour++ {
			key := fmt.Sprintf("%s-%02d", dateStr, hour)
			if hourTrades, exists := f.hourCache[key]; exists {
				for _, trade := range hourTrades {
					if trade.Timestamp >= startMS && trade.Timestamp < endMS {
						trades = append(trades, trade)
					}
				}
			}
		}
		// Load from next day
		nextDate := endTime.Format("2006-01-02")
		for hour := 0; hour <= endTime.Hour(); hour++ {
			key := fmt.Sprintf("%s-%02d", nextDate, hour)
			if hourTrades, exists := f.hourCache[key]; exists {
				for _, trade := range hourTrades {
					if trade.Timestamp >= startMS && trade.Timestamp < endMS {
						trades = append(trades, trade)
					}
				}
			}
		}
	} else {
		// Same day, filter from relevant hours
		dateStr := startTime.Format("2006-01-02")
		startHour := startTime.Hour()
		endHour := endTime.Hour()

		for hour := startHour; hour <= endHour; hour++ {
			key := fmt.Sprintf("%s-%02d", dateStr, hour)
			if hourTrades, exists := f.hourCache[key]; exists {
				for _, trade := range hourTrades {
					if trade.Timestamp >= startMS && trade.Timestamp < endMS {
						trades = append(trades, trade)
					}
				}
			}
		}
	}

	f.tradesToFire = trades
}

// preloadHourWindow preloads a configurable hour window around the current time
func (f *TradeFeeder) preloadHourWindow(currentTime time.Time) {
	currentHour := currentTime.Hour()
	currentDate := currentTime.Format("2006-01-02")

	// Get cache window size from config (default to 3 hours if not set)
	cacheHours := config.Data.TradeCacheHours
	if cacheHours <= 0 {
		cacheHours = 3 // Default fallback
	}

	// Clear old cache entries to prevent memory buildup
	// Keep max double the cache window size for efficiency
	maxCacheEntries := cacheHours * 2
	if len(f.hourCache) > maxCacheEntries {
		f.hourCache = make(map[string][]*banexg.Trade)
	}

	// Build hours to load around current time
	var hoursToLoad []struct {
		date string
		hour int
	}
	
	// Load window from current hour forward: [current, current+1, ..., current+cacheHours-1]
	for i := 0; i < cacheHours; i++ {
		targetHour := currentHour + i
		targetDate := currentDate
		targetDateTime := currentTime
		
		// Handle day boundary crossings
		if targetHour >= 24 {
			targetHour -= 24
			targetDateTime = currentTime.AddDate(0, 0, 1)
			targetDate = targetDateTime.Format("2006-01-02")
		}
		
		// Check if target time is within backtest range
		targetTimeMS := targetDateTime.Truncate(time.Hour).Add(time.Duration(targetHour) * time.Hour).UnixMilli()
		if targetTimeMS > f.endMS {
			log.Debug("Skipping hour beyond backtest end time",
				zap.String("symbol", f.symbol),
				zap.String("target_time", targetDateTime.Format("2006-01-02 15:04")),
				zap.Int("target_hour", targetHour),
				zap.Int64("target_ms", targetTimeMS),
				zap.Int64("end_ms", f.endMS))
			break // Stop loading further hours
		}
		
		hoursToLoad = append(hoursToLoad, struct {
			date string
			hour int
		}{targetDate, targetHour})
	}


	loadedCount := 0
	for _, hourInfo := range hoursToLoad {
		key := fmt.Sprintf("%s-%02d", hourInfo.date, hourInfo.hour)

		// Skip if already loaded
		if _, exists := f.hourCache[key]; exists {
			continue
		}

		// Ensure date is processed to disk cache (thread-safe check)
		f.mu.Lock()
		needProcess := !f.cacheReady[hourInfo.date]
		if needProcess {
			f.cacheReady[hourInfo.date] = true // Mark immediately to prevent duplicate processing
		}
		f.mu.Unlock()
		
		if needProcess {
			zipPath := f.downloader.GetFilePath(f.symbol, hourInfo.date)
			err := f.cache.ProcessAndCacheZipFile(zipPath, f.market, f.symbol, hourInfo.date)
			if err != nil {
				log.Debug("Failed to process cache for preload",
					zap.String("symbol", f.symbol),
					zap.String("date", hourInfo.date),
					zap.Error(err))
				// Reset flag on error so it can be retried later
				f.mu.Lock()
				f.cacheReady[hourInfo.date] = false
				f.mu.Unlock()
				continue
			}
		}

		// Load hour data from disk cache to memory
		hourTrades, err := f.cache.LoadHourlyTrades(f.market, f.symbol, hourInfo.date, hourInfo.hour)
		if err != nil {
			// Hour might not have data, continue
			continue
		}

		f.hourCache[key] = hourTrades
		loadedCount++
	}

	if loadedCount > 0 {
		log.Debug("⚡ Preloaded hour window",
			zap.String("symbol", f.symbol),
			zap.String("center_time", currentTime.Format("2006-01-02 15:04")),
			zap.Int("cache_window_hours", cacheHours),
			zap.Int("hours_loaded", loadedCount),
			zap.Int("total_cached_hours", len(f.hourCache)))
	}
}

// Old loadHourRangeTrades function removed - now using configurable memory window approach

// Removed ensureDataAvailable - no longer needed with hourly cache approach

func (f *TradeFeeder) fireTrades(trades []*banexg.Trade) {
	if len(trades) == 0 {
		return
	}

	// Get all strategy jobs that subscribe to trade data
	// We need to iterate through all accounts
	allJobs := strat.GetJobs("")
	for _, pairJobs := range allJobs {
		for _, job := range pairJobs {
			if job.Strat.OnWsTrades != nil {
				// Check if strategy subscribes to this pair's trades
				if wsSubVal, ok := job.Strat.WsSubs[core.WsSubTrade]; ok {
					if wsSubVal == "_cur_" || wsSubVal == f.symbol {
						job.Strat.OnWsTrades(job, f.symbol, trades)
					}
				}
			}
		}
	}
}

// Start initializes and starts the feeder
func (f *TradeFeeder) Start() error {
	// Download data if needed
	startTime := time.UnixMilli(f.startMS)
	endTime := time.UnixMilli(f.endMS)

	err := f.downloader.EnsureDataRange(f.symbol, startTime, endTime)
	if err != nil {
		return err
	}

	// Preload initial data
	err2 := f.preloadFiles()
	if err2 != nil {
		return err2
	}
	return nil
}

// Stop stops the feeder
func (f *TradeFeeder) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = true
	f.fileCache = make(map[string][]*banexg.Trade)
}

// ClearCache clears all cached trade data
func (f *TradeFeeder) ClearCache() error {
	return f.cache.ClearCache()
}

// ClearOldCache removes cache files older than specified days
func (f *TradeFeeder) ClearOldCache(days int) error {
	return f.cache.ClearOldCache(days)
}

// GetCacheStats returns cache statistics
func (f *TradeFeeder) GetCacheStats() (totalFiles int, totalSize int64, err error) {
	return f.cache.GetCacheStats()
}
