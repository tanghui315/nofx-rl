# 异常监控回放器使用说明（dev 专用）

本回放器用于在低波动行情或开发环境中“可控地”触发异常监控闭环（检测 → 决策/验证 → 执行），以验证功能完备性与边界条件。该能力仅在 dev 构建中启用，生产构建不包含此功能。

- 回放脚本：`scripts/anomaly_replay.sh`
- dev 路由（仅 dev 构建存在）：
  - `POST /api/anomaly/replay/start`
  - `POST /api/anomaly/replay/stop`
  - `GET  /api/anomaly/replay/status`
- 代码位置：
  - `api/replay_dev.go`（dev 版路由）/ `api/replay_stub.go`（非 dev 空实现）
  - `market/replay_dev.go`（dev 版 K 线回放注入）

---

## 1. 启动条件（dev 构建）

1) 设置环境变量（管理员模式 & 回放令牌）：

```bash
export NOFX_ADMIN_PASSWORD='your_admin_password'
export REPLAY_TOKEN='your_replay_token'
```

2) 启动后端（dev 构建标签）：

```bash
REPLAY_TOKEN=$REPLAY_TOKEN \
NOFX_ADMIN_PASSWORD=$NOFX_ADMIN_PASSWORD \
go run -tags dev main.go
```

- 仅 dev 构建会注册回放端点；普通构建不包含任何回放代码。
- 端点在“管理员模式 + 登录 + X-Replay-Token”双重保护下开放。

---

## 2. 脚本安装与登录

脚本位置：`scripts/anomaly_replay.sh`

重要：请使用 bash 运行，不要用 `sh` 调用（`sh scripts/...` 会导致环境与语法不兼容，从而出现 `404` 或 `jq parse error` 等问题）。

推荐用法：

```bash
./scripts/anomaly_replay.sh status
# 或
bash scripts/anomaly_replay.sh status
```

自动读取 `.env`：
- 脚本会在当前目录存在 `.env` 时自动加载其中的变量（等价于 `export`），因此可在 `.env` 里配置：
  - `NOFX_ADMIN_PASSWORD` 或 `ADMIN_PASSWORD`
  - `REPLAY_TOKEN`
  - `API`（例如 `http://localhost:8080/api`）
  - 其他自定义变量

依赖环境变量：
- `API`（可选，默认 `http://localhost:8080/api`）
- `REPLAY_TOKEN`（必填，应与后端一致）
- `ADMIN_PASSWORD`（与后端 `NOFX_ADMIN_PASSWORD` 相同；用于获取 JWT），或直接提供 `TOKEN`（已有 Bearer JWT）

首次登录获取 JWT（保存在 `.replay.jwt`）：

```bash
ADMIN_PASSWORD=your_admin_password \
REPLAY_TOKEN=your_replay_token \
scripts/anomaly_replay.sh login

或在 `.env` 中写入：

```env
NOFX_ADMIN_PASSWORD=your_admin_password
REPLAY_TOKEN=your_replay_token
API=http://localhost:8080/api
```

然后直接运行：

```bash
scripts/anomaly_replay.sh login
```
```

---

## 3. 常用命令

- 查看状态
```bash
REPLAY_TOKEN=your_token scripts/anomaly_replay.sh status
```

- 停止回放
```bash
# 停止所有
REPLAY_TOKEN=your_token scripts/anomaly_replay.sh stop
# 停止指定 symbol
REPLAY_TOKEN=your_token scripts/anomaly_replay.sh stop BTCUSDT
```

- 价格连续上涨（每根 +1.6%，5 根，1 根/秒）
```bash
REPLAY_TOKEN=your_token \
scripts/anomaly_replay.sh start price_up BTCUSDT 5 1.6 1
```

- 价格连续下跌（同上）
```bash
REPLAY_TOKEN=your_token \
scripts/anomaly_replay.sh start price_down BTCUSDT 5 1.6 1
```

- 量能极端 20x（oneshot 一根）
```bash
REPLAY_TOKEN=your_token \
scripts/anomaly_replay.sh start volume BTCUSDT 20 1 oneshot
```

- 连续同向累计 5.5%（3 根均分，向下）
```bash
REPLAY_TOKEN=your_token \
scripts/anomaly_replay.sh start consecutive BTCUSDT 5.5 3 down
```

> 提示：脚本会自动携带 `Authorization: Bearer <token>`（来自 `login` 或 `TOKEN` 环境变量）与 `X-Replay-Token: $REPLAY_TOKEN` 头；如安装了 `jq`，输出将自动格式化。

---

## 4. 参数说明

回放器（dev）仅支持 3m K 线注入，等效“新 K 线闭合”事件：

- `price_spike`
  - `abs_change_pct_per_bar`：单根绝对涨跌幅（默认 1.5）
  - `direction`：`up|down`
  - `bars`：回放根数（默认 3）
  - `mode`：`stream|oneshot`（默认 stream，逐根推送）
  - `speed`：每秒推送多少根（默认 1）
- `consecutive`
  - `abs_change_pct_total`：总累计涨跌幅（默认 5）
  - 其余同上（均分到 `bars` 根）
- `volume_spike`
  - `volume_x`：相对近 20 根均量的倍数（默认 20）
  - `bars`：默认 1
  - `mode`：建议 `oneshot`

内部生成规则：
- 价格：按指定涨跌幅递推（open→close），并生成窄幅高低点（±0.1%）。
- 成交量：如指定 `volume_spike`，将 `Volume` 设置为 `baseVol × volume_x`，`QuoteVolume = Volume × Close`。
- 时间：每根 K 线间隔 3 分钟，依次递增。

---

## 5. 推荐验证流程（示例）

1) Guard 真执行（LLM 关闭）：
```bash
# 先将异常监控设为 Guard + high
curl -sS -X PUT "$API/anomaly/config" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"mode":"guard","sensitivity":"high","use_llm":false}'
# 连续下跌 5.5%（3 根）触发“减仓 + 收紧止损”
scripts/anomaly_replay.sh start consecutive BTCUSDT 5.5 3 down
```

2) 动作冷却验证：同一 symbol 在冷却期内重复触发，日志出现“动作冷却中”，不重复下单。

3) 搏一搏边界：
- 在前端 `/anomaly-config` 设置较高 `min_amount`；回放 `volume 20x` → 看到“仓位过小跳过执行”。
- 设置合理的 `max_position_pct / max_amount`，回放 `volume 20x` → 执行且不超边界。

4) 杠杆上限：开启 LLM 后回放触发，若决策杠杆超出“交易员上限×倍数”或系统 50x，将被验证层拒绝。

---

  新用法

  - 查看 ATR（默认 period=14）
      - bash scripts/anomaly_replay.sh atr ETHUSDT
      - 或指定周期：bash scripts/anomaly_replay.sh atr
        ETHUSDT 14
  - 输出内容（安装了 jq 会格式化）
      - symbol、period、close、atr_abs、atr_pct
      - thresholds: { high, medium, low }
          - 对 BTC/ETH/BNB：使用我们下调后的绝对下限
            （high=1.5%、medium=1.7%、low=2.0%）
          - 对其他山寨：保持较高绝对下限（high=2.0%、
            medium=2.5%、low=3.0%）
          - K 系数固定（high=2.0、medium=2.5、low=3.0）
          - 最终门槛 = max(K×ATR%，绝对下限)

  注意

  - 该 ATR 接口是 dev 专用（GET /api/anomaly/replay/atr），需
    要 dev 构建 + REPLAY_TOKEN + 登录；脚本会自动读取 .env 并
    自动登录。
  - 如果你希望 UI 页面也显示“当前 ATR 与推导门槛”，我可以在 /
    anomaly-config 页补一个“阈值探测”小卡片，输入 symbol 即可
    查看。

## 6. 常见问题

- 401 invalid replay token
  - 未设置或不匹配 `REPLAY_TOKEN`。确保服务端环境变量与脚本一致，并在请求头带 `X-Replay-Token`。
- 403 仅管理员模式可用 / 登录失败
  - dev 回放端点需要管理员模式 + 管理员登录。检查 `NOFX_ADMIN_PASSWORD`、`ADMIN_PASSWORD` 与 `login` 步骤。
- 回放端点 404
  - 需要 dev 构建：确认以 `go run -tags dev main.go` 启动。
- 触发后无动作
  - 检查 `/api/anomaly/config` 是否开启（模式非 off/watch）；
  - Guard 模式建议关闭 LLM；
  - 冷却期内重复触发会被“动作冷却”阻止（查看日志）。

---

## 7. 安全与注意

- 回放能力仅用于开发/测试；生产构建不会注册任何回放代码与路由。
- 回放注入只影响内存中的 K 线序列，不破坏真实订阅；停止即可恢复正常。
- 建议配合小额 Testnet/子账户、最小/最大金额与仓位限制，严格验证风控边界。

---

## 8. 相关文件
- `scripts/anomaly_replay.sh`（脚本）
- `api/replay_dev.go` / `api/replay_stub.go`（路由）
- `market/replay_dev.go`（回放实现）
- `api/server.go`：`augmentRoutes` 挂载点（dev/非 dev 自动切换）
