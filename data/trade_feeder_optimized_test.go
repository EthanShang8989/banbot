package data

import (
	"fmt"
	"testing"

	"github.com/banbox/banexg"
	"github.com/stretchr/testify/assert"
)

func TestOptimizedTradeCache(t *testing.T) {
	t.Run("StreamReading", func(t *testing.T) {
		// Create test trades
		baseTime := int64(1704067200000) // 2024-01-01 00:00:00
		trades := make([]*banexg.Trade, 100)
		for i := 0; i < 100; i++ {
			trades[i] = &banexg.Trade{
				ID:        fmt.Sprintf("%d", i),
				Price:     42000.0 + float64(i),
				Amount:    0.1,
				Timestamp: baseTime + int64(i*100), // 100ms apart
				Side:      banexg.OdSideBuy,
			}
		}

		// Create stream
		stream := NewHourTradeStream(trades)

		// First read: 0-1000ms (10 trades)
		batch1, hasMore := stream.ReadRange(baseTime, baseTime+1000)
		assert.Equal(t, 10, len(batch1))
		assert.True(t, hasMore)
		assert.Equal(t, "0", batch1[0].ID)
		assert.Equal(t, "9", batch1[9].ID)

		// Second read: 1000-2000ms (10 trades)
		batch2, hasMore := stream.ReadRange(baseTime+1000, baseTime+2000)
		assert.Equal(t, 10, len(batch2))
		assert.True(t, hasMore)
		assert.Equal(t, "10", batch2[0].ID)
		assert.Equal(t, "19", batch2[9].ID)

		// Third read with gap: 3000-4000ms (10 trades)
		batch3, hasMore := stream.ReadRange(baseTime+3000, baseTime+4000)
		assert.Equal(t, 10, len(batch3))
		assert.True(t, hasMore)
		assert.Equal(t, "30", batch3[0].ID)
	})

	t.Run("OptimizedCacheUsage", func(t *testing.T) {
		cache := NewOptimizedTradeCache()
		baseTime := int64(1704067200000) // 2024-01-01 00:00:00

		// Load hour 0 trades
		hour0Trades := make([]*banexg.Trade, 50)
		for i := 0; i < 50; i++ {
			hour0Trades[i] = &banexg.Trade{
				ID:        fmt.Sprintf("h0-%d", i),
				Timestamp: baseTime + int64(i*1000), // 1 second apart
			}
		}
		cache.LoadHour("2024-01-01-00", hour0Trades)

		// Load hour 1 trades
		hour1Time := baseTime + 3600000 // 1 hour later
		hour1Trades := make([]*banexg.Trade, 50)
		for i := 0; i < 50; i++ {
			hour1Trades[i] = &banexg.Trade{
				ID:        fmt.Sprintf("h1-%d", i),
				Timestamp: hour1Time + int64(i*1000),
			}
		}
		cache.LoadHour("2024-01-01-01", hour1Trades)

		// Read across hour boundary
		trades := cache.ReadTimeRange("2024-01-01", 
			baseTime+45000,     // Last 5 seconds of hour 0
			hour1Time+5000,     // First 5 seconds of hour 1
			0, 1)

		// Should get last 5 from hour 0 and first 5 from hour 1
		assert.Equal(t, 10, len(trades))
		assert.Equal(t, "h0-45", trades[0].ID)
		assert.Equal(t, "h1-4", trades[9].ID)

		// Verify cleanup works
		cache.CleanupBefore("2024-01-01-01")
		assert.Equal(t, 1, len(cache.hourCache)) // Only hour 1 remains
	})

	t.Run("BinarySearchStart", func(t *testing.T) {
		// Test binary search for finding start position
		baseTime := int64(1704067200000)
		trades := make([]*banexg.Trade, 100)
		for i := 0; i < 100; i++ {
			trades[i] = &banexg.Trade{
				ID:        fmt.Sprintf("%d", i),
				Timestamp: baseTime + int64(i*100),
			}
		}

		stream := NewHourTradeStream(trades)
		
		// Jump to middle
		idx := stream.findStartIndex(baseTime + 5000) // Should find index 50
		assert.Equal(t, 50, idx)
		
		// Read from there
		batch, _ := stream.ReadRange(baseTime+5000, baseTime+6000)
		assert.Equal(t, 10, len(batch))
		assert.Equal(t, "50", batch[0].ID)
	})
}

func BenchmarkTradeCacheComparison(b *testing.B) {
	// Create test data
	baseTime := int64(1704067200000)
	hourTrades := make([]*banexg.Trade, 40000) // Typical hour has ~40k trades
	for i := 0; i < 40000; i++ {
		hourTrades[i] = &banexg.Trade{
			ID:        fmt.Sprintf("%d", i),
			Price:     42000.0 + float64(i%100),
			Amount:    0.1,
			Timestamp: baseTime + int64(i*90), // ~90ms apart
			Side:      banexg.OdSideBuy,
		}
	}

	b.Run("Original_FullScan", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Simulate reading 10ms windows
			windowStart := baseTime + int64(i%3600)*1000
			windowEnd := windowStart + 10
			
			var trades []*banexg.Trade
			for _, trade := range hourTrades {
				if trade.Timestamp >= windowStart && trade.Timestamp < windowEnd {
					trades = append(trades, trade)
				}
			}
			_ = trades
		}
	})

	b.Run("Optimized_Stream", func(b *testing.B) {
		stream := NewHourTradeStream(hourTrades)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Simulate reading 10ms windows
			windowStart := baseTime + int64(i%3600)*1000
			windowEnd := windowStart + 10
			
			if i%3600 == 0 {
				stream.Reset() // Reset every "hour"
			}
			
			trades, _ := stream.ReadRange(windowStart, windowEnd)
			_ = trades
		}
	})
}