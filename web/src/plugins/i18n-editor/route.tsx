import { lazy } from 'react'
import type { ModuleRoute } from '@/framework/router/types'
export const routes: ModuleRoute[] = [{ path: 'system/i18n-editor', component: lazy(() => import('./I18nEditorPage')), perm: 'i18n-editor:list', menu: { title: 'plugin_i18n_editor.menu', icon: 'Languages', order: 130, group: 'system' } }]
