import { useCallback, useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import { fetchJob, jobKeys, type Job } from '@/framework/modules/exportcenter/api'

const POLL_MS = 2000
const CENTER_PATH = '/jobs'

/**
 * 异步任务通用流程：创建 → 轮询 → 完成提示。
 * 创建成功后提示「已提交到导出中心」（可跳转），然后每 2 秒查询一次任务（浏览器标签页不可见时暂停），
 * 完成时 toast 带「去下载」，失败时 toast 带失败原因。组件卸载后停止轮询（任务本身不受影响，仍可在导出中心查看）。
 *
 *   const { start } = useAsyncJob()
 *   await start(() => api.post<{ id: string }>('/users/export-async', null, { params: filters }))
 */
export function useAsyncJob() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [ids, setIds] = useState<string[]>([])

  const finish = useCallback(
    (job: Job) => {
      void qc.invalidateQueries({ queryKey: jobKeys.all })
      if (job.status === 'success') {
        toast.success(t('jobs.toast.done'), { action: { label: t('jobs.toast.goDownload'), onClick: () => navigate(CENTER_PATH) } })
      } else if (job.status === 'failed') {
        toast.error(job.error ? `${t('jobs.toast.failed')}: ${job.error}` : t('jobs.toast.failed'), {
          action: { label: t('jobs.toast.goCenter'), onClick: () => navigate(CENTER_PATH) },
        })
      }
    },
    [qc, t, navigate],
  )

  useEffect(() => {
    if (ids.length === 0) return
    let stopped = false
    const tick = async () => {
      if (document.hidden) return
      const gone: string[] = []
      for (const id of ids) {
        try {
          const job = await fetchJob(id, true)
          if (job.status !== 'queued' && job.status !== 'running') {
            gone.push(id)
            if (!stopped) finish(job)
          }
        } catch {
          gone.push(id) // 任务不存在（已被删除）或无权访问：不再跟踪
        }
      }
      if (!stopped && gone.length) setIds((cur) => cur.filter((x) => !gone.includes(x)))
    }
    const timer = setInterval(() => void tick(), POLL_MS)
    return () => {
      stopped = true
      clearInterval(timer)
    }
  }, [ids, finish])

  /** 创建任务并开始跟踪；create 失败时抛出（由调用方 toast），不会提示「已提交」 */
  const start = useCallback(
    async (create: () => Promise<{ id: string }>) => {
      const { id } = await create()
      void qc.invalidateQueries({ queryKey: jobKeys.all })
      toast.success(t('jobs.toast.submitted'), { action: { label: t('jobs.toast.goCenter'), onClick: () => navigate(CENTER_PATH) } })
      setIds((cur) => (cur.includes(id) ? cur : [...cur, id]))
      return id
    },
    [qc, t, navigate],
  )

  return { start, watching: ids.length }
}
