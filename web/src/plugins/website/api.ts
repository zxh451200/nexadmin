import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo } from 'react'
import { api, http } from '@/framework/request/http'
import { useLanguages as usePlatformLanguages } from '@/framework/i18n/useLanguages'
import type { EntitySchema, LocaleOption } from '@/framework/components/i18n'
import type { Page, PageQuery } from '@/framework/request/types'

/** 后端 entityViews 的键；新增内容类型时同步。 */
export type I18nEntity = 'setting' | 'nav' | 'feature' | 'page' | 'post'

/** 页脚社交图标可选平台；键集合与后端 socialPlatforms 一致。 */
export type SocialPlatform = 'github' | 'x' | 'linkedin' | 'youtube' | 'wechat' | 'weibo' | 'bilibili' | 'rss' | 'mail'

export interface SocialLink {
  platform: SocialPlatform
  url: string
}

export interface FriendLink {
  name: string
  url: string
}

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
  contactPhone: string
  qrCodeUrl: string
  qrCodeCaption: string
  /** 页脚社交图标（后端 JSON 数组展开） */
  socials: SocialLink[]
  /** 页脚友情链接 */
  friendLinks: FriendLink[]
  icp: string
  footerNote: string
  marketEnabled: boolean
  i18nJson: string
  updatedAt: string
}

export type SiteSettingsForm = Omit<SiteSettings, 'id' | 'updatedAt'>

export interface NavItem {
  id: string
  /** 上级导航 ID；"0" = 顶级（雪花 ID 用字符串避免丢精度） */
  parentId: string
  label: string
  href: string
  sort: number
  enabled: boolean
  i18nJson: string
}

export interface NavForm {
  parentId: string
  label: string
  href: string
  sort: number
  enabled: boolean
  i18nJson: string
}

/** 拖拽排序：一次提交同一层内的完整顺序。 */
export interface NavOrderItem {
  id: string
  parentId: string
  sort: number
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
  /** 非默认语言发布态；空 = 所有语言沿用 published */
  publishedJson: string
  updatedAt: string
}

export interface PageForm {
  slug: string
  title: string
  seoDesc: string
  content: string
  published: boolean
  i18nJson: string
  publishedJson: string
}

export interface SitePost {
  id: string
  slug: string
  title: string
  coverUrl: string
  summary: string
  seoDesc: string
  content: string
  published: boolean
  publishedAt?: string | null
  i18nJson: string
  /** 非默认语言发布态；空 = 所有语言沿用 published */
  publishedJson: string
  updatedAt: string
}

export interface PostForm {
  slug: string
  title: string
  coverUrl: string
  summary: string
  seoDesc: string
  content: string
  published: boolean
  /** 只在发布时生效；不传则由服务端按「首次发布写当前时间」处理 */
  publishedAt?: string
  i18nJson: string
  publishedJson: string
}

export interface ContentQuery extends PageQuery {
  status?: string
  /** 语言码：筛「该语言完全没有译文」的条目（不是缺某个字段，那看列表里的 done/total 徽标） */
  missing?: string
}

/** 特性卡片内置图标：名称 + SVG 内部标记。路径以后端 iconPaths 为准，前端不另存一份。 */
export interface FeatureIcon {
  name: string
  paths: string
}

const BASE = '/plugins/website'
const KEY = ['plugin_website'] as const

/** 图标集随插件发布变化极少，按 Infinity 缓存，切页面回来不闪空列表。 */
export function useFeatureIcons() {
  return useQuery({
    queryKey: [...KEY, 'feature-icons'],
    queryFn: () => api.get<FeatureIcon[]>(`${BASE}/feature-icons`),
    staleTime: Infinity,
  })
}

/** 翻译表单的字段元数据：白名单与长度只在 Go 里有一份，界面据此渲染。 */
export function useI18nSchema(entity: I18nEntity) {
  return useQuery({
    queryKey: [...KEY, 'i18n-schema', entity],
    queryFn: () => api.get<EntitySchema>(`${BASE}/i18n/schema/${entity}`),
    staleTime: Infinity,
  })
}

/**
 * 批量机翻：同一批必须同一种格式（Format 是请求级的），所以纯文本与富文本分两批。
 * 只回译文、不落库——写库仍走各自的 save 接口，由后端统一清洗。
 */
export async function translateContent(
  entity: I18nEntity, texts: string[], from: string, to: string, format: 'text' | 'html',
): Promise<string[]> {
  const res = await api.post<{ texts: string[] }>(`${BASE}/i18n/translate/${entity}`, { texts, from, to, format })
  return res.texts ?? []
}

/**
 * 内容语言就是平台语言清单（sys_language，见 modules/system/language）：
 * 官网不再单独维护一份，否则「后台有哪些语言」和「官网有哪些语言」会各自跑偏。
 * label 取 name，整成框架 locale 表单需要的最小形状。
 */
export function useLanguages() {
  const { languages, isLoading } = usePlatformLanguages()
  // 必须 memo：LocaleFormProvider 的 context 以 locales 为依赖，
  // 每次渲染都新建数组会让整棵表单子树无谓重渲染。
  const data = useMemo<LocaleOption[]>(
    () => languages.map((l) => ({ code: l.code, label: l.name, default: l.default, prefix: l.prefix })),
    [languages],
  )
  return { data, isLoading }
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

/** 拖拽排序 / 改上级：后端在一个事务里批量写 sort 与 parentId。 */
export function useReorderNavs() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (items: NavOrderItem[]) => api.put<null>(`${BASE}/navs/reorder`, { items }),
    // 失败时 TreeTable 会回滚乐观数据并 toast，这里只保证最终与库一致
    onSettled: () => void qc.invalidateQueries({ queryKey: [...KEY, 'navs'] }),
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

/** 官网「在线留言」：匿名投稿，后台只读/标记/删除。 */
export interface SiteMessage {
  id: string
  name: string
  email: string
  phone: string
  content: string
  ip: string
  ua: string
  read: boolean
  createdAt: string
  updatedAt: string
}

export interface MessageQuery {
  page: number
  pageSize: number
  keyword?: string
  unread?: boolean
}

export function useMessages(query: MessageQuery) {
  return useQuery({
    queryKey: [...KEY, 'messages', query],
    queryFn: () => api.get<Page<SiteMessage>>(`${BASE}/messages`, { ...query, unread: query.unread ? 1 : undefined }),
    placeholderData: keepPreviousData,
  })
}

export function useSetMessageRead() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, read }: { id: string; read: boolean }) => api.put<null>(`${BASE}/messages/${id}/read`, { read }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'messages'] }),
  })
}

export function useDeleteMessage() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/messages/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...KEY, 'messages'] }),
  })
}
