// Package audio decodes and emits the canonical PCM WAV form the AMI VAD
// evaluator operates on (muesli#778): 16-bit PCM, a manifest-declared
// sample rate and channel count, with explicit or averaged channel
// selection producing stable mono float32 PCM. It never resamples --
// inputs must already be at the required rate.
package audio

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// Format describes one decoded WAV file's PCM layout.
type Format struct {
	SampleRate    int
	Channels      int
	BitsPerSample int
}

// Decoded is a fully-read WAV file: its format plus interleaved 16-bit PCM
// samples (length = frame count * Channels).
type Decoded struct {
	Format  Format
	Samples []int16
}

// FrameCount returns the number of multi-channel sample frames decoded.
func (d Decoded) FrameCount() int {
	if d.Format.Channels == 0 {
		return 0
	}
	return len(d.Samples) / d.Format.Channels
}

// maxChunkBytes bounds any single chunk this decoder will allocate for,
// so a corrupt or hostile length field cannot force an unbounded
// allocation.
const maxChunkBytes = 1 << 31 // 2 GiB; far beyond any real AMI recording's data chunk

// Decode validates RIFF/WAVE structure and decodes exactly one PCM 16-bit
// WAV file, requiring its declared sample rate to equal expectedSampleRate
// (no resampling is ever performed).
func Decode(r io.Reader, expectedSampleRate int) (Decoded, error) {
	br := bufio.NewReader(r)

	var riffHeader [12]byte
	if _, err := io.ReadFull(br, riffHeader[:]); err != nil {
		return Decoded{}, fmt.Errorf("audio: read riff header: %w", err)
	}
	if string(riffHeader[0:4]) != "RIFF" {
		return Decoded{}, fmt.Errorf("audio: not a RIFF file")
	}
	if string(riffHeader[8:12]) != "WAVE" {
		return Decoded{}, fmt.Errorf("audio: not a WAVE file")
	}

	var (
		format   Format
		haveFmt  bool
		samples  []int16
		haveData bool
	)

	for {
		var chunkHeader [8]byte
		_, err := io.ReadFull(br, chunkHeader[:])
		if err == io.EOF {
			break
		}
		if err != nil {
			return Decoded{}, fmt.Errorf("audio: read chunk header: %w", err)
		}
		id := string(chunkHeader[0:4])
		size := binary.LittleEndian.Uint32(chunkHeader[4:8])
		if size > maxChunkBytes {
			return Decoded{}, fmt.Errorf("audio: chunk %q declares implausible size %d", id, size)
		}

		switch id {
		case "fmt ":
			buf := make([]byte, size)
			if _, err := io.ReadFull(br, buf); err != nil {
				return Decoded{}, fmt.Errorf("audio: read fmt chunk: %w", err)
			}
			if len(buf) < 16 {
				return Decoded{}, fmt.Errorf("audio: fmt chunk too short (%d bytes)", len(buf))
			}
			audioFormat := binary.LittleEndian.Uint16(buf[0:2])
			channels := binary.LittleEndian.Uint16(buf[2:4])
			sampleRate := binary.LittleEndian.Uint32(buf[4:8])
			bitsPerSample := binary.LittleEndian.Uint16(buf[14:16])
			if audioFormat != 1 {
				return Decoded{}, fmt.Errorf("audio: unsupported encoding %d, only PCM (1) is supported", audioFormat)
			}
			if bitsPerSample != 16 {
				return Decoded{}, fmt.Errorf("audio: unsupported bits per sample %d, only 16-bit PCM is supported", bitsPerSample)
			}
			if channels < 1 {
				return Decoded{}, fmt.Errorf("audio: channels must be at least 1, got %d", channels)
			}
			if int(sampleRate) != expectedSampleRate {
				return Decoded{}, fmt.Errorf("audio: sample rate %d does not match required %d (no resampling is performed)", sampleRate, expectedSampleRate)
			}
			format = Format{SampleRate: int(sampleRate), Channels: int(channels), BitsPerSample: int(bitsPerSample)}
			haveFmt = true
			if size%2 == 1 {
				if _, err := io.CopyN(io.Discard, br, 1); err != nil {
					return Decoded{}, fmt.Errorf("audio: read fmt chunk padding: %w", err)
				}
			}
		case "data":
			if !haveFmt {
				return Decoded{}, fmt.Errorf("audio: data chunk appears before fmt chunk")
			}
			frameSize := format.Channels * 2
			if frameSize == 0 || int(size)%frameSize != 0 {
				return Decoded{}, fmt.Errorf("audio: data chunk size %d is not aligned to frame size %d", size, frameSize)
			}
			buf := make([]byte, size)
			if _, err := io.ReadFull(br, buf); err != nil {
				return Decoded{}, fmt.Errorf("audio: read data chunk (declared %d bytes, likely corrupt length): %w", size, err)
			}
			n := int(size) / 2
			samples = make([]int16, n)
			for i := 0; i < n; i++ {
				samples[i] = int16(binary.LittleEndian.Uint16(buf[i*2 : i*2+2]))
			}
			haveData = true
			if size%2 == 1 {
				if _, err := io.CopyN(io.Discard, br, 1); err != nil {
					return Decoded{}, fmt.Errorf("audio: read data chunk padding: %w", err)
				}
			}
		default:
			skip := int64(size)
			if size%2 == 1 {
				skip++
			}
			if _, err := io.CopyN(io.Discard, br, skip); err != nil {
				return Decoded{}, fmt.Errorf("audio: skip chunk %q (declared %d bytes, likely corrupt length): %w", id, size, err)
			}
		}
	}

	if !haveFmt {
		return Decoded{}, fmt.Errorf("audio: missing fmt chunk")
	}
	if !haveData {
		return Decoded{}, fmt.Errorf("audio: missing data chunk")
	}
	return Decoded{Format: format, Samples: samples}, nil
}

// Channel selection policies, mirroring manifest.ChannelPolicy.
const (
	ChannelPolicyExplicit = "explicit"
	ChannelPolicyAverage  = "average"
)

// Select produces canonical mono float32 PCM (range approximately [-1,1])
// from decoded interleaved 16-bit PCM, either taking one explicit channel
// or averaging every channel when the manifest declares them all as one
// source.
func Select(d Decoded, policy string, selectChannel int) ([]float32, error) {
	ch := d.Format.Channels
	if ch <= 0 {
		return nil, fmt.Errorf("audio: decoded format has no channels")
	}
	frames := d.FrameCount()
	out := make([]float32, frames)
	switch policy {
	case ChannelPolicyExplicit:
		if selectChannel < 0 || selectChannel >= ch {
			return nil, fmt.Errorf("audio: select_channel %d out of range [0,%d)", selectChannel, ch)
		}
		for i := 0; i < frames; i++ {
			out[i] = int16ToFloat32(d.Samples[i*ch+selectChannel])
		}
	case ChannelPolicyAverage:
		for i := 0; i < frames; i++ {
			var sum float64
			for c := 0; c < ch; c++ {
				sum += float64(d.Samples[i*ch+c])
			}
			out[i] = float32(sum / float64(ch) / 32768.0)
		}
	default:
		return nil, fmt.Errorf("audio: unknown channel policy %q", policy)
	}
	return out, nil
}

func int16ToFloat32(s int16) float32 { return float32(s) / 32768.0 }

func floatToInt16(f float32) int16 {
	v := float64(f) * 32767.0
	if v > 32767 {
		v = 32767
	}
	if v < -32768 {
		v = -32768
	}
	return int16(math.Round(v))
}

// Encode writes canonical single-channel 16-bit PCM WAV: a fixed 44-byte
// canonical header (no extension chunks, no extra metadata) followed by the
// data chunk, so byte-identical input always produces byte-identical
// output.
func Encode(w io.Writer, samples []float32, sampleRate int) error {
	if sampleRate <= 0 {
		return fmt.Errorf("audio: sample rate must be positive")
	}
	const (
		channels      = 1
		bitsPerSample = 16
	)
	dataSize := uint32(len(samples) * 2)
	byteRate := uint32(sampleRate * channels * bitsPerSample / 8)
	blockAlign := uint16(channels * bitsPerSample / 8)
	riffSize := 36 + dataSize

	buf := make([]byte, 0, 44+len(samples)*2)
	buf = append(buf, []byte("RIFF")...)
	buf = binary.LittleEndian.AppendUint32(buf, riffSize)
	buf = append(buf, []byte("WAVE")...)
	buf = append(buf, []byte("fmt ")...)
	buf = binary.LittleEndian.AppendUint32(buf, 16)
	buf = binary.LittleEndian.AppendUint16(buf, 1) // PCM
	buf = binary.LittleEndian.AppendUint16(buf, channels)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(sampleRate))
	buf = binary.LittleEndian.AppendUint32(buf, byteRate)
	buf = binary.LittleEndian.AppendUint16(buf, blockAlign)
	buf = binary.LittleEndian.AppendUint16(buf, bitsPerSample)
	buf = append(buf, []byte("data")...)
	buf = binary.LittleEndian.AppendUint32(buf, dataSize)
	for _, s := range samples {
		buf = binary.LittleEndian.AppendUint16(buf, uint16(floatToInt16(s)))
	}
	_, err := w.Write(buf)
	return err
}
