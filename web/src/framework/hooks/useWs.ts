// useWs 是框架通用 WebSocket hook:自动带 token 鉴权、断线指数退避重连、
// 按主题订阅。所有插件的实时推送共用一条 WS 连接(框架 boot 时已挂 /ws 端点)。
//
// 用法:
//   const { status, subscribe } = useWs()
//   useEffect(() => subscribe('im:CONV123', (data) => { ... }), [subscribe])
//
// 状态:connecting | open | closed | error

import { useCallback, useEffect, useState } from 'react'

import { api } from '@/framework/request/http'

export type WsStatus = 'connecting' | 'open' | 'closed' | 'error'

export interface WsFrame {
  type: 'subscribe' | 'unsubscribe' | 'ping' | 'pong' | 'message' | 'error'
  topic?: string
  data?: unknown
}

// 进程级单例:多个组件 useWs 共享同一条连接。
let wsInstance: WebSocket | null = null
let wsStatus: WsStatus = 'closed'
const statusListeners = new Set<(s: WsStatus) => void>()
const topicHandlers = new Map<string, Set<(data: unknown) => void>>()
let reconnectDelay = 1000
let reconnectTimer: number | null = null
let pingTimer: number | null = null

function setStatus(s: WsStatus) {
  wsStatus = s
  statusListeners.forEach((fn) => fn(s))
}

let connectInFlight = false

// 建连前先换一张一次性入场票：WebSocket 的握手 URL 会进反代日志与浏览器网络面板，
// 长寿命访问令牌不能出现在那里。
async function connect() {
  if (wsInstance && (wsInstance.readyState === WebSocket.OPEN || wsInstance.readyState === WebSocket.CONNECTING)) {
    return
  }
  if (connectInFlight) {
    return
  }
  connectInFlight = true
  setStatus('connecting')
  let ticket = ''
  try {
    ticket = (await api.post<{ ticket: string }>('/ws/ticket', undefined, { silent: true })).ticket
  } catch {
    // 换票失败多半是登录态失效；http 层会统一清会话并跳登录，这里只按退避重试。
    connectInFlight = false
    setStatus('error')
    scheduleReconnect()
    return
  }
  connectInFlight = false
  if (wsInstance && (wsInstance.readyState === WebSocket.OPEN || wsInstance.readyState === WebSocket.CONNECTING)) {
    return
  }
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const url = `${proto}//${window.location.host}/ws?ticket=${encodeURIComponent(ticket)}`
  try {
    wsInstance = new WebSocket(url)
  } catch {
    setStatus('error')
    scheduleReconnect()
    return
  }

  wsInstance.onopen = () => {
    setStatus('open')
    reconnectDelay = 1000 // 重置退避
    // 重连后重新订阅全部主题
    topicHandlers.forEach((_handlers, topic) => {
      wsInstance?.send(JSON.stringify({ type: 'subscribe', topic }))
    })
    // 应用层心跳(协议层 ping 由服务端发)
    if (pingTimer) window.clearInterval(pingTimer)
    pingTimer = window.setInterval(() => {
      if (wsInstance?.readyState === WebSocket.OPEN) {
        wsInstance.send(JSON.stringify({ type: 'ping' }))
      }
    }, 30_000)
  }

  wsInstance.onmessage = (ev) => {
    try {
      const frame: WsFrame = JSON.parse(ev.data)
      if (frame.type === 'message' && frame.topic) {
        topicHandlers.get(frame.topic)?.forEach((fn) => fn(frame.data))
      }
    } catch {
      // 非 JSON 帧忽略
    }
  }

  wsInstance.onclose = () => {
    setStatus('closed')
    if (pingTimer) window.clearInterval(pingTimer)
    if (topicHandlers.size > 0) {
      scheduleReconnect() // 有订阅者才重连
    }
  }

  wsInstance.onerror = () => {
    setStatus('error')
  }
}

function scheduleReconnect() {
  if (reconnectTimer) return
  reconnectTimer = window.setTimeout(() => {
    reconnectTimer = null
    reconnectDelay = Math.min(reconnectDelay * 2, 30_000)
    void connect()
  }, reconnectDelay)
}

function sendSubscribe(topic: string) {
  if (wsInstance?.readyState === WebSocket.OPEN) {
    wsInstance.send(JSON.stringify({ type: 'subscribe', topic }))
  }
}

function sendUnsubscribe(topic: string) {
  if (wsInstance?.readyState === WebSocket.OPEN) {
    wsInstance.send(JSON.stringify({ type: 'unsubscribe', topic }))
  }
}

// useWs 返回连接状态与订阅/退订函数。
export function useWs() {
  const [status, setStatusState] = useState<WsStatus>(wsStatus)

  useEffect(() => {
    const listener = (s: WsStatus) => setStatusState(s)
    statusListeners.add(listener)
    void connect()
    return () => {
      statusListeners.delete(listener)
      // 最后一个组件卸载时断开(无订阅者的连接没有意义)
      if (statusListeners.size === 0 && topicHandlers.size === 0) {
        wsInstance?.close()
        wsInstance = null
        setStatus('closed')
      }
    }
  }, [])

  // subscribe 返回清理函数(组件卸载时自动退订)。
  const subscribe = useCallback((topic: string, handler: (data: unknown) => void) => {
    let set = topicHandlers.get(topic)
    if (!set) {
      set = new Set()
      topicHandlers.set(topic, set)
      sendSubscribe(topic)
    }
    set.add(handler)
    return () => {
      set?.delete(handler)
      if (set && set.size === 0) {
        topicHandlers.delete(topic)
        sendUnsubscribe(topic)
      }
    }
  }, [])

  return { status, subscribe }
}
