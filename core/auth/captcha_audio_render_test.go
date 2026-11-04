package auth

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// 新版音频合成（captcha_audio.go）的质量契约：清晰可听的关键在
// 「数字干净地放在静音底上、整串读两遍」，而不是 dchest 默认的噪声混音。

func wavPCM(t *testing.T, b []byte) []byte {
	t.Helper()
	if len(b) < 44 {
		t.Fatalf("WAV 太短：%d 字节", len(b))
	}
	pcm, err := pcmWAV(b)
	if err != nil {
		t.Fatal(err)
	}
	return pcm
}

func TestComposedAudioLayout(t *testing.T) {
	a, err := renderCaptchaAudio("k1", "12345", "zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(a, []byte("RIFF")) || string(a[8:12]) != "WAVE" {
		t.Fatalf("不是 WAV：%q", a[:12])
	}
	if got := binary.LittleEndian.Uint32(a[24:28]); got != audioSampleRate {
		t.Errorf("采样率 = %d, want %d", got, audioSampleRate)
	}
	if ch := binary.LittleEndian.Uint16(a[22:24]); ch != 1 {
		t.Errorf("声道 = %d, want 单声道", ch)
	}
	if bits := binary.LittleEndian.Uint16(a[34:36]); bits != 8 {
		t.Errorf("位深 = %d, want 8", bits)
	}

	// 同 key 同答案重放一致（刷新前的 <audio> 可重复播）；
	// 换 key 必须逐字节不同——否则「哈希 → 答案」字典能不靠听就认题。
	same, err := renderCaptchaAudio("k1", "12345", "zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, same) {
		t.Error("同一 key 两次合成结果应完全一致（无随机噪声）")
	}
	other, err := renderCaptchaAudio("k2", "12345", "zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, other) {
		t.Error("不同 key 的同一答案合成结果不应逐字节相同（离线指纹）")
	}
}

func TestComposedAudioRepeatsSequence(t *testing.T) {
	const key = "challenge-key-1"
	wav, err := renderCaptchaAudio(key, "31415", "zh")
	if err != nil {
		t.Fatal(err)
	}
	pcm := wavPCM(t, wav)

	// 按本次挑战的停顿计划推算布局：beep + sil200 + seq + repeatP + seq + sil200 + beep。
	beepPCM := wavPCM(t, mustEmbed(t, "captchasounds/beep.wav"))
	digits := []string{"3", "1", "4", "1", "5"}
	pauses, repeatP := audioGaps(key, len(digits)-1)
	seqLen := 0
	for _, d := range digits {
		seqLen += len(wavPCM(t, mustEmbed(t, "captchasounds/zh/"+d+".wav")))
	}
	for i := range pauses {
		seqLen += len(pauses[i])
	}

	leadIn := len(beepPCM) + audioSampleRate*200/1000
	first := pcm[leadIn : leadIn+seqLen]
	second := pcm[leadIn+seqLen+len(repeatP) : leadIn+2*seqLen+len(repeatP)]
	if !bytes.Equal(first, second) {
		t.Error("两遍数字序列应逐字节一致（抖动只按 key 定一次，不随播放变化）")
	}
}

func TestComposedAudioGapJitterBounded(t *testing.T) {
	// 抖动必须留在可听范围内：停顿超出 [250ms, 400ms] 要么黏糊要么拖沓。
	seen := map[int]bool{}
	for i := 0; i < 200; i++ {
		pauses, repeat := audioGaps(string(rune('a'+i%26))+"key", 4)
		for _, p := range pauses {
			ms := len(p) * 1000 / audioSampleRate
			if ms < audioDigitPauseMs || ms > audioDigitPauseMs+(audioGapLevels-1)*audioGapStepMs {
				t.Fatalf("数字间停顿 %dms 越界 [%d,%d]", ms, audioDigitPauseMs, audioDigitPauseMs+(audioGapLevels-1)*audioGapStepMs)
			}
			seen[ms] = true
		}
		ms := len(repeat) * 1000 / audioSampleRate
		if ms < audioRepeatPauseMs || ms > audioRepeatPauseMs+(audioGapLevels-1)*audioGapStepMs*2 {
			t.Fatalf("两遍间隔 %dms 越界 [%d,%d]", ms, audioRepeatPauseMs, audioRepeatPauseMs+(audioGapLevels-1)*audioGapStepMs*2)
		}
	}
	if len(seen) < 2 {
		t.Errorf("200 个 key 只得到 %d 档停顿——抖动没生效，字典攻击回来了", len(seen))
	}
}

func mustEmbed(t *testing.T, name string) []byte {
	t.Helper()
	b, err := soundFS.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestComposedAudioDurationSane(t *testing.T) {
	wav, err := renderCaptchaAudio("k1", "12345", "en")
	if err != nil {
		t.Fatal(err)
	}
	seconds := float64(len(wavPCM(t, wav))) / audioSampleRate
	// 5 位数字读两遍 + 停顿 + 提示音，应在 4–12 秒之间：
	// 短于 4s 说明序列没读全，长于 12s 说明混入了多余内容。
	if seconds < 4 || seconds > 12 {
		t.Errorf("音频时长 %.1fs 不在预期区间 [4,12]", seconds)
	}
}

// 音源格式守卫：pcmWAV 只接受 8kHz/8bit/单声道 PCM。去掉守卫会让采样率不符的音源
// 被当成合法数据拼进输出，播出来是变速/噪音，而合成函数本身不会报错。
func TestPcmWAVRejectsNonConformingSource(t *testing.T) {
	good := mustEmbed(t, "captchasounds/beep.wav")
	mutate := func(at int, v byte) []byte {
		if good[at] == v {
			t.Fatalf("beep.wav 在偏移 %d 本来就是 %d，这一档没改到任何东西", at, v)
		}
		b := append([]byte(nil), good...)
		b[at] = v
		return b
	}

	cases := []struct {
		name string
		b    []byte
	}{
		{"非 WAV", []byte("not a wav file at all, padding padding padding padding padding")},
		{"过短", good[:20]},
		{"RIFF 魔数坏", mutate(0, 'J')},
		{"WAVE 魔数坏", mutate(8, 'J')},
		{"非 PCM(IEEE float)", mutate(20, 3)},
		{"双声道", mutate(22, 2)},
		{"采样率非 8k", mutate(24, 16)},
		{"位深非 8bit", mutate(34, 16)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := pcmWAV(tc.b); err == nil {
				t.Errorf("pcmWAV 接受了不合格式的音源（守卫被删过？）")
			}
		})
	}

	// data 块越界 / 找不到 data 块也要报错，不能让调用方拿到切片越界的 panic。
	overflow := append([]byte(nil), good...)
	binary.LittleEndian.PutUint32(overflow[40:44], uint32(len(overflow)))
	if _, err := pcmWAV(overflow); err == nil {
		t.Error("pcmWAV 接受了 data 长度越界的音源")
	}
	noData := append([]byte(nil), good[:44]...)
	copy(noData[36:40], "junk")
	if _, err := pcmWAV(noData); err == nil {
		t.Error("pcmWAV 在没有 data 块的文件上返回了成功")
	}

	// 正控制：原样音源必须通过，否则上面全是假绿。
	if _, err := pcmWAV(good); err != nil {
		t.Errorf("合法音源被拒：%v", err)
	}
}

// 语言回落：打包里没有的语言（或该语言缺某个数字）必须用英文读音，
// 而不是整个音频接口 500。删掉 digitSound 的回落分支，这里会直接报错。
func TestDigitSoundFallsBackToEnglish(t *testing.T) {
	en, err := digitSound("en", "7")
	if err != nil {
		t.Fatal(err)
	}
	if len(en) == 0 {
		t.Fatal("英文音源为空")
	}

	for _, lang := range []string{"de", "fr", "", "zh-TW", "en-GB"} {
		got, err := digitSound(lang, "7")
		if err != nil {
			t.Errorf("digitSound(%q) 回落失败：%v", lang, err)
			continue
		}
		if !bytes.Equal(got, en) {
			t.Errorf("digitSound(%q) 没有回落到英文读音", lang)
		}
	}

	// 有本地化音源的语言不该被英文顶掉。
	if b, err := soundFS.ReadFile("captchasounds/zh/7.wav"); err == nil {
		zh, err := digitSound("zh", "7")
		if err != nil {
			t.Fatal(err)
		}
		want, err := pcmWAV(b)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(zh, en) {
			t.Error("zh 有本地音源却拿到英文（回落后覆盖真音源）")
		}
		if !bytes.Equal(zh, want) {
			t.Error("zh 读音与打包音源不一致")
		}
	} else {
		t.Errorf("缺少 zh 音源，语言分支无从校验：%v", err)
	}

	// renderCaptchaAudio 对未知语言整条链路要能出片。
	if _, err := renderCaptchaAudio("kx", "12345", "de"); err != nil {
		t.Errorf("未知语言的音频合成应回落英文：%v", err)
	}
}
