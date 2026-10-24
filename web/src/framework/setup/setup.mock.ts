import { defineMock, mockError } from '@/framework/request/mock'
import { isMockInitialized, setMockInitialized } from '@/framework/request/system.mock'

function guard() {
  // 完成后 /setup/* 一律 404
  if (isMockInitialized()) mockError(404, 'Not Found')
}

defineMock([
  {
    method: 'GET',
    url: '/setup/env',
    handler: () => {
      guard()
      return {
        checks: [
          { key: 'dataWritable', ok: true, detail: './data' },
          { key: 'configWritable', ok: true, detail: './config' },
          { key: 'port', ok: true, detail: '8080' },
          { key: 'engines', ok: true, detail: 'MySQL / MariaDB / PostgreSQL' },
        ],
      }
    },
  },
  {
    method: 'POST',
    url: '/setup/test-db',
    handler: async ({ body }) => {
      guard()
      if (!body?.host || !body?.port || !body?.username) return { ok: false, message: 'Connection refused' }
      return { ok: true, message: 'ok', version: body.type === 'postgresql' ? 'PostgreSQL 16.2' : 'MySQL 8.0.36' }
    },
  },
  {
    method: 'POST',
    url: '/setup/test-cache',
    handler: async ({ body }) => {
      guard()
      if (body?.type === 'valkey' && !body?.addr) return { ok: false, message: 'Connection refused' }
      return { ok: true, message: 'ok' }
    },
  },
  {
    method: 'POST',
    url: '/setup/execute',
    handler: async ({ body }) => {
      guard()
      // 记住向导里创建的管理员，登录 mock 用它校验
      if (body?.admin?.username) {
        try {
          localStorage.setItem('mock-admin', JSON.stringify({ username: body.admin.username, password: body.admin.password ?? '' }))
        } catch {
          // 存储不可用时沿用默认账号
        }
      }
      setMockInitialized(true)
      return {
        steps: [
          { key: 'writeConfig', status: 'done' },
          { key: 'migrate', status: 'done' },
          { key: 'seed', status: 'done' },
          { key: 'createAdmin', status: 'done' },
          { key: 'writeMark', status: 'done' },
        ],
      }
    },
  },
])
