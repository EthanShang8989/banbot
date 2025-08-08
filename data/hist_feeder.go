package data

import (
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg/errs"
)

// IHistFeeder is the base interface for all historical data feeders
type IHistFeeder interface {
	// Time management
	getNextMS() int64    // Get next event time
	SetSeek(since int64) // Set start time
	SetEndMS(ms int64)   // Set end time

	// Batch processing
	GetBatch() Batch                  // Get next batch of data
	RunBatch(batch Batch) *errs.Error // Process batch
	CallNext()                        // Move to next time period
}

// IHistWSFeeder is for WebSocket-like data feeders (trades, depth, ticker)
type IHistWSFeeder interface {
	IHistFeeder

	// Symbol information
	getSymbol() string

	// Warmup period handling
	Warmup(curMS, startMS int64, pBar *utils.PrgBar) *errs.Error
}

// TradeFeederAdapter adapts TradeFeeder to IHistWSFeeder
type TradeFeederAdapter struct {
	*TradeFeeder
	precisionMS int64
}

// NewTradeFeederAdapter creates a new adapter for trade feeders
func NewTradeFeederAdapter(feeder *TradeFeeder, precisionMS int64) *TradeFeederAdapter {
	if precisionMS <= 0 {
		precisionMS = 10 // Default 10ms
	}
	return &TradeFeederAdapter{
		TradeFeeder: feeder,
		precisionMS: precisionMS,
	}
}

func (a *TradeFeederAdapter) getNextMS() int64 {
	return a.nextBarMS
}

func (a *TradeFeederAdapter) SetSeek(since int64) {
	a.TradeFeeder.SetSeek(since)
}

func (a *TradeFeederAdapter) SetEndMS(ms int64) {
	a.TradeFeeder.SetEndMS(ms)
}

func (a *TradeFeederAdapter) GetBatch() Batch {
	// Check if we've reached the end
	if a.nextBarMS > a.endMS+a.precisionMS {
		return nil
	}

	// Create batch for current time window
	startMS := a.nextBarMS - a.precisionMS
	batch := NewTradeBatch(a.symbol, startMS, a.nextBarMS)

	// Add trades in this time window
	for _, trade := range a.tradesToFire {
		batch.AddTrade(trade)
	}

	return batch
}

func (a *TradeFeederAdapter) RunBatch(batch Batch) *errs.Error {
	if tradeBatch, ok := batch.(*TradeBatch); ok && len(tradeBatch.Trades) > 0 {
		a.fireTrades(tradeBatch.Trades)
	}
	return nil
}

func (a *TradeFeederAdapter) CallNext() {
	a.nextBarMS += a.precisionMS
	if a.nextBarMS <= a.endMS {
		a.loadNextBatchTrades()
	}
}

func (a *TradeFeederAdapter) getSymbol() string {
	return a.symbol
}

func (a *TradeFeederAdapter) Warmup(curMS, startMS int64, pBar *utils.PrgBar) *errs.Error {
	// Trade data doesn't need traditional warmup
	return nil
}
