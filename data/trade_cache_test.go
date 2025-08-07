package data

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/banbox/banexg"
)

// TestTradeCacheProcessing 测试基本缓存功能
func TestTradeCacheProcessing(t *testing.T) {
	// Create temporary directory for test
	tempDir := "/tmp/banbot_test_cache"
	defer os.RemoveAll(tempDir)

	cache := NewTradeCache(tempDir)

	// Test cache path generation for CSV
	cache.useBinary = false
	path := cache.GetCachePath("spot", "BTCUSDT", "2024-01-01", 10)
	expected := filepath.Join(tempDir, "trades", "spot", "BTCUSDT", "2024-01-01", "10.csv")
	if path != expected {
		t.Errorf("GetCachePath() CSV = %v, want %v", path, expected)
	}

	// Test cache path generation for binary
	cache.useBinary = true
	path = cache.GetCachePath("spot", "BTCUSDT", "2024-01-01", 10)
	expected = filepath.Join(tempDir, "trades_bin", "spot", "BTCUSDT", "2024-01-01", "10.bin")
	if path != expected {
		t.Errorf("GetCachePath() Binary = %v, want %v", path, expected)
	}

	// Test IsCached
	if cache.IsCached("spot", "BTCUSDT", "2024-01-01", 10) {
		t.Error("IsCached() should return false for non-existent file")
	}
}

// TestGlobalCacheManager 测试全局缓存管理器
func TestGlobalCacheManager(t *testing.T) {
	manager := GetGlobalCacheManager()

	// Get initial stats
	files1, size1, _ := manager.GetGlobalCacheStats()
	t.Logf("Initial cache stats: %d files, %d bytes", files1, size1)

	// Get summary
	summary := manager.GetCacheSummary()
	t.Logf("Cache summary: %s", summary)

	// Test clear old caches (should not fail even if no caches exist)
	err := manager.ClearOldCaches(7)
	if err != nil {
		t.Logf("ClearOldCaches error (expected): %v", err)
	}
}

// TestBinaryVsCSVPerformance 对比二进制和CSV格式的性能
func TestBinaryVsCSVPerformance(t *testing.T) {
	// 准备测试数据
	numTrades := 100000
	testTrades := make([]*banexg.Trade, numTrades)
	for i := 0; i < numTrades; i++ {
		testTrades[i] = &banexg.Trade{
			ID:        fmt.Sprintf("%d", 1000000+i),
			Price:     3456.78 + float64(i)*0.01,
			Amount:    0.123456 + float64(i)*0.0001,
			Timestamp: 1721433600000 + int64(i*1000),
			Side:      map[bool]string{true: "sell", false: "buy"}[i%2 == 0],
		}
	}

	market := "linear"
	symbol := "TEST/USDT"
	date := "2025-01-01"
	hour := 10

	// 测试二进制写入
	binaryCache := NewTradeCache("/tmp/bench_binary")
	binaryCache.useBinary = true
	defer os.RemoveAll("/tmp/bench_binary")

	start := time.Now()
	cachePath := binaryCache.GetCachePath(market, symbol, date, hour)
	err := binaryCache.writeBinaryTrades(testTrades, cachePath)
	if err != nil {
		t.Fatal(err)
	}
	binaryWriteTime := time.Since(start)

	// 测试二进制读取
	start = time.Now()
	loadedTrades, err := binaryCache.LoadHourlyTrades(market, symbol, date, hour)
	if err != nil {
		t.Fatal(err)
	}
	binaryReadTime := time.Since(start)

	// 获取文件大小
	binaryInfo, _ := os.Stat(cachePath)
	binarySize := binaryInfo.Size()

	// 测试CSV写入
	csvCache := NewTradeCache("/tmp/bench_csv")
	csvCache.useBinary = false
	defer os.RemoveAll("/tmp/bench_csv")

	// 创建CSV文件
	csvPath := csvCache.GetCachePath(market, symbol, date, hour)
	start = time.Now()
	err = csvCache.writeCSVTrades(testTrades, csvPath)
	if err != nil {
		t.Fatal(err)
	}
	csvWriteTime := time.Since(start)

	// 测试CSV读取
	start = time.Now()
	csvLoadedTrades, err := csvCache.LoadHourlyTrades(market, symbol, date, hour)
	if err != nil {
		t.Fatal(err)
	}
	csvReadTime := time.Since(start)

	// 获取CSV文件大小
	csvInfo, _ := os.Stat(csvPath)
	csvSize := csvInfo.Size()

	// 输出对比结果
	t.Logf("\n性能对比 (%d trades):", numTrades)
	t.Logf("┌─────────────┬──────────────┬──────────────┬─────────────┐")
	t.Logf("│   格式      │    写入时间   │    读取时间   │   文件大小   │")
	t.Logf("├─────────────┼──────────────┼──────────────┼─────────────┤")
	t.Logf("│  二进制     │  %10v  │  %10v  │  %7.2f MB │", binaryWriteTime, binaryReadTime, float64(binarySize)/(1024*1024))
	t.Logf("│  CSV        │  %10v  │  %10v  │  %7.2f MB │", csvWriteTime, csvReadTime, float64(csvSize)/(1024*1024))
	t.Logf("└─────────────┴──────────────┴──────────────┴─────────────┘")

	// 计算提升比例
	writeSpeedup := float64(csvWriteTime) / float64(binaryWriteTime)
	readSpeedup := float64(csvReadTime) / float64(binaryReadTime)
	sizeRatio := float64(csvSize) / float64(binarySize)

	t.Logf("\n提升比例:")
	t.Logf("  写入速度: %.2fx", writeSpeedup)
	t.Logf("  读取速度: %.2fx", readSpeedup)
	t.Logf("  文件大小: %.2fx (CSV是二进制的%.1f倍)", sizeRatio, sizeRatio)

	// 验证数据完整性
	if len(loadedTrades) != len(testTrades) {
		t.Errorf("二进制加载数量不匹配: got %d, want %d", len(loadedTrades), len(testTrades))
	}
	if len(csvLoadedTrades) != len(testTrades) {
		t.Errorf("CSV加载数量不匹配: got %d, want %d", len(csvLoadedTrades), len(testTrades))
	}
}

// TestRealZipToBinary 测试实际ZIP文件转换为二进制格式
func TestRealZipToBinary(t *testing.T) {
	zipPath := "/tmp/data_bb/binance/trades/futures/ETHUSDT/ETHUSDT-aggTrades-2025-07-20.zip"

	if _, err := os.Stat(zipPath); os.IsNotExist(err) {
		t.Skip("Test ZIP file not found: " + zipPath)
	}

	// 使用新的二进制缓存
	cache := NewTradeCache("/tmp/test_binary_real")
	cache.useBinary = true
	defer os.RemoveAll("/tmp/test_binary_real")

	start := time.Now()
	err := cache.ProcessAndCacheZipFile(zipPath, "linear", "ETH/USDT:USDT", "2025-07-20")
	if err != nil {
		t.Fatal(err)
	}
	processTime := time.Since(start)

	t.Logf("ZIP转二进制处理时间: %v", processTime)

	// 测试加载所有小时的数据
	totalTrades := 0
	start = time.Now()
	for hour := 0; hour < 24; hour++ {
		trades, err := cache.LoadHourlyTrades("linear", "ETH/USDT:USDT", "2025-07-20", hour)
		if err == nil {
			totalTrades += len(trades)
		}
	}
	loadTime := time.Since(start)

	t.Logf("加载24小时二进制数据:")
	t.Logf("  总耗时: %v", loadTime)
	t.Logf("  总交易数: %d", totalTrades)
	t.Logf("  平均每小时: %v", loadTime/24)
	if totalTrades > 0 {
		t.Logf("  平均每交易: %.2f ns", float64(loadTime.Nanoseconds())/float64(totalTrades))
	}

	// 获取缓存大小
	files, size, _ := cache.GetCacheStats()
	t.Logf("二进制缓存统计:")
	t.Logf("  文件数: %d", files)
	t.Logf("  总大小: %.2f MB", float64(size)/(1024*1024))
	t.Logf("  压缩率: %.1f%% (相对于CSV)", float64(size)/(170.8*1024*1024)*100)
}

// TestOptimizedProcessing 测试优化后的实际ZIP处理性能
func TestOptimizedProcessing(t *testing.T) {
	zipPath := "/tmp/data_bb/binance/trades/futures/ETHUSDT/ETHUSDT-aggTrades-2025-07-20.zip"

	if _, err := os.Stat(zipPath); os.IsNotExist(err) {
		t.Skip("Test ZIP file not found: " + zipPath)
	}

	// 清理旧缓存
	cache := NewTradeCache("/tmp/benchmark_optimized")
	defer os.RemoveAll("/tmp/benchmark_optimized")

	// 测试优化后的处理速度
	start := time.Now()
	err := cache.ProcessAndCacheZipFile(zipPath, "linear", "ETH/USDT:USDT", "2025-07-20")
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)

	t.Logf("优化后ZIP处理耗时: %v", elapsed)

	// 获取缓存统计
	files, size, _ := cache.GetCacheStats()
	t.Logf("生成文件数: %d, 总大小: %.2f MB", files, float64(size)/(1024*1024))
}

// TestRealWorldPerformance 测试实际场景性能（多个ZIP文件）
func TestRealWorldPerformance(t *testing.T) {
	// 测试3天的数据处理
	dates := []string{
		"2025-07-20",
		"2025-07-21",
		"2025-07-22",
	}

	cache := NewTradeCache("/tmp/benchmark_realworld")
	defer os.RemoveAll("/tmp/benchmark_realworld")

	totalStart := time.Now()
	successCount := 0

	for _, date := range dates {
		zipPath := "/tmp/data_bb/binance/trades/futures/ETHUSDT/ETHUSDT-aggTrades-" + date + ".zip"

		if _, err := os.Stat(zipPath); os.IsNotExist(err) {
			t.Logf("跳过不存在的文件: %s", zipPath)
			continue
		}

		start := time.Now()
		err := cache.ProcessAndCacheZipFile(zipPath, "linear", "ETH/USDT:USDT", date)
		if err != nil {
			t.Logf("处理失败 %s: %v", date, err)
			continue
		}
		elapsed := time.Since(start)
		t.Logf("  %s 处理耗时: %v", date, elapsed)
		successCount++
	}

	totalElapsed := time.Since(totalStart)

	if successCount > 0 {
		avgTime := totalElapsed / time.Duration(successCount)
		t.Logf("\n总结:")
		t.Logf("  成功处理: %d 天", successCount)
		t.Logf("  总耗时: %v", totalElapsed)
		t.Logf("  平均每天: %v", avgTime)
		t.Logf("  预估13天: %v", avgTime*13)

		// 获取缓存统计
		files, size, _ := cache.GetCacheStats()
		t.Logf("  缓存文件: %d 个", files)
		t.Logf("  缓存大小: %.2f GB", float64(size)/(1024*1024*1024))
	}
}

// BenchmarkOptimizedVsOriginal 对比优化前后的性能
func BenchmarkOptimizedVsOriginal(b *testing.B) {
	zipPath := "/tmp/data_bb/binance/trades/futures/ETHUSDT/ETHUSDT-aggTrades-2025-07-20.zip"

	if _, err := os.Stat(zipPath); os.IsNotExist(err) {
		b.Skip("Test ZIP file not found: " + zipPath)
	}

	b.Run("Optimized", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cache := NewTradeCache("/tmp/bench_opt")
			cache.ProcessAndCacheZipFile(zipPath, "linear", "ETH/USDT:USDT", "2025-07-20")
			os.RemoveAll("/tmp/bench_opt")
		}
	})
}

// TestLoadPerformance 测试加载性能
func TestLoadPerformance(t *testing.T) {
	// 先确保有缓存数据
	cache := NewTradeCache("/tmp/benchmark_load")
	defer os.RemoveAll("/tmp/benchmark_load")

	zipPath := "/tmp/data_bb/binance/trades/futures/ETHUSDT/ETHUSDT-aggTrades-2025-07-20.zip"
	if _, err := os.Stat(zipPath); os.IsNotExist(err) {
		t.Skip("Test ZIP file not found: " + zipPath)
	}

	// 先创建缓存
	err := cache.ProcessAndCacheZipFile(zipPath, "linear", "ETH/USDT:USDT", "2025-07-20")
	if err != nil {
		t.Fatal(err)
	}

	// 测试加载性能
	totalTrades := 0
	start := time.Now()

	for hour := 0; hour < 24; hour++ {
		trades, err := cache.LoadHourlyTrades("linear", "ETH/USDT:USDT", "2025-07-20", hour)
		if err == nil {
			totalTrades += len(trades)
		}
	}

	elapsed := time.Since(start)
	t.Logf("加载24小时数据耗时: %v", elapsed)
	t.Logf("总交易数: %d", totalTrades)
	t.Logf("平均每小时加载: %v", elapsed/24)
}

// TestCacheFormatSwitch 测试CSV和二进制格式切换
func TestCacheFormatSwitch(t *testing.T) {
	// 测试数据
	testTrades := []*banexg.Trade{
		{ID: "1", Price: 3456.78, Amount: 0.123, Timestamp: 1721433600000, Side: "buy"},
		{ID: "2", Price: 3456.79, Amount: 0.456, Timestamp: 1721433601000, Side: "sell"},
		{ID: "3", Price: 3456.80, Amount: 0.789, Timestamp: 1721433602000, Side: "buy"},
	}

	market := "linear"
	symbol := "TEST/USDT"
	date := "2025-01-01"
	hour := 10

	// 测试CSV格式
	csvCache := NewTradeCache("/tmp/test_csv_format")
	csvCache.useBinary = false
	defer os.RemoveAll("/tmp/test_csv_format")

	cachePath := csvCache.GetCachePath(market, symbol, date, hour)
	err := csvCache.writeCSVTrades(testTrades, cachePath)
	if err != nil {
		t.Fatal("CSV write failed:", err)
	}

	loadedCSV, err := csvCache.LoadHourlyTrades(market, symbol, date, hour)
	if err != nil {
		t.Fatal("CSV load failed:", err)
	}

	if len(loadedCSV) != len(testTrades) {
		t.Errorf("CSV trades count mismatch: got %d, want %d", len(loadedCSV), len(testTrades))
	}

	// 测试二进制格式
	binCache := NewTradeCache("/tmp/test_bin_format")
	binCache.useBinary = true
	defer os.RemoveAll("/tmp/test_bin_format")

	binPath := binCache.GetCachePath(market, symbol, date, hour)
	err = binCache.writeBinaryTrades(testTrades, binPath)
	if err != nil {
		t.Fatal("Binary write failed:", err)
	}

	loadedBin, err := binCache.LoadHourlyTrades(market, symbol, date, hour)
	if err != nil {
		t.Fatal("Binary load failed:", err)
	}

	if len(loadedBin) != len(testTrades) {
		t.Errorf("Binary trades count mismatch: got %d, want %d", len(loadedBin), len(testTrades))
	}

	// 验证数据一致性
	for i := range testTrades {
		if loadedCSV[i].ID != testTrades[i].ID || loadedBin[i].ID != testTrades[i].ID {
			t.Errorf("Trade ID mismatch at index %d", i)
		}
		if loadedCSV[i].Price != testTrades[i].Price || loadedBin[i].Price != testTrades[i].Price {
			t.Errorf("Trade Price mismatch at index %d", i)
		}
		if loadedCSV[i].Side != testTrades[i].Side || loadedBin[i].Side != testTrades[i].Side {
			t.Errorf("Trade Side mismatch at index %d", i)
		}
	}

	t.Log("✅ Format switch test passed")
}

// TestMeasureActualZipProcessing 实际测量ZIP处理各阶段耗时
func TestMeasureActualZipProcessing(t *testing.T) {
	zipPath := "/tmp/data_bb/binance/trades/futures/ETHUSDT/ETHUSDT-aggTrades-2025-07-20.zip"
	
	if _, err := os.Stat(zipPath); os.IsNotExist(err) {
		t.Skip("Test ZIP file not found: " + zipPath)
	}
	
	// 阶段1：打开ZIP文件
	start := time.Now()
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	t.Logf("打开ZIP文件耗时: %v", time.Since(start))
	
	// 找到CSV文件
	var csvFile *zip.File
	for _, f := range reader.File {
		if strings.HasSuffix(f.Name, ".csv") {
			csvFile = f
			break
		}
	}
	
	if csvFile == nil {
		t.Fatal("No CSV file found in zip")
	}
	
	// 阶段2：读取并解析CSV
	start = time.Now()
	rc, err := csvFile.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	
	scanner := bufio.NewScanner(rc)
	lineCount := 0
	hourMap := make(map[int]int)
	
	for scanner.Scan() {
		line := scanner.Text()
		lineCount++
		
		if line == "" {
			continue
		}
		
		fields := strings.Split(line, ",")
		if len(fields) >= 7 {
			timestamp, _ := strconv.ParseInt(fields[5], 10, 64)
			t := time.UnixMilli(timestamp)
			hour := t.Hour()
			hourMap[hour]++
		}
	}
	t.Logf("读取解析CSV耗时: %v, 总行数: %d", time.Since(start), lineCount)
	
	// 输出每小时的交易数量
	for hour := 0; hour < 24; hour++ {
		if count, ok := hourMap[hour]; ok {
			t.Logf("  %02d:00 - %d trades", hour, count)
		}
	}
	
	// 阶段3：测试文件写入速度
	tempDir := "/tmp/benchmark_test"
	os.MkdirAll(tempDir, 0755)
	defer os.RemoveAll(tempDir)
	
	start = time.Now()
	for hour := 0; hour < 24; hour++ {
		filePath := filepath.Join(tempDir, fmt.Sprintf("%02d.csv", hour))
		file, _ := os.Create(filePath)
		writer := bufio.NewWriter(file)
		
		// 写入模拟数据
		for i := 0; i < hourMap[hour]; i++ {
			writer.WriteString("test,line,data\n")
		}
		
		writer.Flush()
		file.Close()
	}
	t.Logf("写入24个文件耗时: %v", time.Since(start))
	
	// 获取文件大小信息
	var totalSize int64
	filepath.Walk(tempDir, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			totalSize += info.Size()
		}
		return nil
	})
	t.Logf("总文件大小: %.2f MB", float64(totalSize)/(1024*1024))
}

// TestCachePerformanceComparison 测试缓存性能对比
func TestCachePerformanceComparison(t *testing.T) {
	// Compare old vs new approach timing
	t.Run("NewCacheApproach", func(t *testing.T) {
		start := time.Now()

		// Simulate cache check and load
		cache := NewTradeCache("")
		for hour := 0; hour < 24; hour++ {
			// Check if cached (very fast)
			_ = cache.IsCached("spot", "ETHUSDT", "2025-08-01", hour)
		}

		elapsed := time.Since(start)
		t.Logf("New cache approach check time: %v", elapsed)
	})

	t.Run("OldDirectLoadApproach", func(t *testing.T) {
		start := time.Now()

		// Simulate loading compressed file (would be much slower with real data)
		// In reality, this would call LoadCompressedCSV which takes 8-9 seconds
		time.Sleep(10 * time.Millisecond) // Simulate some work

		elapsed := time.Since(start)
		t.Logf("Old direct load approach (simulated): %v", elapsed)
	})
}

// BenchmarkCacheProcessing 基准测试缓存处理
func BenchmarkCacheProcessing(b *testing.B) {
	// This benchmark tests the cache processing speed
	tempDir := "/tmp/banbot_bench_cache"
	defer os.RemoveAll(tempDir)

	cache := NewTradeCache(tempDir)

	// Create a dummy zip file path (would need actual file for real test)
	zipPath := "/tmp/test_data/BTCUSDT-aggTrades-2024-01-01.zip"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// In real scenario, this would process the zip file
		_ = cache.ProcessAndCacheZipFile(zipPath, "spot", "BTCUSDT", "2024-01-01")
	}
}

// BenchmarkZipProcessing 测试整个ZIP处理流程
func BenchmarkZipProcessing(b *testing.B) {
	zipPath := "/tmp/data_bb/binance/trades/futures/ETHUSDT/ETHUSDT-aggTrades-2025-07-20.zip"
	
	if _, err := os.Stat(zipPath); os.IsNotExist(err) {
		b.Skip("Test ZIP file not found: " + zipPath)
	}
	
	cache := NewTradeCache("/tmp/benchmark_cache")
	defer os.RemoveAll("/tmp/benchmark_cache")
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := cache.ProcessAndCacheZipFile(zipPath, "linear", "ETH/USDT:USDT", "2025-07-20")
		if err != nil {
			b.Fatal(err)
		}
		// 清理缓存以便下次测试
		os.RemoveAll("/tmp/benchmark_cache/trades")
		os.RemoveAll("/tmp/benchmark_cache/trades_bin")
	}
}

// BenchmarkStringVsBytes 比较字符串操作和字节操作
func BenchmarkStringVsBytes(b *testing.B) {
	testData := []byte("1234567890,3456.78,0.123456,1000,true,1721433600000,true")
	
	b.Run("StringSplit", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			line := string(testData)
			fields := strings.Split(line, ",")
			_ = fields[5]
		}
	})
	
	b.Run("BytesIndex", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			// 找到第5个逗号的位置
			count := 0
			idx := 0
			for j, b := range testData {
				if b == ',' {
					count++
					if count == 5 {
						idx = j + 1
						break
					}
				}
			}
			// 找到第6个逗号的位置
			endIdx := idx
			for j := idx; j < len(testData); j++ {
				if testData[j] == ',' {
					endIdx = j
					break
				}
			}
			_ = testData[idx:endIdx]
		}
	})
}

// BenchmarkRealZipFile 测试实际ZIP文件处理
func BenchmarkRealZipFile(b *testing.B) {
	zipPath := "/tmp/data_bb/binance/trades/futures/ETHUSDT/ETHUSDT-aggTrades-2025-07-20.zip"
	
	if _, err := os.Stat(zipPath); os.IsNotExist(err) {
		b.Skip("Test ZIP file not found: " + zipPath)
	}
	
	// 读取整个文件到内存
	reader, _ := zip.OpenReader(zipPath)
	defer reader.Close()
	
	var csvFile *zip.File
	for _, f := range reader.File {
		if strings.HasSuffix(f.Name, ".csv") {
			csvFile = f
			break
		}
	}
	
	rc, _ := csvFile.Open()
	data, _ := io.ReadAll(rc)
	rc.Close()
	
	b.ResetTimer()
	b.Run("ParseOnly", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			scanner := bufio.NewScanner(strings.NewReader(string(data)))
			for scanner.Scan() {
				line := scanner.Text()
				fields := strings.Split(line, ",")
				if len(fields) >= 7 {
					timestamp, _ := strconv.ParseInt(fields[5], 10, 64)
					t := time.UnixMilli(timestamp)
					_ = t.Hour()
				}
			}
		}
	})
}
