package auth

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// checkWav 校验合成出的 WAV 头：24kHz/16-bit/单声道 PCM，data 块长度与文件一致。
func checkWav(t *testing.T, wav []byte) int {
	t.Helper()
	if len(wav) < 44 || string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		t.Fatalf("不是合法的 RIFF/WAVE 头")
	}
	if got := binary.LittleEndian.Uint32(wav[4:8]); got != uint32(len(wav)-8) {
		t.Fatalf("RIFF size %d 与文件长度 %d 不符", got, len(wav)-8)
	}
	if format := binary.LittleEndian.Uint16(wav[20:22]); format != 1 {
		t.Fatalf("audioFormat = %d，要 PCM(1)", format)
	}
	if channels := binary.LittleEndian.Uint16(wav[22:24]); channels != 1 {
		t.Fatalf("channels = %d，要单声道", channels)
	}
	if rate := binary.LittleEndian.Uint32(wav[24:28]); rate != audioSampleRate {
		t.Fatalf("sampleRate = %d，要 %d", rate, audioSampleRate)
	}
	byteRate := binary.LittleEndian.Uint32(wav[28:32])
	if byteRate != audioSampleRate*audioSampleBytes {
		t.Fatalf("byteRate = %d，要 %d", byteRate, audioSampleRate*audioSampleBytes)
	}
	if align := binary.LittleEndian.Uint16(wav[32:34]); align != audioSampleBytes {
		t.Fatalf("blockAlign = %d，要 %d", align, audioSampleBytes)
	}
	if bits := binary.LittleEndian.Uint16(wav[34:36]); bits != audioSampleBytes*8 {
		t.Fatalf("bits = %d，要 %d", bits, audioSampleBytes*8)
	}
	if string(wav[36:40]) != "data" {
		t.Fatalf("找不到 data 块标记，got %q", string(wav[36:40]))
	}
	dataLen := binary.LittleEndian.Uint32(wav[40:44])
	if uint32(len(wav)) != 44+dataLen {
		t.Fatalf("data 长度 %d 与文件长度不符（应为 44+%d）", dataLen, dataLen)
	}
	return int(dataLen)
}

// 组合总长是纯拼接：首静音 + 数字与按 key 抖动的停顿 + 尾静音。
// 音源整体更换（如 gen_kokoro.py 再生成）时该等式不应被破坏。
func TestCaptchaAudioComposition(t *testing.T) {
	const key, text, lang = "audio-test-key", "1234", "zh"
	wav, err := renderCaptchaAudio(key, text, lang)
	if err != nil {
		t.Fatalf("renderCaptchaAudio: %v", err)
	}
	dataLen := checkWav(t, wav)
	pcm := wav[44:]

	want := len(silenceMs(200))
	pauses, repeat := audioGaps(key, len(text)-1)
	for i := range text {
		s, err := digitSound(lang, text[i:i+1])
		if err != nil {
			t.Fatalf("digitSound(%s,%c): %v", lang, text[i], err)
		}
		want += len(s)
		if i > 0 {
			want += len(pauses[i-1])
		}
	}
	want += len(silenceMs(200))
	if audioPlayTimes > 1 {
		want += (audioPlayTimes - 1) * len(repeat)
	}
	if dataLen != want {
		t.Fatalf("PCM 总长 = %d，按拼接应为 %d（audioPlayTimes=%d）", dataLen, want, audioPlayTimes)
	}

	// 16-bit PCM 的静音是全零：检查首段 200ms 静音底不是 8-bit 时代的 0x80
	head := pcm[:len(silenceMs(200))]
	if !bytes.Equal(head, make([]byte, len(head))) {
		t.Fatalf("静音段非全零（应为 16-bit 全零样本）")
	}
}

// 同 key 同文本合成必须逐字节一致（缓存友好）；换 key 必须不同（停顿抖动随 key 变，
// 防止「文件哈希 → 答案」字典攻击）。
func TestCaptchaAudioDeterminismAndJitter(t *testing.T) {
	a, err := renderCaptchaAudio("key-a", "1234", "en")
	if err != nil {
		t.Fatalf("key-a: %v", err)
	}
	a2, err := renderCaptchaAudio("key-a", "1234", "en")
	if err != nil {
		t.Fatalf("key-a 重放: %v", err)
	}
	b, err := renderCaptchaAudio("key-b", "1234", "en")
	if err != nil {
		t.Fatalf("key-b: %v", err)
	}
	if !bytes.Equal(a, a2) {
		t.Fatalf("同 key 同文本两次合成结果不一致")
	}
	if bytes.Equal(a, b) {
		t.Fatalf("不同 key 合成结果相同：抖动失效，音频可被哈希字典攻击")
	}
}

// 语言映射：前缀归一（zh-CN→zh）；未打包语言回落英文且不报错。
func TestCaptchaAudioLanguageFallback(t *testing.T) {
	for lang, want := range map[string]string{
		"zh":     "zh",
		"zh-CN":  "zh",
		"EN-us":  "en",
		"pt-BR":  "en", // 未打包语言 → 回落英文（打包口径只有 zh/en）
		"ja-JP":  "en",
		"fr":     "en",
		"":       "en",
		"ko-KR":  "en",
	} {
		if got := audioLang(lang); got != want {
			t.Errorf("audioLang(%q) = %q，要 %q", lang, got, want)
		}
	}
	wav, err := renderCaptchaAudio("key-fb", "909", "fr-FR")
	if err != nil {
		t.Fatalf("未打包语言应回落英文而非报错: %v", err)
	}
	checkWav(t, wav)
}

// 非法输入：空答案与全非数字文本都要报错而不是产出空音频。
func TestCaptchaAudioRejectsBadText(t *testing.T) {
	for _, text := range []string{"", "abcd", "..-"} {
		if _, err := renderCaptchaAudio("key-bad", text, "zh"); err == nil {
			t.Errorf("文本 %q 应报错", text)
		} else if !strings.Contains(err.Error(), "空答案") {
			t.Errorf("文本 %q 应报「空答案」，got: %v", text, err)
		}
	}
}
