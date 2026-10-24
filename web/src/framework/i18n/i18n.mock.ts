import { defineMock } from '@/framework/request/mock'
import type { LanguageInfo } from '@/framework/request/types'

const languages: LanguageInfo[] = [
  { code: 'zh-CN', name: '简体中文', dir: 'ltr', default: true },
  { code: 'en-US', name: 'English', dir: 'ltr' },
]

defineMock([{ method: 'GET', url: '/i18n/languages', handler: () => languages }])
