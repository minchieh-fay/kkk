package main

import (
	"fmt"
	"time"

	"github.com/gorilla/websocket"
)

func testWS(url string, label string, duration time.Duration) {
	fmt.Printf("=== %s: %s ===\n", label, url)

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

	c.SetReadDeadline(time.Now().Add(duration))
	count := 0
	start := time.Now()
	for {
		_, msg, err := c.ReadMessage()
		if err != nil {
			if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
				break
			}
			fmt.Printf("Read error after %s: %v\n", time.Since(start), err)
			return
		}
		count++
		elapsed := time.Since(start)
		if count <= 2 {
			preview := string(msg)
			if len(preview) > 200 {
				preview = preview[:200] + "..."
			}
			fmt.Printf("  [#%d] at %v len=%d: %s\n", count, elapsed, len(msg), preview)
		} else if count%10 == 0 {
			fmt.Printf("  ... #%d at %v\n", count, elapsed)
		}
	}
	fmt.Printf("Total: %d messages in %v\n\n", count, duration)
}

func main() {
	tests := []struct {
		url      string
		label    string
		duration time.Duration
	}{
		{"wss://fstream.binance.com/ws/!miniTicker@arr", "miniTicker@arr (futures)", 5 * time.Second},
		{"wss://fstream.binance.com/ws/btcusdt@aggTrade", "aggTrade BTCUSDT (futures)", 5 * time.Second},
		{"wss://fstream.binance.com/ws/btcusdt@trade", "trade BTCUSDT (futures)", 5 * time.Second},
		{"wss://fstream.binance.com/ws/btcusdt@markPrice", "markPrice BTCUSDT (futures)", 5 * time.Second},
		{"wss://fstream.binance.com/stream?streams=btcusdt@kline_1h", "combined kline 1h (futures)", 5 * time.Second},
		{"wss://stream.binance.com:9443/ws/!miniTicker@arr", "miniTicker@arr (spot)", 5 * time.Second},
		{"wss://stream.binance.com:9443/ws/btcusdt@trade", "trade BTCUSDT (spot)", 5 * time.Second},
	}

	for _, t := range tests {
		testWS(t.url, t.label, t.duration)
	}
}
