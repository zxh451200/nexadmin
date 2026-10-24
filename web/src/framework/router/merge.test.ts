import assert from 'node:assert/strict'
import { test } from 'node:test'
import { mergeRoutes } from './merge.ts'
import type { ModuleRoute } from './types'

const page = (path: string, tag: string): ModuleRoute => ({ path, component: (() => null) as never, menu: { title: tag } })

// 运行：cd web && node --test src/framework/router/merge.test.ts
test('用户 pages 的路由覆盖框架同 path，与文件名字母序无关', () => {
  const out = mergeRoutes({
    '/src/pages/aaa/route.tsx': { routes: [page('/system/user', 'mine')] },
    '/src/plugins/p/route.tsx': { routes: [page('system/user', 'plugin'), page('p/x', 'plugin-x')] },
    '/src/framework/modules/zzz/route.tsx': { routes: [page('system/user', 'framework'), page('system/role', 'framework-role')] },
  })
  const byPath = new Map(out.map((r) => [r.path, r.menu?.title]))
  assert.equal(byPath.get('system/user'), 'mine')
  assert.equal(byPath.get('system/role'), 'framework-role')
  assert.equal(byPath.get('p/x'), 'plugin-x')
  assert.equal(out.length, 3)
})

test('插件覆盖框架，前导斜杠被忽略', () => {
  const out = mergeRoutes({
    '/src/plugins/p/route.tsx': { routes: [page('/system/post', 'plugin')] },
    '/src/framework/modules/post/route.tsx': { routes: [page('system/post', 'framework')] },
  })
  assert.deepEqual(out.map((r) => [r.path, r.menu?.title]), [['system/post', 'plugin']])
})
