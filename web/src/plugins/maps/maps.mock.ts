import { defineMock } from '@/framework/request/mock'
import type { MapsConfigView } from './api'

let config: MapsConfigView = {
  amap: { enabled: false, publicKey: '', hasServerKey: false },
  baidu: { enabled: false, publicKey: '', hasServerKey: false },
  google: { enabled: false, publicKey: '', hasServerKey: false },
}

defineMock([
  {
    method: 'GET',
    url: '/plugins/maps/config',
    handler: (): MapsConfigView => config,
  },
  {
    method: 'PUT',
    url: '/plugins/maps/config',
    handler: ({ body }): MapsConfigView => {
      const pick = (id: 'amap' | 'baidu' | 'google') => ({
        enabled: !!body?.[id]?.enabled,
        publicKey: String(body?.[id]?.publicKey ?? ''),
        hasServerKey: config[id].hasServerKey || !!body?.[id]?.serverKey,
      })
      config = { amap: pick('amap'), baidu: pick('baidu'), google: pick('google') }
      return config
    },
  },
  {
    method: 'GET',
    url: '/plugins/maps/regeo',
    handler: ({ query }): { address: string } => {
      const provider = String(query?.provider ?? 'amap')
      const names: Record<string, string> = {
        amap: '北京市东城区天安门广场',
        baidu: '北京市东城区天安门广场（百度）',
        google: 'Tiananmen Square, Dongcheng, Beijing',
      }
      return { address: names[provider] ?? names.amap }
    },
  },
])
