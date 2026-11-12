package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
)

// 模拟你的 PriceHistory 结构
var prices = make(map[string]float64)

func main() {
	proxyURL := os.Getenv("HTTPS_PROXY")
	_ = proxyURL

	// 使用 spot 的 miniTicker@arr，这是能收到数据的
	url := "wss://stream.binance.com:9443/ws/!miniTicker@arr"
	fmt.Printf("Connecting to %s\n", url)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	c, _, err := dialer.Dial(url, nil)
	if err != nil {
		fmt.Printf("Dial error: %v\n", err)
		return
	}
	defer c.Close()
	fmt.Println("Connected!")

	go func() {
		for {
			_, msg, err := c.ReadMessage()
			if err != nil {
				fmt.Printf("Read error: %v\n", err)
				return
			}

			var tickers []struct {
				Event string `json:"e"`
				Symbol string `json:"s"`
				Close string `json:"c"`
			}
			json.Unmarshal(msg, &tickers)

			for _, t := range tickers {
				if t.Event != "24hrMiniTicker" {
					continue
				}
				price, _ := strconv.ParseFloat(t.Close, 64)
				if price > 0 {
					prices[t.Symbol] = price
				}
			}
		}
	}()

	// 每秒打印 BTCUSDT 和 ETHUSDT 的价格
	for i := 0; i < 15; i++ {
		time.Sleep(1 * time.Second)
		fmt.Printf("[%s] BTCUSDT=%.2f ETHUSDT=%.2f\n",
			time.Now().Format("15:04:05"),
			prices["BTCUSDT"], prices["ETHUSDT"])
	}
}
