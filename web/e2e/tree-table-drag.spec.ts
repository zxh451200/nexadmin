import { expect, test, type Page } from '@playwright/test'

/**
 * 树形表格拖拽落位线的几何回归。
 * 为什么值得钉住：这条线的坐标全是物理像素算出来的，书写方向（LTR/RTL）、
 * 列宽、缩进任一处改动都不会报错，只会「画歪」——肉眼在宽表格里根本看不出画到表外。
 * 三条已知历史问题各留一条断言：线贯穿整行、RTL 下画到表外、RTL 手势方向反了。
 */

const HANDLE = '[data-drag-handle]'

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

async function anchorOf(page: Page, index = 1) {
  return page.evaluate((sel) => {
    const el = document.querySelectorAll('[data-sortable-id]')[1]
    const h = el.querySelector(sel)!.getBoundingClientRect()
    return { left: h.left, right: h.right, x: h.left + h.width / 2, y: h.top + h.height / 2, n: document.querySelectorAll(sel).length }
  }, HANDLE)
}

/** 起拖 → 过激活阈值 → 移到落点；返回一次测量结果 */
async function dragAndMeasure(page: Page, from: { x: number; y: number }, dx: number, dy: number) {
  await page.mouse.move(from.x, from.y)
  await page.mouse.down()
  await page.mouse.move(from.x, from.y + 14, { steps: 6 })
  await page.mouse.move(from.x + dx, from.y + dy, { steps: 10 })
  await page.waitForTimeout(150)
  const m = await page.evaluate(() => {
    const line = document.querySelector('[data-drop-line="1"]')
    if (!line) return null
    const lr = line.getBoundingClientRect()
    const rows = [...document.querySelectorAll('[data-sortable-id]')] as HTMLElement[]
    const over = rows.find((r) => {
      const b = r.getBoundingClientRect()
      return lr.top >= b.top - 2 && lr.top <= b.bottom + 2
    }) ?? rows[rows.length - 1]
    const ob = over.getBoundingClientRect()
    const name = over.querySelector('td:nth-child(2)')?.getBoundingClientRect()
    return {
      line: { left: lr.left, right: lr.right, width: lr.width },
      row: { left: ob.left, right: ob.right },
      name: name ? { left: name.left, right: name.right } : null,
      level: Number(/(\d+)/.exec(line.textContent ?? '')?.[1] ?? NaN),
    }
  })
  // 取消这次拖拽：落位线只是预览，用例不许真的改数据
  await page.keyboard.press('Escape')
  await page.mouse.up()
  await page.waitForTimeout(120)
  return m
}

const INSET = 48
const STEP = 24

test.describe('落位线几何 · LTR', () => {
  test.beforeEach(async ({ page }) => { await openMenu(page) })

  test('从把手 +48 起算，落在行内且远短于整行', async ({ page }) => {
    const a = await anchorOf(page)
    expect(a.n).toBeGreaterThan(0)
    const m = await dragAndMeasure(page, a, 0, 60)!
    expect(m, '拖拽没起出落位线').not.toBeNull()
    const r = m!
    expect(Math.abs(r.line.left - (a.left + INSET))).toBeLessThanOrEqual(2)
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
    const a = await anchorOf(page)
    const flat = (await dragAndMeasure(page, a, 0, 60))!
    const deep = (await dragAndMeasure(page, a, 120, 60))!
    expect(deep.level).toBe(flat.level + 1)
    expect(Math.abs(deep.line.left - (flat.line.left + STEP))).toBeLessThanOrEqual(2)
  })
})

test.describe('落位线几何 · RTL', () => {
  test.beforeEach(async ({ page }) => {
    await openMenu(page)
    // 与真阿语走同一条 CSS 路径：Tailwind 的逻辑类按 dir 生效
    await page.evaluate(() => { document.documentElement.dir = 'rtl' })
    await page.waitForTimeout(200)
  })

  test('线落在行内、从右侧把手往左起算', async ({ page }) => {
    const a = await anchorOf(page)
    const m = await dragAndMeasure(page, a, 0, 60)
    expect(m, '拖拽没起出落位线').not.toBeNull()
    const r = m!
    expect(Math.abs(r.line.right - (a.right - INSET))).toBeLessThanOrEqual(2)
    // 曾经的表现：线从行的物理右边起算，整条画到表格外面
    expect(r.line.right).toBeLessThanOrEqual(r.row.right + 1)
    expect(r.line.left).toBeGreaterThanOrEqual(r.row.left - 1)
    expect(r.line.width).toBeGreaterThanOrEqual(120)
    expect(r.line.width).toBeLessThan(r.row.right - r.row.left - 200)
    expect(r.level).toBe(1)
  })

  test('往左拖（RTL 的内侧）= 降级，缩进往左一档', async ({ page }) => {
    const a = await anchorOf(page)
    const flat = (await dragAndMeasure(page, a, 0, 60))!
    const deep = (await dragAndMeasure(page, a, -120, 60))!
    expect(deep.level).toBe(flat.level + 1)
    expect(Math.abs(deep.line.right - (flat.line.right - STEP))).toBeLessThanOrEqual(2)
  })
})

test.describe('落位线几何 · 移动端卡片', () => {
  test.use({ viewport: { width: 420, height: 900 } })

  test('量不到 td 时退回整卡，仍不越界', async ({ page }) => {
    await openMenu(page)
    await page.evaluate(() => { document.documentElement.dir = 'rtl' })
    await page.waitForTimeout(300)
    const a = await anchorOf(page)
    const m = await dragAndMeasure(page, a, 0, 70)
    expect(m, '卡片模式没起出落位线').not.toBeNull()
    const r = m!
    expect(r.line.left).toBeGreaterThanOrEqual(r.row.left - 1)
    expect(r.line.right).toBeLessThanOrEqual(r.row.right + 1)
  })
})
