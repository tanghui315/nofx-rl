import { useEffect, useMemo, useState } from 'react'
import { api } from '../lib/api'
import type { NewsItem } from '../types'

interface Props {
  defaultSymbol?: string
}

export function NewsDebugPanel({ defaultSymbol }: Props) {
  const [symbols, setSymbols] = useState<string[]>([])
  const [symbol, setSymbol] = useState<string>(defaultSymbol || '')
  const [items, setItems] = useState<NewsItem[]>([])
  const [limit, setLimit] = useState<number>(5)
  const [loading, setLoading] = useState<boolean>(false)
  const [error, setError] = useState<string>('')
  const [showSummary, setShowSummary] = useState<boolean>(false)

  // Load default_coins from /api/config
  useEffect(() => {
    const load = async () => {
      try {
        const res = await fetch('/api/config')
        const cfg = await res.json()
        const coins: string[] = cfg.default_coins || [
          'BTCUSDT','ETHUSDT','BNBUSDT','SOLUSDT','XRPUSDT','DOGEUSDT','ADAUSDT'
        ]
        setSymbols(coins)
        if (!symbol && coins.length > 0) setSymbol(coins[0])
      } catch (e) {
        setSymbols(['BTCUSDT','ETHUSDT','BNBUSDT'])
        if (!symbol) setSymbol('BTCUSDT')
      }
    }
    load()
  }, [])

  const canQuery = useMemo(() => !!symbol, [symbol])

  const refresh = async () => {
    if (!canQuery) return
    setLoading(true)
    setError('')
    try {
      const list = await api.getNews(symbol, limit)
      setItems(list || [])
    } catch (e: any) {
      setError(e?.message || '加载失败')
      setItems([])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    if (canQuery) refresh()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [symbol, limit])

  return (
    <div className="mt-4 bg-[#0B0E11] border border-[#2B3139] rounded-lg p-4">
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-3">
          <h3 className="text-sm font-semibold text-[#EAECEF]">📰 新闻缓存调试</h3>
          <select
            value={symbol}
            onChange={(e) => setSymbol(e.target.value)}
            className="px-2 py-1 bg-[#0B0E11] border border-[#2B3139] rounded text-[#EAECEF]"
          >
            {symbols.map((s) => (
              <option key={s} value={s}>{s}</option>
            ))}
          </select>
          <select
            value={String(limit)}
            onChange={(e) => setLimit(parseInt(e.target.value, 10) || 5)}
            className="px-2 py-1 bg-[#0B0E11] border border-[#2B3139] rounded text-[#EAECEF]"
          >
            {[3,5,10].map((n) => (
              <option key={n} value={n}>{n}条</option>
            ))}
          </select>
        </div>
        <button
          onClick={refresh}
          disabled={loading || !canQuery}
          className="px-3 py-1.5 rounded text-xs font-semibold transition-all"
          style={{
            background: '#2B3139',
            color: '#EAECEF',
            border: '1px solid #474D57',
            opacity: loading ? 0.6 : 1,
          }}
        >
          {loading ? '刷新中…' : '刷新'}
        </button>
      </div>

      {/* Global Toggle: Show Summary */}
      <div className="flex items-center gap-2 mb-3 text-xs" style={{ color: '#EAECEF' }}>
        <label className="flex items-center gap-2 cursor-pointer">
          <input
            type="checkbox"
            checked={showSummary}
            onChange={(e) => setShowSummary(e.target.checked)}
          />
          展开摘要
        </label>
        <span className="text-[#848E9C]">（显示后每条包含简要内容）</span>
      </div>

      {error && (
        <div className="text-xs text-red-400 mb-2">{error}</div>
      )}

      {items.length === 0 ? (
        <div className="text-xs text-[#848E9C]">暂无缓存新闻（确保服务已设置 TAVILY_API_KEY）</div>
      ) : (
        <ul className="space-y-2">
          {items.map((it, idx) => (
            <li key={idx} className="p-3 rounded border border-[#2B3139] bg-[#0E1116]">
              <div className="text-sm text-[#EAECEF] font-medium mb-1">
                <a href={it.url} target="_blank" rel="noreferrer" className="hover:underline">{it.title}</a>
              </div>
              <div className="text-xs text-[#848E9C] flex gap-3">
                <span>{it.source || 'unknown'}</span>
                {it.published_at && <span>{it.published_at}</span>}
              </div>
              {showSummary && it.summary && (
                <div className="mt-2 text-xs leading-5" style={{ color: '#C5CFD9' }}>
                  {it.summary}
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  )}
