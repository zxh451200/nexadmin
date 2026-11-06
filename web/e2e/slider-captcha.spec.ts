import { expect, test, type Page } from '@playwright/test'

/**
 * 滑块验证码的几何与接线回归。
 * 为什么值得钉住：孔位、滑块图落点、把手行程三处坐标分别按「后端图片像素」与「CSS 像素」
 * 两套口径算，任一处写错都不报错——只会画歪，或永远拖不到位。
 * 另外钉一条真实通道：滑块签发的 token 必须当作 captcha 交给 /auth/login 现成的校验口
 * （登录、短信、注册共用这一条），走偏一次就是「验证码错误或已过期」。
 *
 * 滑块交互由 rc-slider-captcha（MIT）承担，我们只对接后端契约。测试针对它的真实 DOM：
 * 容器 .rc-slider-captcha（内联 width = 背景图宽 = 280，显示与图片 1:1），
 * 拖动把手 .rc-slider-captcha-control-button。rc 会把按钮行程按比例映射成拼图行程
 * （ratio = 拼图可用行程 / 按钮可用行程），上报的 x 是拼图行程，所以按钮要走的鼠标
 * 距离 = 目标图片像素 / ratio。
 * 只在 mock 数据源上跑：孔位从假数据背景 SVG 里读出（与后端同一套尺寸常量）。
 * 这里量的是前端接线，后端判定（容差、轨迹像人、一次性消费、IP 绑定）由 core/auth 的用例钉住。
 * 断言只看 UI：mock 在 axios 适配器里就地应答，不产生网络事件，waitForResponse 永远等不到。
 *
 * 键盘路径已随 rc 迁移移除（CaptchaField.tsx 顶部注释：方向键兜底改为「改用图形验证码」，
 * 图形码可输入、有语音，无障碍语义不缺门），本文件不再有键盘用例。
 */

const SLIDER = '.rc-slider-captcha'
const SLIDER_BG_IMG = '.rc-slider-captcha-jigsaw-bg'
// 与后端 slider_render.go 的 sliderBgW 同口径（auth.mock.ts 出图也按这组常量）
const BG_W = 280

test.setTimeout(240_000)

/**
 * dev 冷启动要把几百个模块重新下一遍，首屏要等 2-4 秒，所以按导航重试一次以上。
 * （历史包袱：这里曾写着「/auth/methods 若在 mock 注册表就绪之前发出就会落到 FALLBACK」——
 * 那个诊断是错的。mock-loader 的 import.meta.glob 是 eager 的静态 import，注册表在 React 挂载前
 * 就填满了；静默降级的真因是登录页另一个匿名请求被 mock 当成需要鉴权的接口回 401，
 * 401 处理顺手 queryClient.clear()，把在途的 ['auth','methods'] 一起抹掉。已修，见 request/mock.ts 的 PUBLIC_PREFIXES。）
 * 全程留在默认的「账号登录」表单上：手机号 tab 在满载机器上会随查询落定反复重挂。
 */
async function openLogin(page: Page): Promise<string> {
  const account = 'slider' + Date.now().toString(36) // 每次跑用新账号：mock 的失败计数按用户名累计
  await page.addInitScript((name) => {
    localStorage.setItem('mock-initialized', 'true')
    localStorage.setItem('nex-lang', 'zh-CN')
    localStorage.setItem('mock-admin', JSON.stringify({ username: name, password: 'pw-for-e2e' }))
  }, account)
  const slider = page.locator(SLIDER)
  const toggle = page.getByRole('button', { name: '改用滑块验证' })
  // 就绪判据按「哪一个先出现」取，两种默认形态都成立：滑块已是默认时直接就绪，
  // 默认若翻回图形码则点一下切换入口。isVisible() 不等待，所以只能走 waitFor。
  const appeared = (loc: import('@playwright/test').Locator) => loc.waitFor({ state: 'visible', timeout: 60_000 }).then(() => true, () => false)
  for (let attempt = 0; attempt < 3; attempt++) {
    await page.goto('/admin/login', { waitUntil: 'domcontentloaded' })
    if (!(await Promise.race([appeared(slider), appeared(toggle)]))) continue
    // 到这里形态已经落定，用不等的 count() 判断入口在不在：滑块是默认时它永远不出现，
    // 再等一次 waitFor 只会白烧 60 秒超时。
    if ((await toggle.count()) > 0) await toggle.click()
    await expect(slider).toBeVisible()
    // 滑轨存在不等于挑战已取到：rc 的 request 在 captcha 未到位时会挂起，jigsaw 一直转圈。
    // 等背景图的 mock SVG data URI 真正挂上 src，后面读孔位才不会扑空。
    // 选择器内联写：waitForFunction 的谓词在浏览器上下文里执行，引用不到外层常量。
    await page.waitForFunction(
      () => {
        const img = document.querySelector<HTMLImageElement>('.rc-slider-captcha-jigsaw-bg')
        return !!img && img.src.startsWith('data:image/svg+xml;base64,')
      },
      undefined,
      { timeout: 60_000 },
    )
    return account
  }
  const where = await page
    .evaluate(() => document.body.innerText.replace(/\s+/g, ' ').slice(0, 200))
    .catch(() => '(读不到正文)')
  throw new Error(`登录页始终没出现验证码区：${where}`)
}

/** 从 mock 背景图（SVG data URI）里读孔位横坐标；图还没换好时返回 null 而不是抛错 */
async function readHole(page: Page): Promise<number | null> {
  return page.evaluate((sel) => {
    const img = document.querySelector<HTMLImageElement>(sel)
    const src = img?.src ?? ''
    if (!src.startsWith('data:image/svg+xml;base64,')) return null
    const svg = atob(src.split(',')[1])
    const m = svg.match(/<rect x="(\d+)" y="(\d+)" width="52" height="52" fill="#111827"/)
    return m ? Number(m[1]) : null
  }, SLIDER_BG_IMG)
}

async function holeOf(page: Page): Promise<number> {
  const x = await readHole(page)
  if (x !== null) return x
  const srcs = await page.evaluate(() => [...document.querySelectorAll('img')].map((i) => i.src.slice(0, 40)).join(' | '))
  throw new Error(`读不到 mock 背景里的孔位。页面上的图是：${srcs || '(没有 img)'}`)
}

/**
 * 按住 rc 把手拖到目标图片像素横坐标。
 * 按钮行程与拼图行程不是 1:1：rc 按 ratio = puzzleMax / buttonMax 换算，
 * 其中 puzzleMax = bgW - puzzleW、buttonMax = bgW - buttonW - 2（2 是滑轨指示条左右边框）。
 * 尺寸全部从真实 DOM 量，避免把 280/40/63 抄成第二套常量。
 */
async function dragTo(page: Page, targetImgX: number) {
  const button = page.locator('.rc-slider-captcha-control-button')
  const btn = await button.boundingBox()
  const panel = await page.locator(SLIDER).boundingBox()
  const piece = await page.locator('.rc-slider-captcha-jigsaw-puzzle').boundingBox()
  if (!btn || !panel || !piece) throw new Error('找不到滑块把手或拼图')
  const buttonMax = panel.width - btn.width - 2
  const puzzleMax = panel.width - piece.width
  // 后端要的是拼图行程；换算成鼠标（按钮）行程。
  const mouseDelta = (targetImgX * buttonMax) / puzzleMax
  const startX = btn.x + btn.width / 2
  const startY = btn.y + btn.height / 2
  await page.mouse.move(startX, startY)
  await page.mouse.down()
  for (let i = 1; i <= 14; i++) {
    await page.mouse.move(startX + (mouseDelta * i) / 14, startY)
    // 每步留一点真实时长：rc 的 trail 没有时间戳，接线层按总时长线性铺开，
    // 后端要求轨迹总时长 ≥150ms，瞬移仍会被判成脚本
    await page.waitForTimeout(20)
  }
  await page.mouse.up()
}

/** 连错 N 次密码，把 mock 推到「登录必须带验证码」的状态（与后端同一阈值 3 次） */
async function forceCaptchaEnforced(page: Page, account: string, times: number) {
  for (let i = 0; i < times; i++) {
    await page.getByRole('textbox', { name: '用户名' }).fill(account)
    await page.getByRole('textbox', { name: '密码' }).fill('nope')
    await page.getByRole('button', { name: '登录', exact: true }).click()
    await expect(page.locator(SLIDER)).toBeVisible()
    await page.waitForTimeout(400) // 失败后表单会清空并重取挑战，等它换完
  }
}

/** 读 mock 记下的最后一次 /auth/slider/verify 交卷体（mock 不留网络事件，只能这么取证） */
async function lastVerify(page: Page): Promise<{ x: number; trail: { t: number; x: number }[] } | null> {
  return page.evaluate(
    () =>
      (window as unknown as { __nexSliderVerify?: { x: number; trail: { t: number; x: number }[] } })
        .__nexSliderVerify ?? null,
  )
}

test('拖到孔位即通过，token 能被登录接口的验证码通道消费', async ({ page }) => {
  const account = await openLogin(page)
  await forceCaptchaEnforced(page, account, 3)

  // 此时 mock 已强制校验 captcha：拖对 → 登录必须真进去
  await dragTo(page, await holeOf(page))
  await expect(page.getByText('验证通过')).toBeVisible()

  // 交卷体的坐标系必须是「拼图行程」：起点归零、推进量不超过落点、末点等于 x、时间戳单调。
  // 直接抄 rc 的绝对 clientX（迁移初期的实际形态，横坐标 700+）或漏掉它的 ratio 缩放，都会在这里红；
  // 用例的拖动不回摆，所以「不超过落点」这条在真人过冲时才有余地，这里按脚本口径钉死。
  const sent = await lastVerify(page)
  if (!sent) throw new Error('mock 没收到 /auth/slider/verify 交卷')
  const xs = sent.trail.map((p) => p.x)
  expect(xs[0]).toBe(0)
  expect(Math.max(...xs)).toBeLessThanOrEqual(sent.x + 1)
  expect(Math.max(...xs)).toBeLessThanOrEqual(BG_W)
  expect(xs[xs.length - 1]).toBe(sent.x)
  for (let i = 1; i < sent.trail.length; i++) expect(sent.trail[i].t).toBeGreaterThanOrEqual(sent.trail[i - 1].t)

  await page.getByRole('textbox', { name: '用户名' }).fill(account)
  await page.getByRole('textbox', { name: '密码' }).fill('pw-for-e2e')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).not.toHaveURL(/\/login/, { timeout: 20_000 })
})

test('拖偏了不通过，并换一张新挑战', async ({ page }) => {
  await openLogin(page)
  const before = await holeOf(page)
  await dragTo(page, before + 120) // 偏出一整块，远超容差

  // 失败提示（toast 几秒后会自动消失，偶发被断言窗口错过）与「换新挑战」是同一件事的两面：
  // 校验失败 → onFailed → 取新挑战。两者任一出现即算通过，不再让 toast 的存续期变成竞态。
  // 用会返回 null 的读取：expect.poll 的回调抛错就直接失败，不会等到超时。
  await expect
    .poll(
      async () => {
        const now = await readHole(page)
        if (now !== null && now !== before) return 'changed'
        return (await page.getByText('滑块验证没通过').count()) > 0 ? 'toast' : 'no'
      },
      { timeout: 20_000 },
    )
    .not.toBe('no')
})
