package ttsserver_test

import (
	"context"
	"log/slog"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	ttsv1 "github.com/the-algovn/protos/gen/go/algovn/tts/v1"
	"github.com/the-algovn/tts-service/internal/backend"
	"github.com/the-algovn/tts-service/internal/ttsserver"
	"github.com/the-algovn/tts-service/internal/voices"
)

type fakeDesigner struct{ takes int }

func (f *fakeDesigner) Design(_ context.Context, _, _ string, takes int) ([][]byte, error) {
	f.takes = takes
	out := make([][]byte, takes)
	for i := range out {
		out[i] = []byte("RIFFtake")
	}
	return out, nil
}

func clip(t *testing.T, seconds string) []byte {
	t.Helper()
	out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "sine=frequency=200:duration="+seconds, "-f", "mp3", "-").Output()
	require.NoError(t, err)
	return out
}

func registryServer(t *testing.T) (*ttsserver.Server, *voices.Memory) {
	store := voices.NewMemory()
	reg := voices.NewRegistry(store, time.Now)
	return ttsserver.New(ttsserver.Deps{
		Logger: slog.Default(), Backends: map[string]backend.Backend{"fake": backend.Fake{}},
		Voices: reg, Designer: &fakeDesigner{},
	}), store
}

func TestCreateVoiceThenListed(t *testing.T) {
	s, _ := registryServer(t)
	resp, err := s.CreateVoice(context.Background(), &ttsv1.CreateVoiceRequest{
		Label: "Duong Duong", Gender: "FEMALE", RefAudio: clip(t, "5"), RefText: "xin chao cac ban"})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(resp.GetVoice().GetId(), "voxcpm:v_"))
	require.Equal(t, "voxcpm", resp.GetVoice().GetProvider())
	require.Equal(t, "self-hosted", resp.GetVoice().GetTier())

	list, err := s.ListVoices(context.Background(), &ttsv1.ListVoicesRequest{})
	require.NoError(t, err)
	var ids []string
	for _, v := range list.GetVoices() {
		ids = append(ids, v.GetId())
	}
	require.Contains(t, ids, resp.GetVoice().GetId())
}

func TestCreateVoiceRejectsBadAudioAndWritesNothing(t *testing.T) {
	cases := map[string][]byte{
		"not audio": []byte("\xff\xd8\xff\xe0jpeg"),
		"too short": clip(t, "2"),
		"too long":  clip(t, "31"),
	}
	for name, audioBytes := range cases {
		t.Run(name, func(t *testing.T) {
			s, store := registryServer(t)
			_, err := s.CreateVoice(context.Background(), &ttsv1.CreateVoiceRequest{
				Label: "x", Gender: "MALE", RefAudio: audioBytes, RefText: "x"})
			require.Equal(t, codes.InvalidArgument, status.Code(err))
			keys, _ := store.List(context.Background(), "")
			require.Empty(t, keys)
		})
	}
}

func TestCreateVoiceRejectsBadFields(t *testing.T) {
	s, _ := registryServer(t)
	_, err := s.CreateVoice(context.Background(), &ttsv1.CreateVoiceRequest{
		Label: "", Gender: "MALE", RefAudio: clip(t, "5"), RefText: "x"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestDesignVoiceBoundsTakes(t *testing.T) {
	s, _ := registryServer(t)
	for _, n := range []int32{0, 4} {
		_, err := s.DesignVoice(context.Background(), &ttsv1.DesignVoiceRequest{
			Description: "giong nu", SampleText: "chao", Takes: n})
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	}
	resp, err := s.DesignVoice(context.Background(), &ttsv1.DesignVoiceRequest{
		Description: "giong nu", SampleText: "chao", Takes: 2})
	require.NoError(t, err)
	require.Len(t, resp.GetTakes(), 2)
}

func TestDeleteVoice(t *testing.T) {
	s, _ := registryServer(t)
	resp, err := s.CreateVoice(context.Background(), &ttsv1.CreateVoiceRequest{
		Label: "x", Gender: "MALE", RefAudio: clip(t, "5"), RefText: "x"})
	require.NoError(t, err)
	_, err = s.DeleteVoice(context.Background(), &ttsv1.DeleteVoiceRequest{Id: resp.GetVoice().GetId()})
	require.NoError(t, err)
	_, err = s.DeleteVoice(context.Background(), &ttsv1.DeleteVoiceRequest{Id: resp.GetVoice().GetId()})
	require.Equal(t, codes.NotFound, status.Code(err))
	_, err = s.DeleteVoice(context.Background(), &ttsv1.DeleteVoiceRequest{Id: "google:vi-VN-Neural2-A"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestRegistryRPCsWithoutRegistry(t *testing.T) {
	s := ttsserver.New(ttsserver.Deps{Logger: slog.Default(), Backends: map[string]backend.Backend{}})
	_, err := s.CreateVoice(context.Background(), &ttsv1.CreateVoiceRequest{})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
}
