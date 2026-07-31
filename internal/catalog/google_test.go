package catalog_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/the-algovn/tts-service/internal/catalog"
)

const voicesJSON = `{"voices":[
  {"languageCodes":["vi-VN"],"name":"vi-VN-Wavenet-B","ssmlGender":"MALE"},
  {"languageCodes":["vi-VN"],"name":"vi-VN-Chirp3-HD-Aoede","ssmlGender":"FEMALE"},
  {"languageCodes":["en-US"],"name":"en-US-Wavenet-A","ssmlGender":"FEMALE"}
]}`

func TestGoogleVoicesNamespacesAndPrices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(voicesJSON))
	}))
	defer srv.Close()

	src := catalog.GoogleSource{APIKey: "k", BaseURL: srv.URL, TTL: time.Minute}
	got, err := src.Voices(context.Background())
	require.NoError(t, err)

	require.Len(t, got, 2, "non vi-VN voices must be filtered out")

	require.Equal(t, "google:vi-VN-Wavenet-B", got[0].ID)
	require.Equal(t, "MALE", got[0].Gender)
	require.Equal(t, "wavenet", got[0].Tier)
	require.EqualValues(t, 4_000_000, got[0].FreeTierChars)

	require.Equal(t, "chirp3-hd", got[1].Tier)
	require.EqualValues(t, 1_000_000, got[1].FreeTierChars)
}

// The key must never appear in the URL: http.Client wraps transport failures
// (timeout, DNS, connection refused) in *url.Error, which formats as
// "GET https://...&key=THEKEY: dial tcp ...", and that error string gets
// logged verbatim by the caller on every transient Google outage.
func TestGoogleVoicesSendsAPIKeyAsHeaderNotQuery(t *testing.T) {
	var gotHeader, gotQueryKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("x-goog-api-key")
		gotQueryKey = r.URL.Query().Get("key")
		_, _ = w.Write([]byte(voicesJSON))
	}))
	defer srv.Close()

	src := catalog.GoogleSource{APIKey: "secret-key", BaseURL: srv.URL, TTL: time.Minute}
	_, err := src.Voices(context.Background())
	require.NoError(t, err)

	require.Equal(t, "secret-key", gotHeader)
	require.Empty(t, gotQueryKey, "api key must not be sent as a query parameter")
}

// A second call inside the TTL must not hit the network again.
func TestGoogleVoicesCaches(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(voicesJSON))
	}))
	defer srv.Close()

	src := catalog.GoogleSource{APIKey: "k", BaseURL: srv.URL, TTL: time.Minute}
	_, err := src.Voices(context.Background())
	require.NoError(t, err)
	_, err = src.Voices(context.Background())
	require.NoError(t, err)

	require.Equal(t, 1, calls)
}
