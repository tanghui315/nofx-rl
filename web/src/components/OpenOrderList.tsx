import React, { useState } from 'react'
import useSWR from 'swr'
import { api } from '../lib/api'
import { t, type Language } from '../i18n/translations'
import type { OpenOrder } from '../types'

interface OpenOrderListProps {
  traderId: string
  language: Language
}

export function OpenOrderList({ traderId, language }: OpenOrderListProps) {
  const { data: orders, mutate } = useSWR<OpenOrder[]>(
    traderId ? `orders-${traderId}` : null,
    () => api.getOpenOrders(traderId),
    {
      refreshInterval: 5000, // 每5秒刷新一次
    }
  )

  const [cancellingId, setCancellingId] = useState<number | null>(null)

  const handleCancelOrder = async (symbol: string, orderId: number) => {
    if (!confirm(t('confirmCancelOrder', language))) return

    setCancellingId(orderId)
    try {
      await api.cancelOrder(traderId, symbol, orderId)
      await mutate() // 刷新列表
    } catch (error: any) {
      console.error('Failed to cancel order:', error)
      alert(t('cancelOrderFailed', language) + ': ' + error.message)
    } finally {
      setCancellingId(null)
    }
  }

  if (!orders || orders.length === 0) {
    return null
  }

  return (
    <div className="binance-card p-6 animate-slide-in">
      <div className="flex items-center justify-between mb-5">
        <h2
          className="text-xl font-bold flex items-center gap-2"
          style={{ color: '#EAECEF' }}
        >
          📋 {t('openOrders', language)}
        </h2>
        <div
          className="text-xs px-3 py-1 rounded"
          style={{
            background: 'rgba(240, 185, 11, 0.1)',
            color: '#F0B90B',
            border: '1px solid rgba(240, 185, 11, 0.2)',
          }}
        >
          {orders.length} {t('active', language)}
        </div>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead className="text-left border-b border-gray-800">
            <tr>
              <th className="pb-3 font-semibold text-gray-400">
                {t('symbol', language)}
              </th>
              <th className="pb-3 font-semibold text-gray-400">
                {t('side', language)}
              </th>
              <th className="pb-3 font-semibold text-gray-400">
                {t('type', language)}
              </th>
              <th className="pb-3 font-semibold text-gray-400">
                {t('price', language)}
              </th>
              <th className="pb-3 font-semibold text-gray-400">
                {t('quantity', language)}
              </th>
              <th className="pb-3 font-semibold text-gray-400">
                {t('filled', language)}
              </th>
              <th className="pb-3 font-semibold text-gray-400">
                {t('operation', language)}
              </th>
            </tr>
          </thead>
          <tbody>
            {orders.map((order) => (
              <tr
                key={order.orderId}
                className="border-b border-gray-800 last:border-0"
              >
                <td className="py-3 font-mono font-semibold">
                  {order.symbol}
                </td>
                <td className="py-3">
                  <span
                    className="px-2 py-1 rounded text-xs font-bold"
                    style={
                      order.side === 'BUY'
                        ? {
                            background: 'rgba(14, 203, 129, 0.1)',
                            color: '#0ECB81',
                          }
                        : {
                            background: 'rgba(246, 70, 93, 0.1)',
                            color: '#F6465D',
                          }
                    }
                  >
                    {order.side}
                  </span>
                </td>
                <td className="py-3 font-mono text-gray-400">
                  {order.type}
                </td>
                <td className="py-3 font-mono" style={{ color: '#EAECEF' }}>
                  {order.price > 0 ? order.price.toFixed(4) : 'Market'}
                </td>
                <td className="py-3 font-mono" style={{ color: '#EAECEF' }}>
                  {order.origQty}
                </td>
                <td className="py-3 font-mono" style={{ color: '#EAECEF' }}>
                  {order.executedQty} ({((order.executedQty / order.origQty) * 100).toFixed(1)}%)
                </td>
                <td className="py-3">
                  <button
                    onClick={() => handleCancelOrder(order.symbol, order.orderId)}
                    disabled={cancellingId === order.orderId}
                    className="px-3 py-1 rounded text-xs font-bold transition-colors disabled:opacity-50 disabled:cursor-not-allowed hover:brightness-110"
                    style={{
                      background: 'rgba(246, 70, 93, 0.1)',
                      color: '#F6465D',
                      border: '1px solid rgba(246, 70, 93, 0.3)',
                    }}
                  >
                    {cancellingId === order.orderId ? t('cancelling', language) : t('cancel', language)}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
