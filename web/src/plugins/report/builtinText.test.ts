import assert from 'node:assert/strict'
import { test } from 'node:test'
import en from './locales/en-US.json' with { type: 'json' }
import zh from './locales/zh-CN.json' with { type: 'json' }
import { builtinText, columnText, fieldText, revertBuiltin } from './builtinText.ts'

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

/** 模拟 i18next：命中返回译文，未命中返回 defaultValue（没给就返回键本身） */
const dictT = (dict: Record<string, string>) => {
  const t = ((k: string, opts?: { defaultValue?: string }) => dict[k] ?? opts?.defaultValue ?? k) as never
  return t
}

test('编辑内置行：没改动写回原键，改过才存原文', () => {
  const t = dictT({
    'plugin_report.builtin.report.ops': '运营与运维总览',
    'plugin_report.builtin.filter.range': '时间范围',
  })
  assert.equal(revertBuiltin(t, 'plugin_report.builtin.report.ops', '运营与运维总览'), 'plugin_report.builtin.report.ops')
  assert.equal(revertBuiltin(t, 'plugin_report.builtin.report.ops', '我自己的看板'), '我自己的看板')
  // 删行/换序后按下标对不上原值，所以允许多个候选原值
  assert.equal(revertBuiltin(t, ['plugin_report.builtin.report.ops', 'plugin_report.builtin.filter.range'], '时间范围'), 'plugin_report.builtin.filter.range')
  assert.equal(revertBuiltin(t, ['plugin_report.builtin.filter.range', undefined], '时间范围'), 'plugin_report.builtin.filter.range')
  assert.equal(revertBuiltin(t, '我自己起的名字', '我自己起的名字'), '我自己起的名字')
  assert.equal(revertBuiltin(t, null, ''), '')
})

test('列名按「表+列」取字典：method 在操作日志是方法、在支付单是支付方式', () => {
  const t = dictT({
    'plugin_report.field.method': '方法',
    'plugin_report.field.plg_pay_order.method': '支付方式',
  })
  assert.equal(fieldText(t, 'plg_pay_order', 'method', '支付方式'), '支付方式')
  assert.equal(fieldText(t, 'sys_oper_log', 'method', '方法'), '方法')
  assert.equal(fieldText(t, 'sys_oper_log', 'no_such_col', '兜底文案'), '兜底文案')
  assert.equal(fieldText(t, '', 'no_such_col', ''), 'no_such_col')
  assert.equal(fieldText(t, 'sys_oper_log', '', '兜底文案'), '兜底文案')
})

test('结果列标题：内置度量按内置键，维度按表+列，都不露键', () => {
  const t = dictT({
    'plugin_report.builtin.measure.count': '次数',
    'plugin_report.field.plg_cron_log.status': '状态',
  })
  assert.equal(columnText(t, 'plg_cron_log', { label: 'plugin_report.builtin.measure.count' }), '次数')
  assert.equal(columnText(t, 'plg_cron_log', { label: '状态', field: 'status' }), '状态')
  assert.equal(columnText(t, undefined, { label: '中文兜底', field: 'x' }), '中文兜底')
})
