package main

import (
	"fmt"
	"os"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	// 直接连 Binance WS，不用 go-binance 库
	// 测试 miniTicker@arr
	url := "wss://fstream.binance.com/ws/!miniTicker@arr"
	fmt.Printf("Connecting to %s\n", url)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		fmt.Printf("Dial error: %v\n", err)
		return
	}
	defer conn.Close()
	fmt.Println("Connected!")

	// 读 10 秒
	deadline := time.Now().Add(10 * time.Second)
	conn.SetReadDeadline(deadline)

	count := 0
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if e, ok := err.(interface{ Timeout() bool }); ok && e.Timeout() {
				break
			}
			fmt.Printf("Read error: %v\n", err)
			return
		}
		count++
		if count <= 3 {
			fmt.Printf("[#%d] len=%d data=%.200s...\n", count, len(msg), string(msg))
		} else {
			fmt.Printf("[#%d] len=%d\n", count, len(msg))
		}
	}

	fmt.Printf("\nDone: received %d messages in 10s\n", count)

	// 再测试 combined kline
	fmt.Println("\n=== Test combined kline (BTC+ETH 1h) ===")
	url2 := "wss://fstream.binance.com/stream?streams=btcusdt@kline_1h/ethusdt@kline_1h/bnbusdt@kline_1h"
	fmt.Printf("Connecting to %s\n", url2)

	conn2, _, err := dialer.Dial(url2, nil)
	if err != nil {
		fmt.Printf("Dial error: %v\n", err)
		return
	}
	defer conn2.Close()
	fmt.Println("Connected!")

	conn2.SetReadDeadline(time.Now().Add(10 * time.Second))

	count2 := 0
	for {
		_, msg, err := conn2.ReadMessage()
		if err != nil {
			if e, ok := err.(interface{ Timeout() bool }); ok && e.Timeout() {
				break
			}
			fmt.Printf("Read error: %v\n", err)
			return
		}
		count2++
		if count2 <= 3 {
			fmt.Printf("[#%d] len=%d data=%.300s...\n", count2, len(msg), string(msg))
		}
	}

	fmt.Printf("\nDone: received %d messages in 10s\n", count2)
	_ = os.Getenv("dummy") // suppress unused import
}
