import { expect, test, type Page } from '@playwright/test'

/**
 * 滑块验证码的几何与接线回归。
 * 为什么值得钉住：孔位、滑块图落点、把手行程三处坐标分别按「后端图片像素」「CSS 百分比」
 * 「轨道 CSS 像素」三套口径算，任一处写错都不报错——只会画歪，或永远拖不到位。
 * 另外钉一条真实通道：滑块签发的 token 必须当作 captcha 交给 /auth/login 现成的校验口
 * （登录、短信、注册共用这一条），走偏一次就是「验证码错误或已过期」。
 *
 * 只在 mock 数据源上跑：孔位从假数据背景 SVG 里读出（与后端同一套尺寸常量）。
 * 这里量的是前端换算与接线，后端判定（容差、轨迹像人、一次性消费、IP 绑定）由 core/auth 的用例钉住。
 * 断言只看 UI：mock 在 axios 适配器里就地应答，不产生网络事件，waitForResponse 永远等不到。
 */

// 与后端 slider_render.go 同口径（auth.mock.ts 出图也按这组常量）
const IMG_W = 280

test.setTimeout(240_000)

/**
 * dev 冷启动有两层慢：新浏览器上下文要把几百个模块重新下一遍；而 /auth/methods 若在
 * mock 注册表就绪之前发出就会落到 FALLBACK（captchaEnabled 缺失 → 连验证码区都不出现），
 * 并被 60s staleTime 钉住。所以按导航重试，而不是假设第一次就成。
 * 全程留在默认的「账号登录」表单上：手机号 tab 在满载机器上会随查询落定反复重挂。
 */
async function openLogin(page: Page): Promise<string> {
  const account = 'slider' + Date.now().toString(36) // 每次跑用新账号：mock 的失败计数按用户名累计
  await page.addInitScript((name) => {
    localStorage.setItem('mock-initialized', 'true')
    localStorage.setItem('nex-lang', 'zh-CN')
    localStorage.setItem('mock-admin', JSON.stringify({ username: name, password: 'pw-for-e2e' }))
  }, account)
  const toggle = page.getByRole('button', { name: '改用滑块验证' })
  for (let attempt = 0; attempt < 3; attempt++) {
    await page.goto('/admin/login', { waitUntil: 'domcontentloaded' })
    // 必须用会等的断言：locator.isVisible() 不等待，timeout 传了也白传。
    const ready = await toggle
      .waitFor({ state: 'visible', timeout: 60_000 })
      .then(() => true)
      .catch(() => false)
    if (ready) {
      await toggle.click()
      await expect(page.getByRole('slider')).toBeVisible()
      // 滑轨不依赖挑战是否取到；只等滑轨会把「图没加载」吞成后面一句「找不到孔位」。
      // 背景图按 alt 取（滑块图是 aria-hidden，role=img 只有背景这一个）。
      await expect(page.getByRole('img', { name: '验证' })).toBeVisible()
      return account
    }
  }
  const where = await page
    .evaluate(() => document.body.innerText.replace(/\s+/g, ' ').slice(0, 200))
    .catch(() => '(读不到正文)')
  throw new Error(`登录页始终没出现验证码区：${where}`)
}

/** 从 mock 背景图（SVG data URI）里读孔位横坐标；图还没换好时返回 null 而不是抛错 */
async function readHole(page: Page): Promise<number | null> {
  return page.evaluate(() => {
    const img = document.querySelector<HTMLImageElement>('img[alt="验证"]')
    const src = img?.src ?? ''
    if (!src.startsWith('data:image/svg+xml;base64,')) return null
    const svg = atob(src.split(',')[1])
    const m = svg.match(/<rect x="(\d+)" y="(\d+)" width="56" height="56" fill="#111827"/)
    return m ? Number(m[1]) : null
  })
}

async function holeOf(page: Page): Promise<number> {
  const x = await readHole(page)
  if (x !== null) return x
  const srcs = await page.evaluate(() => [...document.querySelectorAll('img')].map((i) => i.src.slice(0, 40)).join(' | '))
  throw new Error(`读不到 mock 背景里的孔位。页面上的图是：${srcs || '(没有 img)'}`)
}

/** 按住把手拖到目标图片像素横坐标；换算系数 = 轨道宽 / 背景图宽 */
async function dragTo(page: Page, targetImgX: number) {
  const box = await page.getByRole('slider').boundingBox()
  if (!box) throw new Error('找不到滑轨')
  const startX = box.x + 4 + 16 // 把手在行程起点时的中心：内边 4px + 半个把手
  const midY = box.y + box.height / 2
  const goalCss = targetImgX * (box.width / IMG_W)
  await page.mouse.move(startX, midY)
  await page.mouse.down()
  for (let i = 1; i <= 14; i++) {
    await page.mouse.move(startX + (goalCss * i) / 14, midY)
    // 每步留一点真实时长：后端要求轨迹总时长 ≥150ms，瞬移会被判成脚本
    await page.waitForTimeout(20)
  }
  await page.mouse.up()
}

/** 连错 N 次密码，把 mock 推到「登录必须带验证码」的状态（与后端同一阈值 3 次） */
async function forceCaptchaEnforced(page: Page, account: string, times: number) {
  for (let i = 0; i < times; i++) {
    await page.getByLabel('用户名').fill(account)
    await page.getByLabel('密码').fill('nope')
    await page.getByRole('button', { name: '登录' }).click()
    await expect(page.getByRole('slider')).toBeVisible()
    await page.waitForTimeout(400) // 失败后表单会清空并重取挑战，等它换完
  }
}

test('拖到孔位即通过，token 能被登录接口的验证码通道消费', async ({ page }) => {
  const account = await openLogin(page)
  await forceCaptchaEnforced(page, account, 3)

  // 此时 mock 已强制校验 captcha：拖对 → 登录必须真进去
  await dragTo(page, await holeOf(page))
  await expect(page.getByText('验证通过')).toBeVisible()

  await page.getByLabel('用户名').fill(account)
  await page.getByLabel('密码').fill('pw-for-e2e')
  await page.getByRole('button', { name: '登录' }).click()
  await expect(page).not.toHaveURL(/\/login/, { timeout: 20_000 })
})

test('拖偏了不通过，并换一张新挑战', async ({ page }) => {
  await openLogin(page)
  const before = await holeOf(page)
  await dragTo(page, before + 120) // 偏出一整块，远超容差

  await expect(page.getByText('滑块验证没通过')).toBeVisible()
  // 复位并换图：新孔位必须与旧的不同（同一张图重出会被当成「怎么拖都过」）。
  // 用会返回 null 的读取：expect.poll 的回调抛错就直接失败，不会等到超时。
  await expect
    .poll(
      async () => {
        const now = await readHole(page)
        return now !== null && now !== before ? 1 : 0
      },
      { timeout: 20_000 },
    )
    .toBe(1)
})

test('键盘也能完成校验（方向键按容差步进，回车提交）', async ({ page }) => {
  await openLogin(page)
  const hole = await holeOf(page)

  const track = page.getByRole('slider')
  await track.focus()
  const presses = Math.round(hole / 6) // 后端容差 ±6px，按 6px 一档推进必然落进容差内
  for (let i = 0; i < presses; i++) {
    await page.keyboard.press('ArrowRight')
    await page.waitForTimeout(8) // 让轨迹有真实时长（后端要求 ≥150ms）
  }
  await page.keyboard.press('Enter')
  await expect(page.getByText('验证通过')).toBeVisible()

  // 落点确实进了容差：拖到这一步还没有「没通过」的提示
  await expect(page.getByText('滑块验证没通过')).toHaveCount(0)
})
