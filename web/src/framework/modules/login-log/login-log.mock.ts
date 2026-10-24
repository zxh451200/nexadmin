import { defineMock } from '@/framework/request/mock'
import { localDate } from '../oper-log/format'
import type { FailReason, LoginLog, LoginType } from './api'

const MIN = 60000
const mkId = (n: number) => (1840000000000000000n + BigInt(n)).toString()

type Row = [string, LoginType, string, string, string, string, FailReason | '', number]

const rows: Row[] = [
  ['admin', 'password', '10.20.1.15', '广东省深圳市', 'Chrome 126', 'Windows 11', '', 3],
  ['zhang.wei', 'wecom', '10.20.3.42', '北京市', 'WeCom 4.1', 'macOS 14', '', 12],
  ['li.na', 'password', '192.168.8.21', '上海市', 'Safari 17', 'iOS 17', '', 25],
  ['unknown_user', 'password', '203.0.113.45', '美国加利福尼亚州', 'Firefox 127', 'Linux', 'credentials', 41],
  ['wang.fang', 'phone', '172.16.5.9', '浙江省杭州市', 'Chrome 126', 'Android 14', '', 58],
  ['sun.yue', 'feishu', '10.20.7.8', '四川省成都市', 'Feishu 7.2', 'Windows 10', '', 80],
  ['admin', 'password', '198.51.100.23', '俄罗斯莫斯科', 'Chrome 120', 'Windows 10', 'captcha', 105],
  ['admin', 'password', '198.51.100.23', '俄罗斯莫斯科', 'Chrome 120', 'Windows 10', 'locked', 106],
  ['zhao.lei', 'dingtalk', '192.168.8.37', '江苏省南京市', 'DingTalk 7.6', 'macOS 14', '', 150],
  ['john.smith', 'google', '172.20.4.66', '美国纽约州', 'Chrome 126', 'macOS 14', '', 210],
  ['dev-bot', 'github', '172.20.4.71', '德国法兰克福', 'Edge 126', 'Ubuntu 22.04', '', 260],
  ['li.na', 'password', '192.168.8.21', '上海市', 'Safari 17', 'iOS 17', 'credentials', 330],
  ['liu.yang', 'password', '10.20.9.3', '广东省广州市', 'Chrome 125', 'Windows 11', 'disabled', 400],
  ['chen.hao', 'oidc', '10.20.2.19', '湖北省武汉市', 'Chrome 126', 'Windows 11', '', 520],
  ['zhang.wei', 'phone', '10.20.3.42', '北京市', 'Safari 17', 'iOS 17', '', 700],
  ['guest01', 'phone', '223.5.5.5', '浙江省杭州市', 'Chrome 126', 'Android 13', 'blocked', 880],
  ['admin', 'password', '10.20.1.15', '广东省深圳市', 'Chrome 126', 'Windows 11', '', 1300],
  ['wang.fang', 'password', '172.16.5.9', '浙江省杭州市', 'Firefox 127', 'Windows 11', 'credentials', 1500],
  ['wang.fang', 'password', '172.16.5.9', '浙江省杭州市', 'Firefox 127', 'Windows 11', '', 1502],
  ['sun.yue', 'password', '10.20.7.8', '四川省成都市', 'Chrome 126', 'Windows 10', '', 2000],
  ['zhao.lei', 'wecom', '192.168.8.37', '江苏省南京市', 'WeCom 4.1', 'Windows 11', '', 2700],
  ['test_user', 'password', '45.33.32.156', '美国弗吉尼亚州', 'curl 8.4', 'Linux', 'other', 3500],
  ['li.na', 'google', '192.168.8.21', '上海市', 'Chrome 126', 'macOS 14', '', 5200],
  ['chen.hao', 'password', '10.20.2.19', '湖北省武汉市', 'Edge 126', 'Windows 11', '', 8000],
  ['admin', 'password', '10.20.1.15', '广东省深圳市', 'Chrome 125', 'Windows 11', '', 14000],
]

let items: LoginLog[] = rows.map(([username, loginType, ip, location, browser, os, reason, ago], i) => ({
  id: mkId(i + 1),
  username,
  loginType,
  ip,
  location,
  browser,
  os,
  status: reason ? 'failed' : 'success',
  failReason: reason || undefined,
  createdAt: new Date(Date.now() - ago * MIN).toISOString(),
}))

function filtered(query: Record<string, unknown>): LoginLog[] {
  const kw = String(query.keyword ?? '').trim().toLowerCase()
  let list = items.slice()
  if (kw) list = list.filter((r) => r.username.toLowerCase().includes(kw) || r.ip.includes(kw))
  if (query.status) list = list.filter((r) => r.status === query.status)
  if (query.loginType) list = list.filter((r) => r.loginType === query.loginType)
  if (query.startDate) list = list.filter((r) => localDate(r.createdAt) >= String(query.startDate))
  if (query.endDate) list = list.filter((r) => localDate(r.createdAt) <= String(query.endDate))
  list.sort((a, b) => b.createdAt.localeCompare(a.createdAt))
  return list
}

const csvCell = (v: string) => (/[",\r\n]/.test(v) ? `"${v.replace(/"/g, '""')}"` : v)

defineMock([
  {
    method: 'GET',
    url: '/login-logs',
    handler: ({ query }) => {
      const page = Math.max(1, Number(query.page) || 1)
      const pageSize = Math.max(1, Number(query.pageSize) || 10)
      const list = filtered(query)
      return { list: list.slice((page - 1) * pageSize, page * pageSize), total: list.length, page, pageSize }
    },
  },
  {
    // 真实后端返回 xlsx 文件；mock 返回同样列的 CSV 文本
    method: 'GET',
    url: '/login-logs/export',
    handler: ({ query }) => {
      const head = ['账号', '登录方式', 'IP', '归属地', '浏览器', '操作系统', '状态', '失败原因', '时间']
      const lines = filtered(query).map((r) =>
        [r.username, r.loginType, r.ip, r.location ?? '', r.browser ?? '', r.os ?? '', r.status === 'success' ? '成功' : '失败', r.failReason ?? '', r.createdAt]
          .map(csvCell)
          .join(','),
      )
      return `\uFEFF${[head.join(','), ...lines].join('\r\n')}\r\n`
    },
  },
  {
    method: 'DELETE',
    url: '/login-logs',
    handler: ({ query }) => {
      const ids = String(query.ids ?? '').split(',').filter(Boolean)
      items = items.filter((r) => !ids.includes(r.id))
      return null
    },
  },
])
