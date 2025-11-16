import { useState } from 'react'
import { api } from '../lib/api'

export default function DevMonitorPage() {
  const [traderId, setTraderId] = useState<string>('')
  const [symbols, setSymbols] = useState<string>('BTCUSDT,ETHUSDT')
  const [metrics, setMetrics] = useState<any>(null)
  const [regime, setRegime] = useState<any>(null)
  const [losses, setLosses] = useState<any>(null)
  const [pending, setPending] = useState<any>(null)
  const [openOrders, setOpenOrders] = useState<any>(null)
  const [loading, setLoading] = useState(false)

  async function refreshAll() {
    if (!traderId) return
    setLoading(true)
    try {
      const [m, r, l, p] = await Promise.all([
        api.devGetMetrics(traderId),
        api.devGetRegime({ traderId, symbols: symbols.split(',').map(s => s.trim()), includeNews: true }),
        api.devGetLosses({ traderId, symbols: symbols.split(',').map(s => s.trim()), lookback: 500, limit: 10, includeCtx: true }),
        api.devGetPendingLimits(traderId),
      ])
      setMetrics(m)
      setRegime(r)
      setLosses(l)
      setPending(p)
      // 默认查询第一个 symbol 的 open_orders
      const first = symbols.split(',')[0].trim()
      const oo = await api.devGetOpenOrders(traderId, first)
      setOpenOrders(oo)
    } catch (e) {
      console.error(e)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="p-4 space-y-4">
      <h1 className="text-xl font-semibold text-white">Dev 调试监控</h1>
      <div className="flex gap-2">
        <input className="bg-[#2B3139] text-white px-2 py-1 rounded" placeholder="trader_id" value={traderId} onChange={e => setTraderId(e.target.value)} />
        <input className="bg-[#2B3139] text-white px-2 py-1 rounded w-[320px]" placeholder="symbols 逗号分隔" value={symbols} onChange={e => setSymbols(e.target.value)} />
        <button className="bg-blue-600 px-3 py-1 rounded text-white" onClick={refreshAll} disabled={loading}>{loading ? '加载中...' : '刷新'}</button>
      </div>

      <section>
        <h2 className="text-white font-medium">Metrics</h2>
        <pre className="text-xs bg-[#1E2329] text-[#EAECEF] p-2 rounded overflow-auto">{metrics ? JSON.stringify(metrics, null, 2) : '无数据'}</pre>
      </section>

      <section>
        <h2 className="text-white font-medium">Regime / Opportunities / News</h2>
        <pre className="text-xs bg-[#1E2329] text-[#EAECEF] p-2 rounded overflow-auto">{regime ? JSON.stringify(regime, null, 2) : '无数据'}</pre>
      </section>

      <section>
        <h2 className="text-white font-medium">Losses</h2>
        <pre className="text-xs bg-[#1E2329] text-[#EAECEF] p-2 rounded overflow-auto">{losses ? JSON.stringify(losses, null, 2) : '无数据'}</pre>
      </section>

      <section>
        <h2 className="text-white font-medium">Pending Limits</h2>
        <pre className="text-xs bg-[#1E2329] text-[#EAECEF] p-2 rounded overflow-auto">{pending ? JSON.stringify(pending, null, 2) : '无数据'}</pre>
      </section>

      <section>
        <h2 className="text-white font-medium">Open Orders (首个symbol)</h2>
        <pre className="text-xs bg-[#1E2329] text-[#EAECEF] p-2 rounded overflow-auto">{openOrders ? JSON.stringify(openOrders, null, 2) : '无数据'}</pre>
      </section>
    </div>
  )
}
