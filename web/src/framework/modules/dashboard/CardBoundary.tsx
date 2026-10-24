import { Component, type ErrorInfo, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { reportClientError } from '@/framework/request/client-error'
import { Button } from '@/framework/ui/button'
import { Card, CardContent } from '@/framework/ui/card'

function Fallback({ onRetry }: { onRetry: () => void }) {
  const { t } = useTranslation()
  return (
    <Card role="alert">
      <CardContent className="flex flex-wrap items-center justify-between gap-3 p-4 text-sm text-muted-foreground">
        <span>{t('dashboard.cardFailed')}</span>
        <Button type="button" variant="outline" size="sm" onClick={onRetry}>
          {t('dashboard.cardRetry')}
        </Button>
      </CardContent>
    </Card>
  )
}

interface State {
  failed: boolean
}

/** 单张卡片的错误边界：某张卡片（尤其是插件/业务卡片）渲染出错不拖垮整个工作台 */
export class CardBoundary extends Component<{ children: ReactNode }, State> {
  state: State = { failed: false }

  static getDerivedStateFromError(): State {
    return { failed: true }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    reportClientError(error, { componentStack: info.componentStack ?? undefined })
  }

  render() {
    if (this.state.failed) return <Fallback onRetry={() => this.setState({ failed: false })} />
    return this.props.children
  }
}
