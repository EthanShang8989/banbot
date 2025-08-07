# 高频交易数据配置指南
High-Frequency Trading Data Configuration Guide

## 概述 Overview

BanBot支持毫秒级精度的高频交易数据处理。你可以配置10ms或更小的批处理间隔来处理逐笔交易数据。

## 配置选项 Configuration Options

### 1. trade_timeframe - K线时间框架
用于K线数据的时间框架，支持标准时间框架：
- `1m` - 1分钟
- `5m` - 5分钟
- `15m` - 15分钟
- `1h` - 1小时
- `4h` - 4小时
- `1d` - 1天

### 2. trade_precision_ms - 交易数据精度（新增）
用于逐笔交易数据的批处理间隔，支持毫秒级精度：
- `1` - 1毫秒（超高频）
- `10` - 10毫秒（高频，默认值）
- `100` - 100毫秒
- `1000` - 1秒
- `5000` - 5秒

## 配置示例 Configuration Example

```yaml
# 基本配置
data:
  # 启用文件交易数据
  use_file_trade_data: true
  
  # 数据目录
  file_data_dir: "/path/to/trade/data"
  
  # K线时间框架
  trade_timeframe: "1m"
  
  # 缓存窗口大小（小时）
  trade_cache_hours: 3
  
  # 交易数据精度（毫秒）- 高频数据的关键配置
  trade_precision_ms: 10  # 10毫秒批处理间隔
```

## 使用场景 Use Cases

### 场景1：超高频策略（1ms精度）
```yaml
data:
  trade_precision_ms: 1
  trade_cache_hours: 1  # 减少缓存以降低内存占用
```

### 场景2：高频策略（10ms精度，推荐）
```yaml
data:
  trade_precision_ms: 10  # 默认值，平衡性能和精度
  trade_cache_hours: 3
```

### 场景3：中频策略（100ms精度）
```yaml
data:
  trade_precision_ms: 100
  trade_cache_hours: 6
```

### 场景4：低频策略（1秒精度）
```yaml
data:
  trade_precision_ms: 1000
  trade_cache_hours: 12
```

## 性能影响 Performance Impact

| 精度 Precision | 内存占用 Memory | CPU使用 CPU | 适用场景 Use Case |
|---------------|----------------|-------------|-------------------|
| 1ms | 非常高 Very High | 非常高 Very High | 超高频套利 Ultra-HFT Arbitrage |
| 10ms | 高 High | 高 High | 高频策略 HFT Strategies |
| 100ms | 中等 Medium | 中等 Medium | 中频策略 Medium Frequency |
| 1000ms | 低 Low | 低 Low | 常规策略 Regular Strategies |

## 注意事项 Important Notes

1. **精度与性能权衡**：精度越高，内存和CPU占用越大
2. **数据量考虑**：高频数据会产生大量的批次，确保有足够的系统资源
3. **缓存配置**：高频策略建议减少`trade_cache_hours`以降低内存占用
4. **回测速度**：精度越高，回测速度可能越慢

## 代码实现 Code Implementation

配置被读取并应用在以下位置：

1. **config/types.go** - 配置定义
```go
type MainConfig struct {
    // ...
    TradePrecisionMS int64 `yaml:"trade_precision_ms,omitempty"`
}
```

2. **data/provider_v2.go** - 配置应用
```go
batchCfg := DefaultBatchConfig()
if config.Data.TradePrecisionMS > 0 {
    batchCfg.TradePrecisionMS = config.Data.TradePrecisionMS
}
```

3. **data/hist_feeder.go** - 使用精度
```go
adapter := NewTradeFeederAdapter(feeder, precisionMS)
// 批次按照precisionMS间隔创建
```

## 验证配置 Verify Configuration

运行以下命令验证配置：

```bash
# 运行精度测试
go test -v -run TestTradePrecision ./data

# 运行完整的缓存测试
./test.sh cache

# 使用配置运行回测
banbot backtest -config config_trade_10ms.yml
```

## 常见问题 FAQ

**Q: 为什么不能在trade_timeframe中设置10ms？**
A: `trade_timeframe`使用标准时间框架格式（如1m、5m），不支持毫秒级。毫秒级精度需要使用`trade_precision_ms`。

**Q: 10ms精度会影响K线数据吗？**
A: 不会。`trade_precision_ms`只影响逐笔交易数据的批处理，K线数据仍然使用`trade_timeframe`配置。

**Q: 如何知道当前使用的精度？**
A: 启动时查看日志，或在代码中打印`config.Data.TradePrecisionMS`的值。

**Q: 精度设置过高会有什么问题？**
A: 可能导致内存溢出、CPU占用过高、回测速度极慢。建议根据实际需求选择合适的精度。