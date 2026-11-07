import { useCallback, useEffect, useRef, useState } from 'react'
import { Check, RefreshCw, Volume2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { UseFormReturn } from 'react-hook-form'
import { toast } from 'sonner'
import SliderCaptchaLib, { type VerifyParam } from 'rc-slider-captcha'
import { api } from '@/framework/request/http'
import type { CaptchaResult } from '@/framework/auth/types'
import { RequiredMark } from '@/framework/components/RequiredMark'
import { Button } from '@/framework/ui/button'
import { FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import { Skeleton } from '@/framework/ui/skeleton'
import { buildTrail, type DragSample } from './sliderTrail'

/** 人机校验的三种形态：滑块拼图（默认）、点选（高风险升级档）与图形数字（带语音，作无障碍兜底） */
export type CaptchaMode = 'image' | 'slider' | 'click'

/**
 * enabled=false 时不请求图片；变为 true（如登录连续失败后）再拉取。
 * baseMode 是服务端指派的形态（滑块 / 升级后的点选），mode 跟随它——用户唯一的
 * 切换出口是图形码（无障碍兜底），不能自行降级回滑块，否则升级档形同虚设
 * （专攻滑块的自动化工具换个点击就绕过了）。mode 变化自动换发对应形态的挑战。
 * 返回的 captcha.key 始终是“当前形态”的 key——调用方提交时读 captcha?.key 就行。
 */
export function useCaptcha(enabled = true, baseMode: CaptchaMode = 'slider') {
  const [mode, setMode] = useState<CaptchaMode>(baseMode)
  const [captcha, setCaptcha] = useState<CaptchaResult | null>(null)
  // 服务端指派变化（升级/回落）时跟随；用户切走的图形码也被指派收回
  useEffect(() => setMode(baseMode), [baseMode])
  // 请求序号：切形态、点刷新都会让上一发请求还在路上。没有这道守卫，先发的图形请求
  // 后到达就会把滑块挑战覆盖掉——表现是滑轨里挂着数字验证码，永远拖不到位。
  const seq = useRef(0)
  const refresh = useCallback(async () => {
    const mine = ++seq.current
    setCaptcha(null)
    try {
      const url = mode === 'slider' ? '/auth/slider' : mode === 'click' ? '/auth/click' : '/auth/captcha'
      const data = await api.get<CaptchaResult>(url, undefined, { silent: true })
      if (mine === seq.current) setCaptcha({ ...data, kind: mode })
    } catch {
      if (mine === seq.current) setCaptcha(null)
    }
  }, [mode])
  useEffect(() => {
    if (enabled) void refresh()
  }, [enabled, refresh])
  return { captcha, refresh, mode, setMode }
}

interface CaptchaFieldProps {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  form: UseFormReturn<any>
  captcha: CaptchaResult | null
  onRefresh: () => void
  mode: CaptchaMode
  onModeChange: (mode: CaptchaMode) => void
  /** 服务端指派的形态（滑块/点选）：图形码兜底的「返回」回到它 */
  baseMode?: CaptchaMode
  /** 服务端要求验证码时显示必填星号 */
  required?: boolean
}

export function CaptchaField({ form, captcha, onRefresh, mode, onModeChange, baseMode = 'slider', required }: CaptchaFieldProps) {
  const { t, i18n } = useTranslation()
  const [audioBusy, setAudioBusy] = useState(false)

  // 语音验证码：读的是同一张图的音频，不消费验证码；听完仍然要手动输入
  const playAudio = async () => {
    if (!captcha?.key) return
    setAudioBusy(true)
    try {
      const { audio } = await api.get<{ audio: string }>(`/auth/captcha/${captcha.key}/audio`, { lang: i18n.language }, { silent: true })
      await new Audio(audio).play()
    } catch {
      toast.error(t('login.captchaAudioFailed'))
    } finally {
      setAudioBusy(false)
    }
  }

  const switchMode = (next: CaptchaMode) => {
    if (next === mode) return
    form.setValue('captcha', '')
    onModeChange(next) // 形态变化会触发重新取挑战
  }

  return (
    <FormField
      control={form.control}
      name="captcha"
      render={({ field }) => (
        <FormItem>
          <FormLabel>
            {t('login.captcha')}
            {required ? <RequiredMark /> : null}
          </FormLabel>
          <div aria-live="polite" className="sr-only">
            {captcha ? t('login.captchaRefreshed') : t('login.captchaLoading')}
          </div>
          {mode === 'slider' ? (
            <SliderCaptcha
              captcha={captcha}
              onRefresh={onRefresh}
              onToken={(token) => field.onChange(token)}
              onFailed={() => {
                field.onChange('')
                onRefresh()
              }}
            />
          ) : mode === 'click' ? (
            <ClickCaptcha
              captcha={captcha}
              onRefresh={onRefresh}
              onToken={(token) => field.onChange(token)}
              onFailed={() => {
                field.onChange('')
                onRefresh()
              }}
            />
          ) : (
            <div className="flex items-center gap-2">
              <FormControl>
                {/* 位数可在参数设置里调到 8，输入框上限放宽到 8 跟随后端口径 */}
                <Input {...field} autoComplete="off" maxLength={8} placeholder={t('login.captchaPlaceholder')} />
              </FormControl>
              {/* 尺寸取后端出图的显示口径（224x72 出图 = 112x36 的 2 倍），并与其他输入框同高：
                  盒子给到 h-14 既会把图放大到发糊，也会让这一行比整张表单高出一截。 */}
              <div className="flex shrink-0 items-center gap-1">
                <button
                  type="button"
                  onClick={onRefresh}
                  aria-label={t('login.refreshCaptcha')}
                  className="group relative flex h-10 w-[124px] shrink-0 items-center justify-center overflow-hidden rounded-md border border-border bg-muted md:h-9 md:w-28"
                >
                  {captcha ? (
                    <img src={captcha.image} alt={t('login.captcha')} className="h-full w-full object-contain" />
                  ) : (
                    <Skeleton className="h-full w-full" />
                  )}
                  <span className="absolute inset-0 flex items-center justify-center bg-background/60 opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100">
                    <RefreshCw className="size-4" aria-hidden="true" />
                  </span>
                </button>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="size-10 md:size-9"
                  aria-label={t('login.captchaAudio')}
                  aria-busy={audioBusy}
                  disabled={!captcha || audioBusy}
                  onClick={() => void playAudio()}
                >
                  <Volume2 aria-hidden="true" />
                </Button>
              </div>
            </div>
          )}
          {/* 形态切换只有一条路：交互形态 ↔ 图形码兜底。指派形态之间不可互切——
              升级档（点选）的意义是筛掉专攻滑块的自动化，用户能自由降级就白升了。 */}
          <button
            type="button"
            className="self-start text-xs text-muted-foreground underline-offset-2 hover:underline"
            onClick={() => switchMode(mode === 'image' ? baseMode : 'image')}
          >
            {mode === 'image' ? (baseMode === 'click' ? t('login.captchaModeClick') : t('login.captchaModeSlider')) : t('login.captchaModeImage')}
          </button>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

/**
 * 窄屏等比缩放容器：外层按容器宽定 scale，内层按 naturalW 布局再 transform 缩放。
 * 不用 maxWidth 收缩，是因为 fieldset 的 min-width:min-content 会被固定宽的根元素
 * 撑破（页面横向溢出），而 maxWidth 压缩是非等比的（背景压扁、控件错位）；
 * transform 全程等比，内部用 clientWidth 做测量也不受影响。naturalH 由内容实测
 * （transform 不占布局，外层要手动撑高，否则缩放后底部留一截空白）。
 */
function ScaledBox({ naturalW, children }: { naturalW: number; children: React.ReactNode }) {
  const wrapRef = useRef<HTMLDivElement>(null)
  const innerRef = useRef<HTMLDivElement>(null)
  const [scale, setScale] = useState(1)
  const [naturalH, setNaturalH] = useState(0)
  useEffect(() => {
    const wrap = wrapRef.current
    const inner = innerRef.current
    if (!wrap || !inner) return
    const update = () => {
      setScale(Math.min(1, wrap.clientWidth / naturalW))
      setNaturalH(inner.offsetHeight)
    }
    update()
    const ro = new ResizeObserver(update)
    ro.observe(wrap)
    ro.observe(inner)
    return () => ro.disconnect()
  }, [naturalW])
  return (
    <div ref={wrapRef} className="w-full" style={{ height: naturalH ? Math.round(naturalH * scale) : undefined }}>
      <div ref={innerRef} style={{ width: naturalW, transform: `scale(${scale})`, transformOrigin: 'top left' }}>
        {children}
      </div>
    </div>
  )
}

/** 验证通过后的收起态：一行 ✓ 徽标 + 「重新验证」（换新挑战，重挂回交互态）。 */
function CaptchaDoneChip({ onRedo }: { onRedo: () => void }) {
  const { t } = useTranslation()
  return (
    <div className="flex h-10 items-center gap-2 rounded-md border border-border bg-muted/40 px-3 text-sm">
      <span className="flex size-5 shrink-0 items-center justify-center rounded-full bg-primary text-primary-foreground">
        <Check className="size-3.5" aria-hidden="true" />
      </span>
      <span>{t('login.sliderDone')}</span>
      <button
        type="button"
        onClick={onRedo}
        className="ms-auto shrink-0 text-xs text-muted-foreground underline-offset-2 hover:underline"
      >
        {t('login.sliderRedo')}
      </button>
    </div>
  )
}

interface SliderCaptchaProps {
  captcha: CaptchaResult | null
  onRefresh: () => void
  onToken: (token: string) => void
  onFailed: () => void
}

/**
 * 滑块拼图：开源组件 rc-slider-captcha（MIT）负责拖拽交互、动画与状态机，
 * 我们只对接后端契约——`/auth/slider` 发挑战（背景 + 抠块），`/auth/slider/verify`
 * 按「图片像素 x + 轨迹」判真伪。bgSize 传图片原始尺寸，让显示与图片 1:1，
 * 组件给的 x 不用换算就是后端要的坐标；puzzleSize 的 top 用后端的 pieceTop 定孔位行，
 * height 也必须显式传：库的样式表把拼图写成 height:100%，少了这一项方片会被拉变形。
 *
 * 内嵌面板（embed）：面板与拖轨都在表单流内，不遮任何字段——这是极验内嵌模式与国内
 * 后台登录页的标配。库在 embed 下成功后不会收起（float 模式才会，但浮层会盖住表单
 * 字段，登录页不能用），所以「通过后收起成一行徽标」由我们自己实现。
 *
 * 库的 bundle 里没有任何 aria/role/tabIndex，也接不了键盘：所以外层只补「组 + 状态播报」
 * 这一层诚实语义，不挂 role="slider"——那会向读屏承诺一个操作不了的控件。
 * 键盘与语音的出路是「改用图形验证码」，那条路可输入、有语音，语义上不缺门。
 */
function SliderCaptcha({ captcha, onRefresh, onToken, onFailed }: SliderCaptchaProps) {
  const { t } = useTranslation()
  const imgW = captcha?.width ?? 384
  const imgH = captcha?.height ?? 150
  const rootRef = useRef<HTMLDivElement>(null)
  const samplesRef = useRef<DragSample[]>([])
  const startRef = useRef(0)
  const [phase, setPhase] = useState<'idle' | 'moving' | 'verifying' | 'done' | 'failed'>('idle')

  // 自己采一遍真实时序：库只给 [clientX, clientY][]，时间轴得由我们这边拿
  useEffect(() => {
    const inWidget = (e: PointerEvent) => rootRef.current?.contains(e.target as Node) ?? false
    const down = (e: PointerEvent) => {
      if (!inWidget(e)) return
      startRef.current = e.timeStamp
      samplesRef.current = [{ t: 0, cx: e.clientX }]
      setPhase('moving')
    }
    const move = (e: PointerEvent) => {
      const samples = samplesRef.current
      if (!samples.length) return
      const tMs = Math.round(e.timeStamp - startRef.current)
      const last = samples[samples.length - 1]
      if (last.cx === e.clientX && tMs - last.t < 16) return
      samples.push({ t: tMs, cx: e.clientX })
    }
    // pointerup 不清空：库的 onVerify 在我们之后才跑，清了就没样本可用；下次 pointerdown 自然复位
    window.addEventListener('pointerdown', down, true)
    window.addEventListener('pointermove', move, true)
    return () => {
      window.removeEventListener('pointerdown', down, true)
      window.removeEventListener('pointermove', move, true)
    }
  }, [])

  // 换一张挑战就回到待拖动，否则播报会停在上一张的结果上
  useEffect(() => setPhase('idle'), [captcha?.key])

  const verify = async (data: VerifyParam) => {
    if (!captcha?.key) throw new Error('captcha missing')
    // data.x 本来就是图片像素口径：rc 的内部比例全部由 bgSize/puzzleSize 这些 prop 计算
    // （clientWidth 不受 transform 缩放影响），手机端外层的等比 transform 只缩显示、
    // 不缩坐标——视觉对齐孔位时 data.x 恰好等于孔位图片坐标，无需换算。
    const x = Math.round(data.x)
    const trail = buildTrail(samplesRef.current, data.trail, x, data.duration)
    setPhase('verifying')
    try {
      const { token } = await api.post<{ token: string }>(
        '/auth/slider/verify',
        { key: captcha.key, x, trail },
        { silent: true },
      )
      setPhase('done')
      onToken(token)
    } catch (e) {
      // 失败就地换血：清掉未定 token、父级取新挑战（key 变化 → 组件重挂）；
      // 抛出让组件走完它的失败动画再消失。
      setPhase('failed')
      toast.error(t('login.sliderFailed'))
      onToken('')
      onFailed()
      throw e
    }
  }

  // 播报用独立文案：库的视觉提示（.rc-slider-captcha-control-tips）本身就是可读文本，
  // 这里再抄一遍会让同一句话在读屏与 getByText 里出现两次。
  const status =
    phase === 'moving'
      ? t('login.sliderAriaMoving')
      : phase === 'verifying'
        ? t('login.sliderAriaVerifying')
        : phase === 'done'
          ? t('login.sliderAriaDone')
          : phase === 'failed'
            ? t('login.sliderAriaFailed')
            : ''

  return (
    <div
      ref={rootRef}
      role="group"
      aria-label={t('login.sliderPrompt')}
      aria-describedby="slider-captcha-hint"
      className="w-full"
    >
      <span id="slider-captcha-hint" className="sr-only">
        {t('login.sliderAriaHint')}
      </span>
      <span role="status" aria-live="polite" className="sr-only">
        {status}
      </span>
      {phase === 'done' ? (
        <CaptchaDoneChip
          onRedo={() => {
            onToken('')
            onRefresh()
          }}
        />
      ) : (
        /* key 绑挑战 key：换一张挑战就重挂组件，拖拽状态与错误计数一并复位。
           刷新按钮放库的 jigsawContent 插槽（面板右上角）：组件内置刷新只会重放当前
           request（拿不到新挑战），所以走父级重新取挑战。 */
        <ScaledBox naturalW={imgW}>
          <div style={{ width: imgW }}>
            <SliderCaptchaLib
              key={captcha?.key ?? 'empty'}
              /* 组件用 CSS 变量做主题，按库的设计挂在根元素：映射到站点的语义 token，
                 跟随浅色/深色主题。 */
              style={
                {
                  '--rcsc-primary': 'hsl(var(--primary))',
                  '--rcsc-panel-border-radius': '8px',
                } as React.CSSProperties
              }
              autoRefreshOnError={false}
              showRefreshIcon={false}
              bgSize={{ width: imgW, height: imgH }}
              puzzleSize={
                captcha
                  ? { width: captcha.pieceWidth ?? 63, height: captcha.pieceHeight ?? 63, top: captcha.pieceTop ?? 0, left: 0 }
                  : { width: 63, height: 63, top: 0, left: 0 }
              }
              request={async () => {
                if (!captcha) return new Promise<never>(() => {}) // 挑战未到位先挂起；key 变化会重挂组件
                return { bgUrl: captcha.image, puzzleUrl: captcha.piece ?? '' }
              }}
              onVerify={verify}
              tipText={{
                default: t('login.sliderPrompt'),
                loading: t('common.loading'),
                moving: t('login.sliderPrompt'),
                verifying: t('common.loading'),
                success: t('login.sliderDone'),
                error: t('login.sliderFailed'),
              }}
              jigsawContent={
                captcha ? (
                  <button
                    type="button"
                    onClick={onRefresh}
                    aria-label={t('login.refreshCaptcha')}
                    className="absolute end-1 top-1 z-10 flex size-7 items-center justify-center rounded-md bg-background/70 text-foreground transition-colors hover:bg-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  >
                    <RefreshCw className="size-3.5" aria-hidden="true" />
                  </button>
                ) : undefined
              }
            />
          </div>
        </ScaledBox>
      )}
    </div>
  )
}

interface ClickCaptchaProps {
  captcha: CaptchaResult | null
  onRefresh: () => void
  onToken: (token: string) => void
  onFailed: () => void
}

/** 点选可点的最大次数：与后端 click builder 的 WithRangeVerifyLen 上限（3）同口径 */
const CLICK_MAX_POINTS = 3

/**
 * 点选验证码（升级档）：后端 `/auth/click` 发主图（混入干扰字）+ 候选字条（要求按
 * 顺序点选的字），点位只存服务端。用户按候选字条的顺序点主图上的字——带序号标记、
 * 可撤销，点完按「确认」提交 `/auth/click/verify`，通过后同样收起成徽标。
 *
 * 无障碍：点击交互没有键盘路径（与滑块同样的诚实语义——不承诺做不到的事），
 * 出路是「改用图形验证码」；计数用 aria-live 播报。
 */
function ClickCaptcha({ captcha, onRefresh, onToken, onFailed }: ClickCaptchaProps) {
  const { t } = useTranslation()
  const imgW = captcha?.width ?? 384
  const imgH = captcha?.height ?? 170
  const imgRef = useRef<HTMLDivElement>(null)
  const [points, setPoints] = useState<{ x: number; y: number }[]>([])
  const [phase, setPhase] = useState<'idle' | 'verifying' | 'done' | 'failed'>('idle')

  // 换一张挑战就回到待点击
  useEffect(() => {
    setPoints([])
    setPhase('idle')
  }, [captcha?.key])

  const addPoint = (e: React.MouseEvent<HTMLDivElement>) => {
    if (phase !== 'idle' || points.length >= CLICK_MAX_POINTS) return
    // getBoundingClientRect 含 transform：按显示尺寸换算回图片像素口径
    const rect = e.currentTarget.getBoundingClientRect()
    if (!rect.width || !rect.height) return
    const x = Math.round(((e.clientX - rect.left) / rect.width) * imgW)
    const y = Math.round(((e.clientY - rect.top) / rect.height) * imgH)
    setPoints([...points, { x, y }])
  }

  const verify = async () => {
    if (!captcha?.key || phase !== 'idle' || !points.length) return
    setPhase('verifying')
    try {
      const { token } = await api.post<{ token: string }>(
        '/auth/click/verify',
        { key: captcha.key, dots: points },
        { silent: true },
      )
      setPhase('done')
      onToken(token)
    } catch (e) {
      setPhase('failed')
      toast.error(t('login.clickFailed'))
      onToken('')
      onFailed()
      throw e
    }
  }

  const status =
    phase === 'verifying'
      ? t('common.loading')
      : phase === 'done'
        ? t('login.sliderDone')
        : phase === 'failed'
          ? t('login.clickFailed')
          : t('login.clickCount', { count: points.length })

  if (phase === 'done') {
    return (
      <CaptchaDoneChip
        onRedo={() => {
          onToken('')
          onRefresh()
        }}
      />
    )
  }

  return (
    <div role="group" aria-label={t('login.clickPrompt')} aria-describedby="click-captcha-hint" className="w-full">
      <span id="click-captcha-hint" className="sr-only">
        {t('login.clickAriaHint')}
      </span>
      <span role="status" aria-live="polite" className="sr-only">
        {status}
      </span>
      <ScaledBox naturalW={imgW}>
        <div style={{ width: imgW }}>
          <div
            ref={imgRef}
            data-testid="click-captcha-image"
            onClick={addPoint}
            className="relative w-full cursor-pointer select-none overflow-hidden rounded-lg border border-border"
          >
            <img src={captcha?.image} alt={t('login.clickPrompt')} className="block w-full" draggable={false} />
            {/* 换一张：与滑块/图形码同一颗刷新钮（右上角）；key 变化会清掉已点标记 */}
            {captcha ? (
              <button
                type="button"
                onClick={onRefresh}
                aria-label={t('login.refreshCaptcha')}
                className="absolute end-1 top-1 z-10 flex size-7 items-center justify-center rounded-md bg-background/70 text-foreground transition-colors hover:bg-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <RefreshCw className="size-3.5" aria-hidden="true" />
              </button>
            ) : null}
            {/* 序号标记用百分比定位：随图片等比缩放，手机端不用另算 */}
            {points.map((p, i) => (
              <span
                key={`${p.x}-${p.y}-${i}`}
                aria-hidden="true"
                className="absolute flex size-6 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-full bg-primary text-xs font-semibold text-primary-foreground shadow"
                style={{ left: `${(p.x / imgW) * 100}%`, top: `${(p.y / imgH) * 100}%` }}
              >
                {i + 1}
              </span>
            ))}
          </div>
          <div className="mt-2 flex items-center gap-2">
            <img
              src={captcha?.thumb}
              alt=""
              aria-hidden="true"
              data-testid="click-captcha-thumb"
              className="h-10 shrink-0 rounded-md border border-border bg-background"
              style={{ width: captcha?.thumbWidth ?? 150 }}
            />
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={!points.length || phase !== 'idle'}
              onClick={() => setPoints(points.slice(0, -1))}
            >
              {t('login.clickUndo')}
            </Button>
            <Button
              type="button"
              size="sm"
              disabled={!points.length || phase !== 'idle'}
              onClick={() => void verify()}
            >
              {t('login.clickConfirm')}
            </Button>
          </div>
        </div>
      </ScaledBox>
    </div>
  )
}
