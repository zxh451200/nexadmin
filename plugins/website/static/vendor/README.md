# 第三方前端组件（按原样内置）

这里的文件都是上游发布包里的原文件，未做修改，随插件 `go:embed` 一起打进二进制，
由 `/website-assets/vendor/...` 提供，不依赖 CDN、离线可用。升级时整包替换并同步
版本号（`site.js` 里的 `?v=` 查询串与下面的表格）。

| 目录 | 组件 | 版本 | 许可 | 用途 | 上游 |
| --- | --- | --- | --- | --- | --- |
| `viewerjs/` | Viewer.js | 1.15.2 | MIT | 商品图 / 正文图的全屏查看器：拖拽、滚轮/双指缩放、旋转、翻转、缩略图导航、键盘操作 | https://github.com/fengyuanchen/viewerjs |

## 接入方式

- Viewer.js 是**按需加载**：第一次点图时才注入脚本和样式。首屏不付这份体积
  （JS 49KB、CSS 14KB，压缩后）。资源与站点同一个二进制，离线可用。
- 商品主图的悬停放大**不走第三方库**：`site.js` 的 `initGalleryZoom` 自己算
  `transform-origin` + `scale`，舞台 `overflow:hidden` 裁切。侧边放大面板（Drift 那类）
  要手动维护面板坐标，窄屏/弹层/滚动容器都得重算；原地放大交给 CSS 即可。
  触屏没有 hover，点图进 Viewer.js 用双指缩放、旋转、翻转。
- `openLb` 把商品主图、缩略图、正文图临时铺到视口外的宿主容器，交给 Viewer.js 连成一组；
  关闭时销毁实例并移除宿主，不留 DOM。
- Viewer.js 1.x 的按钮是纯图形、提示文案固定英文，组件没有 locale 选项；
  `viewerLabels()` 在 shown 后补 `aria-label/title`，文案走本站双语文案。
- 升级时先看 `plugins/website/static/site.js` 的 `ensureViewer` / `openLb` / `viewerLabels`。

## 为什么从 PhotoSwipe 换成 Viewer.js

PhotoSwipe 5.4.4 同为 MIT、25.3k stars，但最后一次发版是 2024-05，173 个开放 issue；
Viewer.js 1.15.2 是 MIT、8.2k stars，2026-10 仍在提交并发版，开放 issue 仅 1 个，
且多了旋转、翻转、缩略图导航、自动播放、放大镜等能力。两者都免授权，可商用。
