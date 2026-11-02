import { defineMock, mockError } from '@/framework/request/mock'
import type { ParseResult, Verification, VerifyConfig, VerifyResult, VerifyStatus } from './api'

// 开发期假数据（VITE_MOCK=true 时由 mock-loader 自动装载）。
// 校验位与归属地的口径刻意跟 pkg/idcard 对齐：假数据不能比真后端更宽松，
// 否则前端看着对、接真后端就改判。

const WEIGHTS = [7, 9, 10, 5, 8, 4, 2, 1, 6, 3, 7, 9, 10, 5, 8, 4, 2]
const CHECK_SET = '10X98765432'

// 只放样本用到的几个区划，其余按「码不存在」处理，与后端的未知码分支同形。
const REGIONS: Record<string, string[]> = {
  '110101': ['北京市', '市辖区', '东城区'],
  '110105': ['北京市', '市辖区', '朝阳区'],
  '330106': ['浙江省', '杭州市', '西湖区'],
  '440304': ['广东省', '深圳市', '福田区'],
  '500154': ['重庆市', '市辖区', '开州区'],
  '610104': ['陕西省', '西安市', '莲湖区'],
  '810000': ['香港特别行政区'],
}
const HISTORICAL = new Set(['500234'])

const normalize = (raw: string) =>
  raw
    .replace(/[\s\u3000\-_]/g, '')
    .replace(/[０-９]/g, (d) => String(d.charCodeAt(0) - 0xff10))
    .replace(/ｘ/g, 'X')
    .replace(/x$/i, 'X')

const checksum = (body: string) => {
  let sum = 0
  for (let i = 0; i < 17; i++) sum += Number(body[i]) * WEIGHTS[i]
  return CHECK_SET[sum % 11]
}

const maskId = (s: string) => (s.length <= 10 ? '***' : `${s.slice(0, 6)}${'*'.repeat(s.length - 10)}${s.slice(-4)}`)
const maskName = (s: string) => (s.length <= 1 ? `${s}**` : `${s.slice(0, 1)}${'*'.repeat(s.length - 1)}`)

/** 与 idcard.Parse 同一判定顺序：长度 → 字符 → 区划 → 生日 → 未来 → 校验位。 */
function parseNumber(raw: string): { ok: true; info: ParseResult } | { ok: false; reason: string; masked: string } {
  let n = normalize(raw)
  const masked = maskId(n)
  if (n === '') return { ok: false, reason: 'empty', masked }
  let legacy = false
  if (n.length === 15) {
    legacy = true
    if (!/^\d{15}$/.test(n)) return { ok: false, reason: 'chars', masked }
    n = `${n.slice(0, 6)}19${n.slice(6)}`
    n += checksum(n)
  } else if (n.length !== 18) {
    return { ok: false, reason: 'length', masked }
  }
  if (!/^\d{17}[0-9X]$/.test(n)) return { ok: false, reason: 'chars', masked }

  const code = n.slice(0, 6)
  const names = REGIONS[code]
  const historical = HISTORICAL.has(code)
  if (!names && !historical) return { ok: false, reason: 'region', masked }

  const y = Number(n.slice(6, 10))
  const mo = Number(n.slice(10, 12))
  const da = Number(n.slice(12, 14))
  const birthday = `${n.slice(6, 10)}-${n.slice(10, 12)}-${n.slice(12, 14)}`
  const validDay = mo >= 1 && mo <= 12 && da >= 1 && da <= new Date(y, mo, 0).getDate()
  if (y < 1900 || !validDay) return { ok: false, reason: 'birthday', masked }
  if (new Date(`${birthday}T00:00:00Z`) > new Date()) return { ok: false, reason: 'birthday_future', masked }
  if (!legacy && checksum(n.slice(0, 17)) !== n[17]) return { ok: false, reason: 'checksum', masked }

  // 与 Info.AgeAt 同一口径：生日当天算已满岁，闰日出生在非闰年按 3 月 1 日长岁。
  const today = new Date()
  const birthdayThisYear = new Date(`${birthday}T00:00:00Z`)
  birthdayThisYear.setUTCFullYear(today.getUTCFullYear())
  const age = Math.max(0, today.getUTCFullYear() - y - (today < birthdayThisYear ? 1 : 0))
  return {
    ok: true,
    info: {
      status: 'valid',
      numberMasked: maskId(n),
      legacy,
      regionCode: code,
      regionNames: historical ? [names?.[0] ?? '已撤销区划'] : names,
      regionHistorical: historical,
      birthday,
      sex: Number(n[16]) % 2 === 1 ? 'male' : 'female',
      age,
    },
  }
}

const now = () => new Date().toISOString()
const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString()

// 假上游的"实名花名册"：只有这里成对登记过的姓名+号码才会 match。
const ROSTER: Record<string, string> = {
  '11010119900307803X': '张三丰',
  '330106199001010018': '李四',
}

let config: VerifyConfig = {
  enabled: false,
  provider: 'generic',
  endpoint: 'https://verify.example.com/idcard/match',
  appKey: 'demo-key',
  hasSecret: true,
  timeoutMs: 3000,
  dailyLimit: 200,
}

let seq = 0
const nextId = () => `7442000000000000${String(++seq).padStart(3, '0')}`

function row(partial: Partial<Verification> & { status: VerifyStatus }): Verification {
  return {
    id: nextId(),
    nameMasked: '',
    idType: 'id_card',
    numberMasked: '',
    latencyMs: 0,
    operatorName: 'admin',
    createdAt: now(),
    ...partial,
  }
}

const ledger: Verification[] = [
  row({
    status: 'match',
    nameMasked: '张**',
    numberMasked: maskId('11010119900307803X'),
    provider: 'generic',
    providerMsg: '姓名与号码一致',
    latencyMs: 187,
    remark: '酒店入住实名登记',
    createdAt: ago(35),
  }),
  row({
    status: 'mismatch',
    nameMasked: '王*',
    numberMasked: maskId('11010519491231002X'),
    provider: 'generic',
    providerMsg: '姓名与号码不一致',
    latencyMs: 214,
    operatorName: 'front-desk',
    createdAt: ago(140),
  }),
  row({
    status: 'invalid',
    nameMasked: '赵*',
    numberMasked: maskId('110101199003078031'),
    reason: 'checksum',
    createdAt: ago(190),
  }),
  row({
    status: 'valid',
    nameMasked: '孙*',
    numberMasked: maskId('440304198802291234'),
    remark: '在线核验未启用，仅格式结论',
    createdAt: ago(260),
  }),
  row({
    status: 'error',
    nameMasked: '周*',
    numberMasked: maskId('500154199301135886'),
    reason: 'daily_limit',
    provider: 'generic',
    createdAt: ago(300),
  }),
]

const paginate = (rows: Verification[], query: Record<string, unknown>) => {
  const page = Number(query.page ?? 1)
  const pageSize = Number(query.pageSize ?? 20)
  return { list: rows.slice((page - 1) * pageSize, page * pageSize), total: rows.length, page, pageSize }
}

defineMock([
  {
    method: 'POST',
    url: '/plugins/realname/parse',
    handler: ({ body }) => {
      if (body.idType && body.idType !== 'id_card') mockError(400, '暂不支持该证件类型的在线核验')
      const r = parseNumber(String(body.number ?? ''))
      if (r.ok) return r.info
      return { status: 'invalid', reason: r.reason, numberMasked: r.masked } satisfies ParseResult
    },
  },
  {
    method: 'POST',
    url: '/plugins/realname/verify',
    handler: ({ body }) => {
      const name = String(body.name ?? '').trim()
      if (!name) mockError(400, '请填写姓名：核验需要姓名和证件号同时具备')
      const r = parseNumber(String(body.number ?? ''))
      const online = config.enabled
      const out: ParseResult = r.ok
        ? r.info
        : { status: 'invalid', reason: r.reason, numberMasked: r.masked }

      let status: VerifyStatus = out.status
      let reason: string | undefined
      let provider = ''
      let providerMsg = ''
      let latencyMs = 0

      if (!r.ok) {
        reason = r.reason
      } else if (!online) {
        providerMsg = ''
      } else {
        provider = config.provider
        latencyMs = 120 + Math.floor(Math.random() * 200)
        const plain = String(body.number ?? '')
        if (ROSTER[plain] === name) {
          status = 'match'
          providerMsg = '姓名与号码一致'
        } else {
          status = 'mismatch'
          providerMsg = '姓名与号码不一致'
        }
      }

      const verification = row({
        status,
        reason,
        provider,
        providerMsg,
        latencyMs,
        nameMasked: maskName(name),
        numberMasked: out.numberMasked,
        remark: String(body.remark ?? ''),
        createdAt: now(),
      })
      ledger.unshift(verification)
      return { verification, parse: out, online: online && r.ok } satisfies VerifyResult
    },
  },
  {
    method: 'GET',
    url: '/plugins/realname/verifications',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      const status = String(query.status ?? '')
      let rows = ledger
      if (status) rows = rows.filter((v) => v.status === status)
      if (kw) rows = rows.filter((v) => `${v.nameMasked}${v.numberMasked}${v.remark ?? ''}`.toLowerCase().includes(kw))
      return paginate(rows, query)
    },
  },
  { method: 'GET', url: '/plugins/realname/config', handler: () => config },
  {
    method: 'PUT',
    url: '/plugins/realname/config',
    handler: ({ body }) => {
      if (body.enabled && !/^https:\/\//.test(String(body.endpoint ?? ''))) {
        mockError(400, '核验服务地址不合法，只接受 https 地址')
      }
      config = {
        enabled: !!body.enabled,
        provider: String(body.provider ?? ''),
        endpoint: String(body.endpoint ?? ''),
        appKey: String(body.appKey ?? ''),
        hasSecret: body.appSecret ? true : config.hasSecret,
        timeoutMs: Number(body.timeoutMs ?? config.timeoutMs),
        dailyLimit: Number(body.dailyLimit ?? config.dailyLimit),
      }
      return config
    },
  },
])
