import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useAuth } from '@/framework/auth/useAuth'
import { api } from '@/framework/request/http'
import { useTheme } from '@/framework/theme/ThemeProvider'

export interface PublicSettings {
  siteName?: string
  logoUrl?: string
  copyright?: string
  watermark?: boolean
}

/** 公开站点设置（Sidebar 品牌区与水印共用同一个缓存键） */
export function usePublicSettings() {
  return useQuery({
    queryKey: ['settings', 'public'],
    queryFn: () => api.get<PublicSettings>('/settings/public', undefined, { silent: true }),
    staleTime: 5 * 60_000,
    retry: false,
  })
}

const TILE_W = 280
const TILE_H = 160

function drawTile(text: string): string {
  const canvas = document.createElement('canvas')
  const dpr = Math.min(window.devicePixelRatio || 1, 2)
  canvas.width = TILE_W * dpr
  canvas.height = TILE_H * dpr
  const ctx = canvas.getContext('2d')
  if (!ctx) return ''
  ctx.scale(dpr, dpr)
  const fg = getComputedStyle(document.documentElement).getPropertyValue('--foreground').trim()
  ctx.fillStyle = fg ? `hsl(${fg} / 0.06)` : 'rgba(128,128,128,0.06)'
  ctx.font = '14px sans-serif'
  ctx.textAlign = 'center'
  ctx.textBaseline = 'middle'
  ctx.translate(TILE_W / 2, TILE_H / 2)
  ctx.rotate((-20 * Math.PI) / 180)
  ctx.fillText(text, 0, 0)
  return canvas.toDataURL('image/png')
}

function today() {
  const d = new Date()
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

export function Watermark() {
  const { data } = usePublicSettings()
  const user = useAuth((s) => s.user)
  const { resolved } = useTheme()
  const [url, setUrl] = useState('')
  const enabled = data?.watermark === true
  const text = user ? `${user.nickname || user.username} ${user.username} ${today()}` : ''

  // 主题切换后 CSS 变量需等 class 应用完成，放到下一帧再取色
  useEffect(() => {
    if (!enabled || !text) {
      setUrl('')
      return
    }
    const id = requestAnimationFrame(() => setUrl(drawTile(text)))
    return () => cancelAnimationFrame(id)
  }, [enabled, text, resolved])

  if (!enabled || !url) return null
  return (
    <div
      aria-hidden="true"
      className="pointer-events-none fixed inset-0 z-40"
      style={{ backgroundImage: `url(${url})`, backgroundSize: `${TILE_W}px ${TILE_H}px`, backgroundRepeat: 'repeat' }}
    />
  )
}
