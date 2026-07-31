package audio_test

import (
	"context"
	"encoding/binary"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/the-algovn/tts-service/internal/audio"
)

// silentWAV builds the same 1s 8kHz mono WAV the fake backend emits.
func silentWAV() []byte {
	const sampleRate, seconds = 8000, 1
	n := sampleRate * seconds * 2
	buf := make([]byte, 44+n)
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(36+n))
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16)
	binary.LittleEndian.PutUint16(buf[20:22], 1)
	binary.LittleEndian.PutUint16(buf[22:24], 1)
	binary.LittleEndian.PutUint32(buf[24:28], sampleRate)
	binary.LittleEndian.PutUint32(buf[28:32], sampleRate*2)
	binary.LittleEndian.PutUint16(buf[32:34], 2)
	binary.LittleEndian.PutUint16(buf[34:36], 16)
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], uint32(n))
	return buf
}

func TestToMP3ProducesMP3(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}

	got, err := audio.ToMP3(context.Background(), silentWAV())
	require.NoError(t, err)
	require.NotEmpty(t, got)

	// An MP3 starts with either an ID3 tag or a frame sync (0xFF 0xEx/0xFx).
	id3 := string(got[:3]) == "ID3"
	sync := got[0] == 0xFF && got[1]&0xE0 == 0xE0
	require.True(t, id3 || sync, "output is not MP3, first bytes: % x", got[:4])
}

// Asking for the format the data already is must not shell out at all.
func TestConvertIsIdentityWhenFormatsMatch(t *testing.T) {
	in := silentWAV()
	got, err := audio.Convert(context.Background(), in, "wav", "wav")
	require.NoError(t, err)
	require.Equal(t, in, got)
}

// filepath.Join cleans ".." rather than rejecting it, so an unchecked
// extension could otherwise escape the temp directory sandbox. These assert
// on the specific rejection message, not just "some error" — an unvalidated
// traversal attempt still errors out today (the OS or ffmpeg happens to
// reject the resulting stray path), which would let a weaker assertion pass
// without the extension actually being validated.
func TestConvertRejectsPathTraversalInFromExt(t *testing.T) {
	_, err := audio.Convert(context.Background(), silentWAV(), "../../../../etc/passwd", "mp3")
	require.ErrorContains(t, err, "unsupported audio format")
}

func TestConvertRejectsPathTraversalInToExt(t *testing.T) {
	_, err := audio.Convert(context.Background(), silentWAV(), "wav", "../../evil")
	require.ErrorContains(t, err, "unsupported audio format")
}
