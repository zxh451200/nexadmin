import { defineMock, mockError } from '@/framework/request/mock'
import type { AllSettings, PublicSettings, SectionKey, TestResult } from './api'

const MASK = '********'
const STORE_KEY = 'mock-settings'

const provider = () => ({ enabled: false, clientId: '', clientSecret: '' })

const defaults = (): AllSettings => ({
  site: {
    siteName: 'NexAdmin',
    logoUrl: '',
    copyright: '© 2026 NexAdmin',
    defaultLanguage: 'zh-CN',
    watermark: false,
  },
  security: { maxAttempts: '5', lockMinutes: '15' },
  cache: { type: 'memory', address: '', password: '' },
  storage: { type: 'local', endpoint: '', bucket: '', region: '', accessKey: '', secretKey: '' },
  ai: { enabled: true, baseUrl: 'https://api.example.com/v1', apiKey: '', model: 'qwen-plus', timeoutSec: 30, redact: true },
  sms: { provider: 'none', accessKey: '', secretKey: '', signName: '', templateCode: '' },
  notify: {
    mail: { host: '', port: 587, security: 'starttls', username: '', password: '', fromName: '', fromAddr: '' },
    robots: [],
    accountMail: false,
  },
  auth: {
    account: true,
    phone: false,
    providers: {
      wecom: provider(),
      dingtalk: provider(),
      feishu: provider(),
      google: provider(),
      github: provider(),
      oidc: { ...provider(), issuer: '' },
    },
    matchBy: 'phone',
    autoCreate: false,
    allowedDomains: '',
    proxy: '',
  },
})

// 浅层按 section 合并默认值，保证缺字段时有兜底
function load(): AllSettings {
  const base = defaults()
  try {
    const raw = localStorage.getItem(STORE_KEY)
    if (!raw) return base
    const saved = JSON.parse(raw) as Partial<AllSettings>
    const out = { ...base } as Record<string, unknown>
    for (const k of Object.keys(base) as SectionKey[]) {
      if (saved[k] && typeof saved[k] === 'object') out[k] = deepAssign(base[k], saved[k])
    }
    return out as unknown as AllSettings
  } catch {
    return base
  }
}

function deepAssign<T>(base: T, patch: unknown): T {
  if (!patch || typeof patch !== 'object' || Array.isArray(patch)) return base
  const out: Record<string, unknown> = { ...(base as Record<string, unknown>) }
  for (const [k, v] of Object.entries(patch as Record<string, unknown>)) {
    out[k] =
      v && typeof v === 'object' && !Array.isArray(v) && out[k] && typeof out[k] === 'object'
        ? deepAssign(out[k], v)
        : v
  }
  return out as T
}

let store: AllSettings = load()

const persist = () => {
  try {
    localStorage.setItem(STORE_KEY, JSON.stringify(store))
  } catch {
    // 存储不可用时仅保留内存数据
  }
}

// 递归：值为占位符则保留旧值
function keepSecrets(next: unknown, prev: unknown): unknown {
  if (next === MASK) return prev ?? ''
  if (Array.isArray(next)) {
    // 列表（机器人）按 name 找回旧值，删除 / 重排后不会串号
    const old = Array.isArray(prev) ? (prev as Record<string, unknown>[]) : []
    return next.map((e) => {
      const name = e && typeof e === 'object' ? (e as Record<string, unknown>).name : undefined
      return keepSecrets(e, name === undefined ? undefined : old.find((o) => o.name === name))
    })
  }
  if (next && typeof next === 'object' && !Array.isArray(next)) {
    const p = (prev && typeof prev === 'object' ? prev : {}) as Record<string, unknown>
    return Object.fromEntries(Object.entries(next).map(([k, v]) => [k, keepSecrets(v, p[k])]))
  }
  return next
}

const SECRET_FIELDS = new Set(['password', 'secretKey', 'apiKey', 'clientSecret'])

// 递归：已设置的密钥返回占位符
function maskSecrets(v: unknown): unknown {
  if (Array.isArray(v)) return v.map(maskSecrets)
  if (!v || typeof v !== 'object') return v
  return Object.fromEntries(
    Object.entries(v).map(([k, val]) => [
      k,
      SECRET_FIELDS.has(k) && typeof val === 'string' && val ? MASK : maskSecrets(val),
    ]),
  )
}

/** 登录锁定策略（auth.mock 读取） */
export function getSecurity(): { maxAttempts: number; lockMinutes: number } {
  return { maxAttempts: Number(store.security.maxAttempts) || 5, lockMinutes: Number(store.security.lockMinutes) || 15 }
}

const isSection = (s: string): s is SectionKey =>
  ['site', 'security', 'cache', 'storage', 'ai', 'sms', 'auth', 'notify'].includes(s)

interface NotifyForm {
  to?: string
  mail?: { host?: string; fromAddr?: string }
  robots?: { name: string; url?: string }[]
}

// 用待测试的表单值（占位符回落到已保存的值）模拟连接检查
function runTest(section: SectionKey, body: unknown): TestResult {
  const v = keepSecrets(body, store[section]) as Record<string, unknown> & NotifyForm
  switch (section) {
    case 'cache':
      if (v.type !== 'valkey') return { ok: true }
      return v.address ? { ok: true } : { ok: false, reason: 'noAddress' }
    case 'storage':
      if (v.type !== 's3') return { ok: true }
      return v.endpoint && v.bucket && v.accessKey && v.secretKey ? { ok: true } : { ok: false, reason: 'incomplete' }
    case 'ai':
      return v.baseUrl && v.apiKey && v.model ? { ok: true } : { ok: false, reason: 'incomplete' }
    case 'sms':
      if (v.provider === 'none') return { ok: false, reason: 'noProvider' }
      if (v.provider === 'log') return { ok: true }
      return v.accessKey && v.secretKey ? { ok: true } : { ok: false, reason: 'incomplete' }
    case 'notify': {
      // 与后端一致：channel = mail | robot:<name>；mock 只检查配置是否完整，不会真的发送
      const ch = String(v.channel ?? '')
      if (ch === 'mail') {
        if (!v.mail?.host || !v.mail?.fromAddr) return { ok: false, reason: 'incomplete' }
        return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(String(v.to ?? '')) ? { ok: true } : { ok: false, reason: 'noRecipient' }
      }
      const r = (v.robots ?? []).find((x) => `robot:${x.name}` === ch)
      if (!r) return { ok: false, reason: 'robotNotFound' }
      return /^https:\/\//.test(r.url ?? '') ? { ok: true } : { ok: false, reason: 'incomplete' }
    }
    default:
      return { ok: true }
  }
}

defineMock([
  // 公开端点必须排在 :section 之前以免被通配
  {
    method: 'GET',
    url: '/settings/public',
    handler: (): PublicSettings => ({
      siteName: store.site.siteName,
      logoUrl: store.site.logoUrl,
      copyright: store.site.copyright,
      watermark: !!store.site.watermark,
    }),
  },
  { method: 'GET', url: '/settings', handler: () => maskSecrets(store) },
  {
    method: 'PUT',
    url: '/settings/:section',
    handler: ({ params, body }) => {
      if (!isSection(params.section)) mockError(404, 'Unknown section')
      const key = params.section
      const merged = keepSecrets(body ?? {}, store[key])
      store = { ...store, [key]: deepAssign(store[key], merged) }
      persist()
      return maskSecrets(store[key])
    },
  },
  {
    method: 'POST',
    url: '/settings/:section/test',
    handler: ({ params, body }) => {
      if (!isSection(params.section)) mockError(404, 'Unknown section')
      return runTest(params.section, body ?? {})
    },
  },
])
