export interface ApiResponse<T = unknown> {
  code: number
  message: string
  data: T
  /** 机器可识别原因，如 CAPTCHA_REQUIRED、NOT_INITIALIZED */
  reason?: string
}

export interface Page<T> {
  list: T[]
  total: number
  page: number
  pageSize: number
}

export interface PageQuery {
  page?: number
  pageSize?: number
  keyword?: string
  [k: string]: unknown
}

export interface DictItem {
  value: string
  label: string
  tag?: 'default' | 'success' | 'warning' | 'destructive' | 'info'
}

export interface RegionNode {
  code: string
  name: string
  pinyin: string
  children?: RegionNode[]
}

export interface LanguageInfo {
  code: string
  name: string
  dir: 'ltr' | 'rtl'
  default?: boolean
}

export interface UploadConfig {
  maxImageMb: number
  maxVideoMb: number
  maxFileMb: number
  allowedTypes: string[]
}
