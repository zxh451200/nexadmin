import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'

export type MapProviderId = 'amap' | 'baidu' | 'google'

export interface ProviderView {
  enabled: boolean
  publicKey: string
  hasServerKey: boolean
}

export type MapsConfigView = Record<MapProviderId, ProviderView>

export interface ProviderInput {
  enabled: boolean
  publicKey: string
  serverKey: string
}

export type MapsConfigInput = Record<MapProviderId, ProviderInput>

export function useMapsConfig() {
  return useQuery({ queryKey: ['maps', 'config'], queryFn: () => api.get<MapsConfigView>('/plugins/maps/config') })
}

export function useSaveMapsConfig() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: MapsConfigInput) => api.put<MapsConfigView>('/plugins/maps/config', body),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['maps'] }),
  })
}

/** 逆地理编码统一走后端代理（服务端 Key 不下发浏览器）。 */
export function regeo(provider: MapProviderId, lng: number, lat: number) {
  return api.get<{ address: string }>('/plugins/maps/regeo', { provider, lng, lat })
}
