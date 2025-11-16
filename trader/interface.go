package trader

import "time"

// Trader 交易器统一接口
// 支持多个交易平台（币安、Hyperliquid等）
type Trader interface {
	// GetBalance 获取账户余额
	GetBalance() (map[string]interface{}, error)

	// GetPositions 获取所有持仓
	GetPositions() ([]map[string]interface{}, error)

	// OpenLong 开多仓
	OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error)

	// OpenShort 开空仓
	OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error)

	// OpenLongLimit 开多（限价）
	OpenLongLimit(symbol string, quantity float64, leverage int, limitPrice float64) (map[string]interface{}, error)
	// OpenShortLimit 开空（限价）
	OpenShortLimit(symbol string, quantity float64, leverage int, limitPrice float64) (map[string]interface{}, error)

	// CloseLong 平多仓（quantity=0表示全部平仓）
	CloseLong(symbol string, quantity float64) (map[string]interface{}, error)

	// CloseShort 平空仓（quantity=0表示全部平仓）
	CloseShort(symbol string, quantity float64) (map[string]interface{}, error)

	// SetLeverage 设置杠杆
	SetLeverage(symbol string, leverage int) error

	// SetMarginMode 设置仓位模式 (true=全仓, false=逐仓)
	SetMarginMode(symbol string, isCrossMargin bool) error

	// GetMarketPrice 获取市场价格
	GetMarketPrice(symbol string) (float64, error)

	// SetStopLoss 设置止损单
	SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error

	// SetTakeProfit 设置止盈单
	SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error

	// CancelStopLossOrders 仅取消止损单（修复 BUG：调整止损时不删除止盈）
	CancelStopLossOrders(symbol string) error

	// CancelTakeProfitOrders 仅取消止盈单（修复 BUG：调整止盈时不删除止损）
	CancelTakeProfitOrders(symbol string) error

	// CancelAllOrders 取消该币种的所有挂单
	CancelAllOrders(symbol string) error

    // CancelStopOrders 取消该币种的止盈/止损单（用于调整止盈止损位置）
    CancelStopOrders(symbol string) error

    // FormatQuantity 格式化数量到正确的精度
    FormatQuantity(symbol string, quantity float64) (string, error)

    // GetStopTakePrices 查询当前挂着的止损/止盈价格（若存在）。
    // positionSide: "LONG" 或 "SHORT"（某些交易所可能不区分，返回符号维度的价格）
    // 返回：stopLoss, takeProfit（不存在时为0）
    GetStopTakePrices(symbol string, positionSide string) (float64, float64, error)

    // ListOpenOrders 列出未完成订单（用于限价生命周期）
    ListOpenOrders(symbol string) ([]map[string]interface{}, error)
    // CancelOrder 取消指定订单
    CancelOrder(symbol string, orderId int64) error
}

// StdUMTrade 标准化的交易填充（统一账户/合约）
// 用于基于交易所数据对账与绩效计算
type StdUMTrade struct {
    Symbol          string    // 交易对，如 BNBUSDT
    OrderID         int64     // 订单ID
    Side            string    // BUY / SELL
    Price           float64   // 成交价
    Qty             float64   // 成交量（合约张数/币）
    RealizedPnl     float64   // 该笔成交实现盈亏（USDT）
    Commission      float64   // 佣金（以 CommissionAsset 计价）
    CommissionAsset string    // 佣金资产，如 USDT/BNB
    Time            time.Time // 成交时间
    PositionSide    string    // LONG / SHORT
    Maker           bool      // 是否挂单方
}
