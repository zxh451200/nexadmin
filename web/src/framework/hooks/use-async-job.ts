import { useCallback, useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import { downloadJobFile, fetchJob, jobKeys, type Job } from '@/framework/modules/exportcenter/api'

const POLL_MS = 2000
const CENTER_PATH = '/jobs'

/**
 * 异步任务通用流程：创建 → 轮询 → 完成提示。
 * 创建成功后提示「已提交到导出中心」（可跳转），然后每 2 秒查询一次任务（浏览器标签页不可见时暂停），
 * 成功时自动下载结果文件（失败兜底：toast 的「去下载」进导出中心手动取），失败时 toast 带失败原因。
 * 组件卸载后停止轮询（任务本身不受影响，仍可在导出中心查看）。
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
      // 任务在「已提交」toast 还挂着时就完成了（快任务第一次轮询即结束）：
      // 先撤掉那条，只留结果提示，不叠两条
      toast.dismiss(`job-${job.id}`)
      if (job.status === 'success') {
        // 完成即自动下载：结果文件要带鉴权头，走 blob 拉取再触发保存（与导出中心同一入口）。
        // 轮询回调里没有用户手势，浏览器对单个自动下载不拦；同批多次导出可能弹「允许多个下载」确认条，用户放行一次即可。
        // 自动下载失败（文件已清理/权限变动）不打断结果提示，toast 的「去下载」仍可进导出中心手动取。
        if (job.downloadable) void downloadJobFile(job).catch(() => {})
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
      // id 与任务绑定：finish 时能按 id 撤掉未消失的「已提交」，避免快任务叠两条提示
      toast.success(t('jobs.toast.submitted'), {
        id: `job-${id}`,
        action: { label: t('jobs.toast.goCenter'), onClick: () => navigate(CENTER_PATH) },
      })
      setIds((cur) => (cur.includes(id) ? cur : [...cur, id]))
      return id
    },
    [qc, t, navigate],
  )

  return { start, watching: ids.length }
}
