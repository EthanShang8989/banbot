package data

import "github.com/banbox/banexg"

// Batch represents a batch of data within a time period
type Batch interface {
	// Basic time information
	StartTime() int64 // Batch start time (milliseconds)
	EndTime() int64   // Batch end time (milliseconds)
	Symbol() string   // Trading pair

	// Data type identification
	Type() BatchType // Batch type
	IsEmpty() bool   // Whether the batch has no data

	// Statistics (optional implementation)
	Count() int // Number of data items
}

// BatchType represents the type of batch data
type BatchType string

const (
	BatchTypeKline    BatchType = "kline"
	BatchTypeTrade    BatchType = "trade"
	BatchTypeAggTrade BatchType = "aggTrade"
)

// BaseBatch provides basic batch implementation (can be embedded by other batches)
type BaseBatch struct {
	symbol    string
	startMS   int64
	endMS     int64
	batchType BatchType
}

func (b *BaseBatch) StartTime() int64 { return b.startMS }
func (b *BaseBatch) EndTime() int64   { return b.endMS }
func (b *BaseBatch) Symbol() string   { return b.symbol }
func (b *BaseBatch) Type() BatchType  { return b.batchType }
func (b *BaseBatch) IsEmpty() bool    { return false }
func (b *BaseBatch) Count() int       { return 0 }

// KlineBatch represents a K-line batch (usually 1 minute)
type KlineBatch struct {
	BaseBatch
	Kline *banexg.Kline
}

// NewKlineBatch creates a new K-line batch
func NewKlineBatch(symbol string, kline *banexg.Kline, timeFrameMS int64) *KlineBatch {
	if kline == nil {
		return &KlineBatch{
			BaseBatch: BaseBatch{
				symbol:    symbol,
				startMS:   0,
				endMS:     0,
				batchType: BatchTypeKline,
			},
			Kline: nil,
		}
	}
	return &KlineBatch{
		BaseBatch: BaseBatch{
			symbol:    symbol,
			startMS:   kline.Time,
			endMS:     kline.Time + timeFrameMS,
			batchType: BatchTypeKline,
		},
		Kline: kline,
	}
}

func (b *KlineBatch) IsEmpty() bool { return b.Kline == nil }
func (b *KlineBatch) Count() int {
	if b.Kline == nil {
		return 0
	}
	return 1
}

// TradeBatch represents a batch of trades within a time period (e.g., all trades within 10ms)
type TradeBatch struct {
	BaseBatch
	Trades []*banexg.Trade

	// Statistics (optional)
	TotalVolume float64
	TotalValue  float64
	VWAPPrice   float64 // Volume-weighted average price
}

// NewTradeBatch creates a new trade batch
func NewTradeBatch(symbol string, startMS, endMS int64) *TradeBatch {
	return &TradeBatch{
		BaseBatch: BaseBatch{
			symbol:    symbol,
			startMS:   startMS,
			endMS:     endMS,
			batchType: BatchTypeTrade,
		},
		Trades: make([]*banexg.Trade, 0),
	}
}

// AddTrade adds a trade to the batch and updates statistics
func (b *TradeBatch) AddTrade(trade *banexg.Trade) {
	b.Trades = append(b.Trades, trade)

	// Update statistics
	b.TotalVolume += trade.Amount
	b.TotalValue += trade.Amount * trade.Price
	if b.TotalVolume > 0 {
		b.VWAPPrice = b.TotalValue / b.TotalVolume
	}
}

func (b *TradeBatch) IsEmpty() bool { return len(b.Trades) == 0 }
func (b *TradeBatch) Count() int    { return len(b.Trades) }

// BatchConfig represents batch configuration
type BatchConfig struct {
	// Time precision configuration (milliseconds)
	TradePrecisionMS int64 `yaml:"trade_precision_ms"` // Default 10ms

	// Batch size limits
	MaxTradesPerBatch int `yaml:"max_trades_per_batch"` // Maximum trades per batch
	MaxBatchMemoryMB  int `yaml:"max_batch_memory_mb"`  // Maximum batch memory
}

// DefaultBatchConfig returns default batch configuration
func DefaultBatchConfig() *BatchConfig {
	return &BatchConfig{
		TradePrecisionMS:  10,    // 10ms
		MaxTradesPerBatch: 50000, // Prevent memory explosion
		MaxBatchMemoryMB:  100,
	}
}

// AlignTime aligns time to the specified precision
func AlignTime(timeMS int64, precisionMS int64) int64 {
	if precisionMS <= 0 {
		return timeMS
	}
	return (timeMS / precisionMS) * precisionMS
}
