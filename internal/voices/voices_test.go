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
	v, _ := r.Create(context.Background(), validNew(), []byte("RIFF"))
	require.NoError(t, r.Delete(context.Background(), v.ID))
	list, _ := r.List(context.Background())
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

func TestCreateRejectsInvalidAndWritesNothing(t *testing.T) {
	s := voices.NewMemory()
	r := voices.NewRegistry(s, fixedNow)
	n := validNew()
	n.Label = ""
	_, err := r.Create(context.Background(), n, []byte("RIFF"))
	require.Error(t, err)
	keys, _ := s.List(context.Background(), "voices/")
	require.Empty(t, keys)
}
