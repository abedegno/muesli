package audio

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildWAV assembles a minimal, valid mono or multi-channel 16-bit PCM WAV
// file from interleaved samples, for use as a generated fixture.
func buildWAV(t *testing.T, sampleRate, channels int, interleaved []int16) []byte {
	t.Helper()
	var buf bytes.Buffer
	dataSize := uint32(len(interleaved) * 2)
	byteRate := uint32(sampleRate * channels * 2)
	blockAlign := uint16(channels * 2)
	riffSize := 36 + dataSize

	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, riffSize)
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16))
	binary.Write(&buf, binary.LittleEndian, uint16(1)) // PCM
	binary.Write(&buf, binary.LittleEndian, uint16(channels))
	binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	binary.Write(&buf, binary.LittleEndian, byteRate)
	binary.Write(&buf, binary.LittleEndian, blockAlign)
	binary.Write(&buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, dataSize)
	for _, s := range interleaved {
		binary.Write(&buf, binary.LittleEndian, s)
	}
	return buf.Bytes()
}

func TestDecodeValidMonoWAV(t *testing.T) {
	data := buildWAV(t, 16000, 1, []int16{100, -200, 300})
	d, err := Decode(bytes.NewReader(data), 16000)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d.Format.Channels != 1 || d.Format.SampleRate != 16000 || d.Format.BitsPerSample != 16 {
		t.Fatalf("unexpected format: %+v", d.Format)
	}
	if len(d.Samples) != 3 {
		t.Fatalf("expected 3 samples, got %d", len(d.Samples))
	}
}

func TestDecodeRejectsNotRIFF(t *testing.T) {
	data := buildWAV(t, 16000, 1, []int16{1})
	data[0] = 'X'
	if _, err := Decode(bytes.NewReader(data), 16000); err == nil {
		t.Fatal("expected RIFF rejection")
	}
}

func TestDecodeRejectsNotWAVE(t *testing.T) {
	data := buildWAV(t, 16000, 1, []int16{1})
	data[8] = 'X'
	if _, err := Decode(bytes.NewReader(data), 16000); err == nil {
		t.Fatal("expected WAVE rejection")
	}
}

func TestDecodeRejectsWrongSampleRate(t *testing.T) {
	data := buildWAV(t, 8000, 1, []int16{1, 2, 3})
	if _, err := Decode(bytes.NewReader(data), 16000); err == nil {
		t.Fatal("expected sample-rate rejection (no resampling)")
	}
}

func TestDecodeRejectsNonPCMEncoding(t *testing.T) {
	data := buildWAV(t, 16000, 1, []int16{1, 2})
	// AudioFormat field lives at byte offset 20.
	binary.LittleEndian.PutUint16(data[20:22], 3) // IEEE float, unsupported
	if _, err := Decode(bytes.NewReader(data), 16000); err == nil {
		t.Fatal("expected non-PCM rejection")
	}
}

func TestDecodeRejectsNon16Bit(t *testing.T) {
	data := buildWAV(t, 16000, 1, []int16{1, 2})
	binary.LittleEndian.PutUint16(data[34:36], 8) // bits per sample
	if _, err := Decode(bytes.NewReader(data), 16000); err == nil {
		t.Fatal("expected non-16-bit rejection")
	}
}

func TestDecodeRejectsMisalignedDataChunk(t *testing.T) {
	data := buildWAV(t, 16000, 2, []int16{1, 2, 3, 4})
	// Corrupt the data chunk size to be odd relative to the 4-byte (stereo)
	// frame size.
	binary.LittleEndian.PutUint32(data[40:44], 3)
	if _, err := Decode(bytes.NewReader(data), 16000); err == nil {
		t.Fatal("expected misaligned data chunk rejection")
	}
}

func TestDecodeRejectsCorruptDataLength(t *testing.T) {
	data := buildWAV(t, 16000, 1, []int16{1, 2, 3, 4})
	// Claim far more data than actually follows.
	binary.LittleEndian.PutUint32(data[40:44], 100000)
	if _, err := Decode(bytes.NewReader(data), 16000); err == nil {
		t.Fatal("expected corrupt data length rejection")
	}
}

func TestDecodeSkipsUnknownChunksWithPadding(t *testing.T) {
	base := buildWAV(t, 16000, 1, []int16{7, 8, 9})
	// Splice an odd-length "LIST" chunk with padding between fmt and data.
	fmtEnd := 12 + 8 + 16 // riff header + fmt chunk header + fmt body
	var extra bytes.Buffer
	extra.WriteString("LIST")
	binary.Write(&extra, binary.LittleEndian, uint32(3))
	extra.Write([]byte{'a', 'b', 'c', 0}) // 3 bytes + 1 pad byte
	spliced := append(append(append([]byte{}, base[:fmtEnd]...), extra.Bytes()...), base[fmtEnd:]...)

	d, err := Decode(bytes.NewReader(spliced), 16000)
	if err != nil {
		t.Fatalf("decode with unknown chunk: %v", err)
	}
	if len(d.Samples) != 3 {
		t.Fatalf("expected 3 samples, got %d", len(d.Samples))
	}
}

func TestDecodeChannelDeclaration(t *testing.T) {
	data := buildWAV(t, 16000, 2, []int16{1, 2, 3, 4})
	d, err := Decode(bytes.NewReader(data), 16000)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d.Format.Channels != 2 {
		t.Fatalf("expected 2 channels, got %d", d.Format.Channels)
	}
	if d.FrameCount() != 2 {
		t.Fatalf("expected 2 frames, got %d", d.FrameCount())
	}
}

func TestSelectExplicitChannel(t *testing.T) {
	data := buildWAV(t, 16000, 2, []int16{100, 200, 300, 400}) // frame0: L=100 R=200; frame1: L=300 R=400
	d, err := Decode(bytes.NewReader(data), 16000)
	if err != nil {
		t.Fatal(err)
	}
	left, err := Select(d, ChannelPolicyExplicit, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 2 || left[0] != int16ToFloat32(100) || left[1] != int16ToFloat32(300) {
		t.Fatalf("unexpected left channel: %+v", left)
	}
	right, err := Select(d, ChannelPolicyExplicit, 1)
	if err != nil {
		t.Fatal(err)
	}
	if right[0] != int16ToFloat32(200) {
		t.Fatalf("unexpected right channel: %+v", right)
	}
}

func TestSelectExplicitOutOfRange(t *testing.T) {
	data := buildWAV(t, 16000, 1, []int16{1, 2})
	d, err := Decode(bytes.NewReader(data), 16000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Select(d, ChannelPolicyExplicit, 5); err == nil {
		t.Fatal("expected out-of-range channel rejection")
	}
}

func TestSelectAverageAllChannels(t *testing.T) {
	data := buildWAV(t, 16000, 2, []int16{100, 300, -100, -300}) // frame0 avg=200; frame1 avg=-200
	d, err := Decode(bytes.NewReader(data), 16000)
	if err != nil {
		t.Fatal(err)
	}
	avg, err := Select(d, ChannelPolicyAverage, 0)
	if err != nil {
		t.Fatal(err)
	}
	want0 := float32(200.0 / 32768.0)
	want1 := float32(-200.0 / 32768.0)
	if avg[0] != want0 || avg[1] != want1 {
		t.Fatalf("got %+v want [%v %v]", avg, want0, want1)
	}
}

func TestSelectUnknownPolicy(t *testing.T) {
	data := buildWAV(t, 16000, 1, []int16{1})
	d, _ := Decode(bytes.NewReader(data), 16000)
	if _, err := Select(d, "bogus", 0); err == nil {
		t.Fatal("expected unknown policy rejection")
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	samples := []float32{0, 0.5, -0.5, 0.999, -1.0}
	var buf bytes.Buffer
	if err := Encode(&buf, samples, 16000); err != nil {
		t.Fatalf("encode: %v", err)
	}
	d, err := Decode(&buf, 16000)
	if err != nil {
		t.Fatalf("decode round trip: %v", err)
	}
	if d.Format.Channels != 1 || d.Format.SampleRate != 16000 {
		t.Fatalf("unexpected format: %+v", d.Format)
	}
	if len(d.Samples) != len(samples) {
		t.Fatalf("expected %d samples, got %d", len(samples), len(d.Samples))
	}
}

func TestEncodeIsByteStable(t *testing.T) {
	samples := []float32{0.1, -0.2, 0.3}
	var a, b bytes.Buffer
	if err := Encode(&a, samples, 16000); err != nil {
		t.Fatal(err)
	}
	if err := Encode(&b, samples, 16000); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("expected byte-identical output for identical input")
	}
	if len(a.Bytes()) != 44+len(samples)*2 {
		t.Fatalf("expected canonical 44-byte header, got total length %d", len(a.Bytes()))
	}
}
