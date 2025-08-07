package data

import (
	"archive/zip"
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/banbox/banexg"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
)

// Trade二进制格式 (固定长度，便于快速读取)
// ID: 8 bytes (uint64)
// Price: 8 bytes (float64)
// Amount: 8 bytes (float64)
// Timestamp: 8 bytes (int64)
// Side: 1 byte (0=buy, 1=sell)
// Total: 33 bytes per trade
const tradeBinarySize = 33

// TradeCache manages cached trade data in both CSV and binary formats
type TradeCache struct {
	cacheDir string
	mu       sync.RWMutex
	// Map of date -> hour -> file path (for CSV compatibility)
	hourlyFiles map[string]map[int]string
	// Use binary format by default
	useBinary bool
}

// NewTradeCache creates a new trade cache manager (defaults to binary format)
func NewTradeCache(baseDir string) *TradeCache {
	if baseDir == "" {
		baseDir = "/tmp/banbot_cache"
	}
	cache := &TradeCache{
		cacheDir:    baseDir,
		hourlyFiles: make(map[string]map[int]string),
		useBinary:   true, // Default to binary format for better performance
	}

	// Register with global cache manager
	GetGlobalCacheManager().RegisterCache(cache)

	return cache
}

// GetCachePath returns the cache path for a specific symbol, date and hour
func (tc *TradeCache) GetCachePath(market, symbol, date string, hour int) string {
	// Clean symbol (remove special characters)
	cleanSymbol := strings.ReplaceAll(symbol, "/", "")
	cleanSymbol = strings.ReplaceAll(cleanSymbol, ":", "")

	if tc.useBinary {
		return filepath.Join(
			tc.cacheDir,
			"trades_bin",
			market,
			cleanSymbol,
			date,
			fmt.Sprintf("%02d.bin", hour),
		)
	}

	return filepath.Join(
		tc.cacheDir,
		"trades",
		market,
		cleanSymbol,
		date,
		fmt.Sprintf("%02d.csv", hour),
	)
}

// IsCached checks if hourly data is cached
func (tc *TradeCache) IsCached(market, symbol, date string, hour int) bool {
	cachePath := tc.GetCachePath(market, symbol, date, hour)
	_, err := os.Stat(cachePath)
	return err == nil
}

// LoadHourlyTrades loads trades for a specific hour from cache
func (tc *TradeCache) LoadHourlyTrades(market, symbol, date string, hour int) ([]*banexg.Trade, error) {
	if !tc.IsCached(market, symbol, date, hour) {
		return nil, fmt.Errorf("cache not found for %s %s %02d:00", date, symbol, hour)
	}

	cachePath := tc.GetCachePath(market, symbol, date, hour)

	// Load based on format
	if tc.useBinary {
		return tc.loadBinaryTrades(cachePath)
	}

	// Load CSV format

	file, err := os.Open(cachePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Pre-allocate trades slice with estimated capacity
	fileInfo, _ := file.Stat()
	estimatedLines := int(fileInfo.Size() / 80) // Estimate ~80 bytes per line
	trades := make([]*banexg.Trade, 0, estimatedLines)

	// Use larger buffer for reading
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 256*1024)
	scanner.Buffer(buf, 256*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// Use fast parsing without full string split
		trade := parseTradeFieldsFast(line)
		if trade != nil {
			trades = append(trades, trade)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return trades, nil
}

// ProcessAndCacheZipFile extracts and splits a zip file into hourly binary files
func (tc *TradeCache) ProcessAndCacheZipFile(zipPath, market, symbol, date string) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	// Check if already processed
	allCached := true
	cachedHours := 0
	for hour := 0; hour < 24; hour++ {
		if tc.IsCached(market, symbol, date, hour) {
			cachedHours++
		} else {
			allCached = false
		}
	}
	if allCached {
		log.Debug("✓ Using cached hourly data",
			zap.String("symbol", symbol),
			zap.String("date", date),
			zap.Int("cached_hours", cachedHours))
		return nil
	}

	log.Info("📦 Processing ZIP to hourly cache",
		zap.String("symbol", symbol),
		zap.String("date", date),
		zap.String("zip_path", zipPath),
		zap.Int("already_cached_hours", cachedHours))

	startTime := time.Now()

	// Open zip file
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	if len(reader.File) == 0 {
		return fmt.Errorf("empty zip file")
	}

	// Find CSV file in zip
	var csvFile *zip.File
	for _, f := range reader.File {
		if strings.HasSuffix(f.Name, ".csv") {
			csvFile = f
			break
		}
	}

	if csvFile == nil {
		return fmt.Errorf("no CSV file found in zip")
	}

	// Open CSV from zip
	rc, err := csvFile.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	// Collect trades by hour for binary storage
	hourlyTrades := make(map[int][]*banexg.Trade)

	// Read and split by hour with optimized parsing
	// Use larger buffer for scanner (1MB instead of default 64KB)
	scanner := bufio.NewScanner(rc)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 1024*1024)

	lineCount := 0
	tradeCount := 0

	for scanner.Scan() {
		line := scanner.Text()
		lineCount++

		if line == "" {
			continue
		}

		// Optimized: Find only the 6th field (timestamp) without full split
		// Format: id,price,amount,first,last,timestamp,buyerMaker
		// We need field index 5 (0-based)
		commaCount := 0
		startIdx := 0
		endIdx := len(line)

		for i, ch := range line {
			if ch == ',' {
				commaCount++
				if commaCount == 5 {
					startIdx = i + 1
				} else if commaCount == 6 {
					endIdx = i
					break
				}
			}
		}

		// Check if we found enough fields
		if commaCount < 6 {
			continue
		}

		// Parse timestamp directly from substring
		timestamp, err := strconv.ParseInt(line[startIdx:endIdx], 10, 64)
		if err != nil {
			continue
		}

		// Determine hour using bitwise operations (faster than time.UnixMilli)
		// timestamp is in milliseconds, we need hours
		// 1 hour = 3600000 ms
		hoursSinceEpoch := timestamp / 3600000
		hour := int(hoursSinceEpoch % 24)

		// Parse full trade data for binary storage
		trade := parseTradeFieldsFast(line)
		if trade != nil {
			hourlyTrades[hour] = append(hourlyTrades[hour], trade)
			tradeCount++
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	// Write all hourly trades to files
	for hour := 0; hour < 24; hour++ {
		trades, exists := hourlyTrades[hour]
		if !exists || len(trades) == 0 {
			continue
		}

		// Write to cache (binary or CSV based on useBinary flag)
		cachePath := tc.GetCachePath(market, symbol, date, hour)
		if tc.useBinary {
			if err := tc.writeBinaryTrades(trades, cachePath); err != nil {
				return err
			}
		} else {
			if err := tc.writeCSVTrades(trades, cachePath); err != nil {
				return err
			}
		}
	}

	elapsed := time.Since(startTime)
	formatType := "CSV"
	if tc.useBinary {
		formatType = "binary"
	}
	log.Info("✅ Hourly cache created successfully",
		zap.String("format", formatType),
		zap.String("symbol", symbol),
		zap.String("date", date),
		zap.Int("total_trades", tradeCount),
		zap.Int("hours_with_data", len(hourlyTrades)),
		zap.Duration("processing_time", elapsed))

	return nil
}

// ClearCache removes all cached data
func (tc *TradeCache) ClearCache() error {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	// Clear CSV cache
	cacheTradesDir := filepath.Join(tc.cacheDir, "trades")
	if _, err := os.Stat(cacheTradesDir); err == nil {
		if err := os.RemoveAll(cacheTradesDir); err != nil {
			return err
		}
		log.Info("cleared CSV trade cache", zap.String("dir", cacheTradesDir))
	}

	// Clear binary cache
	cacheBinDir := filepath.Join(tc.cacheDir, "trades_bin")
	if _, err := os.Stat(cacheBinDir); err == nil {
		if err := os.RemoveAll(cacheBinDir); err != nil {
			return err
		}
		log.Info("cleared binary trade cache", zap.String("dir", cacheBinDir))
	}

	tc.hourlyFiles = make(map[string]map[int]string)
	return nil
}

// ClearOldCache removes cache files older than specified days
func (tc *TradeCache) ClearOldCache(days int) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	cutoffTime := time.Now().AddDate(0, 0, -days)
	cacheTradesDir := filepath.Join(tc.cacheDir, "trades")

	err := filepath.Walk(cacheTradesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		if !info.IsDir() && info.ModTime().Before(cutoffTime) {
			os.Remove(path)
			log.Debug("removed old cache file", zap.String("path", path))
		}

		return nil
	})

	return err
}

// GetCacheStats returns statistics about the cache
func (tc *TradeCache) GetCacheStats() (totalFiles int, totalSize int64, err error) {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	// Get stats for both CSV and binary caches
	dirs := []string{
		filepath.Join(tc.cacheDir, "trades"),
		filepath.Join(tc.cacheDir, "trades_bin"),
	}

	for _, dir := range dirs {
		err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}

			if !info.IsDir() {
				totalFiles++
				totalSize += info.Size()
			}

			return nil
		})
	}

	return totalFiles, totalSize, err
}

// Helper function to parse trade fields - optimized version
func parseTradeFields(fields []string) *banexg.Trade {
	if len(fields) < 7 {
		return nil
	}

	timestamp, err := strconv.ParseInt(fields[5], 10, 64)
	if err != nil {
		return nil
	}

	price, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return nil
	}

	amount, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return nil
	}

	// Optimize: avoid map lookup
	side := "buy"
	if fields[6] == "true" || fields[6] == "1" {
		side = "sell"
	}

	return &banexg.Trade{
		ID:        fields[0],
		Price:     price,
		Amount:    amount,
		Timestamp: timestamp,
		Side:      side,
	}
}

// parseTradeFieldsFast parses trade line without full string split
func parseTradeFieldsFast(line string) *banexg.Trade {
	// Format: id,price,amount,first,last,timestamp,buyerMaker
	var fields [7]string
	fieldIdx := 0
	startIdx := 0

	for i := 0; i < len(line) && fieldIdx < 7; i++ {
		if line[i] == ',' {
			fields[fieldIdx] = line[startIdx:i]
			fieldIdx++
			startIdx = i + 1
		}
	}
	// Last field
	if fieldIdx < 7 && startIdx < len(line) {
		fields[fieldIdx] = line[startIdx:]
		fieldIdx++
	}

	if fieldIdx < 7 {
		return nil
	}

	timestamp, err := strconv.ParseInt(fields[5], 10, 64)
	if err != nil {
		return nil
	}

	price, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return nil
	}

	amount, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return nil
	}

	side := "buy"
	if fields[6] == "true" || fields[6] == "1" {
		side = "sell"
	}

	return &banexg.Trade{
		ID:        fields[0],
		Price:     price,
		Amount:    amount,
		Timestamp: timestamp,
		Side:      side,
	}
}

// loadBinaryTrades loads trades from binary file
func (tc *TradeCache) loadBinaryTrades(cachePath string) ([]*banexg.Trade, error) {
	file, err := os.Open(cachePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Use buffered reader for better performance
	reader := bufio.NewReaderSize(file, 1024*1024) // 1MB buffer

	// Read trade count
	var count uint32
	if err := binary.Read(reader, binary.LittleEndian, &count); err != nil {
		return nil, err
	}

	// Read all data at once
	totalSize := int(count) * tradeBinarySize
	bigBuf := make([]byte, totalSize)
	if _, err := io.ReadFull(reader, bigBuf); err != nil {
		return nil, err
	}

	// Pre-allocate trades slice
	trades := make([]*banexg.Trade, count)

	// Batch decode
	for i := uint32(0); i < count; i++ {
		offset := int(i) * tradeBinarySize
		trades[i] = decodeTrade(bigBuf[offset : offset+tradeBinarySize])
	}

	return trades, nil
}

// writeBinaryTrades writes trades to binary file
func (tc *TradeCache) writeBinaryTrades(trades []*banexg.Trade, cachePath string) error {
	// Create directory if needed
	dir := filepath.Dir(cachePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	file, err := os.Create(cachePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Use buffered writer
	writer := bufio.NewWriterSize(file, 1024*1024) // 1MB buffer
	defer writer.Flush()

	// Write trade count
	if err := binary.Write(writer, binary.LittleEndian, uint32(len(trades))); err != nil {
		return err
	}

	// Pre-allocate big buffer for all trades
	totalSize := len(trades) * tradeBinarySize
	bigBuf := make([]byte, totalSize)

	// Batch encode
	for i, trade := range trades {
		offset := i * tradeBinarySize
		encodeTrade(trade, bigBuf[offset:offset+tradeBinarySize])
	}

	// Write all at once
	if _, err := writer.Write(bigBuf); err != nil {
		return err
	}

	return nil
}

// writeCSVTrades writes trades to CSV file
func (tc *TradeCache) writeCSVTrades(trades []*banexg.Trade, cachePath string) error {
	// Create directory if needed
	dir := filepath.Dir(cachePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	file, err := os.Create(cachePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Use buffered writer
	writer := bufio.NewWriterSize(file, 256*1024) // 256KB buffer
	defer writer.Flush()

	for _, trade := range trades {
		buyerMaker := "false"
		if trade.Side == "sell" {
			buyerMaker = "true"
		}
		line := fmt.Sprintf("%s,%.6f,%.6f,0,0,%d,%s\n",
			trade.ID, trade.Price, trade.Amount, trade.Timestamp, buyerMaker)
		writer.WriteString(line)
	}

	return nil
}

// encodeTrade encodes a trade to binary format
func encodeTrade(trade *banexg.Trade, buf []byte) {
	// ID as uint64
	var idNum uint64
	fmt.Sscanf(trade.ID, "%d", &idNum)
	binary.LittleEndian.PutUint64(buf[0:8], idNum)

	// Price
	binary.LittleEndian.PutUint64(buf[8:16], math.Float64bits(trade.Price))

	// Amount
	binary.LittleEndian.PutUint64(buf[16:24], math.Float64bits(trade.Amount))

	// Timestamp
	binary.LittleEndian.PutUint64(buf[24:32], uint64(trade.Timestamp))

	// Side (0=buy, 1=sell)
	if trade.Side == "sell" {
		buf[32] = 1
	} else {
		buf[32] = 0
	}
}

// decodeTrade decodes a trade from binary format
func decodeTrade(buf []byte) *banexg.Trade {
	trade := &banexg.Trade{}

	// ID
	idNum := binary.LittleEndian.Uint64(buf[0:8])
	trade.ID = fmt.Sprintf("%d", idNum)

	// Price
	trade.Price = math.Float64frombits(binary.LittleEndian.Uint64(buf[8:16]))

	// Amount
	trade.Amount = math.Float64frombits(binary.LittleEndian.Uint64(buf[16:24]))

	// Timestamp
	trade.Timestamp = int64(binary.LittleEndian.Uint64(buf[24:32]))

	// Side
	if buf[32] == 1 {
		trade.Side = "sell"
	} else {
		trade.Side = "buy"
	}

	return trade
}
