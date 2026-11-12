package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/adshao/go-binance/v2"
	"github.com/adshao/go-binance/v2/futures"
)

// cfg 保存已校验的运行配置。
var cfg Config

// dingToken 仅在内存中保存从独立文件读取的机器人凭证。
var dingToken string

// 每个币的价格历史：每小时存一个快照
type PriceHistory struct {
	hourly  []float64 // 按时间顺序存放，最多保留 historyHours 个
	current float64   // 当前最新价格
}

// Update 更新最新价格，并按需维护小时快照窗口。
func (p *PriceHistory) Update(price float64, saveHourly bool) {
	p.current = price

	if saveHourly {
		p.hourly = append(p.hourly, price)
		if len(p.hourly) > cfg.HistoryHours {
			p.hourly = p.hourly[len(p.hourly)-cfg.HistoryHours:]
		}
	}
}

// Avg 计算历史快照的平均价格。
func (p *PriceHistory) Avg() float64 {
	if len(p.hourly) == 0 {
		return 0
	}
	var sum float64
	for _, v := range p.hourly {
		sum += v
	}
	return sum / float64(len(p.hourly))
}

// Current 返回最新行情价格。
func (p *PriceHistory) Current() float64 {
	return p.current
}

// 全局状态
var (
	histories   = make(map[string]*PriceHistory)
	symbolRules map[string]symbolTradingRules

	lastNotifyTime   = make(map[string]time.Time)
	lastSnapshotTime = time.Now() // 上次存小时快照的时间
)

// main 初始化策略并全天执行行情扫描和交易。
func main() {
	runtime.GOMAXPROCS(1)

	// step.1 读取配置和外部密钥，并确认账户已启用双向持仓
	var err error
	cfg, err = loadConfig("config.json")
	if err != nil {
		panic(err)
	}
	// 从独立的两行密钥文件读取凭证，避免把交易密钥放在配置中。
	apiKey, secretKey, err := helpReadKeys(cfg.KeyFile)
	if err != nil {
		panic(err)
	}
	// 从独立凭证文件读取钉钉 token，空路径表示关闭通知。
	dingToken, err = helpReadDingToken(cfg.DingKeyFile)
	if err != nil {
		panic(err)
	}
	client := binance.NewFuturesClient(apiKey, secretKey)
	// 启动时校验双向持仓；仅在账户允许时切换，失败则停止启动。
	if err := helpEnsureHedgeMode(client); err != nil {
		panic(err)
	}
	// 加载所有合约的数量步长和价格最小变动单位，供下单格式化使用。
	symbolRules, err = helpLoadSymbolTradingRules(client)
	if err != nil {
		panic(fmt.Errorf("加载合约精度规则失败: %w", err))
	}

	// step.2 初始化可交易合约及历史行情
	symbols, err := initPrices(client)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Found %d USDT symbols\n", len(symbols))

	// 拉取历史K线，填充前 historyHours 小时的数据
	fillHistory(client, symbols)

	fmt.Printf("Start monitoring: low=%.2f high=%.2f interval=%v cooldown=%v\n",
		cfg.LowThreshold, cfg.HighThreshold, (time.Duration(cfg.CheckIntervalMinutes) * time.Minute), (time.Duration(cfg.NotifyCooldownHours) * time.Hour))

	// step.3 全天刷新行情并检测交易信号
	ticker := time.NewTicker((time.Duration(cfg.CheckIntervalMinutes) * time.Minute))
	defer ticker.Stop()

	for range ticker.C {
		refreshPrices(client)
		checkAlerts(client)
	}
}

// initPrices 获取所有 USDT 交易对并初始化
func initPrices(client *futures.Client) ([]string, error) {
	// step.1 获取全市场最新报价
	prices, err := client.NewListPricesService().Do(context.Background())
	if err != nil {
		return nil, err
	}

	// step.2 筛选具备下单规则的 USDT 合约并创建行情窗口
	var symbols []string
	for _, p := range prices {
		if len(p.Symbol) < 4 {
			continue
		}
		if _, ok := symbolRules[p.Symbol]; !ok {
			continue
		}
		if p.Symbol[len(p.Symbol)-4:] != "USDT" {
			continue
		}
		price, _ := strconv.ParseFloat(p.Price, 64)
		if price > 1000000 {
			continue
		}
		symbols = append(symbols, p.Symbol)

		h := &PriceHistory{}
		h.current = price
		h.hourly = append(h.hourly, price)

		histories[p.Symbol] = h
	}
	return symbols, nil
}

// fillHistory 用K线数据填充历史均价（首次启动），串行+sleep避免触发API限频
func fillHistory(client *futures.Client, symbols []string) {
	// step.1 限速获取各合约的小时 K 线
	fmt.Printf("Filling history for %d symbols...\n", len(symbols))

	for i, symbol := range symbols {
		if i > 0 {
			time.Sleep(100 * time.Millisecond)
		}

		klines, err := client.NewKlinesService().
			Symbol(symbol).
			Interval("1h").
			Limit(cfg.HistoryHours).
			Do(context.Background())
		if err != nil {
			fmt.Printf("Error fetching klines for %s: %v\n", symbol, err)
			continue
		}

		h := histories[symbol]
		if h == nil {
			continue
		}

		// step.2 将 K 线收盘价格写入历史均价窗口
		h.hourly = nil
		for _, k := range klines {
			close, _ := strconv.ParseFloat(k.Close, 64)
			h.hourly = append(h.hourly, close)
		}
		if len(h.hourly) > cfg.HistoryHours {
			h.hourly = h.hourly[len(h.hourly)-cfg.HistoryHours:]
		}
	}
	fmt.Println("History fill done")
}

// refreshPrices 每分钟调用一次，刷新所有币的当前价格
func refreshPrices(client *futures.Client) {
	// step.1 获取全市场当前价格
	prices, err := client.NewListPricesService().Do(context.Background())
	if err != nil {
		fmt.Printf("Error refreshing prices: %v\n", err)
		return
	}

	// step.2 更新已监控合约的价格，并每小时保存快照
	saveHourly := time.Since(lastSnapshotTime) >= time.Hour
	if saveHourly {
		lastSnapshotTime = time.Now()
	}

	for _, p := range prices {
		h := histories[p.Symbol]
		if h == nil {
			continue
		}
		price, _ := strconv.ParseFloat(p.Price, 64)
		if price > 0 {
			h.Update(price, saveHourly)
		}
	}
}

// checkAlerts 检测所有币是否触发报警
func checkAlerts(client *futures.Client) {
	// step.1 汇总行情偏离程度并全天检查交易信号
	now := time.Now()
	cst := now.In(time.FixedZone("CST", 8*3600))

	var alerts []string

	var maxHighSymbol, maxLowSymbol string
	var maxHighRatio, maxLowRatio float64
	var maxHighCurrent, maxHighAvg, maxLowCurrent, maxLowAvg float64

	for symbol, h := range histories {
		current := h.Current()
		avg := h.Avg()
		if current <= 0 || avg <= 0 {
			continue
		}

		ratio := current / avg

		if maxHighSymbol == "" || ratio > maxHighRatio {
			maxHighSymbol = symbol
			maxHighRatio = ratio
			maxHighCurrent = current
			maxHighAvg = avg
		}
		if maxLowSymbol == "" || ratio < maxLowRatio {
			maxLowSymbol = symbol
			maxLowRatio = ratio
			maxLowCurrent = current
			maxLowAvg = avg
		}

		if ratio <= cfg.LowThreshold || ratio >= cfg.HighThreshold {
			if !canNotify(symbol, now) {
				continue
			}

			isLong := ratio <= cfg.LowThreshold
			direction := "HIGH(做空 SHORT)"
			side := futures.SideTypeSell
			if isLong {
				direction = "LOW(做多 LONG)"
				side = futures.SideTypeBuy
			}

			alertLine := fmt.Sprintf("%s %s: current=%.4f, history_avg=%.4f, ratio=%.2f",
				symbol, direction, current, avg, ratio)

			// step.2 按低位做多、高位做空的方向下单并记录信号冷却
			if err := executeTrade(client, symbol, side, current); err != nil {
				alertLine += fmt.Sprintf(" [交易失败: %v]", err)
			} else {
				alertLine += " [已下单]"
			}

			alerts = append(alerts, alertLine)
			markNotified(symbol, now)
		}
	}

	fmt.Printf("[%s] TOP HIGH: %s ratio=%.4f current=%.4f avg=%.4f | TOP LOW: %s ratio=%.4f current=%.4f avg=%.4f\n",
		cst.Format("15:04:05"),
		maxHighSymbol, maxHighRatio, maxHighCurrent, maxHighAvg,
		maxLowSymbol, maxLowRatio, maxLowCurrent, maxLowAvg)

	// step.3 输出交易结果；钉钉通知内部独立执行夜间静音
	if len(alerts) > 0 {
		msg := fmt.Sprintf("Price Alert!\n%s", alerts)
		fmt.Printf("[%s] %s\n", cst.Format("15:04:05"), msg)
		sendDingTalk(msg)
	}
}

// isQuietHour 检查北京时间是否处于钉钉静音时段，不影响交易。
func isQuietHour(hour int) bool {
	start := cfg.QuietHourStart
	end := cfg.QuietHourEnd
	if start > end {
		return hour >= start || hour < end
	}
	return hour >= start && hour < end
}

// executeTrade 执行市价单开仓，同时挂止损止盈条件单
func executeTrade(client *futures.Client, symbol string, side futures.SideType, price float64) error {
	// step.1 根据交易金额和当前价格计算名义下单数量
	qty := cfg.TradeAmountUSDT / price
	// 按交易所公布的 LOT_SIZE 步长取整，避免数量精度错误。
	qtyStr, err := helpFormatQuantity(symbol, qty, symbolRules)
	if err != nil {
		return err
	}

	// 根据开仓买卖方向选择 LONG 或 SHORT，避免双向模式下方向不匹配。
	positionSide := helpPositionSide(side)
	// step.2 提交符合该合约数量精度的市价开仓单
	_, err = client.NewCreateOrderService().
		Symbol(symbol).
		Side(side).
		PositionSide(positionSide).
		Type(futures.OrderTypeMarket).
		Quantity(qtyStr).
		Do(context.Background())
	if err != nil {
		return err
	}

	fmt.Printf("[TRADE] %s %s %s @ %.4f\n", symbol, side, qtyStr, price)

	// step.3 为该方向仓位创建止损止盈保护
	if cfg.EnableTpSl {
		if err := placeTpSlOrders(client, symbol, side, price, qtyStr); err != nil {
			fmt.Printf("[TP/SL] %s 挂单失败: %v\n", symbol, err)
		}
	}

	return nil
}

// placeTpSlOrders 开仓后挂止损止盈条件单
func placeTpSlOrders(client *futures.Client, symbol string, side futures.SideType, entryPrice float64, qtyStr string) error {
	// step.1 根据持仓方向计算止损和止盈触发价
	var stopPrice, tpPrice float64
	var closeSide futures.SideType

	if side == futures.SideTypeBuy {
		closeSide = futures.SideTypeSell
		stopPrice = entryPrice * (1 - cfg.StopLossPct)
		tpPrice = entryPrice * (1 + cfg.TakeProfitPct)
	} else {
		closeSide = futures.SideTypeBuy
		stopPrice = entryPrice * (1 + cfg.StopLossPct)
		tpPrice = entryPrice * (1 - cfg.TakeProfitPct)
	}

	// 按 PRICE_FILTER 的最小变动单位格式化止损触发价格。
	stopPriceStr, err := helpFormatPrice(symbol, stopPrice, symbolRules)
	if err != nil {
		return fmt.Errorf("止损价格: %v", err)
	}
	// 按 PRICE_FILTER 的最小变动单位格式化止盈触发价格。
	tpPriceStr, err := helpFormatPrice(symbol, tpPrice, symbolRules)
	if err != nil {
		return fmt.Errorf("止盈价格: %v", err)
	}

	// 保护单沿用开仓的持仓方向，反向买卖只关闭该方向仓位。
	positionSide := helpPositionSide(side)
	// step.2 提交符合价格精度的止损和止盈条件单
	_, err = client.NewCreateAlgoOrderService().
		Symbol(symbol).
		Side(closeSide).
		PositionSide(positionSide).
		Type(futures.AlgoOrderTypeStopMarket).
		Quantity(qtyStr).
		TriggerPrice(stopPriceStr).
		Do(context.Background())
	if err != nil {
		return fmt.Errorf("止损单: %v", err)
	}
	fmt.Printf("[TP/SL] %s 止损 STOP_MARKET price=%.4f\n", symbol, stopPrice)

	time.Sleep(100 * time.Millisecond)

	_, err = client.NewCreateAlgoOrderService().
		Symbol(symbol).
		Side(closeSide).
		PositionSide(positionSide).
		Type(futures.AlgoOrderTypeTakeProfitMarket).
		Quantity(qtyStr).
		TriggerPrice(tpPriceStr).
		Do(context.Background())
	if err != nil {
		return fmt.Errorf("止盈单: %v", err)
	}
	fmt.Printf("[TP/SL] %s 止盈 TAKE_PROFIT_MARKET price=%.4f\n", symbol, tpPrice)

	return nil
}

// canNotify 检查是否在冷却期内
func canNotify(symbol string, now time.Time) bool {
	last, ok := lastNotifyTime[symbol]
	if !ok {
		return true
	}
	return now.Sub(last) >= (time.Duration(cfg.NotifyCooldownHours) * time.Hour)
}

// markNotified 记录通知时间
func markNotified(symbol string, now time.Time) {
	lastNotifyTime[symbol] = now
}

// sendDingTalk 发送钉钉通知
func sendDingTalk(msg string) {
	// step.1 根据北京时间独立过滤静音时段，交易扫描不受影响
	if dingToken == "" || isQuietHour(time.Now().In(time.FixedZone("CST", 8*3600)).Hour()) {
		return
	}
	// step.2 构造文本通知并发送到钉钉机器人
	c := struct {
		Msgtype string `json:"msgtype"`
		Text    struct {
			Content string `json:"content"`
		} `json:"text"`
		At struct {
			IsAtAll bool `json:"isAtAll"`
		} `json:"at"`
	}{
		Msgtype: "text",
	}
	c.Text.Content = fmt.Sprintf("letbin:[%s] %s", time.Now().Format("15:04:05"), msg)
	c.At.IsAtAll = true

	data, _ := json.Marshal(c)
	url := fmt.Sprintf("https://oapi.dingtalk.com/robot/send?access_token=%s", dingToken)

	req, _ := http.NewRequest("POST", url, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")

	httpClient := http.Client{Timeout: 10 * time.Second}

	resp, err := httpClient.Do(req)
	if err != nil {
		// HTTP 错误可能包含带 token 的完整 URL，不把原始错误写入日志。
		fmt.Println("DingTalk send error: 请求失败，请检查网络或机器人配置")
		return
	}
	defer resp.Body.Close()
}
