import { Suspense, lazy, useMemo } from 'react'
import { Navigate, Route, Routes } from 'react-router-dom'
import { RequireAuth } from '@/framework/auth/RequireAuth'
import { checkPerm } from '@/framework/auth/usePermission'
import { useAuth } from '@/framework/auth/useAuth'
import { AppLayout } from '@/framework/layout/AppLayout'
import ForbiddenPage from '@/framework/pages/ForbiddenPage'
import LoginPage from '@/framework/pages/LoginPage'
import NotFoundPage from '@/framework/pages/NotFoundPage'
import PlatformOnlyPage from '@/framework/pages/PlatformOnlyPage'
import PluginInactivePage from '@/framework/pages/PluginInactivePage'
import { PageLoading } from '@/framework/components/PageLoading'
import { useActivePlugins } from '@/framework/modules/plugin/active'
import { allRoutes, buildMenuTree } from './discover'
import type { ModuleRoute } from './types'

// SSO 授权确认页：无布局的全屏页；未登录由 RequireAuth 跳登录页并带回跳地址
const SsoAuthorizePage = lazy(() => import('@/framework/modules/sso/AuthorizePage'))
// 第三方登录 / 绑定的授权回调落地页：不要求已登录（登录场景尚无令牌）
const OAuthCallbackPage = lazy(() => import('@/framework/pages/OAuthCallbackPage'))
const McpConfirmationPage = lazy(() => import('@/framework/pages/McpConfirmationPage'))

function Guarded({ route }: { route: ModuleRoute }) {
  const perms = useAuth((s) => s.perms)
  const platform = useAuth((s) => s.user?.platform)
  const active = useActivePlugins()
  if (!checkPerm(perms, route.perm)) return <ForbiddenPage />
  // 平台级插件（表里没有 tenant_id，读也会把别的租户的记录带走）：后端整组挂了
  // RequirePlatformFeature，这里只负责不让人撞一串 403。判据只认服务端明确说了 false——
  // 旧后端没这个字段时照常渲染，由后端自己挡（UI 判据缺件不该把平台管理员也关在门外）。
  if (route.platform && platform === false) return <PlatformOnlyPage />
  // 插件页：先确认插件已启用，再渲染页面本身（否则未启用插件的页面会发出一串必然 404 的请求）。
  // active.data 为 null 表示问不到（mock / 旧后端 / 网络错误），按未知处理，不拦。
  if (route.plugin) {
    if (active.isPending) return <PageLoading />
    if (active.data && !active.data.includes(route.plugin)) return <PluginInactivePage />
  }
  const Component = route.component
  return (
    <Suspense fallback={<PageLoading />}>
      <Component />
    </Suspense>
  )
}

function HomeRedirect() {
  const perms = useAuth((s) => s.perms)
  const first = useMemo(() => buildMenuTree(perms)[0]?.children?.[0]?.path, [perms])
  return first ? <Navigate to={first} replace /> : <ForbiddenPage />
}

export function AppRoutes() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route
        path="/login/oauth/:provider"
        element={
          <Suspense fallback={<PageLoading />}>
            <OAuthCallbackPage />
          </Suspense>
        }
      />
      {/* 已初始化的进程里向导不存在，直接去登录页 */}
      <Route path="/setup" element={<Navigate to="/login" replace />} />
      <Route
        path="/mcp/confirmations/:id"
        element={
          <RequireAuth>
            <Suspense fallback={<PageLoading />}>
              <McpConfirmationPage />
            </Suspense>
          </RequireAuth>
        }
      />
      <Route
        path="/sso/authorize"
        element={
          <RequireAuth>
            <Suspense fallback={<PageLoading />}>
              <SsoAuthorizePage />
            </Suspense>
          </RequireAuth>
        }
      />
      {/* fullscreen 路由：有鉴权但无布局壳（数据大屏 / 三维场景等沉浸页面），先于布局分支注册 */}
      {allRoutes
        .filter((r) => r.fullscreen)
        .map((r) => (
          <Route
            key={r.path}
            path={r.path}
            element={
              <RequireAuth>
                <Guarded route={r} />
              </RequireAuth>
            }
          />
        ))}
      <Route
        element={
          <RequireAuth>
            <AppLayout />
          </RequireAuth>
        }
      >
        <Route index element={<HomeRedirect />} />
        {allRoutes
          .filter((r) => !r.fullscreen)
          .map((r) => (
            <Route key={r.path} path={r.path} element={<Guarded route={r} />} />
          ))}
        <Route path="403" element={<ForbiddenPage />} />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  )
}

export default AppRoutes
