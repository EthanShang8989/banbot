package data

import (
	"testing"
	"time"

	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/orm"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestBatchInterfaceV2(t *testing.T) {
	t.Run("KlineBatch", func(t *testing.T) {
		kline := &banexg.Kline{
			Time:   1704067200000,
			Open:   42000,
			High:   42100,
			Low:    41900,
			Close:  42050,
			Volume: 100,
		}
		
		batch := NewKlineBatch("BTC/USDT", kline, 60000)
		
		assert.Equal(t, int64(1704067200000), batch.StartTime())
		assert.Equal(t, int64(1704067260000), batch.EndTime())
		assert.Equal(t, "BTC/USDT", batch.Symbol())
		assert.Equal(t, BatchTypeKline, batch.Type())
		assert.False(t, batch.IsEmpty())
		assert.Equal(t, 1, batch.Count())
	})
	
	t.Run("TradeBatch", func(t *testing.T) {
		batch := NewTradeBatch("BTC/USDT", 1704067200000, 1704067200010)
		
		// Add trades
		batch.AddTrade(&banexg.Trade{
			ID:        "1",
			Price:     42000,
			Amount:    0.5,
			Timestamp: 1704067200002,
			Side:      banexg.OdSideBuy,
		})
		
		batch.AddTrade(&banexg.Trade{
			ID:        "2",
			Price:     42010,
			Amount:    0.3,
			Timestamp: 1704067200005,
			Side:      banexg.OdSideSell,
		})
		
		assert.Equal(t, int64(1704067200000), batch.StartTime())
		assert.Equal(t, int64(1704067200010), batch.EndTime())
		assert.Equal(t, "BTC/USDT", batch.Symbol())
		assert.Equal(t, BatchTypeTrade, batch.Type())
		assert.False(t, batch.IsEmpty())
		assert.Equal(t, 2, batch.Count())
		assert.Equal(t, 2, len(batch.Trades))
	})
}

func TestRunHistFeedersV2(t *testing.T) {
	t.Run("BasicFunctionality", func(t *testing.T) {
		// Simple test to verify RunHistFeedersV2 works
		processed := 0
		
		// Create a simple mock feeder
		mockFeeder := &SimpleMockFeeder{
			batches: []*SimpleMockBatch{
				{startMS: 1000, endMS: 1010, symbol: "TEST", batchType: BatchTypeTrade},
				{startMS: 1010, endMS: 1020, symbol: "TEST", batchType: BatchTypeTrade},
			},
		}
		
		makeFeeders := func() []IHistFeeder {
			return []IHistFeeder{mockFeeder}
		}
		
		versions := make(chan int, 1)
		
		// Track processed batches
		mockFeeder.onRun = func() {
			processed++
			if processed >= 2 {
				versions <- -1 // Stop after processing 2 batches
			}
		}
		
		// Run test
		core.BotRunning = true
		err := RunHistFeedersV2(makeFeeders, versions, nil)
		core.BotRunning = false
		
		// Check if error is expected (e.g., no more data)
		if err != nil {
			t.Logf("RunHistFeedersV2 returned: %v", err)
		}
		assert.Equal(t, 2, processed, "should process 2 batches")
	})
}

func TestHistProviderV2(t *testing.T) {
	t.Run("Integration", func(t *testing.T) {
		// Initialize config
		if config.TimeRange == nil {
			config.TimeRange = &config.TimeTuple{}
		}
		config.TimeRange.StartMS = 1704067200000
		config.TimeRange.EndMS = 1704067800000
		
		// Create provider
		var barCount int
		onBar := func(bar *orm.InfoKline) {
			barCount++
			log.Debug("processed bar", 
				zap.String("symbol", bar.Symbol),
				zap.Int64("time", bar.Time))
		}
		
		onEnvEnd := func(bar *banexg.PairTFKline, adj *orm.AdjInfo) {
			log.Debug("environment ended")
		}
		
		getEnd := func() int64 {
			return config.TimeRange.EndMS
		}
		
		provider := NewHistProviderV2(onBar, onEnvEnd, getEnd, true, nil)
		
		// Add mock K-line feeder
		mockKlineFeeder := &MockHistKlineFeeder{
			symbol:      "BTC/USDT",
			bars:        createMockKlines(),
			currentIdx:  0,
			timeFrameMS: 60000,
		}
		provider.AddKlineFeeder("BTC/USDT", mockKlineFeeder, 60000)
		
		// Add mock trade feeder
		mockTradeFeeder := &TradeFeeder{
			symbol:      "ETH/USDT",
			startMS:     config.TimeRange.StartMS,
			endMS:       config.TimeRange.EndMS,
			timeFrameMS: 10,
			nextBarMS:   config.TimeRange.StartMS + 10,
		}
		provider.AddTradeFeeder("ETH/USDT", mockTradeFeeder)
		
		// Verify feeders are added
		feeders := provider.MakeFeeders()
		assert.Equal(t, 2, len(feeders), "should have 2 feeders")
		
		// Test SetDirty
		provider.SetDirty()
		select {
		case v := <-provider.dirtyVers:
			assert.Equal(t, 1, v)
		default:
			t.Error("SetDirty should send version")
		}
		
		// Test Terminate
		go func() {
			time.Sleep(10 * time.Millisecond)
			provider.Terminate()
		}()
		
		select {
		case v := <-provider.dirtyVers:
			assert.Equal(t, -1, v, "Terminate should send -1")
		case <-time.After(100 * time.Millisecond):
			t.Error("Terminate timeout")
		}
	})
}

// Mock implementations for testing

// SimpleMockBatch is a simple batch implementation for testing
type SimpleMockBatch struct {
	startMS   int64
	endMS     int64
	symbol    string
	batchType BatchType
}

func (b *SimpleMockBatch) StartTime() int64 { return b.startMS }
func (b *SimpleMockBatch) EndTime() int64   { return b.endMS }
func (b *SimpleMockBatch) Symbol() string   { return b.symbol }
func (b *SimpleMockBatch) Type() BatchType  { return b.batchType }
func (b *SimpleMockBatch) IsEmpty() bool    { return false }
func (b *SimpleMockBatch) Count() int       { return 1 }

// SimpleMockFeeder is a simple feeder for testing
type SimpleMockFeeder struct {
	batches []*SimpleMockBatch
	idx     int
	onRun   func()
}

func (f *SimpleMockFeeder) getNextMS() int64 {
	if f.idx < len(f.batches) {
		return f.batches[f.idx].startMS
	}
	return int64(1<<63 - 1)
}

func (f *SimpleMockFeeder) SetSeek(since int64) {
	for i, b := range f.batches {
		if b.startMS >= since {
			f.idx = i
			return
		}
	}
}

func (f *SimpleMockFeeder) SetEndMS(ms int64) {}

func (f *SimpleMockFeeder) GetBatch() Batch {
	if f.idx < len(f.batches) {
		return f.batches[f.idx]
	}
	return nil
}

func (f *SimpleMockFeeder) RunBatch(batch Batch) *errs.Error {
	if f.onRun != nil {
		f.onRun()
	}
	return nil
}

func (f *SimpleMockFeeder) CallNext() {
	f.idx++
}

type MockHistKlineFeeder struct {
	symbol      string
	bars        []*banexg.Kline
	currentIdx  int
	timeFrameMS int64
}

func (m *MockHistKlineFeeder) getSymbol() string                    { return m.symbol }
func (m *MockHistKlineFeeder) getStates() []*PairTFCache            { return nil }
func (m *MockHistKlineFeeder) getWaitBar() *banexg.Kline            { return nil }
func (m *MockHistKlineFeeder) setWaitBar(bar *banexg.Kline)         {}
func (m *MockHistKlineFeeder) SubTfs(tfs []string, del bool) []string { return nil }
func (m *MockHistKlineFeeder) WarmTfs(cur int64, nums map[string]int, p *utils.PrgBar) (int64, map[string][2]int, *errs.Error) {
	return cur, nil, nil
}
func (m *MockHistKlineFeeder) onNewBars(tfMS int64, bars []*banexg.Kline) (bool, *errs.Error) {
	return false, nil
}
func (m *MockHistKlineFeeder) getNextMS() int64 {
	if m.currentIdx < len(m.bars) {
		return m.bars[m.currentIdx].Time
	}
	return int64(1<<63 - 1)
}
func (m *MockHistKlineFeeder) DownIfNeed(sess *orm.Queries, exg banexg.BanExchange, p *utils.PrgBar) *errs.Error {
	return nil
}
func (m *MockHistKlineFeeder) SetSeek(since int64) {
	for i, bar := range m.bars {
		if bar.Time >= since {
			m.currentIdx = i
			break
		}
	}
}
func (m *MockHistKlineFeeder) SetEndMS(ms int64) {}

// GetBatch implements IHistFeeder interface
func (m *MockHistKlineFeeder) GetBatch() Batch {
	if m.currentIdx < len(m.bars) {
		return NewKlineBatch(m.symbol, m.bars[m.currentIdx], m.timeFrameMS)
	}
	return nil
}

// RunBatch implements IHistFeeder interface
func (m *MockHistKlineFeeder) RunBatch(batch Batch) *errs.Error {
	if klineBatch, ok := batch.(*KlineBatch); ok {
		btime.CurTimeMS = klineBatch.Kline.Time
	}
	return nil
}

func (m *MockHistKlineFeeder) CallNext() {
	m.currentIdx++
}

type MockTradeFeeder struct {
	symbol      string
	batches     []*TradeBatch
	currentIdx  int
	precisionMS int64
}

type MockTradeFeederAdapter struct {
	feeder       *MockTradeFeeder
	RunBatchFunc func(Batch) *errs.Error
}

func (m *MockTradeFeederAdapter) getNextMS() int64 {
	if m.feeder.currentIdx < len(m.feeder.batches) {
		return m.feeder.batches[m.feeder.currentIdx].StartTime()
	}
	return int64(1<<63 - 1)
}
func (m *MockTradeFeederAdapter) SetSeek(since int64) {
	for i, batch := range m.feeder.batches {
		if batch.StartTime() >= since {
			m.feeder.currentIdx = i
			break
		}
	}
}
func (m *MockTradeFeederAdapter) SetEndMS(ms int64) {}
func (m *MockTradeFeederAdapter) GetBatch() Batch {
	if m.feeder.currentIdx < len(m.feeder.batches) {
		return m.feeder.batches[m.feeder.currentIdx]
	}
	return nil
}
func (m *MockTradeFeederAdapter) RunBatch(batch Batch) *errs.Error {
	if m.RunBatchFunc != nil {
		return m.RunBatchFunc(batch)
	}
	return nil
}
func (m *MockTradeFeederAdapter) CallNext() {
	m.feeder.currentIdx++
}

// Helper functions

func createMockKlines() []*banexg.Kline {
	return []*banexg.Kline{
		{Time: 1704067200000, Open: 42000, High: 42100, Low: 41900, Close: 42050},
		{Time: 1704067260000, Open: 42050, High: 42150, Low: 42000, Close: 42100},
		{Time: 1704067320000, Open: 42100, High: 42200, Low: 42050, Close: 42150},
	}
}

