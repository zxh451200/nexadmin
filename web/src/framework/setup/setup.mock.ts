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
    // 与后端 core/setup/probe.go 同形：{ok, code, scope}；无效配置回 invalid，其余一律成功。
    method: 'POST',
    url: '/setup/test-s3',
    handler: ({ body }) => {
      guard()
      const b = body ?? {}
      if (!b.s3Endpoint || !b.s3Bucket || !b.s3AccessKey || !b.s3SecretKey) return { ok: false, code: 'invalid', scope: 's3' }
      return { ok: true, code: 'ok', scope: 's3' }
    },
  },
  {
    // 对齐后端 Config.Ready()：模型名 + http(s) 地址必填；API key 缺失时仅 localhost 放行。
    method: 'POST',
    url: '/setup/test-ai',
    handler: ({ body }) => {
      guard()
      const b = body ?? {}
      let host = ''
      try {
        const u = new URL(String(b.aiBaseUrl || ''))
        host = (u.protocol === 'http:' || u.protocol === 'https:') ? u.hostname : ''
      } catch {
        host = ''
      }
      if (!host || !b.aiModel || (!b.aiApiKey && !['localhost', '127.0.0.1', '::1'].includes(host))) {
        return { ok: false, code: 'invalid', scope: 'ai' }
      }
      return { ok: true, code: 'ok', scope: 'ai' }
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
