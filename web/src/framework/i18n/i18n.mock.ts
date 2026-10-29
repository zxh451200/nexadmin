import { defineMock } from '@/framework/request/mock'
import type { LanguageInfo } from '@/framework/request/types'

// 平台语言清单的 mock 镜像，字段与 sys_language / core.Language 一致。
// 第三门语言（ja-JP）是故意的：多语言编辑器「表单高度不随语言数增长」这条主张，
// 只有三档以上才验得出来。
const languages: LanguageInfo[] = [
  { code: 'zh-CN', name: '简体中文', dir: 'ltr', default: true, short: '中', htmlLang: 'zh-CN', ogLocale: 'zh_CN', prefix: '', dateFormat: '2006年1月2日' },
  { code: 'en-US', name: 'English', dir: 'ltr', default: false, short: 'EN', htmlLang: 'en', ogLocale: 'en_US', prefix: 'en', dateFormat: 'Jan 2, 2006' },
  { code: 'ja-JP', name: '日本語', dir: 'ltr', default: false, short: 'JA', htmlLang: 'ja', ogLocale: 'ja_JP', prefix: 'ja', dateFormat: '2006年1月2日' },
]

defineMock([
  { method: 'GET', url: '/i18n/languages', handler: () => languages },
  { method: 'GET', url: '/languages', handler: () => languages },
  {
    method: 'PUT',
    url: '/languages',
    handler: ({ body }) => ({ languages: body.languages ?? languages, needRestart: true }),
  },
])
