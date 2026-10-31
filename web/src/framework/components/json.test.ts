import assert from 'node:assert/strict'
import { test } from 'node:test'
import { checkJsonText, formatJsonText, isJsonObjectText, parseJsonText } from './json.ts'

// 运行：cd web && node --test src/framework/components/json.test.ts

test('parseJsonText 返回解析结果', () => {
  assert.deepEqual(parseJsonText('{"a":1}'), { ok: true, value: { a: 1 } })
})

test('parseJsonText 给出行列（1 起算）', () => {
  // 出错位置指向第三个行的意外符号 2
  const r = parseJsonText('{\n  "a": 1,\n  "b" 2\n}')
  assert.equal(r.ok, false)
  if (!r.ok) {
    assert.equal(r.line, 3)
    assert.equal(r.column, 7)
  }
})

test('parseJsonText 空串按语法错误处理', () => {
  assert.equal(parseJsonText('').ok, false)
})

test('checkJsonText 区分空值、语法错误与对象约束', () => {
  assert.deepEqual(checkJsonText(''), { ok: true })
  assert.deepEqual(checkJsonText('', { optional: false }), { ok: false, kind: 'empty' })
  assert.deepEqual(checkJsonText('   '), { ok: true })
  assert.deepEqual(checkJsonText('["a"]'), { ok: false, kind: 'object' })
  assert.deepEqual(checkJsonText('["a"]', { objectOnly: false }), { ok: true })
  assert.deepEqual(checkJsonText('"text"'), { ok: false, kind: 'object' })
  assert.deepEqual(checkJsonText('"text"', { objectOnly: false }), { ok: true })
  assert.deepEqual(checkJsonText('null'), { ok: false, kind: 'object' })
  assert.deepEqual(checkJsonText('{"a":1}'), { ok: true })
  const broken = checkJsonText('{"a" 1}')
  assert.equal(broken.ok, false)
  assert.equal(broken.ok ? '' : broken.kind, 'syntax')
})

test('isJsonObjectText 允许留空、拒绝数组与非法 JSON', () => {
  assert.equal(isJsonObjectText(''), true)
  assert.equal(isJsonObjectText('{}'), true)
  assert.equal(isJsonObjectText('[]'), false)
  assert.equal(isJsonObjectText('{"a" 1}'), false)
})

test('checkJsonText 按原因给出可操作的修复提示', () => {
  const reason = (text: string) => {
    const r = checkJsonText(text)
    return r.ok ? '' : r.kind === 'syntax' ? (r.reason ?? '') : r.kind
  }
  assert.equal(reason("{'a':1}"), 'singleQuote')
  assert.equal(reason('{“a”:1}'), 'fullWidth')
  assert.equal(reason('{"a":1,}'), 'trailingComma')
  assert.equal(reason('{"a":[1,2}'), 'unclosed')
  assert.equal(reason('{"a":1'), 'unclosed')
  assert.equal(reason('{\n// c\n"a":1}'), 'comment')
  // 字符串内容里的撇号 / 全角引号合法，不能被误判
  assert.equal(reason('{"a":"it\'s"}'), '')
  assert.equal(reason('{"a":"“ok”"}'), '')
})

test('formatJsonText 格式化与压缩，非法输入返回 null', () => {
  assert.equal(formatJsonText('{"a":1,"b":[2]}'), '{\n  "a": 1,\n  "b": [\n    2\n  ]\n}')
  assert.equal(formatJsonText('{ "a" : 1 }', 0), '{"a":1}')
  assert.equal(formatJsonText('{"a" 1}'), null)
})
