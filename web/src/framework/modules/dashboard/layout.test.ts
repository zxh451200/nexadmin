import assert from 'node:assert/strict'
import { test } from 'node:test'
import { mergeLayout, moveItem, moveVisible, sameLayout } from './layout.ts'
import type { DashboardCard } from './types'

const c = (id: string, extra: Partial<DashboardCard> = {}): DashboardCard => ({ id, title: id, size: 'sm', component: (() => null) as never, ...extra })

// 运行：cd web && node --test src/framework/modules/dashboard/*.test.ts
test('没有保存的布局：按 order 排序，defaultHidden 初始隐藏', () => {
  const out = mergeLayout([c('c', { order: 30 }), c('a', { order: 10 }), c('b', { order: 20, defaultHidden: true }), c('z')], null)
  assert.deepEqual(out.order, ['a', 'b', 'c', 'z'])
  assert.deepEqual(out.hidden, ['b'])
})

test('已保存顺序里仍存在的卡片按保存顺序', () => {
  const out = mergeLayout([c('a', { order: 1 }), c('b', { order: 2 }), c('c', { order: 3 })], { order: ['c', 'a', 'b'], hidden: [] })
  assert.deepEqual(out.order, ['c', 'a', 'b'])
})

test('不再存在的 id 被丢弃（order 与 hidden 都是）', () => {
  const out = mergeLayout([c('a'), c('b')], { order: ['gone', 'b', 'a'], hidden: ['gone2', 'a'] })
  assert.deepEqual(out.order, ['b', 'a'])
  assert.deepEqual(out.hidden, ['a'])
})

test('新卡片追加到末尾并按 order 排序；defaultHidden 的新卡片初始隐藏', () => {
  const out = mergeLayout(
    [c('old1'), c('old2'), c('new-late', { order: 50 }), c('new-early', { order: 5 }), c('new-hidden', { order: 10, defaultHidden: true })],
    { order: ['old2', 'old1'], hidden: [] },
  )
  assert.deepEqual(out.order, ['old2', 'old1', 'new-early', 'new-hidden', 'new-late'])
  assert.deepEqual(out.hidden, ['new-hidden'])
})

test('用户显式显示过的 defaultHidden 卡片不会被重新隐藏', () => {
  const out = mergeLayout([c('a', { defaultHidden: true })], { order: ['a'], hidden: [] })
  assert.deepEqual(out.hidden, [])
})

test('只出现在 hidden 的已知卡片保持隐藏，并追加到末尾以便恢复', () => {
  const out = mergeLayout([c('a'), c('b'), c('x', { defaultHidden: false })], { order: ['b'], hidden: ['x'] })
  assert.deepEqual(out.order, ['b', 'a', 'x'])
  assert.deepEqual(out.hidden, ['x'])
})

test('保存的 order 里重复的 id 只保留第一个', () => {
  assert.deepEqual(mergeLayout([c('a'), c('b')], { order: ['b', 'a', 'b'], hidden: [] }).order, ['b', 'a'])
})

test('没有 order 的卡片排在有 order 的后面，同值保持输入顺序', () => {
  assert.deepEqual(mergeLayout([c('n1'), c('o', { order: 1 }), c('n2')], null).order, ['o', 'n1', 'n2'])
})

test('moveItem 到头不动、不存在不动', () => {
  const ids = ['a', 'b', 'c']
  assert.deepEqual(moveItem(ids, 'a', -1), ids)
  assert.deepEqual(moveItem(ids, 'c', 1), ids)
  assert.deepEqual(moveItem(ids, 'x', 1), ids)
  assert.deepEqual(moveItem(ids, 'b', -1), ['b', 'a', 'c'])
})

test('moveVisible 跳过隐藏的卡片，隐藏项位置不变', () => {
  const order = ['a', 'h', 'b', 'c']
  assert.deepEqual(moveVisible(order, ['h'], 'b', -1), ['b', 'h', 'a', 'c'])
  assert.deepEqual(moveVisible(order, ['h'], 'c', 1), order)
})

test('sameLayout：hidden 视为集合', () => {
  assert.equal(sameLayout({ order: ['a', 'b'], hidden: ['a', 'b'] }, { order: ['a', 'b'], hidden: ['b', 'a'] }), true)
  assert.equal(sameLayout({ order: ['a', 'b'], hidden: [] }, { order: ['b', 'a'], hidden: [] }), false)
})
