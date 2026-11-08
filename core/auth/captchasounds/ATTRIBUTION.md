# captchasounds

数字音源（`zh|en/0-9.wav`）由 [Kokoro-82M](https://huggingface.co/hexgrad/kokoro)
（**Apache-2.0**）经 sherpa-onnx 的 `kokoro-multi-lang-v1_1` 中英双语导出模型本地离线
合成；**可随商业产品再分发**。生成脚本与再生成步骤同在本目录：`gen_kokoro.py`
（模型拉到 `~/.kokoro-models/`，不进仓库；音色 sid 见脚本内注释，0=af_maple 英女声、
3=zf_001 中女声，不满意换同段位 sid 重跑即可）。

格式：24 kHz、16-bit、单声道 PCM（44 字节标准 WAV 头），与
`core/auth/captcha_audio.go` 的 `audioSampleRate` 等常量一一对应，改一边要同步另一边。

打包语言只有 zh/en（其余界面语言由 `digitSound` 自动回落英文——语音只是无障碍兜底，
图形码不受影响）；数字读法有意避开易混音：zh 的 1 读「幺」（避开「一/七」韵母混淆，
电话报号惯例）。

历史出处（均已弃用，保留以备忘）：
- 最初音源取自 [dchest/captcha](https://github.com/dchest/captcha) v1.1.0 的
  `capgensounds/`（MIT）。8 kHz/8-bit espeak 老合成，"机器人嗓"明显。
- 后改用微软 Edge TTS 合成 5 语言音源——质量达标，但输出无再分发授权，商用不可带。
