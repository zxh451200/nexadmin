import { useRef, useState } from 'react'
import { FormProvider, useForm } from 'react-hook-form'
import { useNavigate } from 'react-router-dom'
import { useMemo } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { zodResolver } from '@hookform/resolvers/zod'
import { CheckCircle2, Circle, Eye, EyeOff, Hexagon, Loader2, XCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { LanguageSwitch } from '@/framework/i18n/LanguageSwitch'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { ThemeToggle } from '@/framework/theme/ThemeToggle'
import { Button } from '@/framework/ui/button'
import { Progress } from '@/framework/ui/progress'
import { cn } from '@/lib/utils'
import { buildSchema, defaultValues, STEP_FIELDS, type SetupValues } from './schema'
import { AdminStep, ConfirmSummary, OptionalStep } from './steps/ConfigSteps'
import { CacheStep, DbStep, EnvStep } from './steps/ConnectionSteps'
import { executeSetup, SetupTokenContext, type SetupStepResult } from './request'

const STEPS = ['env', 'db', 'cache', 'admin', 'optional', 'confirm'] as const
const EXEC_STEPS = ['writeConfig', 'migrate', 'seed', 'createAdmin', 'writeMark'] as const
const LAST_STEP = STEPS.length - 1

type ExecStatus = 'pending' | 'running' | 'done' | 'failed'
type Phase = 'idle' | 'running' | 'done' | 'failed'

function ExecutionProgress({ statuses, error }: { statuses: Record<string, ExecStatus>; error?: string }) {
  const { t } = useTranslation()
  const done = EXEC_STEPS.filter((k) => statuses[k] === 'done').length
  return (
    <div className="space-y-4" aria-live="polite">
      <Progress value={(done / EXEC_STEPS.length) * 100} />
      <ul className="divide-y divide-border rounded-lg border border-border">
        {EXEC_STEPS.map((k) => {
          const s = statuses[k] ?? 'pending'
          return (
            <li key={k} className="flex min-h-11 items-center gap-3 px-3 py-2 text-sm">
              {s === 'done' && <CheckCircle2 className="size-4 text-success" aria-hidden="true" />}
              {s === 'running' && <Loader2 className="size-4 animate-spin text-primary motion-reduce:animate-none" aria-hidden="true" />}
              {s === 'failed' && <XCircle className="size-4 text-destructive" aria-hidden="true" />}
              {s === 'pending' && <Circle className="size-4 text-muted-foreground" aria-hidden="true" />}
              <span className={cn(s === 'pending' && 'text-muted-foreground')}>{t(`setup.exec.${k}`)}</span>
            </li>
          )
        })}
      </ul>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
    </div>
  )
}

export default function SetupWizard() {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [step, setStep] = useState(0)
  const [phase, setPhase] = useState<Phase>('idle')
  const [statuses, setStatuses] = useState<Record<string, ExecStatus>>({})
  const [error, setError] = useState<string>()
  const [setupToken, setSetupToken] = useState('')
  const [showToken, setShowToken] = useState(false)
  const executing = useRef(false)

  const schema = useMemo(() => buildSchema(t), [t])
  const form = useForm<SetupValues>({
    resolver: zodResolver(schema),
    defaultValues: defaultValues(i18n.language),
    mode: 'onTouched',
  })

  const next = async () => {
    const fields = STEP_FIELDS[step]
    if (fields.length) {
      const ok = await form.trigger(fields as never[])
      if (!ok) return
    }
    setStep((s) => Math.min(s + 1, LAST_STEP))
  }

  const execute = async () => {
    if (executing.current) return
    const parsed = schema.safeParse(form.getValues())
    if (!parsed.success) {
      await form.trigger()
      const invalid = parsed.error.issues[0]?.path.join('.') ?? ''
      const target = STEP_FIELDS.findIndex((fields) => fields.some((field) => invalid === field || invalid.startsWith(`${field}.`)))
      if (target >= 0) setStep(target)
      setPhase('idle')
      return
    }
    const values = parsed.data
    const admin: Partial<typeof values.admin> = { ...values.admin }
    delete admin.confirm
    setError(undefined)
    setPhase('running')
    executing.current = true
    setStatuses(Object.fromEntries(EXEC_STEPS.map((k) => [k, 'pending'])))
    try {
      const onStep = (result: SetupStepResult) => {
        if (EXEC_STEPS.some((key) => key === result.key)) {
          setStatuses((s) => ({ ...s, [result.key]: result.status }))
        }
      }
      const results = await executeSetup({ ...values, admin }, setupToken, onStep)
      results.forEach(onStep)
      const failed = results.find((result) => result.status === 'failed')
      if (failed) throw new Error(failed.message || t('error.unknown'))
      if (!EXEC_STEPS.every((key) => results.some((r) => r.key === key && r.status === 'done'))) {
        throw new Error(t('setup.streamIncomplete'))
      }
      const activationWarning = results.find((result) => result.message)
      if (activationWarning) setError(t('setup.restartRequired'))
      setSetupToken('')
      setPhase('done')
    } catch (e) {
      setStatuses((s) => {
        const running = EXEC_STEPS.find((k) => s[k] === 'running')
        return running ? { ...s, [running]: 'failed' } : s
      })
      setError(e instanceof Error ? e.message : t('error.unknown'))
      setPhase('failed')
    } finally {
      executing.current = false
    }
  }

  const finish = () => {
    qc.setQueryData(['system', 'status'], { initialized: true })
    navigate('/login', { replace: true })
  }

  const locked = phase === 'running' || phase === 'done'

  return (
    <div className="tech-grid relative min-h-dvh bg-background px-4 pb-[max(2rem,env(safe-area-inset-bottom))] pt-16 lg:py-8">
      <div className="absolute end-4 top-4 flex items-center gap-1">
        <LanguageSwitch />
        <ThemeToggle />
      </div>
      <main className="glass mx-auto w-full max-w-3xl rounded-xl border border-border p-4 md:p-8">
        <header className="mb-6 flex items-center gap-3">
          <span className="flex size-10 items-center justify-center rounded-xl bg-primary text-primary-foreground">
            <Hexagon className="size-5" aria-hidden="true" />
          </span>
          <div className="min-w-0">
            <h1 className="text-xl font-semibold">{t('setup.title')}</h1>
            <p className="text-sm text-muted-foreground">{t('setup.subtitle')}</p>
          </div>
        </header>

        <ol className="mb-6 hidden items-center gap-2 md:flex" aria-label={t('setup.steps')}>
          {STEPS.map((k, i) => (
            <li key={k} className="flex flex-1 items-center gap-2" aria-current={i === step ? 'step' : undefined}>
              <span
                className={cn(
                  'num flex size-7 shrink-0 items-center justify-center rounded-full border text-xs',
                  i < step && 'border-primary bg-primary text-primary-foreground',
                  i === step && 'border-primary text-primary',
                  i > step && 'border-border text-muted-foreground',
                )}
              >
                {i + 1}
              </span>
              <span className={cn('truncate text-xs', i === step ? 'font-medium text-foreground' : 'text-muted-foreground')}>{t(`setup.step.${k}`)}</span>
            </li>
          ))}
        </ol>
        <div className="mb-6 space-y-2 md:hidden">
          <p className="num text-sm text-muted-foreground">
            {step + 1} / {STEPS.length} · <span className="text-foreground">{t(`setup.step.${STEPS[step]}`)}</span>
          </p>
          <Progress value={((step + 1) / STEPS.length) * 100} className="h-1.5" />
        </div>

        <SetupTokenContext.Provider value={setupToken}>
        <FormProvider {...form}>
          <form
            onSubmit={(e) => e.preventDefault()}
            noValidate
            className="space-y-6 [&_input]:text-base md:[&_input]:text-sm"
          >
            {phase !== 'done' && (
              <div className="space-y-2">
                <Label htmlFor="setup-token">{t('setup.tokenLabel')}</Label>
                <div className="relative">
                  <Input id="setup-token" type={showToken ? 'text' : 'password'} autoComplete="off" spellCheck={false}
                    value={setupToken} disabled={locked} onChange={(e) => setSetupToken(e.target.value.trim())}
                    aria-describedby="setup-token-hint" className="pe-12" />
                  <button
                    type="button"
                    onClick={() => setShowToken((s) => !s)}
                    aria-label={showToken ? t('setup.hidePassword') : t('setup.showPassword')}
                    className="absolute inset-y-0 end-0 flex w-11 items-center justify-center text-muted-foreground hover:text-foreground md:w-9"
                  >
                    {showToken ? <Eye className="size-4" aria-hidden="true" /> : <EyeOff className="size-4" aria-hidden="true" />}
                  </button>
                </div>
                <p id="setup-token-hint" className="text-xs text-muted-foreground">{t('setup.tokenHint')}</p>
              </div>
            )}
            <section aria-labelledby="step-title">
              <h2 id="step-title" className="mb-4 text-base font-medium">
                {t(`setup.step.${STEPS[step]}`)}
              </h2>
              {step === 0 && <EnvStep />}
              {step === 1 && <DbStep />}
              {step === 2 && <CacheStep />}
              {step === 3 && <AdminStep />}
              {step === 4 && <OptionalStep />}
              {step === LAST_STEP && (phase === 'idle' ? <ConfirmSummary /> : <ExecutionProgress statuses={statuses} error={error} />)}
            </section>

            <div className="flex flex-col-reverse gap-3 border-t border-border pt-4 sm:flex-row sm:justify-between">
              <Button type="button" variant="outline" disabled={step === 0 || locked} onClick={() => { setPhase('idle'); setStep((s) => s - 1) }}>
                {t('common.prev')}
              </Button>
              <div className="flex flex-col-reverse gap-3 sm:flex-row">
                {step < LAST_STEP && (
                  <Button type="button" onClick={next}>
                    {t('common.next')}
                  </Button>
                )}
                {step === LAST_STEP && phase !== 'done' && (
                  <Button type="button" onClick={execute} disabled={phase === 'running'}>
                    {phase === 'running' && <Loader2 className="me-2 size-4 animate-spin motion-reduce:animate-none" aria-hidden="true" />}
                    {phase === 'failed' ? t('setup.retry') : t('setup.execute')}
                  </Button>
                )}
                {phase === 'done' && (
                  <Button type="button" onClick={finish}>
                    {t('setup.goLogin')}
                  </Button>
                )}
              </div>
            </div>
          </form>
        </FormProvider>
        </SetupTokenContext.Provider>
      </main>
    </div>
  )
}
