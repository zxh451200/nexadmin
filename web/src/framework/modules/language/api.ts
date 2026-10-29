import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { LanguageInfo } from '@/framework/request/types'

/** 一门语言；与 core 的 sys_language / 后端 config.Language 同构。 */
export type LanguageRow = LanguageInfo

const KEY = ['language', 'list'] as const

export interface SaveLanguagesResult {
  languages: LanguageRow[]
  /** 前缀或语言集合变了：公开站路由在引擎构建期生成，要重启才生效 */
  needRestart: boolean
}

export function useAdminLanguages() {
  return useQuery({ queryKey: KEY, queryFn: () => api.get<LanguageRow[]>('/languages') })
}

export function useSaveLanguages() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (languages: LanguageRow[]) => api.put<SaveLanguagesResult>('/languages', { languages }),
    // 语言切换器读的是 /i18n/languages（同一份清单的公开视图），一起失效才不会显示旧列表
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: KEY })
      void qc.invalidateQueries({ queryKey: ['i18n', 'languages'] })
    },
  })
}
