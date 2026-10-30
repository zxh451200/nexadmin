export interface AuthUser {
  id: string
  username: string
  nickname: string
  avatar?: string
  tenantId?: string
  roles: string[]
  perms: string[]
  /** 所属租户即将到期时由服务端返回（登录与 /auth/me）；超管与未临近到期的租户没有该字段 */
  tenantExpireSoon?: { expireAt: string; daysLeft: number }
}

export type LoginType = 'password' | 'phone'

export interface LoginPayload {
  type: LoginType
  username?: string
  password?: string
  captcha?: string
  captchaKey?: string
  phone?: string
  code?: string
}

export interface LoginResult {
  accessToken: string
  refreshToken: string
  user: AuthUser
  /** 通过 SAML 单点登录时带回的来源 id（登出时据此跳 IdP 的 SLO）；其他登录方式为空 */
  samlSourceId?: string
}

/** 服务端要求二次验证时的短期挑战（不含令牌）：一次性、绑定 IP、有时效与尝试上限 */
export interface LoginChallenge {
  id: string
  /** 可用的验证方式，如 totp / recovery；空数组按 totp 处理 */
  methods: string[]
  /** 剩余有效期（秒） */
  expiresIn: number
}

/** 登录接口在需要二次验证时的响应：没有 accessToken / refreshToken */
export interface ChallengeResult {
  challenge: LoginChallenge
}

/** 登录一步的结果：已登录，或需要继续二次验证 */
export type LoginStep = { user: AuthUser } | ChallengeResult

/** POST /auth/oauth/:provider/callback：登录成功 / 需要二次验证 / 需要绑定已有账号 / 个人中心绑定完成 */
export type OAuthCallbackResult = LoginResult | ChallengeResult | { needBind: true; bindToken: string } | { bound: true; provider: string }

export interface AuthMethods {
  methods: LoginType[]
  providers: { key: string; name: string }[]
  /** 邮件或短信任一可用时为 true；登录页据此显示「忘记密码」入口 */
  forgotEnabled?: boolean
  /** 可用的找回渠道：email（SMTP 已配置）/ phone（短信已配置）；找回对话框据此适配输入 */
  forgotChannels?: string[]
  /** 运行时开关（设置页「参数设置」的 user.register_enabled，默认关闭），与 /auth/register 的闸门同源 */
  registerEnabled?: boolean
  /** 「登录验证码」强制开启（login.captcha_enabled，出厂 true）：登录首屏就显示图形验证码；关闭时按失败响应自适应 */
  captchaEnabled?: boolean
}

export interface CaptchaResult {
  key: string
  image: string
}
