package main

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

func main() {
	// 测试 HTTP REST API
	urls := []string{
		"https://fapi.binance.com/fapi/v1/ping",
		"https://fapi.binance.com/fapi/v1/ticker/price?symbol=BTCUSDT",
		"https://fapi.binance.com/fapi/v1/ticker/24hr",
	}

	client := &http.Client{Timeout: 10 * time.Second}

	for _, url := range urls {
		fmt.Printf("\n=== GET %s ===\n", url)
		resp, err := client.Get(url)
		if err != nil {
			fmt.Printf("ERROR: %v\n", err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("Status: %d\nBody: %s\n", resp.StatusCode, string(body)[:min(500, len(body))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
