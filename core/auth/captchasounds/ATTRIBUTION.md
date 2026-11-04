# captchasounds

数字音源（0–9.wav）与提示音（beep.wav）取自
[dchest/captcha](https://github.com/dchest/captcha) v1.1.0 的 `capgensounds/`，
MIT 许可，Copyright (c) 2011-2014 Dmitry Chestnykh <dmitry@codingrobots.com>。

格式：8 kHz、8-bit、单声道、无符号 PCM（44 字节标准 WAV 头）。

本项目在此目录用 `go:embed` 嵌入这些音源，自行合成清晰的音频验证码
（数字间干净停顿、整串读两遍、无背景噪声混音），替代
dchest/captcha `NewAudio` 默认的"数字混入随机噪声"式输出。
