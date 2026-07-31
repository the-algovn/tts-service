// tts: the shared Vietnamese text-to-speech service. Internal gRPC only --
// never registered at a gateway.
package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/the-algovn/gopkg/obs"
	ttsv1 "github.com/the-algovn/protos/gen/go/algovn/tts/v1"
	"github.com/the-algovn/tts-service/internal/config"
	"github.com/the-algovn/tts-service/internal/ttsserver"
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
	ttsv1.RegisterTTSServiceServer(srv, ttsserver.New(ttsserver.Deps{Logger: logger}))
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
