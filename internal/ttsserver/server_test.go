package ttsserver_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	ttsv1 "github.com/the-algovn/protos/gen/go/algovn/tts/v1"
	"github.com/the-algovn/tts-service/internal/ttsserver"
)

// An empty text is a caller bug, not a provider failure: it must be rejected
// before any backend or billing decision is made.
func TestSynthesizeRejectsEmptyText(t *testing.T) {
	s := ttsserver.New(ttsserver.Deps{Logger: slog.Default()})

	_, err := s.Synthesize(context.Background(), &ttsv1.SynthesizeRequest{
		Text:    "",
		VoiceId: "google:vi-VN-Wavenet-B",
	})

	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
