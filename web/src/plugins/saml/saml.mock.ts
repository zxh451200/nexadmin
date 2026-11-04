import { defineMock } from '@/framework/request/mock'

/**
 * 登录页的 SAML 入口在真后端是匿名路由（plugins/saml/plugin.go 用 d.Public.Group("/auth/saml")），
 * mock 侧必须同时满足两件事：路径在 PUBLIC_PREFIXES 白名单里，且有对应规则。
 * 只补白名单不补规则会退成「Mock not found」的 404，面板靠自身 .catch 隐藏，
 * 于是这块 UI 在开发态永远跑不到——这里给一个源，让登录页能真渲染出按钮。
 */
defineMock([
  {
    method: 'GET',
    url: '/auth/saml/sources',
    handler: () => [
      { id: 'idp-demo', name: '企业单点登录（示例）' },
    ],
  },
])
