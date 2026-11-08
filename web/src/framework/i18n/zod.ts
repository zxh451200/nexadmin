import { z } from 'zod'

import i18n from './init'

/**
 * zod 的默认错误文案是英文的（"String must contain at least 1 character(s)"），
 * 中文界面上会直接露出来。这里把常用的几类 issue 映射到 i18n 文案，其余保持默认。
 * 校验发生在提交时，所以文案跟着当前语言走，不需要重启。
 */
const t = (key: string, opts?: Record<string, unknown>) => i18n.t(key, opts)

z.config({
  customError: (issue) => {
    switch (issue.code) {
      case 'invalid_type':
        if (issue.input === undefined || issue.input === null) return { message: t('validation.required') }
        return { message: t('validation.invalidType', { expected: issue.expected }) }
      case 'too_small':
        if (issue.origin === 'string' && Number(issue.minimum) <= 1) return { message: t('validation.required') }
        if (issue.origin === 'array') return { message: t('validation.pickAtLeast', { minimum: issue.minimum }) }
        return { message: t('validation.tooSmall', { minimum: issue.minimum }) }
      case 'too_big':
        return { message: t('validation.tooBig', { maximum: issue.maximum }) }
      case 'invalid_format':
        switch (issue.format) {
          case 'email':
            return { message: t('validation.email') }
          case 'url':
            return { message: t('validation.url') }
          case 'uuid':
            return { message: t('validation.invalidType', { expected: 'UUID' }) }
          default:
            return { message: t('validation.pattern') }
        }
      case 'invalid_value':
        return { message: t('validation.required') }
      default:
        return undefined
    }
  },
})
