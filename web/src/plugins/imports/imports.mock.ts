import { defineMock, mockError } from '@/framework/request/mock'
import type { ImportJob, ImportSourceMeta } from './api'

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString()

const SOURCES: ImportSourceMeta[] = [
  {
    name: 'crm_customers',
    title: '客户导入',
    // 与 plugins/crm/import.go 的 ImportSources 同列同序：mock 的模板与列提示也是契约的一部分。
    columns: [
      { key: 'name', label: '客户名称', required: true, example: '张三' },
      { key: 'type', label: '客户类型（个人/企业）', required: false, example: '个人' },
      { key: 'phone', label: '手机号', required: false, example: '13800138000' },
      { key: 'email', label: '邮箱', required: false, example: 'zhangsan@example.com' },
      { key: 'level', label: '客户等级', required: false, example: 'A' },
      { key: 'owner', label: '负责人', required: false, example: '王五' },
      { key: 'country', label: '国家/地区', required: false, example: 'CN' },
      { key: 'nationality', label: '国籍', required: false, example: 'CN' },
      { key: 'id_type', label: '证件类型', required: false, example: '身份证' },
      { key: 'id_no', label: '证件号', required: false, example: '33010619900101001X' },
      { key: 'tax_no', label: '统一社会信用代码', required: false, example: '91330100MA2AB1CD3X' },
      { key: 'industry', label: '所属行业', required: false, example: '住宿业' },
      { key: 'address', label: '企业注册地址', required: false, example: '' },
      { key: 'remark', label: '备注', required: false, example: '' },
    ],
  },
  {
    name: 'dict_data',
    title: '字典数据导入',
    columns: [
      { key: 'type_code', label: '字典类型编码', required: true, example: 'sys_common_status' },
      { key: 'label', label: '标签', required: true, example: '成功' },
      { key: 'value', label: '值', required: true, example: '1' },
      { key: 'tag', label: '标签样式', required: false, example: 'success' },
      { key: 'sort', label: '排序', required: false, example: '10' },
      { key: 'status', label: '状态(1启用 0停用)', required: false, example: '1' },
      { key: 'remark', label: '备注', required: false, example: '' },
    ],
  },
  {
    // 与 plugins/website/product_io.go 的 ImportSources 同列同序：mock 的模板与列提示也是契约的一部分。
    name: 'website_products',
    title: '产品导入',
    columns: [
      { key: 'title', label: '商品名称', required: true, example: '18V 无刷冲击扳手' },
      { key: 'slug', label: 'Slug', required: false, example: '18v-brushless-impact-wrench' },
      { key: 'categories', label: '产品类目', required: false, example: '电动工具|冲击扳手' },
      { key: 'status', label: '状态', required: false, example: 'draft' },
      { key: 'keywords', label: '关键词', required: false, example: 'impact wrench 18V brushless' },
      { key: 'summary', label: '摘要', required: false, example: '18V 无刷冲击扳手，最大扭矩 300N·m' },
      { key: 'content', label: '详情', required: false, example: '<p>详情富文本 HTML</p>' },
      { key: 'coverUrl', label: '主图', required: false, example: 'https://example.com/cover.jpg' },
      { key: 'images', label: '图集', required: false, example: 'https://example.com/1.jpg|https://example.com/2.jpg' },
      { key: 'videoUrl', label: '视频地址', required: false, example: 'https://example.com/demo.mp4' },
      { key: 'priceMin', label: '价格（起）', required: false, example: '49.90' },
      { key: 'priceMax', label: '价格（止）', required: false, example: '59.90' },
      { key: 'currency', label: '币种', required: false, example: 'USD' },
      { key: 'priceUnit', label: '计量单位', required: false, example: 'piece' },
      { key: 'moq', label: '最小起订量', required: false, example: '100' },
      { key: 'leadTimeDays', label: '发货天数', required: false, example: '15' },
      { key: 'port', label: '装运港口', required: false, example: 'Shanghai' },
      { key: 'paymentTerms', label: '付款方式', required: false, example: 'T/T, L/C' },
      { key: 'specs', label: '动态属性', required: false, example: '[{"label":"Voltage","value":"18V"}] 或 电压:18V|品牌:NexTool' },
      { key: 'seoTitle', label: 'SEO 标题', required: false, example: '' },
      { key: 'seoDesc', label: 'SEO 描述', required: false, example: '' },
      { key: 'seoKeywords', label: 'SEO 关键词', required: false, example: '' },
      { key: 'author', label: '作者', required: false, example: '外贸部' },
      { key: 'i18n', label: '多语言', required: false, example: '{"en":{"title":"18V Brushless Impact Wrench"}}' },
    ],
  },
]

let jobs: ImportJob[] = [
  {
    id: '7450000000000000001',
    source: 'crm_customers',
    fileName: 'customers-2025Q3.csv',
    totalRows: 120,
    okRows: 116,
    failRows: 4,
    status: 'done',
    durationMs: 860,
    errors: [
      { row: 23, key: 'name', message: '必填列为空' },
      { row: 57, key: 'email', message: '邮箱格式非法' },
    ],
    createdBy: 'admin',
    createdAt: ago(60 * 5),
  },
  {
    id: '7450000000000000002',
    source: 'crm_customers',
    fileName: 'customers-hotels.csv',
    totalRows: 30,
    okRows: 30,
    failRows: 0,
    status: 'done',
    durationMs: 210,
    createdBy: 'zhangwei',
    createdAt: ago(60 * 24),
  },
]

const paginate = <T,>(rows: T[], query: Record<string, unknown>) => {
  const page = Math.max(1, Number(query.page) || 1)
  const pageSize = Math.max(1, Number(query.pageSize) || 10)
  return { list: rows.slice((page - 1) * pageSize, page * pageSize), total: rows.length, page, pageSize }
}

defineMock([
  { method: 'GET', url: '/plugins/imports/sources', handler: () => SOURCES },
  {
    method: 'GET',
    url: '/plugins/imports',
    handler: ({ query }) => {
      let rows = jobs
      if (query.source) rows = rows.filter((j) => j.source === query.source)
      if (query.status) rows = rows.filter((j) => j.status === query.status)
      return paginate(rows, query)
    },
  },
  // mock 环境只给 CSV 文本桩；真实 XLSX 由后端生成。表头按请求的来源拼，不能所有来源都发商品模板。
  // 排在 /:id 之前，避免被通配路由吃掉。
  {
    method: 'GET',
    url: '/plugins/imports/template',
    handler: ({ query }) => {
      const src = SOURCES.find((s) => s.name === String(query.source ?? ''))
      if (!src) mockError(404, 'unknown source')
      const head = src.columns.map((c) => `${c.label}${c.required ? '(必填)' : ''}`).join(',')
      const sample = src.columns.map((c) => c.example).join(',')
      return `\uFEFF${head}\r\n${sample}\r\n`
    },
  },
  {
    method: 'GET',
    url: '/plugins/imports/:id',
    handler: ({ params }) => jobs.find((j) => j.id === params.id) ?? { error: 'not found' },
  },
  {
    method: 'POST',
    url: '/plugins/imports',
    handler: ({ body }) => {
      // 上传走 multipart，body 是 FormData；来源与文件名按真实请求回填，台账才和页面对得上。
      const form = body instanceof FormData ? body : null
      const source = String(form?.get('source') ?? '') || 'crm_customers'
      const file = form?.get('file')
      const job: ImportJob = {
        id: String(7450000000000010000n + BigInt(jobs.length + 1)),
        source,
        fileName: file instanceof File ? file.name : 'upload.csv',
        totalRows: 3,
        okRows: 2,
        failRows: 1,
        status: 'done',
        durationMs: 95,
        errors: [{ row: 2, key: 'name', message: '必填列为空' }],
        createdBy: 'admin',
        createdAt: new Date().toISOString(),
      }
      jobs = [job, ...jobs]
      return job
    },
  },
  {
    method: 'POST',
    url: '/plugins/imports/:id/cancel',
    handler: ({ params }) => {
      const j = jobs.find((x) => x.id === params.id) ?? mockError(404, 'Not found')
      if (j.status !== 'pending' && j.status !== 'running') mockError(409, 'The job is not running, so there is nothing to cancel.')
      Object.assign(j, { status: 'cancelled' })
      return j
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/imports/:id',
    handler: ({ params }) => {
      jobs = jobs.filter((j) => j.id !== params.id)
      return { ok: true }
    },
  },
  {
    // 批量删除：与后端 DELETE /plugins/imports?ids= 同口径。
    // ids 缺失/非法（含 0）一律 400，不退化成「删全部」；批次里有排队中/执行中的任务整批 409（一条都不删）；
    // 越权/不存在的 id 不计入实删数；0 命中 404。
    method: 'DELETE',
    url: '/plugins/imports',
    handler: ({ query }) => {
      const parts = String(query.ids ?? '').split(',').map((s) => s.trim()).filter(Boolean)
      if (parts.length === 0 || parts.some((s) => !/^\d+$/.test(s) || /^0+$/.test(s))) mockError(400, 'Bad request')
      const picked = jobs.filter((j) => parts.includes(j.id))
      if (picked.length === 0) mockError(404, 'Not found')
      if (picked.some((j) => j.status === 'pending' || j.status === 'running')) mockError(409, 'The job is running; it can be deleted once it finishes.')
      const ids = new Set(parts)
      const before = jobs.length
      jobs = jobs.filter((j) => !ids.has(j.id))
      return { deleted: before - jobs.length }
    },
  },
  {
    method: 'GET',
    url: '/plugins/imports/:id/errors.csv',
    handler: ({ params }) => {
      const j = jobs.find((x) => x.id === params.id)
      return `row,key,message\n${(j?.errors ?? []).map((e) => `${e.row},${e.key},${e.message}`).join('\n')}`
    },
  },
  {
    method: 'GET',
    url: '/plugins/imports/:id/retry.csv',
    handler: ({ params }) => {
      const j = jobs.find((x) => x.id === params.id)
      // 与后端同口径：台账没留存原始行内容就报错，不发只有表头的空文件
      const rows = (j?.errors ?? []).filter((e) => e.data && Object.keys(e.data).length > 0)
      if (rows.length === 0) return { code: 400, message: 'imports.retry_empty' }
      const header = Object.keys(rows[0].data as Record<string, string>)
      const body = rows.map((e) => header.map((h) => (e.data as Record<string, string>)[h] ?? '').join(','))
      return [header.join(','), ...body].join('\n')
    },
  },
])
