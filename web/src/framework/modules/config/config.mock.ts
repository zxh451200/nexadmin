import { createCrudMock, defineMock, type CrudStore } from '@/framework/request/mock'
import type { ConfigRow, UploadSettings } from './api'

const KEY_IMAGE = 'upload.max_size_image_mb'
const KEY_VIDEO = 'upload.max_size_video_mb'
const KEY_FILE = 'upload.max_size_file_mb'
const KEY_TYPES = 'upload.allowed_types'
const KEY_WEBP = 'upload.webp_variant'

const DEFAULT_TYPES = [
  'image/jpeg',
  'image/png',
  'image/gif',
  'image/webp',
  'video/mp4',
  'video/webm',
  'application/pdf',
  '.doc',
  '.docx',
  '.xls',
  '.xlsx',
  '.zip',
]

const UPLOAD_TYPE_OPTIONS = [
  { id: 'image/jpeg', name: 'JPEG (image/jpeg)' },
  { id: 'image/png', name: 'PNG (image/png)' },
  { id: 'image/gif', name: 'GIF (image/gif)' },
  { id: 'image/webp', name: 'WebP (image/webp)' },
  { id: 'image/svg+xml', name: 'SVG (image/svg+xml)' },
  { id: 'video/mp4', name: 'MP4 (video/mp4)' },
  { id: 'video/webm', name: 'WebM (video/webm)' },
  { id: 'application/pdf', name: 'PDF (application/pdf)' },
  { id: '.doc', name: 'Word (.doc)' },
  { id: '.docx', name: 'Word (.docx)' },
  { id: '.xls', name: 'Excel (.xls)' },
  { id: '.xlsx', name: 'Excel (.xlsx)' },
  { id: '.ppt', name: 'PowerPoint (.ppt)' },
  { id: '.pptx', name: 'PowerPoint (.pptx)' },
  { id: '.zip', name: 'ZIP (.zip)' },
  { id: '.txt', name: 'Text (.txt)' },
]

const iso = (daysAgo: number) => new Date(Date.now() - daysAgo * 86400000).toISOString()
const cid = (n: number) => `73000000000${String(10000000 + n)}`

const row = (
  n: number,
  name: string,
  key: string,
  value: string,
  type: ConfigRow['type'],
  isPublic: boolean,
  remark?: string,
): ConfigRow => ({ id: cid(n), name, key, value, type, isPublic, remark, createdAt: iso(90 - n * 4) })

const seed: ConfigRow[] = [
  row(3, '版权信息', 'site.copyright', '© 2026 NexAdmin', 'string', true, '页脚展示'),
  row(4, '用户初始密码', 'user.default_password', 'Nex@123456', 'string', false, '新建用户及重置密码时使用'),
  row(5, '开放注册', 'user.register_enabled', 'false', 'boolean', true),
  row(6, '登录验证码', 'login.captcha_enabled', 'true', 'boolean', true, '关闭后登录页不再出现验证码'),
  row(7, '登录最大重试次数', 'login.max_retry', '5', 'number', false),
  row(8, '账号锁定时长(分钟)', 'login.lock_minutes', '30', 'number', false),
  row(10, 'IP 白名单', 'security.ip_whitelist', '["10.0.0.0/8","192.168.0.0/16"]', 'json', false, '仅超管端生效'),
  row(11, '图片上传大小上限(MB)', KEY_IMAGE, '0', 'number', true, '0 或留空表示不限制'),
  row(12, '视频上传大小上限(MB)', KEY_VIDEO, '0', 'number', true, '0 或留空表示不限制'),
  row(13, '普通文件上传大小上限(MB)', KEY_FILE, '0', 'number', true, '0 或留空表示不限制'),
  row(14, '允许上传的文件类型', KEY_TYPES, DEFAULT_TYPES.join(','), 'string', true, '逗号分隔的 MIME 或扩展名'),
  row(15, '为图片生成 WebP 副本', KEY_WEBP, 'false', 'boolean', true, '原图保留，按 Accept 优先下发 WebP'),
]

// eslint-disable-next-line prefer-const
let store: CrudStore<ConfigRow>

const find = (key: string) => store.all().find((r) => r.key === key)
const num = (key: string) => Number(find(key)?.value ?? 0) || 0
const put = (key: string, value: string) => {
  const r = find(key)
  if (r) r.value = value
}

// 必须先于 createCrudMock 注册，避免被 /configs/:id 抢先匹配
defineMock([
  {
    method: 'GET',
    url: '/configs/upload',
    handler: (): UploadSettings => ({
      maxImageMb: num(KEY_IMAGE),
      maxVideoMb: num(KEY_VIDEO),
      maxFileMb: num(KEY_FILE),
      allowedTypes: (find(KEY_TYPES)?.value ?? '').split(',').filter(Boolean),
      imageWebp: (find(KEY_WEBP)?.value ?? 'false') === 'true',
    }),
  },
  {
    // 供 Uploader 读取（后台保存的上传限制立即生效）
    method: 'GET',
    url: '/config/upload',
    handler: (): UploadSettings => ({
      maxImageMb: num(KEY_IMAGE),
      maxVideoMb: num(KEY_VIDEO),
      maxFileMb: num(KEY_FILE),
      allowedTypes: (find(KEY_TYPES)?.value ?? '').split(',').filter(Boolean),
      imageWebp: (find(KEY_WEBP)?.value ?? 'false') === 'true',
    }),
  },
  {
    method: 'PUT',
    url: '/configs/upload',
    handler: ({ body }): UploadSettings => {
      const v = (x: unknown) => Math.max(0, Number(x) || 0)
      const next: UploadSettings = {
        maxImageMb: v(body?.maxImageMb),
        maxVideoMb: v(body?.maxVideoMb),
        maxFileMb: v(body?.maxFileMb),
        allowedTypes: Array.isArray(body?.allowedTypes) ? body.allowedTypes : [],
        imageWebp: !!body?.imageWebp,
      }
      put(KEY_IMAGE, String(next.maxImageMb))
      put(KEY_VIDEO, String(next.maxVideoMb))
      put(KEY_FILE, String(next.maxFileMb))
      put(KEY_TYPES, next.allowedTypes.join(','))
      put(KEY_WEBP, String(next.imageWebp))
      return next
    },
  },
  {
    method: 'GET',
    url: '/configs/upload-types',
    handler: () => UPLOAD_TYPE_OPTIONS,
  },
  {
    method: 'POST',
    url: '/configs/refresh-cache',
    handler: () => null,
  },
])

store = createCrudMock<ConfigRow>({
  base: '/configs',
  seed,
  searchFields: ['name', 'key', 'value'],
  filterFields: ['type', 'isPublic'],
})
