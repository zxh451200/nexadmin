import { useCallback } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { loadLanguages } from './init'

export function useLanguages() {
  const { i18n } = useTranslation()
  const { data = [], isLoading } = useQuery({
    queryKey: ['i18n', 'languages'],
    queryFn: loadLanguages,
    staleTime: Infinity,
  })
  const change = useCallback((code: string) => i18n.changeLanguage(code), [i18n])
  return { languages: data, current: i18n.language, change, isLoading }
}
