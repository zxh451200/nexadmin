import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, http } from '@/framework/request/http'
import type { Page, PageQuery } from '@/framework/request/types'

export interface SiteSettings {
  id: string
  siteName: string
  siteDesc: string
  logoUrl: string
  homeTitle: string
  homeSubtitle: string
  homeCtaLabel: string
  homeCtaHref: string
  seoKeywords: string
  contactEmail: string
  icp: string
  footerNote: string
  marketEnabled: boolean
  i18nJson: string
  updatedAt: string
}

export type SiteSettingsForm = Omit<SiteSettings, 'id' | 'updatedAt'>

export interface NavItem {
  id: string
  label: string
  href: string
  sort: number
  enabled: boolean
  i18nJson: string
}

export interface NavForm {
  label: string
  href: string
  sort: number
  enabled: boolean
  i18nJson: string
}

export interface FeatureItem {
  id: string
  icon: string
  title: string
  description: string
  sort: number
  enabled: boolean
  i18nJson: string
}

export interface FeatureForm {
  icon: string
  title: string
  description: string
  sort: number
  enabled: boolean
  i18nJson: string
}

export interface SitePage {
  id: string
  slug: string
  title: string
  seoDesc: string
  content: string
  published: boolean
  i18nJson: string
  updatedAt: string
}

export interface PageForm {
  slug: string
  title: string
  seoDesc: string
  content: string
  published: boolean
  i18nJson: string
}

export interface SitePost {
  id: string
  slug: string
  title: string
  coverUrl: string
  summary: string
  content: string
  published: boolean
  publishedAt?: string | null
  i18nJson: string
  updatedAt: string
}

export interface PostForm {
  slug: string
  title: string
  coverUrl: string
  summary: string
  content: string
  published: boolean
  i18nJson: string
}

export interface ContentQuery extends PageQuery {
  status?: string
}

/** 官网已配置的语言（/plugins/website/languages），翻译编辑器据此自动生成输入框。 */
export interface SiteLanguage {
  code: string
  label: string
  htmlLang: string
  default: boolean
}

/** 特性卡片可选的内置图标名（模板内置集，未知名渲染默认圆点）。 */
export const FEATURE_ICONS = [
  'building', 'shield', 'key', 'plug', 'file', 'chart', 'globe', 'puzzle',
  'terminal', 'zap', 'layers', 'box', 'users', 'bell', 'lock', 'code',
] as const

const BASE = '/plugins/website'
const KEY = ['plugin_website'] as const

export function useLanguages() {
  return useQuery({
    queryKey: [...KEY, 'languages'],
    queryFn: () => api.get<SiteLanguage[]>(`${BASE}/languages`),
    staleTime: Infinity,
  })
}

export function useSettings() {
  return useQuery({
    queryKey: [...KEY, 'settings'],
    queryFn: () => api.get<SiteSettings>(`${BASE}/settings`),
  })
}

export function useSaveSettings() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (data: SiteSettingsForm) => api.put<SiteSettings>(`${BASE}/settings`, data),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'settings'] }),
  })
}

/** 上传 logo / 封面小图（PNG/JPEG/WebP/GIF，≤2MB），返回可直接使用的站内 URL。 */
export function uploadMedia(file: File) {
  const fd = new FormData()
  fd.append('file', file)
  return http
    .post<{ url: string; mime: string; size: number }>(`${BASE}/media`, fd)
    .then((r) => r.data)
}

export function useNavs() {
  return useQuery({
    queryKey: [...KEY, 'navs'],
    queryFn: () => api.get<NavItem[]>(`${BASE}/navs`),
  })
}

export function useSaveNav() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: NavForm }) =>
      id ? api.put<NavItem>(`${BASE}/navs/${id}`, data) : api.post<NavItem>(`${BASE}/navs`, data),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'navs'] }),
  })
}

export function useDeleteNav() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/navs/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'navs'] }),
  })
}

export function useFeatures() {
  return useQuery({
    queryKey: [...KEY, 'features'],
    queryFn: () => api.get<FeatureItem[]>(`${BASE}/features`),
  })
}

export function useSaveFeature() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: FeatureForm }) =>
      id ? api.put<FeatureItem>(`${BASE}/features/${id}`, data) : api.post<FeatureItem>(`${BASE}/features`, data),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'features'] }),
  })
}

export function useDeleteFeature() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/features/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'features'] }),
  })
}

export function usePages(query: ContentQuery) {
  return useQuery({
    queryKey: [...KEY, 'pages', query],
    queryFn: () => api.get<Page<SitePage>>(`${BASE}/pages`, query),
    placeholderData: keepPreviousData,
  })
}

export function useSavePage() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: PageForm }) =>
      id ? api.put<SitePage>(`${BASE}/pages/${id}`, data) : api.post<SitePage>(`${BASE}/pages`, data),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'pages'] }),
  })
}

export function useDeletePage() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/pages/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'pages'] }),
  })
}

export function usePosts(query: ContentQuery) {
  return useQuery({
    queryKey: [...KEY, 'posts', query],
    queryFn: () => api.get<Page<SitePost>>(`${BASE}/posts`, query),
    placeholderData: keepPreviousData,
  })
}

export function useSavePost() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: PostForm }) =>
      id ? api.put<SitePost>(`${BASE}/posts/${id}`, data) : api.post<SitePost>(`${BASE}/posts`, data),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'posts'] }),
  })
}

export function useDeletePost() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/posts/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'posts'] }),
  })
}
