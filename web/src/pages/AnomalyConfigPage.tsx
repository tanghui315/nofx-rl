import { useEffect, useState } from 'react'
import { useAuth } from '../contexts/AuthContext'
import { api } from '../lib/api'

interface AnomalyConfig {
  mode: string
  sensitivity: string
  use_llm: boolean
  gambit_enabled: boolean
  is_enabled: boolean
  can_take_action: boolean
  gambit_config?: {
    enabled: boolean
    max_position_pct: number
    max_amount: number
    min_confidence: number
    leverage_multiplier: number
  }
}

interface AnomalyStatus {
  enabled: boolean
  mode: string
  sensitivity: string
  use_llm: boolean
  gambit_enabled: boolean
  description: string
}

export function AnomalyConfigPage() {
  const { token } = useAuth()
  const [config, setConfig] = useState<AnomalyConfig | null>(null)
  const [status, setStatus] = useState<AnomalyStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  
  // 表单状态
  const [mode, setMode] = useState<string>('watch')
  const [sensitivity, setSensitivity] = useState<string>('medium')
  const [useLLM, setUseLLM] = useState<boolean>(false)
  const [gambitEnabled, setGambitEnabled] = useState<boolean>(false)
  // 高级设置
  const [gambitMaxPosition, setGambitMaxPosition] = useState<number>(0.02)
  const [gambitMaxAmount, setGambitMaxAmount] = useState<number>(5000)
  const [gambitMinAmount, setGambitMinAmount] = useState<number>(50)
  const [gambitMinConfidence, setGambitMinConfidence] = useState<number>(0.8)
  const [gambitMaxStopLoss, setGambitMaxStopLoss] = useState<number>(0.03)
  const [gambitCoolingMinutes, setGambitCoolingMinutes] = useState<number>(60)
  const [showAdvanced, setShowAdvanced] = useState<boolean>(false)
  
  // 搏一搏风险确认弹窗
  const [showGambitConfirm, setShowGambitConfirm] = useState(false)
  const [gambitConfirmChecks, setGambitConfirmChecks] = useState({
    readRisk: false,
    hasExperience: false,
    understandLoss: false,
  })
  
  // 加载配置
  useEffect(() => {
    loadConfig()
    loadStatus()
  }, [])
  
  const loadConfig = async () => {
    try {
      const data = await api.get('/anomaly/config', token)
      setConfig(data)
      setMode(data.mode)
      setSensitivity(data.sensitivity)
      setUseLLM(data.use_llm)
      setGambitEnabled(data.gambit_enabled)
      const gc = data.gambit_config || {}
      if (gc) {
        if (typeof gc.max_position_pct === 'number') setGambitMaxPosition(gc.max_position_pct)
        if (typeof gc.max_amount === 'number') setGambitMaxAmount(gc.max_amount)
        if (typeof gc.min_amount === 'number') setGambitMinAmount(gc.min_amount)
        if (typeof gc.min_confidence === 'number') setGambitMinConfidence(gc.min_confidence)
        if (typeof gc.max_stop_loss === 'number') setGambitMaxStopLoss(gc.max_stop_loss)
        if (typeof gc.cooling_minutes === 'number') setGambitCoolingMinutes(gc.cooling_minutes)
      }
    } catch (error) {
      console.error('加载配置失败:', error)
      setMessage({ type: 'error', text: '加载配置失败' })
    } finally {
      setLoading(false)
    }
  }
  
  const loadStatus = async () => {
    try {
      const data = await api.get('/anomaly/status', token)
      setStatus(data)
    } catch (error) {
      console.error('加载状态失败:', error)
    }
  }
  
  const handleSave = async () => {
    setSaving(true)
    setMessage(null)
    
    try {
      await api.put('/anomaly/config', {
        mode,
        sensitivity,
        use_llm: useLLM,
        gambit_enabled: gambitEnabled,
        // 高级设置（可选传输）
        gambit_max_position: gambitEnabled ? gambitMaxPosition : undefined,
        gambit_max_amount: gambitEnabled ? gambitMaxAmount : undefined,
        gambit_min_amount: gambitEnabled ? gambitMinAmount : undefined,
        gambit_min_confidence: gambitEnabled ? gambitMinConfidence : undefined,
        gambit_max_stop_loss: gambitEnabled ? gambitMaxStopLoss : undefined,
        gambit_cooling_minutes: gambitEnabled ? gambitCoolingMinutes : undefined,
      }, token)
      
      setMessage({ type: 'success', text: '配置保存成功' })
      
      // 重新加载配置和状态
      await loadConfig()
      await loadStatus()
    } catch (error: any) {
      console.error('保存配置失败:', error)
      setMessage({ 
        type: 'error', 
        text: error.response?.data?.error || '保存配置失败' 
      })
    } finally {
      setSaving(false)
    }
  }
  
  const getModeDescription = (modeValue: string) => {
    const descriptions: { [key: string]: string } = {
      off: '不监控异常事件',
      watch: '只提醒，不自动操作（推荐新手）',
      guard: '自动减仓止损，不追涨（推荐）',
      balanced: '可追涨杀跌，小仓位试探',
      aggressive: '快速响应，加大仓位（高风险）',
    }
    return descriptions[modeValue] || ''
  }
  
  const getSensitivityDescription = (sensValue: string) => {
    const descriptions: { [key: string]: string } = {
      low: '只捕捉极端行情（5%+ 暴涨暴跌）',
      medium: '捕捉明显异常（3%+ 快速变化）',
      high: '捕捉早期信号（1.5%+ 异动）',
    }
    return descriptions[sensValue] || ''
  }
  
  if (loading) {
    return (
      <div className="flex items-center justify-center min-h-screen">
        <div className="text-[#9CA3AF]">加载中...</div>
      </div>
    )
  }
  
  return (
    <div className="py-8 px-4">
      <div className="max-w-4xl mx-auto">
        {/* 头部 */}
        <div className="mb-8">
          <h1 className="text-3xl font-bold text-[#EAECEF] mb-2 flex items-center gap-3">
            <span>⚡</span>
            <span>异常监控配置</span>
          </h1>
          <p className="text-[#9CA3AF]">
            实时监控市场异常，智能决策追涨杀跌
          </p>
        </div>
        
        {/* 当前状态 */}
        {status && (
          <div className={`mb-6 p-4 rounded-lg border ${
            status.enabled 
              ? 'bg-green-500/10 border-green-500/30' 
              : 'bg-gray-500/10 border-gray-500/30'
          }`}>
            <div className="flex items-center justify-between">
              <div>
                <div className="text-sm text-[#9CA3AF] mb-1">当前状态</div>
                <div className="text-lg font-semibold text-[#EAECEF]">
                  {status.description}
                </div>
              </div>
              <div className={`px-3 py-1 rounded-full text-sm font-medium ${
                status.enabled 
                  ? 'bg-green-500 text-white' 
                  : 'bg-gray-500 text-white'
              }`}>
                {status.enabled ? '已启用' : '已禁用'}
              </div>
            </div>
          </div>
        )}
        
        {/* 消息提示 */}
        {message && (
          <div className={`mb-6 p-4 rounded-lg border ${
            message.type === 'success'
              ? 'bg-green-500/10 border-green-500/30 text-green-400'
              : 'bg-red-500/10 border-red-500/30 text-red-400'
          }`}>
            {message.text}
          </div>
        )}
        
        {/* 配置表单 */}
        <div className="space-y-6">
          {/* 拨片 1: 防护模式 */}
          <div className="bg-[#0B0E11] border border-[#2B3139] rounded-lg p-6">
            <div className="mb-4">
              <h2 className="text-xl font-semibold text-[#EAECEF] mb-2">
                🎛️ 防护模式
              </h2>
              <p className="text-sm text-[#9CA3AF]">
                {getModeDescription(mode)}
              </p>
            </div>
            
            <div className="grid grid-cols-1 md:grid-cols-5 gap-3">
              {['off', 'watch', 'guard', 'balanced', 'aggressive'].map((modeValue) => (
                <button
                  key={modeValue}
                  onClick={() => setMode(modeValue)}
                  className={`p-4 rounded-lg border transition-all ${
                    mode === modeValue
                      ? 'bg-blue-500/20 border-blue-500 text-blue-400'
                      : 'bg-[#1a1d24] border-[#2B3139] text-[#9CA3AF] hover:border-[#3B4149]'
                  }`}
                >
                  <div className="text-2xl mb-2">
                    {modeValue === 'off' && '🔴'}
                    {modeValue === 'watch' && '🟡'}
                    {modeValue === 'guard' && '🟢'}
                    {modeValue === 'balanced' && '🔵'}
                    {modeValue === 'aggressive' && '🟣'}
                  </div>
                  <div className="text-sm font-medium capitalize">
                    {modeValue === 'off' && '关闭'}
                    {modeValue === 'watch' && '观察'}
                    {modeValue === 'guard' && '防守'}
                    {modeValue === 'balanced' && '平衡'}
                    {modeValue === 'aggressive' && '激进'}
                  </div>
                </button>
              ))}
            </div>
            
            {/* 模式说明 */}
            <div className="mt-4 p-3 bg-[#1a1d24] rounded-lg">
              <div className="text-sm text-[#9CA3AF] space-y-1">
                <div>• <span className="text-[#F59E0B]">观察</span>：只记录和提醒，不自动操作（适合新手）</div>
                <div>• <span className="text-[#10B981]">防守</span>：只减仓止损，不追涨（推荐）</div>
                <div>• <span className="text-[#3B82F6]">平衡</span>：可追涨杀跌，小仓位（需经验）</div>
                <div>• <span className="text-[#8B5CF6]">激进</span>：快速响应，大仓位（高风险）</div>
              </div>
            </div>
          </div>
          
          {/* 拨片 2: 灵敏度 */}
          <div className="bg-[#0B0E11] border border-[#2B3139] rounded-lg p-6">
            <div className="mb-4">
              <h2 className="text-xl font-semibold text-[#EAECEF] mb-2">
                📊 灵敏度
              </h2>
              <p className="text-sm text-[#9CA3AF]">
                {getSensitivityDescription(sensitivity)}
              </p>
            </div>
            
            <div className="space-y-4">
              {/* 滑块 */}
              <div className="relative">
                <input
                  type="range"
                  min="0"
                  max="2"
                  step="1"
                  value={sensitivity === 'low' ? 0 : sensitivity === 'medium' ? 1 : 2}
                  onChange={(e) => {
                    const val = parseInt(e.target.value)
                    setSensitivity(val === 0 ? 'low' : val === 1 ? 'medium' : 'high')
                  }}
                  className="w-full h-2 bg-[#2B3139] rounded-lg appearance-none cursor-pointer"
                  style={{
                    background: `linear-gradient(to right, #3B82F6 0%, #3B82F6 ${
                      sensitivity === 'low' ? 0 : sensitivity === 'medium' ? 50 : 100
                    }%, #2B3139 ${
                      sensitivity === 'low' ? 0 : sensitivity === 'medium' ? 50 : 100
                    }%, #2B3139 100%)`
                  }}
                />
                <div className="flex justify-between mt-2 text-sm">
                  <span className={sensitivity === 'low' ? 'text-[#3B82F6] font-medium' : 'text-[#9CA3AF]'}>
                    低
                  </span>
                  <span className={sensitivity === 'medium' ? 'text-[#3B82F6] font-medium' : 'text-[#9CA3AF]'}>
                    中
                  </span>
                  <span className={sensitivity === 'high' ? 'text-[#3B82F6] font-medium' : 'text-[#9CA3AF]'}>
                    高
                  </span>
                </div>
              </div>
              
              {/* 说明 */}
              <div className="p-3 bg-[#1a1d24] rounded-lg">
                <div className="text-sm text-[#9CA3AF] space-y-1">
                  <div>• <span className="text-white">低</span>：只捕捉极端行情（5%+ 暴涨暴跌）</div>
                  <div>• <span className="text-white">中</span>：捕捉明显异常（3%+ 快速变化）</div>
                  <div>• <span className="text-white">高</span>：捕捉早期信号（1.5%+ 异动）</div>
                  <div className="text-[#F59E0B] mt-2">
                    ⚠️ 灵敏度越高，触发越频繁，可能增加交易成本
                  </div>
                </div>
              </div>
            </div>
          </div>
          
          {/* 拨片 3: LLM 智能决策 */}
          <div className="bg-[#0B0E11] border border-[#2B3139] rounded-lg p-6">
            <div className="mb-4">
              <h2 className="text-xl font-semibold text-[#EAECEF] mb-2">
                🤖 LLM 智能决策
              </h2>
            </div>
            
            <div className="flex items-center justify-between p-4 bg-[#1a1d24] rounded-lg">
              <div className="flex-1">
                <div className="text-[#EAECEF] font-medium mb-1">
                  {useLLM ? '已开启' : '已关闭'}
                </div>
                <div className="text-sm text-[#9CA3AF]">
                  {useLLM 
                    ? 'LLM 会分析市场上下文，给出更合理的建议（响应时间 5-15 秒）'
                    : '使用预设规则快速决策（更快但机械）'
                  }
                </div>
              </div>
              <button
                onClick={() => setUseLLM(!useLLM)}
                className={`ml-4 relative inline-flex h-8 w-14 items-center rounded-full transition-colors ${
                  useLLM ? 'bg-blue-500' : 'bg-[#2B3139]'
                }`}
              >
                <span
                  className={`inline-block h-6 w-6 transform rounded-full bg-white transition-transform ${
                    useLLM ? 'translate-x-7' : 'translate-x-1'
                  }`}
                />
              </button>
            </div>
            
            <div className="mt-3 p-3 bg-[#1a1d24] rounded-lg text-sm text-[#9CA3AF]">
              💡 建议新手开启 LLM 决策，专业用户可选关闭以获得更快响应
            </div>
          </div>

          {/* 搏一搏模式 */}
          <div className="bg-[#0B0E11] border border-[#2B3139] rounded-lg p-6">
            <div className="mb-4 flex items-center justify-between">
              <h2 className="text-xl font-semibold text-[#EAECEF] mb-2">⚡ 搏一搏模式（高风险）</h2>
              <button
                onClick={() => setGambitEnabled(!gambitEnabled)}
                className={`ml-4 relative inline-flex h-8 w-14 items-center rounded-full transition-colors ${
                  gambitEnabled ? 'bg-blue-500' : 'bg-[#2B3139]'
                }`}
              >
                <span
                  className={`inline-block h-6 w-6 transform rounded-full bg-white transition-transform ${
                    gambitEnabled ? 'translate-x-7' : 'translate-x-1'
                  }`}
                />
              </button>
            </div>
            <p className="text-sm text-[#9CA3AF] mb-4">开启后，在极端行情下允许小仓位高杠杆试探性开仓。请谨慎使用。</p>

            {/* 高级设置折叠 */}
            <button
              onClick={() => setShowAdvanced(!showAdvanced)}
              className="text-sm text-blue-400 hover:underline mb-3"
            >
              {showAdvanced ? '隐藏高级设置' : '显示高级设置'}
            </button>

            {showAdvanced && (
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm text-[#9CA3AF] mb-1">最大仓位比例（%）</label>
                  <input
                    type="number"
                    step="0.01"
                    min={0.01}
                    max={0.2}
                    value={gambitMaxPosition}
                    onChange={(e) => setGambitMaxPosition(Math.max(0.01, Math.min(0.2, Number(e.target.value))))}
                    className="w-full bg-[#1a1d24] border border-[#2B3139] rounded px-3 py-2 text-[#EAECEF]"
                  />
                  <div className="text-xs text-[#6B7280] mt-1">建议 0.01–0.05（1%–5%）</div>
                </div>
                <div>
                  <label className="block text-sm text-[#9CA3AF] mb-1">最大金额（USDT）</label>
                  <input
                    type="number"
                    min={10}
                    value={gambitMaxAmount}
                    onChange={(e) => setGambitMaxAmount(Math.max(10, Number(e.target.value)))}
                    className="w-full bg-[#1a1d24] border border-[#2B3139] rounded px-3 py-2 text-[#EAECEF]"
                  />
                </div>
                <div>
                  <label className="block text-sm text-[#9CA3AF] mb-1">最小金额（USDT）</label>
                  <input
                    type="number"
                    min={10}
                    value={gambitMinAmount}
                    onChange={(e) => setGambitMinAmount(Math.max(10, Number(e.target.value)))}
                    className="w-full bg-[#1a1d24] border border-[#2B3139] rounded px-3 py-2 text-[#EAECEF]"
                  />
                  <div className="text-xs text-[#6B7280] mt-1">低于该值将跳过执行，避免过小单失败</div>
                </div>
                <div>
                  <label className="block text-sm text-[#9CA3AF] mb-1">最小置信度</label>
                  <input
                    type="number"
                    min={0}
                    max={1}
                    step={0.05}
                    value={gambitMinConfidence}
                    onChange={(e) => setGambitMinConfidence(Math.max(0, Math.min(1, Number(e.target.value))))}
                    className="w-full bg-[#1a1d24] border border-[#2B3139] rounded px-3 py-2 text-[#EAECEF]"
                  />
                </div>
                <div>
                  <label className="block text-sm text-[#9CA3AF] mb-1">最大止损（%）</label>
                  <input
                    type="number"
                    min={0.5}
                    max={20}
                    step={0.5}
                    value={gambitMaxStopLoss * 100}
                    onChange={(e) => setGambitMaxStopLoss(Math.max(0.005, Math.min(0.2, Number(e.target.value)/100)))}
                    className="w-full bg-[#1a1d24] border border-[#2B3139] rounded px-3 py-2 text-[#EAECEF]"
                  />
                </div>
                <div>
                  <label className="block text-sm text-[#9CA3AF] mb-1">冷却期（分钟）</label>
                  <input
                    type="number"
                    min={0}
                    max={360}
                    step={1}
                    value={gambitCoolingMinutes}
                    onChange={(e) => setGambitCoolingMinutes(Math.max(0, Math.min(360, Number(e.target.value))))}
                    className="w-full bg-[#1a1d24] border border-[#2B3139] rounded px-3 py-2 text-[#EAECEF]"
                  />
                </div>
              </div>
            )}
          </div>

          {/* 拨片 4: 搏一搏模式 */}
          <div className="bg-[#0B0E11] border border-[#2B3139] rounded-lg p-6">
            <div className="mb-4">
              <h2 className="text-xl font-semibold text-[#EAECEF] mb-2 flex items-center gap-2">
                <span>⚡</span>
                <span>搏一搏模式</span>
                <span className="text-xs px-2 py-1 bg-red-500/20 text-red-400 rounded-full">
                  高风险
                </span>
              </h2>
            </div>
            
            <div className="flex items-center justify-between p-4 bg-[#1a1d24] rounded-lg mb-4">
              <div className="flex-1">
                <div className="text-[#EAECEF] font-medium mb-1">
                  {gambitEnabled ? '已开启' : '已关闭'}
                </div>
                <div className="text-sm text-[#9CA3AF]">
                  {gambitEnabled 
                    ? '允许极端行情下小仓位高杠杆试探'
                    : '不使用高杠杆试探'
                  }
                </div>
              </div>
              <button
                onClick={() => {
                  if (!gambitEnabled) {
                    // 首次开启，显示确认弹窗
                    setShowGambitConfirm(true)
                  } else {
                    // 关闭，直接执行
                    setGambitEnabled(false)
                  }
                }}
                disabled={(mode !== 'balanced' && mode !== 'aggressive') || !useLLM}
                className={`ml-4 relative inline-flex h-8 w-14 items-center rounded-full transition-colors ${
                  gambitEnabled ? 'bg-red-500' : 'bg-[#2B3139]'
                } ${(mode !== 'balanced' && mode !== 'aggressive') || !useLLM ? 'opacity-50 cursor-not-allowed' : ''}`}
              >
                <span
                  className={`inline-block h-6 w-6 transform rounded-full bg-white transition-transform ${
                    gambitEnabled ? 'translate-x-7' : 'translate-x-1'
                  }`}
                />
              </button>
            </div>
            
            {mode !== 'balanced' && mode !== 'aggressive' && (
              <div className="mb-4 p-3 bg-yellow-500/10 border border-yellow-500/30 rounded-lg text-sm text-yellow-400">
                ⚠️ 搏一搏模式仅在"平衡"或"激进"模式下可用
              </div>
            )}
            
            {!useLLM && (mode === 'balanced' || mode === 'aggressive') && (
              <div className="mb-4 p-3 bg-yellow-500/10 border border-yellow-500/30 rounded-lg text-sm text-yellow-400">
                ⚠️ 搏一搏模式需要开启 LLM 智能决策
              </div>
            )}
            
            <div className="p-4 bg-red-500/10 border border-red-500/30 rounded-lg">
              <div className="text-sm text-red-400 space-y-2">
                <div className="font-medium mb-2">⚠️ 危险提示：</div>
                <div>• 搏一搏会在极端行情时使用 1-2% 仓位配合高杠杆（最高 50x）</div>
                <div>• 虽然仓位很小，但杠杆极高，判断错误会快速亏损</div>
                <div>• 系统会严格控制：仓位 ≤ 2%，资金 ≤ $5,000，必须止损 ≤ 3%</div>
                <div>• 需要 LLM 置信度 ≥ 80% 才会执行</div>
              </div>
              
              {config?.gambit_config && gambitEnabled && (
                <div className="mt-4 pt-4 border-t border-red-500/30">
                  <div className="text-sm text-[#9CA3AF] space-y-1">
                    <div>最大仓位：{(config.gambit_config.max_position_pct * 100).toFixed(1)}%</div>
                    <div>最大金额：${config.gambit_config.max_amount.toLocaleString()}</div>
                    <div>杠杆倍数：交易员杠杆 × {config.gambit_config.leverage_multiplier} (上限 50x)</div>
                    <div>最小置信度：{(config.gambit_config.min_confidence * 100).toFixed(0)}%</div>
                  </div>
                </div>
              )}
            </div>
          </div>
          
          {/* 保存按钮 */}
          <div className="flex gap-4">
            <button
              onClick={handleSave}
              disabled={saving}
              className="flex-1 bg-blue-500 hover:bg-blue-600 disabled:bg-gray-600 disabled:cursor-not-allowed text-white font-medium py-3 px-6 rounded-lg transition-colors"
            >
              {saving ? '保存中...' : '保存配置'}
            </button>
            
            <button
              onClick={() => {
                setMode('guard')
                setSensitivity('medium')
                setUseLLM(true)
                setGambitEnabled(false)
              }}
              className="px-6 py-3 border border-[#2B3139] text-[#9CA3AF] hover:text-[#EAECEF] hover:border-[#3B4149] rounded-lg transition-colors"
            >
              恢复推荐配置
            </button>
          </div>
        </div>
        
        {/* 搏一搏风险确认弹窗 */}
        {showGambitConfirm && (
          <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
            <div className="bg-[#0B0E11] border border-[#2B3139] rounded-lg p-6 max-w-lg w-full mx-4">
              <h2 className="text-2xl font-bold text-[#EAECEF] mb-4 flex items-center gap-2">
                <span>⚠️</span>
                <span>搏一搏模式风险提示</span>
              </h2>
              
              <div className="text-[#9CA3AF] space-y-3 mb-6">
                <p>
                  搏一搏模式会在极端行情时，使用小仓位配合高杠杆试探入场。这是一种高风险策略，请确保你理解以下风险：
                </p>
                
                <div className="space-y-2 text-sm">
                  <div className="text-red-400">⚠️ 虽然仓位很小（1-2%），但杠杆极高（最高50x）</div>
                  <div className="text-red-400">⚠️ 如果判断错误，这部分资金可能在几分钟内归零</div>
                  <div className="text-red-400">⚠️ 极端行情波动剧烈，止损可能无法精确执行</div>
                  <div className="text-red-400">⚠️ 频繁使用会增加交易成本（手续费）</div>
                </div>
                
                <div className="mt-4 pt-4 border-t border-[#2B3139]">
                  <div className="text-sm text-[#10B981] font-medium mb-2">✅ 安全措施：</div>
                  <ul className="text-sm space-y-1 text-[#9CA3AF]">
                    <li>• 仓位限制：最多 2% 资金</li>
                    <li>• 强制止损：最大亏损 3%</li>
                    <li>• LLM 评估：置信度 ≥ 80% 才执行</li>
                    <li>• 自动平仓：超过设定时间自动平仓</li>
                    <li>• 冷却期：60 分钟内不会再次触发</li>
                  </ul>
                </div>
              </div>
              
              <div className="space-y-3 mb-6">
                <label className="flex items-center gap-3 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={gambitConfirmChecks.readRisk}
                    onChange={(e) => setGambitConfirmChecks({...gambitConfirmChecks, readRisk: e.target.checked})}
                    className="w-5 h-5 rounded border-[#2B3139] bg-[#1a1d24] text-blue-500 focus:ring-blue-500"
                  />
                  <span className="text-[#EAECEF]">我已阅读并理解上述风险</span>
                </label>
                
                <label className="flex items-center gap-3 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={gambitConfirmChecks.hasExperience}
                    onChange={(e) => setGambitConfirmChecks({...gambitConfirmChecks, hasExperience: e.target.checked})}
                    className="w-5 h-5 rounded border-[#2B3139] bg-[#1a1d24] text-blue-500 focus:ring-blue-500"
                  />
                  <span className="text-[#EAECEF]">我确认我有足够的交易经验</span>
                </label>
                
                <label className="flex items-center gap-3 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={gambitConfirmChecks.understandLoss}
                    onChange={(e) => setGambitConfirmChecks({...gambitConfirmChecks, understandLoss: e.target.checked})}
                    className="w-5 h-5 rounded border-[#2B3139] bg-[#1a1d24] text-blue-500 focus:ring-blue-500"
                  />
                  <span className="text-[#EAECEF]">我理解这部分资金可能全部亏损</span>
                </label>
              </div>
              
              <div className="flex gap-4">
                <button
                  onClick={() => {
                    const allChecked = gambitConfirmChecks.readRisk && 
                                     gambitConfirmChecks.hasExperience && 
                                     gambitConfirmChecks.understandLoss
                    if (allChecked) {
                      setGambitEnabled(true)
                      setShowGambitConfirm(false)
                      setGambitConfirmChecks({readRisk: false, hasExperience: false, understandLoss: false})
                    }
                  }}
                  disabled={!gambitConfirmChecks.readRisk || !gambitConfirmChecks.hasExperience || !gambitConfirmChecks.understandLoss}
                  className="flex-1 bg-red-500 hover:bg-red-600 disabled:bg-gray-600 disabled:cursor-not-allowed text-white font-medium py-3 px-6 rounded-lg transition-colors"
                >
                  确认开启
                </button>
                
                <button
                  onClick={() => {
                    setShowGambitConfirm(false)
                    setGambitConfirmChecks({readRisk: false, hasExperience: false, understandLoss: false})
                  }}
                  className="px-6 py-3 border border-[#2B3139] text-[#9CA3AF] hover:text-[#EAECEF] hover:border-[#3B4149] rounded-lg transition-colors"
                >
                  取消
                </button>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
