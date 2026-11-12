package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/adshao/go-binance/v2"
	"github.com/adshao/go-binance/v2/futures"
)

func main() {
	apiKey, secretKey, tradeAmount := loadTestEnv(".env")
	if apiKey == "" || secretKey == "" {
		fmt.Println("API keys not configured in .env")
		os.Exit(1)
	}
	if tradeAmount <= 0 {
		tradeAmount = 12
	}

	client := binance.NewFuturesClient(apiKey, secretKey)

	// 1. Test account connectivity
	info, err := client.NewGetAccountService().Do(context.Background())
	if err != nil {
		fmt.Printf("Account info failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[OK] Account connected! Total wallet balance: %s USDT\n", info.TotalWalletBalance)

	// 2. Test: place a market buy order on BTCUSDT
	symbol := "XRPUSDT"
	amount := tradeAmount

	prices, err := client.NewListPricesService().Symbol(symbol).Do(context.Background())
	if err != nil {
		fmt.Printf("Failed to get price for %s: %v\n", symbol, err)
		os.Exit(1)
	}

	priceFloat, _ := strconv.ParseFloat(prices[0].Price, 64)
	qty := amount / priceFloat

	// Get lot size precision from exchange info
	exchangeInfo, err := client.NewExchangeInfoService().Do(context.Background())
	if err != nil {
		fmt.Printf("Failed to get exchange info: %v\n", err)
		os.Exit(1)
	}
	var stepSize float64
	for _, sym := range exchangeInfo.Symbols {
		if sym.Symbol == symbol {
			for _, filter := range sym.Filters {
				if filter["filterType"] == "LOT_SIZE" {
					stepSize, _ = strconv.ParseFloat(filter["stepSize"].(string), 64)
					break
				}
			}
			break
		}
	}
	if stepSize == 0 {
		stepSize = 1
	}
	// Round qty to step size precision
	qty = float64(int(qty/stepSize)) * stepSize
	qtyStr := strconv.FormatFloat(qty, 'f', -1, 64)

	fmt.Printf("[TRADE] Placing BUY %s: qty=%s (~%.2f USDT @ %.2f)\n", symbol, qtyStr, amount, priceFloat)

	order, err := client.NewCreateOrderService().
		Symbol(symbol).
		Side(futures.SideTypeBuy).
		Type(futures.OrderTypeMarket).
		Quantity(qtyStr).
		Do(context.Background())
	if err != nil {
		fmt.Printf("[FAIL] Order failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[OK] Order success! OrderID=%d, Status=%s, ExecutedQty=%s\n",
		order.OrderID, order.Status, order.ExecutedQuantity)

	// 3. Immediately close the position
	closeQty := order.ExecutedQuantity
	closeOrder, err := client.NewCreateOrderService().
		Symbol(symbol).
		Side(futures.SideTypeSell).
		Type(futures.OrderTypeMarket).
		Quantity(closeQty).
		Do(context.Background())
	if err != nil {
		fmt.Printf("[WARN] Close position failed: %v\n", err)
		fmt.Println("[WARN] Open position still exists, please close manually!")
		os.Exit(1)
	}
	fmt.Printf("[OK] Position closed! CloseOrderID=%d, Status=%s\n",
		closeOrder.OrderID, closeOrder.Status)

	fmt.Println("[DONE] Trade API test completed successfully!")
}

// loadTestEnv reads only the needed keys from .env
func loadTestEnv(path string) (apiKey, secretKey string, tradeAmount float64) {
	f, err := os.Open(path)
	if err != nil {
		fmt.Printf("Cannot open %s: %v\n", path, err)
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "BINANCE_API_KEY":
			apiKey = val
		case "BINANCE_SECRET_KEY":
			secretKey = val
		case "TRADE_AMOUNT_USDT":
			tradeAmount, _ = strconv.ParseFloat(val, 64)
		}
	}
	return
}
