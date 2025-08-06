# Banbot逐笔交易数据回测实现文档

## 1. 项目概述

### 1.1 目标
为 Banbot 回测系统添加逐笔交易数据支持，使策略能够在回测过程中接收 `OnWsTrades` 回调，处理来自 Binance Vision 的历史逐笔交易数据。

### 1.2 核心需求
- 支持从 https://data.binance.vision/ 自动下载逐笔交易数据
- 智能内存缓存，预加载2-3天数据到内存
- 文件压缩存储，运行时内存解压处理
- 与现有K线回测系统完美集成
- 保证事件的正确时间顺序

## 2. 架构设计

### 2.1 整体架构
```
FileTradeProvider (管理者)
├── TradeFeeder (工作者) - 处理单个交易对
│   ├── fileCache (内存缓存2-3天数据)
│   └── 滚动缓存机制
└── BinanceDataDownloader - 数据下载
```

### 2.2 时间同步机制
- 使用 `btime.CurTimeMS` 作为全局虚拟时间
- 通过 `RunHistFeeders` 进行事件排序
- 确保逐笔数据在K线数据之前触发

## 3. 核心组件详细设计

### 3.1 BinanceDataDownloader（数据下载器）

#### 结构体定义
```go
type BinanceDataDownloader struct {
    baseURL    string       // Binance Vision API基础URL
    dataDir    string       // 本地数据存储目录
    httpClient *http.Client // HTTP客户端
    mu         sync.Mutex   // 并发安全锁
}
```

#### 核心方法
```go
func NewBinanceDataDownloader(dataDir string) *BinanceDataDownloader
func (d *BinanceDataDownloader) DownloadTradeData(symbol, date string) error
```

#### 功能描述
- 从 Binance Vision 下载指定交易对和日期的逐笔交易数据
- 数据格式：`{SYMBOL}-trades-{YYYY-MM-DD}.csv.gz`
- 支持断点续传（检查文件是否已存在）
- 自动创建目录结构
- 原子性下载（临时文件 + 重命名）

#### 数据格式
Binance 逐笔数据 CSV 格式：
```
[id, price, qty, base_qty, time, is_buyer_maker]
```

### 3.2 内存缓存管理

#### 缓存策略
- **预加载机制**：启动时预加载2-3天的完整交易数据到内存
- **滚动缓存**：当需要新日期数据时，自动移除最旧缓存，加载新数据
- **内存占用**：单文件约200MB，缓存3天约600MB，现代机器完全可接受

#### 缓存结构
```go
type TradeCache struct {
    fileCache   map[string][]*banexg.Trade  // 日期 -> 交易数据映射
    cacheSize   int                         // 最大缓存天数 (默认3)
    maxMemMB    int                         // 最大内存占用限制
    mu          sync.RWMutex                // 并发安全锁
}
```

#### 核心方法
```go
func (c *TradeCache) PreloadData(dates []string) error
func (c *TradeCache) GetDayTrades(date string) []*banexg.Trade
func (c *TradeCache) EnsureDataAvailable(date string) error
func (c *TradeCache) EvictOldest() // 移除最旧缓存
func (c *TradeCache) GetMemoryUsage() int // 获取内存使用量
```

### 3.3 TradeFeeder（交易数据馈送器）

#### 结构体定义
```go
type TradeFeeder struct {
    // 基础信息
    symbol      string                    // 交易对符号
    dataDir     string                    // 数据目录
    downloader  *BinanceDataDownloader   // 下载器
    
    // 内存缓存系统
    fileCache   map[string][]*banexg.Trade // 缓存的交易数据 (日期->交易列表)
    cacheSize   int                       // 缓存天数 (默认3天)
    currentDate string                    // 当前处理的日期
    nextDates   []string                  // 预加载的日期列表
    
    // 时间管理
    startMS     int64                     // 回测开始时间
    endMS       int64                     // 回测结束时间
    nextBarMS   int64                     // 下一个K线结束时间
    timeFrameMS int64                     // 时间周期(毫秒)
    
    // 当前处理窗口
    tradesToFire []*banexg.Trade          // 当前时间窗口的交易数据
    tradeIndex   int                      // 当前交易索引
    
    // 并发安全
    mu          sync.RWMutex              // 读写锁
    stopped     bool                      // 停止标志
}
```

#### 接口实现（IHistKlineFeeder）
```go
// 时间管理接口
func (f *TradeFeeder) getNextMS() int64        // 获取下一个事件时间
func (f *TradeFeeder) SetSeek(since int64)     // 设置开始时间
func (f *TradeFeeder) SetEndMS(ms int64)       // 设置结束时间

// 数据处理接口
func (f *TradeFeeder) GetBar() *banexg.Kline   // 获取当前"虚拟K线"
func (f *TradeFeeder) RunBar(bar *banexg.Kline) *errs.Error // 执行数据处理
func (f *TradeFeeder) CallNext()               // 移动到下一个时间窗口

// 基础接口
func (f *TradeFeeder) getSymbol() string       // 获取交易对名称
```

#### 核心方法
```go
func NewTradeFeeder(symbol, dataDir string, startMS, endMS int64, timeframe string) *TradeFeeder
func (f *TradeFeeder) Start() error
func (f *TradeFeeder) Stop()

// 内存缓存相关
func (f *TradeFeeder) preloadFiles() error                    // 预加载文件到内存
func (f *TradeFeeder) loadFullDayTrades(date string) ([]*banexg.Trade, error) // 加载整天数据
func (f *TradeFeeder) ensureDataAvailable(date string)       // 确保数据在缓存中
func (f *TradeFeeder) evictOldestCache()                     // 移除最旧缓存

// 数据处理
func (f *TradeFeeder) loadNextMinuteTrades()                 // 从内存缓存快速加载分钟数据
func (f *TradeFeeder) parseTradeRecord(record []string) *banexg.Trade
func (f *TradeFeeder) fireTrades(trades []*banexg.Trade)     // 触发策略回调
```

#### 时间窗口处理逻辑
```
时间轴示例（1分钟K线）：
09:00:00 -------- 09:01:00 -------- 09:02:00
    ^                ^                ^
    |                |                |
trades窗口1       trades窗口2       trades窗口3

每个窗口处理：
1. 从内存缓存中快速筛选该分钟内的所有逐笔交易
2. 在 RunBar 中：
   - 设置 btime.CurTimeMS = 窗口开始时间
   - 触发 OnWsTrades 回调
   - 设置 btime.CurTimeMS = 窗口结束时间

内存缓存优势：
- 无磁盘I/O延迟，处理速度极快
- 预加载机制避免回测过程中的阻塞
- 滚动缓存保持稳定的内存占用
```

### 3.4 FileTradeProvider（文件交易数据提供者）

#### 结构体定义
```go
type FileTradeProvider struct {
    feeders   map[string]*TradeFeeder // 管理的 TradeFeeder 集合
    dataDir   string                  // 数据根目录
    startMS   int64                   // 全局开始时间
    endMS     int64                   // 全局结束时间
    timeframe string                  // 时间周期
    mu        sync.RWMutex            // 并发安全锁
}
```

#### 核心方法
```go
func NewFileTradeProvider(dataDir, timeframe string) *FileTradeProvider
func (p *FileTradeProvider) AddSymbol(symbol string) error
func (p *FileTradeProvider) RemoveSymbol(symbol string)
func (p *FileTradeProvider) GetFeeders() []IHistKlineFeeder
func (p *FileTradeProvider) Stop()
```

#### 功能描述
- 管理多个 TradeFeeder 的生命周期
- 提供统一的接口给回测框架
- 处理交易对的动态添加/删除
- 返回符合 IHistKlineFeeder 接口的 feeder 列表

## 4. 时间同步和事件排序

### 4.1 虚拟时间系统
```go
// btime 包中的全局虚拟时间
var CurTimeMS = int64(0)

func TimeMS() int64 {
    if core.BackTestMode {
        return CurTimeMS // 回测模式返回虚拟时间
    } else {
        return UTCStamp() // 实盘模式返回真实时间
    }
}
```

### 4.2 事件排序机制
`RunHistFeeders` 函数负责事件排序：

```go
func RunHistFeeders(makeFeeders func() []IHistKlineFeeder, versions chan int, pBar *utils.PrgBar) *errs.Error {
    for {
        // 1. 获取所有 feeder
        holds = makeFeeders()
        
        // 2. 按时间排序（关键！）
        holds = SortFeeders(holds, nil, false)
        
        // 3. 取时间最早的 feeder
        hold = holds[0]
        bar := hold.GetBar()
        
        // 4. 执行当前事件
        err := hold.RunBar(bar)
        
        // 5. 移动 feeder 到下一个位置
        hold.CallNext()
    }
}
```

排序规则：
1. 按 `getNextMS()` 返回的时间戳升序排列
2. 时间相同时按交易对名称排序
3. 确保相同时间内逐笔数据在K线数据之前

### 4.3 时间顺序保证
```
同一分钟内的执行顺序：
1. TradeFeeder(BTCUSDT).RunBar()
   - btime.CurTimeMS = 09:00:00 (分钟开始)
   - 发送 09:00:00-09:01:00 所有逐笔交易
   - btime.CurTimeMS = 09:01:00 (分钟结束)

2. KlineFeeder(BTCUSDT).RunBar()
   - btime.CurTimeMS = 09:01:00 (已正确)
   - 发送 09:00:00-09:01:00 的K线
   - 触发策略 OnBar 回调
```

## 5. 数据文件处理

### 5.1 目录结构
```
{dataDir}/
└── trades/
    ├── BTCUSDT/
    │   ├── BTCUSDT-trades-2024-01-01.csv.gz
    │   ├── BTCUSDT-trades-2024-01-02.csv.gz
    │   └── ...
    ├── ETHUSDT/
    │   ├── ETHUSDT-trades-2024-01-01.csv.gz
    │   └── ...
    └── ...
```

### 5.2 文件命名规范
- 格式：`{SYMBOL}-trades-{YYYY-MM-DD}.csv.gz`
- 示例：`BTCUSDT-trades-2024-01-01.csv.gz`

### 5.3 数据解析
```go
func (f *TradeFeeder) parseTradeRecord(record []string) *banexg.Trade {
    // Binance CSV格式：[id, price, qty, base_qty, time, is_buyer_maker]
    timeMs, _ := strconv.ParseInt(record[4], 10, 64)
    price, _ := strconv.ParseFloat(record[1], 64)
    qty, _ := strconv.ParseFloat(record[2], 64)
    isBuyerMaker, _ := strconv.ParseBool(record[5])
    
    return &banexg.Trade{
        Symbol: f.symbol,
        ID:     record[0],
        Price:  price,
        Amount: qty,
        Time:   timeMs,
        Side:   utils2.If(isBuyerMaker, banexg.OdSideSell, banexg.OdSideBuy),
    }
}
```

## 6. 集成方案

### 6.1 配置扩展
在 `config` 包中添加：

```go
type Config struct {
    // 现有配置...
    UseFileTradeData bool   `yaml:"use_file_trade_data"` // 启用文件交易数据
    FileDataDir     string  `yaml:"file_data_dir"`       // 数据目录
    TradeTimeframe  string  `yaml:"trade_timeframe"`     // 交易数据时间周期
    TradeCacheSize  int     `yaml:"trade_cache_size"`    // 内存缓存天数
    TradePreloadDays int    `yaml:"trade_preload_days"`  // 预加载天数
}
```

配置文件示例：
```yaml
use_file_trade_data: true
file_data_dir: "/data/binance"
trade_timeframe: "1m"
trade_cache_size: 3      # 缓存3天数据到内存
trade_preload_days: 1    # 提前预加载1天
time_range:
  start: "2024-01-01"
  end: "2024-01-02"
```

### 6.2 回测框架集成
修改 `opt/backtest.go`：

```go
type BackTest struct {
    *BackTestLite
    // 现有字段...
    tradeProvider *data.FileTradeProvider // 新增
}

func (b *BackTest) Init() *errs.Error {
    err := b.BackTestLite.Init()
    if err != nil {
        return err
    }
    
    // 初始化交易数据提供者
    if config.UseFileTradeData {
        b.tradeProvider = data.NewFileTradeProvider(
            config.FileDataDir, 
            config.TradeTimeframe,
        )
        
        // 添加需要的交易对
        for pair := range core.PairsMap {
            if err := b.tradeProvider.AddSymbol(pair); err != nil {
                log.Error("add trade symbol fail", zap.String("pair", pair), zap.Error(err))
            }
        }
    }
    
    return nil
}
```

### 6.3 数据提供者集成
修改 `data/provider.go` 中的 `RunHistFeeders` 调用：

```go
func (p *HistProvider) LoopMain() *errs.Error {
    makeFeeders := func() []IHistKlineFeeder {
        var feeders []IHistKlineFeeder
        
        // 添加K线 feeders
        for _, feeder := range p.holders {
            feeders = append(feeders, feeder)
        }
        
        // 添加交易数据 feeders（如果启用）
        if tradeProvider != nil {
            tradeProvider.GetFeeders()
            feeders = append(feeders, tradeFeeders...)
        }
        
        return feeders
    }
    
    return RunHistFeeders(makeFeeders, p.dirtyVers, pBar)
}
```

## 7. 策略使用示例

### 7.1 策略配置
```go
func NewMyStrategy() *strat.TradeStrat {
    return &strat.TradeStrat{
        Name:      "MyTradeStrategy",
        WsSubs:    map[string]string{
            core.WsSubTrade: "_cur_", // 订阅当前交易对的逐笔数据
        },
        OnWsTrades: onWsTrades,
        OnBar:      onBar,
    }
}
```

### 7.2 回调实现
```go
func onWsTrades(job *strat.StratJob, pair string, trades []*banexg.Trade) {
    for _, trade := range trades {
        log.Info("received trade", 
                 zap.String("symbol", trade.Symbol),
                 zap.String("id", trade.ID),
                 zap.Float64("price", trade.Price),
                 zap.Float64("amount", trade.Amount),
                 zap.String("side", trade.Side),
                 zap.Int64("time", trade.Time))
        
        // 处理逐笔交易逻辑
        // 例如：计算买卖压力、大单识别等
    }
}

func onBar(job *strat.StratJob) {
    // K线回调，在所有逐笔数据处理完后执行
    log.Info("bar completed", zap.Int64("time", btime.TimeMS()))
}
```

## 8. 性能优化

### 8.1 内存管理优化
- **预加载策略**：启动时一次性加载2-3天数据到内存
- **滚动缓存**：自动管理缓存，移除最旧数据，加载新数据
- **内存占用控制**：最大600MB内存占用，现代机器完全可接受
- **快速访问**：内存数组访问，避免磁盘I/O延迟

### 8.2 缓存优化
- **智能预加载**：提前加载即将需要的数据
- **批量加载**：按天为单位加载，减少文件操作次数
- **内存复用**：滚动使用缓存空间，避免内存碎片
- **并发安全**：使用读写锁保证多线程安全

### 8.3 I/O优化
- **压缩存储**：文件以gzip格式存储，节省70-80%磁盘空间
- **一次性解压**：将整个文件解压到内存，避免重复解压
- **顺序读取**：按时间顺序处理，提高缓存命中率
- **批量下载**：预下载未来几天的数据文件

### 8.4 内存缓存实现示例
```go
// 预加载文件到内存
func (f *TradeFeeder) preloadFiles() error {
    for i := 0; i < f.cacheSize; i++ {
        date := f.getDateByOffset(i)
        trades, err := f.loadFullDayTrades(date)
        if err != nil {
            continue // 文件不存在时跳过
        }
        f.fileCache[date] = trades
        log.Info("preloaded trade data", 
                 zap.String("symbol", f.symbol),
                 zap.String("date", date),
                 zap.Int("trades", len(trades)),
                 zap.Int("memoryMB", len(trades)*64/1024/1024)) // 估算内存占用
    }
    return nil
}

// 快速从内存获取分钟级数据
func (f *TradeFeeder) loadNextMinuteTrades() {
    startMS := f.nextBarMS - f.timeFrameMS
    endMS := f.nextBarMS
    
    dateStr := time.UnixMilli(startMS).Format("2006-01-02")
    f.ensureDataAvailable(dateStr)
    
    // 从内存缓存快速筛选
    dayTrades, exists := f.fileCache[dateStr]
    if !exists {
        f.tradesToFire = nil
        return
    }
    
    // 二分查找优化（可选）
    var trades []*banexg.Trade
    for _, trade := range dayTrades {
        if trade.Time >= startMS && trade.Time < endMS {
            trades = append(trades, trade)
        } else if trade.Time >= endMS {
            break // 已排序，可提前退出
        }
    }
    
    f.tradesToFire = trades
}

// 滚动缓存管理
func (f *TradeFeeder) ensureDataAvailable(targetDate string) {
    if _, exists := f.fileCache[targetDate]; exists {
        return
    }
    
    // 移除最旧缓存
    if len(f.fileCache) >= f.cacheSize {
        oldestDate := f.getOldestCachedDate()
        delete(f.fileCache, oldestDate)
        log.Debug("evicted old cache", zap.String("date", oldestDate))
    }
    
    // 加载新数据
    trades, err := f.loadFullDayTrades(targetDate)
    if err == nil {
        f.fileCache[targetDate] = trades
        log.Info("loaded new trade data", 
                 zap.String("date", targetDate),
                 zap.Int("trades", len(trades)))
    }
}
```

### 8.5 性能基准预期
- **启动时间**：首次加载3天数据约10-30秒
- **内存占用**：稳定在600MB左右
- **处理速度**：内存访问，每分钟数据处理 < 1ms
- **磁盘空间**：原文件大小的20-30%（压缩存储）

## 9. 错误处理

### 9.1 下载失败处理
- 重试机制：下载失败时自动重试
- 降级策略：文件不存在时跳过该时间段
- 错误日志：详细记录失败原因和恢复措施

### 9.2 数据格式错误
- 格式验证：解析前验证 CSV 格式
- 容错处理：跳过格式错误的行
- 统计报告：记录跳过的数据量

### 9.3 时间同步错误
- 时间校验：确保交易时间在合理范围内
- 乱序检测：检测并修正时间乱序问题
- 缺失检测：识别数据缺失的时间段

## 10. 测试验证

### 10.1 单元测试
- 数据下载器测试
- 文件解析测试
- 时间排序测试
- 批量处理测试

### 10.2 集成测试
- 完整回测流程测试
- 多交易对同时处理测试
- 大数据量性能测试
- 异常情况恢复测试

### 10.3 验证指标
- 事件顺序正确性
- 数据完整性
- 性能基准
- 内存使用量

## 11. 部署和维护

### 11.1 部署要求
- 磁盘空间：根据数据量预估存储需求
- 网络带宽：首次下载需要良好的网络环境
- 内存配置：建议至少 4GB 可用内存

### 11.2 监控指标
- 下载成功率
- 数据处理延迟
- 内存使用峰值
- 错误率统计

### 11.3 维护任务
- 定期清理过期数据
- 监控存储空间使用
- 更新数据源配置
- 性能调优

---

这个文档提供了完整的技术设计方案，可以直接用于指导具体的代码实现。每个组件的职责、接口、数据流程都已经详细描述，确保实现的一致性和完整性。