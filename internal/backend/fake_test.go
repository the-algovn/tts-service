package backend_test

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/the-algovn/tts-service/internal/backend"
)

// Fake's byte layout is load-bearing outside this package: radio-service's
// director/render.go measures -inf LUFS on exactly this WAV and takes a
// special plain-decode path (see its silenceFloorLUFS constant). If this test
// starts failing, a "harmless" cleanup of the header packing has just broken
// radio's silence handling — restore the exact layout, don't just relax this
// test.
func TestFakeSynthesizeProducesKnownSilentWAV(t *testing.T) {
	data, ext, err := backend.Fake{}.Synthesize(context.Background(), "any text", "any voice", 1.0)
	require.NoError(t, err)
	require.Equal(t, "wav", ext)

	const headerLen = 44
	const dataLen = 16000 // 8000 samples/sec * 1 sec * 2 bytes/sample
	require.Len(t, data, headerLen+dataLen,
		"total WAV length changed; radio's silence detection depends on this exact byte count")

	require.Equal(t, "RIFF", string(data[0:4]), "RIFF magic moved")
	require.Equal(t, uint32(36+dataLen), binary.LittleEndian.Uint32(data[4:8]), "RIFF chunk size wrong")
	require.Equal(t, "WAVE", string(data[8:12]), "WAVE magic moved")
	require.Equal(t, "fmt ", string(data[12:16]), "fmt  magic moved")
	require.Equal(t, uint32(16), binary.LittleEndian.Uint32(data[16:20]), "fmt chunk size must be 16 (PCM)")
	require.Equal(t, uint16(1), binary.LittleEndian.Uint16(data[20:22]), "audio format must be 1 (PCM)")
	require.Equal(t, uint16(1), binary.LittleEndian.Uint16(data[22:24]), "channel count must be 1 (mono)")
	require.Equal(t, uint32(8000), binary.LittleEndian.Uint32(data[24:28]), "sample rate must be 8000 Hz")
	require.Equal(t, uint32(16000), binary.LittleEndian.Uint32(data[28:32]), "byte rate must be 16000 (8000*2)")
	require.Equal(t, uint16(2), binary.LittleEndian.Uint16(data[32:34]), "block align must be 2")
	require.Equal(t, uint16(16), binary.LittleEndian.Uint16(data[34:36]), "bits per sample must be 16")
	require.Equal(t, "data", string(data[36:40]), "data magic moved")
	require.Equal(t, uint32(dataLen), binary.LittleEndian.Uint32(data[40:44]), "data chunk size wrong")

	pcm := data[headerLen:]
	for i, b := range pcm {
		if b != 0 {
			t.Fatalf("pcm byte %d is %#x, want 0x00: this is supposed to be digital silence", i, b)
		}
	}
}
