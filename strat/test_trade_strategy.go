package strat

import (
	"github.com/banbox/banbot/core"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
)

// TestTradeStrategy is a sample strategy that uses trade data
func TestTradeStrategy() *TradeStrat {
	return &TradeStrat{
		Name:      "TradeTestStrategy",
		WarmupNum: 20,
		WsSubs: map[string]string{
			core.WsSubTrade: "_cur_", // Subscribe to trade data for current pair
		},
		OnStartUp:  onTradeStartUp,
		OnBar:      onTradeBar,
		OnWsTrades: onWsTrades,
	}
}

func onTradeStartUp(s *StratJob) {
	log.Info("TradeTestStrategy started", 
		zap.String("pair", s.Symbol.Symbol),
		zap.String("timeframe", s.TimeFrame))
}

func onTradeBar(s *StratJob) {
	// Process K-line data
	close := s.Env.Close.Get(0)
	vol := s.Env.Volume.Get(0)
	
	log.Debug("Bar processed",
		zap.String("pair", s.Symbol.Symbol),
		zap.Float64("close", close),
		zap.Float64("volume", vol))
}

func onWsTrades(s *StratJob, pair string, trades []*banexg.Trade) {
	if len(trades) == 0 {
		return
	}
	
	// Calculate buy/sell pressure
	var buyVolume, sellVolume float64
	var buyCount, sellCount int
	
	for _, trade := range trades {
		if trade.Side == banexg.OdSideBuy {
			buyVolume += trade.Amount
			buyCount++
		} else {
			sellVolume += trade.Amount
			sellCount++
		}
	}
	
	totalVolume := buyVolume + sellVolume
	buyPressure := float64(0)
	if totalVolume > 0 {
		buyPressure = buyVolume / totalVolume * 100
	}
	
	log.Info("Trade data received",
		zap.String("pair", pair),
		zap.Int("trades", len(trades)),
		zap.Int("buys", buyCount),
		zap.Int("sells", sellCount),
		zap.Float64("buyVol", buyVolume),
		zap.Float64("sellVol", sellVolume),
		zap.Float64("buyPressure", buyPressure))
	
	// Example: Generate entry signal based on buy pressure
	if buyPressure > 70 && len(s.LongOrders) == 0 {
		// Strong buy pressure, consider entering long
		enter := &EnterReq{
			Tag:       "HighBuyPressure",
			Short:     false, // Long position
			Amount:    0,     // Will use default stake amount
			StratName: s.Strat.Name,
		}
		s.Entrys = append(s.Entrys, enter)
		log.Info("Entry signal generated",
			zap.String("pair", pair),
			zap.Float64("buyPressure", buyPressure))
	}
}