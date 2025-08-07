package data

import (
	"testing"

	"github.com/banbox/banbot/config"
)

func TestTradePrecisionConfig(t *testing.T) {
	tests := []struct {
		name         string
		configuredMS int64
		expectedMS   int64
	}{
		{
			name:         "Default 10ms when not configured",
			configuredMS: 0,
			expectedMS:   10,
		},
		{
			name:         "Use 1ms when configured",
			configuredMS: 1,
			expectedMS:   1,
		},
		{
			name:         "Use 100ms when configured",
			configuredMS: 100,
			expectedMS:   100,
		},
		{
			name:         "Use 1000ms (1 second) when configured",
			configuredMS: 1000,
			expectedMS:   1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original value
			originalValue := config.Data.TradePrecisionMS
			defer func() {
				config.Data.TradePrecisionMS = originalValue
			}()

			// Set test value
			config.Data.TradePrecisionMS = tt.configuredMS

			// Create batch config
			batchCfg := DefaultBatchConfig()
			if config.Data.TradePrecisionMS > 0 {
				batchCfg.TradePrecisionMS = config.Data.TradePrecisionMS
			}

			// Check result
			if batchCfg.TradePrecisionMS != tt.expectedMS {
				t.Errorf("Expected precision %dms, got %dms", tt.expectedMS, batchCfg.TradePrecisionMS)
			}
		})
	}
}

func TestTradeFeederAdapterPrecision(t *testing.T) {
	tests := []struct {
		name        string
		precisionMS int64
		expectedMS  int64
	}{
		{
			name:        "Default 10ms when 0",
			precisionMS: 0,
			expectedMS:  10,
		},
		{
			name:        "Use 1ms precision",
			precisionMS: 1,
			expectedMS:  1,
		},
		{
			name:        "Use 10ms precision",
			precisionMS: 10,
			expectedMS:  10,
		},
		{
			name:        "Use 100ms precision",
			precisionMS: 100,
			expectedMS:  100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a dummy TradeFeeder
			feeder := &TradeFeeder{
				symbol:      "BTC/USDT",
				timeFrameMS: 60000, // 1 minute
			}

			// Create adapter with specified precision
			adapter := NewTradeFeederAdapter(feeder, tt.precisionMS)

			// Check precision
			if adapter.precisionMS != tt.expectedMS {
				t.Errorf("Expected adapter precision %dms, got %dms", tt.expectedMS, adapter.precisionMS)
			}
		})
	}
}

func TestAlignTimeWithPrecision(t *testing.T) {
	tests := []struct {
		name        string
		timeMS      int64
		precisionMS int64
		expectedMS  int64
	}{
		{
			name:        "Align to 10ms",
			timeMS:      1704067200123,
			precisionMS: 10,
			expectedMS:  1704067200120,
		},
		{
			name:        "Align to 100ms",
			timeMS:      1704067200456,
			precisionMS: 100,
			expectedMS:  1704067200400,
		},
		{
			name:        "Align to 1000ms (1 second)",
			timeMS:      1704067200789,
			precisionMS: 1000,
			expectedMS:  1704067200000,
		},
		{
			name:        "No alignment with 0 precision",
			timeMS:      1704067200123,
			precisionMS: 0,
			expectedMS:  1704067200123,
		},
		{
			name:        "Align to 1ms (no change needed)",
			timeMS:      1704067200123,
			precisionMS: 1,
			expectedMS:  1704067200123,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AlignTime(tt.timeMS, tt.precisionMS)
			if result != tt.expectedMS {
				t.Errorf("AlignTime(%d, %d) = %d, expected %d",
					tt.timeMS, tt.precisionMS, result, tt.expectedMS)
			}
		})
	}
}