package market

import (
	"strconv"
	"strings"
	"sync"
	"time"
)

// 缓存交易所规则，避免频繁请求
var (
	exInfoCache     *ExchangeInfo
	exInfoCacheTime time.Time
	exInfoMu        sync.RWMutex
	exInfoTTL       = 30 * time.Minute
)

func getExchangeInfoCached() (*ExchangeInfo, error) {
	exInfoMu.RLock()
	if exInfoCache != nil && time.Since(exInfoCacheTime) < exInfoTTL {
		defer exInfoMu.RUnlock()
		return exInfoCache, nil
	}
	exInfoMu.RUnlock()

	exInfoMu.Lock()
	defer exInfoMu.Unlock()
	// double check
	if exInfoCache != nil && time.Since(exInfoCacheTime) < exInfoTTL {
		return exInfoCache, nil
	}
	api := NewAPIClient()
	info, err := api.GetExchangeInfo()
	if err != nil {
		return nil, err
	}
	exInfoCache = info
	exInfoCacheTime = time.Now()
	return info, nil
}

// GetSymbolExchangeRules 提取某个 symbol 的步长与最小名义（若能解析）
func GetSymbolExchangeRules(symbol string) (*ExchangeRules, error) {
	info, err := getExchangeInfoCached()
	if err != nil {
		return nil, err
	}
	s := strings.ToUpper(symbol)
	for _, si := range info.Symbols {
		if strings.ToUpper(si.Symbol) != s {
			continue
		}
		r := &ExchangeRules{}
		for _, f := range si.Filters {
			ft, _ := f["filterType"].(string)
			switch ft {
			case "MARKET_LOT_SIZE":
				if ss, ok := f["stepSize"].(string); ok {
					if v, e := strconv.ParseFloat(ss, 64); e == nil {
						r.StepSizeMarket = v
					}
				}
			case "LOT_SIZE":
				if ss, ok := f["stepSize"].(string); ok {
					if v, e := strconv.ParseFloat(ss, 64); e == nil {
						r.StepSizeLot = v
					}
				}
			case "NOTIONAL", "MIN_NOTIONAL":
				if mn, ok := f["minNotional"].(string); ok {
					if v, e := strconv.ParseFloat(mn, 64); e == nil {
						r.MinNotional = v
					}
				}
				if n, ok := f["notional"].(string); ok && r.MinNotional == 0 {
					if v, e := strconv.ParseFloat(n, 64); e == nil {
						r.MinNotional = v
					}
				}
			}
		}
		return r, nil
	}
	return &ExchangeRules{}, nil
}
