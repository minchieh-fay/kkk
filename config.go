package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// Config 保存交易策略、通知参数及外部密钥文件路径。
type Config struct {
	KeyFile              string  `json:"keyfile"`
	DingKeyFile          string  `json:"ding_keyfile"`
	HistoryHours         int     `json:"history_hours"`
	LowThreshold         float64 `json:"low_threshold"`
	HighThreshold        float64 `json:"high_threshold"`
	NotifyCooldownHours  int     `json:"notify_cooldown_hours"`
	CheckIntervalMinutes int     `json:"check_interval_minutes"`
	TradeAmountUSDT      float64 `json:"trade_amount_usdt"`
	QuietHourStart       int     `json:"quiet_hour_start"`
	QuietHourEnd         int     `json:"quiet_hour_end"`
	EnableTpSl           bool    `json:"enable_tp_sl"`
	StopLossPct          float64 `json:"stop_loss_pct"`
	TakeProfitPct        float64 `json:"take_profit_pct"`
}

// loadConfig 从 JSON 加载配置，补齐缺省参数并校验交易条件。
func loadConfig(path string) (Config, error) {
	// step.1 设置默认参数并读取部署方准备的 JSON 文件
	c := Config{
		KeyFile: "/root/key", DingKeyFile: "/root/dingkey", HistoryHours: 10, LowThreshold: 0.5, HighThreshold: 2,
		NotifyCooldownHours: 10, CheckIntervalMinutes: 1, TradeAmountUSDT: 12,
		QuietHourStart: 23, QuietHourEnd: 7, EnableTpSl: true,
		StopLossPct: 0.1, TakeProfitPct: 0.3,
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("读取配置 %s（可从 config.sample.json 复制）: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("解析配置: %w", err)
	}
	// step.2 校验信号窗口、交易金额、冷却和保护单参数
	if err := c.validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

// validate 阻止无效策略配置进入实盘运行。
func (c Config) validate() error {
	// step.1 校验凭证路径和行情、资金及信号参数
	if c.KeyFile == "" || c.HistoryHours < 1 || c.HistoryHours > 1500 ||
		c.CheckIntervalMinutes < 1 || c.TradeAmountUSDT <= 0 || c.NotifyCooldownHours < 0 {
		return fmt.Errorf("keyfile 不能为空，history_hours 必须为 1~1500，检查周期和金额须为正，冷却不可为负")
	}
	if c.LowThreshold <= 0 || c.LowThreshold >= 1 || c.HighThreshold <= 1 {
		return fmt.Errorf("low_threshold 必须在 0 和 1 之间，high_threshold 必须大于 1")
	}
	// step.2 校验静音区间与多空保护价格的有效范围
	if c.QuietHourStart < 0 || c.QuietHourStart > 23 || c.QuietHourEnd < 0 || c.QuietHourEnd > 23 {
		return fmt.Errorf("quiet_hour_start 和 quiet_hour_end 必须为 0~23；相同时不静音")
	}
	if c.StopLossPct <= 0 || c.StopLossPct >= 1 || c.TakeProfitPct <= 0 || c.TakeProfitPct >= 1 {
		return fmt.Errorf("stop_loss_pct 必须在 0 和 1 之间，take_profit_pct 必须在 0 和 1 之间")
	}
	return nil
}
