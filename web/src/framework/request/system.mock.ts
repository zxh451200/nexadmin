import { defineMock } from './mock'

// 初始化状态存 localStorage：mock-initialized = 'true' | 'false'
// 未设置时：VITE_MOCK_INITIALIZED=true 视为已初始化，否则未初始化。
export function isMockInitialized(): boolean {
  const v = localStorage.getItem('mock-initialized')
  if (v === 'true') return true
  if (v === 'false') return false
  return import.meta.env.VITE_MOCK_INITIALIZED === 'true'
}

export function setMockInitialized(v: boolean) {
  localStorage.setItem('mock-initialized', String(v))
}

defineMock([{ method: 'GET', url: '/system/status', handler: () => ({ initialized: isMockInitialized() }) }])
