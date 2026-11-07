export interface AuthUser {
  id: string
  username: string
  nickname: string
  avatar?: string
  tenantId?: string
  roles: string[]
  perms: string[]
  /**
   * 平台级调用方（超管，或落在默认租户里）：平台共享表（没有 tenant_id、一行服务所有租户）的
   * 写入口只认这种调用方。与 perms.includes('*') 不是一回事——平台自己的非超管坐席也是 true。
   * 由 core/auth 的 normalize() 随登录与 /auth/me 给出；缺字段按「不是平台」处理（宁可只读，不误写）。
   */
  platform?: boolean
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
  /** 「登录验证码」强制开启（login.captcha_enabled，出厂 false = 无感优先）：关闭时登录首屏不出验证码，同用户名/同 IP 连续失败达到阈值后按失败响应自适应挂出 */
  captchaEnabled?: boolean
}

export interface CaptchaResult {
  key: string
  image: string
  /** 校验形态：缺省按图形处理（后端旧响应没有这个字段） */
  kind?: 'image' | 'slider' | 'click'
  /** 以下各项仅滑块形态返回，单位都是图片像素，前端据此把拖动量换算回校验口径 */
  piece?: string
  pieceTop?: number
  width?: number
  height?: number
  pieceWidth?: number
  pieceHeight?: number
  /** 以下三项仅点选形态返回：thumb 是候选字条（要求按顺序点选的字），width/height 是主图口径 */
  thumb?: string
  thumbWidth?: number
  thumbHeight?: number
}
