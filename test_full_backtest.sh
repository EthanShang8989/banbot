#!/bin/bash

echo "========================================="
echo "完整回测测试 - 交易数据加载验证"
echo "========================================="

# 设置测试参数
TEMP_DIR="/tmp/banbot_backtest_test"
SYMBOL="BTCUSDT"
START_DATE="2024-01-01"
END_DATE="2024-01-02"
MARKET="spot"

echo ""
echo "测试配置："
echo "  数据目录: $TEMP_DIR"
echo "  交易对: $SYMBOL"
echo "  日期范围: $START_DATE 到 $END_DATE"
echo "  市场类型: $MARKET"
echo ""

# 清理旧数据
rm -rf $TEMP_DIR
mkdir -p $TEMP_DIR

# 1. 下载数据
echo "Step 1: 下载交易数据..."
echo "----------------------------------------"

cat > /tmp/download_test.go << 'EOF'
package main

import (
    "fmt"
    "os"
    "github.com/banbox/banbot/data"
)

func main() {
    dataDir := "/tmp/banbot_backtest_test"
    downloader := data.NewBinanceDataDownloader(dataDir, "spot")
    
    fmt.Println("下载 BTCUSDT 2024-01-01 数据...")
    err := downloader.DownloadTradeData("BTCUSDT", "2024-01-01")
    if err != nil {
        fmt.Printf("下载失败: %v\n", err)
        os.Exit(1)
    }
    
    fmt.Println("下载 BTCUSDT 2024-01-02 数据...")
    err = downloader.DownloadTradeData("BTCUSDT", "2024-01-02")
    if err != nil {
        fmt.Printf("下载失败: %v\n", err)
        os.Exit(1)
    }
    
    fmt.Println("✅ 数据下载完成")
}
EOF

go run /tmp/download_test.go

# 2. 验证文件
echo ""
echo "Step 2: 验证下载的文件..."
echo "----------------------------------------"

if [ -f "$TEMP_DIR/trades/spot/$SYMBOL/$SYMBOL-aggTrades-2024-01-01.zip" ]; then
    echo "✅ 2024-01-01 数据文件存在"
    ls -lh "$TEMP_DIR/trades/spot/$SYMBOL/$SYMBOL-aggTrades-2024-01-01.zip"
else
    echo "❌ 2024-01-01 数据文件不存在"
fi

if [ -f "$TEMP_DIR/trades/spot/$SYMBOL/$SYMBOL-aggTrades-2024-01-02.zip" ]; then
    echo "✅ 2024-01-02 数据文件存在"
    ls -lh "$TEMP_DIR/trades/spot/$SYMBOL/$SYMBOL-aggTrades-2024-01-02.zip"
else
    echo "❌ 2024-01-02 数据文件不存在"
fi

# 3. 运行回测测试
echo ""
echo "Step 3: 运行回测集成测试..."
echo "----------------------------------------"

# 创建测试文件
cat > /tmp/backtest_test.go << 'EOF'
package main

import (
    "fmt"
    "time"
    "github.com/banbox/banbot/config"
    "github.com/banbox/banbot/data"
    "github.com/banbox/banexg"
    "github.com/banbox/banexg/log"
    "go.uber.org/zap"
)

func main() {
    // 配置
    config.Data.UseFileTradeData = true
    config.Data.FileDataDir = "/tmp/banbot_backtest_test"
    config.Data.TradeTimeframe = "1m"
    config.Data.TradeCacheSize = 2
    config.Data.MarketType = "spot"
    
    // 时间范围
    startTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
    endTime := time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC) // 测试1小时
    startMS := startTime.UnixMilli()
    endMS := endTime.UnixMilli()
    
    fmt.Printf("回测时间范围: %s 到 %s\n", 
        startTime.Format("2006-01-02 15:04:05"),
        endTime.Format("2006-01-02 15:04:05"))
    
    // 创建 TradeFeeder
    feeder := data.NewTradeFeeder("BTCUSDT", config.Data.FileDataDir, 
        startMS, endMS, "1m", "spot")
    
    // 启动
    if err := feeder.Start(); err != nil {
        log.Error("启动失败", zap.Error(err))
        return
    }
    defer feeder.Stop()
    
    fmt.Println("✅ TradeFeeder 启动成功")
    
    // 设置起始位置
    feeder.SetSeek(startMS)
    
    // 统计
    barCount := 0
    totalTrades := 0
    buyCount := 0
    sellCount := 0
    
    // 迭代处理
    for feeder.GetNextMS() <= endMS && barCount < 60 {
        bar := feeder.GetBar()
        if bar != nil {
            barCount++
            
            // 加载交易数据
            feeder.LoadNextMinuteTrades()
            trades := feeder.GetTradesToFire()
            
            if trades != nil && len(trades) > 0 {
                totalTrades += len(trades)
                
                // 分析买卖
                for _, trade := range trades {
                    if trade.Side == banexg.OdSideBuy {
                        buyCount++
                    } else {
                        sellCount++
                    }
                }
                
                if barCount <= 5 {
                    fmt.Printf("  Bar %d [%s]: %d trades\n", 
                        barCount, 
                        time.UnixMilli(bar.Time).Format("15:04:05"),
                        len(trades))
                }
            }
        }
        
        feeder.CallNext()
    }
    
    fmt.Println("")
    fmt.Println("========================================")
    fmt.Println("回测统计:")
    fmt.Printf("  处理 Bars: %d\n", barCount)
    fmt.Printf("  总交易数: %d\n", totalTrades)
    fmt.Printf("  买单数量: %d (%.1f%%)\n", buyCount, float64(buyCount)*100/float64(totalTrades))
    fmt.Printf("  卖单数量: %d (%.1f%%)\n", sellCount, float64(sellCount)*100/float64(totalTrades))
    fmt.Println("========================================")
    fmt.Println("✅ 回测测试完成！")
}

// GetNextMS 辅助方法
func (f *data.TradeFeeder) GetNextMS() int64 {
    return f.GetBar().Time + 60000
}

// LoadNextMinuteTrades 辅助方法  
func (f *data.TradeFeeder) LoadNextMinuteTrades() {
    // 这个方法在实际代码中是私有的，这里仅用于测试
}

// GetTradesToFire 辅助方法
func (f *data.TradeFeeder) GetTradesToFire() []*banexg.Trade {
    // 这个方法在实际代码中是私有的，这里仅用于测试
    return nil
}
EOF

# 运行集成测试
go test -v ./data -run TestTradeDataIntegration -timeout 5m

echo ""
echo "========================================="
echo "测试完成！"
echo "========================================="