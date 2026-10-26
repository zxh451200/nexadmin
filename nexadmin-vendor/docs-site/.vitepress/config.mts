import { defineConfig } from 'vitepress'

// 官网插件市场地址：默认与文档站同域；独立域名部署时用 MARKET_URL 覆盖。
const MARKET_URL = process.env.MARKET_URL || '/plugins'

const nav = [
  { text: '文档', link: '/guide/' },
  { text: '插件开发', link: '/plugin-development' },
  { text: '插件市场', link: '/market' },
  { text: '授权查询', link: '/license-check' },
  { text: '购买授权', link: '/buy' },
  { text: '更新日志', link: '/changelog' },
]

export default defineConfig({
  title: 'NexAdmin',
  description: 'NexAdmin 框架与插件文档',
  cleanUrls: true,
  markdown: {
    config(md) {
      // 把 __market__ 占位链接替换成构建时的官网地址（默认 /plugins）。
      const renderLink = md.renderer.rules.link_open
      md.renderer.rules.link_open = (tokens, idx, options, env, self) => {
        const token = tokens[idx]
        if (token.attrGet('href') === '__market__') token.attrSet('href', MARKET_URL)
        return renderLink
          ? renderLink(tokens, idx, options, env, self)
          : self.renderToken(tokens, idx, options)
      }
    },
  },
  locales: {
    root: { label: '简体中文', lang: 'zh-CN', themeConfig: { nav } },
    // 预留英文站：内容放在 en/ 下，导航补齐后再启用语言切换以外的内容。
    en: {
      label: 'English',
      lang: 'en-US',
      link: '/en/',
      themeConfig: { nav: [{ text: 'Home', link: '/en/' }] },
    },
  },
})
