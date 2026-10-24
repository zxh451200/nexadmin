import assert from 'node:assert/strict'
import { test } from 'node:test'
import { canPlace, clampItem, firstFreeSlot, GRID_COLS, overlaps, shapeError, toCartesian, toPie } from './chartData.ts'
import type { QueryResult, ScreenItem } from './api'

// 运行：cd web && node --test src/plugins/report/chartData.test.ts
const it = (x: number, y: number, w: number, h: number, id = 'c'): ScreenItem => ({ chartId: id, x, y, w, h })

test('栅格：重叠、越界与空位查找', () => {
  assert.equal(overlaps(it(0, 0, 4, 4), it(4, 0, 4, 4)), false) // 相邻不算重叠
  assert.equal(overlaps(it(0, 0, 4, 4), it(3, 3, 4, 4)), true)
  const items = [it(0, 0, 12, 4, 'a'), it(12, 0, 12, 4, 'b')]
  assert.equal(canPlace(items, 0, it(0, 0, 13, 4, 'a')), false) // 压到 b
  assert.equal(canPlace(items, 0, it(0, 0, 12, 8, 'a')), true)
  assert.equal(canPlace(items, 0, it(20, 0, 8, 4, 'a')), false) // 超出 24 列
  assert.equal(canPlace(items, 0, it(-1, 0, 4, 4, 'a')), false)
  assert.deepEqual(firstFreeSlot(items, 8, 4), { x: 0, y: 4 })
  assert.equal(firstFreeSlot([it(0, 0, GRID_COLS, 60)], 1, 1), null)
  assert.deepEqual(clampItem(it(30, -2, 40, 3)), { chartId: 'c', x: 0, y: 0, w: 24, h: 3 })
})

test('图表形状校验与后端一致', () => {
  const d = { column: 'a' }
  const m = { agg: 'count' as const }
  assert.equal(shapeError('bar', { dimensions: [d], measures: [m] }), null)
  assert.equal(shapeError('bar', { dimensions: [], measures: [m] }), 'shapeCartesian')
  assert.equal(shapeError('pie', { dimensions: [d, d], measures: [m] }), 'shapePie')
  assert.equal(shapeError('metric', { dimensions: [d], measures: [m] }), 'shapeMetric')
  assert.equal(shapeError('metric', { dimensions: [], measures: [m] }), null)
  assert.equal(shapeError('table', { dimensions: [], measures: [] }), 'shapeTable')
})

test('结果集转换：单维度多度量、双维度展开、饼图', () => {
  const one: QueryResult = {
    truncated: false,
    columns: [
      { key: 'd0', label: 'D', kind: 'dim', type: 'string' },
      { key: 'm0', label: 'M0', kind: 'measure', type: 'number' },
      { key: 'm1', label: 'M1', kind: 'measure', type: 'number' },
    ],
    rows: [{ d0: 'a', m0: 1, m1: 2 }, { d0: 'b', m0: 3, m1: null }],
  }
  const c1 = toCartesian(one)
  assert.deepEqual(c1.series.map((s) => s.key), ['m0', 'm1'])
  assert.deepEqual(c1.data[1], { x: 'b', m0: 3, m1: null })

  const two: QueryResult = {
    truncated: false,
    columns: [
      { key: 'd0', label: 'D0', kind: 'dim', type: 'string' },
      { key: 'd1', label: 'D1', kind: 'dim', type: 'string' },
      { key: 'm0', label: 'M', kind: 'measure', type: 'number' },
    ],
    rows: [{ d0: 'x', d1: 'ok', m0: 5 }, { d0: 'x', d1: 'bad', m0: 1 }, { d0: 'y', d1: 'ok', m0: 2 }],
  }
  const c2 = toCartesian(two)
  assert.deepEqual(c2.series.map((s) => s.label), ['ok', 'bad'])
  assert.deepEqual(c2.data[0], { x: 'x', 's:ok': 5, 's:bad': 1 })
  assert.deepEqual(toPie(one), [{ name: 'a', value: 1 }, { name: 'b', value: 3 }])
})
