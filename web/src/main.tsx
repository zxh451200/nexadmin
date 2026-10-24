import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import '@fontsource-variable/inter'
import '@fontsource-variable/jetbrains-mono'
import './index.css'
import '@/framework/request/mock-loader'
import { initI18n } from '@/framework/i18n/init'
import { installClientErrorReporter } from '@/framework/request/client-error'
import { registerServiceWorker } from '@/framework/pwa/register'
import App from './App'

initI18n()
installClientErrorReporter()
registerServiceWorker()

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
