import assert from 'node:assert/strict'
import { test } from 'node:test'
import { promoteLoneGroups, type MenuNodeShape } from './menu-shape.ts'

// 运行：cd web && node --test src/framework/router/menu-shape.test.ts

const item = (key: string): MenuNodeShape => ({ key, title: key, path: `/${key}` })
const group = (key: string, kids: MenuNodeShape[]): MenuNodeShape => ({ key, title: key, icon: 'Folder', children: kids })

test('两个及以上子项的分组保持原样', () => {
  const groups = [group('content', [item('notice'), item('file')])]
  const out = promoteLoneGroups(groups)
  assert.deepEqual(out, groups)
  assert.equal(out[0].title, 'content')
})

test('只剩一个子项的分组被拆成一级单项（标题清空、加 top- 前缀）', () => {
  const [only] = promoteLoneGroups([group('io', [item('jobs')])])
  assert.equal(only.title, '')
  assert.equal(only.key, 'top-io')
  assert.equal(only.icon, undefined)
  assert.deepEqual(only.children?.map((c) => c.key), ['jobs'])
})

test('已经是合成单项（title 为空）的节点不会被再套一层', () => {
  const wrapper: MenuNodeShape = { key: 'top-42', title: '', children: [item('dashboard')] }
  const out = promoteLoneGroups([wrapper])
  assert.equal(out[0].key, 'top-42')
  assert.equal(out[0].title, '')
})

test('空分组不在这里删（交给调用方），但也不能被误提升', () => {
  const empty = group('gone', [])
  const out = promoteLoneGroups([empty])
  assert.deepEqual(out, [empty])
})

test('多组混合时只动退化的那一组', () => {
  const out = promoteLoneGroups([group('a', [item('1'), item('2')]), group('io', [item('jobs')])])
  assert.equal(out[0].key, 'a')
  assert.equal(out[1].key, 'top-io')
  assert.equal(out[1].children?.length, 1)
})
