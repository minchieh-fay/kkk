package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/adshao/go-binance/v2/futures"
)

// symbolTradingRules 保存单个合约下单时必须遵守的数量和价格规则。
type symbolTradingRules struct {
	quantityStep float64
	quantityMin  float64
	priceTick    float64
	quantityPrec int
	pricePrec    int
}

// helpLoadSymbolTradingRules 从 Binance 读取所有合约的下单精度规则。
func helpLoadSymbolTradingRules(client *futures.Client) (map[string]symbolTradingRules, error) {
	// step.1 获取交易所公布的合约过滤器
	info, err := client.NewExchangeInfoService().Do(context.Background())
	if err != nil {
		return nil, err
	}

	// step.2 提取数量步长、最小数量和价格最小变动单位
	rules := make(map[string]symbolTradingRules, len(info.Symbols))
	for _, symbol := range info.Symbols {
		if symbol.QuoteAsset != "USDT" || symbol.ContractType != futures.ContractTypePerpetual ||
			symbol.Status != "TRADING" {
			continue
		}
		rule := symbolTradingRules{}
		for _, filter := range symbol.Filters {
			filterType, _ := filter["filterType"].(string)
			switch filterType {
			case "LOT_SIZE":
				// 读取数量步长，确保下单数量符合 LOT_SIZE 规则。
				rule.quantityStep = helpFilterFloat(filter, "stepSize")
				// 读取最小数量，提前拦截交易所必然拒绝的订单。
				rule.quantityMin = helpFilterFloat(filter, "minQty")
			case "PRICE_FILTER":
				// 读取价格最小变动单位，确保止盈止损价格可提交。
				rule.priceTick = helpFilterFloat(filter, "tickSize")
			}
		}
		if rule.quantityStep <= 0 || rule.priceTick <= 0 {
			continue
		}
		// 计算数量步长对应的小数位数，用于生成无多余精度的字符串。
		rule.quantityPrec = helpDecimalPlaces(rule.quantityStep)
		// 计算价格最小单位对应的小数位数，用于生成触发价格字符串。
		rule.pricePrec = helpDecimalPlaces(rule.priceTick)
		rules[symbol.Symbol] = rule
	}
	return rules, nil
}

// helpReadKeys 按参考工程的两行格式从外部文件读取 API key 和 secret。
func helpReadKeys(path string) (string, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("读取密钥文件: %w", err)
	}
	keys := strings.Fields(string(data))
	if len(keys) != 2 {
		return "", "", fmt.Errorf("密钥文件必须包含 API key 和 secret 两行")
	}
	return keys[0], keys[1], nil
}

// helpEnsureHedgeMode 确认双向持仓，账户有持仓或挂单而无法切换时停止启动。
func helpEnsureHedgeMode(client *futures.Client) error {
	mode, err := client.NewGetPositionModeService().Do(context.Background())
	if err != nil {
		return fmt.Errorf("读取持仓模式: %w", err)
	}
	if mode.DualSidePosition {
		return nil
	}
	if err := client.NewChangePositionModeService().DualSide(true).Do(context.Background()); err != nil {
		return fmt.Errorf("启用双向持仓失败，请检查账户现有持仓和挂单: %w", err)
	}
	return nil
}

// helpPositionSide 将开仓买卖方向转换成双向持仓标识。
func helpPositionSide(side futures.SideType) futures.PositionSideType {
	if side == futures.SideTypeBuy {
		return futures.PositionSideTypeLong
	}
	return futures.PositionSideTypeShort
}

// helpReadDingToken 从独立文件读取钉钉 token，空路径表示禁用通知。
func helpReadDingToken(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取钉钉凭证文件: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" || len(strings.Fields(token)) != 1 {
		return "", fmt.Errorf("钉钉凭证文件必须包含一个非空 token")
	}
	return token, nil
}

// helpFilterFloat 从交易所过滤器中读取一个数字字段。
func helpFilterFloat(filter map[string]any, key string) float64 {
	value, ok := filter[key].(string)
	if !ok {
		return 0
	}
	parsed, _ := strconv.ParseFloat(value, 64)
	return parsed
}

// helpDecimalPlaces 计算步长或价格最小单位需要保留的小数位数。
func helpDecimalPlaces(value float64) int {
	text := strconv.FormatFloat(value, 'f', -1, 64)
	point := strings.IndexByte(text, '.')
	if point < 0 {
		return 0
	}
	return len(text) - point - 1
}

// helpFormatQuantity 按合约数量步长向下取整，生成可提交的数量字符串。
func helpFormatQuantity(symbol string, quantity float64, rules map[string]symbolTradingRules) (string, error) {
	// step.1 找到合约数量规则并按步长向下取整
	rule, ok := rules[symbol]
	if !ok || rule.quantityStep <= 0 {
		return "", fmt.Errorf("未找到 %s 的数量精度规则", symbol)
	}
	units := math.Floor(quantity/rule.quantityStep + 1e-9)
	adjusted := units * rule.quantityStep
	if adjusted < rule.quantityMin || adjusted <= 0 {
		return "", fmt.Errorf("%s 数量 %.12g 小于最小下单数量 %.12g", symbol, adjusted, rule.quantityMin)
	}

	// step.2 用规则要求的位数输出，避免把无效小数位提交给交易所
	return strconv.FormatFloat(adjusted, 'f', rule.quantityPrec, 64), nil
}

// helpFormatPrice 按合约价格最小变动单位向下取整并格式化触发价格。
func helpFormatPrice(symbol string, price float64, rules map[string]symbolTradingRules) (string, error) {
	rule, ok := rules[symbol]
	if !ok || rule.priceTick <= 0 {
		return "", fmt.Errorf("未找到 %s 的价格精度规则", symbol)
	}
	units := math.Floor(price/rule.priceTick + 1e-9)
	adjusted := units * rule.priceTick
	if adjusted <= 0 {
		return "", fmt.Errorf("%s 触发价格无效: %.12g", symbol, price)
	}
	return strconv.FormatFloat(adjusted, 'f', rule.pricePrec, 64), nil
}
