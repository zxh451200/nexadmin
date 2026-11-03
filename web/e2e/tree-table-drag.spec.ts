import { expect, test, type Page } from '@playwright/test'

/**
 * 树形表格拖拽落位线的几何回归。
 * 为什么值得钉住：这条线的坐标全是物理像素算出来的，书写方向（LTR/RTL）、
 * 列宽、缩进任一处改动都不会报错，只会「画歪」——肉眼在宽表格里根本看不出画到表外。
 * 四条已知历史问题各留一条断言：线贯穿整行、RTL 下画到表外、RTL 手势方向反了、
 * 以及「把手按钮自身已按深度缩进，plan() 又叠一次 depth×步长」的双重缩进。
 */

// 与 TreeTable 的常量对齐：落位线从把手单元格起始沿往里 48px，一层缩进 24px
const INSET = 48
const STEP = 24

// vite dev 首次按需编译可能很慢，机器上还跑着别的活时更明显：给足冷启动时间，
// 别让它变成一条「机器忙就红」的假红用例。
test.setTimeout(120_000)

async function openMenu(page: Page) {
  await page.addInitScript(() => {
    localStorage.setItem('mock-initialized', 'true')
    localStorage.setItem('nex-access-token', 'mock.a.admin.1')
    localStorage.setItem('nex-refresh-token', 'mock.r.admin.1')
    localStorage.setItem('nex-lang', 'zh-CN')
  })
  await page.goto('/admin/system/menu', { waitUntil: 'domcontentloaded' })
  // 表格是 tr、移动端是 div，统一等 [data-sortable-id]
  await page.waitForSelector('[data-sortable-id]')
  // 公告弹窗的遮罩会吃掉指针事件，拖拽会静默不生效
  const close = page.locator('[role=dialog] button[aria-label="Close"]')
  if (await close.count()) {
    await close.click()
    await page.waitForSelector('[role=dialog]', { state: 'detached', timeout: 5000 })
  }
}

/** 第 i 行的把手中心 + 把手单元格的起始沿（落位线就是从这条边往里量起算的） */
async function handleOf(page: Page, index: number) {
  return page.evaluate((i) => {
    const rows = [...document.querySelectorAll('[data-sortable-id]')] as HTMLElement[]
    const el = rows[i]
    const h = el?.querySelector('[data-drag-handle]')?.getBoundingClientRect()
    const cell = el?.querySelector('td')?.getBoundingClientRect()
    if (!h || !cell) return null
    return {
      x: h.left + h.width / 2,
      y: h.top + h.height / 2,
      startEdge: document.documentElement.dir === 'rtl' ? cell.right : cell.left,
    }
  }, index)
}

/**
 * 找一个缩进比最浅那档更深的行当靶子（折叠就先展开）。
 * 深度按把手按钮相对全表最浅起始沿算：不假设第一行是顶级，也不假设书写方向。
 */
async function nestedRow(page: Page) {
  const find = () => page.evaluate((step) => {
    const rows = [...document.querySelectorAll('[data-sortable-id]')] as HTMLElement[]
    const rtl = document.documentElement.dir === 'rtl'
    const hs = rows
      .map((el) => el.querySelector('[data-drag-handle]')?.getBoundingClientRect())
      .filter((h): h is DOMRect => !!h)
    if (!hs.length) return null
    const base = rtl ? Math.max(...hs.map((h) => h.right)) : Math.min(...hs.map((h) => h.left))
    for (const el of rows) {
      const h = el.querySelector('[data-drag-handle]')?.getBoundingClientRect()
      const cell = el.querySelector('td')?.getBoundingClientRect()
      if (!h || !cell) continue
      const depth = Math.round(rtl ? (base - h.right) / step : (h.left - base) / step)
      if (depth >= 1) return { x: h.left + h.width / 2, y: h.top + h.height / 2, depth }
    }
    return null
  }, STEP)
  if (await find()) return find()
  const toggle = page.locator('[data-sortable-id] button[aria-expanded="false"]').first()
  if (await toggle.count()) {
    await toggle.click()
    await page.waitForTimeout(250)
  }
  return find()
}

interface Measure {
  line: { left: number; right: number; width: number }
  row: { left: number; right: number }
  /** 落点行把手单元格的起始沿；卡片布局量不到 td 时为 0 */
  cellStart: number
  /** 落位线靠树那一头的边沿，与 cellStart 同一把尺 */
  lineStart: number
  rtl: boolean
  level: number
}

/** 起拖 → 过激活阈值 → 移到落点；返回一次测量结果 */
async function dragAndMeasure(page: Page, from: { x: number; y: number }, dx: number, dy: number): Promise<Measure | null> {
  await page.mouse.move(from.x, from.y)
  await page.mouse.down()
  await page.mouse.move(from.x, from.y + 14, { steps: 6 })
  await page.mouse.move(from.x + dx, from.y + dy, { steps: 10 })
  await page.waitForTimeout(150)
  const m = await page.evaluate<Measure | null>(() => {
    const line = document.querySelector('[data-drop-line="1"]')
    if (!line) return null
    const lr = line.getBoundingClientRect()
    const rtl = document.documentElement.dir === 'rtl'
    const rows = [...document.querySelectorAll('[data-sortable-id]')] as HTMLElement[]
    const over = rows.find((r) => {
      const b = r.getBoundingClientRect()
      return lr.top >= b.top - 2 && lr.top <= b.bottom + 2
    }) ?? rows[rows.length - 1]
    const ob = over.getBoundingClientRect()
    const cell = over.querySelector('td')?.getBoundingClientRect()
    return {
      line: { left: lr.left, right: lr.right, width: lr.width },
      row: { left: ob.left, right: ob.right },
      cellStart: (rtl ? cell?.right : cell?.left) ?? 0,
      lineStart: rtl ? lr.right : lr.left,
      rtl,
      level: Number(/(\d+)/.exec(line.textContent ?? '')?.[1] ?? NaN),
    }
  })
  // 取消这次拖拽：落位线只是预览，用例不许真的改数据
  await page.keyboard.press('Escape')
  await page.mouse.up()
  await page.waitForTimeout(120)
  return m
}

/**
 * 落位线起算的唯一尺子：落点行把手单元格的起始沿 + INSET + 每往下一级一个 STEP。
 * 少算一级线会贴到上一级；多算一级（把手按钮自己缩过进、plan() 又叠一次）从第 2 级起
 * 每级偏 24px，而顶级行完全看不出差别——所以这条断言必须在更深的行上跑。
 */
function expectLineStart(m: Measure) {
  const want = m.cellStart + (m.rtl ? -1 : 1) * (INSET + STEP * (m.level - 1))
  expect(Math.abs(m.lineStart - want), `落位线起始沿偏了 ${m.lineStart - want}px（第 ${m.level} 级）`).toBeLessThanOrEqual(2)
}

test.describe('落位线几何 · LTR', () => {
  test.beforeEach(async ({ page }) => { await openMenu(page) })

  test('从把手单元格 +48 起算，落在行内且远短于整行', async ({ page }) => {
    const a = await handleOf(page, 1)
    expect(a, '量不到把手列').not.toBeNull()
    const m = await dragAndMeasure(page, a!, 0, 60)
    expect(m, '拖拽没起出落位线').not.toBeNull()
    const r = m!
    expectLineStart(r)
    expect(r.line.left).toBeGreaterThanOrEqual(r.row.left - 1)
    expect(r.line.right).toBeLessThanOrEqual(r.row.right + 1)
    expect(r.line.width).toBeGreaterThanOrEqual(120)
    expect(r.level).toBe(1)
    // 线必须明显短于整行：贯穿整行就是当初那条「进度条」。
    // 这里不再断言「止于名称列右边界」——那要按单元格序号取列，而序号会随把手列/勾选列变化，
    // 钉它等于钉一个实现细节；「远短于整行 + 落在行内」已经覆盖了用户看到的那个缺陷。
    expect(r.line.width).toBeLessThan(r.row.right - r.row.left - 200)
  })

  test('往树内侧拖 = 降级，缩进一档', async ({ page }) => {
    const a = (await handleOf(page, 1))!
    const flat = (await dragAndMeasure(page, a, 0, 60))!
    const deep = (await dragAndMeasure(page, a, 120, 60))!
    expect(deep.level).toBe(flat.level + 1)
    expect(Math.abs(deep.line.left - (flat.line.left + STEP))).toBeLessThanOrEqual(2)
  })

  test('深层行：缩进只算一次，不跟着深度翻倍', async ({ page }) => {
    const g = await nestedRow(page)
    expect(g, '展开后也找不到比顶级更深的行，这条用例没有靶子').not.toBeNull()
    // 只做微小的纵向位移：落点仍在自己这一行，层级不变，量到的就是这一档缩进
    const m = await dragAndMeasure(page, g!, 0, 8)
    expect(m, '嵌套行拖拽没起出落位线').not.toBeNull()
    const r = m!
    expect(r.level).toBe(g!.depth + 1)
    expectLineStart(r)
  })
})

test.describe('落位线几何 · RTL', () => {
  test.beforeEach(async ({ page }) => {
    await openMenu(page)
    // 与真阿语走同一条 CSS 路径：Tailwind 的逻辑类按 dir 生效
    await page.evaluate(() => { document.documentElement.dir = 'rtl' })
    await page.waitForTimeout(200)
  })

  test('线落在行内、从把手单元格的右边往左起算', async ({ page }) => {
    const a = (await handleOf(page, 1))!
    const m = await dragAndMeasure(page, a, 0, 60)
    expect(m, '拖拽没起出落位线').not.toBeNull()
    const r = m!
    expect(Math.abs(r.line.right - (a.startEdge - INSET - STEP * (r.level - 1)))).toBeLessThanOrEqual(2)
    // 曾经的表现：线从行的物理右边起算，整条画到表格外面
    expect(r.line.right).toBeLessThanOrEqual(r.row.right + 1)
    expect(r.line.left).toBeGreaterThanOrEqual(r.row.left - 1)
    expect(r.line.width).toBeGreaterThanOrEqual(120)
    expect(r.line.width).toBeLessThan(r.row.right - r.row.left - 200)
    expect(r.level).toBe(1)
  })

  test('往左拖（RTL 的内侧）= 降级，缩进往左一档', async ({ page }) => {
    const a = (await handleOf(page, 1))!
    const flat = (await dragAndMeasure(page, a, 0, 60))!
    const deep = (await dragAndMeasure(page, a, -120, 60))!
    expect(deep.level).toBe(flat.level + 1)
    expect(Math.abs(deep.line.right - (flat.line.right - STEP))).toBeLessThanOrEqual(2)
  })

  test('深层行：缩进只算一次（往物理左）', async ({ page }) => {
    const g = await nestedRow(page)
    expect(g, '展开后也找不到比顶级更深的行，这条用例没有靶子').not.toBeNull()
    const m = await dragAndMeasure(page, g!, 0, 8)
    expect(m, '嵌套行拖拽没起出落位线').not.toBeNull()
    const r = m!
    expect(r.rtl).toBe(true)
    expect(r.level).toBe(g!.depth + 1)
    expectLineStart(r)
  })
})

test.describe('落位线几何 · 移动端卡片', () => {
  test.use({ viewport: { width: 420, height: 900 } })

  test('量不到 td 时退回整卡，仍不越界', async ({ page }) => {
    await openMenu(page)
    await page.evaluate(() => { document.documentElement.dir = 'rtl' })
    await page.waitForTimeout(300)
    // 卡片布局没有把手列单元格：直接按把手按钮取起拖点
    const from = await page.evaluate(() => {
      const h = document.querySelectorAll('[data-sortable-id]')[1]?.querySelector('[data-drag-handle]')?.getBoundingClientRect()
      return h ? { x: h.left + h.width / 2, y: h.top + h.height / 2 } : null
    })
    expect(from, '卡片模式量不到把手').not.toBeNull()
    const m = await dragAndMeasure(page, from!, 0, 70)
    expect(m, '卡片模式没起出落位线').not.toBeNull()
    const r = m!
    expect(r.cellStart, '卡片布局本就没有 td，起算边应退回 0').toBe(0)
    expect(r.line.left).toBeGreaterThanOrEqual(r.row.left - 1)
    expect(r.line.right).toBeLessThanOrEqual(r.row.right + 1)
  })
})
