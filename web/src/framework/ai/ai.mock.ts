import i18n from 'i18next'
import { DEMO_TENANT_ID, aiPolicyOf } from '@/framework/modules/tenant/tenant.mock'
import { defineMock, mockError, sleep, type MockContext } from '@/framework/request/mock'

type Scope = 'user' | 'notice' | 'operLog'

function readSettings(): { enabled: boolean; model: string } {
  const def = { enabled: true, model: 'qwen-plus' }
  try {
    const raw = localStorage.getItem('mock-settings')
    if (!raw) return def
    const ai = JSON.parse(raw)?.ai
    if (!ai || typeof ai !== 'object') return def
    return { enabled: ai.enabled !== false, model: typeof ai.model === 'string' && ai.model ? ai.model : def.model }
  } catch {
    return def
  }
}

const pad = (n: number) => String(n).padStart(2, '0')
const ymd = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
const addDays = (d: Date, n: number) => new Date(d.getFullYear(), d.getMonth(), d.getDate() + n)

const CN_NUM: Record<string, number> = { 一: 1, 二: 2, 三: 3, 四: 4, 五: 5, 六: 6, 七: 7, 八: 8, 九: 9, 十: 10 }

/** 解析时间范围，返回 [from, to, 描述]；无时间词返回 null */
function parseRange(text: string): [string, string, string] | null {
  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  if (/昨天/.test(text)) return [ymd(addDays(today, -1)), ymd(addDays(today, -1)), '昨天']
  if (/今天/.test(text)) return [ymd(today), ymd(today), '今天']
  const recent = text.match(/(?:最近|近|过去)\s*(\d+|[一二三四五六七八九十])\s*天/)
  if (recent) {
    const n = /\d/.test(recent[1]) ? Number(recent[1]) : CN_NUM[recent[1]]
    return [ymd(addDays(today, -(n - 1))), ymd(today), `最近 ${n} 天`]
  }
  if (/上个?月/.test(text)) {
    return [ymd(new Date(now.getFullYear(), now.getMonth() - 1, 1)), ymd(new Date(now.getFullYear(), now.getMonth(), 0)), '上个月']
  }
  if (/本月|这个月/.test(text)) {
    return [ymd(new Date(now.getFullYear(), now.getMonth(), 1)), ymd(new Date(now.getFullYear(), now.getMonth() + 1, 0)), '本月']
  }
  if (/本周|这周|这一周/.test(text)) {
    const offset = (today.getDay() + 6) % 7
    return [ymd(addDays(today, -offset)), ymd(addDays(today, 6 - offset)), '本周']
  }
  return null
}

function parseKeyword(text: string): string | undefined {
  const quoted = text.match(/[「"“'‘]([^」"”'’]+)[」"”'’]/)
  if (quoted) return quoted[1].trim()
  const after = text.match(/(?:叫|姓|包含|含有|关键字|关键词)\s*([^\s,，。；;的]+)/)
  return after ? after[1].trim() : undefined
}

interface Parsed {
  filters: Record<string, string>
  parts: string[]
}

function pick(text: string, rules: [RegExp, string, string][], key: string, p: Parsed) {
  for (const [re, value, label] of rules) {
    if (re.test(text)) {
      p.filters[key] = value
      p.parts.push(label)
      return
    }
  }
}

function parse(scope: Scope, text: string): Parsed {
  const p: Parsed = { filters: {}, parts: [] }
  const range = parseRange(text)
  const rangeKeys: Record<Scope, [string, string]> = {
    user: ['createdFrom', 'createdTo'],
    notice: ['', ''],
    operLog: ['startDate', 'endDate'],
  }

  if (scope === 'user') {
    pick(text, [[/停用|禁用/, '0', '状态：停用'], [/启用|正常/, '1', '状态：启用']], 'status', p)
    pick(text, [[/男/, '1', '性别：男'], [/女/, '2', '性别：女']], 'gender', p)
  } else if (scope === 'notice') {
    pick(text, [[/通知/, 'notice', '类型：通知'], [/公告/, 'announcement', '类型：公告'], [/活动/, 'activity', '类型：活动']], 'type', p)
    pick(text, [[/重要/, 'important', '级别：重要'], [/普通/, 'normal', '级别：普通']], 'level', p)
    pick(text, [[/草稿/, 'draft', '状态：草稿'], [/已发布|发布/, 'published', '状态：已发布'], [/已撤回|撤回/, 'revoked', '状态：已撤回']], 'status', p)
  } else {
    pick(
      text,
      [
        [/新增|新建|创建/, 'create', '操作类型：新增'],
        [/修改|更新|编辑/, 'update', '操作类型：修改'],
        [/删除/, 'delete', '操作类型：删除'],
        [/导出/, 'export', '操作类型：导出'],
        [/导入/, 'import', '操作类型：导入'],
        [/查询/, 'other', '操作类型：查询'],
      ],
      'type',
      p,
    )
    pick(text, [[/成功/, 'success', '状态：成功'], [/失败|异常|出错/, 'failed', '状态：失败']], 'status', p)
  }

  const [fromKey, toKey] = rangeKeys[scope]
  if (range && fromKey) {
    p.filters[fromKey] = range[0]
    p.filters[toKey] = range[1]
    p.parts.push(`时间：${range[2]}（${range[0]} ~ ${range[1]}）`)
  }

  const kw = parseKeyword(text)
  if (kw) {
    p.filters.keyword = kw
    p.parts.push(`关键词：${kw}`)
  }
  return p
}

/**
 * 租户级开关与每日配额（与后端一致）：超管（admin）不受限；演示账号 demo 属于演示租户，
 * 受该租户的 aiEnabled / aiDailyQuota（租户覆盖优先，其次套餐）限制。计数在内存里，刷新页面即清零。
 */
const usage = new Map<string, number>()

function tenantOf(headers: MockContext['headers']): string | null {
  const username = (headers.authorization ?? '').replace(/^Bearer\s+/i, '').split('.')[2]
  return username === 'demo' ? DEMO_TENANT_ID : null
}

function tenantAllowed(headers: MockContext['headers']): boolean {
  const id = tenantOf(headers)
  return !id || !aiPolicyOf(id)?.disabled
}

/** 通过则计入一次今日用量；被关闭抛 403 TENANT_AI_DISABLED，超额抛 429 TENANT_QUOTA_AI */
function guard(headers: MockContext['headers']) {
  const id = tenantOf(headers)
  const policy = id ? aiPolicyOf(id) : null
  if (!id || !policy) return
  if (policy.disabled) mockError(403, i18n.t('ai.error.tenantDisabled'), 'TENANT_AI_DISABLED')
  if (policy.dailyQuota <= 0) return
  const key = `${id}:${ymd(new Date())}`
  const n = (usage.get(key) ?? 0) + 1
  usage.set(key, n)
  if (n > policy.dailyQuota) mockError(429, i18n.t('ai.error.quotaExceeded', { count: policy.dailyQuota }), 'TENANT_QUOTA_AI')
}

const escapeHtml = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')

/** 规则式润色：整理空白与标点，按语气做简单替换；html 只处理文本节点，保留标签 */
function polishText(text: string, tone: string): string {
  let s = text.replace(/[ \t\u3000]+/g, ' ').replace(/ *\n */g, '\n').trim()
  if (/[\u4e00-\u9fa5]/.test(s)) {
    s = s.replace(/,/g, '，').replace(/;/g, '；').replace(/!/g, '！').replace(/\?/g, '？').replace(/([\u4e00-\u9fa5]) ([\u4e00-\u9fa5])/g, '$1$2')
    if (tone === 'formal') s = s.replace(/你们/g, '各位').replace(/你/g, '您').replace(/马上/g, '立即')
    if (tone === 'concise') s = s.replace(/非常|十分|特别|真的|其实|比较/g, '')
    if (tone === 'friendly') s = s.replace(/务必/g, '请').replace(/必须/g, '请')
    if (s && !/[。！？.!?>）)」]$/.test(s)) s += '。'
  } else {
    s = s.replace(/ +([,.;!?])/g, '$1').replace(/\bi\b/g, 'I')
    if (tone === 'formal') s = s.replace(/\bcan't\b/gi, 'cannot').replace(/\bwon't\b/gi, 'will not').replace(/\bASAP\b/g, 'as soon as possible')
    if (tone === 'concise') s = s.replace(/\b(very|really|just|actually|quite)\s+/gi, '')
    if (s) s = s[0].toUpperCase() + s.slice(1)
    if (s && !/[.!?>)]$/.test(s)) s += '.'
  }
  return s
}

function polish(text: string, tone: string, format: string): string {
  if (format !== 'html') return polishText(text, tone)
  return text.replace(/(^|>)([^<>]+)(?=<|$)/g, (m, lead: string, seg: string) => (seg.trim() ? `${lead}${polishText(seg, tone)}` : m))
}

function draft(brief: string, target: string, tone: string, format: string): string {
  const lines = brief.split(/\n+/).map((l) => polishText(l, tone)).filter(Boolean)
  if (target === 'title') return i18n.t('ai.write.mock.title', { text: brief.replace(/[。.!！\n]+$/g, '').split('\n')[0].slice(0, 60) })
  const paras = [i18n.t('ai.write.mock.greeting'), ...lines, i18n.t('ai.write.mock.closing')]
  return format === 'html' ? paras.map((p) => `<p>${escapeHtml(p)}</p>`).join('') : paras.join('\n\n')
}

/** 译文模拟：纯文本加前缀；html 只在文本节点前加前缀，保留标签 */
function fakeTranslate(text: string, to: string, format: string): string {
  if (format !== 'html') return `[${to}] ${text}`
  return text.replace(/(^|>)([^<>]+)(?=<|$)/g, (m, lead: string, s: string) => (s.trim() ? `${lead}[${to}] ${s}` : m))
}

defineMock([
  {
    method: 'GET',
    url: '/ai/status',
    handler: ({ headers }) => {
      const st = readSettings()
      return { ...st, enabled: st.enabled && tenantAllowed(headers) }
    },
  },
  {
    method: 'POST',
    url: '/ai/filter',
    handler: async ({ body, headers }) => {
      guard(headers)
      await sleep(600 + Math.floor(Math.random() * 300))
      const scope = (body?.scope ?? 'user') as Scope
      const text = String(body?.text ?? '').trim()
      const { filters, parts } = parse(scope, text)
      if (!Object.keys(filters).length) return { filters: {}, explanation: '没理解，请换种说法' }
      return { filters, explanation: parts.join('；') }
    },
  },
  {
    method: 'POST',
    url: '/ai/translate',
    handler: async ({ body, headers }) => {
      guard(headers)
      await sleep(700)
      return { text: fakeTranslate(String(body?.text ?? ''), String(body?.to ?? ''), String(body?.format ?? 'text')) }
    },
  },
  {
    method: 'POST',
    url: '/ai/write',
    handler: async ({ body, headers }) => {
      const mode = String(body?.mode ?? '')
      const text = String(body?.text ?? '').trim()
      const target = String(body?.target ?? 'body')
      const tone = String(body?.tone ?? '')
      const format = String(body?.format ?? 'text')
      if ((mode !== 'write' && mode !== 'polish') || !text) mockError(400, 'Bad request')
      if (text.length > 4000) mockError(400, 'Input is too long')
      guard(headers)
      await sleep(800)
      if (mode === 'polish') {
        const out = polish(text, tone, target === 'title' ? 'text' : format)
        return { text: target === 'title' ? out.replace(/[。.]$/, '').replace(/\n+/g, ' ') : out }
      }
      return { text: draft(text, target, tone, format) }
    },
  },
])
