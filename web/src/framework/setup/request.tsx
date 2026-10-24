import { createContext, useContext } from 'react'
import i18n from 'i18next'
import { api, useMock } from '@/framework/request/http'

// Keep the token only in the mounted wizard, never in query keys or storage.
export const SetupTokenContext = createContext('')
export const useSetupToken = () => useContext(SetupTokenContext)
export const setupHeaders = (token: string): Record<string, string> => token ? { 'X-Setup-Token': token } : {}

export interface SetupStepResult {
  key: string
  status: 'pending' | 'running' | 'done' | 'failed'
  message?: string
}

export async function executeSetup(payload: unknown, token: string, onStep: (step: SetupStepResult) => void): Promise<SetupStepResult[]> {
  if (useMock) {
    const res = await api.post<{ steps: SetupStepResult[] }>('/setup/execute', payload, {
      silent: true, headers: setupHeaders(token),
    })
    return res.steps
  }
  const response = await fetch(`${(import.meta.env.VITE_API_BASE || '/api/v1').replace(/\/$/, '')}/setup/execute`, {
    method: 'POST',
    headers: {
      ...setupHeaders(token),
      'Content-Type': 'application/json',
      Accept: 'text/event-stream, application/json',
      'Accept-Language': i18n.language,
    },
    body: JSON.stringify(payload),
  })
  if (!response.headers.get('Content-Type')?.includes('text/event-stream')) {
    const body = await response.json()
    if (!response.ok || (typeof body.code === 'number' && body.code !== 0)) {
      throw new Error(body.message || i18n.t('error.unknown'))
    }
    const steps = (body.data ?? body).steps
    if (!Array.isArray(steps)) throw new Error(i18n.t('setup.streamIncomplete'))
    return steps
  }
  if (!response.ok || !response.body) throw new Error(i18n.t('error.network'))
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let completed: SetupStepResult[] | undefined
  const consume = (frame: string) => {
    const lines = frame.split(/\r\n|\r|\n/)
    const event = lines.find((line) => line.startsWith('event:'))?.slice(6).trim()
    const data = lines.filter((line) => line.startsWith('data:')).map((line) => line.slice(5).trimStart()).join('\n')
    if (!data) return
    if (event === 'step') onStep(JSON.parse(data) as SetupStepResult)
    if (event === 'complete') completed = JSON.parse(data).steps
  }
  try {
    while (!completed) {
      const { value, done } = await reader.read()
      buffer += decoder.decode(value, { stream: !done })
      let boundary: RegExpExecArray | null
      while ((boundary = /\r\n\r\n|\n\n|\r\r/.exec(buffer))) {
        consume(buffer.slice(0, boundary.index))
        buffer = buffer.slice(boundary.index + boundary[0].length)
      }
      if (done) break
    }
    if (!completed) throw new Error(i18n.t('setup.streamIncomplete'))
    return completed
  } finally {
    await reader.cancel().catch(() => {})
    reader.releaseLock()
  }
}
