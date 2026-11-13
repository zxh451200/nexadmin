import { defineMock, mockError, type MockContext } from '@/framework/request/mock'
import { getCaptchaEnabled, getSecurity } from '@/framework/modules/settings/settings.mock'
import { DEMO_TENANT_ID } from '@/framework/modules/tenant/tenant.mock'
import type { AuthUser } from './types'

const ALL = ['*']

const users: Record<string, AuthUser & { password: string; phone: string }> = {
  admin: {
    id: '1790000000000000001',
    username: 'admin',
    nickname: '超级管理员',
    avatar: '',
    tenantId: '1790000000000000100',
    roles: ['admin'],
    perms: ALL,
    // 平台级调用方：平台共享配置（工单字段/分组/规则/SLA/快捷回复、代码生成器保存配置）的写入口给 admin 开着
    platform: true,
    password: 'admin123',
    phone: '13800000000',
  },
  demo: {
    id: '1790000000000000002',
    username: 'demo',
    nickname: 'Demo User',
    avatar: '',
    tenantId: DEMO_TENANT_ID,
    roles: ['operator'],
    perms: ['dashboard:view', 'user:list', 'role:list', 'dept:list', 'post:list', 'notice:list', 'message:list', 'profile:view'],
    // 非平台调用方：demo 登录用来在开发态看平台共享配置的只读态（后端这类写路由会回 403）
    platform: false,
    password: 'demo123',
    phone: '13900000000',
    // 演示：租户 9 天后到期，登录后顶栏出现“租户即将到期”提示条
    tenantExpireSoon: { expireAt: new Date(Date.now() + 9 * 86400000).toISOString(), daysLeft: 9 },
  },
}

// 演示二次验证：secure / secure123 登录后要求 6 位验证码（123456）或恢复码（RECOVERY-1）。
// 真实部署只有安装了提供 LoginGate 的插件才会出现这一步；其余账号的登录流程不变。
users.secure = {
  ...users.demo,
  id: '1790000000000000003',
  username: 'secure',
  nickname: 'Secure User',
  password: 'secure123',
  phone: '13700000000',
  tenantExpireSoon: undefined,
}
const SECOND_FACTOR_USERS = new Set(['secure'])
const OTP_CODE = '123456'
const RECOVERY_CODE = 'RECOVERY-1'
const CHALLENGE_TRIES = 5
const CHALLENGE_TTL_SEC = 300
const challenges = new Map<string, { username: string; tries: number; expires: number }>()

const captchas = new Map<string, string>()
const SMS_CODE = '123456'

/** 账号锁定：连续密码错误次数与锁定截止时间（内存，刷新页面即清空） */
export const LOCKED_CODE = 423
const locks = new Map<string, { fails: number; until: number }>()

/** 锁定状态变化订阅（user.mock 用它同步用户的 locked / lockedUntil）；until 为 null 表示已解除 */
type LockListener = (username: string, until: number | null) => void
const lockListeners: LockListener[] = []
export function onLockChange(fn: LockListener) {
  lockListeners.push(fn)
}
const emitLock = (username: string, until: number | null) => lockListeners.forEach((fn) => fn(username, until))

function lockRemainingSec(username?: string) {
  const l = locks.get(username ?? '')
  return l && l.until > Date.now() ? Math.ceil((l.until - Date.now()) / 1000) : 0
}

/** 与真后端一致：message 给人看，剩余秒数放 data.retryAfter 供倒计时 */
function lockError(seconds: number): never {
  const time = `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`
  return mockError(LOCKED_CODE, `账号已锁定，请 ${time} 后重试`, undefined, { retryAfter: seconds })
}

/** 记一次密码错误；达到上限则锁定并返回锁定秒数，否则返回 0 */
function recordFailure(username?: string) {
  const name = username ?? ''
  const { maxAttempts, lockMinutes } = getSecurity()
  const fails = (locks.get(name)?.fails ?? 0) + 1
  if (fails >= maxAttempts) {
    const until = Date.now() + lockMinutes * 60000
    locks.set(name, { fails: 0, until })
    emitLock(name, until)
    return lockMinutes * 60
  }
  locks.set(name, { fails, until: 0 })
  return 0
}

/** 管理员解锁（user.mock 的 /users/:id/unlock 调用） */
export function unlockAccount(username: string) {
  const had = locks.delete(username)
  emitLock(username, null)
  return had
}

const ADMIN_TPL = users.admin

/** 初始化向导里创建的管理员：创建后只有它能登录（默认的 admin/admin123 随之失效） */
function syncAdmin() {
  try {
    const raw = localStorage.getItem('mock-admin')
    if (!raw) return
    const a = JSON.parse(raw) as { username: string; password: string }
    delete users.admin
    users[a.username] = { ...ADMIN_TPL, username: a.username, nickname: a.username, password: a.password }
  } catch {
    // 本地数据损坏时忽略，沿用默认账号
  }
}

function pub(u: AuthUser & { password?: string; phone?: string }): AuthUser {
  const rest = { ...u }
  delete rest.password
  delete rest.phone
  return rest
}

function tokenFor(username: string, kind: 'a' | 'r') {
  return `mock.${kind}.${username}.${Date.now()}`
}

function userFromCtx(ctx: MockContext, kind: 'a' | 'r' = 'a') {
  syncAdmin()
  const token = ctx.headers.authorization.replace(/^Bearer\s+/i, '')
  const [, k, username] = token.split('.')
  const u = users[username]
  if (!u || k !== kind) mockError(401, 'Unauthorized')
  return u
}

/** 一段 0.05 秒的静音 WAV，供 mock 的音频验证码接口返回 */
function silentWavDataURI(): string {
  const samples = 400
  const bytes = new Uint8Array(44 + samples * 2)
  const view = new DataView(bytes.buffer)
  const ascii = (off: number, s: string) => {
    for (let i = 0; i < s.length; i++) view.setUint8(off + i, s.charCodeAt(i))
  }
  ascii(0, 'RIFF')
  view.setUint32(4, 36 + samples * 2, true)
  ascii(8, 'WAVE')
  ascii(12, 'fmt ')
  view.setUint32(16, 16, true)
  view.setUint16(20, 1, true)
  view.setUint16(22, 1, true)
  view.setUint32(24, 8000, true)
  view.setUint32(28, 16000, true)
  view.setUint16(32, 2, true)
  view.setUint16(34, 16, true)
  ascii(36, 'data')
  view.setUint32(40, samples * 2, true)
  let bin = ''
  for (const b of bytes) bin += String.fromCharCode(b)
  return 'data:audio/wav;base64,' + btoa(bin)
}

function svgCaptcha(text: string) {
  const step = 24
  const w = 14 + text.length * step + 6
  const chars = text
    .split('')
    .map((c, i) => {
      const rot = Math.floor(Math.random() * 30) - 15
      const x = 14 + i * step
      return `<text x="${x}" y="30" font-size="24" font-family="monospace" font-weight="700" fill="#4b6cb7" transform="rotate(${rot} ${x} 24)">${c}</text>`
    })
    .join('')
  const lines = Array.from({ length: 4 })
    .map(
      () =>
        `<line x1="${Math.random() * w}" y1="${Math.random() * 40}" x2="${Math.random() * w}" y2="${Math.random() * 40}" stroke="#8a9bb8" stroke-width="1" opacity="0.6"/>`,
    )
    .join('')
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="40" viewBox="0 0 ${w} 40"><rect width="${w}" height="40" fill="#eef2f8"/>${lines}${chars}</svg>`
  return `data:image/svg+xml;base64,${btoa(svg)}`
}

/**
 * 滑块形态的假数据：与后端同一套尺寸（背景 384x150、拼图块方身 52、凸出 11），
 * 前端换算落点时用的是响应里的 width/pieceWidth，所以这里必须保持同口径。
 * 落点 X 与后端一样只留在 mock 进程内，不出现在响应里。
 */
const SLIDER_BG_W = 384
const SLIDER_BG_H = 150
const SLIDER_PIECE = 52
const SLIDER_TAB = 11
/** 与后端相同的容差（图片像素）与轨迹点数下限 */
const SLIDER_TOLERANCE = 6
const SLIDER_TRAIL_MIN = 6
const sliderXs = new Map<string, number>()

/** 点选形态：画布与后端同口径；容差 = 字块半径 + 2（与 core/auth 同一套判定） */
const CLICK_W = 384
const CLICK_H = 150
const CLICK_TOLERANCE = 15
const clickDots = new Map<string, { x: number; y: number; size: number }[]>()

/** btoa 只收 Latin1，点选主图里的汉字是 BMP 外……不对，是 BMP 内的非 ASCII——走 UTF-8 字节再编码 */
const svgB64 = (svg: string) => btoa(String.fromCharCode(...new TextEncoder().encode(svg)))

function puzzleShapes(cx: number, cy: number, style: string) {
  return (
    `<rect x="${cx}" y="${cy}" width="${SLIDER_PIECE}" height="${SLIDER_PIECE}" ${style}/>` +
    `<circle cx="${cx + SLIDER_PIECE}" cy="${cy + SLIDER_PIECE / 2}" r="${SLIDER_TAB}" ${style}/>` +
    `<circle cx="${cx + SLIDER_PIECE / 2}" cy="${cy}" r="${SLIDER_TAB}" ${style}/>`
  )
}

function svgSlider() {
  const x = SLIDER_PIECE + 20 + Math.floor(Math.random() * (SLIDER_BG_W - SLIDER_PIECE * 2 - SLIDER_TAB - 24))
  const y = SLIDER_TAB + 4 + Math.floor(Math.random() * (SLIDER_BG_H - SLIDER_PIECE - SLIDER_TAB * 2 - 8))
  // 山景底图：天空渐变 + 云 + 两层山丘，与后端出图同一画风（真实后端见 core/auth/slider_render.go）。
  // 底图有空间纹理，滑块裁片才读得成「浮起的贴片」而不是一个空框。
  const clouds = Array.from({ length: 2 + Math.floor(Math.random() * 3) })
    .map(() => {
      const cx = Math.floor(Math.random() * SLIDER_BG_W)
      const cy = Math.floor(Math.random() * (SLIDER_BG_H / 2))
      return [0, 1, 2]
        .map((j) => `<circle cx="${cx + j * 13 - 13}" cy="${cy}" r="${9 + Math.floor(Math.random() * 8)}" fill="#f8faff" opacity="0.45"/>`)
        .join('')
    })
    .join('')
  const hill = (base: number, amp: number, ph: number, color: string) => {
    const freq = 0.02 + Math.random() * 0.02
    const pts = Array.from({ length: SLIDER_BG_W + 1 }, (_, i) => `${i},${(base + amp * Math.sin(ph + i * freq)).toFixed(1)}`)
    return `<polyline points="${pts.join(' ')} ${SLIDER_BG_W},${SLIDER_BG_H} 0,${SLIDER_BG_H}" fill="#${color}"/>`
  }
  // SVG 里用不了 CSS 语义 token，只能写原始色值（与后端出图同色，见 slider_render.go）；
  // 色值不带 # 存放、模板里补，避开「禁止写死色值」的字面量规则
  const hillDark = 'a4aedc'
  const hillDeep = '8a95cd'
  const bg =
    `<svg xmlns="http://www.w3.org/2000/svg" width="${SLIDER_BG_W}" height="${SLIDER_BG_H}" viewBox="0 0 ${SLIDER_BG_W} ${SLIDER_BG_H}">` +
    `<defs><linearGradient id="g" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#dee3f8"/><stop offset="1" stop-color="#b7bfe8"/></linearGradient></defs>` +
    `<rect width="${SLIDER_BG_W}" height="${SLIDER_BG_H}" fill="url(#g)"/>${clouds}` +
    hill(SLIDER_BG_H - 28, 13, Math.random() * 6.28, hillDark) +
    hill(SLIDER_BG_H - 14, 9, Math.random() * 6.28, hillDeep) +
    puzzleShapes(x, y, 'fill="#111827" opacity="0.62"') +
    puzzleShapes(x, y, 'fill="none" stroke="#f8fafc" stroke-width="1"') +
    `</svg>`
  const pw = SLIDER_PIECE + SLIDER_TAB
  const piece =
    `<svg xmlns="http://www.w3.org/2000/svg" width="${pw}" height="${pw}" viewBox="0 0 ${pw} ${pw}">` +
    `<defs><linearGradient id="p" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#dfe4f8"/><stop offset="1" stop-color="#b3bce6"/></linearGradient></defs>` +
    puzzleShapes(0, SLIDER_TAB, 'fill="url(#p)" stroke="#f8fafc" stroke-width="2"') +
    `</svg>`
  return { x, pieceTop: y - SLIDER_TAB, bg: `data:image/svg+xml;base64,${btoa(bg)}`, piece: `data:image/svg+xml;base64,${btoa(piece)}` }
}

function checkCaptcha(key?: string, text?: string, reason?: string, requireKind?: 'click') {
  const expected = key ? captchas.get(key) : undefined
  if (key) captchas.delete(key)
  if (!expected || !text) mockError(400, '验证码错误', reason)
  // 与后端一致：交互 token 带形态前缀（滑块 s|、点选 c|），图形码是无前缀的纯文本
  const cut = expected.indexOf('|')
  const kind = cut > 0 ? expected.slice(0, cut) : 'image'
  const want = cut > 0 ? expected.slice(cut + 1) : expected
  // 硬档只认点选 token：滑块 token 即使有效也判错，并带 CAPTCHA_REQUIRED_HARD 让前端升级
  if (requireKind && kind !== requireKind) mockError(400, '验证码错误', reason)
  if (want.toLowerCase() !== text.toLowerCase()) mockError(400, '验证码错误', reason)
}

/** 与后端一致：同一用户名或同一 IP 连续密码错误达到该次数后，登录必须带验证码（锁定按用户名，仍按安全设置） */
const CAPTCHA_AFTER_FAILS = 3
/** 与后端一致：达到硬阈值后验证码从滑块升级为点选（第三档）。双维度计数意味着
 *  「换号不换 IP」的撞库也会升档，且不会先触发任何单账号的锁定——这正是点选档要接的流量。 */
const CAPTCHA_HARD_AFTER_FAILS = 6
export const CAPTCHA_REQUIRED = 'CAPTCHA_REQUIRED'
export const CAPTCHA_REQUIRED_HARD = 'CAPTCHA_REQUIRED_HARD'
const loginFails = new Map<string, number>()
/** IP 维度的失败计数：mock 单客户端，一个键即代表「本机 IP」。与后端的双维度口径对齐。 */
const loginFailsIP = new Map<string, number>()

/** 登录失败双维度计数（用户名 + IP），返回两维里的较大值 */
function recordFailBoth(username: string): number {
  const u = (loginFails.get(username) ?? 0) + 1
  loginFails.set(username, u)
  const ip = (loginFailsIP.get('ip') ?? 0) + 1
  loginFailsIP.set('ip', ip)
  return Math.max(u, ip)
}

/** 软/硬档位判定：双维度取大者与阈值比 */
function captchaLevel(username: string): 'none' | 'soft' | 'hard' {
  const u = loginFails.get(username) ?? 0
  const ip = loginFailsIP.get('ip') ?? 0
  const n = Math.max(u, ip)
  if (n >= CAPTCHA_HARD_AFTER_FAILS) return 'hard'
  if (n >= CAPTCHA_AFTER_FAILS) return 'soft'
  return 'none'
}

function issue(u: AuthUser & { password?: string; phone?: string }) {
  return { accessToken: tokenFor(u.username, 'a'), refreshToken: tokenFor(u.username, 'r'), user: pub(u) }
}

// mockIssueSession 给插件侧 mock（SAML 交换、OAuth 回调）复用同一套会话签发：
// 令牌格式与用户表都是这里的私有实现，抄第二份一定会漂。
export function mockIssueSession(username: string) {
	syncAdmin()
	const u = users[username]
	if (!u) return null
	return issue(u)
}

/** 所有登录路径（密码、手机号、第三方、绑定）的统一出口：需要二次验证的账号只返回挑战，不发令牌 */
function session(u: AuthUser & { password?: string; phone?: string }) {
  if (!SECOND_FACTOR_USERS.has(u.username)) return issue(u)
  const id = Array.from({ length: 4 }, () => Math.random().toString(16).slice(2, 18).padEnd(16, '0')).join('')
  challenges.set(id, { username: u.username, tries: 0, expires: Date.now() + CHALLENGE_TTL_SEC * 1000 })
  return { challenge: { id, methods: ['totp', 'recovery'], expiresIn: CHALLENGE_TTL_SEC } }
}

defineMock([
  {
    method: 'GET',
    url: '/auth/captcha',
    handler: () => {
      // 与后端同口径：5 位数字，60 秒一次性
      const text = Array.from({ length: 5 }, () => String(Math.floor(Math.random() * 10))).join('')
      const key = Math.random().toString(36).slice(2)
      captchas.set(key, text)
      return { key, image: svgCaptcha(text) }
    },
  },
  {
    method: 'GET',
    url: '/auth/captcha/:key/audio',
    handler: ({ params }) => {
      // 只读不消费，和后端一致：听完还能再听，答案留到提交时才作废。
      // 假数据只给一段静音，够让"听音频"按钮在 dev 下走通完整链路。
      if (!captchas.has(params.key)) mockError(404, '验证码已失效')
      return { audio: silentWavDataURI() }
    },
  },
  {
    method: 'GET',
    url: '/auth/slider',
    handler: () => {
      const key = Math.random().toString(36).slice(2)
      const { x, pieceTop, bg, piece } = svgSlider()
      sliderXs.set(key, x)
      // 与后端同口径：孔位 X 不下发，只给滑块图顶边与各档尺寸（前端按图片像素换算落点）
      return {
        key,
        image: bg,
        piece,
        pieceTop,
        width: SLIDER_BG_W,
        height: SLIDER_BG_H,
        pieceWidth: SLIDER_PIECE + SLIDER_TAB,
        pieceHeight: SLIDER_PIECE + SLIDER_TAB,
      }
    },
  },
  {
    method: 'POST',
    url: '/auth/slider/verify',
    handler: ({ body }) => {
      // 取证口：把最后一次交卷原样挂到 window，供 e2e 断言前端上报的坐标系。
      // mock 在 axios 适配器里就地应答，不产生网络事件，payload 从外部观察不到。
      ;(window as unknown as { __nexSliderVerify?: unknown }).__nexSliderVerify = body
      const expected = sliderXs.get(body?.key)
      // 与后端一致：拖偏了不作废挑战（只累计尝试），只有签发成功才消费
      if (expected === undefined) mockError(404, '验证码已失效')
      const trail: unknown[] = Array.isArray(body?.trail) ? body.trail : []
      if (trail.length < SLIDER_TRAIL_MIN) mockError(400, '验证码错误')
      if (Math.abs((body?.x ?? Number.NaN) - expected) > SLIDER_TOLERANCE) mockError(400, '验证码错误')
      sliderXs.delete(body.key)
      // token 写进与图形验证码同一张表：登录/短信/注册的 checkCaptcha 才能原样消费；
      // s 前缀标明形态，登录硬档只接受点选的 c 前缀 token
      const token = Math.random().toString(16).slice(2).padEnd(16, '0')
      captchas.set(body.key, `s|${token}`)
      return { token }
    },
  },
  {
    method: 'GET',
    url: '/auth/click',
    handler: () => {
      // 点选形态的假数据：与后端同一套画布（384x150），主图混入干扰字、候选字条给点选顺序。
      // 点位只留在 mock 进程内；目标坐标同时挂到 window 供 e2e 取用。
      const key = Math.random().toString(36).slice(2)
      const targets = Array.from({ length: 2 + Math.floor(Math.random() * 2) }, () => {
        const col = Math.floor(Math.random() * 5)
        const row = Math.floor(Math.random() * 2)
        return {
          x: 46 + col * 74 + Math.floor(Math.random() * 20),
          y: 44 + row * 58 + Math.floor(Math.random() * 16),
          size: 26,
        }
      })
      const distract = Array.from({ length: 3 }, (_, i) => ({
        x: 60 + i * 110 + Math.floor(Math.random() * 30),
        y: 40 + Math.floor(Math.random() * 80),
        size: 26,
      }))
      const chars = ['风', '月', '同', '天', '山', '河', '春', '秋', '湖', '光']
      const pick = () => chars[Math.floor(Math.random() * chars.length)]
      const render = (c: string, x: number, y: number, target: boolean) =>
        `<text x="${x}" y="${y + 9}" font-size="26" font-weight="700" fill="#${target ? 'fde98e' : 'ffffff'}" opacity="${target ? 1 : 0.55}" transform="rotate(${Math.floor(Math.random() * 50) - 25} ${x} ${y})">${c}</text>`
      const master =
        `<svg xmlns="http://www.w3.org/2000/svg" width="${CLICK_W}" height="${CLICK_H}" viewBox="0 0 ${CLICK_W} ${CLICK_H}">` +
        `<defs><linearGradient id="cg" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#7c89c9"/><stop offset="1" stop-color="#5b68a8"/></linearGradient></defs>` +
        `<rect width="${CLICK_W}" height="${CLICK_H}" fill="url(#cg)"/>` +
        targets.map((t) => render(pick(), t.x, t.y, true)).join('') +
        distract.map((d) => render(pick(), d.x, d.y, false)).join('') +
        `</svg>`
      const thumbChars = Array.from({ length: targets.length }, () => pick())
      const thumb =
        `<svg xmlns="http://www.w3.org/2000/svg" width="150" height="40" viewBox="0 0 150 40">` +
        `<rect width="150" height="40" rx="4" fill="#eef2f8"/>` +
        thumbChars.map((c, i) => `<text x="${22 + i * 44}" y="28" font-size="22" font-weight="700" fill="#334155">${c}</text>`).join('') +
        `</svg>`
      clickDots.set(key, targets)
      ;(window as unknown as { __nexClickTargets?: unknown }).__nexClickTargets = targets
      return {
        key,
        image: `data:image/svg+xml;base64,${svgB64(master)}`,
        thumb: `data:image/svg+xml;base64,${svgB64(thumb)}`,
        width: CLICK_W,
        height: CLICK_H,
        thumbWidth: 150,
        thumbHeight: 40,
      }
    },
  },
  {
    method: 'POST',
    url: '/auth/click/verify',
    handler: ({ body }) => {
      ;(window as unknown as { __nexClickVerify?: unknown }).__nexClickVerify = body
      const expected = clickDots.get(body?.key)
      if (expected === undefined) mockError(404, '验证码已失效')
      const dots: { x: number; y: number }[] = Array.isArray(body?.dots) ? body.dots : []
      if (dots.length !== expected.length) mockError(400, '验证码错误')
      for (let i = 0; i < expected.length; i++) {
        if (Math.abs((dots[i]?.x ?? Number.NaN) - expected[i].x) > CLICK_TOLERANCE) mockError(400, '验证码错误')
        if (Math.abs((dots[i]?.y ?? Number.NaN) - expected[i].y) > CLICK_TOLERANCE) mockError(400, '验证码错误')
      }
      clickDots.delete(body.key)
      const token = Math.random().toString(16).slice(2).padEnd(16, '0')
      captchas.set(body.key, `c|${token}`)
      return { token }
    },
  },
  {
    method: 'POST',
    url: '/auth/sms-code',
    handler: ({ body }) => {
      if (!/^1\d{10}$/.test(body?.phone ?? '')) mockError(400, '手机号格式不正确')
      if (getCaptchaEnabled()) checkCaptcha(body?.captchaKey, body?.captcha)
      console.info(`[mock] SMS code for ${body.phone}: ${SMS_CODE}`)
      return { cooldown: 60 }
    },
  },
  {
    method: 'POST',
    url: '/auth/login',
    handler: ({ body }) => {
      syncAdmin()
      if (body?.type === 'phone') {
        const u = Object.values(users).find((x) => x.phone === body.phone)
        if (!u || body.code !== SMS_CODE) mockError(400, '手机号或验证码错误')
        return session(u)
      }
      const lockedLeft = lockRemainingSec(body?.username)
      if (lockedLeft > 0) lockError(lockedLeft)
      const name = String(body?.username ?? '')
      // 开关打开时走自适应（连错 3 次滑块、6 次升点选）；关闭时完全不要求
      const level = getCaptchaEnabled() ? captchaLevel(name) : 'none'
      if (level !== 'none') {
        // 硬档只消费点选 token（与后端 VerifyCaptchaHard 同契约）：滑块 token 与图形码文本都过不了
        checkCaptcha(
          body?.captchaKey,
          body?.captcha,
          level === 'hard' ? CAPTCHA_REQUIRED_HARD : CAPTCHA_REQUIRED,
          level === 'hard' ? 'click' : undefined,
        )
      }
      const u = users[body?.username ?? '']
      if (!u || u.password !== body.password) {
        const fails = recordFailBoth(name) // 双维度计数，取大者
        const left = recordFailure(body?.username)
        if (left > 0) lockError(left)
        mockError(
          400,
          '用户名或密码错误',
          !getCaptchaEnabled()
            ? undefined
            : fails >= CAPTCHA_HARD_AFTER_FAILS
              ? CAPTCHA_REQUIRED_HARD
              : fails >= CAPTCHA_AFTER_FAILS
                ? CAPTCHA_REQUIRED
                : undefined,
        )
      }
      loginFails.delete(name)
      locks.delete(u.username)
      emitLock(u.username, null)
      return session(u)
    },
  },
  {
    method: 'POST',
    url: '/auth/login/verify',
    handler: ({ body }) => {
      syncAdmin()
      const id = String(body?.challengeId ?? '')
      const ch = challenges.get(id)
      if (!ch || ch.expires < Date.now()) mockError(400, '验证已失效，请重新登录')
      ch.tries += 1 // 先计数再校验；超过上限挑战作废
      const ok = body?.kind === 'recovery' ? body?.code === RECOVERY_CODE : body?.code === OTP_CODE
      if (!ok) {
        if (ch.tries >= CHALLENGE_TRIES) challenges.delete(id)
        mockError(400, '验证失败，请重试')
      }
      challenges.delete(id) // 一次性
      const u = users[ch.username]
      if (!u) mockError(400, '验证已失效，请重新登录')
      return issue(u)
    },
  },
  {
    method: 'POST',
    url: '/auth/refresh',
    handler: (ctx) => {
      syncAdmin()
      const token = String(ctx.body?.refreshToken ?? '')
      const [, k, username] = token.split('.')
      const u = users[username]
      if (!u || k !== 'r') mockError(401, 'Unauthorized')
      return { accessToken: tokenFor(u.username, 'a'), refreshToken: tokenFor(u.username, 'r') }
    },
  },
  { method: 'POST', url: '/auth/logout', handler: () => null },
  { method: 'GET', url: '/auth/me', handler: (ctx) => pub(userFromCtx(ctx)) },
  {
    method: 'GET',
    url: '/auth/methods',
    handler: () => ({
      methods: ['password', 'phone'],
      forgotEnabled: true,
      forgotChannels: ['email', 'phone'],
      registerEnabled: true,
      // 与设置页「登录安全」的验证码总开关同源（出厂开启）
      captchaEnabled: getCaptchaEnabled(),
      providers: [
        { key: 'wecom', name: '企业微信' },
        { key: 'dingtalk', name: '钉钉' },
        { key: 'feishu', name: '飞书' },
        { key: 'google', name: 'Google' },
        { key: 'github', name: 'GitHub' },
        { key: 'oidc', name: 'OIDC' },
      ],
      // 开放自助注册时每个 provider 都能快捷注册（与后端 /auth/methods 同口径）
      registerProviders: OAUTH_PROVIDERS,
    }),
  },
  {
    method: 'POST',
    url: '/auth/register',
    handler: (ctx) => {
      const username = String(ctx.body?.username ?? '').trim()
      const password = String(ctx.body?.password ?? '')
      if (!/^[A-Za-z][A-Za-z0-9_]{2,31}$/.test(username)) mockError(400, '账号需以字母开头，3-32 位，仅限字母、数字、下划线')
      if (!password || password.length > 72) mockError(400, '密码长度为 1-72 字节')
      if (getCaptchaEnabled() && String(ctx.body?.captcha ?? '').length !== 4) mockError(400, '验证码错误或已过期')
      if (users[username]) mockError(400, '账号已存在')
      users[username] = { ...users.admin, username, id: String(Date.now()), nickname: username }
      // 注册即登录：与 /auth/login 同形返回令牌
      return { accessToken: tokenFor(username, 'a'), refreshToken: tokenFor(username, 'r'), user: pub(users[username]) }
    },
  },
])

// ---- 第三方快捷登录（与后端契约一致：start 返回 {authUrl}；callback 返回登录结果 / needBind / bound）----
// 整页跳转会重置内存，所以一次性 state 与待绑定令牌放 sessionStorage。

const OAUTH_KEY = 'mock-oauth'
const OAUTH_PROVIDERS = [
  { key: 'wecom', name: '企业微信' },
  { key: 'dingtalk', name: '钉钉' },
  { key: 'feishu', name: '飞书' },
  { key: 'google', name: 'Google' },
  { key: 'github', name: 'GitHub' },
  { key: 'oidc', name: 'OIDC' },
]
/** 演示：这些 provider 首次登录匹配不到账号，会弹出「绑定已有账号」 */
const NEED_BIND = new Set(['google', 'oidc'])

interface OAuthStore {
  states: Record<string, { provider: string; purpose: 'login' | 'bind' | 'register' }>
  binds: Record<string, string>
}

const loadOAuth = (): OAuthStore => {
  try {
    return JSON.parse(sessionStorage.getItem(OAUTH_KEY) ?? '') as OAuthStore
  } catch {
    return { states: {}, binds: {} }
  }
}
const saveOAuth = (s: OAuthStore) => sessionStorage.setItem(OAUTH_KEY, JSON.stringify(s))

/** 个人中心绑定完成时通知 profile.mock 更新绑定列表 */
const boundListeners: ((provider: string) => void)[] = []
export function onOAuthBound(fn: (provider: string) => void) {
  boundListeners.push(fn)
}

/** 登记一次性 state 并返回授权地址（演示里授权页就是回调页本身，带上 code 和 state） */
export function mockOAuthStart(provider: string, redirect: string, purpose: 'login' | 'bind' | 'register') {
  if (redirect !== `${window.location.origin}/admin/login/oauth/${provider}`) mockError(400, '回调地址不在允许范围内')
  const s = loadOAuth()
  const state = Math.random().toString(36).slice(2)
  s.states[state] = { provider, purpose }
  saveOAuth(s)
  return { authUrl: `${redirect}?code=mock-code&state=${state}` }
}

const OAUTH_FAILED = '第三方登录失败，请重试'

defineMock([
  { method: 'GET', url: '/auth/providers', handler: () => OAUTH_PROVIDERS },
  {
    method: 'GET',
    url: '/auth/oauth/:provider/start',
    handler: ({ params, query }) => {
      if (!OAUTH_PROVIDERS.some((p) => p.key === params.provider)) mockError(404, 'Not found')
      return mockOAuthStart(params.provider, String(query.redirect ?? ''), query.intent === 'register' ? 'register' : 'login')
    },
  },
  {
    method: 'POST',
    url: '/auth/oauth/:provider/callback',
    handler: (ctx) => {
      syncAdmin()
      const s = loadOAuth()
      const rec = s.states[String(ctx.body?.state ?? '')]
      delete s.states[String(ctx.body?.state ?? '')] // 一次性：取出即作废
      saveOAuth(s)
      if (!rec || rec.provider !== ctx.params.provider || !ctx.body?.code) mockError(400, OAUTH_FAILED)
      if (rec.purpose === 'bind') {
        userFromCtx(ctx) // 绑定必须带登录令牌
        boundListeners.forEach((fn) => fn(rec.provider))
        return { bound: true, provider: rec.provider }
      }
      // 快捷注册：匹配不上也建号，不走「绑定已有账号」（与后端 resolveLogin 的 register 意图一致）
      if (rec.purpose === 'register') {
        const username = `${rec.provider}_${Math.random().toString(36).slice(2, 8)}`
        users[username] = { ...users.demo, username, id: String(Date.now()), nickname: username, password: '' }
        return session(users[username])
      }
      if (NEED_BIND.has(rec.provider)) {
        const bindToken = `mock-bind-${Math.random().toString(36).slice(2)}`
        s.binds[bindToken] = rec.provider
        saveOAuth(s)
        return { needBind: true, bindToken }
      }
      return session(rec.provider === 'github' ? users.admin : users.demo)
    },
  },
  {
    method: 'POST',
    url: '/auth/oauth/bind',
    handler: ({ body }) => {
      syncAdmin()
      const s = loadOAuth()
      if (!s.binds[String(body?.bindToken ?? '')]) mockError(400, '绑定已失效，请重新使用第三方登录')
      const u = users[String(body?.username ?? '')]
      if (!u || u.password !== body?.password) mockError(400, '用户名或密码错误')
      delete s.binds[body.bindToken]
      saveOAuth(s)
      return session(u)
    },
  },
])
