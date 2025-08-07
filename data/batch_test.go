package data

import (
	"testing"

	"github.com/banbox/banexg"
	"github.com/stretchr/testify/assert"
)

func TestBaseBatch(t *testing.T) {
	batch := &BaseBatch{
		symbol:    "BTC/USDT",
		startMS:   1704067200000,
		endMS:     1704067260000,
		batchType: BatchTypeKline,
	}

	assert.Equal(t, "BTC/USDT", batch.Symbol())
	assert.Equal(t, int64(1704067200000), batch.StartTime())
	assert.Equal(t, int64(1704067260000), batch.EndTime())
	assert.Equal(t, BatchTypeKline, batch.Type())
	assert.False(t, batch.IsEmpty())
	assert.Equal(t, 0, batch.Count())
}

func TestKlineBatch(t *testing.T) {
	t.Run("with kline data", func(t *testing.T) {
		kline := &banexg.Kline{
			Time:   1704067200000,
			Open:   42000.0,
			High:   42500.0,
			Low:    41800.0,
			Close:  42300.0,
			Volume: 100.5,
		}

		batch := NewKlineBatch("BTC/USDT", kline, 60000) // 1 minute timeframe

		assert.Equal(t, "BTC/USDT", batch.Symbol())
		assert.Equal(t, int64(1704067200000), batch.StartTime())
		assert.Equal(t, int64(1704067260000), batch.EndTime()) // +60 seconds
		assert.Equal(t, BatchTypeKline, batch.Type())
		assert.False(t, batch.IsEmpty())
		assert.Equal(t, 1, batch.Count())
		assert.Equal(t, kline, batch.Kline)
	})

	t.Run("with nil kline", func(t *testing.T) {
		batch := NewKlineBatch("BTC/USDT", nil, 60000)

		assert.Equal(t, "BTC/USDT", batch.Symbol())
		assert.Equal(t, int64(0), batch.StartTime())
		assert.Equal(t, int64(0), batch.EndTime())
		assert.Equal(t, BatchTypeKline, batch.Type())
		assert.True(t, batch.IsEmpty())
		assert.Equal(t, 0, batch.Count())
		assert.Nil(t, batch.Kline)
	})
}

func TestTradeBatch(t *testing.T) {
	t.Run("empty batch", func(t *testing.T) {
		batch := NewTradeBatch("BTC/USDT", 1704067200000, 1704067200010) // 10ms window

		assert.Equal(t, "BTC/USDT", batch.Symbol())
		assert.Equal(t, int64(1704067200000), batch.StartTime())
		assert.Equal(t, int64(1704067200010), batch.EndTime())
		assert.Equal(t, BatchTypeTrade, batch.Type())
		assert.True(t, batch.IsEmpty())
		assert.Equal(t, 0, batch.Count())
		assert.Equal(t, 0, len(batch.Trades))
	})

	t.Run("batch with trades", func(t *testing.T) {
		batch := NewTradeBatch("BTC/USDT", 1704067200000, 1704067200010)

		// Add first trade
		trade1 := &banexg.Trade{
			Symbol:    "BTC/USDT",
			ID:        "1",
			Price:     42000.0,
			Amount:    0.5,
			Timestamp: 1704067200002,
			Side:      banexg.OdSideBuy,
		}
		batch.AddTrade(trade1)

		assert.False(t, batch.IsEmpty())
		assert.Equal(t, 1, batch.Count())
		assert.Equal(t, 0.5, batch.TotalVolume)
		assert.Equal(t, 21000.0, batch.TotalValue)
		assert.Equal(t, 42000.0, batch.VWAPPrice)

		// Add second trade
		trade2 := &banexg.Trade{
			Symbol:    "BTC/USDT",
			ID:        "2",
			Price:     42100.0,
			Amount:    1.0,
			Timestamp: 1704067200005,
			Side:      banexg.OdSideSell,
		}
		batch.AddTrade(trade2)

		assert.Equal(t, 2, batch.Count())
		assert.Equal(t, 1.5, batch.TotalVolume)
		assert.Equal(t, 63100.0, batch.TotalValue)
		assert.InDelta(t, 42066.67, batch.VWAPPrice, 0.01)
	})

	t.Run("VWAP calculation", func(t *testing.T) {
		batch := NewTradeBatch("BTC/USDT", 1704067200000, 1704067200010)

		trades := []struct {
			price  float64
			amount float64
		}{
			{100.0, 10.0},  // 1000 value
			{110.0, 20.0},  // 2200 value
			{105.0, 15.0},  // 1575 value
		}

		for i, tr := range trades {
			batch.AddTrade(&banexg.Trade{
				ID:        string(rune('0' + i)),
				Price:     tr.price,
				Amount:    tr.amount,
				Timestamp: 1704067200000 + int64(i),
			})
		}

		expectedVolume := 45.0                          // 10 + 20 + 15
		expectedValue := 4775.0                         // 1000 + 2200 + 1575
		expectedVWAP := expectedValue / expectedVolume  // 106.111...

		assert.Equal(t, 3, batch.Count())
		assert.Equal(t, expectedVolume, batch.TotalVolume)
		assert.Equal(t, expectedValue, batch.TotalValue)
		assert.InDelta(t, expectedVWAP, batch.VWAPPrice, 0.01)
	})
}

func TestBatchConfig(t *testing.T) {
	config := DefaultBatchConfig()

	assert.Equal(t, int64(10), config.TradePrecisionMS)
	assert.Equal(t, 10000, config.MaxTradesPerBatch)
	assert.Equal(t, 100, config.MaxBatchMemoryMB)
}

func TestAlignTime(t *testing.T) {
	tests := []struct {
		name        string
		timeMS      int64
		precisionMS int64
		expected    int64
	}{
		{
			name:        "align to 10ms",
			timeMS:      1704067200007,
			precisionMS: 10,
			expected:    1704067200000,
		},
		{
			name:        "align to 100ms",
			timeMS:      1704067200157,
			precisionMS: 100,
			expected:    1704067200100,
		},
		{
			name:        "align to 1000ms",
			timeMS:      1704067200999,
			precisionMS: 1000,
			expected:    1704067200000,
		},
		{
			name:        "already aligned",
			timeMS:      1704067200100,
			precisionMS: 100,
			expected:    1704067200100,
		},
		{
			name:        "zero precision",
			timeMS:      1704067200123,
			precisionMS: 0,
			expected:    1704067200123,
		},
		{
			name:        "negative precision",
			timeMS:      1704067200123,
			precisionMS: -10,
			expected:    1704067200123,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AlignTime(tt.timeMS, tt.precisionMS)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBatchInterface(t *testing.T) {
	// Test that all batch types implement the Batch interface
	var _ Batch = &BaseBatch{}
	var _ Batch = &KlineBatch{}
	var _ Batch = &TradeBatch{}

	// Test polymorphic usage
	batches := []Batch{
		NewKlineBatch("BTC/USDT", &banexg.Kline{Time: 1704067200000}, 60000),
		NewTradeBatch("ETH/USDT", 1704067200000, 1704067200010),
	}

	for i, batch := range batches {
		assert.NotEmpty(t, batch.Symbol())
		assert.NotZero(t, batch.StartTime())
		assert.NotZero(t, batch.EndTime())
		assert.NotEmpty(t, batch.Type())

		if i == 0 {
			assert.Equal(t, BatchTypeKline, batch.Type())
		} else {
			assert.Equal(t, BatchTypeTrade, batch.Type())
		}
	}
}

func BenchmarkTradeBatchAddTrade(b *testing.B) {
	batch := NewTradeBatch("BTC/USDT", 1704067200000, 1704067200010)
	trade := &banexg.Trade{
		Symbol:    "BTC/USDT",
		ID:        "1",
		Price:     42000.0,
		Amount:    0.5,
		Timestamp: 1704067200002,
		Side:      banexg.OdSideBuy,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		batch.AddTrade(trade)
	}
}

func BenchmarkAlignTime(b *testing.B) {
	timeMS := int64(1704067200007)
	precisionMS := int64(10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = AlignTime(timeMS, precisionMS)
	}
}