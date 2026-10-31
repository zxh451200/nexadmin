import { BrowserRouter } from 'react-router-dom'
import { QueryClientProvider } from '@tanstack/react-query'
import { I18nextProvider } from 'react-i18next'
import { AppErrorBoundary } from '@/framework/components/ErrorBoundary'
import i18n from '@/framework/i18n/init'
import { queryClient } from '@/framework/query/client'
import { AppRoutes } from '@/framework/router/AppRoutes'
import { ADMIN_BASE } from '@/framework/router/base'
import { SetupGate } from '@/framework/setup/SetupGate'
import { ThemeProvider } from '@/framework/theme/ThemeProvider'
import { Toaster } from '@/framework/ui/sonner'
import { TooltipProvider } from '@/framework/ui/tooltip'

export default function App() {
  return (
    <I18nextProvider i18n={i18n}>
      <ThemeProvider>
        <QueryClientProvider client={queryClient}>
          <TooltipProvider delayDuration={200}>
            <BrowserRouter basename={ADMIN_BASE} future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
              <AppErrorBoundary>
                <SetupGate>
                  <AppRoutes />
                </SetupGate>
              </AppErrorBoundary>
            </BrowserRouter>
            <Toaster position="top-center" richColors closeButton />
          </TooltipProvider>
        </QueryClientProvider>
      </ThemeProvider>
    </I18nextProvider>
  )
}
