import assert from 'node:assert/strict'
import { test } from 'node:test'
import { repairJsonText } from './json-repair.ts'

// 运行：cd web && node --test src/framework/components/json-repair.test.ts

test('修复单引号与裸键', () => {
  const r = repairJsonText("{'sadad':222}")
  assert.equal(r.ok, true)
  if (r.ok) assert.deepEqual(JSON.parse(r.text), { sadad: 222 })
})

test('修复尾逗号、注释与未闭合括号', () => {
  for (const broken of ['{"a":1,}', '{"a":"x" // note\n}', '{"a":[1,2']) {
    const r = repairJsonText(broken)
    assert.equal(r.ok, true, broken)
    if (r.ok) assert.doesNotThrow(() => JSON.parse(r.text), broken)
  }
})

test('长整数保持原样，不被 JS 数字精度截断', () => {
  const r = repairJsonText('{id: 12345678901234567890}')
  assert.equal(r.ok, true)
  if (r.ok) {
    assert.ok(r.text.includes('12345678901234567890'), r.text)
    assert.equal(r.text.includes('e+'), false, r.text)
  }
})

test('无法修复时返回失败原因而不是抛异常', () => {
  const r = repairJsonText('')
  assert.equal(r.ok, false)
  if (!r.ok) assert.ok(r.message.length > 0)
})
