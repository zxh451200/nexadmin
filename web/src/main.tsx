import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import '@fontsource-variable/inter'
import '@fontsource-variable/jetbrains-mono'
import './index.css'
import '@/framework/request/mock-loader'
import { initI18n } from '@/framework/i18n/init'
import '@/framework/i18n/zod'
import { installClientErrorReporter } from '@/framework/request/client-error'
import { installLabelCopy } from '@/framework/lib/label-copy'
import { watchBuildFreshness } from '@/framework/pwa/freshness'
import { registerServiceWorker } from '@/framework/pwa/register'
import App from './App'

initI18n()
installClientErrorReporter()
installLabelCopy()
registerServiceWorker()
watchBuildFreshness()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
