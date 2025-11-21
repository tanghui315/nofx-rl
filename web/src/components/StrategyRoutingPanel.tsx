import React from 'react'
import { t, type Language } from '../i18n/translations'
import { Zap, Activity, TrendingUp, TrendingDown, ArrowUpRight, BarChart2 } from 'lucide-react'

interface StrategyRoutingPanelProps {
  routing?: { [symbol: string]: string }
  trendDirections?: { [symbol: string]: string }
  language: Language
}

export function StrategyRoutingPanel({ routing, trendDirections, language }: StrategyRoutingPanelProps) {
  if (!routing || Object.keys(routing).length === 0) {
    return null
  }

  // Helper to get strategy color and icon
  const getStrategyStyle = (strategy: string, dominantTrend?: string) => {
    if (strategy.includes('trend')) {
      // 根据该策略组的主导趋势方向调整卡片颜色与图标
      if (dominantTrend === 'bearish') {
        return { color: '#F6465D', icon: TrendingDown, label: 'Trend Carry' }
      }
      if (dominantTrend === 'sideways') {
        return { color: '#848E9C', icon: Activity, label: 'Trend Carry' }
      }
      // 默认视为看多趋势
      return { color: '#0ECB81', icon: TrendingUp, label: 'Trend Carry' }
    }
    if (strategy.includes('breakout')) return { color: '#F0B90B', icon: ArrowUpRight, label: 'Breakout' }
    if (strategy.includes('pullback')) return { color: '#60a5fa', icon: Activity, label: 'Pullback' }
    if (strategy.includes('range')) return { color: '#c084fc', icon: BarChart2, label: 'Range Grid' }
    if (strategy.includes('news')) return { color: '#F6465D', icon: Zap, label: 'News Catalyst' }
    return { color: '#848E9C', icon: Activity, label: strategy }
  }

  // Group symbols by strategy
  const grouped: { [strategy: string]: string[] } = {}
  Object.entries(routing).forEach(([symbol, strategy]) => {
    if (!grouped[strategy]) grouped[strategy] = []
    grouped[strategy].push(symbol)
  })

  return (
    <div className="binance-card p-4 mb-6 animate-slide-in">
      <div className="flex items-center gap-2 mb-4 border-b border-gray-800 pb-3">
        <Zap className="w-5 h-5" style={{ color: '#F0B90B' }} />
        <h3 className="font-bold text-lg" style={{ color: '#EAECEF' }}>
          {t('strategyRouting', language)}
        </h3>
        <span className="text-xs px-2 py-0.5 rounded bg-gray-800 text-gray-400">
          {Object.keys(routing).length} Symbols
        </span>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {Object.entries(grouped).map(([strategy, symbols]) => {
          let dominantTrend: string | undefined
          if (strategy.includes('trend') && trendDirections) {
            let bull = 0
            let bear = 0
            let side = 0
            symbols.forEach(sym => {
              const dir = trendDirections[sym]
              if (dir === 'bullish') bull++
              else if (dir === 'bearish') bear++
              else if (dir === 'sideways') side++
            })
            // 仅当该组全为单一方向时，才认为有明确主导方向
            const total = bull + bear + side
            if (total > 0) {
              if (bear === total) dominantTrend = 'bearish'
              else if (bull === total) dominantTrend = 'bullish'
              else if (side === total) dominantTrend = 'sideways'
            }
          }

          const style = getStrategyStyle(strategy, dominantTrend)
          const Icon = style.icon

          return (
            <div
              key={strategy}
              className="rounded p-3 border transition-all hover:border-opacity-50"
              style={{
                background: 'rgba(11, 14, 17, 0.5)',
                borderColor: `${style.color}30`
              }}
            >
              <div className="flex items-center gap-2 mb-2">
                <Icon className="w-4 h-4" style={{ color: style.color }} />
                <span className="font-bold text-sm" style={{ color: style.color }}>
                  {style.label}
                </span>
                <span className="text-xs text-gray-500 ml-auto">
                  {symbols.length}
                </span>
              </div>
              <div className="flex flex-wrap gap-1.5">
                {symbols.map(sym => {
                  const dir = trendDirections?.[sym]
                  let dirSymbol = ''
                  if (dir === 'bullish') dirSymbol = '↑'
                  else if (dir === 'bearish') dirSymbol = '↓'
                  else if (dir === 'sideways') dirSymbol = '↔'

                  return (
                    <span
                      key={sym}
                      className="text-xs px-2 py-1 rounded font-mono"
                      style={{
                        background: `${style.color}15`,
                        color: '#EAECEF'
                      }}
                    >
                      {sym}{dirSymbol ? ` ${dirSymbol}` : ''}
                    </span>
                  )
                })}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}
