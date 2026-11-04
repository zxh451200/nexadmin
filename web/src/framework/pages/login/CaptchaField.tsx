import { useCallback, useEffect, useRef, useState } from 'react'
import { Check, GripVertical, RefreshCw, Volume2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { UseFormReturn } from 'react-hook-form'
import { toast } from 'sonner'
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
                <Input {...field} autoComplete="off" maxLength={6} placeholder={t('login.captchaPlaceholder')} />
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

/** 轨迹采样上限：超过就隔点丢弃，免得长距离慢拖把请求体撑大 */
const TRAIL_MAX = 240
/** 轨道把手边长（CSS 像素），用于把手在轨道内的行程换算 */
const KNOB = 32

interface SliderCaptchaProps {
  captcha: CaptchaResult | null
  onRefresh: () => void
  onToken: (token: string) => void
  onFailed: () => void
}

/**
 * 滑块拼图：把滑块图拖到背景的孔位上。坐标一律换算成“图片像素”——后端容差按图片像素判定，
 * 而图片在页面上是等比缩放的，直接用 CSS 像素会随容器宽度漂掉。
 */
function SliderCaptcha({ captcha, onRefresh, onToken, onFailed }: SliderCaptchaProps) {
  const { t } = useTranslation()
  const trackRef = useRef<HTMLDivElement>(null)
  const dragRef = useRef<{ pointerX: number; startTime: number; startX: number; trackW: number } | null>(null)
  const trailRef = useRef<{ t: number; x: number }[]>([])
  const [x, setX] = useState(0)
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState(false)

  const imgW = captcha?.width ?? 280
  const imgH = captcha?.height ?? 120
  const pieceW = captcha?.pieceWidth ?? 68
  const pieceH = captcha?.pieceHeight ?? 68
  const pieceTop = captcha?.pieceTop ?? 0
  const maxX = Math.max(1, imgW - pieceW)
  const frac = Math.min(1, Math.max(0, x / maxX))

  // 换一张挑战（或首次加载）就复位，否则残留落点会与新孔位错位。
  useEffect(() => {
    setX(0)
    setBusy(false)
    setDone(false)
    trailRef.current = []
  }, [captcha?.key])

  const pushTrail = (now: number, pos: number) => {
    const el = trailRef.current
    const last = el[el.length - 1]
    if (last && now - last.t < 16 && last.x === pos) return
    el.push({ t: now, x: pos })
    if (el.length > TRAIL_MAX) trailRef.current = el.filter((_, i) => i % 2 === 0 || i === el.length - 1)
  }

  const verify = async (pos: number) => {
    if (!captcha?.key || busy || done) return
    const trail = trailRef.current
    if (trail.length < 2) return
    setBusy(true)
    try {
      const { token } = await api.post<{ token: string }>(
        '/auth/slider/verify',
        { key: captcha.key, x: Math.round(pos), trail: trail.map((p) => ({ t: Math.round(p.t), x: Math.round(p.x) })) },
        { silent: true },
      )
      setDone(true)
      onToken(token)
    } catch {
      setBusy(false)
      toast.error(t('login.sliderFailed'))
      onFailed()
    }
  }

  const onPointerDown = (e: React.PointerEvent<HTMLDivElement>) => {
    if (!captcha || done || busy) return
    dragRef.current = {
      pointerX: e.clientX,
      startX: x,
      startTime: performance.now(),
      trackW: trackRef.current?.getBoundingClientRect().width || imgW,
    }
    trailRef.current = [{ t: 0, x }]
    e.currentTarget.setPointerCapture(e.pointerId)
  }

  const onPointerMove = (e: React.PointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current
    if (!drag || done || busy) return
    // CSS 位移 ×（图片宽 / 轨道宽）= 图片像素位移；轨道与背景等宽，所以同一个比例。
    const next = drag.startX + (e.clientX - drag.pointerX) * (imgW / drag.trackW)
    const pos = Math.min(maxX, Math.max(0, next))
    setX(pos)
    pushTrail(performance.now() - drag.startTime, pos)
  }

  const onPointerUp = (e: React.PointerEvent<HTMLDivElement>) => {
    if (!dragRef.current) return
    dragRef.current = null
    e.currentTarget.releasePointerCapture(e.pointerId)
    void verify(x)
  }

  // 键盘也能完成校验：方向键按容差步长推进并记轨迹，回车或空格提交。
  const onKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (!captcha || done || busy) return
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      void verify(x)
      return
    }
    const step = e.key === 'PageUp' || e.key === 'PageDown' ? 24 : 6
    let next: number | null = null
    if (e.key === 'ArrowRight' || e.key === 'PageUp') next = Math.min(maxX, x + step)
    if (e.key === 'ArrowLeft' || e.key === 'PageDown') next = Math.max(0, x - step)
    if (next === null) return
    e.preventDefault()
    if (!trailRef.current.length) trailRef.current = [{ t: 0, x }]
    setX(next)
    pushTrail(performance.now(), next)
  }

  return (
    <div className="w-[280px] max-w-full space-y-2">
      <div className="relative overflow-hidden rounded-md border border-border" style={{ aspectRatio: `${imgW} / ${imgH}` }}>
        {!captcha ? (
          <Skeleton className="h-full w-full" />
        ) : (
          <>
            <img src={captcha.image} alt={t('login.captcha')} className="h-full w-full" draggable={false} />
            {captcha.piece ? (
              <img
                src={captcha.piece}
                alt=""
                aria-hidden="true"
                draggable={false}
                className="pointer-events-none absolute"
                style={{
                  left: `${(x / imgW) * 100}%`,
                  top: `${(pieceTop / imgH) * 100}%`,
                  width: `${(pieceW / imgW) * 100}%`,
                  height: `${(pieceH / imgH) * 100}%`,
                }}
              />
            ) : null}
            {done ? null : (
              <button
                type="button"
                onClick={onRefresh}
                aria-label={t('login.refreshCaptcha')}
                className="absolute end-1 top-1 flex size-7 items-center justify-center rounded-md bg-background/70 text-foreground transition-colors hover:bg-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <RefreshCw className="size-3.5" aria-hidden="true" />
              </button>
            )}
          </>
        )}
      </div>
      <div
        ref={trackRef}
        role="slider"
        tabIndex={0}
        aria-label={t('login.sliderPrompt')}
        aria-valuemin={0}
        aria-valuemax={Math.round(maxX)}
        aria-valuenow={Math.round(x)}
        aria-busy={busy}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={onPointerUp}
        onKeyDown={onKeyDown}
        className="relative h-10 touch-none select-none rounded-md border border-border bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        {/* 进度填充跟着把手走：没有它，滑轨看上去只是一条空白条，拖到哪一档全凭把手一个方块 */}
        <span
          className={`pointer-events-none absolute inset-y-0 left-0 rounded-l-[5px] ${done ? 'bg-success/15' : 'bg-primary/15'}`}
          style={{ width: `calc(4px + (100% - ${KNOB + 8}px) * ${frac} + ${KNOB / 2}px)` }}
        />
        <span className="pointer-events-none absolute inset-0 flex items-center justify-center px-12 text-xs text-muted-foreground">
          {done ? t('login.sliderDone') : t('login.sliderPrompt')}
        </span>
        <span
          className={`pointer-events-none absolute flex items-center justify-center rounded border ${
            done ? 'border-success bg-success text-success-foreground' : 'border-primary/30 bg-primary text-primary-foreground'
          }`}
          style={{ width: KNOB, height: KNOB, top: 4, left: `calc(4px + (100% - ${KNOB + 8}px) * ${frac})` }}
        >
          {done ? <Check className="size-4" aria-hidden="true" /> : <GripVertical className="size-4" aria-hidden="true" />}
        </span>
      </div>
    </div>
  )
}
