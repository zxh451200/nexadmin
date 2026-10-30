import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'
import { registerLoginContinuation, registerLoginPanel } from '@/framework/auth/useAuth'
import SamlLoginPanel from './SamlLoginPanel'
import { resumeSamlLogin } from './login'

// 登录页入口（企业 SSO 按钮）与 ACS 回跳的一次性交换码兑换。
registerLoginPanel(SamlLoginPanel)
registerLoginContinuation('saml', resumeSamlLogin)

export const routes: ModuleRoute[] = [
  {
    path: 'system/saml',
    component: lazy(() => import('./SamlPage')),
    perm: 'saml:list',
    menu: { title: 'plugin_saml.menu', icon: 'KeyRound', order: 115, group: 'system' },
  },
]
