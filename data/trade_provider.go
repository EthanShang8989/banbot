package data

import (
	"sync"

	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
)

// FileTradeProvider manages multiple TradeFeeder instances
type FileTradeProvider struct {
	feeders   map[string]*TradeFeeder // Symbol -> TradeFeeder mapping
	dataDir   string
	startMS   int64
	endMS     int64
	timeframe string
	market    string // "spot" or "linear" for futures
	mu        sync.RWMutex
}

// NewFileTradeProvider creates a new file trade data provider
func NewFileTradeProvider(dataDir, timeframe string, market string) *FileTradeProvider {
	return &FileTradeProvider{
		feeders:   make(map[string]*TradeFeeder),
		dataDir:   dataDir,
		timeframe: timeframe,
		market:    market,
	}
}

// SetTimeRange sets the time range for all feeders
func (p *FileTradeProvider) SetTimeRange(startMS, endMS int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	p.startMS = startMS
	p.endMS = endMS
	
	// Update existing feeders
	for _, feeder := range p.feeders {
		feeder.SetSeek(startMS)
		feeder.SetEndMS(endMS)
	}
}

// AddSymbol adds a new symbol to track
func (p *FileTradeProvider) AddSymbol(symbol string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if _, exists := p.feeders[symbol]; exists {
		return nil // Already tracking
	}
	
	feeder := NewTradeFeeder(symbol, p.dataDir, p.startMS, p.endMS, p.timeframe, p.market)
	if err := feeder.Start(); err != nil {
		return err
	}
	
	p.feeders[symbol] = feeder
	log.Info("added trade feeder", zap.String("symbol", symbol), zap.String("market", p.market))
	
	return nil
}

// RemoveSymbol removes a symbol from tracking
func (p *FileTradeProvider) RemoveSymbol(symbol string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if feeder, exists := p.feeders[symbol]; exists {
		feeder.Stop()
		delete(p.feeders, symbol)
		log.Info("removed trade feeder", zap.String("symbol", symbol))
	}
}

// GetFeeders returns all feeders as IHistKlineFeeder interface
func (p *FileTradeProvider) GetFeeders() []IHistKlineFeeder {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	feeders := make([]IHistKlineFeeder, 0, len(p.feeders))
	for _, feeder := range p.feeders {
		feeders = append(feeders, feeder)
	}
	
	return feeders
}

// DownloadAll downloads all required data for all symbols
func (p *FileTradeProvider) DownloadAll(sess interface{}, exchange interface{}, pBar interface{}) *errs.Error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	for symbol, feeder := range p.feeders {
		if err := feeder.DownIfNeed(nil, nil, nil); err != nil {
			log.Error("failed to download trade data",
				zap.String("symbol", symbol),
				zap.Error(err))
			return err
		}
	}
	
	return nil
}

// Stop stops all feeders
func (p *FileTradeProvider) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	for _, feeder := range p.feeders {
		feeder.Stop()
	}
	
	p.feeders = make(map[string]*TradeFeeder)
	log.Info("stopped all trade feeders")
}