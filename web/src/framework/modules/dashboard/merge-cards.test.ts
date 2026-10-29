import assert from 'node:assert/strict'
import { test } from 'node:test'
import { dropInactivePluginCards, isValidCardId, mergeCards } from './merge-cards.ts'
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

const cardFiles = () => ({
  '/src/framework/modules/dashboard/cards.tsx': { cards: [card('fw-a', 'fw')] },
  '/src/pages/biz/cards.tsx': { cards: [card('page-a', 'page')] },
  '/src/plugins/report/cards.tsx': { cards: [card('report-a', 'report')] },
  '/src/plugins/cron/cards.tsx': { cards: [card('cron-a', 'cron')] },
})

test('未启用插件的卡片被剔除，框架与用户页面卡片不受影响', () => {
  const out = mergeCards(dropInactivePluginCards(cardFiles(), ['cron']))
  assert.deepEqual(out.map((c) => c.id).sort(), ['cron-a', 'fw-a', 'page-a'])
})

test('名单为空 = 一张插件卡片都不挂（含查询未回来的时候）', () => {
  const out = mergeCards(dropInactivePluginCards(cardFiles(), []))
  assert.deepEqual(out.map((c) => c.id).sort(), ['fw-a', 'page-a'])
})

test('问不到启用名单（mock / 旧后端）时保持原样', () => {
  assert.deepEqual(mergeCards(dropInactivePluginCards(cardFiles(), null)).length, 4)
})
