import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'

export interface SsoApp {
  id: string
  name: string
  clientId: string
  redirectUris: string[]
  logo: string
  homeUrl: string
  description: string
  enabled: boolean
  /** 空 = 所有登录用户可见 */
  roleIds: string[]
  /** 允许该应用用 client_credentials 令牌（scope org.read）读取通讯录 */
  allowOrgRead: boolean
  createdAt: string
}

/** 创建成功时额外返回 clientSecret，只显示一次 */
export type SsoAppCreated = SsoApp & { clientSecret: string }

export interface SsoAppForm {
  name: string
  redirectUris: string[]
  logo: string
  homeUrl: string
  description: string
  enabled: boolean
  roleIds: string[]
  allowOrgRead: boolean
}

/** 签名密钥信息（不含密钥材料）：active = 当前签名 key；retiring = 仅用于验签，到期后从 JWKS 移除 */
export interface SsoKey {
  kid: string
  createdAt: string
  status: 'active' | 'retiring'
  retiresAt?: string
}

export interface PortalApp {
  id: string
  name: string
  logo: string
  homeUrl: string
  description: string
}

export interface AuthorizeInfo {
  name: string
  logo: string
  scopes: string[]
}

export interface AuthorizeParams {
  clientId: string
  redirectUri: string
  scope: string
  state?: string
  nonce?: string
  codeChallenge?: string
  codeChallengeMethod?: string
}

const KEY = ['sso'] as const

export function useSsoApps() {
  return useQuery({
    queryKey: [...KEY, 'apps'],
    queryFn: async () => {
      const res = await api.get<SsoApp[] | { list: SsoApp[] }>('/sso/apps')
      return Array.isArray(res) ? res : (res.list ?? [])
    },
  })
}

export function useSaveSsoApp() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: SsoAppForm }) =>
      id ? api.put<SsoApp>(`/sso/apps/${id}`, data) : api.post<SsoAppCreated>('/sso/apps', data),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
  })
}

export function useDeleteSsoApp() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del(`/sso/apps/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
  })
}

export function useSetSsoAppEnabled() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) => api.put(`/sso/apps/${id}/enabled`, { enabled }),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
  })
}

export function useResetSsoSecret() {
  return useMutation({
    mutationFn: (id: string) => api.post<{ clientSecret: string }>(`/sso/apps/${id}/reset-secret`),
  })
}

export function useSsoKeys(enabled = true) {
  return useQuery({
    queryKey: [...KEY, 'keys'],
    queryFn: () => api.get<SsoKey[]>('/sso/keys'),
    enabled,
  })
}

/** 轮换签名密钥：新 key 立即成为签名 key，旧 key 留在 JWKS 里 7 天后移除 */
export function useRotateSsoKeys() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api.post<SsoKey[]>('/sso/keys/rotate'),
    onSuccess: (keys) => qc.setQueryData([...KEY, 'keys'], keys),
  })
}

export function usePortalApps() {
  return useQuery({
    queryKey: [...KEY, 'portal'],
    queryFn: () => api.get<PortalApp[]>('/sso/portal'),
  })
}

export function useAuthorizeInfo(clientId: string, redirectUri: string) {
  return useQuery({
    queryKey: [...KEY, 'authorize-info', clientId, redirectUri],
    queryFn: () => api.get<AuthorizeInfo>('/oauth/authorize/info', { client_id: clientId, redirect_uri: redirectUri }, { silent: true }),
    enabled: !!clientId && !!redirectUri,
    retry: false,
    staleTime: 0,
  })
}

export function useAuthorize() {
  return useMutation({
    mutationFn: (p: AuthorizeParams) => api.post<{ redirectTo: string }>('/oauth/authorize', p),
  })
}

/** 回调地址校验：完整 URL，https，或 localhost / 127.0.0.1 的 http(s)；不允许带 # 片段 */
export function isValidRedirectUri(raw: string): boolean {
  try {
    const u = new URL(raw)
    if (u.hash) return false
    if (u.protocol === 'https:') return !!u.hostname
    const local = u.hostname === 'localhost' || u.hostname === '127.0.0.1'
    return u.protocol === 'http:' && local
  } catch {
    return false
  }
}

/** 站外跳转前的协议白名单，防 javascript: 等 */
export function isHttpUrl(raw: string): boolean {
  try {
    const p = new URL(raw).protocol
    return p === 'https:' || p === 'http:'
  } catch {
    return false
  }
}

/** 下游应用接入所需信息；issuer 带 /api/v1/oauth 后缀，与后端默认 issuer 一致（标准发现文档直接可用） */
export function buildEndpoints(origin: string) {
  const site = origin.replace(/\/+$/, '')
  const issuer = `${site}/api/v1/oauth`
  return {
    issuer,
    discovery: `${issuer}/.well-known/openid-configuration`,
    authorization: `${site}/sso/authorize`,
    token: `${issuer}/token`,
  }
}
