// tts: the shared Vietnamese text-to-speech service. Internal gRPC only --
// never registered at a gateway.
package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/the-algovn/gopkg/obs"
	ttsv1 "github.com/the-algovn/protos/gen/go/algovn/tts/v1"
	"github.com/the-algovn/tts-service/internal/backend"
	"github.com/the-algovn/tts-service/internal/cache"
	"github.com/the-algovn/tts-service/internal/catalog"
	"github.com/the-algovn/tts-service/internal/config"
	"github.com/the-algovn/tts-service/internal/ttsserver"
	"github.com/the-algovn/tts-service/internal/voices"
)

// version is stamped at build time with -ldflags "-X main.version=<sha>".
var version = "dev"

func main() {
	obsCfg, err := obs.ConfigFromEnv("tts-service", version)
	if err != nil {
		slog.Error("observability config", "err", err)
		os.Exit(1)
	}
	shutdownObs, err := obs.Setup(context.Background(), obsCfg)
	if err != nil {
		slog.Error("observability setup", "err", err)
		os.Exit(1)
	}
	logger := slog.Default()
	defer func() { _ = shutdownObs(context.Background()) }()

	if err := config.Load(); err != nil {
		logger.Error("config load failed", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	addr := config.Get("LISTEN_ADDR", ":9490")
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		logger.ErrorContext(ctx, "listen failed", "addr", addr, "err", err)
		os.Exit(1)
	}

	srv := grpc.NewServer(
		grpc.StatsHandler(obs.ServerHandler()),
		grpc.ChainUnaryInterceptor(obs.UnaryServerInterceptor()),
	)
	backends := map[string]backend.Backend{"fake": backend.Fake{}}
	var googleSrc *catalog.GoogleSource
	googleIsFake := false
	if k := config.Get("GOOGLE_TTS_API_KEY", ""); k != "" {
		backends["google"] = backend.NewGoogle(k)
		googleSrc = &catalog.GoogleSource{APIKey: k, TTL: time.Hour}
	} else {
		// Keyless dev: bare and google-namespaced ids still resolve, to
		// silence. googleIsFake tells the server so it reports the actual
		// (fake) provider instead of "google".
		backends["google"] = backend.Fake{}
		googleIsFake = true
		logger.WarnContext(ctx, "no google tts key; google voices synthesize silence")
	}

	if u := config.Get("VIENEU_URL", ""); u != "" {
		backends["vieneu"] = backend.NewVieNeu(u)
	}

	var store cache.Store
	if ep := config.Get("MINIO_ENDPOINT", ""); ep != "" {
		s3, err := cache.NewS3(cache.Config{
			Endpoint:  ep,
			AccessKey: config.Get("MINIO_ACCESS_KEY", ""),
			SecretKey: config.Get("MINIO_SECRET_KEY", ""),
			Bucket:    config.Get("MINIO_BUCKET", "tts-cache"),
			UseSSL:    config.Get("MINIO_USE_SSL", "false") == "true",
		})
		if err != nil {
			logger.ErrorContext(ctx, "minio setup failed", "err", err)
			os.Exit(1)
		}
		store = s3
	}

	var registry *voices.Registry
	var designer ttsserver.Designer
	if u := config.Get("VOXCPM_URL", ""); u != "" {
		ep := config.Get("MINIO_ENDPOINT", "")
		if ep == "" {
			logger.ErrorContext(ctx, "VOXCPM_URL needs MINIO_ENDPOINT for the voice registry")
			os.Exit(1)
		}
		vs, err := cache.NewS3(cache.Config{
			Endpoint:  ep,
			AccessKey: config.Get("MINIO_ACCESS_KEY", ""),
			SecretKey: config.Get("MINIO_SECRET_KEY", ""),
			Bucket:    config.Get("VOICES_BUCKET", "tts-voices"),
			UseSSL:    config.Get("MINIO_USE_SSL", "false") == "true",
		})
		if err != nil {
			logger.ErrorContext(ctx, "voice store setup failed", "err", err)
			os.Exit(1)
		}
		registry = voices.NewRegistry(vs, time.Now)
		parallel, err := strconv.Atoi(config.Get("VOXCPM_PARALLEL", "3"))
		if err != nil || parallel < 1 {
			logger.ErrorContext(ctx, "VOXCPM_PARALLEL must be a positive integer")
			os.Exit(1)
		}
		timeout, err := time.ParseDuration(config.Get("VOXCPM_CHUNK_TIMEOUT", "180s"))
		if err != nil || timeout <= 0 {
			logger.ErrorContext(ctx, "VOXCPM_CHUNK_TIMEOUT must be a positive duration", "err", err)
			os.Exit(1)
		}
		vx := backend.NewVoxCPM(backend.VoxCPMConfig{BaseURL: u, Parallel: parallel, ChunkTimeout: timeout}, registry)
		backends["voxcpm"] = vx
		designer = vx
	}

	ttsv1.RegisterTTSServiceServer(srv, ttsserver.New(ttsserver.Deps{
		Logger:       logger,
		Backends:     backends,
		Cache:        store,
		Google:       googleSrc,
		VieNeuVoices: catalog.VieNeuVoices(),
		GoogleIsFake: googleIsFake,
		Voices:       registry,
		Designer:     designer,
	}))
	healthpb.RegisterHealthServer(srv, health.NewServer())
	reflection.Register(srv)

	go func() {
		<-ctx.Done()
		logger.InfoContext(ctx, "shutting down")
		srv.GracefulStop()
	}()

	logger.InfoContext(ctx, "tts-service listening", "addr", addr)
	if err := srv.Serve(lis); err != nil {
		logger.ErrorContext(ctx, "serve failed", "err", err)
		os.Exit(1)
	}
}
