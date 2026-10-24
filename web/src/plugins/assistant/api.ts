import { api } from '@/framework/request/http'

export interface Session {
  messages: { role: 'user' | 'assistant'; content: string }[]
  status: string
  runId: string
  tokens: number
  rounds: number
  confirmation?: { confirmationId: string; confirmationUrl: string; expiresAt: string }
}
export interface Snapshot { session: Session; gateway: { enabled: boolean; model: string } }
export interface Conversation { id: string; name: string; createdAt: string; updatedAt: string }
const base = '/plugins/assistant'
// Explicit real transport, including when the host runs in mock mode.
const config = { adapter: 'xhr' as const, silent: true }
const selected = (id: string) => id ? `?conversationId=${encodeURIComponent(id)}` : ''
export const assistantApi = {
  conversations: () => api.get<Conversation[]>(`${base}/conversations`, undefined, config),
  create: (name: string) => api.post<Conversation>(`${base}/conversations`, { name }, config),
  rename: (id: string, name: string) => api.put(`${base}/conversations/${encodeURIComponent(id)}`, { name }, config),
  remove: (id: string) => api.del(`${base}/conversations/${encodeURIComponent(id)}`, undefined, config),
  session: (id = '') => api.get<Snapshot>(`${base}/session${selected(id)}`, undefined, config),
  run: (text: string, id = '') => api.post<Session>(`${base}/run${selected(id)}`, { text }, { ...config, timeout: 100000 }),
  continue: (id = '') => api.post<Session>(`${base}/continue${selected(id)}`, {}, { ...config, timeout: 100000 }),
  cancel: (runId: string, id = '') => api.post(`${base}/cancel${selected(id)}`, { runId }, config),
  clear: (id = '') => api.del(`${base}/session${selected(id)}`, undefined, config),
}
