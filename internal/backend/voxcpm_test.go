package backend_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/the-algovn/tts-service/internal/audio"
	"github.com/the-algovn/tts-service/internal/backend"
	"github.com/the-algovn/tts-service/internal/voices"
)

type fakeVoices map[string]voices.Voice

func (f fakeVoices) Get(_ context.Context, id string) (voices.Voice, []byte, error) {
	v, ok := f[id]
	if !ok {
		return voices.Voice{}, nil, voices.ErrNotFound
	}
	return v, []byte("REFWAV"), nil
}

func toneWAV(t *testing.T) []byte {
	t.Helper()
	out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "sine=frequency=300:duration=1:sample_rate=48000", "-f", "wav", "-").Output()
	require.NoError(t, err)
	return out
}

type modelReq struct {
	Text        string  `json:"text"`
	RefWAVB64   *string `json:"ref_wav_b64"`
	RefText     *string `json:"ref_text"`
	Description *string `json:"description"`
}

func longScript() string {
	return strings.Repeat("Day la mot cau noi kha dai cua nguoi dan chuong trinh radio. ", 12)
}

func TestVoxCPMChunksInParallel(t *testing.T) {
	wav := toneWAV(t)
	var inFlight, peak int32
	var mu sync.Mutex
	var texts []string
	var bad []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		defer atomic.AddInt32(&inFlight, -1)
		var req modelReq
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		if err := json.Unmarshal(raw, &req); err != nil {
			bad = append(bad, err.Error())
		} else {
			if req.RefWAVB64 == nil || *req.RefWAVB64 != base64.StdEncoding.EncodeToString([]byte("REFWAV")) {
				bad = append(bad, "ref_wav_b64")
			}
			if req.RefText == nil || *req.RefText != "xin chao" {
				bad = append(bad, "ref_text")
			}
			if req.Description != nil {
				bad = append(bad, "description set")
			}
			texts = append(texts, req.Text)
		}
		mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write(wav)
	}))
	defer srv.Close()

	v := backend.NewVoxCPM(backend.VoxCPMConfig{BaseURL: srv.URL, Parallel: 2, ChunkTimeout: 5 * time.Second},
		fakeVoices{"v_aaaaaaaaaaaa": {ID: "v_aaaaaaaaaaaa", RefText: "xin chao"}})
	out, ext, err := v.Synthesize(context.Background(), longScript(), "v_aaaaaaaaaaaa", 1.0)
	require.NoError(t, err)
	require.Empty(t, bad)
	require.Equal(t, "wav", ext)
	require.LessOrEqual(t, peak, int32(2))
	require.Greater(t, len(texts), 2)
	d, err := audio.Probe(context.Background(), out)
	require.NoError(t, err)
	require.InDelta(t, float64(len(texts))+0.15*float64(len(texts)-1), d, 0.1)
}

func orderedScript() string {
	var b strings.Builder
	for i := 1; i <= 12; i++ {
		fmt.Fprintf(&b, "Cau so %02d la mot cau noi kha dai cua nguoi dan chuong trinh radio. ", i)
	}
	return b.String()
}

// constWAV is a 48 kHz mono s16 WAV of ms milliseconds of one constant sample
// value, so a decoded output identifies which part sits where.
func constWAV(value int16, ms int) []byte {
	n := 48 * ms
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, struct {
		RIFF   [4]byte
		Size   uint32
		WAVE   [4]byte
		Fmt    [4]byte
		FmtLen uint32
		Format uint16
		Chans  uint16
		Rate   uint32
		BRate  uint32
		Align  uint16
		Bits   uint16
		Data   [4]byte
		DLen   uint32
	}{[4]byte{'R', 'I', 'F', 'F'}, uint32(36 + 2*n), [4]byte{'W', 'A', 'V', 'E'}, [4]byte{'f', 'm', 't', ' '},
		16, 1, 1, 48000, 96000, 2, 16, [4]byte{'d', 'a', 't', 'a'}, uint32(2 * n)})
	for i := 0; i < n; i++ {
		_ = binary.Write(&buf, binary.LittleEndian, value)
	}
	return buf.Bytes()
}

// runValues returns the distinct constant values of the PCM in output order,
// ignoring the zero-valued gaps.
func runValues(t *testing.T, wav []byte) []int16 {
	t.Helper()
	i := bytes.Index(wav, []byte("data"))
	require.GreaterOrEqual(t, i, 0)
	pcm := wav[i+8:]
	var runs []int16
	var last int16
	for j := 0; j+1 < len(pcm); j += 2 {
		s := int16(binary.LittleEndian.Uint16(pcm[j:]))
		if s != 0 && s != last {
			runs = append(runs, s)
		}
		if s != 0 {
			last = s
		}
	}
	return runs
}

func TestVoxCPMConcatsChunksInTextOrder(t *testing.T) {
	var mu sync.Mutex
	var texts, bad []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req modelReq
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &req)
		var n int
		if _, err := fmt.Sscanf(req.Text, "Cau so %d", &n); err != nil {
			mu.Lock()
			bad = append(bad, req.Text)
			mu.Unlock()
			n = 1
		}
		mu.Lock()
		texts = append(texts, req.Text)
		mu.Unlock()
		// Later chunks answer sooner, so completion order is the reverse of text order.
		time.Sleep(time.Duration(13-n) * 20 * time.Millisecond)
		_, _ = w.Write(constWAV(int16(1000*n), 100+10*n))
	}))
	defer srv.Close()
	v := backend.NewVoxCPM(backend.VoxCPMConfig{BaseURL: srv.URL, Parallel: 4, ChunkTimeout: 5 * time.Second},
		fakeVoices{"v_aaaaaaaaaaaa": {RefText: "x"}})
	out, _, err := v.Synthesize(context.Background(), orderedScript(), "v_aaaaaaaaaaaa", 1.0)
	require.NoError(t, err)
	require.Empty(t, bad)
	require.Greater(t, len(texts), 2)

	got := runValues(t, out)
	require.Len(t, got, len(texts))
	require.True(t, sort.SliceIsSorted(got, func(i, j int) bool { return got[i] < got[j] }),
		"chunks concatenated out of order: %v", got)
	require.Equal(t, strings.TrimSpace(orderedScript()), strings.Join(sortedByNumber(texts), " "))
}

func sortedByNumber(texts []string) []string {
	out := append([]string(nil), texts...)
	key := func(s string) int { var n int; _, _ = fmt.Sscanf(s, "Cau so %d", &n); return n }
	sort.Slice(out, func(i, j int) bool { return key(out[i]) < key(out[j]) })
	return out
}

func TestVoxCPMSpreadsABreakOverReplicas(t *testing.T) {
	var calls int32
	wav := toneWAV(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = w.Write(wav)
	}))
	defer srv.Close()
	text := strings.Repeat("Mot cau noi ngan cua nguoi dan chuong trinh radio. ", 10)
	require.GreaterOrEqual(t, len([]rune(text)), 500)
	v := backend.NewVoxCPM(backend.VoxCPMConfig{BaseURL: srv.URL, Parallel: 3, ChunkTimeout: 5 * time.Second},
		fakeVoices{"v_aaaaaaaaaaaa": {RefText: "x"}})
	_, _, err := v.Synthesize(context.Background(), text, "v_aaaaaaaaaaaa", 1.0)
	require.NoError(t, err)
	require.GreaterOrEqual(t, atomic.LoadInt32(&calls), int32(3))
}

func TestVoxCPMRetriesWhileBusy(t *testing.T) {
	wav := toneWAV(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("busy"))
			return
		}
		_, _ = w.Write(wav)
	}))
	defer srv.Close()
	v := backend.NewVoxCPM(backend.VoxCPMConfig{BaseURL: srv.URL, Parallel: 1, ChunkTimeout: 5 * time.Second},
		fakeVoices{"v_aaaaaaaaaaaa": {RefText: "x"}})
	out, _, err := v.Synthesize(context.Background(), "xin chao.", "v_aaaaaaaaaaaa", 1.0)
	require.NoError(t, err)
	require.NotEmpty(t, out)
	require.Equal(t, int32(3), atomic.LoadInt32(&calls))
}

func TestVoxCPMRetryStopsWhenParentContextEnds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	v := backend.NewVoxCPM(backend.VoxCPMConfig{BaseURL: srv.URL, Parallel: 1, ChunkTimeout: 5 * time.Second},
		fakeVoices{"v_aaaaaaaaaaaa": {RefText: "x"}})
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, err := v.Synthesize(ctx, "xin chao.", "v_aaaaaaaaaaaa", 1.0)
	require.Error(t, err)
	require.Less(t, time.Since(start), 3*time.Second)
	require.GreaterOrEqual(t, atomic.LoadInt32(&calls), int32(1))
}

func TestVoxCPMRetriesConnectionErrors(t *testing.T) {
	wav := toneWAV(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			conn, _, _ := w.(http.Hijacker).Hijack()
			_ = conn.Close()
			return
		}
		_, _ = w.Write(wav)
	}))
	defer srv.Close()
	v := backend.NewVoxCPM(backend.VoxCPMConfig{BaseURL: srv.URL, Parallel: 1, ChunkTimeout: 5 * time.Second},
		fakeVoices{"v_aaaaaaaaaaaa": {RefText: "x"}})
	_, _, err := v.Synthesize(context.Background(), "xin chao.", "v_aaaaaaaaaaaa", 1.0)
	require.NoError(t, err)
	require.Equal(t, int32(2), atomic.LoadInt32(&calls))
}

func TestVoxCPMDoesNotRetryServerErrors(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	v := backend.NewVoxCPM(backend.VoxCPMConfig{BaseURL: srv.URL, Parallel: 1, ChunkTimeout: 5 * time.Second},
		fakeVoices{"v_aaaaaaaaaaaa": {RefText: "x"}})
	_, _, err := v.Synthesize(context.Background(), "xin chao.", "v_aaaaaaaaaaaa", 1.0)
	require.ErrorContains(t, err, "500")
	require.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestVoxCPMRejectsInvalidInput(t *testing.T) {
	v := backend.NewVoxCPM(backend.VoxCPMConfig{BaseURL: "http://unused", Parallel: 1, ChunkTimeout: time.Second},
		fakeVoices{"v_aaaaaaaaaaaa": {RefText: "x"}})
	for name, c := range map[string]struct {
		text string
		rate float64
	}{
		"blank":    {"  \n\t ", 1.0},
		"too slow": {"xin chao.", 0.4},
		"too fast": {"xin chao.", 2.5},
	} {
		_, _, err := v.Synthesize(context.Background(), c.text, "v_aaaaaaaaaaaa", c.rate)
		require.True(t, errors.Is(err, backend.ErrInvalidInput), name)
	}
}

type countingVoices struct {
	fakeVoices
	gets int32
}

func (c *countingVoices) Get(ctx context.Context, id string) (voices.Voice, []byte, error) {
	atomic.AddInt32(&c.gets, 1)
	return c.fakeVoices.Get(ctx, id)
}

func TestVoxCPMCachesVoiceLookups(t *testing.T) {
	wav := toneWAV(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(wav) }))
	defer srv.Close()
	src := &countingVoices{fakeVoices: fakeVoices{"v_aaaaaaaaaaaa": {RefText: "x"}}}
	v := backend.NewVoxCPM(backend.VoxCPMConfig{BaseURL: srv.URL, Parallel: 1, ChunkTimeout: 5 * time.Second}, src)
	for i := 0; i < 2; i++ {
		_, _, err := v.Synthesize(context.Background(), "xin chao.", "v_aaaaaaaaaaaa", 1.0)
		require.NoError(t, err)
	}
	require.Equal(t, int32(1), atomic.LoadInt32(&src.gets))

	for i := 0; i < 2; i++ {
		_, _, err := v.Synthesize(context.Background(), "xin chao.", "v_missing00000", 1.0)
		require.True(t, errors.Is(err, voices.ErrNotFound))
	}
	require.Equal(t, int32(3), atomic.LoadInt32(&src.gets), "a failed Get must not be cached")
}

func TestVoxCPMOneChunkFailureFailsWhole(t *testing.T) {
	wav := toneWAV(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 2 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("oom"))
			return
		}
		_, _ = w.Write(wav)
	}))
	defer srv.Close()
	v := backend.NewVoxCPM(backend.VoxCPMConfig{BaseURL: srv.URL, Parallel: 1, ChunkTimeout: 5 * time.Second},
		fakeVoices{"v_aaaaaaaaaaaa": {RefText: "x"}})
	out, _, err := v.Synthesize(context.Background(), longScript(), "v_aaaaaaaaaaaa", 1.0)
	require.ErrorContains(t, err, "oom")
	require.Nil(t, out)
}

func TestVoxCPMUnknownVoiceIsNotFound(t *testing.T) {
	v := backend.NewVoxCPM(backend.VoxCPMConfig{BaseURL: "http://unused", Parallel: 1, ChunkTimeout: time.Second}, fakeVoices{})
	_, _, err := v.Synthesize(context.Background(), "xin chao.", "v_missing00000", 1.0)
	require.True(t, errors.Is(err, voices.ErrNotFound))
}

func TestVoxCPMDesignSendsDescriptionWithoutRef(t *testing.T) {
	wav := toneWAV(t)
	var got []modelReq
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req modelReq
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &req)
		mu.Lock()
		got = append(got, req)
		mu.Unlock()
		_, _ = w.Write(wav)
	}))
	defer srv.Close()
	v := backend.NewVoxCPM(backend.VoxCPMConfig{BaseURL: srv.URL, Parallel: 3, ChunkTimeout: time.Second}, fakeVoices{})
	takes, err := v.Design(context.Background(), "giong nu tre", "Chao ban.", 3)
	require.NoError(t, err)
	require.Len(t, takes, 3)
	require.Len(t, got, 3)
	for _, r := range got {
		require.Equal(t, "giong nu tre", *r.Description)
		require.Equal(t, "Chao ban.", r.Text)
		require.Nil(t, r.RefWAVB64)
	}
}

func TestVoxCPMChunkTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()
	v := backend.NewVoxCPM(backend.VoxCPMConfig{BaseURL: srv.URL, Parallel: 1, ChunkTimeout: 50 * time.Millisecond},
		fakeVoices{"v_aaaaaaaaaaaa": {RefText: "x"}})
	_, _, err := v.Synthesize(context.Background(), "xin chao.", "v_aaaaaaaaaaaa", 1.0)
	require.Error(t, err)
}

func TestVoxCPMChunkTimeoutGrowsWithText(t *testing.T) {
	wav := toneWAV(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write(wav)
	}))
	defer srv.Close()
	v := backend.NewVoxCPM(backend.VoxCPMConfig{BaseURL: srv.URL, Parallel: 1,
		ChunkTimeout: 50 * time.Millisecond, CharTimeout: 100 * time.Millisecond},
		fakeVoices{"v_aaaaaaaaaaaa": {RefText: "x"}})
	_, _, err := v.Synthesize(context.Background(), "xin chao.", "v_aaaaaaaaaaaa", 1.0)
	require.NoError(t, err)
}
