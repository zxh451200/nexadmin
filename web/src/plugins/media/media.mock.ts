import { defineMock } from '@/framework/request/mock'
import type { MediaStats, PendingImage, VideoConfigView, VideoItem } from './api'

const ago = (days: number) => new Date(Date.now() - days * 86400000).toISOString()

const images: PendingImage[] = [
  { id: '7600000000000000001', name: 'banner-spring.png', type: 'image/png', size: 842113, url: '/icon-512.png', createdAt: ago(3) },
  { id: '7600000000000000002', name: 'cover-photo.jpg', type: 'image/jpeg', size: 512440, url: '/icon-192.png', createdAt: ago(9) },
]

let config: VideoConfigView = { enabled: true, accountId: 'demo-account', subdomain: 'demo', hasToken: true }

let videos: VideoItem[] = [
  {
    id: '7610000000000000001', name: 'product-tour.mp4', size: 48_211_000, provider: 'cloudflare',
    videoId: 'vid-12345678', playbackUrl: 'https://customer-demo.cloudflarestream.com/vid-12345678/iframe',
    downloadUrl: 'https://customer-demo.cloudflarestream.com/vid-12345678/downloads/default.mp4',
    status: 'ready', uploader: 'admin', createdAt: ago(2),
  },
]

defineMock([
  {
    method: 'GET',
    url: '/plugins/media/stats',
    handler: (): MediaStats => ({ total: 12, done: 10, pending: images.length }),
  },
  {
    method: 'GET',
    url: '/plugins/media/images',
    handler: () => ({ list: images, total: images.length, page: 1, pageSize: 20 }),
  },
  {
    method: 'GET',
    url: '/plugins/media/video/config',
    handler: (): VideoConfigView => config,
  },
  {
    method: 'PUT',
    url: '/plugins/media/video/config',
    handler: ({ body }): VideoConfigView => {
      config = {
        enabled: !!body?.enabled,
        accountId: String(body?.accountId ?? ''),
        subdomain: String(body?.subdomain ?? ''),
        hasToken: config.hasToken || !!body?.token,
      }
      return config
    },
  },
  {
    method: 'POST',
    url: '/plugins/media/video/upload',
    handler: ({ body }) => ({
      uploadUrl: 'https://upload.example/tus/mock',
      videoId: 'vid-87654321',
      playbackUrl: `https://customer-demo.cloudflarestream.com/vid-87654321/iframe?name=${encodeURIComponent(String(body?.name ?? ''))}`,
    }),
  },
  {
    method: 'GET',
    url: '/plugins/media/video/assets',
    handler: () => ({ list: videos, total: videos.length, page: 1, pageSize: 10 }),
  },
  {
    method: 'POST',
    url: '/plugins/media/video/assets',
    handler: ({ body }): VideoItem => {
      const row: VideoItem = {
        id: String(7620000000000000000 + videos.length),
        name: String(body?.name ?? ''),
        size: Number(body?.size ?? 0),
        provider: 'cloudflare',
        videoId: String(body?.videoId ?? ''),
        playbackUrl: `https://customer-demo.cloudflarestream.com/${String(body?.videoId ?? '')}/iframe`,
        downloadUrl: `https://customer-demo.cloudflarestream.com/${String(body?.videoId ?? '')}/downloads/default.mp4`,
        status: 'ready',
        uploader: 'admin',
        createdAt: new Date().toISOString(),
      }
      videos = [row, ...videos]
      return row
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/media/video/assets/:id',
    handler: ({ params }) => {
      videos = videos.filter((v) => v.id !== params.id)
      return { ok: true }
    },
  },
])
