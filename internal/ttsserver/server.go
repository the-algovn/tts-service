// Package ttsserver implements algovn.tts.v1.TTSService. It owns request
// validation and assembles catalog, cache, backend and pricing; each of those
// concerns lives in its own package.
package ttsserver

import (
	"context"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	ttsv1 "github.com/the-algovn/protos/gen/go/algovn/tts/v1"
)

// maxTextChars bounds one utterance. A DJ break is a few hundred characters and
// a race line under a hundred; anything past this is a caller bug, and letting
// it through would bill for it.
const maxTextChars = 5000

type Deps struct {
	Logger *slog.Logger
}

type Server struct {
	ttsv1.UnimplementedTTSServiceServer
	deps Deps
}

func New(deps Deps) *Server { return &Server{deps: deps} }

func (s *Server) Synthesize(ctx context.Context, req *ttsv1.SynthesizeRequest) (*ttsv1.SynthesizeResponse, error) {
	if req.GetText() == "" {
		return nil, status.Error(codes.InvalidArgument, "text is required")
	}
	if len([]rune(req.GetText())) > maxTextChars {
		return nil, status.Errorf(codes.InvalidArgument, "text exceeds %d characters", maxTextChars)
	}
	return nil, status.Error(codes.Unimplemented, "synthesize not wired yet")
}
