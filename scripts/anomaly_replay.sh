#!/usr/bin/env bash
set -euo pipefail

# Simple helper to drive the dev-only anomaly replay API.
# Requirements:
#   - Backend started with:  REPLAY_TOKEN=your_token NOFX_ADMIN_PASSWORD=your_pwd go run -tags dev main.go
#   - Env vars here:
#       API (default: http://localhost:8080/api)
#       REPLAY_TOKEN (same as backend)
#       ADMIN_PASSWORD (same as NOFX_ADMIN_PASSWORD) or TOKEN (Bearer)

# Auto-load .env if present (exports all VAR=VAL)
if [[ -z "${DOTENV_LOADED:-}" && -f ".env" ]]; then
  # shellcheck disable=SC1090
  set -a; . ".env"; set +a
  DOTENV_LOADED=1
fi

# Allow NOFX_ADMIN_PASSWORD from .env as ADMIN_PASSWORD fallback
API=${API:-http://localhost:8080/api}
REPLAY_TOKEN=${REPLAY_TOKEN:-}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-${NOFX_ADMIN_PASSWORD:-}}
TOKEN=${TOKEN:-}
JWT_FILE=".replay.jwt"

usage() {
  cat <<EOF
Usage:
  $(basename "$0") login                            # admin login, save token to ${JWT_FILE}
  $(basename "$0") status                           # show replay status
  $(basename "$0") stop [SYMBOL]                    # stop all or one symbol

  # Quick ATR check (3m, default period=14)
  $(basename "$0") atr SYMBOL [PERIOD]

  # Price spike per-bar change (1.6% per bar, 5 bars, up)
  $(basename "$0") start price_up  SYMBOL [BARS  ABS_PER_BAR  SPEED]
  $(basename "$0") start price_down SYMBOL [BARS  ABS_PER_BAR  SPEED]

  # Volume spike X times (default oneshot)
  $(basename "$0") start volume   SYMBOL [X  BARS  MODE]

  # Consecutive move total change split over bars
  $(basename "$0") start consecutive SYMBOL TOTAL_ABS_PCT [BARS  up|down]

Env:
  API (default http://localhost:8080/api)
  REPLAY_TOKEN (required)
  ADMIN_PASSWORD or TOKEN (Bearer). If not provided, 'login' will request and store in ${JWT_FILE}
EOF
  exit 1
}

require_token() {
  if [[ -z "${TOKEN}" ]]; then
    if [[ -f "${JWT_FILE}" ]]; then
      TOKEN=$(cat "${JWT_FILE}")
    fi
  fi
  # Auto login if ADMIN_PASSWORD is available
  if [[ -z "${TOKEN}" && -n "${ADMIN_PASSWORD}" ]]; then
    login >/dev/null 2>&1 || true
    if [[ -f "${JWT_FILE}" ]]; then
      TOKEN=$(cat "${JWT_FILE}")
    fi
  fi
  if [[ -z "${TOKEN}" ]]; then
    echo "TOKEN missing. Run: ADMIN_PASSWORD=... $0 login" >&2
    exit 1
  fi
}

require_replay() {
  if [[ -z "${REPLAY_TOKEN}" ]]; then
    echo "REPLAY_TOKEN env missing" >&2
    exit 1
  fi
}

login() {
  if [[ -z "${ADMIN_PASSWORD}" ]]; then
    echo "ADMIN_PASSWORD env missing" >&2
    exit 1
  fi
  resp=$(curl -sS -X POST "${API}/admin-login" \
    -H 'Content-Type: application/json' \
    -d "{\"password\":\"${ADMIN_PASSWORD}\"}")
  token=$(echo "$resp" | sed -n 's/.*"token"\s*:\s*"\([^"]*\)".*/\1/p')
  if [[ -z "$token" ]]; then
    echo "Login failed: $resp" >&2
    exit 1
  fi
  echo "$token" > "$JWT_FILE"
  echo "Saved token to ${JWT_FILE}"
}

has_jq() { command -v jq >/dev/null 2>&1; }

post() {
  require_token
  require_replay
  local path="$1"; shift
  out=$(curl -sS -X POST "${API}${path}" \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "X-Replay-Token: ${REPLAY_TOKEN}" \
    -H 'Content-Type: application/json' \
    -d "$*")
  if has_jq; then echo "$out" | jq .; else echo "$out"; fi
}

get() {
  require_token
  require_replay
  local path="$1"; shift
  out=$(curl -sS -X GET "${API}${path}" \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "X-Replay-Token: ${REPLAY_TOKEN}")
  if has_jq; then echo "$out" | jq .; else echo "$out"; fi
}

cmd=${1:-}
case "$cmd" in
  login)
    login
    ;;
  status)
    get "/anomaly/replay/status"
    ;;
  atr)
    symbol=${2:-}
    period=${3:-14}
    if [[ -z "$symbol" ]]; then echo "symbol required" >&2; exit 1; fi
    # Fetch ATR
    require_token; require_replay
    url="${API}/anomaly/replay/atr?symbol=${symbol}&period=${period}"
    out=$(curl -sS -X GET "$url" \
      -H "Authorization: Bearer ${TOKEN}" \
      -H "X-Replay-Token: ${REPLAY_TOKEN}")
    if has_jq; then
      # Determine top-tier floors (BTC/ETH/BNB) vs ALT
      sym_up=$(echo "$symbol" | tr '[:lower:]' '[:upper:]')
      if [[ "$sym_up" == "BTCUSDT" || "$sym_up" == "ETHUSDT" || "$sym_up" == "BNBUSDT" ]]; then
        ABS_H=1.5; ABS_M=1.7; ABS_L=2.0
      else
        ABS_H=2.0; ABS_M=2.5; ABS_L=3.0
      fi
      K_H=2.0; K_M=2.5; K_L=3.0
      echo "$out" | jq --argjson kh "$K_H" --argjson km "$K_M" --argjson kl "$K_L" \
                        --argjson ah "$ABS_H" --argjson am "$ABS_M" --argjson al "$ABS_L" '
        if (has("error") or (.atr_pct | type) != "number") then
          .
        else
          . as $d |
          {
            symbol: $d.symbol,
            period: $d.period,
            close:  $d.close,
            atr_abs: $d.atr_abs,
            atr_pct: $d.atr_pct,
            thresholds: {
              high:   ((($kh * $d.atr_pct) as $x | if $x < $ah then $ah else $x end)),
              medium: ((($km * $d.atr_pct) as $x | if $x < $am then $am else $x end)),
              low:    ((($kl * $d.atr_pct) as $x | if $x < $al then $al else $x end))
            }
          }
        end'
    else
      echo "$out"
    fi
    ;;
  stop)
    symbol=${2:-}
    if [[ -n "$symbol" ]]; then
      post "/anomaly/replay/stop" "{\"symbol\":\"$symbol\"}"
    else
      post "/anomaly/replay/stop" "{}"
    fi
    ;;
  start)
    shift || true
    kind=${1:-}
    case "$kind" in
      price_up)
        symbol=${2:-BTCUSDT}; bars=${3:-5}; per=${4:-1.6}; speed=${5:-1}
        post "/anomaly/replay/start" "{\"symbol\":\"$symbol\",\"pattern\":\"price_spike\",\"interval\":\"3m\",\"bars\":$bars,\"abs_change_pct_per_bar\":$per,\"direction\":\"up\",\"mode\":\"stream\",\"speed\":$speed}"
        ;;
      price_down)
        symbol=${2:-BTCUSDT}; bars=${3:-5}; per=${4:-1.6}; speed=${5:-1}
        post "/anomaly/replay/start" "{\"symbol\":\"$symbol\",\"pattern\":\"price_spike\",\"interval\":\"3m\",\"bars\":$bars,\"abs_change_pct_per_bar\":$per,\"direction\":\"down\",\"mode\":\"stream\",\"speed\":$speed}"
        ;;
      volume)
        symbol=${2:-BTCUSDT}; x=${3:-20}; bars=${4:-1}; mode=${5:-oneshot}
        post "/anomaly/replay/start" "{\"symbol\":\"$symbol\",\"pattern\":\"volume_spike\",\"interval\":\"3m\",\"bars\":$bars,\"volume_x\":$x,\"mode\":\"$mode\"}"
        ;;
      consecutive)
        symbol=${2:-BTCUSDT}; total=${3:-5.5}; bars=${4:-3}; dir=${5:-up}
        post "/anomaly/replay/start" "{\"symbol\":\"$symbol\",\"pattern\":\"consecutive\",\"interval\":\"3m\",\"bars\":$bars,\"abs_change_pct_total\":$total,\"direction\":\"$dir\"}"
        ;;
      *)
        usage ;;
    esac
    ;;
  *)
    usage ;;
esac
