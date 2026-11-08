# captchasounds 数字音源生成脚本（assets provenance）。
#
# 源：CosyVoice2-0.5B（Apache-2.0，FunAudioLLM），零样本模式合成——
# 参考音频由 Kokoro-82M（Apache-2.0）的实测最干净音色（zh sid8 / en sid1 af_sol）
# 生成，许可链全程可商用。模型与本脚本只在**再生成资产时**使用，运行时不依赖。
#
# 为什么是 CosyVoice2（实测结论，别再走回头路）：
#   - Kokoro 直出：底噪干净（选对 sid）但数字韵律差（"读的效果太差"）；
#   - MeloTTS / ZipVoice：单字/短文本时长建模失败（空音频或黏成一坨）；
#   - CosyVoice-300M-SFT：真录音音色但 v1 hift 声码器底噪大（SNR 27-32dB）；
#   - CosyVoice2 零样本：单字时长健康（~0.4s）、底噪数字零（SNR 89-104dB）。
#
# 环境准备（一次性，模型不进仓库）：
#   uv venv ~/.cosyvoice-venv --python 3.12
#   uv pip install --python ~/.cosyvoice-venv/Scripts/python.exe -r <CosyVoice>/requirements.txt
#     （Windows 上跳过 grpcio/grpcio-tools——只服务 gRPC server，推理不需要）
#   克隆 FunAudioLLM/CosyVoice（含 third_party/Matcha-TTS 子模块），
#   snapshot_download('FunAudioLLM/CosyVoice2-0.5B', endpoint='https://hf-mirror.com')
#   注：HF 上的 CosyVoice2-0.5B 不带 spk2info（预训练音色），只有零样本模式；
#       模型目录里的 speech_tokenizer_v2.batch.onnx 与 flow.decoder.estimator.fp32.onnx
#       推理用不到，可删（各省 780MB）。
#
# 跑法：用该 venv 的 python 执行本脚本（~20 分钟 CPU），产出 24kHz/16-bit WAV 覆盖本目录 zh/en。

import os
import pathlib
import sys

import numpy as np
import soundfile as sf

HERE = pathlib.Path(__file__).parent
SAMPLE_RATE = 24000
PEAK = 0.70
COSYVOICE_REPO = os.environ.get("COSYVOICE_REPO", r"D:\tmp\CosyVoice")
MODEL_DIR = os.environ.get(
    "COSYVOICE_MODEL", pathlib.Path.home() / ".cosyvoice-models" / "CosyVoice2-0.5B"
)

# 参考句（转写必须与参考音频逐字一致；刻意用 ~3s 短句：prompt token 少一半，
# 单字合成更快，也缓解"合成文本比参考文本短"的时长失配警告）。
REF_TEXT_ZH = "秋日的阳光洒在书桌上，安静而从容。"
REF_TEXT_EN = "The afternoon light feels calm and quiet."
DIGITS = {
    "zh": (["零", "幺", "二", "三", "四", "五", "六", "七", "八", "九"], "zh-CN", 8),
    "en": (["zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"], "en-US", 1),
}


def shape(pcm: np.ndarray) -> np.ndarray:
    """去首尾静音 → 归一响度 → 淡入淡出（与 gen_kokoro 同口径，Go 端拼接零改动）。"""
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
    sys.path.insert(0, COSYVOICE_REPO)
    sys.path.insert(0, os.path.join(COSYVOICE_REPO, "third_party", "Matcha-TTS"))
    from cosyvoice.cli.cosyvoice import CosyVoice2  # noqa: E402

    sys.path.insert(0, str(HERE))
    import gen_kokoro as g  # noqa: E402

    g.ensure_model()
    ktts = g.build_tts()

    cv = CosyVoice2(MODEL_DIR, load_jit=False, load_trt=False, fp16=False)
    if cv.sample_rate != SAMPLE_RATE:
        raise RuntimeError(f"采样率 {cv.sample_rate} != {SAMPLE_RATE}")

    for lang, (words, locale, kokoro_sid) in DIGITS.items():
        ref_text = REF_TEXT_ZH if lang == "zh" else REF_TEXT_EN
        ref_sid = kokoro_sid
        ra = ktts.generate(ref_text, sid=ref_sid, speed=1.0)
        ref_path = HERE / f"_ref_{lang}.wav"
        sf.write(ref_path, np.asarray(ra.samples), ra.sample_rate, subtype="PCM_16")
        for d, text in enumerate(words):
            out = HERE / lang / f"{d}.wav"
            if out.is_file() and out.stat().st_size > 10000:  # 断点续跑：已有成品跳过
                print(f"{lang}/{d}.wav  已存在，跳过", flush=True)
                continue
            outs = list(cv.inference_zero_shot(text, ref_text, str(ref_path), stream=False))
            if not outs:
                raise RuntimeError(f"{lang}/{d}: 空输出")
            pcm = shape(outs[0]["tts_speech"].numpy())
            sf.write(out, pcm, SAMPLE_RATE, subtype="PCM_16")
            print(f"{lang}/{d}.wav  {text}  {len(pcm) / SAMPLE_RATE:.2f}s", flush=True)
        ref_path.unlink()
    print("done", flush=True)


if __name__ == "__main__":
    main()
