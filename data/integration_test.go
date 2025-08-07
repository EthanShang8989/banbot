package data

import (
	"os"
	"testing"
	"time"

	"github.com/banbox/banbot/config"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
)

// TestTradeDataIntegration 测试完整的交易数据流程
func TestTradeDataIntegration(t *testing.T) {
	// 设置测试环境
	tempDir := "/tmp/banbot_integration_test"
	os.RemoveAll(tempDir)
	os.MkdirAll(tempDir, 0755)
	defer os.RemoveAll(tempDir)

	// 配置
	config.Data.UseFileTradeData = true
	config.Data.FileDataDir = tempDir
	config.Data.TradeTimeframe = "1m"
	config.Data.TradeCacheSize = 2

	// 设置时间范围（使用较短的范围以加快测试）
	startTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endTime := time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC) // 只测试1小时
	startMS := startTime.UnixMilli()
	endMS := endTime.UnixMilli()

	t.Run("FullPipeline", func(t *testing.T) {
		// 1. 创建下载器并下载数据
		t.Log("Step 1: 下载数据...")
		downloader := NewBinanceDataDownloader(tempDir, "spot")
		err := downloader.DownloadTradeData("BTCUSDT", "2024-01-01")
		if err != nil {
			t.Fatalf("下载失败: %v", err)
		}
		t.Log("✅ 数据下载成功")

		// 2. 创建 TradeFeeder
		t.Log("Step 2: 创建 TradeFeeder...")
		feeder := NewTradeFeeder("BTCUSDT", tempDir, startMS, endMS, "1m", "spot")
		err = feeder.Start()
		if err != nil {
			t.Fatalf("TradeFeeder 启动失败: %v", err)
		}
		t.Log("✅ TradeFeeder 启动成功")

		// 3. 测试数据加载
		t.Log("Step 3: 测试数据加载...")
		feeder.SetSeek(startMS)

		// 获取第一个 batch
		batch := feeder.GetBatch()
		if batch == nil {
			t.Fatal("获取 batch 失败")
		}
		t.Logf("✅ 获取到 batch，时间: %d", batch.StartTime())

		// 4. 测试数据迭代
		t.Log("Step 4: 测试数据迭代...")
		tradeCount := 0
		barCount := 0

		for feeder.getNextMS() <= endMS && barCount < 60 { // 最多处理60个batch（60分钟）
			batch := feeder.GetBatch()
			if batch != nil {
				barCount++

				// 统计交易数量
				if tradeBatch, ok := batch.(*TradeBatch); ok && len(tradeBatch.Trades) > 0 {
					tradeCount += len(tradeBatch.Trades)
					if barCount <= 3 { // 显示前3个batch的信息
						t.Logf("  Batch %d: %d trades", barCount, len(tradeBatch.Trades))
					}
				}
			}

			feeder.CallNext()
		}

		t.Logf("✅ 处理了 %d 个 batches，包含 %d 笔交易", barCount, tradeCount)

		// 5. 清理
		feeder.Stop()
		t.Log("✅ 测试完成")
	})
}

// TestFileTradeProvider 测试 FileTradeProvider
func TestFileTradeProvider(t *testing.T) {
	tempDir := "/tmp/banbot_provider_test"
	os.RemoveAll(tempDir)
	os.MkdirAll(tempDir, 0755)
	defer os.RemoveAll(tempDir)

	// 下载测试数据
	downloader := NewBinanceDataDownloader(tempDir, "spot")
	symbols := []string{"BTCUSDT", "ETHUSDT"}

	t.Log("下载测试数据...")
	for _, symbol := range symbols {
		err := downloader.DownloadTradeData(symbol, "2024-01-01")
		if err != nil {
			t.Logf("警告: %s 下载失败: %v", symbol, err)
		}
	}

	// 创建 Provider
	provider := NewFileTradeProvider(tempDir, "1m", "spot")
	startMS := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	endMS := time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC).UnixMilli()
	provider.SetTimeRange(startMS, endMS)

	// 添加交易对
	for _, symbol := range symbols {
		err := provider.AddSymbol(symbol)
		if err != nil {
			t.Errorf("添加 %s 失败: %v", symbol, err)
		} else {
			t.Logf("✅ 添加 %s 成功", symbol)
		}
	}

	// 获取 feeders
	feeders := provider.GetFeeders()
	t.Logf("获取到 %d 个 feeders", len(feeders))

	if len(feeders) != len(symbols) {
		t.Errorf("Feeder 数量不匹配: 期望 %d, 实际 %d", len(symbols), len(feeders))
	}

	// 测试删除
	provider.RemoveSymbol("ETHUSDT")
	feeders = provider.GetFeeders()
	if len(feeders) != 1 {
		t.Errorf("删除后 Feeder 数量错误: 期望 1, 实际 %d", len(feeders))
	}

	provider.Stop()
	t.Log("✅ Provider 测试完成")
}

// TestTradeDataCallback 测试交易数据回调
func TestTradeDataCallback(t *testing.T) {
	tempDir := "/tmp/banbot_callback_test"
	os.RemoveAll(tempDir)
	os.MkdirAll(tempDir, 0755)
	defer os.RemoveAll(tempDir)

	// 下载测试数据
	downloader := NewBinanceDataDownloader(tempDir, "spot")
	err := downloader.DownloadTradeData("BTCUSDT", "2024-01-01")
	if err != nil {
		t.Skipf("跳过测试，下载失败: %v", err)
	}

	// 创建 feeder
	startMS := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	endMS := time.Date(2024, 1, 1, 0, 5, 0, 0, time.UTC).UnixMilli() // 5分钟

	feeder := NewTradeFeeder("BTCUSDT", tempDir, startMS, endMS, "1m", "spot")
	feeder.Start()
	feeder.SetSeek(startMS)

	// 记录回调数据
	var totalTrades int
	var callbackCount int

	// 迭代处理
	for feeder.getNextMS() <= endMS {
		batch := feeder.GetBatch()
		if batch != nil {
			if tradeBatch, ok := batch.(*TradeBatch); ok && len(tradeBatch.Trades) > 0 {
				callbackCount++
				totalTrades += len(tradeBatch.Trades)

				// 分析买卖压力
				var buyCount, sellCount int
				for _, trade := range tradeBatch.Trades {
					if trade.Side == banexg.OdSideBuy {
						buyCount++
					} else {
						sellCount++
					}
				}

				if callbackCount <= 5 {
					t.Logf("Batch %d: %d trades (Buy: %d, Sell: %d)",
						callbackCount, len(tradeBatch.Trades), buyCount, sellCount)
				}
			}
		}
		feeder.CallNext()
		
		if callbackCount >= 5 {
			break // 只处理5个batch用于测试
		}
	}

	t.Logf("✅ 处理完成: %d 个回调，共 %d 笔交易", callbackCount, totalTrades)

	if callbackCount == 0 {
		t.Error("没有触发任何回调")
	}

	feeder.Stop()
}

// Example function showing how to use the trade data
func ExampleTradeFeeder() {
	// 创建 TradeFeeder
	feeder := NewTradeFeeder(
		"BTCUSDT",                                // 交易对
		"/data/binance",                          // 数据目录
		time.Now().AddDate(0, 0, -1).UnixMilli(), // 开始时间
		time.Now().UnixMilli(),                   // 结束时间
		"1m",                                     // 时间周期
		"spot",                                   // 市场类型
	)

	// 启动 feeder
	if err := feeder.Start(); err != nil {
		log.Error("启动失败", zap.Error(err))
		return
	}
	defer feeder.Stop()

	// 设置起始位置
	feeder.SetSeek(feeder.startMS)

	// 迭代处理数据
	for feeder.getNextMS() <= feeder.endMS {
		batch := feeder.GetBatch()
		if batch != nil {
			// 运行 batch 处理（会触发策略回调）
			feeder.RunBatch(batch)
		}
		feeder.CallNext()
	}
}
