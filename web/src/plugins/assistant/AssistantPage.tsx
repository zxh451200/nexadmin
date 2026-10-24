import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '@/framework/auth/useAuth'
import { usePermission } from '@/framework/auth/usePermission'
import { ApiError } from '@/framework/request/http'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Button } from '@/framework/ui/button'
import { Textarea } from '@/framework/ui/textarea'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { assistantApi, type Conversation, type Snapshot } from './api'

export default function AssistantPage() {
  const user = useAuth(s => s.user)
  return <Conversations key={`${user?.tenantId}:${user?.id}`} />
}

function Conversations() {
  const { t } = useTranslation()
  const [rows, setRows] = useState<Conversation[]>([])
  const [selected, setSelected] = useState('')
  const [editing, setEditing] = useState<'create' | 'rename'>()
  const [name, setName] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState(false)
  const alive = useRef(true)
  const busy = useRef(false)
  const refresh = async () => {
    try {
      const next = await assistantApi.conversations()
      if (alive.current) { setRows(next); setError(false) }
    } catch { if (alive.current) setError(true) }
  }
  useEffect(() => {
    alive.current = true
    void refresh()
    return () => { alive.current = false }
  }, [])
  const mutate = async (remove = false) => {
    if (busy.current || (!remove && !name.trim())) return
    if (remove && !window.confirm(t('plugin_assistant.conversations.confirmDelete'))) return
    busy.current = true
    setPending(true)
    try {
      if (remove) {
        await assistantApi.remove(selected)
        if (alive.current) setSelected('')
      } else if (editing === 'create') {
        const row = await assistantApi.create(name.trim())
        if (alive.current) { setRows(previous => [row, ...previous]); setSelected(row.id) }
      } else {
        await assistantApi.rename(selected, name.trim())
      }
      if (alive.current) { setEditing(undefined); setName(''); await refresh() }
    } catch { if (alive.current) setError(true) }
    finally { busy.current = false; if (alive.current) setPending(false) }
  }
  return <PageContainer>
    <PageHeader title={t('plugin_assistant.title')} description={t('plugin_assistant.description')} />
    <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
      <div className="w-full min-w-0 sm:max-w-sm">
        <Select value={selected || 'legacy'} disabled={pending || !!editing} onValueChange={value => setSelected(value === 'legacy' ? '' : value)}>
          <SelectTrigger aria-label={t('plugin_assistant.conversations.select')}><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="legacy">{t('plugin_assistant.conversations.legacy')}</SelectItem>
            {rows.map(row => <SelectItem key={row.id} value={row.id}>{row.name}</SelectItem>)}
          </SelectContent>
        </Select>
      </div>
      <div className="flex flex-wrap gap-2">
        <Button variant="outline" disabled={pending || rows.length >= 30} onClick={() => { setEditing('create'); setName('') }}>{t('plugin_assistant.conversations.new')}</Button>
        <Button variant="outline" disabled={pending || !selected} onClick={() => { setEditing('rename'); setName(rows.find(row => row.id === selected)?.name ?? '') }}>{t('plugin_assistant.conversations.rename')}</Button>
        <Button variant="outline" disabled={pending || !selected} onClick={() => { void mutate(true) }}>{t('plugin_assistant.conversations.delete')}</Button>
        <Button variant="outline" disabled={pending} onClick={() => { void refresh() }}>{t('plugin_assistant.refresh')}</Button>
      </div>
    </div>
    {editing && <form className="flex flex-col gap-2 sm:flex-row" onSubmit={event => { event.preventDefault(); void mutate() }}>
      <Input autoFocus aria-label={t('plugin_assistant.conversations.name')} placeholder={t('plugin_assistant.conversations.name')} value={name} maxLength={80} disabled={pending} onChange={event => setName(event.target.value)} />
      <Button disabled={pending || !name.trim() || new TextEncoder().encode(name.trim()).length > 240}>{t('plugin_assistant.conversations.save')}</Button>
      <Button type="button" variant="outline" disabled={pending} onClick={() => setEditing(undefined)}>{t('plugin_assistant.conversations.dismiss')}</Button>
    </form>}
    {error && <p role="alert" className="text-destructive">{t('plugin_assistant.conversations.error')}</p>}
    <Chat key={selected} conversationId={selected} />
  </PageContainer>
}

function Chat({ conversationId }: { conversationId: string }) {
  const { t } = useTranslation()
  const { has } = usePermission()
  const [snapshot, setSnapshot] = useState<Snapshot>()
  const [text, setText] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState(false)
  const [cancelRequested, setCancelRequested] = useState(false)
  const [confirmationPending, setConfirmationPending] = useState(false)
  const [now, setNow] = useState(Date.now)
  const inFlight = useRef(false)
  const alive = useRef(true)
  const generation = useRef(0)
  const bottom = useRef<HTMLDivElement>(null)
  const refresh = async () => {
    const version = generation.current
    try {
      const next = await assistantApi.session(conversationId)
      if (alive.current && version === generation.current) {
        setSnapshot(next)
        setError(false)
      }
    } catch {
      if (alive.current && version === generation.current) setError(true)
    }
  }
  useEffect(() => {
    alive.current = true
    void refresh()
    // Sequential polling avoids accumulating requests on a slow connection.
    let timer: ReturnType<typeof setTimeout>
    let stopped = false
    const poll = async () => {
      await refresh()
      if (!stopped) timer = setTimeout(() => { void poll() }, 1500)
    }
    timer = setTimeout(() => { void poll() }, 1500)
    return () => { stopped = true; alive.current = false; clearTimeout(timer) }
  }, [])
  useEffect(() => { bottom.current?.scrollIntoView({ block: 'nearest' }) }, [snapshot?.session.messages])
  const running = snapshot?.session.status === 'running'
  const awaitingConfirmation = snapshot?.session.status === 'awaiting_confirmation'
  const confirmation = snapshot?.session.confirmation
  const expiresAt = confirmation ? Date.parse(confirmation.expiresAt) : NaN
  // Accept only the exact internal route, bound to the supplied confirmation ID.
  const confirmationUrl = confirmation &&
    /^[a-f0-9]{64}$/.test(confirmation.confirmationId) &&
    confirmation.confirmationUrl === `/mcp/confirmations/${confirmation.confirmationId}`
    ? confirmation.confirmationUrl : undefined
  const validConfirmation = !!confirmationUrl && Number.isFinite(expiresAt)
  const expired = snapshot?.session.status === 'confirmation_expired' ||
    (awaitingConfirmation && Number.isFinite(expiresAt) && expiresAt <= now)
  const canConfirm = has('mcp:list')
  useEffect(() => {
    if (!awaitingConfirmation) return
    setNow(Date.now())
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [awaitingConfirmation, confirmation?.expiresAt])
  const busy = pending || running
  const canContinue = awaitingConfirmation && validConfirmation && !expired &&
    canConfirm && !busy && !!snapshot?.gateway.enabled
  const bytes = new TextEncoder().encode(text).length
  const execute = async (resume = false) => {
    if (inFlight.current) return
    if (resume) {
      if (!canContinue || expiresAt <= Date.now()) return
    } else if (busy || awaitingConfirmation || !text.trim() || bytes > 4096 || !snapshot?.gateway.enabled) return
    inFlight.current = true
    setPending(true)
    setCancelRequested(false)
    setConfirmationPending(false)
    setError(false)
    generation.current++
    try {
      const session = await (resume ? assistantApi.continue(conversationId) : assistantApi.run(text, conversationId))
      if (alive.current) {
        generation.current++
        setSnapshot(previous => previous ? { ...previous, session } : previous)
        if (session.status === 'completed') setText('')
      }
    } catch (cause) {
      if (alive.current) {
        const conflict = cause instanceof ApiError && (cause.status === 409 || cause.code === 409)
        setConfirmationPending(conflict)
        setError(!conflict)
      }
    } finally {
      inFlight.current = false
      if (alive.current) { setPending(false); void refresh() }
    }
  }
  const clear = async () => {
    if (inFlight.current || busy || !window.confirm(t('plugin_assistant.confirmClear'))) return
    inFlight.current = true
    setPending(true)
    generation.current++
    try {
      await assistantApi.clear(conversationId)
      if (alive.current) { setText(''); setConfirmationPending(false); await refresh() }
    } catch {
      if (alive.current) setError(true)
    } finally {
      inFlight.current = false
      if (alive.current) setPending(false)
    }
  }
  const cancel = async () => {
    if (!snapshot?.session.runId) return
    try {
      await assistantApi.cancel(snapshot.session.runId, conversationId)
      if (alive.current) setCancelRequested(true)
    } catch {
      if (alive.current) { setError(true); void refresh() }
    }
  }
  const status = snapshot?.session.status ?? 'idle'
  const knownStatuses = ['idle', 'running', 'awaiting_confirmation', 'confirmation_expired', 'completed', 'stopped', 'cancelled', 'timeout', 'output_limit', 'disabled', 'quota', 'rate_limited', 'budget']
  return <div className="space-y-4">
    <div className="flex flex-wrap items-center gap-3">
      <span role="status">{t(`plugin_assistant.status.${knownStatuses.includes(status) ? status : 'stopped'}`)}</span>
      {snapshot && <span className="text-sm text-muted-foreground">{snapshot.gateway.model} · {t('plugin_assistant.usage', { tokens: snapshot.session.tokens, rounds: snapshot.session.rounds })}</span>}
      <Button variant="outline" disabled={busy || !snapshot} onClick={() => { void clear() }}>{t('plugin_assistant.clear')}</Button>
      <Button variant="outline" onClick={() => { void refresh() }}>{t('plugin_assistant.refresh')}</Button>
      {running && <Button variant="outline" disabled={cancelRequested} onClick={() => { void cancel() }}>{t(cancelRequested ? 'plugin_assistant.cancelling' : 'plugin_assistant.cancel')}</Button>}
    </div>
    {error && <p role="alert" className="text-destructive">{t('plugin_assistant.error')}</p>}
    {snapshot && !snapshot.gateway.enabled && <p role="alert">{t('plugin_assistant.disabled')}</p>}
    {confirmationPending && <p role="status">{t('plugin_assistant.confirmation.pending')}</p>}
    {(awaitingConfirmation || status === 'confirmation_expired') && <section aria-label={t('plugin_assistant.confirmation.title')} className="space-y-2 rounded-md border p-4">
      <p className="font-medium">{t('plugin_assistant.confirmation.title')}</p>
      <p>{t('plugin_assistant.confirmation.instructions')}</p>
      {Number.isFinite(expiresAt) && <p>{t('plugin_assistant.confirmation.expiry')} <time dateTime={confirmation?.expiresAt}>{new Date(expiresAt).toLocaleString()}</time></p>}
      {expired && <p role="status">{t('plugin_assistant.confirmation.expired')}</p>}
      {awaitingConfirmation && !validConfirmation && <p role="alert">{t('plugin_assistant.confirmation.invalid')}</p>}
      {!canConfirm && <p>{t('plugin_assistant.confirmation.permission')}</p>}
      {awaitingConfirmation && validConfirmation && !expired && canConfirm && <a className="inline-block text-primary underline" href={confirmationUrl} target="_blank" rel="noopener noreferrer">{t('plugin_assistant.confirmation.open')}</a>}
      <div><Button disabled={!canContinue} onClick={() => { void execute(true) }}>{t('plugin_assistant.confirmation.continue')}</Button></div>
    </section>}
    <div role="log" aria-label={t('plugin_assistant.title')} className="max-h-[55vh] min-h-64 space-y-4 overflow-y-auto rounded-md border p-4">
      {!snapshot?.session.messages.length && <p className="text-muted-foreground">{t('plugin_assistant.empty')}</p>}
      {snapshot?.session.messages.map((message, index) => <article key={index} className={`rounded-md border p-3 ${message.role === 'user' ? 'bg-muted' : ''}`}>
        <p className="mb-2 text-sm font-medium">{t(message.role === 'user' ? 'plugin_assistant.you' : 'plugin_assistant.title')}</p>
        <p className="whitespace-pre-wrap break-words [overflow-wrap:anywhere]">{message.content}</p>
      </article>)}
      <div ref={bottom} />
    </div>
    <form className="space-y-2" onSubmit={event => { event.preventDefault(); void execute() }}>
      <Textarea aria-label={t('plugin_assistant.input')} placeholder={t('plugin_assistant.input')} value={text} maxLength={4096}
        disabled={busy || awaitingConfirmation} onChange={event => setText(event.target.value)} />
      <div className="flex items-center justify-between gap-3">
        <span className={bytes > 4096 ? 'text-destructive' : 'text-sm text-muted-foreground'}>{bytes}/4096 {t('plugin_assistant.bytes')}</span>
        <Button type="submit" disabled={busy || awaitingConfirmation || !snapshot?.gateway.enabled || !text.trim() || bytes > 4096}>{t('plugin_assistant.send')}</Button>
      </div>
    </form>
  </div>
}
