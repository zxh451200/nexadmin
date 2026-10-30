---
title: Media Capabilities
summary: Images (WebP derivatives), video hosting and embeds, and map picking — what ships in the framework, what lives in plugins, and how to roll it out.
---

# Media Capabilities

This page covers images, video and maps in NexAdmin: what ships in the framework, what lives in plugins, how operators enable it, and how developers use it.

## 1. Where each capability lives

| Capability | Location | Default | Depends on |
| --- | --- | --- | --- |
| WebP derivative on upload (original kept, served via `Accept`) | Framework (file module + uploader) | **Off** (switch in settings) | Nothing |
| Backfill WebP for existing images | Plugin `media` → Media optimizer → Images | — | Browser + `file:upload` permission |
| Video hosting (Cloudflare Stream / Bunny direct upload) | Plugin `media` → Media optimizer → Videos | Off | Provider account and keys |
| Video registration (Aliyun / Tencent / existing assets) | Same page | Off | Playback domain |
| Video embeds (Bilibili / YouTube / Cloudflare Stream) | Framework (rich text + both HTML sanitizers) | On | Nothing |
| Map picking and reverse geocoding | Framework `framework/map` + plugin `maps` | Off | Provider keys |

`media` and `maps` are **pro plugins**: without a license file their routes are not mounted, their tables are not created, and their menus stay hidden.

## 2. Install and license

1. Ship the plugin under `plugins/` (source plugins compile with the framework) or install a release package; make sure it is imported in `plugins/plugins.go`.
2. Import the license in Plugin Management. Unlicensed plugins fall through to the "plugin inactive" route and never create `plg_media_*` or `plg_maps_setting`.
3. Restart: migrations and menu seeding run at boot (menus are only added, never overwritten).
4. Grant permissions:

| Permission | Purpose |
| --- | --- |
| `media:optimize` | Media optimizer page and all of its APIs |
| `file:upload` | Uploading the WebP derivative during backfill |
| `map:config` | Map settings and the reverse-geocoding proxy |

## 3. Images

### 3.1 Behaviour

Settings → Upload limits → "Generate WebP copies of images".

- The original is **never overwritten**; a WebP copy is uploaded alongside it (browser-side, longest edge 2560, quality 0.82).
- The copy is discarded when it ends up larger than the original; GIF / SVG / WebP / AVIF are skipped.
- Delivery keeps the same URL and negotiates on `Accept: image/webp`, with `Vary: Accept`; older browsers get the original.
- Derivatives are hidden rows: they stay out of file lists and group counts, still count toward storage, are deleted with the original, and always inherit its visibility.

### 3.2 Backfill

Media optimizer → Images → "Generate all".

- Conversion runs in the administrator's browser (fetch → convert → upload). Keep the page open and work one page of 20 at a time.
- Only public, stored JPEG/PNG files without a copy are listed; private images and GIF/SVG are excluded.
- Failed rows stay as they are and can be retried; "saved this run" reports bytes saved in that pass.

### 3.3 Developer usage

- Nothing to change: every upload going through the framework uploader is covered.
- Custom uploads: `uploadToTus(file, endpoint, { kind: 'image', variantOf: originalId })`.
- Rendering: `<img src="/api/v1/files/{id}/content">` — browsers negotiate WebP automatically.

## 4. Video

### 4.1 Providers

| Provider | Direct upload | Playback URL | Fields |
| --- | --- | --- | --- |
| Cloudflare Stream | Yes (tus) | `customer-<sub>.cloudflarestream.com/<uid>/iframe` | Account ID / API token / playback subdomain |
| Bunny Stream | Yes (tus + signature) | `<pull zone>.b-cdn.net/<guid>/playlist.m3u8` | Library ID / API key / pull zone |
| Aliyun VOD | No, register only | `<domain>/<videoId>/index.m3u8` | Playback domain |
| Tencent VOD | No, register only | `<domain>/<fileId>.m3u8` | Playback domain |

Browsers upload straight to the provider; the server only signs and records. Videos never pass through local storage and are never transcoded here. Playback and download URLs are derived server-side from the provider id — client-supplied URLs are ignored.

### 4.2 Workflow

1. Media optimizer → Videos → enable → pick a provider → fill the fields → save.
2. Providers with direct upload show "Upload video"; every provider also offers "Register existing video" (paste the id and a display name).
3. The ledger offers "Copy MP4 link" (Cloudflare / Bunny only) and an inline player.
4. In content: Insert video → paste the MP4 link, or a Bilibili / YouTube / Cloudflare Stream embed URL.

### 4.3 Prerequisites and billing

- Cloudflare: enable "Allow MP4 downloads" or the direct link 404s; billed per stored and delivered minute.
- Bunny: enable MP4 Fallback in the library; billed per storage and traffic, with a trial period.
- Aliyun / Tencent: transcoding and CDN traffic are billed separately (Aliyun storage is free up to 50 GB); the playback domain needs ICP filing or you use the vendor's test domain.

## 5. Embeds

Only these iframe sources are allowed; every other iframe is dropped together with its content (identical rules on the client and the server):

- `https://www.youtube.com/embed/*`, `https://www.youtube-nocookie.com/embed/*`
- `https://player.bilibili.com/player.html`
- `https://customer-*.cloudflarestream.com/<uid>/iframe`

Pasting a Bilibili video page, a `watch?v=` link, a `youtu.be` short link or a Cloudflare player URL is normalised automatically. `http://`, arbitrary hosts, `javascript:` and `srcdoc` are rejected. The allowlist lives in Go, so rebuild the binary after changing it.

## 6. Maps

1. Install and activate `maps` → System → Map service, then enable providers and fill their keys:
   - AMap: JS API key (shipped to the browser) + web-service key (server-side reverse geocoding)
   - Baidu: browser AK + server AK (the server AK must have reverse geocoding enabled)
   - Google: API key (Maps JavaScript API + Geocoding API enabled)
2. Usage:

```tsx
import { LocationField, gcj02ToWgs84, type GeoPoint } from '@/framework/map'

const [point, setPoint] = useState<GeoPoint | null>(null)
<LocationField value={point} onChange={setPoint} label="Store location" />
```

- Without the plugin the field degrades to manual coordinates and never blocks the form.
- Coordinate systems follow the provider: AMap GCJ-02, Baidu BD-09, Google WGS-84; normalise with `gcj02ToWgs84` / `bd09ToGcj02` before storing.
- Reverse geocoding goes through the server proxy, so service keys never reach the browser.

## 7. Troubleshooting

| Symptom | Cause and fix |
| --- | --- |
| AMap returns `USERKEY_PLAT_NOMATCH` | A JS API key was used for a web-service call; create a Web service key |
| Baidu returns `240 APP 服务被禁用` | The AK lacks reverse geocoding, or it is a browser AK; enable the service or use a server AK |
| Direct MP4 link 404s | MP4 downloads disabled at Cloudflare / Bunny, or the Aliyun/Tencent asset is not transcoded |
| iframe dropped from content | Host not on the allowlist; it must be `https` with `/embed/`, `/player.html` or `/<uid>/iframe` |
| Media optimizer lists nothing | No public JPEG/PNG without a copy, or the plugin is unlicensed |
| Backfill stops midway | Page closed or network lost; reopen and continue — finished items are not repeated |

## 8. Rollout checklist

1. Licenses imported, menus visible, migrations applied.
2. WebP switch on as needed; backfill one page first, then everything.
3. Provider keys verified: upload URL issued, playback works, MP4 link copies.
4. Permissions assigned: `media:optimize`, `file:upload`, `map:config`.
5. Rebuild the binary (iframe allowlist and htmlsafe live in Go).
6. Tell customers the known limits: Aliyun/Tencent register only; image backfill runs in the admin's browser; transcoding is done by the external provider.
