import assert from 'node:assert/strict'
import { test } from 'node:test'
import { allCollapsed, nextExpandedAfterBulkToggle } from './tree-expand.ts'

// 运行：cd web && node --test src/framework/components/tree-expand.test.ts

test('一个都没展开 = 全收起，按钮给「全部展开」', () => {
  assert.equal(allCollapsed(['a', 'b'], new Set()), true)
})

test('部分展开与全展开都算「未收起」，按钮给「全部收起」', () => {
  assert.equal(allCollapsed(['a', 'b'], new Set(['a'])), false)
  assert.equal(allCollapsed(['a', 'b'], new Set(['a', 'b'])), false)
})

test('没有可展开节点时不算全收起（按钮不该出现）', () => {
  assert.equal(allCollapsed([], new Set()), false)
})

test('展开集合里的旧 id 不影响判定', () => {
  assert.equal(allCollapsed(['a', 'b'], new Set(['deleted-node'])), true)
})

test('点一次：全收起→全展开，其余→全收起', () => {
  assert.deepEqual([...nextExpandedAfterBulkToggle(['a', 'b'], new Set())].sort(), ['a', 'b'])
  assert.equal(nextExpandedAfterBulkToggle(['a', 'b'], new Set(['a'])).size, 0)
  assert.equal(nextExpandedAfterBulkToggle(['a', 'b'], new Set(['a', 'b'])).size, 0)
})
