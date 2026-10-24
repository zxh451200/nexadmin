import { defineMock, type MockRule } from '@/framework/request/mock'

let seq = 2
const tokens = [{ id: '1', name: '示例 IdP', revoked: false, createdAt: '2026-01-01T00:00:00Z' }]
const rules: MockRule[] = [
  { method: 'GET', url: '/plugins/scim/tokens', handler: () => ({ list: tokens, total: tokens.length }) },
  { method: 'POST', url: '/plugins/scim/tokens', handler: ({ body }) => {
    const item = { id: String(seq++), name: body.name, revoked: false, createdAt: new Date().toISOString() }
    tokens.unshift(item)
    return { token: `nxscim_mock_${item.id}`, item }
  } },
  { method: 'DELETE', url: '/plugins/scim/tokens/:id', handler: ({ params }) => {
    const item = tokens.find((x) => x.id === params.id)
    if (item) item.revoked = true
    return null
  } },
]
defineMock(rules)
