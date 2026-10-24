import { z } from 'zod'
import type { TFunction } from 'i18next'

export const DB_TYPES = ['mysql', 'mariadb', 'postgresql'] as const
export const DEFAULT_PORT: Record<(typeof DB_TYPES)[number], string> = { mysql: '3306', mariadb: '3306', postgresql: '5432' }
/** 默认取浏览器当前时区 */
export const browserTimezone = () => {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'Asia/Shanghai'
  } catch {
    return 'Asia/Shanghai'
  }
}

export function buildSchema(t: TFunction) {
  const required = t('setup.validation.required')
  const port = z
    .string()
    .regex(/^\d{1,5}$/, t('setup.validation.port'))
    .refine((v) => Number(v) >= 1 && Number(v) <= 65535, t('setup.validation.port'))

  return z.object({
    db: z.object({
      type: z.enum(DB_TYPES),
      host: z.string().trim().min(1, required),
      port,
      username: z.string().trim().min(1, required),
      password: z.string(),
      database: z.string().trim().min(1, required),
      autoCreate: z.boolean(),
    }),
    cache: z
      .object({
        type: z.enum(['memory', 'valkey']),
        addr: z.string().trim(),
        password: z.string(),
      })
      .superRefine((v, ctx) => {
        if (v.type === 'valkey' && !v.addr) ctx.addIssue({ code: 'custom', path: ['addr'], message: required })
      }),
    admin: z
      .object({
        username: z.string().trim().min(3, t('setup.validation.usernameMin')).regex(/^[A-Za-z][A-Za-z0-9_]*$/, t('setup.validation.usernameFormat')),
        password: z.string().min(1, required),
        confirm: z.string(),
        siteName: z.string().trim().min(1, required).max(60),
        language: z.string().min(1, required),
        timezone: z.string().min(1, required),
      })
      .superRefine((v, ctx) => {
        if (v.confirm !== v.password) ctx.addIssue({ code: 'custom', path: ['confirm'], message: t('setup.validation.passwordMismatch') })
      }),
    optional: z
      .object({
        storage: z.enum(['local', 's3']),
        s3Endpoint: z.string().trim(),
        s3Bucket: z.string().trim(),
        s3AccessKey: z.string().trim(),
        s3SecretKey: z.string(),
        s3Region: z.string().trim(),
        s3Prefix: z.string().trim(),
        s3PathStyle: z.boolean(),
        aiEnabled: z.boolean(),
        aiBaseUrl: z.string().trim(),
        aiApiKey: z.string(),
        aiModel: z.string().trim(),
      })
      .superRefine((v, ctx) => {
        const need = (cond: boolean, keys: (keyof typeof v)[]) => {
          if (!cond) return
          for (const k of keys) if (!v[k]) ctx.addIssue({ code: 'custom', path: [k], message: required })
        }
        need(v.storage === 's3', ['s3Endpoint', 's3Bucket', 's3AccessKey', 's3SecretKey'])
        need(v.aiEnabled, ['aiBaseUrl', 'aiModel'])
      }),
  })
}

export type SetupValues = z.infer<ReturnType<typeof buildSchema>>

export const defaultValues = (language: string): SetupValues => ({
  db: { type: 'mysql', host: '127.0.0.1', port: '3306', username: 'root', password: '', database: 'nexadmin', autoCreate: true },
  cache: { type: 'memory', addr: '127.0.0.1:6379', password: '' },
  admin: { username: 'admin', password: '', confirm: '', siteName: 'NexAdmin', language, timezone: browserTimezone() },
  optional: {
    storage: 'local',
    s3Endpoint: '',
    s3Bucket: '',
    s3AccessKey: '',
    s3SecretKey: '',
    s3Region: '',
    s3Prefix: '',
    s3PathStyle: false,
    aiEnabled: false,
    aiBaseUrl: '',
    aiApiKey: '',
    aiModel: '',
  },
})

export const STEP_FIELDS: string[][] = [
  [],
  ['db.type', 'db.host', 'db.port', 'db.username', 'db.password', 'db.database', 'db.autoCreate'],
  ['cache.type', 'cache.addr', 'cache.password'],
  ['admin.username', 'admin.password', 'admin.confirm', 'admin.siteName', 'admin.language', 'admin.timezone'],
  ['optional.storage', 'optional.s3Endpoint', 'optional.s3Bucket', 'optional.s3AccessKey', 'optional.s3SecretKey', 'optional.s3Region', 'optional.s3Prefix', 'optional.s3PathStyle', 'optional.aiEnabled', 'optional.aiBaseUrl', 'optional.aiApiKey', 'optional.aiModel'],
  [],
]
