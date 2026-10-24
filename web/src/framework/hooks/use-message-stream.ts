import { useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { tokenStore, useMock } from '@/framework/request/http'

export const FALLBACK_POLL_MS = 30_000
const MAX_FAILS = 3
const BASE_DELAY_MS = 1_000
const MAX_DELAY_MS = 60_000

interface UnreadCount {
  messages: number
  notices: number
}

/** 同时更新顶栏铃铛（['notify']）与消息中心（['message']）的未读数缓存 */
const UNREAD_KEYS = [
  ['notify', 'unread-count'],
  ['message', 'unread-count'],
] as const

/** 解析一个 SSE 事件块，返回 event 名与拼接后的 data；纯注释（心跳）块返回 null */
function parseBlock(block: string): { event: string; data: string } | null {
  let event = 'message'
  const data: string[] = []
  for (const line of block.split('\n')) {
    if (line.startsWith(':')) continue
    if (line.startsWith('event:')) event = line.slice(6).trim()
    else if (line.startsWith('data:')) data.push(line.slice(5).replace(/^ /, ''))
  }
  return data.length > 0 ? { event, data: data.join('\n') } : null
}

/**
 * 订阅 GET /messages/stream（SSE）推送的未读数变化并写入 query 缓存。
 * EventSource 不能带 Authorization 头，所以用 fetch + ReadableStream 读取。
 * 断线后指数退避重连；连续失败 3 次退化为 30 秒轮询（仍在后台继续尝试重连，成功后恢复推送）。
 * mock 模式不连 SSE。返回值是未读数 query 应使用的 refetchInterval。
 */
export function useMessageStream(): number | false {
  const qc = useQueryClient()
  const [polling, setPolling] = useState(true)

  useEffect(() => {
    if (useMock) return
    const ctrl = new AbortController()
    let timer: number | undefined
    let fails = 0

    const retry = () => {
      if (ctrl.signal.aborted) return
      fails += 1
      if (fails >= MAX_FAILS) setPolling(true)
      const delay = Math.min(BASE_DELAY_MS * 2 ** (fails - 1), MAX_DELAY_MS)
      timer = window.setTimeout(() => void connect(), delay)
    }

    const connect = async () => {
      let gotEvent = false
      try {
        const base = import.meta.env.VITE_API_BASE || '/api/v1'
        const headers: Record<string, string> = { Accept: 'text/event-stream' }
        const token = tokenStore.access
        if (token) headers.Authorization = `Bearer ${token}`
        const lang = localStorage.getItem('nex-lang')
        if (lang) headers['Accept-Language'] = lang
        const res = await fetch(`${base}/messages/stream`, { headers, signal: ctrl.signal })
        if (!res.ok || !res.body) throw new Error(`stream ${res.status}`)

        const reader = res.body.getReader()
        const decoder = new TextDecoder()
        let buf = ''
        for (;;) {
          const { done, value } = await reader.read()
          if (done) break
          buf += decoder.decode(value, { stream: true }).replace(/\r\n/g, '\n')
          let idx: number
          while ((idx = buf.indexOf('\n\n')) >= 0) {
            const msg = parseBlock(buf.slice(0, idx))
            buf = buf.slice(idx + 2)
            if (msg?.event !== 'unread') continue
            const count = JSON.parse(msg.data) as UnreadCount
            for (const key of UNREAD_KEYS) qc.setQueryData(key, count)
            if (!gotEvent) {
              gotEvent = true
              fails = 0
              setPolling(false)
            }
          }
        }
      } catch {
        // 主动取消（卸载）不算失败；其余错误走重连
      }
      if (ctrl.signal.aborted) return
      if (gotEvent) fails = 0
      retry()
    }

    void connect()
    return () => {
      ctrl.abort()
      window.clearTimeout(timer)
    }
  }, [qc])

  return useMock || polling ? FALLBACK_POLL_MS : false
}
