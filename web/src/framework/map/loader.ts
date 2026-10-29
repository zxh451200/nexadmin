/**
 * 加载插件注册的地图 provider。
 * 约定：插件把注册代码放在 web/src/plugins/<id>/map-provider.ts，模块顶层调用
 * registerMapProvider；这里在首屏之外同步求值，保证用到 LocationField 时注册表已就绪。
 */
import.meta.glob(['/src/plugins/*/map-provider.ts', '/src/pages/*/map-provider.ts'], { eager: true })

export {}
