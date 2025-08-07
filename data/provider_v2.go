package data

import (
	"math"
	"sort"

	"github.com/banbox/banbot/btime"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
)

// RunHistFeedersV2 runs historical feeders with the new Batch interface
// This is the upgraded version that supports both K-line and high-frequency data
func RunHistFeedersV2(makeFeeders func() []IHistFeeder, versions chan int, pBar *utils.PrgBar) *errs.Error {
	var lastTimeMS int64
	var oldVer int
	var feeders []IHistFeeder
	var firstInit = true
	
	for {
		// Check for version updates
		var ver = 0
		select {
		case ver = <-versions:
			if ver < 0 {
				return nil // Exit signal
			}
		default:
			ver = 0
		}
		
		// Refresh feeders if version changed
		if ver > oldVer || firstInit {
			feeders = makeFeeders()
			feeders = SortFeedersV2(feeders)
			oldVer = max(oldVer, ver)
			firstInit = false
		}
		
		// Find feeder with earliest next time
		minTimeMS := int64(math.MaxInt64)
		var earliestFeeder IHistFeeder
		var earliestIdx int
		
		for i, feeder := range feeders {
			nextMS := feeder.getNextMS()
			if nextMS < minTimeMS {
				minTimeMS = nextMS
				earliestFeeder = feeder
				earliestIdx = i
			}
		}
		
		// No more data
		if earliestFeeder == nil || minTimeMS == math.MaxInt64 {
			break
		}
		
		// Get batch from earliest feeder
		batch := earliestFeeder.GetBatch()
		if batch == nil {
			// Remove exhausted feeder
			feeders = append(feeders[:earliestIdx], feeders[earliestIdx+1:]...)
			continue
		}
		
		// Update virtual time
		btime.CurTimeMS = batch.StartTime()
		
		// Process batch
		err := earliestFeeder.RunBatch(batch)
		if err != nil {
			log.Error("RunBatch failed", 
				zap.String("type", string(batch.Type())),
				zap.String("symbol", batch.Symbol()),
				zap.Int64("time", batch.StartTime()),
				zap.Error(err))
		}
		
		// Move to next time period
		earliestFeeder.CallNext()
		
		// Update progress bar
		if batch.StartTime() > lastTimeMS {
			if pBar != nil {
				if pBar.Last == 0 {
					pBar.Last = lastTimeMS
				}
				pBar.Add(int((batch.StartTime() - pBar.Last) / 1000))
				pBar.Last = batch.StartTime()
			}
			lastTimeMS = batch.StartTime()
		}
		
		// Check stop signal
		if !core.BotRunning {
			return nil
		}
	}
	
	return nil
}

// SortFeedersV2 sorts feeders by their next event time
func SortFeedersV2(feeders []IHistFeeder) []IHistFeeder {
	sort.Slice(feeders, func(i, j int) bool {
		timeI := feeders[i].getNextMS()
		timeJ := feeders[j].getNextMS()
		
		if timeI != timeJ {
			return timeI < timeJ
		}
		
		// If times are equal, sort by type priority
		// K-line data has lower priority than high-frequency data
		batchI := feeders[i].GetBatch()
		batchJ := feeders[j].GetBatch()
		
		if batchI != nil && batchJ != nil {
			// Trade data before K-line data at same time
			if batchI.Type() == BatchTypeTrade && batchJ.Type() == BatchTypeKline {
				return true
			}
			if batchI.Type() == BatchTypeKline && batchJ.Type() == BatchTypeTrade {
				return false
			}
			
			// Otherwise sort by symbol
			return batchI.Symbol() < batchJ.Symbol()
		}
		
		return false
	})
	
	return feeders
}

// HistProviderV2 provides historical data using the new Batch interface
type HistProviderV2 struct {
	// K-line feeders (existing)
	klineFeeders map[string]IHistKlineFeeder
	
	// High-frequency data feeders (new)
	tradeFeeders   map[string]IHistWSFeeder
	aggTradeFeeders map[string]IHistWSFeeder
	
	// Configuration
	config      *BatchConfig
	onBar       FnPairKline
	onEnvEnd    FuncEnvEnd
	getEnd      FnGetInt64
	showLog     bool
	pBar        *utils.StagedPrg
	dirtyVers   chan int
}

// NewHistProviderV2 creates a new historical data provider with Batch support
func NewHistProviderV2(onBar FnPairKline, onEnvEnd FuncEnvEnd, getEnd FnGetInt64, showLog bool, pBar *utils.StagedPrg) *HistProviderV2 {
	batchCfg := DefaultBatchConfig()
	// Use configured precision if available
	if config.Data.TradePrecisionMS > 0 {
		batchCfg.TradePrecisionMS = config.Data.TradePrecisionMS
	}
	return &HistProviderV2{
		klineFeeders:    make(map[string]IHistKlineFeeder),
		tradeFeeders:    make(map[string]IHistWSFeeder),
		aggTradeFeeders: make(map[string]IHistWSFeeder),
		config:          batchCfg,
		onBar:           onBar,
		onEnvEnd:        onEnvEnd,
		getEnd:          getEnd,
		showLog:         showLog,
		pBar:            pBar,
		dirtyVers:       make(chan int, 2),
	}
}

// AddTradeFeeder adds a trade data feeder for a symbol
func (p *HistProviderV2) AddTradeFeeder(symbol string, feeder *TradeFeeder) {
	adapter := NewTradeFeederAdapter(feeder, p.config.TradePrecisionMS)
	p.tradeFeeders[symbol] = adapter
}

// AddKlineFeeder adds a K-line feeder for a symbol
func (p *HistProviderV2) AddKlineFeeder(symbol string, feeder IHistKlineFeeder, timeFrameMS int64) {
	p.klineFeeders[symbol] = feeder
}

// MakeFeeders returns all feeders as IHistFeeder interface
func (p *HistProviderV2) MakeFeeders() []IHistFeeder {
	var feeders []IHistFeeder
	
	// Add K-line feeders (now directly implement IHistFeeder)
	for _, klineFeeder := range p.klineFeeders {
		feeders = append(feeders, klineFeeder)
	}
	
	// Add trade feeders (already implement IHistFeeder)
	for _, tradeFeeder := range p.tradeFeeders {
		feeders = append(feeders, tradeFeeder)
	}
	
	// Add aggregated trade feeders
	for _, aggTradeFeeder := range p.aggTradeFeeders {
		feeders = append(feeders, aggTradeFeeder)
	}
	
	return feeders
}

// LoopMain runs the main event loop
func (p *HistProviderV2) LoopMain() *errs.Error {
	if len(p.klineFeeders) == 0 && len(p.tradeFeeders) == 0 && len(p.aggTradeFeeders) == 0 {
		return errs.NewMsg(core.ErrBadConfig, "no feeders configured")
	}
	
	makeFeeders := func() []IHistFeeder {
		return p.MakeFeeders()
	}
	
	if p.showLog {
		log.Info("running historical data loop with new Batch interface")
	}
	
	// Run the event loop
	var prgBar *utils.PrgBar
	if p.pBar != nil {
		// Create a simple progress bar from staged progress
		totalMS := p.getEnd() - btime.CurTimeMS
		prgBar = utils.NewPrgBar(int(totalMS/1000), "RunHistV2")
	}
	return RunHistFeedersV2(makeFeeders, p.dirtyVers, prgBar)
}

// Terminate signals the provider to stop
func (p *HistProviderV2) Terminate() {
	p.dirtyVers <- -1
}

// SetDirty signals that feeders need to be refreshed
func (p *HistProviderV2) SetDirty() {
	select {
	case p.dirtyVers <- 1:
	default:
	}
}