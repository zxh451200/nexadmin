import assert from 'node:assert/strict'
import { test } from 'node:test'
import { isValidCardId, mergeCards } from './merge-cards.ts'
import type { DashboardCard } from './types'

const card = (id: string, tag: string): DashboardCard => ({ id, title: tag, size: 'sm', component: (() => null) as never })

// 运行：cd web && node --test src/framework/modules/dashboard/*.test.ts
test('用户页面覆盖插件覆盖框架，与文件名字母序无关', () => {
  const out = mergeCards({
    '/src/pages/aaa/cards.tsx': { cards: [card('dashboard-stats', 'mine')] },
    '/src/plugins/p/cards.tsx': { cards: [card('dashboard-stats', 'plugin'), card('p-x', 'plugin-x')] },
    '/src/framework/modules/zzz/cards.tsx': { cards: [card('dashboard-stats', 'framework'), card('zzz-a', 'fw-a')] },
  })
  const byId = new Map(out.map((c) => [c.id, c.title]))
  assert.equal(byId.get('dashboard-stats'), 'mine')
  assert.equal(byId.get('p-x'), 'plugin-x')
  assert.equal(byId.get('zzz-a'), 'fw-a')
  assert.equal(out.length, 3)
})

test('插件覆盖框架', () => {
  const out = mergeCards({
    '/src/plugins/p/cards.tsx': { cards: [card('a-b', 'plugin')] },
    '/src/framework/modules/m/cards.tsx': { cards: [card('a-b', 'framework')] },
  })
  assert.deepEqual(out.map((c) => [c.id, c.title]), [['a-b', 'plugin']])
})

test('空目录与没有 cards 导出的模块不报错', () => {
  assert.deepEqual(mergeCards({}), [])
  assert.deepEqual(mergeCards({ '/src/pages/x/cards.tsx': {} }), [])
})

test('不合法的 id 被忽略', () => {
  const out = mergeCards({
    '/src/pages/x/cards.tsx': { cards: [card('Bad', 'x'), card('-bad', 'x'), card('a b', 'x'), card('a'.repeat(65), 'x'), card('ok-1_a.b', 'ok')] },
  })
  assert.deepEqual(out.map((c) => c.id), ['ok-1_a.b'])
  assert.equal(isValidCardId('a'.repeat(64)), true)
  assert.equal(isValidCardId(''), false)
})
