package data

import (
	"fmt"
	"sort"
	
	"github.com/banbox/banexg"
)

// HourTradeStream represents a stream of trades for efficient sequential reading
type HourTradeStream struct {
	trades []*banexg.Trade
	cursor int // Current position in the trades slice
}

// NewHourTradeStream creates a new trade stream
func NewHourTradeStream(trades []*banexg.Trade) *HourTradeStream {
	return &HourTradeStream{
		trades: trades,
		cursor: 0,
	}
}

// ReadRange reads trades within [startMS, endMS) and advances cursor
// Returns trades in range and whether there are more trades after endMS
func (s *HourTradeStream) ReadRange(startMS, endMS int64) ([]*banexg.Trade, bool) {
	if s.cursor >= len(s.trades) {
		return nil, false
	}
	
	// Binary search to find start position if cursor is behind
	if s.cursor == 0 || s.trades[s.cursor].Timestamp < startMS {
		s.cursor = s.findStartIndex(startMS)
	}
	
	// Collect trades in range
	var result []*banexg.Trade
	
	for s.cursor < len(s.trades) {
		trade := s.trades[s.cursor]
		if trade.Timestamp >= endMS {
			// Found a trade beyond our range, stop here
			break
		}
		if trade.Timestamp >= startMS {
			result = append(result, trade)
		}
		s.cursor++
	}
	
	// Check if there are more trades
	hasMore := s.cursor < len(s.trades)
	
	return result, hasMore
}

// findStartIndex uses binary search to find first trade >= startMS
func (s *HourTradeStream) findStartIndex(startMS int64) int {
	return sort.Search(len(s.trades), func(i int) bool {
		return s.trades[i].Timestamp >= startMS
	})
}

// HasMore returns true if there are unread trades
func (s *HourTradeStream) HasMore() bool {
	return s.cursor < len(s.trades)
}

// Reset resets the cursor to beginning
func (s *HourTradeStream) Reset() {
	s.cursor = 0
}

// OptimizedTradeCache wraps hourly trades with streaming capability
type OptimizedTradeCache struct {
	// Original hour cache
	hourCache map[string][]*banexg.Trade
	
	// Stream cache for efficient sequential reading
	streams map[string]*HourTradeStream
}

// NewOptimizedTradeCache creates an optimized cache
func NewOptimizedTradeCache() *OptimizedTradeCache {
	return &OptimizedTradeCache{
		hourCache: make(map[string][]*banexg.Trade),
		streams:   make(map[string]*HourTradeStream),
	}
}

// LoadHour loads an hour's trades and creates a stream
func (c *OptimizedTradeCache) LoadHour(key string, trades []*banexg.Trade) {
	c.hourCache[key] = trades
	c.streams[key] = NewHourTradeStream(trades)
}

// ReadTimeRange efficiently reads trades in time range across hours
func (c *OptimizedTradeCache) ReadTimeRange(dateStr string, startMS, endMS int64, startHour, endHour int) []*banexg.Trade {
	var result []*banexg.Trade
	
	for hour := startHour; hour <= endHour && hour < 24; hour++ {
		key := fmt.Sprintf("%s-%02d", dateStr, hour)
		stream, exists := c.streams[key]
		if !exists {
			continue
		}
		
		// Read from stream (cursor advances automatically)
		trades, _ := stream.ReadRange(startMS, endMS)
		result = append(result, trades...)
		
		// Clean up if this hour is fully consumed and we've moved past it
		if !stream.HasMore() && hour < endHour {
			c.CleanupHour(key)
		}
	}
	
	return result
}

// CleanupHour removes hour data that's been fully consumed
func (c *OptimizedTradeCache) CleanupHour(key string) {
	delete(c.hourCache, key)
	delete(c.streams, key)
}

// CleanupBefore removes all hours before the given hour key
func (c *OptimizedTradeCache) CleanupBefore(currentKey string) {
	for key := range c.hourCache {
		if key < currentKey {
			c.CleanupHour(key)
		}
	}
}

// Example usage in TradeFeeder:
/*
func (f *TradeFeeder) loadNextBatchTradesOptimized() {
	startMS := f.nextBarMS - f.timeFrameMS
	endMS := f.nextBarMS
	
	// Use optimized cache
	if f.optimizedCache == nil {
		f.optimizedCache = NewOptimizedTradeCache()
	}
	
	startTime := time.UnixMilli(startMS)
	dateStr := startTime.Format("2006-01-02")
	startHour := startTime.Hour()
	endHour := startTime.Add(time.Duration(f.timeFrameMS)*time.Millisecond).Hour()
	
	// Efficiently read from cache with cursor
	trades := f.optimizedCache.ReadTimeRange(dateStr, startMS, endMS, startHour, endHour)
	
	// Clean up old hours periodically
	currentKey := fmt.Sprintf("%s-%02d", dateStr, startHour)
	f.optimizedCache.CleanupBefore(currentKey)
	
	f.tradesToFire = trades
}
*/