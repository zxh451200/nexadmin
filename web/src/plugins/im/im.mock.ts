import { defineMock } from '@/framework/request/mock'
import type { ImConversation, ImMessage } from './api'

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString()

let seq = 100
const convs: ImConversation[] = [
  {
    id: '1',
    convNo: 'IM7420000000000000001',
    bizType: 'trade_group',
    bizNo: 'G-20250601001',
    visitorToken: 'anon-1',
    visitorName: '微信用户 8127',
    subject: '订单 G-20250601001 咨询',
    status: 'open',
    lastPreview: '请问这个订单什么时候发货？',
    lastMsgAt: ago(3),
    createdAt: ago(30),
    unread: 2,
  },
  {
    id: '2',
    convNo: 'IM7420000000000000002',
    bizType: '',
    bizNo: '',
    visitorToken: 'anon-2',
    visitorName: '游客 3391',
    subject: '',
    status: 'open',
    lastPreview: '支持货到付款吗？',
    lastMsgAt: ago(48),
    createdAt: ago(50),
    unread: 0,
  },
  {
    id: '3',
    convNo: 'IM7420000000000000003',
    bizType: 'trade_group',
    bizNo: 'G-20250531077',
    visitorToken: 'anon-3',
    visitorName: '会员 Alice',
    subject: '退款进度',
    status: 'closed',
    lastPreview: '好的，谢谢',
    lastMsgAt: ago(60 * 26),
    createdAt: ago(60 * 27),
    unread: 0,
  },
]

const messages: Record<string, ImMessage[]> = {
  IM7420000000000000001: [
    { id: '11', seq: 1, senderType: 'visitor', senderId: 'anon-1', senderName: '微信用户 8127', content: '你好，我想问下订单 G-20250601001', createdAt: ago(10) },
    { id: '12', seq: 2, senderType: 'visitor', senderId: 'anon-1', senderName: '微信用户 8127', content: '请问这个订单什么时候发货？', createdAt: ago(3) },
  ],
  IM7420000000000000002: [
    { id: '21', seq: 1, senderType: 'visitor', senderId: 'anon-2', senderName: '游客 3391', content: '支持货到付款吗？', createdAt: ago(48) },
    { id: '22', seq: 2, senderType: 'agent', senderId: '7', senderName: 'admin', content: '您好，目前仅支持在线支付。', createdAt: ago(47) },
  ],
  IM7420000000000000003: [
    { id: '31', seq: 1, senderType: 'visitor', senderId: 'anon-3', senderName: '会员 Alice', content: '退款已经三天了还没到账', createdAt: ago(60 * 26.5) },
    { id: '32', seq: 3, senderType: 'agent', senderId: '7', senderName: 'admin', content: '已加急处理，预计 1-2 个工作日到账。', createdAt: ago(60 * 26) },
  ],
}

const find = (no: string) => convs.find((c) => c.convNo === no)

defineMock([
  {
    method: 'GET',
    url: '/plugins/im/conversations',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let rows = convs.filter(
        (c) =>
          !kw ||
          c.convNo.toLowerCase().includes(kw) ||
          c.visitorName.toLowerCase().includes(kw) ||
          c.lastPreview.toLowerCase().includes(kw),
      )
      if (query.status) rows = rows.filter((c) => c.status === query.status)
      if (query.bizType) rows = rows.filter((c) => c.bizType === query.bizType)
      return { list: rows, total: rows.length, page: 1, pageSize: rows.length }
    },
  },
  {
    method: 'GET',
    url: '/plugins/im/conversations/:no/messages',
    handler: ({ params }) => {
      const conv = find(params.no)
      if (!conv) throw new Error('not found')
      if (conv.status === 'open') conv.unread = 0
      return { conversation: conv, messages: messages[params.no] ?? [] }
    },
  },
  {
    method: 'POST',
    url: '/plugins/im/conversations/:no/messages',
    handler: ({ params, body }) => {
      const conv = find(params.no)
      if (!conv) throw new Error('not found')
      if (conv.status !== 'open') throw new Error('conversation closed')
      seq += 1
      const m: ImMessage = {
        id: String(seq),
        seq,
        senderType: 'agent',
        senderId: '7',
        senderName: 'admin',
        content: String(body.content ?? ''),
        createdAt: new Date().toISOString(),
      }
      messages[params.no] = [...(messages[params.no] ?? []), m]
      conv.lastPreview = m.content
      conv.lastMsgAt = m.createdAt
      return m
    },
  },
  {
    method: 'POST',
    url: '/plugins/im/conversations/:no/close',
    handler: ({ params }) => {
      const conv = find(params.no)
      if (!conv) throw new Error('not found')
      conv.status = 'closed'
      return conv
    },
  },
])
