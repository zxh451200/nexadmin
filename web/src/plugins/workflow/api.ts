import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { Page, PageQuery } from '@/framework/request/types'

export type WfStatus = 'running' | 'approved' | 'rejected' | 'cancelled'
export type ReplierType = 'start' | 'approve' | 'reject' | 'countersign' | 'rollback' | 'escalate' | 'withdraw'

export interface WfNode {
  key: string
  name: string
  kind: 'start' | 'approval' | 'exclusive' | 'end'
  approvers?: string[]
  mode?: string
  escalateTo?: string
  timeoutMinutes?: number
}

export interface WfEdge {
  from: string
  to: string
  when?: string
}

export interface WfDefinition {
  key: string
  name: string
  start: string
  nodes: WfNode[]
  edges: WfEdge[]
}

export interface WfDef {
  id: string
  key: string
  name: string
  status: 'draft' | 'published' | 'disabled'
  version: number
  definition: WfDefinition
  /** 建模器保存的 BPMN XML（历史定义由后端现场生成） */
  xml?: string
  createdAt: string
  updatedAt: string
}

export interface WfPending {
  nodeKey: string
  nodeName: string
  assignee: string
}

export interface WfEvent {
  at: string
  node: string
  actor: string
  action: string
  comment?: string
}

export interface WfInst {
  id: string
  defId: string
  defKey: string
  defVersion: number
  title: string
  bizType: string
  businessKey: string
  submitter: string
  status: WfStatus
  revision: number
  current: string[]
  done: string[]
  vars: Record<string, string>
  votes: Record<string, Record<string, string>>
  history: WfEvent[]
  /** 当前待办（会签节点会有多人） */
  pending: WfPending[]
  /** 发起时的定义快照，用于画流程图 */
  definition?: WfDefinition
  createdAt: string
  updatedAt: string
}

export interface WfQuery extends PageQuery {
  status?: string
  bizType?: string
  /** started = 我发起的 */
  mine?: string
}

export interface DefQuery extends PageQuery {
  status?: string
}

export interface WfTemplate {
  key: string
  name: string
  definition: WfDefinition
  xml?: string
}

const BASE = '/plugins/workflow'
const KEY = ['plugin_workflow'] as const

export function useDefs(query: DefQuery) {
  return useQuery({
    queryKey: [...KEY, 'defs', query],
    queryFn: () => api.get<Page<WfDef>>(`${BASE}/defs`, query),
    placeholderData: keepPreviousData,
  })
}

export function useTemplates() {
  return useQuery({
    queryKey: [...KEY, 'templates'],
    queryFn: () => api.get<WfTemplate[]>(`${BASE}/templates`),
    staleTime: 5 * 60_000,
  })
}

export function useInstances(query: WfQuery & { todo?: string; done?: string }) {
  return useQuery({
    queryKey: [...KEY, 'instances', query],
    queryFn: () => api.get<Page<WfInst>>(`${BASE}/instances`, query),
    placeholderData: keepPreviousData,
    refetchInterval: 8000,
  })
}

export function useInstance(id?: string) {
  return useQuery({
    queryKey: [...KEY, 'instance', id],
    queryFn: () => api.get<WfInst>(`${BASE}/instances/${id}`),
    enabled: !!id,
  })
}

function useInvalidate() {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: KEY })
}

export function useSaveDef() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({
      id,
      data,
    }: {
      id?: string
      data: { key: string; name: string; status: string; definition: WfDefinition; xml?: string }
    }) => (id ? api.put<WfDef>(`${BASE}/defs/${id}`, data) : api.post<WfDef>(`${BASE}/defs`, data)),
    onSuccess: invalidate,
  })
}

export function useDeleteDef() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.del(`${BASE}/defs/${id}`),
    onSuccess: invalidate,
  })
}

export function useStartInstance() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (data: { defId: string; title: string; bizType?: string; businessKey?: string; vars: Record<string, string> }) =>
      api.post<WfInst>(`${BASE}/instances`, data),
    onSuccess: invalidate,
  })
}

export function useWfAction() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, action, body }: { id: string; action: 'approve' | 'reject' | 'countersign' | 'rollback' | 'escalate' | 'withdraw'; body: Record<string, unknown> }) =>
      api.post<WfInst>(`${BASE}/instances/${id}/${action}`, body),
    onSuccess: invalidate,
  })
}
