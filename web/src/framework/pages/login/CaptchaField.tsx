import { useCallback, useEffect, useRef, useState } from 'react'
import { RefreshCw, Volume2 } from 'lucide-react'
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

/** 人机校验的两种形态：滑块拼图（默认）与图形数字（带语音，作无障碍兜底） */
export type CaptchaMode = 'image' | 'slider'

/**
 * enabled=false 时不请求图片；变为 true（如登录连续失败后）再拉取。
 * mode 切到 slider 时自动换发滑块挑战。返回的 captcha.key 始终是“当前形态”的 key——
 * 调用方提交时读 captcha?.key 就行；否则滑块 token 会配着图形验证码的 key 提交，
 * 登录必然报“验证码错误或已过期”。
 */
export function useCaptcha(enabled = true) {
  // 滑块优先，图形码退成一键兜底：读数字打字在手机上最费事，而滑块这里方向键也能走完
  const [mode, setMode] = useState<CaptchaMode>('slider')
  const [captcha, setCaptcha] = useState<CaptchaResult | null>(null)
  // 请求序号：切形态、点刷新都会让上一发请求还在路上。没有这道守卫，先发的图形请求
  // 后到达就会把滑块挑战覆盖掉——表现是滑轨里挂着数字验证码，永远拖不到位。
  const seq = useRef(0)
  const refresh = useCallback(async () => {
    const mine = ++seq.current
    setCaptcha(null)
    try {
      const data = await api.get<CaptchaResult>(mode === 'slider' ? '/auth/slider' : '/auth/captcha', undefined, { silent: true })
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
  /** 服务端要求验证码时显示必填星号 */
  required?: boolean
}

export function CaptchaField({ form, captcha, onRefresh, mode, onModeChange, required }: CaptchaFieldProps) {
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
          <button
            type="button"
            className="self-start text-xs text-muted-foreground underline-offset-2 hover:underline"
            onClick={() => switchMode(mode === 'slider' ? 'image' : 'slider')}
          >
            {mode === 'slider' ? t('login.captchaModeImage') : t('login.captchaModeSlider')}
          </button>
          <FormMessage />
        </FormItem>
      )}
    />
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
 * 组件给的 x 不用换算就是后端要的坐标；puzzleSize 的 top 用后端的 pieceTop 定孔位行。
 * 无键盘操作路径：方向键可完成的兜底是「改用图形验证码」（可输入、有语音），语义上不缺门。
 */
function SliderCaptcha({ captcha, onRefresh, onToken, onFailed }: SliderCaptchaProps) {
  const { t } = useTranslation()
  const imgW = captcha?.width ?? 280
  const imgH = captcha?.height ?? 110

  const verify = async (data: VerifyParam) => {
    if (!captcha?.key) throw new Error('captcha missing')
    // rc 的 trail 是 [x,y][] 无时间戳，而且 x 记的是绝对 clientX；后端要的坐标系是「拼图行程」
    // （落点 x、容差、推进量同一口径），另外要单调毫秒时间戳与「末点 = 落点」。
    // 这里把轨迹平移到 0 起点并按落点缩放，推进量量的就是真实拖动，而不是页面横坐标的跨度。
    // 时间戳按总时长线性铺开：真人形状仍是 rc 记录的原始轨迹，瞬间位移依旧会被时长下限（150ms）挡住。
    const pts = data.trail.length ? data.trail : [[0, 0]]
    const n = pts.length
    const x0 = pts[0][0]
    const span = pts[n - 1][0] - x0
    const scale = span === 0 ? 0 : data.x / span
    const trail = pts.map((p, i) => ({ t: Math.round((i / Math.max(1, n - 1)) * data.duration), x: Math.round((p[0] - x0) * scale) }))
    trail[n - 1] = { t: Math.max(1, data.duration), x: Math.round(data.x) }
    try {
      const { token } = await api.post<{ token: string }>(
        '/auth/slider/verify',
        { key: captcha.key, x: Math.round(data.x), trail },
        { silent: true },
      )
      onToken(token)
    } catch (e) {
      // 失败就地换血：清掉未定 token、父级取新挑战（key 变化 → 组件重挂）；
      // 抛出让组件走完它的失败动画再消失。
      toast.error(t('login.sliderFailed'))
      onToken('')
      onFailed()
      throw e
    }
  }

  return (
    <div
      className="w-full max-w-[280px]"
      // 组件用 CSS 变量做主题；映射到站点的语义 token，跟随浅色/深色主题
      style={{ '--rcsc-primary': 'hsl(var(--primary))', '--rcsc-panel-border-radius': '8px' } as React.CSSProperties}
    >
      {/* key 绑挑战 key：换一张挑战就重挂组件，拖拽状态与错误计数一并复位 */}
      <div className="relative">
        <SliderCaptchaLib
        key={captcha?.key ?? 'empty'}
        autoRefreshOnError={false}
        showRefreshIcon={false}
        bgSize={{ width: imgW, height: imgH }}
        puzzleSize={
          captcha
            ? { width: captcha.pieceWidth ?? 68, top: captcha.pieceTop ?? 0, left: 0 }
            : { width: 68, top: 0, left: 0 }
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
        />
        {/* 换一张挑战：组件内置刷新只会重放当前 request（拿不到新挑战），所以走父级重新取挑战 */}
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
      </div>
    </div>
  )
}
