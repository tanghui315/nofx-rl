#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Binance Futures (UM/CM) Kline Downloader
- Default: ETHUSDT, 5m interval, USDT-M (UM) contracts
- Supports custom symbol/interval/start/end
- Adds gentle delay between requests to respect rate limits
- Outputs CSV with columns: timestamp, open, high, low, close, volume, amount
- Optional streaming checkpoint: write each batch to CSV to avoid memory pressure
  and allow resuming when interrupted.

Notes:
- amount = quote_asset_volume (e.g., USDT for ETHUSDT)
- volume = base_asset_volume (e.g., ETH for ETHUSDT)
"""

import argparse
import sys
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional, Tuple, List
try:
    # optional progress bar
    from tqdm import tqdm  # type: ignore
except Exception:  # pragma: no cover
    tqdm = None  # fallback if tqdm not installed
from tqdm import tqdm

import pandas as pd
import requests

UM_ENDPOINT = "https://fapi.binance.com/fapi/v1/klines"  # USDT-M Futures
CM_ENDPOINT = "https://dapi.binance.com/dapi/v1/klines"  # Coin-M Futures

INTERVALS = {
    # Binance supported intervals
    "1m", "3m", "5m", "15m", "30m",
    "1h", "2h", "4h", "6h", "8h", "12h",
    "1d", "3d", "1w", "1M"
}


def to_ms(ts: Optional[str]) -> Optional[int]:
    """Convert various time inputs to milliseconds since epoch.
    - None -> None
    - digits string -> int(ms)
    - ISO-like datetime string -> ms (UTC assumed if no tz)
    """
    if ts is None:
        return None
    s = ts.strip()
    if s.isdigit():
        return int(s)
    # Try several formats
    fmts = [
        "%Y-%m-%d %H:%M:%S",
        "%Y-%m-%d %H:%M",
        "%Y-%m-%d",
        "%Y/%m/%d %H:%M:%S",
        "%Y/%m/%d %H:%M",
        "%Y/%m/%d",
        "%Y-%m-%dT%H:%M:%S",
        "%Y-%m-%dT%H:%M:%SZ",
    ]
    dt = None
    for f in fmts:
        try:
            dt = datetime.strptime(s, f)
            break
        except ValueError:
            continue
    if dt is None:
        raise ValueError(f"Unrecognized datetime format: {ts}")
    # Assume naive time is in local time? We choose UTC to be explicit.
    dt = dt.replace(tzinfo=timezone.utc)
    return int(dt.timestamp() * 1000)


def now_ms() -> int:
    return int(datetime.now(tz=timezone.utc).timestamp() * 1000)


class BinanceFuturesKlinesFetcher:
    def __init__(self, symbol: str = "ETHUSDT", interval: str = "5m", market: str = "um"):
        self.symbol = symbol.upper()
        self.interval = interval
        market = market.lower()
        if market not in ("um", "cm"):
            raise ValueError("market must be 'um' (USDT-M) or 'cm' (Coin-M)")
        self.market = market
        self.endpoint = UM_ENDPOINT if self.market == "um" else CM_ENDPOINT

    def fetch_once(self, start_ms: Optional[int] = None, end_ms: Optional[int] = None,
                   interval: Optional[str] = None, limit: int = 1500) -> pd.DataFrame:
        params = {
            "symbol": self.symbol,
            "interval": interval or self.interval,
            "limit": limit,
        }
        if start_ms is not None:
            params["startTime"] = int(start_ms)
        if end_ms is not None:
            params["endTime"] = int(end_ms)
        r = requests.get(self.endpoint, params=params, timeout=15)
        r.raise_for_status()
        data = r.json()
        if not data:
            return pd.DataFrame()
        # Binance kline fields
        cols = [
            'open_time', 'open', 'high', 'low', 'close', 'volume',
            'close_time', 'quote_volume', 'trades', 'taker_buy_base',
            'taker_buy_quote', 'ignore'
        ]
        df = pd.DataFrame(data, columns=cols)
        # Convert types
        for c in ['open', 'high', 'low', 'close', 'volume', 'quote_volume']:
            df[c] = df[c].astype(float)
        df['timestamp'] = pd.to_datetime(df['open_time'], unit='ms', utc=True)
        # Standardized columns for this project
        df['amount'] = df['quote_volume']
        out = df[['timestamp', 'open', 'high', 'low', 'close', 'volume', 'amount']].copy()
        return out

    def fetch_range(self, start: Optional[int], end: Optional[int], interval: Optional[str],
                    delay: float = 0.5, limit: int = 1500, verbose: bool = True,
                    max_empty: int = 3,
                    stream_to: Optional[Path] = None,
                    checkpoint_every_batches: int = 1,
                    progress: Optional[object] = None) -> pd.DataFrame:
        interval = interval or self.interval
        if interval not in INTERVALS:
            raise ValueError(f"Unsupported interval: {interval}")
        if end is None:
            end = now_ms()
        if start is None:
            raise ValueError("start time must be provided for futures historical download")

        frames: List[pd.DataFrame] = []  # used only if not streaming
        cur = int(start)
        start0 = int(start)
        empty_streak = 0
        batch = 0
        written_batches = 0

        # Prepare streaming file if enabled
        if stream_to is not None:
            stream_to = Path(stream_to)
            stream_to.parent.mkdir(parents=True, exist_ok=True)
            header_needed = not stream_to.exists() or stream_to.stat().st_size == 0
            # small ring buffer for batching writes if checkpoint_every_batches > 1
            stream_buffer: List[pd.DataFrame] = []
        else:
            header_needed = False
        while cur < end:
            batch += 1
            if verbose:
                print(f"Fetching batch #{batch} | start={cur} end={end} interval={interval} ...", flush=True)
            try:
                df = self.fetch_once(start_ms=cur, end_ms=end, interval=interval, limit=limit)
            except requests.RequestException as e:
                print(f"Request error: {e}. Sleeping {delay}s and retrying...")
                time.sleep(delay)
                empty_streak += 1
                if empty_streak >= max_empty:
                    break
                continue
            except Exception as e:
                print(f"Error: {e}")
                empty_streak += 1
                if empty_streak >= max_empty:
                    break
                time.sleep(delay)
                continue

            if df.empty:
                empty_streak += 1
                if empty_streak >= max_empty:
                    if verbose:
                        print("No more data returned; stopping.")
                    break
                time.sleep(delay)
                continue

            empty_streak = 0

            # Drop duplicates within batch and sort
            df = df.drop_duplicates(subset=['timestamp']).sort_values('timestamp')

            if stream_to is not None:
                # Buffer this batch
                stream_buffer.append(df)
                flush_now = (len(stream_buffer) >= max(1, int(checkpoint_every_batches)))
                if flush_now:
                    flush_df = pd.concat(stream_buffer, ignore_index=True)
                    flush_df.to_csv(stream_to, mode='a', index=False, header=header_needed)
                    header_needed = False
                    written_batches += len(stream_buffer)
                    stream_buffer.clear()
                    if verbose:
                        print(f"  -> appended {len(flush_df)} rows to {stream_to} (batches written: {written_batches})")
            else:
                frames.append(df)
            last_ts = int(df['timestamp'].iloc[-1].timestamp() * 1000)
            next_start = last_ts + 1
            if next_start <= cur:
                # Safety to avoid infinite loop
                next_start = cur + 1
            cur = next_start

            # update progress bar by time covered (ms)
            if progress is not None and hasattr(progress, 'update') and hasattr(progress, 'n') and hasattr(progress, 'total'):
                covered = max(0, last_ts - start0)
                inc = covered - progress.n
                if inc > 0:
                    progress.update(inc)

            time.sleep(delay)

        if stream_to is not None:
            # Flush remaining buffer
            if 'stream_buffer' in locals() and len(stream_buffer) > 0:
                flush_df = pd.concat(stream_buffer, ignore_index=True)
                flush_df.to_csv(stream_to, mode='a', index=False, header=header_needed)
                if verbose:
                    print(f"  -> appended (final) {len(flush_df)} rows to {stream_to}")
            # When streaming, data already persisted. Return empty df to indicate success.
            return pd.DataFrame()
        else:
            if not frames:
                return pd.DataFrame()
            all_df = pd.concat(frames, ignore_index=True)
            all_df = all_df.drop_duplicates(subset=['timestamp']).sort_values('timestamp').reset_index(drop=True)
            return all_df


def build_default_out(symbol: str, interval: str, start_ms: int, end_ms: int, market: str) -> Path:
    start_s = datetime.fromtimestamp(start_ms/1000, tz=timezone.utc).strftime('%Y%m%d_%H%M%S')
    end_s   = datetime.fromtimestamp(end_ms/1000, tz=timezone.utc).strftime('%Y%m%d_%H%M%S')
    return Path(f"data/{symbol}_{market}_{interval}_{start_s}_to_{end_s}.csv")


def main():
    p = argparse.ArgumentParser(description="Download Binance Futures klines (UM/CM)")
    p.add_argument('--symbol', default='ETHUSDT', help='Trading pair, e.g., ETHUSDT')
    p.add_argument('--market', default='um', choices=['um', 'cm'], help='Futures market: um=USDT-M, cm=Coin-M')
    p.add_argument('--interval', default='5m', help='Kline interval (e.g., 1m,3m,5m,15m,1h,4h,1d,1w,1M)')
    p.add_argument('--start', required=True, help='Start time (ms or ISO: YYYY-MM-DD[ HH:MM[:SS]])')
    p.add_argument('--end', default=None, help='End time (ms or ISO). Default: now')
    p.add_argument('--delay', type=float, default=0.5, help='Delay seconds between requests (rate limit friendly)')
    p.add_argument('--limit', type=int, default=1500, help='Max candles per request (futures max 1500)')
    p.add_argument('--out', default=None, help='Output CSV path')
    p.add_argument('--no-save', action='store_true', help='Do not save CSV, just print summary')
    p.add_argument('--progress', action='store_true', help='Show a progress bar (time coverage)')
    p.add_argument('--resume', action='store_true', help='Resume from existing CSV by starting after its last timestamp')
    p.add_argument('--stream', action='store_true', help='Stream-append each batch to CSV (checkpointing)')
    p.add_argument('--checkpoint-every-batches', type=int, default=1, help='Append every N batches (only with --stream)')
    args = p.parse_args()

    start_ms = to_ms(args.start)
    end_ms = to_ms(args.end) if args.end else now_ms()

    fetcher = BinanceFuturesKlinesFetcher(symbol=args.symbol, interval=args.interval, market=args.market)

    # optional progress bar
    pbar = None
    if args.progress and tqdm is not None:
        total_ms = max(1, end_ms - start_ms)
        pbar = tqdm(total=total_ms, unit='ms', desc=f"{args.symbol.upper()} {args.interval} {args.market.upper()}")

    out_path = Path(args.out) if args.out else build_default_out(args.symbol.upper(), args.interval, start_ms, end_ms, args.market.lower())
    # If --out points to a directory (or has no suffix), place a default-named file inside it
    if args.out:
        maybe_dir = Path(args.out)
        if maybe_dir.is_dir() or str(args.out).endswith(('/', '\\')) or maybe_dir.suffix == "":
            default_name = build_default_out(args.symbol.upper(), args.interval, start_ms, end_ms, args.market.lower()).name
            out_path = maybe_dir / default_name

    # Resume from existing file if requested
    if args.resume and out_path.exists() and out_path.stat().st_size > 0:
        try:
            with open(out_path, 'rb') as f:
                f.seek(0, 2)
                endpos = f.tell()
                back = min(endpos, 65536)
                f.seek(endpos - back)
                chunk = f.read().splitlines()
                last_line = next((line for line in reversed(chunk) if line.strip()), None)
            if last_line is not None:
                first_field = last_line.decode('utf-8').split(',')[0]
                last_dt = pd.to_datetime(first_field, utc=True)
                start_ms = int(last_dt.value // 10**6) + 1
                print(f"Resuming from {last_dt} (start_ms={start_ms})")
        except Exception as e:
            print(f"Resume detection failed, starting from provided --start. Error: {e}")

    if args.stream and not args.no_save:
        out_path.parent.mkdir(parents=True, exist_ok=True)
        print(f"Streaming to CSV: {out_path}")
        _ = fetcher.fetch_range(start=start_ms, end=end_ms, interval=args.interval, delay=args.delay,
                                limit=args.limit, verbose=True, stream_to=out_path,
                                checkpoint_every_batches=args.checkpoint_every_batches,
                                progress=pbar)
        # Try to summarize file range
        try:
            head = pd.read_csv(out_path, nrows=1, parse_dates=['timestamp'])
            tail = pd.read_csv(out_path, parse_dates=['timestamp']).tail(1)
            approx_rows = sum(1 for _ in open(out_path)) - 1
            print(f"Done. File: {out_path} | rows≈{approx_rows} | range: {head['timestamp'].iloc[0]} -> {tail['timestamp'].iloc[0]}")
        except Exception:
            print(f"Done. File: {out_path}")
    else:
        df = fetcher.fetch_range(start=start_ms, end=end_ms, interval=args.interval, delay=args.delay, limit=args.limit,
                                 progress=pbar)
        if df.empty:
            print("No data fetched.")
            sys.exit(1)
        print(f"Fetched {len(df)} rows | {df['timestamp'].min()} -> {df['timestamp'].max()}")
        if not args.no_save:
            out_path.parent.mkdir(parents=True, exist_ok=True)
            df.to_csv(out_path, index=False)
            print(f"Saved CSV: {out_path}")
    # close progress bar
    if pbar is not None:
        try:
            remaining = pbar.total - pbar.n
            if remaining > 0:
                pbar.update(remaining)
        finally:
            pbar.close()

if __name__ == '__main__':
    main()
 
