import 'i18next'

// 语言包是运行时从后端/插件加载的（src/framework/i18n/init.ts 的 import.meta.glob），
// 没有可静态检查的 key 集合；v27 默认开启的严格插值检查在这里只能作用于 string 动态键，
// 无法真正校验，反而把「把 t 当函数值传给组件」的写法全部判红。保持 v26 的类型行为。
declare module 'i18next' {
  interface CustomTypeOptions {
    strictInterpolationChecks: false
  }
}
