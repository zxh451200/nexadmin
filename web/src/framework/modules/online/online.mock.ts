import { defineMock, mockError } from '@/framework/request/mock'
import type { OnlineUser } from './api'

const MIN = 60000
const mkId = (n: number) => (1850000000000000000n + BigInt(n)).toString()

type Row = [string, string, string, OnlineUser['device'], string, string, string, number, number]

const rows: Row[] = [
  ['admin', '系统管理员', '技术部', 'web', 'Chrome 126', 'Windows 11', '10.20.1.15', 180, 0],
  ['zhang.wei', '张伟', '产品部', 'web', 'WeCom 4.1', 'macOS 14', '10.20.3.42', 95, 2],
  ['li.na', '李娜', '运营部', 'mobile', 'Safari 17', 'iOS 17', '192.168.8.21', 240, 6],
  ['wang.fang', '王芳', '财务部', 'web', 'Firefox 127', 'Windows 11', '172.16.5.9', 60, 11],
  ['sun.yue', '孙悦', '技术部', 'tablet', 'Safari 17', 'iPadOS 17', '10.20.7.8', 310, 18],
  ['zhao.lei', '赵磊', '市场部', 'web', 'DingTalk 7.6', 'macOS 14', '192.168.8.37', 45, 25],
  ['john.smith', 'John Smith', 'Overseas', 'web', 'Chrome 126', 'macOS 14', '172.20.4.66', 130, 33],
  ['chen.hao', '陈浩', '技术部', 'mobile', 'Chrome 126', 'Android 14', '10.20.2.19', 20, 47],
]

let items: OnlineUser[] = rows.map(([username, nickname, deptName, device, browser, os, ip, login, active], i) => ({
  id: mkId(i + 1),
  username,
  nickname,
  deptName,
  device,
  browser,
  os,
  ip,
  loginAt: new Date(Date.now() - login * MIN).toISOString(),
  lastActiveAt: new Date(Date.now() - active * MIN).toISOString(),
}))

defineMock([
  {
    method: 'GET',
    url: '/online-users',
    handler: ({ query }) => {
      const page = Math.max(1, Number(query.page) || 1)
      const pageSize = Math.max(1, Number(query.pageSize) || 10)
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let list = items.slice()
      if (kw) list = list.filter((r) => [r.username, r.nickname, r.ip].some((s) => s.toLowerCase().includes(kw)))
      list.sort((a, b) => b.lastActiveAt.localeCompare(a.lastActiveAt))
      return { list: list.slice((page - 1) * pageSize, page * pageSize), total: list.length, page, pageSize }
    },
  },
  {
    method: 'DELETE',
    url: '/online-users/:id',
    handler: ({ params }) => {
      if (!items.some((r) => r.id === params.id)) mockError(404, 'Not found')
      items = items.filter((r) => r.id !== params.id)
      return null
    },
  },
])
