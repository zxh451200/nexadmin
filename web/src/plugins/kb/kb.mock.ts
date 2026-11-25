import { defineMock, genId } from '@/framework/request/mock'
import type { KbAnswer, KbChunk, KbDocument, KbHit, KbMiss } from './api'

// 知识库 mock：文档 / 设置 / 状态 / 测试提问 / 知识块运营 / 重命名 / 删除 / 重嵌 / 重排队。
// 开发态没有 AI 网关，mock 让整条管理链路（含异步入库、召回调试候选）都能走通。

const NOW = new Date().toISOString()
const EMBED_MODEL = 'text-embedding-v3'

// 单块启停的开发态覆盖：key 是 `${docId}:${seq}`，缺省视为启用。
const chunkEnabled = new Map<string, boolean>()

const docs: KbDocument[] = [
  {
    id: genId(), title: '产品常见问题 FAQ', sourceName: 'faq.txt', mime: 'text/plain',
    size: 18_400, status: 'ready', chunkCount: 12, model: EMBED_MODEL, dimension: 1024,
    createdAt: new Date(Date.now() - 3 * 86400_000).toISOString(),
  },
  {
    // 异步入库中间态：列表会自动轮询到落定，用来验证 stage 展示与排队筛选。
    id: genId(), title: '新版价格表（入库中）', sourceName: 'pricing-2026.pdf', mime: 'application/pdf',
    size: 63_000, status: 'processing', stage: '向量化 12/40 块', chunkCount: 0, model: EMBED_MODEL, dimension: 1024,
    createdAt: new Date(Date.now() - 120_000).toISOString(),
  },
  {
    id: genId(), title: '售后与退换政策', sourceName: 'after-sales.md', mime: 'text/markdown',
    size: 9_200, status: 'ready', chunkCount: 6, model: EMBED_MODEL, dimension: 1024,
    createdAt: new Date(Date.now() - 1 * 86400_000).toISOString(),
  },
  {
    id: genId(), title: '物流说明（旧文档）', sourceName: 'shipping.pdf', mime: 'application/pdf',
    size: 120_000, status: 'ready', chunkCount: 8, model: 'text-embedding-v2', dimension: 1536,
    createdAt: new Date(Date.now() - 20 * 86400_000).toISOString(),
  },
  {
    id: genId(), title: '内部试用资料', sourceName: 'trial.docx', mime: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
    size: 45_000, status: 'failed', chunkCount: 0, model: '', dimension: 0,
    error: '文档没有可用内容', createdAt: new Date(Date.now() - 3600_000).toISOString(),
  },
]

const misses: KbMiss[] = [
  { id: genId(), question: 'XR-500 型号的保修期是多久？', askCount: 7, status: 'pending', documentId: '', firstAt: new Date(Date.now() - 6 * 86400_000).toISOString(), lastAt: new Date(Date.now() - 3600_000).toISOString() },
  { id: genId(), question: '可以货到付款吗', askCount: 4, status: 'pending', documentId: '', firstAt: new Date(Date.now() - 4 * 86400_000).toISOString(), lastAt: new Date(Date.now() - 7200_000).toISOString() },
  { id: genId(), question: '企业批量采购有折扣吗', askCount: 3, status: 'resolved', documentId: docs[0]?.id ?? '', firstAt: new Date(Date.now() - 9 * 86400_000).toISOString(), lastAt: new Date(Date.now() - 2 * 86400_000).toISOString() },
  { id: genId(), question: '能开增值税专用发票吗', askCount: 2, status: 'ignored', documentId: '', firstAt: new Date(Date.now() - 12 * 86400_000).toISOString(), lastAt: new Date(Date.now() - 8 * 86400_000).toISOString() },
]

/** 伪分块：按标题生成几条像样的段落文本，块数与 doc.chunkCount 对齐。 */
function makeChunks(doc: KbDocument): KbChunk[] {
  const n = Math.max(1, Math.min(doc.chunkCount, 8))
  return Array.from({ length: n }, (_, i) => {
    const seq = i + 1
    return {
      id: `${doc.id}-c${seq}`,
      seq,
      content: `【${doc.title}】第 ${seq} 段：这是入库时切分出来的知识块原文——与向量一起参与余弦检索，也是「AI 为什么这么答」的直接依据。实际内容来自上传文档的空行分段；每块约 500 字，过短碎块会并入上一块。`,
      enabled: chunkEnabled.get(`${doc.id}:${seq}`) !== false,
    }
  })
}

defineMock([
  { method: 'GET', url: '/plugins/kb/status', handler: () => ({ embedReady: true, chatReady: true, model: 'qwen-plus', embedModel: EMBED_MODEL }) },
  {
    method: 'GET',
    url: '/plugins/kb/settings',
    handler: () => ({ threshold: 0.35, topK: 4, systemPrompt: '', mode: 'hybrid', chunkSize: 500, chunkOverlap: 50 }),
  },
  { method: 'PUT', url: '/plugins/kb/settings', handler: ({ body }) => body },
  {
    method: 'GET',
    url: '/plugins/kb/stats',
    handler: () => ({
      docs: docs.length,
      chunks: docs.reduce((n, d) => n + d.chunkCount, 0),
      ready: docs.filter((d) => d.status === 'ready').length,
      failed: docs.filter((d) => d.status === 'failed').length,
      queued: docs.filter((d) => d.status === 'pending' || d.status === 'processing').length,
      size: docs.reduce((n, d) => n + d.size, 0),
      pendingMiss: misses.filter((m) => m.status === 'pending').length,
    }),
  },
  {
    method: 'GET',
    url: '/plugins/kb/documents',
    // 分页 + 状态筛选都在「服务端」（这里）做：stale = 失败或向量模型与当前配置不一致
    // （排队/处理中的文档正在走正常流程，不算 stale，否则批量重嵌会和后台写切块撞车）；
    // queued = pending + processing 两个异步入库中间态。
    handler: ({ query }) => {
      const status = String(query.status ?? '')
      const isBusy = (d: KbDocument) => d.status === 'pending' || d.status === 'processing'
      const matched = docs.filter((d) => {
        if (!status) return true
        if (status === 'queued') return isBusy(d)
        if (status === 'stale') return !isBusy(d) && (d.status === 'failed' || (d.model !== '' && d.model !== EMBED_MODEL))
        return d.status === status
      })
      const page = Math.max(1, Number(query.page) || 1)
      const pageSize = Math.max(1, Math.min(200, Number(query.pageSize) || 20))
      return {
        list: matched.slice((page - 1) * pageSize, page * pageSize),
        total: matched.length,
        page,
        pageSize,
      }
    },
  },
  {
    method: 'GET',
    url: '/plugins/kb/documents/:id/chunks',
    handler: ({ params }) => {
      const doc = docs.find((x) => x.id === params.id)
      if (!doc) throw Object.assign(new Error('文档不存在'), { code: 404 })
      return { document: doc, chunks: makeChunks(doc) }
    },
  },
  {
    // 编辑单块文案：只改这一块的原文，其余块不变（后端会重算该块向量）。
    method: 'PUT',
    url: '/plugins/kb/documents/:id/chunks/:seq',
    handler: ({ params, body }) => {
      const doc = docs.find((x) => x.id === params.id)
      if (!doc) throw Object.assign(new Error('文档不存在'), { code: 404 })
      const seq = Number(params.seq)
      const content = String(body?.content ?? '')
      return { id: `${doc.id}-c${seq}`, seq, content, enabled: chunkEnabled.get(`${doc.id}:${seq}`) !== false }
    },
  },
  {
    // 启停单块：禁用后不参与检索，可随时恢复。
    method: 'POST',
    url: '/plugins/kb/documents/:id/chunks/:seq/toggle',
    handler: ({ params, body }) => {
      const doc = docs.find((x) => x.id === params.id)
      if (!doc) throw Object.assign(new Error('文档不存在'), { code: 404 })
      const seq = Number(params.seq)
      const enabled = body?.enabled !== false
      chunkEnabled.set(`${doc.id}:${seq}`, enabled)
      const chunk = makeChunks(doc).find((c) => c.seq === seq)
      if (!chunk) throw Object.assign(new Error('知识块不存在'), { code: 404 })
      return chunk
    },
  },
  {
    // 预览重切：用「块数 + 1」模拟参数变化后的切分结果，不落库。
    method: 'GET',
    url: '/plugins/kb/documents/:id/rechunk',
    handler: ({ params }) => {
      const doc = docs.find((x) => x.id === params.id)
      if (!doc) throw Object.assign(new Error('文档不存在'), { code: 404 })
      const chunks = makeChunks({ ...doc, chunkCount: doc.chunkCount + 1 }).map((c) => c.content)
      return { document: doc, chunks, count: chunks.length }
    },
  },
  {
    method: 'POST',
    url: '/plugins/kb/documents/:id/rechunk',
    handler: ({ params }) => {
      const doc = docs.find((x) => x.id === params.id)
      if (!doc) throw Object.assign(new Error('文档不存在'), { code: 404 })
      doc.chunkCount += 1
      doc.status = 'ready'
      doc.model = EMBED_MODEL
      doc.dimension = 1024
      doc.error = ''
      return doc
    },
  },
  {
    method: 'PUT',
    url: '/plugins/kb/documents/:id',
    handler: ({ params, body }) => {
      const doc = docs.find((x) => x.id === params.id)
      if (!doc) throw Object.assign(new Error('文档不存在'), { code: 404 })
      doc.title = String(body?.title ?? doc.title).slice(0, 200)
      return doc
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/kb/documents/:id',
    handler: ({ params }) => {
      const i = docs.findIndex((x) => x.id === params.id)
      if (i >= 0) docs.splice(i, 1)
      return { ok: true }
    },
  },
  {
    method: 'POST',
    url: '/plugins/kb/documents/:id/reembed',
    handler: ({ params }) => {
      const doc = docs.find((x) => x.id === params.id)
      if (!doc) throw Object.assign(new Error('文档不存在'), { code: 404 })
      doc.status = 'ready'
      doc.model = EMBED_MODEL
      doc.dimension = 1024
      doc.error = ''
      doc.stage = ''
      return doc
    },
  },
  {
    // 失败文档重新排队：原文已在库里，无需重传；mock 里直接落到 processing 并给个 stage。
    method: 'POST',
    url: '/plugins/kb/documents/:id/retry',
    handler: ({ params }) => {
      const doc = docs.find((x) => x.id === params.id)
      if (!doc) throw Object.assign(new Error('文档不存在'), { code: 404 })
      doc.status = 'processing'
      doc.stage = '切分中'
      doc.error = ''
      return doc
    },
  },
  {
    method: 'POST',
    url: '/plugins/kb/documents',
    handler: () => {
      // multipart 在 mock 适配器里拿不到文件内容：给一个确定的伪入库结果。
      // 与真实接口一致——投递即返回 pending，列表轮询到落定（mock 不推进，停在中间态）。
      const doc: KbDocument = {
        id: genId(), title: `新入库文档 ${new Date().toLocaleTimeString()}`,
        sourceName: 'uploaded.txt', mime: 'text/plain', size: 8_000,
        status: 'pending', stage: '排队中', chunkCount: 0, model: EMBED_MODEL, dimension: 1024, createdAt: NOW,
      }
      docs.unshift(doc)
      return doc
    },
  },
  {
    method: 'POST',
    url: '/plugins/kb/ask',
    handler: ({ body }) => {
      const q = String(body?.query ?? '')
      // 命中场景：正常问题；handoff 场景：问题里带「人工/退款投诉」等未收录话题，
      // 返回候选（分数低于阈值 0.35）供召回调试看「差多少」。
      if (/人工|投诉|退款/.test(q)) {
        const candidates: KbHit[] = [
          { documentId: docs[0]?.id ?? '', title: '售后与退换政策', excerpt: '自签收之日起 7 天内可申请无理由退货……（score 0.21，低于阈值 0.35）', score: 0.21 },
          { documentId: docs[1]?.id ?? '', title: '产品常见问题 FAQ', excerpt: '关于发货时效：现货 48 小时内出库……', score: 0.18 },
        ]
        const out: KbAnswer = { answer: '', hits: [], handoff: true, candidates }
        return out
      }
      const hits: KbHit[] = [
        { documentId: docs[0]?.id ?? '', title: '产品常见问题 FAQ', excerpt: '现货商品付款后 48 小时内出库，偏远地区 72 小时……', score: 0.62 },
        // 关键词兜底命中：向量分低于阈值，但字面几乎全中（型号 / 订单号类问法）
        { documentId: docs[1]?.id ?? '', title: '售后与退换政策', excerpt: '自签收之日起 7 天内可申请无理由退货，运费自理……', score: 0.29, match: 'keyword' },
      ]
      const out: KbAnswer = {
        answer: '根据知识库：现货商品付款后 48 小时内出库，偏远地区 72 小时内出库。如需了解具体订单的物流状态，请提供订单号由人工为您查询。',
        hits, handoff: false,
      }
      return out
    },
  },
  {
    // 未命中问题池：分页 / 状态筛选 / 排序都在「服务端」（这里）做，与真实接口一致。
    method: 'GET',
    url: '/plugins/kb/misses',
    handler: ({ query }) => {
      const status = String(query.status ?? '')
      const matched = misses.filter((m) => !status || m.status === status)
      const sort = String(query.sort ?? 'askCount')
      const desc = query.desc !== 'false' && query.desc !== false
      const val = (m: KbMiss): string | number =>
        sort === 'lastAt' ? Date.parse(m.lastAt) : sort === 'question' ? m.question : m.askCount
      matched.sort((a, b) => {
        const av = val(a)
        const bv = val(b)
        const cmp = typeof av === 'string' ? av.localeCompare(String(bv)) : Number(av) - Number(bv)
        return desc ? -cmp : cmp
      })
      const page = Math.max(1, Number(query.page) || 1)
      const pageSize = Math.max(1, Math.min(100, Number(query.pageSize) || 20))
      return {
        list: matched.slice((page - 1) * pageSize, page * pageSize),
        total: matched.length,
        page,
        pageSize,
      }
    },
  },
  {
    method: 'POST',
    url: '/plugins/kb/misses/ignore',
    handler: ({ body }) => {
      const ids: string[] = Array.isArray(body?.ids) ? body.ids : []
      let count = 0
      for (const m of misses) {
        if (ids.includes(m.id) && m.status === 'pending') {
          m.status = 'ignored'
          count++
        }
      }
      return { count }
    },
  },
  {
    method: 'POST',
    url: '/plugins/kb/misses/:id/resolve',
    handler: ({ params, body }) => {
      const m = misses.find((x) => x.id === params.id)
      if (!m) throw Object.assign(new Error('问题不存在'), { code: 404 })
      const doc: KbDocument = {
        id: genId(), title: m.question.slice(0, 200), sourceName: '未命中问题转QA.txt',
        mime: 'text/plain', size: String(body?.answer ?? '').length, status: 'ready',
        chunkCount: 1, model: EMBED_MODEL, dimension: 1024, createdAt: NOW,
      }
      docs.unshift(doc)
      m.status = 'resolved'
      m.documentId = doc.id
      return doc
    },
  },
  {
    method: 'POST',
    url: '/plugins/kb/misses/:id/reopen',
    handler: ({ params }) => {
      const m = misses.find((x) => x.id === params.id)
      if (!m) throw Object.assign(new Error('问题不存在'), { code: 404 })
      m.status = 'pending'
      m.documentId = ''
      return m
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/kb/misses/:id',
    handler: ({ params }) => {
      const i = misses.findIndex((x) => x.id === params.id)
      if (i >= 0) misses.splice(i, 1)
      return { ok: true }
    },
  },
])
