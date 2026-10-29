import assert from 'node:assert/strict'
import { test } from 'node:test'
import en from './locales/en-US.json' with { type: 'json' }
import zh from './locales/zh-CN.json' with { type: 'json' }
import { builtinText } from './builtinText.ts'

// 运行：cd web && node --test src/plugins/report/builtinText.test.ts

type Dict = Record<string, unknown>

const flatten = (obj: Dict, prefix = ''): string[] =>
  Object.entries(obj).flatMap(([k, v]) =>
    v && typeof v === 'object' ? flatten(v as Dict, `${prefix}${k}.`) : [`${prefix}${k}`],
  )

const builtinKeys = (d: Dict) => flatten(d).filter((k) => k.startsWith('plugin_report.builtin.'))

test('内置键在双语字典里都存在且集合相同', () => {
  const zhKeys = builtinKeys(zh as Dict)
  const enKeys = builtinKeys(en as Dict)
  assert.ok(zhKeys.length >= 25, `内置键数量异常：${zhKeys.length}`)
  assert.deepEqual([...zhKeys].sort(), [...enKeys].sort())
  for (const k of zhKeys) {
    const pick = (d: Dict) => k.split('.').reduce<unknown>((acc, seg) => (acc as Dict)?.[seg], d)
    assert.ok(String(pick(zh as Dict)).trim().length > 0, `中文值缺失：${k}`)
    assert.ok(String(pick(en as Dict)).trim().length > 0, `英文值缺失：${k}`)
  }
})

test('只翻译内置前缀，用户自建的名称原样返回', () => {
  const dict: Record<string, string> = { 'plugin_report.builtin.chart.loginTrend': 'Login trend' }
  const calls: string[] = []
  const t = ((k: string) => {
    calls.push(k)
    return dict[k] ?? k
  }) as never
  assert.equal(builtinText(t, 'plugin_report.builtin.chart.loginTrend'), 'Login trend')
  assert.equal(builtinText(t, '我自己起的名字'), '我自己起的名字')
  assert.equal(builtinText(t, 'plugin_report.builtin.chart.loginTrend'), 'Login trend')
  assert.deepEqual(
    calls,
    ['plugin_report.builtin.chart.loginTrend', 'plugin_report.builtin.chart.loginTrend'],
    '非内置键不该走翻译',
  )
  assert.equal(builtinText(t, ''), '')
  assert.equal(builtinText(t, null), '')
})
