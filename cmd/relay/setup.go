package relay

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/dwethmar/lingo/apps/relay"
	"github.com/dwethmar/lingo/apps/relay/server"
	"github.com/dwethmar/lingo/apps/relay/token"
	"github.com/dwethmar/lingo/cmd/config"
	"github.com/dwethmar/lingo/pkg/clock"
	"github.com/dwethmar/lingo/pkg/database"
	"github.com/dwethmar/lingo/pkg/grpcserver"
	"github.com/dwethmar/lingo/pkg/httpserver"
	protorelay "github.com/dwethmar/lingo/proto/gen/go/private/relay/v1"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	ReadTimeout     = 5 * time.Second
	WriteTimeout    = 10 * time.Second
	IdleTimeout     = 15 * time.Second
	ShutdownTimeout = 5 * time.Second
)

// setupRelay sets up the relay application.
func setupRelay(logger *slog.Logger, _ database.DB) (*relay.Relay, error) {
	signingKeyRegistration, err := config.SigningKeyRegistration()
	if err != nil {
		return nil, err
	}

	signingKeyAuthentication, err := config.SigningKeyAuthentication()
	if err != nil {
		return nil, err
	}

	tokenCreated := make(chan token.Created)
	go func() {
		for created := range tokenCreated {
			logger.Info("Token created", slog.String("email", created.Email), slog.String("token", created.Token))
		}
	}()

	clock := clock.New(time.UTC)

	relay := relay.New(
		logger,
		token.NewManager(
			clock,
			[]byte(signingKeyRegistration),
			15*time.Minute,
			tokenCreated,
		),
		token.NewManager(
			clock,
			[]byte(signingKeyAuthentication),
			5*time.Minute,
			tokenCreated,
		),
	)

	return relay, nil
}

// setupGrpcService sets up a gRPC server for the relay service.
func setupGrpcService(relay *relay.Relay) (*server.Service, error) {
	return server.New(relay), nil
}

// setupGrpcServer sets up a gRPC server for the relay service.
func setupGrpcServer(serverRegisters []func(*grpc.Server)) (*grpcserver.Server, error) {
	grpcPort, err := config.GRPCPort()
	if err != nil {
		return nil, err
	}

	certFile, err := config.GrpcTLSCertFile()
	if err != nil {
		return nil, err
	}

	keyFile, err := config.GrpcTLSKeyFile()
	if err != nil {
		return nil, err
	}

	creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS keys: %v", err)
	}

	grpcAddress := fmt.Sprintf(":%d", grpcPort)
	lis, err := net.Listen("tcp", grpcAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to create listener: %w", err)
	}

	return grpcserver.New(grpcserver.Config{
		Listener: lis,
		ServerOptions: []grpc.ServerOption{
			grpc.Creds(creds),
		},
		ServerRegisters: serverRegisters,
		Reflection:      true,
	}), nil
}

// setupHttpServer
func setupHttpServer(ctx context.Context) (*httpserver.Server, error) {
	port, err := config.HTTPPort()
	if err != nil {
		return nil, err
	}

	relayUrl, err := config.RelayUrl()
	if err != nil {
		return nil, err
	}

	certFile, err := config.HTTPTLSCertFile()
	if err != nil {
		return nil, err
	}

	keyFile, err := config.HTTPTLSKeyFile()
	if err != nil {
		return nil, err
	}

	grpcCertFile, err := config.GrpcTLSCertFile()
	if err != nil {
		return nil, err
	}

	creds, err := credentials.NewClientTLSFromFile(grpcCertFile, "lingo")
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS keys: %v", err)
	}

	dialOptions := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
	}

	mux := runtime.NewServeMux()
	if err := protorelay.RegisterRelayServiceHandlerFromEndpoint(ctx, mux, relayUrl, dialOptions); err != nil {
		return nil, fmt.Errorf("failed to register gateway: %w", err)
	}

	return httpserver.New(httpserver.Config{
		Addr:            fmt.Sprintf(":%d", port),
		Handler:         mux,
		ReadTimeout:     ReadTimeout,
		WriteTimeout:    WriteTimeout,
		IdleTimeout:     IdleTimeout,
		ShutdownTimeout: ShutdownTimeout,
		CertFile:        certFile,
		KeyFile:         keyFile,
		Cors:            true,
	}), nil
}