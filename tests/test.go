package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

func main() {
	proxyURL := os.Getenv("HTTPS_PROXY")
	if proxyURL != "" {
		fmt.Printf("Using WS proxy: %s\n", proxyURL)
		futures.SetWsProxyUrl(proxyURL)
	}

	// 测试1: 只订阅 3 个热门币
	fmt.Println("=== Test 1: 3 symbols (BTC/ETH/BNB) 1h kline ===")
	testKline(map[string]string{
		"BTCUSDT": "1h",
		"ETHUSDT": "1h",
		"BNBUSDT": "1h",
	})
	time.Sleep(10 * time.Second)

	// 测试2: miniTicker
	fmt.Println("\n=== Test 2: miniTicker@arr ===")
	testMiniTicker()
	time.Sleep(10 * time.Second)

	fmt.Println("\n=== DONE ===")
}

func testKline(input map[string]string) {
	count := 0
	wsHandler := func(event *futures.WsKlineEvent) {
		count++
		raw, _ := json.Marshal(event)
		fmt.Printf("[KLINE #%d] %s symbol=%s event=%q close=%s\n",
			count, time.Now().Format("15:04:05"), event.Symbol, event.Event, event.Kline.Close)
		if count <= 2 {
			fmt.Printf("  full raw: %s\n", string(raw))
		}
	}
	errHandler := func(err error) {
		fmt.Printf("[KLINE ERROR] %v\n", err)
	}

	_, _, err := futures.WsCombinedKlineServe(input, wsHandler, errHandler)
	if err != nil {
		fmt.Printf("[KLINE] connect error: %v\n", err)
		return
	}
	fmt.Println("[KLINE] connected!")
}

func testMiniTicker() {
	count := 0
	wsHandler := func(event futures.WsAllMiniMarketTickerEvent) {
		count++
		fmt.Printf("[TICKER #%d] %s got %d tickers\n", count, time.Now().Format("15:04:05"), len(event))
		for _, t := range event {
			if t.Symbol == "BTCUSDT" || t.Symbol == "ETHUSDT" {
				fmt.Printf("  %s event=%q close=%s\n", t.Symbol, t.Event, t.ClosePrice)
			}
		}
	}
	errHandler := func(err error) {
		fmt.Printf("[TICKER ERROR] %v\n", err)
	}

	_, _, err := futures.WsAllMiniMarketTickerServe(wsHandler, errHandler)
	if err != nil {
		fmt.Printf("[TICKER] connect error: %v\n", err)
		return
	}
	fmt.Println("[TICKER] connected!")
}
