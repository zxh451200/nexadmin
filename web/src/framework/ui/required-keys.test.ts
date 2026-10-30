import assert from 'node:assert/strict'
import { test } from 'node:test'
import { z } from 'zod'
import { zodResolver } from '@hookform/resolvers/zod'
import { clearedCopy, computeRequiredKeys, isEmptyValue, valuesSignature } from './required-keys.ts'

// 运行：cd web && node --test src/framework/ui/required-keys.test.ts

const resolverOf = (schema: z.ZodTypeAny) =>
  zodResolver(schema) as unknown as Parameters<typeof computeRequiredKeys>[0]

const isJsonText = (v: string) => {
  if (!v.trim()) return true
  try {
    const o: unknown = JSON.parse(v)
    return !!o && typeof o === 'object' && !Array.isArray(o)
  } catch {
    return false
  }
}

// 定时任务：参数允许留空，但写了就必须是 JSON 对象
const jobSchema = z.object({
  name: z.string().trim().min(1, 'name required').max(100),
  handler: z.string().min(1, 'handler required'),
  retryCount: z.number().int().min(0).max(10),
  enabled: z.boolean(),
  params: z.string().refine(isJsonText, 'params must be object'),
})

test('选填字段即使内容非法也不算必填', async () => {
  const required = await computeRequiredKeys(resolverOf(jobSchema), {
    name: '心跳',
    handler: 'http',
    retryCount: 0,
    enabled: true,
    params: '{"sss"ssss"',
  })
  assert.equal(required.has('params'), false)
})

test('必填字段填对之后星号仍然保留', async () => {
  const required = await computeRequiredKeys(resolverOf(jobSchema), {
    name: '心跳',
    handler: 'http',
    retryCount: 0,
    enabled: true,
    params: '{"method":"GET"}',
  })
  assert.deepEqual([...required].sort(), ['handler', 'name'])
})

test('数字与布尔没有「空」的形态，不被判成必填', async () => {
  const required = await computeRequiredKeys(resolverOf(jobSchema), {
    name: '',
    handler: '',
    retryCount: 3,
    enabled: false,
    params: '',
  })
  assert.equal(required.has('retryCount'), false)
  assert.equal(required.has('enabled'), false)
  assert.deepEqual([...required].sort(), ['handler', 'name'])
})

// 菜单：路径只在类型为「菜单」时必填，权限串只在「按钮」时必填
const menuSchema = z
  .object({
    label: z.string().trim().min(1),
    type: z.enum(['catalog', 'menu', 'button']),
    path: z.string().max(200),
    perm: z.string().max(100),
  })
  .superRefine((v, ctx) => {
    if (v.type === 'menu' && !v.path.trim()) ctx.addIssue({ code: 'custom', path: ['path'], message: 'path required' })
    if (v.type === 'button' && !v.perm.trim()) ctx.addIssue({ code: 'custom', path: ['perm'], message: 'perm required' })
  })

test('条件必填：当前为空且正在报错时给出星号', async () => {
  const base = { label: '用户列表', path: '', perm: '' }
  const asMenu = await computeRequiredKeys(resolverOf(menuSchema), { ...base, type: 'menu' })
  assert.equal(asMenu.has('path'), true)
  assert.equal(asMenu.has('perm'), false)

  const asButton = await computeRequiredKeys(resolverOf(menuSchema), { ...base, type: 'button' })
  assert.equal(asButton.has('perm'), true)
  assert.equal(asButton.has('path'), false)
})

// 公告：范围不是「全部」时必须选接收对象
const noticeSchema = z
  .object({
    title: z.string().trim().min(1),
    scope: z.enum(['all', 'dept']),
    targetIds: z.array(z.string()),
  })
  .superRefine((v, ctx) => {
    if (v.scope !== 'all' && v.targetIds.length === 0) ctx.addIssue({ code: 'custom', path: ['targetIds'], message: 'pick targets' })
  })

test('数组型条件必填按长度判定', async () => {
  const dept = await computeRequiredKeys(resolverOf(noticeSchema), { title: '维护', scope: 'dept', targetIds: [] })
  assert.equal(dept.has('targetIds'), true)
  const all = await computeRequiredKeys(resolverOf(noticeSchema), { title: '维护', scope: 'all', targetIds: [] })
  assert.equal(all.has('targetIds'), false)
})

test('isEmptyValue 只把空串与空数组当没填', () => {
  assert.equal(isEmptyValue(''), true)
  assert.equal(isEmptyValue('   '), true)
  assert.equal(isEmptyValue([]), true)
  assert.equal(isEmptyValue(undefined), true)
  assert.equal(isEmptyValue(0), false)
  assert.equal(isEmptyValue(false), false)
  assert.equal(isEmptyValue(['a']), false)
})

test('clearedCopy 清空字符串与数组、保留开关与数字', () => {
  assert.deepEqual(
    clearedCopy({ s: 'x', a: ['1', '2'], n: 7, b: true, o: { k: 1 } }),
    { s: '', a: [], n: 7, b: true, o: { k: 1 } },
  )
})

test('valuesSignature 值变则变、引用换但内容不换则不变', () => {
  assert.notEqual(valuesSignature({ params: '{}' }), valuesSignature({ params: '{' }))
  assert.equal(valuesSignature({ list: ['a'] }), valuesSignature({ list: ['b'] }))
  assert.notEqual(valuesSignature({ list: ['a'] }), valuesSignature({ list: [] }))
})
