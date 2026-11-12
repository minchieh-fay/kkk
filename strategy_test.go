package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

// TestSignalsTradeDuringQuietHours 验证静音时段仍下单，且高位做空低位做多及保护单方向正确。
func TestSignalsTradeDuringQuietHours(t *testing.T) {
	// step.1 设置静音窗口与两种信号，保存原有全局状态
	oldCfg, oldRules, oldHistories, oldNotify, oldToken := cfg, symbolRules, histories, lastNotifyTime, dingToken
	t.Cleanup(func() {
		cfg, symbolRules, histories, lastNotifyTime, dingToken = oldCfg, oldRules, oldHistories, oldNotify, oldToken
	})
	hour := time.Now().In(time.FixedZone("CST", 8*3600)).Hour()
	cfg = Config{LowThreshold: 0.5, HighThreshold: 2, TradeAmountUSDT: 12,
		QuietHourStart: hour, QuietHourEnd: (hour + 2) % 24,
		EnableTpSl: true, StopLossPct: 0.1, TakeProfitPct: 0.3}
	dingToken = "fake-token-must-not-send"
	histories = map[string]*PriceHistory{
		"HIGHUSDT": {hourly: []float64{0.025}, current: 0.0519},
		"LOWUSDT":  {hourly: []float64{0.2}, current: 0.0519},
	}
	lastNotifyTime = make(map[string]time.Time)
	symbolRules = map[string]symbolTradingRules{
		"HIGHUSDT": {quantityStep: 1, quantityMin: 1, priceTick: 0.00001, pricePrec: 5},
		"LOWUSDT":  {quantityStep: 1, quantityMin: 1, priceTick: 0.00001, pricePrec: 5},
	}

	// step.2 用模拟接口记录订单，不连接真实交易所或钉钉
	var orders []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		orders = append(orders, r.Form)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	client := futures.NewClient("test", "test")
	client.BaseURL = server.URL
	checkAlerts(client)

	// step.3 核对开仓及止盈止损的持仓方向、反向平仓和整数数量
	if len(orders) != 6 {
		t.Fatalf("静音时段应有两笔开仓及四笔保护单，实际 %d", len(orders))
	}
	for _, order := range orders {
		short := order.Get("symbol") == "HIGHUSDT"
		wantPosition, wantSide := "LONG", "BUY"
		if short {
			wantPosition, wantSide = "SHORT", "SELL"
		}
		if order.Get("type") != "MARKET" {
			if short {
				wantSide = "BUY"
			} else {
				wantSide = "SELL"
			}
		}
		if order.Get("type") == "STOP_MARKET" {
			wantTrigger := "0.04671"
			if short {
				wantTrigger = "0.05709"
			}
			if order.Get("triggerPrice") != wantTrigger {
				t.Errorf("10%% 止损触发价应为 %s，实际 %s", wantTrigger, order.Get("triggerPrice"))
			}
		}
		if order.Get("positionSide") != wantPosition || order.Get("side") != wantSide ||
			order.Get("quantity") != "231" || order.Has("reduceOnly") {
			t.Errorf("订单参数不正确: %v", order)
		}
	}
}

// TestConfigAndCredentialFiles 验证 JSON 配置及交易、钉钉独立凭证文件读取。
func TestConfigAndCredentialFiles(t *testing.T) {
	// step.1 用临时文件验证双凭证格式与 JSON 默认值
	dir := t.TempDir()
	key := filepath.Join(dir, "key")
	ding := filepath.Join(dir, "dingkey")
	config := filepath.Join(dir, "config.json")
	for path, content := range map[string]string{key: "api\nsecret\n", ding: "token\n", config: `{}`} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// 核对交易凭证按参考工程的两行格式读取。
	api, secret, err := helpReadKeys(key)
	if err != nil || api != "api" || secret != "secret" {
		t.Fatalf("读取交易凭证失败: %v", err)
	}
	// 核对钉钉凭证去除文件末尾换行后可用。
	token, err := helpReadDingToken(ding)
	if err != nil || token != "token" {
		t.Fatalf("读取钉钉凭证失败: %v", err)
	}
	c, err := loadConfig(config)
	if err != nil || c.KeyFile != "/root/key" || c.DingKeyFile != "/root/dingkey" {
		t.Fatalf("默认配置不正确: %v", err)
	}
	// step.2 阻止未知字段和无效交易方向阈值被静默接受
	for _, content := range []string{`{"ding_token":"unsupported"}`, `{"low_threshold":2}`,
		`{"stop_loss_pct":-0.1}`, `{"stop_loss_pct":0}`, `{"stop_loss_pct":1}`} {
		if err := os.WriteFile(config, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadConfig(config); err == nil {
			t.Fatalf("应拒绝配置 %s", content)
		}
	}
}

// TestHedgeModeInitialization 验证已有双向持仓不切换，单向持仓切换失败则中止。
func TestHedgeModeInitialization(t *testing.T) {
	// step.1 分别模拟已启用双向、允许切换、交易所拒绝切换三种账户状态
	for _, state := range []string{"hedge", "switch", "reject"} {
		t.Run(state, func(t *testing.T) {
			changes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					_, _ = w.Write([]byte(`{"dualSidePosition":` + map[bool]string{true: "true", false: "false"}[state == "hedge"] + `}`))
					return
				}
				changes++
				if state == "reject" {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"code":-4068,"msg":"Position exists"}`))
					return
				}
				_, _ = w.Write([]byte(`{"code":200,"msg":"success"}`))
			}))
			defer server.Close()
			client := futures.NewClient("test", "test")
			client.BaseURL = server.URL
			// step.2 校验初始化结果，禁止模式不匹配后继续运行
			// 用模拟账户校验双向持仓初始化，不调用真实账户接口。
			err := helpEnsureHedgeMode(client)
			if (err != nil) != (state == "reject") || (state == "hedge" && changes != 0) ||
				(state != "hedge" && changes != 1) {
				t.Fatalf("模式初始化异常 changes=%d err=%v", changes, err)
			}
		})
	}
}
