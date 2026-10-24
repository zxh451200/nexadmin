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
  /** 邮件渠道已配置时为 true；登录页据此显示「忘记密码」入口 */
  forgotEnabled?: boolean
}

export interface CaptchaResult {
  key: string
  image: string
}
