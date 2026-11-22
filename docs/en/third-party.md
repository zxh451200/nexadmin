---
title: Third-party open-source components
summary: Third-party UI and asset components bundled by the framework, their licenses and sources — check this page when upgrading dependencies.
---

# Third-party open-source components

Third-party UI and asset components brought in beyond the framework's own dependencies. Each entry links to its upstream license text; re-check this page when upgrading dependencies.

## Login slider CAPTCHA (core/auth, web login page)

| Component | License | Source | Use |
| --- | --- | --- | --- |
| [go-captcha v2](https://github.com/wenlng/go-captcha) | Apache-2.0 | Go module `github.com/wenlng/go-captcha/v2` | Slider-puzzle rendering: real photo background plus a shadowed/masked puzzle piece (`core/auth/slider_render_photo.go`) |
| [go-captcha-assets v1.0.7](https://github.com/wenlng/go-captcha-assets) | Apache-2.0 | Go module `github.com/wenlng/go-captcha-assets` | Embedded assets: 8 photos and 4 puzzle shapes (a subset is bundled to keep the binary small) |
| [rc-slider-captcha v1.9](https://github.com/caijf/rc-slider-captcha) | MIT | npm `rc-slider-captcha` | Login-page drag interaction (`web/src/framework/pages/login/CaptchaField.tsx`) |

Where they are wired and the contract:

- Go's `renderSliderPhoto` and the hand-drawn `renderSlider` share the same signature and contract (background / cut-out data URIs; the hole's X coordinate stays server-side); if asset initialization fails it falls back to the hand-drawn version.
- The frontend `rc-slider-captcha` only handles drag interaction and animation; verification still goes through our own endpoint `/auth/slider/verify` (x tolerance + trajectory behaviour checks).
- go-captcha is Apache-2.0: the upstream LICENSE ships with the module (in the Go module cache, `go-captcha@v2.0.5/LICENSE` and `go-captcha-assets@v1.0.7/LICENSE`); this page is the attribution notice.
