import { useQuery } from '@tanstack/react-query'
import { api } from '@/framework/request/http'

export interface AboutInfo {
  version: string
  /** ISO 时间 */
  buildTime: string
  dbEngine: string
  goVersion: string
  os: string
}

export interface LicenseRow {
  name: string
  version: string
  license: string
}

export function useAbout() {
  return useQuery({ queryKey: ['about', 'info'], queryFn: () => api.get<AboutInfo>('/about') })
}

export function useLicenses() {
  return useQuery({
    queryKey: ['about', 'licenses'],
    queryFn: () => api.get<{ list: LicenseRow[] }>('/about/licenses'),
    staleTime: Infinity,
  })
}
