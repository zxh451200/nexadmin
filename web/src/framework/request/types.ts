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

/**
 * 一门语言的完整档案，与 core 的 sys_language 同构：全站只有这一份语言清单，
 * 界面文案、公开站路由、字典标签、导出表头都读它。
 */
export interface LanguageInfo {
  code: string
  name: string
  dir: 'ltr' | 'rtl'
  default: boolean
  /** 紧凑展示名（列表徽标一类） */
  short?: string
  /** 写进 <html lang> 与 hreflang；缺省等于 code */
  htmlLang?: string
  /** 写进 og:locale；缺省由 code 推导（en-US → en_US） */
  ogLocale?: string
  /** 公开站的 URL 路径前缀；默认语言为空 */
  prefix?: string
  /** Go 布局串 */
  dateFormat?: string
}

export interface UploadConfig {
  maxImageMb: number
  maxVideoMb: number
  maxFileMb: number
  allowedTypes: string[]
}
