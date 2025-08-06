package data

import (
	"strconv"
	"sync"
	"time"

	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/strat"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	utils2 "github.com/banbox/banexg/utils"
	"go.uber.org/zap"
)

// TradeFeeder feeds trade data from files for backtesting
type TradeFeeder struct {
	// Basic info
	symbol      string
	dataDir     string
	market      string // "spot" or "linear" for futures
	downloader  *BinanceDataDownloader
	
	// Memory cache system
	fileCache   map[string][]*banexg.Trade // Date -> trades mapping
	cacheSize   int                        // Cache days (default 3)
	currentDate string                     // Current processing date
	nextDates   []string                   // Preloaded date list
	
	// Time management
	startMS     int64  // Backtest start time
	endMS       int64  // Backtest end time
	nextBarMS   int64  // Next bar end time
	timeFrameMS int64  // Time frame in milliseconds
	
	// Current processing window
	tradesToFire []*banexg.Trade // Trades in current time window
	tradeIndex   int             // Current trade index
	
	// Concurrency safety
	mu      sync.RWMutex
	stopped bool
}

// NewTradeFeeder creates a new trade data feeder
func NewTradeFeeder(symbol, dataDir string, startMS, endMS int64, timeframe string, market string) *TradeFeeder {
	tfSecs := utils2.TFToSecs(timeframe)
	
	return &TradeFeeder{
		symbol:      symbol,
		dataDir:     dataDir,
		market:      market,
		downloader:  NewBinanceDataDownloader(dataDir, market),
		fileCache:   make(map[string][]*banexg.Trade),
		cacheSize:   3, // Default cache 3 days
		startMS:     startMS,
		endMS:       endMS,
		timeFrameMS: int64(tfSecs) * 1000,
		nextBarMS:   startMS + int64(tfSecs)*1000,
	}
}

// IHistKlineFeeder interface implementation

func (f *TradeFeeder) getSymbol() string {
	return f.symbol
}

func (f *TradeFeeder) getStates() []*PairTFCache {
	// Return a dummy state for compatibility
	return []*PairTFCache{{
		TimeFrame: "1m",
		TFSecs:    60,
		NextMS:    f.nextBarMS,
	}}
}

func (f *TradeFeeder) getWaitBar() *banexg.Kline {
	return nil
}

func (f *TradeFeeder) setWaitBar(bar *banexg.Kline) {
	// Not used for trade data
}

func (f *TradeFeeder) SubTfs(timeFrames []string, delOther bool) []string {
	// Trade feeder doesn't support multiple timeframes
	return nil
}

func (f *TradeFeeder) WarmTfs(curMS int64, tfNums map[string]int, pBar *utils.PrgBar) (int64, map[string][2]int, *errs.Error) {
	// Trade feeder doesn't need warming up in the traditional sense
	// It loads data on demand from files
	return curMS, make(map[string][2]int), nil
}

func (f *TradeFeeder) onNewBars(barTfMSecs int64, bars []*banexg.Kline) (bool, *errs.Error) {
	// Not used for trade data
	return false, nil
}

func (f *TradeFeeder) getNextMS() int64 {
	return f.nextBarMS
}

func (f *TradeFeeder) DownIfNeed(sess *orm.Queries, exchange banexg.BanExchange, pBar *utils.PrgBar) *errs.Error {
	// Download trade data for the entire range
	startTime := time.UnixMilli(f.startMS)
	endTime := time.UnixMilli(f.endMS)
	
	err := f.downloader.EnsureDataRange(f.symbol, startTime, endTime)
	if err != nil {
		return errs.New(errs.CodeRunTime, err)
	}
	
	// Preload initial data
	return f.preloadFiles()
}

func (f *TradeFeeder) SetSeek(since int64) {
	f.startMS = since
	f.nextBarMS = since + f.timeFrameMS
	f.loadNextMinuteTrades()
}

func (f *TradeFeeder) SetEndMS(ms int64) {
	f.endMS = ms
}

func (f *TradeFeeder) GetBar() *banexg.Kline {
	// Check if we've reached the end
	if f.nextBarMS > f.endMS + f.timeFrameMS {
		return nil
	}
	// Return a virtual bar for time synchronization
	return &banexg.Kline{
		Time: f.nextBarMS - f.timeFrameMS,
	}
}

func (f *TradeFeeder) RunBar(bar *banexg.Kline) *errs.Error {
	if len(f.tradesToFire) == 0 {
		return nil
	}
	
	// Set virtual time to bar start
	btime.CurTimeMS = bar.Time
	
	// Fire trades through strategy callbacks
	f.fireTrades(f.tradesToFire)
	
	// Set virtual time to bar end (end of the minute)
	btime.CurTimeMS = bar.Time + f.timeFrameMS
	
	return nil
}

func (f *TradeFeeder) CallNext() {
	f.nextBarMS += f.timeFrameMS
	if f.nextBarMS <= f.endMS {
		f.loadNextMinuteTrades()
	}
}

// Internal methods

func (f *TradeFeeder) preloadFiles() *errs.Error {
	startTime := time.UnixMilli(f.startMS)
	
	for i := 0; i < f.cacheSize; i++ {
		date := startTime.AddDate(0, 0, i).Format("2006-01-02")
		trades, err := f.loadFullDayTrades(date)
		if err != nil {
			log.Debug("skip loading trade data", 
				zap.String("symbol", f.symbol),
				zap.String("date", date),
				zap.Error(err))
			continue
		}
		
		f.mu.Lock()
		f.fileCache[date] = trades
		f.mu.Unlock()
		
		log.Info("preloaded trade data",
			zap.String("symbol", f.symbol),
			zap.String("date", date),
			zap.Int("trades", len(trades)))
	}
	
	return nil
}

func (f *TradeFeeder) loadFullDayTrades(date string) ([]*banexg.Trade, error) {
	filePath := f.downloader.GetFilePath(f.symbol, date)
	
	records, err := LoadCompressedCSV(filePath)
	if err != nil {
		return nil, err
	}
	
	trades := make([]*banexg.Trade, 0, len(records))
	for _, record := range records {
		trade := f.parseTradeRecord(record)
		if trade != nil {
			trades = append(trades, trade)
		}
	}
	
	return trades, nil
}

func (f *TradeFeeder) parseTradeRecord(record []string) *banexg.Trade {
	// Binance Aggregated Trade CSV format:
	// [agg_trade_id, price, quantity, first_trade_id, last_trade_id, timestamp, is_buyer_maker]
	// Example: 1234567,50000.00,0.001,7890123,7890125,1704067200000,true
	if len(record) < 7 {
		return nil
	}
	
	// Parse fields
	aggTradeID := record[0]
	price, _ := strconv.ParseFloat(record[1], 64)
	qty, _ := strconv.ParseFloat(record[2], 64)
	// firstTradeID := record[3] // Not used but available
	// lastTradeID := record[4]  // Not used but available
	timeMs, _ := strconv.ParseInt(record[5], 10, 64)
	isBuyerMaker, _ := strconv.ParseBool(record[6])
	
	// Determine trade side
	// is_buyer_maker = true means the buyer was the maker (passive order)
	// which means it was a market sell hitting a buy limit order
	side := banexg.OdSideBuy
	if isBuyerMaker {
		side = banexg.OdSideSell
	}
	
	return &banexg.Trade{
		Symbol:    f.symbol,
		ID:        aggTradeID,
		Price:     price,
		Amount:    qty,
		Timestamp: timeMs,
		Side:      side,
		Type:      "aggTrade", // Mark as aggregated trade
	}
}

func (f *TradeFeeder) loadNextMinuteTrades() {
	startMS := f.nextBarMS - f.timeFrameMS
	endMS := f.nextBarMS
	
	dateStr := time.UnixMilli(startMS).Format("2006-01-02")
	f.ensureDataAvailable(dateStr)
	
	f.mu.RLock()
	dayTrades, exists := f.fileCache[dateStr]
	f.mu.RUnlock()
	
	if !exists {
		f.tradesToFire = nil
		return
	}
	
	// Filter trades for current time window
	var trades []*banexg.Trade
	for _, trade := range dayTrades {
		if trade.Timestamp >= startMS && trade.Timestamp < endMS {
			trades = append(trades, trade)
		} else if trade.Timestamp >= endMS {
			break // Data is sorted, can exit early
		}
	}
	
	f.tradesToFire = trades
}

func (f *TradeFeeder) ensureDataAvailable(targetDate string) {
	f.mu.RLock()
	_, exists := f.fileCache[targetDate]
	f.mu.RUnlock()
	
	if exists {
		return
	}
	
	// Remove oldest cache if needed
	f.mu.Lock()
	defer f.mu.Unlock()
	
	if len(f.fileCache) >= f.cacheSize {
		// Find and remove oldest date
		var oldestDate string
		for date := range f.fileCache {
			if oldestDate == "" || date < oldestDate {
				oldestDate = date
			}
		}
		if oldestDate != "" {
			delete(f.fileCache, oldestDate)
			log.Debug("evicted old cache", zap.String("date", oldestDate))
		}
	}
	
	// Load new data
	trades, err := f.loadFullDayTrades(targetDate)
	if err == nil {
		f.fileCache[targetDate] = trades
		log.Info("loaded new trade data",
			zap.String("date", targetDate),
			zap.Int("trades", len(trades)))
	}
}

func (f *TradeFeeder) fireTrades(trades []*banexg.Trade) {
	if len(trades) == 0 {
		return
	}
	
	// Get all strategy jobs that subscribe to trade data
	// We need to iterate through all accounts
	allJobs := strat.GetJobs("")
	for _, pairJobs := range allJobs {
		for _, job := range pairJobs {
			if job.Strat.OnWsTrades != nil {
				// Check if strategy subscribes to this pair's trades
				if wsSubVal, ok := job.Strat.WsSubs[core.WsSubTrade]; ok {
					if wsSubVal == "_cur_" || wsSubVal == f.symbol {
						job.Strat.OnWsTrades(job, f.symbol, trades)
					}
				}
			}
		}
	}
}

// Start initializes and starts the feeder
func (f *TradeFeeder) Start() error {
	err := f.preloadFiles()
	if err != nil {
		return err
	}
	return nil
}

// Stop stops the feeder
func (f *TradeFeeder) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = true
	f.fileCache = make(map[string][]*banexg.Trade)
}