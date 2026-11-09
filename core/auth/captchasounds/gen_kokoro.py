# captchasounds 数字音源生成脚本（assets provenance）。
#
# 源：Kokoro-82M（Apache-2.0，hexgrad/kokoro），用 sherpa-onnx 的
# kokoro-multi-lang-v1_1 中英双语导出模型本地离线合成——**商用可再分发**，
# 替代此前无再分发授权的 Edge TTS 音频（后者仅限本机试听，禁止提交发布）。
# 打包语言只有 zh/en；其余界面语言由后端 digitSound 自动回落英文。
#
# 在家（有网络）跑一次即可：
#   pip install sherpa-onnx soundfile numpy huggingface_hub
#   python gen_kokoro.py
# 模型（~310MB，model.onnx + lexicon/voices 等）经 hf-mirror 拉到
# ~/.kokoro-models/kokoro-multi-lang-v1_1/（不进仓库；可用 KOKORO_MODEL_DIR 重定向）。
# 产出：24 kHz / 16-bit / 单声道 PCM WAV，与 core/auth/captcha_audio.go 常量对应。
#
# 各语言读法用标准数字读法（zh 的 1 读「一」）；en 用标准 zero..nine。

import os
import pathlib

import numpy as np
import soundfile as sf

HERE = pathlib.Path(__file__).parent
# 模型默认放 ~/.kokoro-models（不进仓库树；缓存盘吃紧时可用 KOKORO_MODEL_DIR 重定向）。
MODEL_DIR = pathlib.Path(
    os.environ.get("KOKORO_MODEL_DIR", pathlib.Path.home() / ".kokoro-models" / "kokoro-multi-lang-v1_1")
)
SAMPLE_RATE = 24000
PEAK = 0.70

# 数字读法。zh 走中文词，en 走英文词；kokoro 多语种模型按文本自动分派 G2P。
DIGITS = {
    "zh": ["零", "一", "二", "三", "四", "五", "六", "七", "八", "九"],
    "en": ["zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"],
}

# 音色 sid（sherpa-onnx k2-fsa PR #1942 的顺序：英文音色在前、中文从 3 起）。
# 由 voice_scan 全量实测选出：zh sid 8 信噪比 165dB、语内嘶声 0%、响度与语速正常
# （原 zf_001=sid3 的语内底噪比它高约三个数量级）；en sid 2（six 不会被读成 sex，
# 5/6/7/8 用 speed=1.1 时长更利落）。不满意就跑 D:\tmp\voice_scan.py 按数据换 sid。
VOICE_SID = {"zh": 8, "en": 2}


def ensure_model():
    if (MODEL_DIR / "model.onnx").is_file():
        return
    from huggingface_hub import snapshot_download

    snapshot_download(
        repo_id="csukuangfj/kokoro-multi-lang-v1_1",
        local_dir=str(MODEL_DIR),
        endpoint=os.environ.get("HF_ENDPOINT", "https://hf-mirror.com"),
    )


def build_tts():
    import sherpa_onnx

    cfg = sherpa_onnx.OfflineTtsConfig(
        model=sherpa_onnx.OfflineTtsModelConfig(
            kokoro=sherpa_onnx.OfflineTtsKokoroModelConfig(
                model=str(MODEL_DIR / "model.onnx"),
                voices=str(MODEL_DIR / "voices.bin"),
                tokens=str(MODEL_DIR / "tokens.txt"),
                lexicon=str(MODEL_DIR / "lexicon-us-en.txt")
                + ","
                + str(MODEL_DIR / "lexicon-zh.txt"),
                data_dir=str(MODEL_DIR / "espeak-ng-data"),
            ),
            num_threads=2,
            debug=False,
        ),
    )
    return sherpa_onnx.OfflineTts(cfg)


def shape(pcm: np.ndarray) -> np.ndarray:
    """去首尾静音 → 归一响度 → 淡入淡出（与旧管线同口径，Go 端拼接逻辑零改动）。

    底噪治理走根修：gen_kokoro 的音色 sid 由 voice_scan 实测信噪比/嘶声选出
    （候选音色的静音段与语内背景都接近数字零，无需任何滤波）；若日后换到
    带底噪的音色，优先换 sid 而不是在这里加滤波补丁。
    """
    pcm = np.asarray(pcm, dtype=np.float64).flatten()
    loud = np.flatnonzero(np.abs(pcm) > 0.004)
    if loud.size == 0:
        raise RuntimeError("音频全静音，疑似合成失败")
    head = max(0, loud[0] - int(0.010 * SAMPLE_RATE))
    tail = min(len(pcm), loud[-1] + int(0.080 * SAMPLE_RATE))
    pcm = pcm[head:tail]
    pcm *= PEAK / np.max(np.abs(pcm))
    fin, fout = int(0.005 * SAMPLE_RATE), int(0.015 * SAMPLE_RATE)
    pcm[:fin] *= np.linspace(0, 1, fin)
    pcm[-fout:] *= np.linspace(1, 0, fout)
    return pcm


def main():
    ensure_model()
    tts = build_tts()
    for lang, words in DIGITS.items():
        for d, text in enumerate(words):
            audio = tts.generate(text, sid=VOICE_SID[lang], speed=1.0)
            pcm = np.asarray(audio.samples, dtype=np.float64)
            if audio.sample_rate != SAMPLE_RATE:
                raise RuntimeError(f"{lang}/{d}: 采样率 {audio.sample_rate} != {SAMPLE_RATE}")
            sf.write(HERE / lang / f"{d}.wav", shape(pcm), SAMPLE_RATE, subtype="PCM_16")
            print(f"{lang}/{d}.wav  {text}  {len(shape(pcm)) / SAMPLE_RATE:.2f}s")


if __name__ == "__main__":
    main()
