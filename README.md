# KKK 合约策略

程序使用 `config.json`，不再读取 `.env`。部署示例：

```sh
cp config.sample.json config.json
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o kkk-linux-amd64 main.go help.go config.go
docker compose up -d
docker compose logs -f kkk
```

Compose 直接挂载 `kkk-linux-amd64` 执行。部署时将二进制、`config.json` 和
`docker-compose.yaml` 放在同一目录；仅上传文件不会自动启动、停止或重启容器。

若服务器仍有旧 `run.sh` 创建的 `kkk` 容器，先执行 `docker stop kkk`，避免新旧策略同时交易。

`keyfile` 默认 `/root/key`：第一行为 Binance API key，第二行为 secret。
`ding_keyfile` 默认 `/root/dingkey`：文件只放钉钉 token，末尾允许换行。
这两个文件在服务器准备，Compose 只读挂载。`config.json` 不含交易或钉钉凭证；
空的 `ding_keyfile` 表示禁用通知（同时可移除 Compose 对应挂载）。
修改凭证路径时需同步调整 Compose 挂载。

启动时查询账户持仓模式；单向模式尝试切换为双向模式。若账户现有持仓或挂单导致
Binance 拒绝切换，程序停止启动并报错，不会自动撤单或平仓。

## 信号和方向

当前策略沿用历史均价比值，并未计算 BOLL 上下轨：

| 条件 | 开仓 | 保护单平仓 |
| --- | --- | --- |
| 当前价 / 历史均价 >= `high_threshold` | SELL + SHORT | BUY + SHORT |
| 当前价 / 历史均价 <= `low_threshold` | BUY + LONG | SELL + LONG |

这是高位做空、低位做多的均值回归方向。突破后也可能延续趋势，方向本身不保证盈利。
双向持仓表示同一合约可同时持有多仓和空仓，每次信号只开对应方向。
数量按合约 `stepSize` 向下取整，保护价格按 `tickSize` 对齐。
双向模式的保护单明确携带 `positionSide`，不发送 `reduceOnly` 参数。

## 参数

- `history_hours`：历史小时价格窗口，默认 10。
- `low_threshold` / `high_threshold`：默认 0.5 / 2.0。
- `trade_amount_usdt`：单次名义下单金额，默认 12 USDT。
- `check_interval_minutes`：全天行情检测间隔，默认 1 分钟。
- `notify_cooldown_hours`：同一交易对信号处理后的冷却，默认 10 小时；沿用原逻辑，冷却限制交易和通知。
- `quiet_hour_start` / `quiet_hour_end`：北京时间钉钉静音区间，默认 23:00 至次日 07:00；交易和控制台日志照常，静音消息不补发。两值相同表示不静音。
- `enable_tp_sl`：启用止损止盈。
- `stop_loss_pct`：正值，默认 0.1（止损 10%），取值须大于 0 且小于 1。
- `take_profit_pct`：正值，默认 0.3（30%）。

## 本地验证

根目录和 `tests/` 中原有多个独立调试程序均声明 `main`，因此采用明确文件列表构建与测试：

```sh
go test -race main.go help.go config.go strategy_test.go
go build -o kkk main.go help.go config.go
```

测试使用本地模拟 HTTP 接口，不发起真实交易或钉钉通知。
