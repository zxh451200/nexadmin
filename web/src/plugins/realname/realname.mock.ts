import { defineMock, mockError } from '@/framework/request/mock'
import type { ParseResult, Verification, VerifyConfig, VerifyResult, VerifyStatus } from './api'

// 开发期假数据（VITE_MOCK=true 时由 mock-loader 自动装载）。
// 校验位与归属地的口径刻意跟 pkg/idcard 对齐：假数据不能比真后端更宽松，
// 否则前端看着对、接真后端就改判。

const WEIGHTS = [7, 9, 10, 5, 8, 4, 2, 1, 6, 3, 7, 9, 10, 5, 8, 4, 2]
const CHECK_SET = '10X98765432'

// 三级分表存，逐级回退：与插件里包 core/region 的实现同一口径——
// 县级查不到只把归属地停在省/地级，不能因此判号码非法。
const PROVINCES: Record<string, string> = {
  '11': '北京市', '41': '河南省', '44': '广东省', '50': '重庆市', '61': '陕西省', '81': '香港特别行政区', '83': '台湾地区',
}
const PREFECTURES: Record<string, string> = {
  '1101': '市辖区', '4101': '郑州市', '4403': '深圳市', '5001': '市辖区', '6101': '西安市',
}
const COUNTIES: Record<string, string> = {
  '110101': '东城区', '110105': '朝阳区', '410181': '巩义市', '440304': '福田区', '500154': '开州区', '610104': '莲湖区',
}

function resolveRegion(code: string): { path: string[]; level: number } {
  const path: string[] = []
  let level = 0
  const push = (name: string | undefined, lv: number) => {
    if (!name) return
    if (path[path.length - 1] === name) return
    path.push(name)
    level = lv
  }
  push(PROVINCES[code.slice(0, 2)], 1)
  push(PREFECTURES[code.slice(0, 4)], 2)
  push(COUNTIES[code], 3)
  return { path, level }
}

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

// 证件类型识别，与 pkg/idcard 的 detectKind 同规则：9 开头是五星卡，
// 810000/820000/830000 是港澳台居民居住证。
const HMT_NAMES: Record<string, string> = {
  '810000': '香港特别行政区',
  '820000': '澳门特别行政区',
  '830000': '台湾地区',
}
// 假数据只需要覆盖演示用的几个国籍数字码；权威表在 pkg/idcard/iso3166.go（249 条）。
const NUMERIC_TO_ALPHA2: Record<string, string> = { '840': 'US', '156': 'CN', '392': 'JP', '410': 'KR', '826': 'GB' }

function detectKind(n: string): { kind: 'resident_id' | 'hmt_permit' | 'foreigner_prp'; regionCode: string; country: string } {
  if (n[0] === '9') {
    return { kind: 'foreigner_prp', regionCode: `${n.slice(1, 3)}0000`, country: NUMERIC_TO_ALPHA2[n.slice(3, 6)] ?? '' }
  }
  const code = n.slice(0, 6)
  if (HMT_NAMES[code]) return { kind: 'hmt_permit', regionCode: code, country: '' }
  return { kind: 'resident_id', regionCode: code, country: '' }
}

/** 与 idcard.Parse 同一判定顺序：长度 → 字符 → 生日 → 未来 → 校验位（区划不参与合法性）。 */
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

  const detected = detectKind(n)
  // 归属地不参与合法性判定：查不到就停在 level 0，与 pkg/idcard 同口径。
  const region = detected.kind === 'hmt_permit'
    ? { path: [HMT_NAMES[detected.regionCode]], level: 1 }
    : resolveRegion(detected.regionCode)

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
      kind: detected.kind,
      country: detected.country,
      regionCode: detected.regionCode,
      regionNames: region.path,
      regionLevel: region.level,
      regionHistorical: false,
      birthday,
      sex: Number(n[16]) % 2 === 1 ? 'male' : 'female',
      age,
    },
  }
}

const now = () => new Date().toISOString()

// 台账默认空着：假数据不该冒充业务记录，页面开起来就是空态，
// 你自己提交的那几条才会进列表（假上游只覆盖接口，不编造记录）。

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

const ledger: Verification[] = []

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

      const nonResident = r.ok && out.kind && out.kind !== 'resident_id'
      let status: VerifyStatus = out.status
      let reason: string | undefined
      let provider = ''
      let providerMsg = ''
      let latencyMs = 0

      if (!r.ok) {
        reason = r.reason
      } else if (nonResident) {
        reason = 'type_unsupported'
      } else if (!online) {
        providerMsg = ''
      } else if (online) {
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
        idType: out.kind === 'foreigner_prp' ? 'foreigner_prp' : out.kind === 'hmt_permit' ? 'hmt_permit' : 'id_card',
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
      return { verification, parse: out, online: online && r.ok && !nonResident } satisfies VerifyResult
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
      // 默认折叠：同一脱敏号码只留最新一条（列表本身按新到旧排），all=1 看全部留痕。
      if (!query.all) {
        const seen = new Set<string>()
        rows = rows.filter((v) => (seen.has(v.numberMasked) ? false : (seen.add(v.numberMasked), true)))
      }
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
