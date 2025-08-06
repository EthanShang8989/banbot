# Banbot 逐笔交易数据回测使用指南

## 功能概述

Banbot 现在支持在回测中使用 Binance Vision 的历史逐笔交易数据。这允许策略基于真实的买卖压力、大单识别等进行更精准的回测。

## 主要特性

- **自动下载**: 从 Binance Vision 自动下载历史逐笔交易数据
- **内存缓存**: 预加载 2-3 天数据到内存，提高处理速度
- **压缩存储**: 文件以 gzip 格式存储，节省 70-80% 磁盘空间
- **时间同步**: 确保逐笔数据在 K 线数据之前触发，保证事件顺序正确
- **无缝集成**: 与现有回测系统完美集成，策略通过 `OnWsTrades` 回调接收数据

## 配置方法

在配置文件中添加以下配置启用逐笔交易数据：

```yaml
# 启用文件逐笔交易数据
use_file_trade_data: true

# 数据存储目录（可选，默认使用 data_dir）
file_data_dir: "/data/binance"

# 交易数据时间周期（默认 1m）
trade_timeframe: "1m"

# 内存缓存天数（默认 3）
trade_cache_size: 3

# 预加载天数（默认 1）
trade_preload_days: 1
```

## 策略使用示例

```go
func MyStrategy() *TradeStrat {
    return &TradeStrat{
        Name: "MyTradeStrategy",
        WsSubs: map[string]string{
            core.WsSubTrade: "_cur_", // 订阅当前交易对的逐笔数据
        },
        OnWsTrades: onWsTrades,
        OnBar:      onBar,
    }
}

func onWsTrades(job *StratJob, pair string, trades []*banexg.Trade) {
    // 计算买卖压力
    var buyVolume, sellVolume float64
    
    for _, trade := range trades {
        if trade.Side == banexg.OdSideBuy {
            buyVolume += trade.Amount
        } else {
            sellVolume += trade.Amount
        }
    }
    
    // 基于买卖压力生成交易信号
    buyPressure := buyVolume / (buyVolume + sellVolume) * 100
    if buyPressure > 70 {
        // 生成入场信号
    }
}
```

## 数据格式

### 文件存储结构
```
{data_dir}/
└── trades/
    ├── BTCUSDT/
    │   ├── BTCUSDT-trades-2024-01-01.csv.gz
    │   ├── BTCUSDT-trades-2024-01-02.csv.gz
    │   └── ...
    └── ETHUSDT/
        ├── ETHUSDT-trades-2024-01-01.csv.gz
        └── ...
```

### CSV 数据格式
Binance 逐笔数据 CSV 格式：
```
[id, price, qty, base_qty, time, is_buyer_maker]
```

## 性能优化

- **预加载策略**: 启动时一次性加载 2-3 天数据到内存
- **滚动缓存**: 自动管理缓存，移除最旧数据，加载新数据
- **内存占用**: 单文件约 200MB，缓存 3 天约 600MB
- **处理速度**: 内存访问，每分钟数据处理 < 1ms

## 运行回测

```bash
# 使用配置文件运行回测
./banbot backtest -config config.yml

# 使用测试配置
./test_trade_backtest.sh
```

## 注意事项

1. **网络要求**: 首次运行需要从 Binance Vision 下载数据，请确保网络连接正常
2. **磁盘空间**: 根据回测时间范围预留足够的磁盘空间
3. **内存使用**: 建议至少 4GB 可用内存
4. **数据延迟**: 下载数据时可能需要等待，建议提前下载所需数据

## 故障排除

### 下载失败
- 检查网络连接
- 确认 Binance Vision 服务可访问
- 查看日志中的错误信息

### 内存不足
- 减少 `trade_cache_size` 配置
- 增加系统可用内存

### 数据缺失
- 某些日期可能没有交易数据（如维护期间）
- 系统会自动跳过缺失的数据继续运行

## 示例测试

项目包含了一个完整的测试示例：

- `test_trade_strategy.go`: 示例策略，展示如何使用逐笔交易数据
- `test_trade_config.yml`: 测试配置文件
- `test_trade_backtest.sh`: 测试运行脚本

运行测试：
```bash
./test_trade_backtest.sh
```

## 技术架构

### 核心组件

1. **BinanceDataDownloader**: 负责从 Binance Vision 下载数据
2. **TradeFeeder**: 处理单个交易对的逐笔数据馈送
3. **FileTradeProvider**: 管理多个 TradeFeeder 实例
4. **HistProvider**: 集成到现有回测框架

### 时间同步机制

系统使用 `btime.CurTimeMS` 作为全局虚拟时间，通过 `RunHistFeeders` 进行事件排序，确保：
1. 逐笔数据在 K 线数据之前触发
2. 相同时间的事件按交易对名称排序
3. 策略接收到的数据时间顺序与真实市场一致

## 未来改进

- [ ] 支持更多交易所的历史数据
- [ ] 添加数据质量检查和修复机制
- [ ] 支持实时模式下的逐笔数据
- [ ] 优化大数据量下的内存使用
- [ ] 添加数据预处理和特征提取工具