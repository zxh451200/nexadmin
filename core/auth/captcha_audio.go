package auth

import (
	"bytes"
	"embed"
	"encoding/binary"
	"fmt"
	"path"
)

// 音频验证码的自绘合成：不再使用 dchest/captcha 的 NewAudio——它把数字语音"混进"
// 一条随机背景噪声里、数字之间随机隔 1–3 秒，人耳基本听不清（这正是它被诟病的原因）。
// 这里改为：数字语音（MIT 许可的录音，见 captchasounds/ATTRIBUTION）在干净的静音底上
// 依次播放，整串重复两遍，首尾各一声提示音。安全性不受影响：语音验证码本来就防不住
// 自动转写，防爆破靠的是一次性 key、IP 绑定、60 秒 TTL 与登录限流。

//go:embed captchasounds/*.wav captchasounds/*/*.wav
var soundFS embed.FS

const (
	// 音源为 8 kHz、8-bit、单声道无符号 PCM（与 dchest 的音源格式一致）。
	audioSampleRate = 8000
	// 数字之间的停顿与两遍之间的间隔（毫秒）。
	audioDigitPauseMs  = 250
	audioRepeatPauseMs = 600
	// 停顿再按 key 加 0–3 档、每档 audioGapStepMs 的抖动（见 audioGaps）。
	audioGapStepMs = 50
	audioGapLevels = 4
)

// audioSeed 从 key 造一条确定性随机流（splitmix64）。
func audioSeed(key string) func() uint64 {
	var s uint64 = 14695981039346656037
	for i := 0; i < len(key); i++ {
		s ^= uint64(key[i])
		s *= 1099511628211
	}
	return func() uint64 {
		s += 0x9E3779B97F4A7C15
		z := s
		z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
		z = (z ^ (z >> 27)) * 0x94D049BB133111EB
		return z ^ (z >> 31)
	}
}

// audioGaps 给出本次挑战的停顿计划：gaps 个数字间停顿 + 一个两遍之间的间隔。
// 抖动只由 key（一次性、服务端已生成）决定，两遍用同一份计划，所以"重复听"的体验不变；
// 目的是让同一个答案每次合成出的字节不同——否则攻击者解一次就能把「文件哈希 → 答案」
// 存成字典，之后比对哈希认题，不用真的听。
func audioGaps(key string, gaps int) (digit [][]byte, repeat []byte) {
	next := audioSeed(key)
	digit = make([][]byte, gaps)
	for i := range digit {
		digit[i] = silenceMs(audioDigitPauseMs + int(next()%audioGapLevels)*audioGapStepMs)
	}
	repeat = silenceMs(audioRepeatPauseMs + int(next()%audioGapLevels)*audioGapStepMs*2)
	return digit, repeat
}

// pcmWAV 从嵌入的 wav 文件提取 PCM 数据（校验格式：8kHz / 8-bit / 单声道）。
func pcmWAV(b []byte) ([]byte, error) {
	if len(b) < 44 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, fmt.Errorf("captcha audio: 不是 WAV 文件")
	}
	audioFormat := binary.LittleEndian.Uint16(b[20:22])
	channels := binary.LittleEndian.Uint16(b[22:24])
	sampleRate := binary.LittleEndian.Uint32(b[24:28])
	bits := binary.LittleEndian.Uint16(b[34:36])
	if audioFormat != 1 || channels != 1 || sampleRate != audioSampleRate || bits != 8 {
		return nil, fmt.Errorf("captcha audio: 音源格式不符（要 8kHz/8bit/单声道 PCM）")
	}
	// 找 data 块（音源由 capgensounds 生成，data 紧跟标准 44 字节头；仍按块遍历以防变体）。
	off := uint32(12)
	for off+8 <= uint32(len(b)) {
		id := string(b[off : off+4])
		size := binary.LittleEndian.Uint32(b[off+4 : off+8])
		if id == "data" {
			if off+8+size > uint32(len(b)) {
				return nil, fmt.Errorf("captcha audio: data 块越界")
			}
			return b[off+8 : off+8+size], nil
		}
		off += 8 + size + size&1 // 块按偶数对齐
	}
	return nil, fmt.Errorf("captcha audio: 找不到 data 块")
}

// digitSound 取某语言某数字的 PCM；语言缺失或解码失败回落英文。
func digitSound(lang, digit string) ([]byte, error) {
	read := func(l string) ([]byte, error) {
		return soundFS.ReadFile(path.Join("captchasounds", l, digit+".wav"))
	}
	if b, err := read(lang); err == nil {
		if pcm, perr := pcmWAV(b); perr == nil {
			return pcm, nil
		}
	}
	b, err := read("en")
	if err != nil {
		return nil, err
	}
	return pcmWAV(b)
}

// beepSound 提示音（开始/结束各一声，提示"开始了/读完了"）。
func beepSound() ([]byte, error) {
	b, err := soundFS.ReadFile(path.Join("captchasounds", "beep.wav"))
	if err != nil {
		return nil, err
	}
	return pcmWAV(b)
}

func silenceMs(ms int) []byte {
	return bytes.Repeat([]byte{0x80}, audioSampleRate*ms/1000)
}

// composeCaptchaAudio 把 digits 依次干净地拼到静音底上，整串重复两遍，首尾加提示音。
func composeCaptchaAudio(key, lang string, digits []byte) ([]byte, error) {
	beep, err := beepSound()
	if err != nil {
		return nil, err
	}
	sounds := make([][]byte, 0, len(digits))
	for _, d := range digits {
		if d > 9 {
			return nil, fmt.Errorf("captcha audio: 非数字 %d", d)
		}
		pcm, err := digitSound(lang, fmt.Sprintf("%d", d))
		if err != nil {
			return nil, err
		}
		sounds = append(sounds, pcm)
	}
	pauses, repeatPause := audioGaps(key, len(sounds)-1)

	var pcm bytes.Buffer
	pcm.Write(beep)
	pcm.Write(silenceMs(200))
	for round := 0; round < 2; round++ {
		if round > 0 {
			pcm.Write(repeatPause)
		}
		for i, s := range sounds {
			if i > 0 {
				pcm.Write(pauses[i-1])
			}
			pcm.Write(s)
		}
	}
	pcm.Write(silenceMs(200))
	pcm.Write(beep)

	return wavBytes(pcm.Bytes()), nil
}

// wavBytes 给 8kHz/8-bit/单声道 PCM 包上 RIFF/WAVE 头。
func wavBytes(pcm []byte) []byte {
	const sampleRate = audioSampleRate
	dataLen := uint32(len(pcm))
	riff := bytes.NewBuffer(make([]byte, 0, 44+len(pcm)))
	riff.WriteString("RIFF")
	chunk := dataLen + 36
	binary.Write(riff, binary.LittleEndian, chunk)
	riff.WriteString("WAVE")
	riff.WriteString("fmt ")
	binary.Write(riff, binary.LittleEndian, uint32(16)) // fmt 块长
	binary.Write(riff, binary.LittleEndian, uint16(1))  // PCM
	binary.Write(riff, binary.LittleEndian, uint16(1))  // 单声道
	binary.Write(riff, binary.LittleEndian, uint32(sampleRate))
	binary.Write(riff, binary.LittleEndian, uint32(sampleRate)) // 字节率 = 采样率×1×1
	binary.Write(riff, binary.LittleEndian, uint16(1))          // 块对齐
	binary.Write(riff, binary.LittleEndian, uint16(8))          // 位深
	riff.WriteString("data")
	binary.Write(riff, binary.LittleEndian, dataLen)
	riff.Write(pcm)
	return riff.Bytes()
}

// renderCaptchaAudio 出 WAV，供音频验证码使用。lang 支持 captchasounds/ 下打包的语言
// （zh/en/ja/ru/pt），其余回落英文。
func renderCaptchaAudio(key, text, lang string) ([]byte, error) {
	digits := digitBytes(text)
	if len(digits) == 0 {
		return nil, fmt.Errorf("captcha audio: 空答案")
	}
	return composeCaptchaAudio(key, audioLang(lang), digits)
}
