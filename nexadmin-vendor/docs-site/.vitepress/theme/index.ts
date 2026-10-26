import DefaultTheme from 'vitepress/theme'
import type { Theme } from 'vitepress'
import LicenseCheck from '../components/LicenseCheck.vue'

export default {
  extends: DefaultTheme,
  enhanceApp({ app }) {
    app.component('LicenseCheck', LicenseCheck)
  },
} satisfies Theme
