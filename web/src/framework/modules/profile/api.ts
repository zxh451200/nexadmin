import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { startOAuthBind } from '@/framework/auth/oauth'
import { useAuth } from '@/framework/auth/useAuth'
import { api } from '@/framework/request/http'

export interface Profile {
  id: string
  username: string
  nickname: string
  avatar?: string
  gender: '0' | '1' | '2'
  phone?: string
  email?: string
  province?: string
  city?: string
  district?: string
  bio?: string
}

export type ProfileInput = Omit<Profile, 'id' | 'username' | 'phone'>

export interface ProviderBinding {
  key: 'wecom' | 'dingtalk' | 'feishu' | 'google' | 'github'
  name: string
  bound: boolean
  externalName?: string
  boundAt?: string
}

export interface Bindings {
  phone?: string
  providers: ProviderBinding[]
}

export interface LoginSession {
  id: string
  device: 'web' | 'mobile' | 'tablet' | 'other'
  browser?: string
  os?: string
  ip: string
  location?: string
  loginAt: string
  lastActiveAt: string
  current: boolean
}

export function useProfile() {
  return useQuery({ queryKey: ['profile', 'me'], queryFn: () => api.get<Profile>('/profile') })
}

export function useUpdateProfile() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: ProfileInput) => api.put<Profile>('/profile', body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['profile', 'me'] })
      void useAuth.getState().fetchMe().catch(() => undefined)
    },
  })
}

export function useChangePassword() {
  return useMutation({
    mutationFn: (body: { oldPassword: string; newPassword: string }) => api.put<null>('/profile/password', body),
  })
}

export function useBindings() {
  return useQuery({ queryKey: ['profile', 'bindings'], queryFn: () => api.get<Bindings>('/profile/bindings') })
}

export function useSendSmsCode() {
  return useMutation({ mutationFn: (phone: string) => api.post<null>('/auth/sms-code', { phone }) })
}

export function useBindPhone() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: { phone: string; code: string }) => api.post<null>('/profile/bindings/phone', body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['profile'] })
    },
  })
}

/** 发起第三方绑定：服务端返回授权地址，调用方整页跳转；授权回来后由 /login/oauth/:provider 页完成绑定 */
export function useBindProvider() {
  return useMutation({ mutationFn: (key: string) => startOAuthBind(key) })
}

/** key 为 'phone' 或第三方 provider key */
export function useUnbind() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (key: string) => api.del<null>(`/profile/bindings/${key}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['profile'] }),
  })
}

export function useSessions() {
  return useQuery({ queryKey: ['profile', 'sessions'], queryFn: () => api.get<LoginSession[]>('/profile/sessions') })
}

export function useKickSession() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del<null>(`/profile/sessions/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['profile', 'sessions'] }),
  })
}

export type TokenScope = 'read' | 'write'

export interface AccessToken {
  id: string
  name: string
  scope: TokenScope
  /** 如 nxp_ab12，仅用于识别 */
  prefix: string
  lastUsedAt?: string | null
  /** 空 = 永不过期 */
  expiresAt?: string | null
  createdAt: string
}

/** 创建成功时额外返回 token 明文，只显示一次 */
export type AccessTokenCreated = AccessToken & { token: string }

export interface AccessTokenInput {
  name: string
  scope: TokenScope
  expiresAt?: string
}

export function useAccessTokens() {
  return useQuery({ queryKey: ['profile', 'tokens'], queryFn: () => api.get<AccessToken[]>('/profile/tokens') })
}

export function useCreateAccessToken() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: AccessTokenInput) => api.post<AccessTokenCreated>('/profile/tokens', body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['profile', 'tokens'] }),
  })
}

export function useRevokeAccessToken() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.del<null>(`/profile/tokens/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['profile', 'tokens'] }),
  })
}
