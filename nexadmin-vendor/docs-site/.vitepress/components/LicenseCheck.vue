<script setup lang="ts">
import { ref } from 'vue'
import pubkeys from '../../data/pubkeys.json'

interface Result { ok: boolean; message: string; fields?: [string, string][] }

const text = ref('')
const result = ref<Result | null>(null)
const busy = ref(false)

const b64 = (s: string) => Uint8Array.from(atob(s), (c) => c.charCodeAt(0))

async function kidOf(raw: Uint8Array) {
  const h = new Uint8Array(await crypto.subtle.digest('SHA-256', raw))
  return Array.from(h.slice(0, 4), (b) => b.toString(16).padStart(2, '0')).join('')
}

async function check() {
  result.value = null
  busy.value = true
  try {
    result.value = await verify(text.value)
  } finally {
    busy.value = false
  }
}

async function verify(raw: string): Promise<Result> {
  if (!globalThis.crypto?.subtle) {
    return { ok: false, message: '当前环境不支持 Web Crypto（需要 HTTPS 或 localhost）。请改用最新版 Chrome / Edge / Firefox / Safari，或使用命令行校验。' }
  }
  let env: { kid?: string; payload: string; signature: string }
  let payload: Uint8Array, sig: Uint8Array
  try {
    env = JSON.parse(raw)
    payload = b64(env.payload)
    sig = b64(env.signature)
  } catch {
    return { ok: false, message: '授权内容格式错误：请粘贴完整的 .lic 文件内容。' }
  }
  const cands = env.kid ? pubkeys.keys.filter((k) => k.kid === env.kid) : pubkeys.keys
  if (cands.length === 0) return { ok: false, message: `签发密钥 ${env.kid} 不在本页的公钥列表中。` }
  let verified = false
  for (const k of cands) {
    const pub = b64(k.pub)
    if ((await kidOf(pub)) !== k.kid) continue
    try {
      const key = await crypto.subtle.importKey('raw', pub, { name: 'Ed25519' }, false, ['verify'])
      if (await crypto.subtle.verify({ name: 'Ed25519' }, key, sig, payload)) { verified = true; break }
    } catch (e) {
      if (e instanceof DOMException && (e.name === 'NotSupportedError' || e.name === 'SyntaxError')) {
        return { ok: false, message: '当前浏览器不支持 Ed25519 验签。请升级到较新的 Chrome（137+）、Edge、Firefox（129+）或 Safari（17+），或使用命令行校验：nexadmin plugin check。' }
      }
      throw e
    }
  }
  if (!verified) return { ok: false, message: '签名校验失败：授权被篡改，或不是由列表中的公钥签发。' }
  const l = JSON.parse(new TextDecoder().decode(payload))
  const exp = new Date(l.expires_at)
  const expired = !(exp.getTime() > Date.now())
  return {
    ok: !expired,
    message: expired ? '签名有效，但授权已到期。' : '签名有效，授权在有效期内。',
    fields: [
      ['客户', l.customer], ['插件', l.plugin], ['签发时间', l.issued_at], ['到期时间', l.expires_at],
      ['域名', (l.domains ?? []).join(', ') || '不限'],
    ],
  }
}
</script>

<template>
  <div class="lc">
    <p class="note" role="note">示例：当前使用的是开发公钥（{{ pubkeys.keys.map((k) => k.kid).join(', ') }}），仅可验证开发环境签发的授权；正式公钥发布后会替换。</p>
    <label for="lic-input">授权文件内容</label>
    <textarea id="lic-input" v-model="text" rows="10" spellcheck="false" placeholder='{"kid": "...", "payload": "...", "signature": "..."}'></textarea>
    <button type="button" :disabled="busy || !text.trim()" @click="check">验证</button>
    <div v-if="result" class="out" :data-ok="result.ok" role="status">
      <p>{{ result.message }}</p>
      <dl v-if="result.fields">
        <template v-for="[k, v] in result.fields" :key="k"><dt>{{ k }}</dt><dd>{{ v }}</dd></template>
      </dl>
    </div>
  </div>
</template>

<style scoped>
.lc { display: grid; gap: 10px; margin: 16px 0; }
.note { font-size: 13px; padding: 8px 12px; border-radius: 6px; background: var(--vp-custom-block-warning-bg); color: var(--vp-custom-block-warning-text); margin: 0; }
textarea { width: 100%; font: 13px/1.5 var(--vp-font-family-mono); padding: 10px; border: 1px solid var(--vp-c-divider); border-radius: 6px; background: var(--vp-c-bg-soft); color: var(--vp-c-text-1); }
button { justify-self: start; padding: 8px 18px; border: 0; border-radius: 6px; background: var(--vp-c-brand-3); color: var(--vp-button-brand-text); cursor: pointer; font: inherit; }
button:disabled { opacity: .5; cursor: not-allowed; }
.out { padding: 12px 16px; border-radius: 8px; border: 1px solid var(--vp-c-divider); }
.out[data-ok='true'] { border-color: var(--vp-c-green-1); }
.out[data-ok='false'] { border-color: var(--vp-c-red-1); }
.out p { margin: 0 0 8px; }
dl { display: grid; grid-template-columns: max-content 1fr; gap: 4px 16px; margin: 0; font-size: 14px; }
dt { color: var(--vp-c-text-3); }
dd { margin: 0; word-break: break-all; }
</style>
