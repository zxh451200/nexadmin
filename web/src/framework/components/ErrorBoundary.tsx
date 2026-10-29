import { Component, type ErrorInfo, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { useLocation } from 'react-router-dom'
import { reportClientError } from '@/framework/request/client-error'
import { Button } from '@/framework/ui/button'

function Fallback() {
  const { t } = useTranslation()
  return (
    <div className="flex min-h-[60vh] flex-col items-center justify-center gap-3 p-6 text-center" role="alert">
      <h1 className="text-xl font-semibold text-foreground">{t('error.boundary.title')}</h1>
      <p className="max-w-md text-sm text-muted-foreground">{t('error.boundary.desc')}</p>
      <div className="mt-2 flex flex-wrap items-center justify-center gap-2">
        {/* 只留整页刷新：渲染期错误基本都是确定性的，单纯重置状态会当场再崩一次，
            切到别的标签再回来本身也会自动重试（见 resetKey）。 */}
        <Button type="button" onClick={() => window.location.reload()}>
          {t('error.boundary.reload')}
        </Button>
      </div>
    </div>
  )
}

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
    if (this.state.failed) return <Fallback />
    return this.props.children
  }
}

/** 放在 Router 内：随路径重置 */
export function AppErrorBoundary({ children }: { children: ReactNode }) {
  const { pathname } = useLocation()
  return <Boundary resetKey={pathname}>{children}</Boundary>
}
