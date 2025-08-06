# 聚合逐笔交易数据支持更新

## 更新内容

### 1. 支持现货和U本位合约市场

- **现货市场 (spot)**
  - URL格式: `https://data.binance.vision/data/spot/daily/aggTrades/{SYMBOL}/{SYMBOL}-aggTrades-{DATE}.zip`
  - 示例: `https://data.binance.vision/data/spot/daily/aggTrades/BTCUSDT/BTCUSDT-aggTrades-2025-08-03.zip`

- **U本位合约 (futures/um)**
  - URL格式: `https://data.binance.vision/data/futures/um/daily/aggTrades/{SYMBOL}/{SYMBOL}-aggTrades-{DATE}.zip`
  - 示例: `https://data.binance.vision/data/futures/um/daily/aggTrades/BTCUSDT/BTCUSDT-aggTrades-2025-08-04.zip`

### 2. 数据格式

聚合交易数据 CSV 格式（7个字段）：
```
[agg_trade_id, price, quantity, first_trade_id, last_trade_id, timestamp, is_buyer_maker]
```

示例：
```
1234567,50000.00,0.001,7890123,7890125,1704067200000,true
```

字段说明：
- `agg_trade_id`: 聚合交易ID
- `price`: 成交价格
- `quantity`: 成交数量
- `first_trade_id`: 该聚合交易中第一笔交易ID
- `last_trade_id`: 该聚合交易中最后一笔交易ID
- `timestamp`: 时间戳（毫秒）
- `is_buyer_maker`: 买方是否为挂单方（true表示市价卖单）

### 3. 文件存储结构

```
{data_dir}/
└── trades/
    ├── spot/                   # 现货市场
    │   ├── BTCUSDT/
    │   │   ├── BTCUSDT-aggTrades-2025-08-03.zip
    │   │   └── ...
    │   └── ETHUSDT/
    │       └── ...
    └── futures/                # 合约市场
        ├── BTCUSDT/
        │   ├── BTCUSDT-aggTrades-2025-08-04.zip
        │   └── ...
        └── ...
```

### 4. 配置使用

在配置文件中指定市场类型：

```yaml
# 启用文件交易数据
use_file_trade_data: true
file_data_dir: "/data/binance"
trade_timeframe: "1m"

# 市场类型配置
market_type: spot      # 现货市场
# market_type: linear  # U本位合约市场
```

### 5. 命令行测试

使用测试工具下载数据：

```bash
# 下载现货数据
./test_dl -symbol BTCUSDT -date 2025-08-03 -market spot

# 下载合约数据
./test_dl -symbol BTCUSDT -date 2025-08-04 -market futures

# 查看帮助
./test_dl -h
```

参数说明：
- `-symbol`: 交易对符号（如 BTCUSDT）
- `-date`: 日期（YYYY-MM-DD格式）
- `-market`: 市场类型（spot 或 futures）
- `-days`: 下载天数（默认1）
- `-dir`: 数据存储目录

### 6. 快速测试

运行快速测试脚本：

```bash
# 测试聚合交易数据下载
./test_agg_trades.sh
```

这个脚本会：
1. 下载现货 BTCUSDT 2025-08-03 的数据
2. 下载合约 BTCUSDT 2025-08-04 的数据
3. 显示下载的文件信息

### 7. API 使用示例

```go
// 创建现货市场下载器
spotDownloader := data.NewBinanceDataDownloader("/data", "spot")
err := spotDownloader.DownloadTradeData("BTCUSDT", "2025-08-03")

// 创建合约市场下载器
futuresDownloader := data.NewBinanceDataDownloader("/data", "futures")
err := futuresDownloader.DownloadTradeData("BTCUSDT", "2025-08-04")

// 创建 TradeFeeder
feeder := data.NewTradeFeeder(
    "BTCUSDT",           // 交易对
    "/data",             // 数据目录
    startMS,             // 开始时间
    endMS,               // 结束时间
    "1m",                // 时间周期
    "spot",              // 市场类型: "spot" 或 "linear"
)
```

### 8. 主要代码更改

1. **BinanceDataDownloader**
   - 添加 `market` 字段支持市场类型
   - 更新 URL 生成逻辑支持不同市场
   - 文件格式从 `.csv.gz` 改为 `.zip`

2. **TradeFeeder**
   - 添加 `market` 参数
   - 更新数据解析支持7字段格式

3. **FileTradeProvider**
   - 添加市场类型支持
   - 目录结构区分现货和合约

4. **LoadCompressedCSV**
   - 支持 ZIP 文件格式
   - 保留 GZIP 支持以兼容旧格式

### 9. 注意事项

1. **日期有效性**：不能下载未来日期的数据
2. **网络要求**：需要能访问 `https://data.binance.vision`
3. **磁盘空间**：聚合数据文件较大，每天约 100-300MB
4. **市场区分**：现货和合约数据分开存储，避免混淆

### 10. 故障排查

如果下载失败：

1. 检查日期是否有效（不能是未来日期）
2. 检查网络连接：`curl -I https://data.binance.vision`
3. 验证 URL 格式：
   - 现货: `/data/spot/daily/aggTrades/`
   - 合约: `/data/futures/um/daily/aggTrades/`
4. 查看错误日志了解具体原因

### 11. 性能优化

- **压缩存储**：使用 ZIP 格式，节省 70-80% 磁盘空间
- **内存缓存**：预加载 2-3 天数据到内存
- **批量处理**：一次加载整天数据，按需筛选时间窗口
- **并发下载**：支持多个交易对并发下载

### 12. 后续计划

- [ ] 支持币本位合约 (COIN-M)
- [ ] 支持更细粒度的数据（如 5分钟文件）
- [ ] 添加数据完整性校验
- [ ] 实现增量更新机制
- [ ] 支持更多交易所的数据格式