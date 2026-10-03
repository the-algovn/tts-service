package audio

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const outRate = "48000"

// Probe returns the duration in seconds of any audio ffmpeg can decode.
// Errors when data is not decodable audio.
func Probe(ctx context.Context, data []byte) (float64, error) {
	// Files, not pipes: ffprobe reports duration N/A for piped WAV/MP3.
	dir, err := os.MkdirTemp("", "tts-probe-*")
	if err != nil {
		return 0, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	in := filepath.Join(dir, "in")
	if err := os.WriteFile(in, data, 0o600); err != nil {
		return 0, err
	}
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "a:0",
		"-show_entries", "format=duration", "-of", "default=nw=1:nk=1", "-i", in)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe: %v: %s", err, lastLine(stderr.String()))
	}
	s := strings.TrimSpace(string(out))
	d, err := strconv.ParseFloat(s, 64)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("not audio (duration %q)", s)
	}
	return d, nil
}

// NormalizeWAV decodes any audio and returns 48 kHz mono 16-bit PCM WAV.
func NormalizeWAV(ctx context.Context, data []byte) ([]byte, error) {
	return reencode(ctx, data, "-ac", "1", "-ar", outRate, "-c:a", "pcm_s16le", "-f", "wav")
}

// Tempo time-stretches wav by rate without changing pitch. rate 1 returns
// wav unchanged; rate outside 0.5-2.0 is an error.
func Tempo(ctx context.Context, wav []byte, rate float64) ([]byte, error) {
	if rate == 1.0 {
		return wav, nil
	}
	if rate < 0.5 || rate > 2.0 {
		return nil, fmt.Errorf("rate %.2f outside 0.5-2.0", rate)
	}
	return reencode(ctx, wav, "-filter:a", fmt.Sprintf("atempo=%.3f", rate), "-c:a", "pcm_s16le", "-f", "wav")
}

// ConcatWAV joins parts in order with gap of silence between consecutive
// parts. Output is 48 kHz mono 16-bit WAV.
func ConcatWAV(ctx context.Context, parts [][]byte, gap time.Duration) ([]byte, error) {
	if len(parts) == 0 {
		return nil, fmt.Errorf("no parts")
	}
	dir, err := os.MkdirTemp("", "tts-concat-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	// Inputs alternate part, gap, part, ...; one lavfi silence input per gap
	// because a filter-graph input pad can feed only one consumer.
	args := []string{"-hide_banner", "-loglevel", "error", "-y"}
	var filter, seq strings.Builder
	n := 0
	for i, p := range parts {
		if i > 0 {
			args = append(args, "-f", "lavfi", "-t", fmt.Sprintf("%.3f", gap.Seconds()),
				"-i", "anullsrc=channel_layout=mono:sample_rate="+outRate)
			fmt.Fprintf(&filter, "[%d:a]aformat=sample_fmts=s16:channel_layouts=mono[x%d];", n, n)
			fmt.Fprintf(&seq, "[x%d]", n)
			n++
		}
		f := filepath.Join(dir, fmt.Sprintf("p%03d.wav", i))
		if err := os.WriteFile(f, p, 0o600); err != nil {
			return nil, err
		}
		args = append(args, "-i", f)
		fmt.Fprintf(&filter, "[%d:a]aresample=%s,aformat=sample_fmts=s16:channel_layouts=mono[x%d];", n, outRate, n)
		fmt.Fprintf(&seq, "[x%d]", n)
		n++
	}
	fmt.Fprintf(&filter, "%sconcat=n=%d:v=0:a=1[out]", seq.String(), n)
	out := filepath.Join(dir, "out.wav")
	args = append(args, "-filter_complex", filter.String(), "-map", "[out]", "-c:a", "pcm_s16le", out)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg concat: %v: %s", err, lastLine(stderr.String()))
	}
	return os.ReadFile(out)
}

// reencode runs ffmpeg file-to-file: a WAV written to stdout carries a
// placeholder length header that ffprobe then reads as N/A.
func reencode(ctx context.Context, data []byte, outArgs ...string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "tts-wav-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	in, out := filepath.Join(dir, "in"), filepath.Join(dir, "out.wav")
	if err := os.WriteFile(in, data, 0o600); err != nil {
		return nil, err
	}
	args := append([]string{"-hide_banner", "-loglevel", "error", "-y", "-i", in}, outArgs...)
	args = append(args, out)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg: %v: %s", err, lastLine(stderr.String()))
	}
	return os.ReadFile(out)
}
