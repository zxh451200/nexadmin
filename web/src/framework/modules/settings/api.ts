import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'

/** 密钥类字段 GET 时若已设置则返回该占位符；保存时仍为占位符则保持原值 */
export const SECRET_MASK = '********'

export interface SiteSettings {
  siteName: string
  logoUrl: string
  copyright: string
  defaultLanguage: string
  watermark: boolean
}

export interface CacheSettings {
  type: 'memory' | 'valkey'
  address: string
  password: string
}

export interface StorageSettings {
  type: 'local' | 's3'
  endpoint: string
  bucket: string
  region: string
  accessKey: string
  secretKey: string
}

export interface AiSettings {
  enabled: boolean
  baseUrl: string
  apiKey: string
  model: string
  timeoutSec: number
  /** 发往模型前对手机号 / 邮箱 / 身份证号 / 银行卡号脱敏（默认开启） */
  redact: boolean
}

export type SmsProvider = 'none' | 'aliyun' | 'tencent' | 'log'

export interface SmsSettings {
  provider: SmsProvider
  accessKey: string
  secretKey: string
  signName: string
  templateCode: string
}

export interface OAuthProvider {
  enabled: boolean
  clientId: string
  clientSecret: string
  /** 仅 OIDC */
  issuer?: string
}

export const PROVIDER_KEYS = ['wecom', 'dingtalk', 'feishu', 'google', 'github', 'oidc'] as const
export type ProviderKey = (typeof PROVIDER_KEYS)[number]

export interface AuthSettings {
  account: boolean
  phone: boolean
  providers: Record<ProviderKey, OAuthProvider>
  matchBy: 'phone' | 'email' | 'employeeNo'
  autoCreate: boolean
  /** 逗号分隔 */
  allowedDomains: string
  proxy: string
}

/** select 的值都是字符串 */
export interface SecuritySettings {
  /** 连续密码错误几次后锁定 */
  maxAttempts: string
  /** 锁定分钟数 */
  lockMinutes: string
}

export const ATTEMPT_OPTIONS = ['3', '5', '10'] as const
export const LOCK_MINUTE_OPTIONS = ['5', '15', '30', '60'] as const

export type MailSecurity = 'none' | 'starttls' | 'ssl'
export const ROBOT_TYPES = ['dingtalk', 'feishu', 'wecom'] as const
export type RobotType = (typeof ROBOT_TYPES)[number]

export interface MailSettings {
  host: string
  port: number
  security: MailSecurity
  username: string
  /** 密钥类字段：已设置时 GET 返回 SECRET_MASK */
  password: string
  fromName: string
  fromAddr: string
}

export interface RobotSettings {
  name: string
  type: RobotType
  url: string
  /** 钉钉加签 / 飞书签名密钥；企业微信不用。已设置时为 SECRET_MASK */
  secretKey: string
  markdown: boolean
}

/** 通知渠道。短信沿用 sms 分区 */
export interface NotifySettings {
  mail: MailSettings
  robots: RobotSettings[]
  /** 账号创建 / 管理员重置密码后给用户发通知邮件（默认关） */
  accountMail: boolean
}

/** 「发送测试」的附加参数：channel = mail | robot:<name>；to 为测试收件邮箱 */
export interface TestExtra {
  channel?: string
  to?: string
}

export interface AllSettings {
  site: SiteSettings
  security: SecuritySettings
  cache: CacheSettings
  storage: StorageSettings
  ai: AiSettings
  sms: SmsSettings
  auth: AuthSettings
  notify: NotifySettings
}

export type SectionKey = keyof AllSettings

export interface PublicSettings {
  siteName: string
  logoUrl: string
  copyright: string
  watermark: boolean
}

export interface TestResult {
  ok: boolean
  /** 失败原因（i18n key 后缀，见 settings.test.reason.*） */
  reason?: string
}

const KEY = ['settings'] as const

export function useSettings() {
  return useQuery({ queryKey: [...KEY, 'all'], queryFn: () => api.get<AllSettings>('/settings') })
}

/** 无需权限的公开站点信息 */
export function usePublicSettings() {
  return useQuery({
    queryKey: [...KEY, 'public'],
    queryFn: () => api.get<PublicSettings>('/settings/public'),
    staleTime: 60 * 1000,
  })
}

export function useSaveSection() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ section, body }: { section: SectionKey; body: AllSettings[SectionKey] }) =>
      api.put<AllSettings[SectionKey]>(`/settings/${section}`, body),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
  })
}

export function useTestSection() {
  return useMutation({
    mutationFn: ({ section, body }: { section: SectionKey; body: AllSettings[SectionKey] | (NotifySettings & TestExtra) }) =>
      api.post<TestResult>(`/settings/${section}/test`, body),
  })
}
