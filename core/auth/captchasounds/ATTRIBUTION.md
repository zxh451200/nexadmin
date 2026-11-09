# captchasounds

中文数字（`zh/0-9.wav`）由 **CosyVoice2-0.5B**（[FunAudioLLM](https://huggingface.co/FunAudioLLM/CosyVoice2-0.5B)，
**Apache-2.0**）零样本模式本地离线合成，参考音频由 **Kokoro-82M**（**Apache-2.0**）zh sid8
合成；英文数字（`en/0-9.wav`）直接由 **Kokoro-82M** en sid2 合成——英文是它的原生语言，
比零样本克隆更干净。许可链全程可随商业产品再分发，**运行时不依赖任何模型**：
生成脚本与音源再生成方法同在本目录 `gen_cosyvoice.py` / `gen_kokoro.py`（模型经 hf-mirror
拉到 `~/.cosyvoice-models/`、`~/.kokoro-models/`，不进仓库；环境与跑法见脚本头注释）。

格式：24 kHz、16-bit、单声道 PCM（44 字节标准 WAV 头），与
`core/auth/captcha_audio.go` 的 `audioSampleRate` 等常量一一对应，改一边要同步另一边。

打包语言只有 zh/en（其余界面语言由 `digitSound` 自动回落英文——语音只是无障碍兜底，
图形码不受影响）；数字按标准读法朗读（zh 的 1 读「一」，不用电话报号的「幺」）。

当前 zh 成品（2026-11-09）的 take 选择与后处理：
- 1/2/3/5 用正式数字「壹/贰/叁/伍」单字合成——普通「一/二/三」的单字 take 会读出降调或怪音；
- 5 于 2026-11-09 晚重做：旧成品从整串合成中切出，起音是硬切（前 20 ms 直接跳到满音量，
  「w」的过渡没了）、尾部又从 175 Hz 一路冲到 264 Hz，听感像带问号的上扬；现用「伍」
  单字 take（HNR 19 dB、jitter 1.0%），PSOLA 由 165 抬到 184 Hz，不做高频提亮；
- 9 同一晚重做：旧成品的三声被平滑 + 底部压到 180 Hz 抹成一条直线（191→180→196，
  150 ms 内基频恒定）听不出调值；现用「九，」take（HNR 17.8 dB、jitter 1.4%），
  三声 187→167→226，归一到 185 Hz，不再平滑、不再压底、不提亮；
- 其余为单字合成；全表按声调分组做 PSOLA 音高归一（一声≈224、二声≈182、三声≈184、
  四声≈265 Hz），再统一响度到 -15 dBFS——否则同一串里各数字的基频能差上百赫兹，
  听起来像几个人轮流念。分组目标写在 `gen_cosyvoice.py` 的 `TONE_TARGET` 里。

当前 en 成品（2026-11-09）：Kokoro sid2；5/6/7/8 用 speed=1.1（seven 从 1.01s 降到 0.63s，
连续两个 7 不再拖沓）；响度统一 -15 dBFS。注意部分 Kokoro 英文音色会把 six 读成 sex，
换音色后必须逐字 ASR 复听。

音源选型实测结论（换音源前先看，别走回头路）：
- Kokoro 直出：底噪可干净（音色由 voice_scan 全量实测选出），但数字韵律差；
- MeloTTS / ZipVoice：单字与短文本的时长建模失败（空音频或黏连）；
- CosyVoice-300M-SFT：真录音音色但 v1 hift 声码器底噪大（SNR 27-36dB，可闻嘶声）；
- CosyVoice2 零样本：单字时长健康（~0.4s）、底噪数字零（SNR 89-104dB）——采用。

重新生成后必须逐字校验（TTS take 有随机性，历史上出过「八→爸」「五→如」「一→嘤」）：
声调曲线、单字时长、ASR 复听、尾音是否被截断；同批 take 的基频中位数也要接近，
否则一串数字会像几个人轮流念。

历史出处（均已弃用，保留以备忘）：
- 最初音源取自 [dchest/captcha](https://github.com/dchest/captcha) v1.1.0 的
  `capgensounds/`（MIT）。8 kHz/8-bit espeak 老合成，"机器人嗓"明显。
- 后改用微软 Edge TTS 合成 5 语言音源——质量达标，但输出无再分发授权，商用不可带。
