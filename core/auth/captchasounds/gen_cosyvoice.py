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
# 换 take 后必须回听声调：8 曾被合成成第四声「爸」（基频 -4 半音下滑，正确读法
# 「八」是第一声，基频应基本持平）。同音节换字（如「捌」「巴」）也要按声调复核。
DIGITS = {
    # 1/2/3 用正式数字：普通「一/二/三」的单字 take 会读出降调或怪音（见 ATTRIBUTION
    # 「当前 zh 成品」）。5 的成品取自整串合成切分，单字「五」需重新校验后再用。
    "zh": (["零", "壹", "贰", "叁", "四", "五", "六", "七", "八", "九"], "zh-CN", 8),
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


def tail_ratio(pcm: np.ndarray) -> float:
    """末 20ms 能量 / 整段 RMS。模型偶尔提前吐 EOS，把字尾切掉——半截音听起来
    就是"语音验证码听不清"（历史音源里的"五"曾只剩一个突兀的"wu"）。"""
    rms = np.sqrt(np.mean(pcm**2)) + 1e-12
    n = int(0.020 * SAMPLE_RATE)
    return float(np.sqrt(np.mean(pcm[-n:] ** 2)) / rms)


MAX_TAIL_RATIO = 0.15


def synth_take(cv, text, ref_text, ref_path):
    """合成并复核字尾：最多试 3 次，取尾音收得最干净的一条。"""
    best = None
    for _ in range(3):
        outs = list(cv.inference_zero_shot(text, ref_text, str(ref_path), stream=False))
        if not outs:
            continue
        pcm = shape(outs[0]["tts_speech"].numpy())
        ratio = tail_ratio(pcm)
        if best is None or ratio < best[0]:
            best = (ratio, pcm)
        if ratio <= MAX_TAIL_RATIO:
            break
        print(f"  尾音被截断（tail={ratio:.2f}），重新合成", flush=True)
    if best is None:
        raise RuntimeError(f"{text}: 空输出")
    return best[1]


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
            if out.is_file() and out.stat().st_size > 10000:  # 断点续跑
                pcm, _ = sf.read(out, dtype="float64")
                if tail_ratio(pcm) <= MAX_TAIL_RATIO:
                    print(f"{lang}/{d}.wav  已存在，跳过", flush=True)
                    continue
                print(f"{lang}/{d}.wav  尾音被截断，重新合成", flush=True)
            pcm = synth_take(cv, text, ref_text, ref_path)
            sf.write(out, pcm, SAMPLE_RATE, subtype="PCM_16")
            print(f"{lang}/{d}.wav  {text}  {len(pcm) / SAMPLE_RATE:.2f}s", flush=True)
        ref_path.unlink()
    print("done", flush=True)


if __name__ == "__main__":
    main()
