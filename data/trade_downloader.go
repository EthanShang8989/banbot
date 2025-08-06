package data

import (
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
)

type BinanceDataDownloader struct {
	baseURL    string
	dataDir    string
	httpClient *http.Client
	mu         sync.Mutex
	market     string // "spot" or "futures/um" for USDT-margined futures
}

func NewBinanceDataDownloader(dataDir string, market string) *BinanceDataDownloader {
	// Normalize market name
	switch market {
	case "linear", "future", "futures":
		market = "futures/um" // U本位合约
	case "":
		market = "spot" // 默认现货
	}

	return &BinanceDataDownloader{
		baseURL: "https://data.binance.vision",
		dataDir: dataDir,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		market: market,
	}
}

// DownloadAggTradeData downloads aggregated trade data for a specific symbol and date
func (d *BinanceDataDownloader) DownloadTradeData(symbol, date string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Clean symbol for URL and file paths
	// ETH/USDT:USDT -> ETHUSDT for futures (remove the :USDT part)
	// BTC/USDT -> BTCUSDT for spot
	cleanSymbol := symbol
	// For futures, remove the settlement currency part (e.g., :USDT)
	if colonIdx := strings.Index(cleanSymbol, ":"); colonIdx != -1 {
		cleanSymbol = cleanSymbol[:colonIdx]
	}
	cleanSymbol = strings.ReplaceAll(cleanSymbol, "/", "")

	// Create directory structure including market type
	marketDir := "spot"
	if d.market == "futures/um" {
		marketDir = "futures"
	}
	symbolDir := filepath.Join(d.dataDir, "trades", marketDir, cleanSymbol)
	if err := os.MkdirAll(symbolDir, 0755); err != nil {
		return errs.New(errs.CodeIOWriteFail, err)
	}

	// Target file path (保存为zip格式)
	fileName := fmt.Sprintf("%s-aggTrades-%s.zip", cleanSymbol, date)
	filePath := filepath.Join(symbolDir, fileName)

	// Check if file already exists
	if _, err := os.Stat(filePath); err == nil {
		log.Debug("trade data file already exists", zap.String("file", filePath))
		return nil
	}

	// Download URL for aggregated trades
	// spot: https://data.binance.vision/data/spot/daily/aggTrades/BTCUSDT/BTCUSDT-aggTrades-2025-08-03.zip
	// futures: https://data.binance.vision/data/futures/um/daily/aggTrades/BTCUSDT/BTCUSDT-aggTrades-2025-08-04.zip
	url := fmt.Sprintf("%s/data/%s/daily/aggTrades/%s/%s", d.baseURL, d.market, cleanSymbol, fileName)
	log.Info("downloading aggregated trade data", zap.String("url", url), zap.String("market", d.market))

	// Create temporary file
	tmpFile := filePath + ".tmp"

	// Download file
	resp, err := d.httpClient.Get(url)
	if err != nil {
		return errs.New(errs.CodeNetFail, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errs.NewMsg(errs.CodeRunTime, "download failed: %d", resp.StatusCode)
	}

	// Write to temporary file
	out, err := os.Create(tmpFile)
	if err != nil {
		return errs.New(errs.CodeIOWriteFail, err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		os.Remove(tmpFile)
		return errs.New(errs.CodeIOWriteFail, err)
	}

	// Atomic rename
	if err := os.Rename(tmpFile, filePath); err != nil {
		os.Remove(tmpFile)
		return errs.New(errs.CodeIOWriteFail, err)
	}

	log.Info("trade data downloaded successfully", zap.String("file", filePath))
	return nil
}

// EnsureDataRange ensures all trade data files are available for the date range
func (d *BinanceDataDownloader) EnsureDataRange(symbol string, startDate, endDate time.Time) error {
	current := startDate
	for current.Before(endDate) || current.Equal(endDate) {
		dateStr := current.Format("2006-01-02")
		if err := d.DownloadTradeData(symbol, dateStr); err != nil {
			log.Warn("failed to download trade data",
				zap.String("symbol", symbol),
				zap.String("date", dateStr),
				zap.Error(err))
		}
		current = current.AddDate(0, 0, 1)
	}
	return nil
}

// GetFilePath returns the file path for a specific symbol and date
func (d *BinanceDataDownloader) GetFilePath(symbol, date string) string {
	// Clean symbol for file paths
	cleanSymbol := symbol
	// For futures, remove the settlement currency part (e.g., :USDT)
	if colonIdx := strings.Index(cleanSymbol, ":"); colonIdx != -1 {
		cleanSymbol = cleanSymbol[:colonIdx]
	}
	cleanSymbol = strings.ReplaceAll(cleanSymbol, "/", "")
	
	marketDir := "spot"
	if d.market == "futures/um" {
		marketDir = "futures"
	}
	fileName := fmt.Sprintf("%s-aggTrades-%s.zip", cleanSymbol, date)
	return filepath.Join(d.dataDir, "trades", marketDir, cleanSymbol, fileName)
}

// LoadCompressedCSV loads and decompresses a zip file containing CSV data
func LoadCompressedCSV(filePath string) ([][]string, error) {
	// Check file extension to determine compression type
	if filepath.Ext(filePath) == ".zip" {
		return loadZipCSV(filePath)
	}
	// Fallback to gzip for backward compatibility
	return loadGzipCSV(filePath)
}

// loadZipCSV loads CSV from a zip file
func loadZipCSV(filePath string) ([][]string, error) {
	reader, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	// Binance zip files typically contain a single CSV file
	if len(reader.File) == 0 {
		return nil, fmt.Errorf("no files in zip archive")
	}

	// Open the first file in the zip
	file := reader.File[0]
	rc, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	// Read all content
	content, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	// Parse CSV content
	lines := splitLines(string(content))
	records := make([][]string, 0, len(lines))

	for _, line := range lines {
		if line == "" {
			continue
		}
		fields := splitCSVLine(line)
		if len(fields) > 0 {
			records = append(records, fields)
		}
	}

	return records, nil
}

// loadGzipCSV loads CSV from a gzip file (for backward compatibility)
func loadGzipCSV(filePath string) ([][]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer gzReader.Close()

	// Read all content
	content, err := io.ReadAll(gzReader)
	if err != nil {
		return nil, err
	}

	// Parse CSV content
	lines := splitLines(string(content))
	records := make([][]string, 0, len(lines))

	for _, line := range lines {
		if line == "" {
			continue
		}
		fields := splitCSVLine(line)
		if len(fields) > 0 {
			records = append(records, fields)
		}
	}

	return records, nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func splitCSVLine(line string) []string {
	var fields []string
	var field string
	inQuote := false

	for i := 0; i < len(line); i++ {
		ch := line[i]

		if ch == '"' {
			inQuote = !inQuote
		} else if ch == ',' && !inQuote {
			fields = append(fields, field)
			field = ""
		} else if ch != '\r' {
			field += string(ch)
		}
	}

	if field != "" || len(fields) > 0 {
		fields = append(fields, field)
	}

	return fields
}
