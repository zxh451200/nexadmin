import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { DataTable } from '@/framework/components/DataTable'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Textarea } from '@/framework/ui/textarea'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { usePermission } from '@/framework/auth/usePermission'
import { useEntries, usePublish, useRollback, useSaveEntry, useVersions } from './api'
import { useLanguages } from '@/framework/i18n/useLanguages'

export default function I18nEditorPage() {
  const { t } = useTranslation(); const { languages } = useLanguages(); const codes=useMemo(()=>languages.map((l)=>l.code),[languages]); const [lang,setLang]=useState(''); const [key,setKey]=useState(''); const [value,setValue]=useState(''); // 语言清单是异步的：未选中或选中项已被删掉时回落到第一门语言
  // 保存走 i18n-editor:edit、发布/回滚走 i18n-editor:publish（与 list 分开授权）：没授权收掉对应入口
  const { has } = usePermission(); const canEdit=has('i18n-editor:edit'); const canPublish=has('i18n-editor:publish')
  const active = codes.includes(lang) ? lang : (codes[0] ?? ''); const entries=useEntries(active); const versions=useVersions(active); const save=useSaveEntry(); const publish=usePublish(); const rollback=useRollback();
  const rows=useMemo(()=>entries.data?.list ?? [],[entries.data?.list]); const selected=useMemo(()=>rows.find(x=>x.key===key),[rows,key]);
  const edit=(k:string,v:string)=>{setKey(k);setValue(v)}
  return <PageContainer><PageHeader title={t('plugin_i18n_editor.title')} description={t('plugin_i18n_editor.description')} />
    <div className='flex flex-wrap items-center gap-2'><Select value={active} onValueChange={setLang}><SelectTrigger className='w-40'><SelectValue /></SelectTrigger><SelectContent>{codes.map(x=><SelectItem key={x} value={x}>{x}</SelectItem>)}</SelectContent></Select>{canPublish&&<Button onClick={()=>publish.mutate(active)} disabled={publish.isPending}>{t('plugin_i18n_editor.publish')}</Button>}{canPublish&&<Select onValueChange={v=>rollback.mutate({lang:active,version:Number(v)})}><SelectTrigger className='w-48'><SelectValue placeholder={t('plugin_i18n_editor.rollback')} /></SelectTrigger><SelectContent>{(versions.data??[]).map(x=><SelectItem key={x.version} value={String(x.version)}>{t('plugin_i18n_editor.version',{version:x.version})}</SelectItem>)}</SelectContent></Select>}</div>
    <div className='grid gap-6 lg:grid-cols-[minmax(260px,1fr)_minmax(320px,1fr)]'><DataTable data={rows} rowKey='id' columns={[{key:'key',title:t('plugin_i18n_editor.columns.key'),render:r=><span className='block max-w-[16rem] truncate' title={r.key}>{r.key}</span>},{key:'value',title:t('plugin_i18n_editor.columns.value'),render:r=>(r.value?<span className='line-clamp-2 max-w-64' title={r.value}>{r.value}</span>:<span className='text-muted-foreground'>-</span>)},{key:'status',title:t('plugin_i18n_editor.columns.status')},{key:'actions',title:t('plugin_i18n_editor.columns.actions'),render:r=>canEdit&&<Button size='sm' variant='outline' onClick={()=>edit(r.key,r.value)}>{t('plugin_i18n_editor.edit')}</Button>}]} />{canEdit&&<div className='space-y-3 rounded-md border p-4'><Input value={key} onChange={e=>setKey(e.target.value)} placeholder={t('plugin_i18n_editor.key')} /><Textarea value={value} onChange={e=>setValue(e.target.value)} placeholder={t('plugin_i18n_editor.value')} /><Button disabled={!key.trim()||!active||save.isPending} onClick={()=>save.mutate({lang:active,key:key.trim(),value})}>{t('plugin_i18n_editor.save')}</Button>{selected&&<span className='text-sm text-muted-foreground'>{t('plugin_i18n_editor.currentVersion',{version:selected.version})}</span>}</div>}</div>
  </PageContainer>
}
