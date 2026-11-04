import { defineMock, mockError } from '@/framework/request/mock'
import { mockIssueSession } from '@/framework/auth/auth.mock'

/**
 * 登录页的 SAML 入口在真后端是匿名路由（plugins/saml/plugin.go 用 d.Public.Group("/auth/saml")
 * 注册 sources / exchange / metadata / login / acs / slo）。mock 侧要同时满足两件事：
 * 路径在 PUBLIC_PREFIXES 白名单里，且有对应规则。只补白名单不补规则会退成
 * 「Mock not found」404，面板与交换流程靠各自 .catch 静默失败——这块 UI 在开发态永远跑不到。
 *
 * 交换码模拟的是「IdP 已认定身份、ACS 回调带码回来」这一步：映射到 mock 的 demo 账号，
 * 令牌与用户表都由 auth.mock 统一签发（不在此抄第二份结构）。
 */
defineMock([
  {
    method: 'GET',
    url: '/auth/saml/sources',
    handler: () => [{ id: 'idp-demo', name: '企业单点登录（示例）' }],
  },
  {
    method: 'POST',
    url: '/auth/saml/exchange',
    handler: ({ body }) => {
      const code = String(body?.code ?? '')
      // 一次性码：真后端用它换一次登录态，空码与重复消费都该拒绝而不是发令牌。
      if (code.length < 6) return mockError(400, 'invalid exchange code')
      const session = mockIssueSession('demo')
      if (!session) return mockError(401, 'session unavailable')
      return { ...session, samlSourceId: 'idp-demo' }
    },
  },
])
