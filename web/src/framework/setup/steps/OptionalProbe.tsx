import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '@/framework/request/http'
import { Button } from '@/framework/ui/button'
import { setupHeaders, useSetupToken } from '../request'

const messages = {
  'zh-CN': {
    test: '探测连接', cancel: '取消探测', running: '正在探测…',
    token: '请先填写控制台输出的初始化令牌，本机探测也需要令牌。',
    s3: '只读检查目标 Bucket 存在及 HEAD 访问权限；不创建桶、不写对象，不验证对象读写或前缀权限。',
    ai: '仅调用 GET /models 检查连接及模型列表；携带已配置的 Key，不发送生成请求，不验证生成能力、额度或生成权限。公开列表不能证明 Key 有效。',
    ok: '探测成功（仅限上述范围）',
    failed: '探测失败',
    invalid: '配置不完整或格式无效。',
    connection: '无法建立连接，请检查地址、网络及 TLS 证书。',
    timeout: '探测超时。',
    cancelled: '探测已取消。',
    denied: '鉴权失败或无访问权限。',
    notFound: '目标 Bucket 或 models 接口不存在。',
    redirect: '为保护凭据，已拒绝重定向；请直接填写最终服务地址。',
    upstreamLimited: '上游服务限流，请稍后重试。',
    upstream: '上游未接受探测请求。',
    invalidResponse: 'models 接口未返回可识别的 OpenAI 模型列表。',
    modelNotListed: 'models 接口可访问，但未列出所选模型；未验证生成能力。',
    modelsUnsupported: '服务不支持 models 探测；未验证生成能力。',
    rateLimited: '探测过于频繁，请稍后重试。',
    request: '请求失败，请检查初始化令牌、初始化状态及网络，稍后重试。',
  },
  'en-US': {
    test: 'Probe connection', cancel: 'Cancel probe', running: 'Probing…',
    token: 'Enter the setup token printed in the console. Local probes also require it.',
    s3: 'Read-only check of target bucket existence and HEAD access. No bucket creation or object writes; object access and prefix permissions are not verified.',
    ai: 'Only GET /models checks connectivity and the model list, using the configured key. No generation requests; generation, quota and generation permissions are not verified. A public list cannot establish key validity.',
    ok: 'Probe succeeded (limited to the scope above)',
    failed: 'Probe failed',
    invalid: 'Configuration is incomplete or invalid.',
    connection: 'Could not connect. Check the endpoint, network and TLS certificate.',
    timeout: 'The probe timed out.',
    cancelled: 'The probe was cancelled.',
    denied: 'Authentication failed or access was denied.',
    notFound: 'The target bucket or models endpoint was not found.',
    redirect: 'Redirect refused to protect credentials. Enter the final service endpoint directly.',
    upstreamLimited: 'The upstream service rate limited the request. Try again later.',
    upstream: 'The upstream service did not accept the probe.',
    invalidResponse: 'The models endpoint did not return a recognizable OpenAI model list.',
    modelNotListed: 'The models endpoint is accessible but does not list the selected model. Generation is not verified.',
    modelsUnsupported: 'The service does not support a models probe. Generation is not verified.',
    rateLimited: 'Probes are too frequent. Try again later.',
    request: 'Request failed. Check the setup token, initialization state and network, then retry.',
  },
}

type ProbeCode = keyof typeof messages['en-US']
interface ProbeResult { ok: boolean; code: string; scope: string }

export function OptionalProbe({ kind, payload }: { kind: 's3' | 'ai'; payload: unknown }) {
  const { i18n } = useTranslation()
  const text = messages[i18n.language.startsWith('zh') ? 'zh-CN' : 'en-US']
  const token = useSetupToken()
  const [running, setRunning] = useState(false)
  const [result, setResult] = useState<{ ok: boolean; code: ProbeCode } | null>(null)
  const active = useRef<AbortController>()
  const key = JSON.stringify(payload)

  useEffect(() => {
    setResult(null)
    setRunning(false)
    active.current?.abort()
    active.current = undefined
    return () => {
      active.current?.abort()
      active.current = undefined
    }
  }, [key, token])

  const run = async () => {
    if (active.current || !token) return
    const controller = new AbortController()
    active.current = controller
    setRunning(true)
    setResult(null)
    const timer = window.setTimeout(() => controller.abort(), 10000)
    try {
      const response = await api.post<ProbeResult>(`/setup/test-${kind}`, payload, {
        silent: true, headers: setupHeaders(token), signal: controller.signal,
      })
      if (active.current !== controller) return
      if (!response || response.scope !== kind || typeof response.ok !== 'boolean' || typeof response.code !== 'string') {
        setResult({ ok: false, code: 'request' })
        return
      }
      const code = Object.hasOwn(messages['en-US'], response.code) ? response.code as ProbeCode : 'request'
      setResult({ ok: response.ok === true, code: response.ok ? 'ok' : code })
    } catch {
      if (active.current === controller) {
        setResult({ ok: false, code: controller.signal.aborted ? 'timeout' : 'request' })
      }
    } finally {
      window.clearTimeout(timer)
      if (active.current === controller) {
        active.current = undefined
        setRunning(false)
      }
    }
  }

  const cancel = () => {
    active.current?.abort()
    active.current = undefined
    setRunning(false)
    setResult({ ok: false, code: 'cancelled' })
  }

  return (
    <div className="space-y-2">
      <p className="text-xs text-muted-foreground">{text[kind]}</p>
      {!token && <p className="text-sm text-muted-foreground">{text.token}</p>}
      <div className="flex gap-2">
        <Button type="button" variant="outline" onClick={run} disabled={running || !token}>
          {running ? text.running : text.test}
        </Button>
        {running && <Button type="button" variant="outline" onClick={cancel}>{text.cancel}</Button>}
      </div>
      {result && (
        <p role="status" aria-live="polite" className={`text-sm ${result.ok ? 'text-success' : 'text-destructive'}`}>
          {result.ok ? text.ok : `${text.failed}: ${text[result.code]}`}
        </p>
      )}
    </div>
  )
}
