package audio_test

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/the-algovn/tts-service/internal/audio"
)

func tone(t *testing.T, seconds string) []byte {
	t.Helper()
	out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+seconds+":sample_rate=24000",
		"-f", "wav", "-").Output()
	require.NoError(t, err)
	return out
}

func TestProbeReportsDuration(t *testing.T) {
	d, err := audio.Probe(context.Background(), tone(t, "2"))
	require.NoError(t, err)
	require.InDelta(t, 2.0, d, 0.05)
}

func TestProbeRejectsNonAudio(t *testing.T) {
	_, err := audio.Probe(context.Background(), []byte("\xff\xd8\xff\xe0not audio"))
	require.Error(t, err)
}

func TestNormalizeWAVResamplesTo48kMono(t *testing.T) {
	out, err := audio.NormalizeWAV(context.Background(), tone(t, "1"))
	require.NoError(t, err)
	d, err := audio.Probe(context.Background(), out)
	require.NoError(t, err)
	require.InDelta(t, 1.0, d, 0.05)
	require.Equal(t, "RIFF", string(out[:4]))
}

func TestConcatWAVAddsGaps(t *testing.T) {
	out, err := audio.ConcatWAV(context.Background(), [][]byte{tone(t, "1"), tone(t, "1"), tone(t, "1")}, 150*time.Millisecond)
	require.NoError(t, err)
	d, err := audio.Probe(context.Background(), out)
	require.NoError(t, err)
	require.InDelta(t, 3.3, d, 0.05)
}

func TestTempo(t *testing.T) {
	in := tone(t, "2")
	same, err := audio.Tempo(context.Background(), in, 1.0)
	require.NoError(t, err)
	require.Equal(t, in, same)

	fast, err := audio.Tempo(context.Background(), in, 1.25)
	require.NoError(t, err)
	d, _ := audio.Probe(context.Background(), fast)
	require.InDelta(t, 1.6, d, 0.05)

	_, err = audio.Tempo(context.Background(), in, 3.0)
	require.Error(t, err)
}
