import { QueryClient, QueryObserver } from '@tanstack/react-query'
import type { i18n } from 'i18next'
import { useAuth } from '@/framework/auth/useAuth'
import { createResourceOverlay } from '@/framework/i18n/dynamic'
import { api, ApiError, tokenStore } from '@/framework/request/http'

interface Published {
  tenantId: string
  lang: string
  namespace: string
  version: number
  entries: Record<string, string>
}

const KEY = ['plugin_i18n_editor', 'published'] as const
const client = new QueryClient()
const terminal = (error: unknown) => error instanceof ApiError && [400, 401, 403, 404].includes(error.status ?? error.code)

export function refreshPublished() {
  return client.invalidateQueries({ queryKey: KEY })
}

export function start(instance: i18n) {
  const overlay = createResourceOverlay(instance)
  let identity = ''
  let generation = 0
  let stops: Array<() => void> = []
  client.mount()

  function clear() {
    generation++
    stops.forEach((stop) => stop())
    stops = []
    void client.cancelQueries({ queryKey: KEY })
    client.removeQueries({ queryKey: KEY })
    overlay.clear()
  }

  function sync() {
    const { user, token } = useAuth.getState()
    const tenant = user?.tenantId
    // No token or tenant data is persisted in the query cache.
    const next = token && tokenStore.access && tenant ? JSON.stringify([user.id, tenant]) : ''
    if (next === identity) return
    identity = next
    clear()
    if (next) load()
  }

  function load() {
    if (!identity) return
    stops.forEach((stop) => stop())
    stops = []
    const epoch = ++generation
    const tenant = useAuth.getState().user!.tenantId!
    const languages = [...new Set([instance.language, ...instance.languages])]
    const namespaces = typeof instance.options.ns === 'string' ? [instance.options.ns] : instance.options.ns || ['translation']
    for (const lang of languages) for (const namespace of namespaces) {
      let applied = -1
      const observer = new QueryObserver<Published>(client, {
        queryKey: [...KEY, identity, tenant, lang, namespace],
        queryFn: async ({ signal }) => {
          const data = await api.get<Published>('/plugins/i18n-editor/published', { lang, namespace }, { signal, silent: true })
          if (String(data.tenantId) !== tenant || data.lang !== lang || data.namespace !== namespace ||
            !Number.isSafeInteger(data.version) || data.version < 0 || !data.entries ||
            typeof data.entries !== 'object' || Array.isArray(data.entries) ||
            Object.values(data.entries).some((value) => typeof value !== 'string')) throw new Error('Invalid published catalogue')
          return data
        },
        enabled: (query) => !terminal(query.state.error),
        staleTime: 30_000,
        gcTime: 0,
        retry: false,
        refetchOnWindowFocus: true,
        refetchOnReconnect: true,
        refetchInterval: (query) => terminal(query.state.error) ? false : 60_000,
      })
      const apply = (data: Published | undefined, error: unknown) => {
        if (epoch !== generation) return
        if (terminal(error)) {
          overlay.replace(lang, namespace, {})
          applied = -1
          return
        }
        if (!data || data.version === applied) return
        applied = data.version
        overlay.replace(lang, namespace, data.entries)
      }
      stops.push(observer.subscribe((result) => apply(result.data, result.error)))
      const result = observer.getCurrentResult()
      apply(result.data, result.error)
    }
  }

  const unsubscribe = useAuth.subscribe(sync)
  instance.on('languageChanged', load)
  sync()
  return () => {
    unsubscribe()
    instance.off('languageChanged', load)
    clear()
    client.unmount()
  }
}
