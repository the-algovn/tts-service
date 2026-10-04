// Package audio converts between the containers the backends emit and the ones
// callers ask for. It does NOT do loudness work: broadcast mastering belongs to
// the consumer that broadcasts (radio's director/render.go).
package audio

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Convert returns data unchanged when it is already in the wanted container,
// so the common path (the backend emits mp3, caller wants mp3) never forks a process.
func Convert(ctx context.Context, data []byte, fromExt, toExt string) ([]byte, error) {
	if !validExt(fromExt) || !validExt(toExt) {
		return nil, fmt.Errorf("unsupported audio format: %q -> %q", fromExt, toExt)
	}
	if strings.EqualFold(fromExt, toExt) {
		return data, nil
	}
	return run(ctx, data, fromExt, toExt)
}

// validExt guards the extensions that reach a filesystem path. filepath.Join
// cleans "..", it does not reject it, so an unchecked extension escapes the
// temp directory.
func validExt(ext string) bool {
	switch strings.ToLower(ext) {
	case "mp3", "wav":
		return true
	default:
		return false
	}
}

func ToMP3(ctx context.Context, wav []byte) ([]byte, error) {
	return Convert(ctx, wav, "wav", "mp3")
}

func run(ctx context.Context, data []byte, fromExt, toExt string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "tts-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	in := filepath.Join(dir, "in."+fromExt)
	out := filepath.Join(dir, "out."+toExt)
	if err := os.WriteFile(in, data, 0o600); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-nostats", "-y", "-i", in, out)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg %s->%s: %v: %s", fromExt, toExt, err, lastLine(stderr.String()))
	}
	return os.ReadFile(out)
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
