package data

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBinanceDataDownloader(t *testing.T) {
	// 创建临时目录用于测试
	tempDir := "/tmp/banbot_trade_test"
	err := os.MkdirAll(tempDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir) // 清理测试目录

	// 创建下载器（测试现货市场）
	downloader := NewBinanceDataDownloader(tempDir, "spot")

	// 测试下载单个文件
	t.Run("DownloadSingleFile", func(t *testing.T) {
		symbol := "BTCUSDT"
		date := "2024-01-01"

		t.Logf("Downloading trade data for %s on %s...", symbol, date)
		err := downloader.DownloadTradeData(symbol, date)
		if err != nil {
			t.Errorf("Failed to download trade data: %v", err)
			return
		}

		// 检查文件是否存在
		expectedFile := filepath.Join(tempDir, "trades", "spot", symbol, fmt.Sprintf("%s-aggTrades-%s.zip", symbol, date))
		if _, err := os.Stat(expectedFile); os.IsNotExist(err) {
			t.Errorf("Expected file not found: %s", expectedFile)
		} else {
			t.Logf("Successfully downloaded: %s", expectedFile)

			// 获取文件大小
			fileInfo, _ := os.Stat(expectedFile)
			t.Logf("File size: %.2f MB", float64(fileInfo.Size())/(1024*1024))
		}
	})

	// 测试重复下载（应该跳过）
	t.Run("SkipExistingFile", func(t *testing.T) {
		symbol := "BTCUSDT"
		date := "2024-01-01"

		t.Logf("Attempting to re-download %s on %s (should skip)...", symbol, date)
		err := downloader.DownloadTradeData(symbol, date)
		if err != nil {
			t.Errorf("Failed on re-download: %v", err)
		} else {
			t.Logf("Successfully skipped existing file")
		}
	})

	// 测试下载日期范围
	t.Run("DownloadDateRange", func(t *testing.T) {
		symbol := "ETHUSDT"
		startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		endDate := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)

		t.Logf("Downloading %s from %s to %s...", symbol,
			startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))

		err := downloader.EnsureDataRange(symbol, startDate, endDate)
		if err != nil {
			t.Errorf("Failed to download date range: %v", err)
			return
		}

		// 检查所有文件
		for d := startDate; !d.After(endDate); d = d.AddDate(0, 0, 1) {
			dateStr := d.Format("2006-01-02")
			expectedFile := filepath.Join(tempDir, "trades", "spot", symbol,
				fmt.Sprintf("%s-aggTrades-%s.zip", symbol, dateStr))

			if _, err := os.Stat(expectedFile); os.IsNotExist(err) {
				t.Errorf("Missing file for date %s: %s", dateStr, expectedFile)
			} else {
				fileInfo, _ := os.Stat(expectedFile)
				t.Logf("  %s: %.2f MB", dateStr, float64(fileInfo.Size())/(1024*1024))
			}
		}
	})

	// 测试加载和解析CSV文件
	t.Run("LoadAndParseCSV", func(t *testing.T) {
		symbol := "BTCUSDT"
		date := "2024-01-01"
		filePath := downloader.GetFilePath(symbol, date)

		t.Logf("Loading and parsing %s...", filePath)
		records, err := LoadCompressedCSV(filePath)
		if err != nil {
			t.Errorf("Failed to load CSV: %v", err)
			return
		}

		t.Logf("Loaded %d trade records", len(records))

		// 显示前几条记录（聚合交易格式）
		if len(records) > 0 {
			t.Logf("First 5 records:")
			for i := 0; i < 5 && i < len(records); i++ {
				if len(records[i]) >= 7 {
					// 聚合交易格式: [agg_trade_id, price, quantity, first_trade_id, last_trade_id, timestamp, is_buyer_maker]
					t.Logf("  [%d] AggID=%s, Price=%s, Qty=%s, FirstID=%s, LastID=%s, Time=%s, IsBuyerMaker=%s",
						i, records[i][0], records[i][1], records[i][2],
						records[i][3], records[i][4], records[i][5], records[i][6])
				}
			}
		}

		// 验证记录格式（聚合交易数据有7个字段）
		if len(records) > 0 {
			for i, record := range records {
				if len(record) < 7 {
					t.Errorf("Record %d has wrong number of fields: expected at least 7, got %d",
						i, len(record))
					if i > 10 {
						break // 只检查前几条
					}
				}
			}
		}
	})

	// 测试无效日期
	t.Run("InvalidDate", func(t *testing.T) {
		symbol := "BTCUSDT"
		date := "2099-12-31" // 未来日期，应该没有数据

		t.Logf("Attempting to download future date %s (should fail)...", date)
		err := downloader.DownloadTradeData(symbol, date)
		if err == nil {
			t.Logf("Warning: Successfully downloaded future date (unexpected)")
		} else {
			t.Logf("Expected failure for future date: %v", err)
		}
	})
}

// 独立测试函数，可以单独运行
func TestDownloadSingleDay(t *testing.T) {
	tempDir := "/tmp/banbot_single_test"
	os.MkdirAll(tempDir, 0755)
	defer os.RemoveAll(tempDir)

	downloader := NewBinanceDataDownloader(tempDir, "spot")

	// 下载最近的数据
	yesterday := time.Now().AddDate(0, 0, -2).Format("2006-01-02")
	symbol := "BTCUSDT"

	t.Logf("Downloading %s for %s", symbol, yesterday)
	err := downloader.DownloadTradeData(symbol, yesterday)
	if err != nil {
		t.Errorf("Download failed: %v", err)
	} else {
		filePath := downloader.GetFilePath(symbol, yesterday)
		if fileInfo, err := os.Stat(filePath); err == nil {
			t.Logf("Success! File size: %.2f MB", float64(fileInfo.Size())/(1024*1024))

			// 尝试加载文件
			records, err := LoadCompressedCSV(filePath)
			if err == nil {
				t.Logf("Loaded %d trades", len(records))
			}
		}
	}
}

// 测试合约市场下载
func TestDownloadFuturesData(t *testing.T) {
	tempDir := "/tmp/banbot_futures_test"
	os.MkdirAll(tempDir, 0755)
	defer os.RemoveAll(tempDir)

	// 创建合约市场下载器
	downloader := NewBinanceDataDownloader(tempDir, "futures")

	t.Run("DownloadFuturesSingleFile", func(t *testing.T) {
		symbol := "BTC/USDT:USDT"
		date := "2024-01-01"

		t.Logf("Downloading futures trade data for %s on %s...", symbol, date)
		err := downloader.DownloadTradeData(symbol, date)
		if err != nil {
			t.Errorf("Failed to download futures trade data: %v", err)
			return
		}

		// 检查文件是否存在
		expectedFile := downloader.GetFilePath(symbol, date)
		if _, err := os.Stat(expectedFile); os.IsNotExist(err) {
			t.Errorf("Expected file not found: %s", expectedFile)
		} else {
			t.Logf("Successfully downloaded: %s", expectedFile)

			// 获取文件大小
			fileInfo, _ := os.Stat(expectedFile)
			t.Logf("File size: %.2f MB", float64(fileInfo.Size())/(1024*1024))

			// 尝试加载文件
			records, err := LoadCompressedCSV(expectedFile)
			if err != nil {
				t.Errorf("Failed to load futures CSV: %v", err)
			} else {
				t.Logf("Loaded %d aggregated trades", len(records))
				// 显示第一条记录
				if len(records) > 0 && len(records[0]) >= 7 {
					t.Logf("First record: ID=%s, Price=%s, Qty=%s, Time=%s",
						records[0][0], records[0][1], records[0][2], records[0][5])
				}
			}
		}
	})
}

// 性能测试
func BenchmarkLoadCompressedCSV(b *testing.B) {
	// 先下载一个测试文件
	tempDir := "/tmp/banbot_bench_test"
	os.MkdirAll(tempDir, 0755)
	defer os.RemoveAll(tempDir)

	downloader := NewBinanceDataDownloader(tempDir, "spot")
	symbol := "BTCUSDT"
	date := "2024-01-01"

	err := downloader.DownloadTradeData(symbol, date)
	if err != nil {
		b.Fatalf("Failed to download test data: %v", err)
	}

	filePath := downloader.GetFilePath(symbol, date)

	// 测试加载性能
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := LoadCompressedCSV(filePath)
		if err != nil {
			b.Fatalf("Failed to load CSV: %v", err)
		}
	}
}
