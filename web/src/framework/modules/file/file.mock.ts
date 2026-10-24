import { defineMock, genId, mockError } from '@/framework/request/mock'
import type { FileCategory, FileGroup, FileItem } from './api'

const hues = [210, 262, 174, 24, 340, 142, 48, 196]

function placeholder(i: number, label: string): string {
  const h = hues[i % hues.length]
  const h2 = (h + 40) % 360
  const svg =
    `<svg xmlns="http://www.w3.org/2000/svg" width="480" height="360" viewBox="0 0 480 360">` +
    `<defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1">` +
    `<stop offset="0" stop-color="hsl(${h} 70% 55%)"/><stop offset="1" stop-color="hsl(${h2} 70% 42%)"/>` +
    `</linearGradient></defs><rect width="480" height="360" fill="url(#g)"/>` +
    `<circle cx="${120 + (i % 4) * 70}" cy="120" r="54" fill="hsl(0 0% 100% / 0.18)"/>` +
    `<rect x="60" y="220" width="360" height="14" rx="7" fill="hsl(0 0% 100% / 0.25)"/>` +
    `<rect x="60" y="250" width="220" height="14" rx="7" fill="hsl(0 0% 100% / 0.18)"/>` +
    `<text x="240" y="190" font-family="sans-serif" font-size="28" fill="hsl(0 0% 100%)" text-anchor="middle">${label}</text></svg>`
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`
}

const groups: FileGroup[] = [
  { id: '7201000000000000101', name: '产品图片' },
  { id: '7201000000000000102', name: '宣传视频' },
  { id: '7201000000000000103', name: '合同文档' },
  { id: '7201000000000000104', name: 'User Avatars' },
]

const uploaders = ['admin', '张伟', 'Emily Chen', '李娜', 'Michael']
const iso = (daysAgo: number, hour: number) => {
  const d = new Date()
  d.setDate(d.getDate() - daysAgo)
  d.setHours(hour, (daysAgo * 7) % 60, 0, 0)
  return d.toISOString()
}

interface Spec {
  name: string
  category: FileCategory
  type: string
  size: number
  group: number // groups index, -1 = none
  vis: 'public' | 'private'
}

const specs: Spec[] = [
  { name: 'banner-spring.png', category: 'image', type: 'image/png', size: 842_113, group: 0, vis: 'public' },
  { name: 'product-front.jpg', category: 'image', type: 'image/jpeg', size: 512_440, group: 0, vis: 'public' },
  { name: 'product-side.jpg', category: 'image', type: 'image/jpeg', size: 498_021, group: 0, vis: 'public' },
  { name: '产品详情图-01.png', category: 'image', type: 'image/png', size: 1_204_551, group: 0, vis: 'public' },
  { name: '产品详情图-02.png', category: 'image', type: 'image/png', size: 1_180_320, group: 0, vis: 'private' },
  { name: 'logo-dark.svg', category: 'image', type: 'image/svg+xml', size: 8_420, group: 0, vis: 'public' },
  { name: 'avatar-zhangwei.png', category: 'image', type: 'image/png', size: 64_220, group: 3, vis: 'private' },
  { name: 'avatar-emily.jpg', category: 'image', type: 'image/jpeg', size: 71_904, group: 3, vis: 'private' },
  { name: 'avatar-lina.webp', category: 'image', type: 'image/webp', size: 38_210, group: 3, vis: 'private' },
  { name: 'team-photo.jpg', category: 'image', type: 'image/jpeg', size: 2_310_998, group: -1, vis: 'private' },
  { name: 'office-night.jpg', category: 'image', type: 'image/jpeg', size: 1_870_332, group: -1, vis: 'public' },
  { name: 'promo-2026.mp4', category: 'video', type: 'video/mp4', size: 48_220_113, group: 1, vis: 'public' },
  { name: 'product-demo.mp4', category: 'video', type: 'video/mp4', size: 31_902_440, group: 1, vis: 'public' },
  { name: '客户访谈-录屏.webm', category: 'video', type: 'video/webm', size: 76_330_901, group: 1, vis: 'private' },
  { name: 'onboarding-tutorial.mov', category: 'video', type: 'video/quicktime', size: 102_450_220, group: 1, vis: 'private' },
  { name: '采购合同-2026-A.pdf', category: 'doc', type: 'application/pdf', size: 1_540_221, group: 2, vis: 'private' },
  { name: '采购合同-2026-B.pdf', category: 'doc', type: 'application/pdf', size: 1_322_908, group: 2, vis: 'private' },
  { name: 'NDA-template.docx', category: 'doc', type: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', size: 96_330, group: 2, vis: 'private' },
  { name: 'Q3-report.xlsx', category: 'doc', type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet', size: 412_090, group: -1, vis: 'private' },
  { name: '季度汇报.pptx', category: 'doc', type: 'application/vnd.openxmlformats-officedocument.presentationml.presentation', size: 5_210_440, group: -1, vis: 'public' },
  { name: 'readme.txt', category: 'doc', type: 'text/plain', size: 2_104, group: -1, vis: 'public' },
  { name: 'backup-20261001.zip', category: 'other', type: 'application/zip', size: 220_450_118, group: -1, vis: 'private' },
  { name: 'fonts-pack.tar.gz', category: 'other', type: 'application/gzip', size: 18_330_220, group: -1, vis: 'public' },
  { name: 'voice-note.mp3', category: 'other', type: 'audio/mpeg', size: 3_450_100, group: -1, vis: 'private' },
]

let files: FileItem[] = specs.map((s, i) => {
  const isImg = s.category === 'image'
  const isVid = s.category === 'video'
  return {
    id: `72010000000000002${String(i).padStart(2, '0')}`,
    name: s.name,
    url: isImg ? placeholder(i, s.name.slice(0, 14)) : isVid ? '' : '',
    posterUrl: isVid ? placeholder(i, 'VIDEO') : undefined,
    type: s.type,
    category: s.category,
    size: s.size,
    visibility: s.vis,
    groupId: s.group >= 0 ? groups[s.group].id : '',
    uploader: uploaders[i % uploaders.length],
    createdAt: iso(i, 9 + (i % 9)),
  }
})

const classify = (type: string, name: string): FileCategory => {
  const n = name.toLowerCase()
  if (type.startsWith('image/') || /\.(png|jpe?g|gif|webp|svg)$/.test(n)) return 'image'
  if (type.startsWith('video/') || /\.(mp4|webm|mov)$/.test(n)) return 'video'
  if (/pdf|word|excel|sheet|presentation|document|^text\//.test(type) || /\.(pdf|docx?|xlsx?|pptx?|txt)$/.test(n)) return 'doc'
  return 'other'
}

defineMock([
  {
    method: 'GET',
    url: '/file-groups',
    handler: () => groups.map((g) => ({ ...g, count: files.filter((f) => f.groupId === g.id).length })),
  },
  {
    method: 'POST',
    url: '/file-groups',
    handler: ({ body }) => {
      const name = String(body?.name ?? '').trim()
      if (!name) mockError(400, 'name required')
      const g: FileGroup = { id: genId(), name }
      groups.push(g)
      return { ...g, count: 0 }
    },
  },
  {
    method: 'PUT',
    url: '/file-groups/:id',
    handler: ({ params, body }) => {
      const g = groups.find((x) => x.id === params.id) ?? mockError(404, 'Not found')
      g.name = String(body?.name ?? g.name).trim() || g.name
      return g
    },
  },
  {
    method: 'DELETE',
    url: '/file-groups/:id',
    handler: ({ params }) => {
      const idx = groups.findIndex((x) => x.id === params.id)
      if (idx >= 0) groups.splice(idx, 1)
      files = files.map((f) => (f.groupId === params.id ? { ...f, groupId: '' } : f))
      return null
    },
  },
  {
    method: 'GET',
    url: '/files',
    handler: ({ query }) => {
      const page = Math.max(1, Number(query.page) || 1)
      const pageSize = Math.max(1, Number(query.pageSize) || 20)
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let rows = files.slice()
      if (query.groupId) rows = rows.filter((f) => f.groupId === (query.groupId === 'none' ? '' : String(query.groupId)))
      if (query.category) rows = rows.filter((f) => f.category === String(query.category))
      if (kw) rows = rows.filter((f) => f.name.toLowerCase().includes(kw) || f.uploader.toLowerCase().includes(kw))
      if (query.from) rows = rows.filter((f) => f.createdAt.slice(0, 10) >= String(query.from))
      if (query.to) rows = rows.filter((f) => f.createdAt.slice(0, 10) <= String(query.to))
      rows.sort((a, b) => b.createdAt.localeCompare(a.createdAt))
      return { list: rows.slice((page - 1) * pageSize, page * pageSize), total: rows.length, page, pageSize }
    },
  },
  {
    method: 'POST',
    url: '/files',
    handler: ({ body }) => {
      const list: Array<{ name: string; url: string; posterUrl?: string; type: string; size: number }> = body?.files ?? []
      const now = new Date().toISOString()
      const created: FileItem[] = list.map((f) => ({
        id: genId(),
        name: f.name,
        url: f.url,
        posterUrl: f.posterUrl,
        type: f.type,
        category: classify(f.type, f.name),
        size: f.size,
        visibility: body?.visibility === 'public' ? 'public' : 'private',
        groupId: String(body?.groupId ?? ''),
        uploader: 'admin',
        createdAt: now,
      }))
      files = [...created, ...files]
      return created
    },
  },
  {
    method: 'PUT',
    url: '/files/move',
    handler: ({ body }) => {
      const ids: string[] = body?.ids ?? []
      const groupId = String(body?.groupId ?? '')
      files = files.map((f) => (ids.includes(f.id) ? { ...f, groupId } : f))
      return null
    },
  },
  {
    method: 'PUT',
    url: '/files/:id',
    handler: ({ params, body }) => {
      const idx = files.findIndex((x) => x.id === params.id)
      if (idx < 0) mockError(404, 'Not found')
      files[idx] = { ...files[idx], ...body, id: params.id }
      return files[idx]
    },
  },
  {
    method: 'DELETE',
    url: '/files/:id',
    handler: ({ params }) => {
      files = files.filter((x) => x.id !== params.id)
      return null
    },
  },
  {
    method: 'DELETE',
    url: '/files',
    handler: ({ query }) => {
      const ids = String(query.ids ?? '').split(',').filter(Boolean)
      files = files.filter((x) => !ids.includes(x.id))
      return null
    },
  },
])
