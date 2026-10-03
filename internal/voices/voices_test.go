package voices_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/the-algovn/tts-service/internal/voices"
)

func fixedNow() time.Time { return time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC) }

func validNew() voices.NewVoice {
	return voices.NewVoice{Label: "Duong Duong", Gender: "FEMALE", RefText: "xin chao", Source: "clone"}
}

func TestCreateThenGetRoundTrips(t *testing.T) {
	r := voices.NewRegistry(voices.NewMemory(), fixedNow)
	v, err := r.Create(context.Background(), validNew(), []byte("RIFFwav"))
	require.NoError(t, err)
	require.Regexp(t, `^v_[0-9a-f]{12}$`, v.ID)
	require.Equal(t, fixedNow(), v.CreatedAt)

	got, wav, err := r.Get(context.Background(), v.ID)
	require.NoError(t, err)
	require.Equal(t, v, got)
	require.Equal(t, []byte("RIFFwav"), wav)
}

func TestIDsAreNeverReused(t *testing.T) {
	r := voices.NewRegistry(voices.NewMemory(), fixedNow)
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		v, err := r.Create(context.Background(), validNew(), []byte("RIFF"))
		require.NoError(t, err)
		require.False(t, seen[v.ID])
		seen[v.ID] = true
	}
}

func TestGetMissingIsErrNotFound(t *testing.T) {
	r := voices.NewRegistry(voices.NewMemory(), fixedNow)
	_, _, err := r.Get(context.Background(), "v_000000000000")
	require.True(t, errors.Is(err, voices.ErrNotFound))
}

func TestListIgnoresRefWithoutMetadata(t *testing.T) {
	s := voices.NewMemory()
	require.NoError(t, s.Put(context.Background(), "voices/v_aaaaaaaaaaaa/ref.wav", []byte("RIFF")))
	r := voices.NewRegistry(s, fixedNow)
	v, err := r.Create(context.Background(), validNew(), []byte("RIFF"))
	require.NoError(t, err)

	list, err := r.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, v.ID, list[0].ID)
}

func TestDeleteRemovesFromList(t *testing.T) {
	r := voices.NewRegistry(voices.NewMemory(), fixedNow)
	v, err := r.Create(context.Background(), validNew(), []byte("RIFF"))
	require.NoError(t, err)
	require.NoError(t, r.Delete(context.Background(), v.ID))
	list, err := r.List(context.Background())
	require.NoError(t, err)
	require.Empty(t, list)
	require.True(t, errors.Is(r.Delete(context.Background(), v.ID), voices.ErrNotFound))
}

func TestValidateNew(t *testing.T) {
	cases := map[string]func(*voices.NewVoice){
		"empty label":    func(n *voices.NewVoice) { n.Label = "" },
		"long label":     func(n *voices.NewVoice) { n.Label = strings.Repeat("a", 81) },
		"bad gender":     func(n *voices.NewVoice) { n.Gender = "X" },
		"empty ref text": func(n *voices.NewVoice) { n.RefText = " " },
		"long ref text":  func(n *voices.NewVoice) { n.RefText = strings.Repeat("a", 501) },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			n := validNew()
			mut(&n)
			var inv *voices.InvalidError
			require.ErrorAs(t, voices.ValidateNew(n), &inv)
		})
	}
	require.NoError(t, voices.ValidateNew(validNew()))
}

func TestValidateNewAcceptsBoundaries(t *testing.T) {
	cases := map[string]func(*voices.NewVoice){
		"80 rune label":       func(n *voices.NewVoice) { n.Label = strings.Repeat("a", 80) },
		"500 rune ref text":   func(n *voices.NewVoice) { n.RefText = strings.Repeat("a", 500) },
		"male":                func(n *voices.NewVoice) { n.Gender = "MALE" },
		"neutral":             func(n *voices.NewVoice) { n.Gender = "NEUTRAL" },
		"multibyte label":     func(n *voices.NewVoice) { n.Label = strings.Repeat("\u1ec7", 80) },
		"padded 80 label":     func(n *voices.NewVoice) { n.Label = " " + strings.Repeat("a", 80) + " " },
		"padded 500 ref text": func(n *voices.NewVoice) { n.RefText = " " + strings.Repeat("a", 500) + " " },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			n := validNew()
			mut(&n)
			require.NoError(t, voices.ValidateNew(n))
		})
	}
	n := validNew()
	n.Label = strings.Repeat("\u1ec7", 81)
	require.Error(t, voices.ValidateNew(n))
}

type listOnlyStore struct {
	voices.Store
	keys []string
}

func (s listOnlyStore) List(context.Context, string) ([]string, error) { return s.keys, nil }

func TestListSkipsVoiceDeletedAfterListing(t *testing.T) {
	s := listOnlyStore{Store: voices.NewMemory(), keys: []string{"voices/v_aaaaaaaaaaaa/voice.json"}}
	list, err := voices.NewRegistry(s, fixedNow).List(context.Background())
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestListReturnsDecodeErrors(t *testing.T) {
	m := voices.NewMemory()
	require.NoError(t, m.Put(context.Background(), "voices/v_aaaaaaaaaaaa/voice.json", []byte("{")))
	_, err := voices.NewRegistry(m, fixedNow).List(context.Background())
	require.Error(t, err)
	require.False(t, errors.Is(err, voices.ErrNotFound))
}

func TestMalformedIDIsNotFound(t *testing.T) {
	r := voices.NewRegistry(voices.NewMemory(), fixedNow)
	for _, id := range []string{"", "v_../../x", "V_AAAAAAAAAAAA", "v_aaaaaaaaaaa"} {
		_, _, err := r.Get(context.Background(), id)
		require.True(t, errors.Is(err, voices.ErrNotFound), id)
		require.True(t, errors.Is(r.Delete(context.Background(), id), voices.ErrNotFound), id)
	}
}

func TestListIsOrderedByCreatedAt(t *testing.T) {
	tick := fixedNow()
	r := voices.NewRegistry(voices.NewMemory(), func() time.Time {
		tick = tick.Add(time.Minute)
		return tick
	})
	var want []string
	for i := 0; i < 10; i++ {
		v, err := r.Create(context.Background(), validNew(), []byte("RIFF"))
		require.NoError(t, err)
		want = append(want, v.ID)
	}
	list, err := r.List(context.Background())
	require.NoError(t, err)
	var got []string
	for _, v := range list {
		got = append(got, v.ID)
	}
	require.Equal(t, want, got)
}

func TestCreateRejectsInvalidAndWritesNothing(t *testing.T) {
	s := voices.NewMemory()
	r := voices.NewRegistry(s, fixedNow)
	n := validNew()
	n.Label = ""
	_, err := r.Create(context.Background(), n, []byte("RIFF"))
	require.Error(t, err)
	keys, err := s.List(context.Background(), "voices/")
	require.NoError(t, err)
	require.Empty(t, keys)
}
