package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/banbox/banbot/data"
)

func main() {
	// 命令行参数
	var (
		symbol  = flag.String("symbol", "BTCUSDT", "交易对符号")
		date    = flag.String("date", "", "日期 (YYYY-MM-DD)，默认为昨天")
		dataDir = flag.String("dir", "/tmp/banbot_trade_data", "数据存储目录")
		days    = flag.Int("days", 1, "下载天数")
		market  = flag.String("market", "spot", "市场类型: spot(现货) 或 futures(合约)")
	)
	flag.Parse()

	// 如果没有指定日期，使用昨天
	if *date == "" {
		*date = time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	}

	fmt.Println("===========================================")
	fmt.Println("Binance 聚合逐笔交易数据下载测试")
	fmt.Println("===========================================")
	fmt.Printf("市场类型: %s\n", *market)
	fmt.Printf("交易对: %s\n", *symbol)
	fmt.Printf("起始日期: %s\n", *date)
	fmt.Printf("下载天数: %d\n", *days)
	fmt.Printf("存储目录: %s\n", *dataDir)
	fmt.Println("-------------------------------------------")

	// 创建下载器
	downloader := data.NewBinanceDataDownloader(*dataDir, *market)

	// 解析起始日期
	startDate, err := time.Parse("2006-01-02", *date)
	if err != nil {
		log.Fatalf("日期格式错误: %v", err)
	}

	// 下载数据
	totalSize := int64(0)
	successCount := 0
	failCount := 0

	for i := 0; i < *days; i++ {
		currentDate := startDate.AddDate(0, 0, i)
		dateStr := currentDate.Format("2006-01-02")
		
		fmt.Printf("\n正在下载 %s 的数据...\n", dateStr)
		
		startTime := time.Now()
		err := downloader.DownloadTradeData(*symbol, dateStr)
		elapsed := time.Since(startTime)
		
		if err != nil {
			fmt.Printf("  ❌ 下载失败: %v\n", err)
			failCount++
			continue
		}
		
		// 检查文件
		filePath := downloader.GetFilePath(*symbol, dateStr)
		if fileInfo, err := os.Stat(filePath); err == nil {
			fileSize := fileInfo.Size()
			totalSize += fileSize
			successCount++
			
			fmt.Printf("  ✅ 下载成功!\n")
			fmt.Printf("     文件: %s\n", filePath)
			fmt.Printf("     大小: %.2f MB\n", float64(fileSize)/(1024*1024))
			fmt.Printf("     耗时: %v\n", elapsed)
			
			// 尝试加载文件验证
			fmt.Printf("     正在验证文件...\n")
			records, err := data.LoadCompressedCSV(filePath)
			if err != nil {
				fmt.Printf("     ⚠️  文件验证失败: %v\n", err)
			} else {
				fmt.Printf("     ✅ 文件有效，包含 %d 条交易记录\n", len(records))
				
				// 显示第一条和最后一条记录的时间
				if len(records) > 0 {
					firstTime := records[0][4]
					lastTime := records[len(records)-1][4]
					fmt.Printf("     时间范围: %s - %s\n", 
						formatTimestamp(firstTime), formatTimestamp(lastTime))
				}
			}
		} else {
			fmt.Printf("  ⚠️  文件不存在: %s\n", filePath)
			failCount++
		}
	}

	// 汇总统计
	fmt.Println("\n===========================================")
	fmt.Println("下载完成!")
	fmt.Println("-------------------------------------------")
	fmt.Printf("成功: %d 个文件\n", successCount)
	fmt.Printf("失败: %d 个文件\n", failCount)
	fmt.Printf("总大小: %.2f MB\n", float64(totalSize)/(1024*1024))
	fmt.Printf("数据目录: %s\n", *dataDir)
	fmt.Println("===========================================")
}

func formatTimestamp(tsStr string) string {
	// 尝试解析时间戳
	if len(tsStr) >= 10 {
		// 假设是毫秒时间戳，转换为时间
		var ts int64
		fmt.Sscanf(tsStr, "%d", &ts)
		if ts > 0 {
			t := time.UnixMilli(ts)
			return t.Format("2006-01-02 15:04:05")
		}
	}
	return tsStr
}