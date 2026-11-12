import { Component, type ErrorInfo, type ReactNode } from 'react'
import { useLocation } from 'react-router-dom'
import { reportClientError } from '@/framework/request/client-error'
import ServerErrorPage from '@/framework/pages/ServerErrorPage'

interface Props {
  children: ReactNode
  /** 变化时清除错误状态（路由切换后给页面重新渲染的机会） */
  resetKey?: string
}

interface State {
  failed: boolean
}

/** 捕获渲染期错误：上报（脱敏、节流在 reportClientError 里）并显示兜底界面 */
class Boundary extends Component<Props, State> {
  state: State = { failed: false }

  static getDerivedStateFromError(): State {
    return { failed: true }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    reportClientError(error, { componentStack: info.componentStack ?? undefined })
  }

  componentDidUpdate(prev: Props) {
    if (this.state.failed && prev.resetKey !== this.props.resetKey) this.setState({ failed: false })
  }

  render() {
    // 兜底用准备好的 500 整页错误页：渲染期错误基本都是确定性的，切到别的标签再回来
    // 本身也会自动重试（见 resetKey），所以不需要单独的「重试」动作。
    if (this.state.failed) return <ServerErrorPage />
    return this.props.children
  }
}

/** 放在 Router 内：随路径重置 */
export function AppErrorBoundary({ children }: { children: ReactNode }) {
  const { pathname } = useLocation()
  return <Boundary resetKey={pathname}>{children}</Boundary>
}
