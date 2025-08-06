# 逐笔交易数据功能测试指南

本文档说明如何分步测试逐笔交易数据功能。

## 测试准备

确保您的系统满足以下要求：
- Go 1.20+ 已安装
- 网络连接正常（需要访问 https://data.binance.vision）
- 至少 1GB 可用磁盘空间

## 分步测试

### 步骤 1：测试下载功能

我们提供了三种测试下载功能的方式：

#### 方式 A：使用独立测试程序（推荐）

```bash
# 运行预设的测试脚本
./run_download_test.sh

# 或者手动运行测试程序
go build -o test_download cmd/test_download/main.go
./test_download -symbol BTCUSDT -date 2024-01-01 -days 1
```

参数说明：
- `-symbol`: 交易对符号（默认 BTCUSDT）
- `-date`: 起始日期 YYYY-MM-DD（默认昨天）
- `-days`: 下载天数（默认 1）
- `-dir`: 数据存储目录（默认 /tmp/banbot_trade_data）

#### 方式 B：运行 Go 测试

```bash
# 运行完整测试套件
go test -v ./data -run TestBinanceDataDownloader

# 只测试单日下载
go test -v ./data -run TestDownloadSingleDay

# 运行性能测试
go test -bench=BenchmarkLoadCompressedCSV ./data
```

#### 方式 C：使用测试脚本

```bash
./test_download.sh
```

### 步骤 2：验证下载的数据

下载完成后，检查数据文件：

```bash
# 查看下载的文件
ls -la /tmp/banbot_trade_data/trades/BTCUSDT/

# 查看文件大小
du -h /tmp/banbot_trade_data/trades/BTCUSDT/*.gz

# 解压查看内容（前10行）
zcat /tmp/banbot_trade_data/trades/BTCUSDT/BTCUSDT-trades-2024-01-01.csv.gz | head -10
```

### 步骤 3：测试 TradeFeeder

创建一个简单的测试脚本：

```go
// test_feeder.go
package main

import (
    "fmt"
    "github.com/banbox/banbot/data"
)

func main() {
    feeder := data.NewTradeFeeder("BTCUSDT", "/tmp/banbot_trade_data", 
        1704067200000, 1704153600000, "1m")
    
    err := feeder.Start()
    if err != nil {
        fmt.Printf("启动失败: %v\n", err)
        return
    }
    
    fmt.Println("TradeFeeder 启动成功!")
    
    // 测试获取下一个时间戳
    nextMS := feeder.getNextMS()
    fmt.Printf("下一个时间戳: %d\n", nextMS)
    
    feeder.Stop()
}
```

### 步骤 4：测试完整回测

使用提供的测试配置运行回测：

```bash
# 构建主程序
go build -o banbot main.go

# 运行回测测试
./banbot backtest -config test_trade_config.yml -v
```

## 常见问题

### 1. 下载失败

**问题**：显示 "download failed: 404" 或网络错误

**解决方案**：
- 检查网络连接
- 确认日期不是未来日期
- 某些历史日期可能没有数据（如交易所维护日）

### 2. 文件验证失败

**问题**：下载成功但文件验证失败

**解决方案**：
- 检查磁盘空间
- 删除损坏的文件重新下载
- 查看错误日志了解具体原因

### 3. 内存不足

**问题**：处理大量数据时内存不足

**解决方案**：
- 减少 `trade_cache_size` 配置值
- 分批处理数据
- 增加系统可用内存

## 测试输出示例

成功的测试输出应该类似：

```
===========================================
Binance 逐笔交易数据下载测试
===========================================
交易对: BTCUSDT
起始日期: 2024-01-01
下载天数: 1
存储目录: /tmp/banbot_trade_data
-------------------------------------------

正在下载 2024-01-01 的数据...
  ✅ 下载成功!
     文件: /tmp/banbot_trade_data/trades/BTCUSDT/BTCUSDT-trades-2024-01-01.csv.gz
     大小: 187.43 MB
     耗时: 15.2s
     正在验证文件...
     ✅ 文件有效，包含 5234567 条交易记录
     时间范围: 2024-01-01 00:00:00 - 2024-01-01 23:59:59

===========================================
下载完成!
-------------------------------------------
成功: 1 个文件
失败: 0 个文件
总大小: 187.43 MB
数据目录: /tmp/banbot_trade_data
===========================================
```

## 性能指标

预期性能指标：
- 下载速度：10-50 MB/s（取决于网络）
- 文件大小：每天 100-300 MB（压缩后）
- 加载速度：< 1 秒加载一天数据到内存
- 内存占用：每天数据约 200 MB（解压后）

## 下一步

测试成功后，您可以：
1. 修改 `test_trade_strategy.go` 实现自己的策略
2. 调整配置参数优化性能
3. 扩展支持更多交易对
4. 集成到生产环境

## 故障排查

如果遇到问题，请检查：
1. 查看日志文件：`out.log`
2. 检查网络连接：`ping data.binance.vision`
3. 验证磁盘空间：`df -h`
4. 查看系统资源：`top` 或 `htop`

## 联系支持

如有问题，请提供：
- 错误日志
- 使用的命令
- 系统环境信息
- 网络连接状态