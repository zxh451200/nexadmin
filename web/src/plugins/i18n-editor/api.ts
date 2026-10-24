import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import { refreshPublished } from './published.i18n-loader'

export interface Entry { id: string; tenantId: string; lang: string; key: string; value: string; status: string; version: number; updatedAt: string }
export interface Version { id: string; lang: string; version: number; entries: string; createdAt: string }
const BASE = '/plugins/i18n-editor'
const KEY = ['plugin_i18n_editor'] as const
export const useEntries = (lang?: string) => useQuery({ queryKey: [...KEY, 'entries', lang], queryFn: () => api.get<{ list: Entry[]; total: number }>(`${BASE}/entries`, lang ? { lang } : undefined) })
export const useVersions = (lang: string) => useQuery({ queryKey: [...KEY, 'versions', lang], queryFn: () => api.get<Version[]>(`${BASE}/versions`, { lang }), enabled: !!lang })
export function useSaveEntry() { const qc=useQueryClient(); return useMutation({ mutationFn:(data: Pick<Entry,'lang'|'key'|'value'>)=>api.put<Entry>(`${BASE}/entries`,data), onSuccess:()=>qc.invalidateQueries({queryKey:KEY}) }) }
export function usePublish() { const qc=useQueryClient(); return useMutation({ mutationFn:(lang:string)=>api.post<{lang:string;version:number;count:number}>(`${BASE}/publish?lang=${encodeURIComponent(lang)}`), onSuccess:()=>Promise.all([qc.invalidateQueries({queryKey:KEY}), refreshPublished()]) }) }
export const useRollback = () => { const qc=useQueryClient(); return useMutation({ mutationFn:({lang,version}:{lang:string;version:number})=>api.post(`${BASE}/rollback?lang=${encodeURIComponent(lang)}&version=${version}`), onSuccess:()=>Promise.all([qc.invalidateQueries({queryKey:KEY}), refreshPublished()]) }) }
