# Data Package

Data management package providing high-performance historical data download, caching, batch processing, and real-time data streaming.

## Table of Contents

- [Architecture Overview](#architecture-overview)
- [Core Components](#core-components)
- [Data Flow](#data-flow)
- [Usage Examples](#usage-examples)
- [Performance Metrics](#performance-metrics)
- [Configuration](#configuration)

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                        Data Package                         │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐ │
│  │   Spider     │    │  Downloader  │    │   Watcher    │ │
│  │  (Real-time) │    │  (Historical)│    │  (WebSocket) │ │
│  └──────┬───────┘    └──────┬───────┘    └──────┬───────┘ │
│         │                   │                    │         │
│         └───────────────────┼────────────────────┘         │
│                             ▼                              │
│                    ┌──────────────┐                        │
│                    │  TradeCache  │                        │
│                    │ (High-speed) │                        │
│                    └──────┬───────┘                        │
│                           │                                │
│         ┌─────────────────┼─────────────────┐              │
│         ▼                 ▼                 ▼              │
│  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐      │
│  │ TradeFeeder  │ │  HistFeeder  │ │    Batch     │      │
│  │  (Tick Data) │ │  (Kline Data)│ │  (Processor) │      │
│  └──────┬───────┘ └──────┬───────┘ └──────┬───────┘      │
│         │                 │                 │              │
│         └─────────────────┼─────────────────┘              │
│                           ▼                                │
│                    ┌──────────────┐                        │
│                    │   Provider   │                        │
│                    │  (Data API)  │                        │
│                    └──────────────┘                        │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

## Core Components

### 1. Data Acquisition Layer

#### `spider.go` - Historical Data Spider
- Batch download historical kline data from exchanges
- Support multi-symbol, multi-timeframe concurrent downloads
- Automatic resume and error retry
- Store data to PostgreSQL/TimescaleDB

#### `trade_downloader.go` - Trade Data Downloader
- Download Binance daily aggregated trades (aggTrades)
- Support spot, futures (USDT-M), and inverse (COIN-M) markets
- Auto-check local cache to avoid duplicate downloads
- ZIP file format containing all trades for the day

#### `watcher.go` - Real-time Data Listener
- WebSocket-based real-time data streaming
- Support OHLCV and Trade data
- Auto-reconnect and heartbeat maintenance
- Multiplexing - single connection for multiple subscriptions

### 2. Cache Management Layer

#### `trade_cache.go` - High-Performance Trade Cache
- **Three-tier cache architecture**:
  - L1: Memory sliding window (default 3 hours)
  - L2: Disk hourly cache (CSV/Binary format)
  - L3: Original ZIP compressed files
- **Smart preloading**: Auto-preload future data based on backtest progress
- **Dual format support**:
  - CSV format: Readable and debuggable
  - Binary format: 2.2x faster reading, 37% storage savings
- **Thread-safe**: Atomic operations in multi-threaded environment

#### `cache_manager.go` - Global Cache Manager
- Unified management of all cache instances
- Provide cache statistics and cleanup functions
- Periodic cleanup of expired cache files
- Cache usage monitoring

### 3. Data Processing Layer

#### `batch.go` - Batch Processor
- Aggregate continuous trade data into fixed time intervals
- Support KlineBatch and TradeBatch
- Auto-calculate VWAP, volume and other indicators
- Time alignment to ensure timestamp precision

#### `trade_feeder.go` - Tick Data Feeder
- Load historical trade data from cache
- Push data in chronological order to strategies
- Support callback functions for batch processing
- Sliding window preloading to reduce I/O latency

#### `hist_feeder.go` - Kline Data Feeder
- Load historical klines from database
- Support multiple timeframes (1m, 5m, 15m, 1h, 4h, 1d, etc.)
- Cache recently used data
- Batch loading for optimized query performance

### 4. Data Provider Layer

#### `provider.go` / `provider_v2.go` - Unified Data Interface
- Coordinate multiple data sources (klines, trades)
- Time synchronization and data alignment
- Support multi-symbol parallel processing
- Provide unified data access API

#### `trade_provider.go` - Trade Data Provider
- Manage multiple TradeFeeder instances
- Support dynamic add/remove data sources
- Batch fetch multi-symbol data
- Lifecycle management

### 5. Tools and Utilities

#### `feeder.go` - Base Data Feeder Interface
- Define standard interface for data feeding
- Provide base implementation and utility methods
- Event-driven data distribution

#### `common.go` - Common Functions and Constants
- Common data structure definitions
- Helper functions (time processing, format conversion)
- Error handling and logging

#### `tools.go` - Utility Toolkit
- Data format conversion
- Performance analysis tools
- Debug helper functions

## Data Flow

### 1. Historical Data Backtest Flow

```
1. Download Phase
   ├── TradeDownloader.DownloadTradeData()
   ├── Check if file exists locally
   ├── Download ZIP from Binance
   └── Save to /data_dir/trades/{market}/{symbol}/

2. Cache Building
   ├── TradeCache.ProcessAndCacheZipFile()
   ├── Extract CSV from ZIP
   ├── Split data by hour
   └── Save as binary format to /tmp/banbot_cache/

3. Data Loading
   ├── TradeFeeder.preloadHourWindow()
   ├── Load current time window to memory
   ├── Preload next N hours
   └── Maintain sliding window cache

4. Data Streaming
   ├── TradeFeeder.NextBatch()
   ├── Get next batch from memory cache
   ├── Aggregate to specified interval
   └── Push to strategy for processing
```

### 2. Real-time Trading Flow

```
1. Connection Setup
   ├── Watcher.Connect()
   ├── Establish WebSocket connection
   └── Subscribe to data streams

2. Data Reception
   ├── Watcher.OnMessage()
   ├── Parse real-time data
   └── Trigger callbacks

3. Data Processing
   ├── Batch.AddTrade()
   ├── Aggregate real-time trades
   └── Calculate indicators

4. Strategy Execution
   └── Push to strategy engine
```

## Usage Examples

### Download Historical Data

```go
// Create downloader
downloader := NewBinanceDataDownloader("/data/dir", "spot")

// Download single day
err := downloader.DownloadTradeData("BTCUSDT", "2024-01-01")

// Download date range
start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
end := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)
err = downloader.EnsureDataRange("BTCUSDT", start, end)
```

### Create Data Feeder

```go
// Create trade data feeder
feeder := NewTradeFeeder("BTCUSDT", "spot", "/data/dir",
    startTime, endTime, 5000) // 5-second batches

// Set data callback
feeder.OnBatchData = func(batch *TradeBatch) {
    fmt.Printf("Received %d trades\n", len(batch.Trades))
}

// Start feeding
feeder.Start()
```

### Use Cache System

```go
// Create cache manager
cache := NewTradeCache("/tmp/banbot_cache")

// Process ZIP file
err := cache.ProcessAndCacheZipFile(
    "/path/to/data.zip", "spot", "BTCUSDT", "2024-01-01")

// Load hourly data
trades, err := cache.LoadHourlyTrades(
    "spot", "BTCUSDT", "2024-01-01", 10) // 10 AM data
```

## Performance Metrics

Based on actual test data (ETH/USDT 2025-07-20):

| Metric | Value | Description |
|--------|-------|-------------|
| **ZIP Processing** | 2.6s/2.7M trades | Including extract, parse, split, store |
| **Cache Loading** | 460 bar/s | From cache to memory |
| **Binary vs CSV** | 2.2x faster | Binary format read speed |
| **Storage Compression** | 37% | Binary format vs CSV |
| **Memory Usage** | ~100MB/1M trades | Sliding window cache |
| **Concurrent Processing** | 10 symbols | Process multiple sources simultaneously |

## Configuration

### Cache Configuration

```yaml
# config.yml
data:
  cache_dir: /tmp/banbot_cache  # Cache directory
  trade_cache_hours: 3           # Sliding window size (hours)
  use_binary: true               # Use binary format
  cleanup_days: 7                # Clean cache older than N days
```

### Data Source Configuration

```yaml
# Download configuration
download:
  data_dir: /data/banbot         # Data storage directory
  markets:                       # Supported markets
    - spot                       # Spot
    - futures                    # USDT-M futures
    - inverse                    # COIN-M futures
  parallel: 5                    # Concurrent downloads
  retry: 3                       # Retry count
```

### Backtest Configuration

```yaml
# Backtest data configuration
backtest:
  batch_ms: 5000                 # Batch interval (milliseconds)
  preload_days: 1                # Preload days
  align_time: 100                # Time alignment precision (ms)
```

## Testing

Run tests:

```bash
# Run all tests
go test -v

# Run cache-related tests
go test -v -run TestCache

# Run performance tests
go test -bench=. -benchtime=10s

# Check test coverage
go test -cover
```

## Notes

1. **Disk Space**: Daily trade data ~100-200MB (compressed), cache uses additional space
2. **Memory Usage**: Sliding window keeps hours of data in memory
3. **Thread Safety**: All cache operations are thread-safe
4. **Data Integrity**: Automatic verification of downloaded data
5. **Network Retry**: Auto-retry on download failures

## Related Links

- [BanBot Main Project](https://github.com/banbox/banbot)
- [BanExg Exchange API](https://github.com/banbox/banexg)
- [Binance Data Documentation](https://github.com/binance/binance-public-data)

## License

This project is licensed under AGPLv3. See [LICENSE](../LICENSE) file for details.
EOF < /dev/null
