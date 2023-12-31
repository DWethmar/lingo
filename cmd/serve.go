package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"time"

	"github.com/dwethmar/lingo/cmd/gateway"
	"github.com/dwethmar/lingo/cmd/relay"
	"github.com/dwethmar/lingo/cmd/relay/token"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	_ "github.com/lib/pq"
)

const (
	// defaultPort default port to listen on
	defaultPort = 8080
)

// serveCmd represents the relay command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "serve lingo services",
	Long:  `serve lingo services.`,
}

// relayCmd represents the relay command for rpc
var relayCmd = &cobra.Command{
	Use:   "relay",
	Short: "Start the relay server rpc service",
	Long:  `Start the relay server rpc service.`,
	RunE:  runRelay,
}

// relayRpcCmd represents the relay command for rpc
var gatewayCmd = &cobra.Command{
	Use:   "gateway",
	Short: "Start the gateway http service",
	Long:  `Start the gateway http service.`,
	RunE:  runGateway,
}

// runRelay runs the relay server
func runRelay(cmd *cobra.Command, args []string) error {
	logger := slog.Default()

	dbConn := viper.GetString("db_url")
	if dbConn == "" {
		return fmt.Errorf("db_url is not set")
	}

	db, err := sql.Open("postgres", dbConn)
	if err != nil {
		return fmt.Errorf("could not open db: %w", err)
	}

	defer db.Close()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("could not ping db: %w", err)
	}

	port := viper.GetInt("port")
	if port == 0 {
		return fmt.Errorf("port is not set")
	}

	// create a listener on TCP
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}

	certFile := viper.GetString("tls_cert_file")
	keyFile := viper.GetString("tls_key_file")

	creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
	if err != nil {
		logger.Error("failed to load TLS keys", err)

		return fmt.Errorf("failed to load TLS keys: %v", err)
	}

	signingKeyRegistration := viper.GetString("SIGNING_KEY_REGISTRATION")
	if signingKeyRegistration == "" {
		return fmt.Errorf("SIGNING_KEY_REGISTRATION is not set")
	}

	signingKeyAuthentication := viper.GetString("SIGNING_KEY_AUTHENTICATION")
	if signingKeyAuthentication == "" {
		return fmt.Errorf("SIGNING_KEY_AUTHENTICATION is not set")
	}

	logger.Info("Starting relay server", slog.Int("port", port))

	tokenCreated := make(chan token.Created)
	go func() {
		for created := range tokenCreated {
			logger.Info("Token created", slog.String("email", created.Email), slog.String("token", created.Token))
		}
	}()

	grpcServer := grpc.NewServer(
		grpc.Creds(creds),
	)

	ctx := cmd.Context()
	ctx, cancel := context.WithCancel(ctx)
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	// Cancel context on interrupt signal
	go func() {
		<-c
		cancel()
	}()

	if err := relay.Start(ctx, &relay.Options{
		Server: grpcServer,
		Lis:    lis,
		RegistrationTokenManager: token.NewManager(
			[]byte(signingKeyRegistration),
			15*time.Minute,
			tokenCreated,
		),
		AuthenticationTokenManager: token.NewManager(
			[]byte(signingKeyAuthentication),
			5*time.Minute,
			tokenCreated,
		),
		Logger: logger,
	}); err != nil {
		logger.Error("failed to start relay server", err)
		return fmt.Errorf("could not start relay server: %w", err)
	}

	return nil
}

// runGateway runs the gateway server
func runGateway(cmd *cobra.Command, args []string) error {
	logger := slog.Default()

	port := viper.GetInt("port")
	if port == 0 {
		return fmt.Errorf("port is not set")
	}

	relayUrl := viper.GetString("relay_url")
	if relayUrl == "" {
		return fmt.Errorf("relay_url is not set")
	}

	certFile := viper.GetString("tls_cert_file")
	if certFile == "" {
		return fmt.Errorf("tls_cert_file is not set")
	}

	creds, err := credentials.NewClientTLSFromFile(certFile, "lingo")
	if err != nil {
		return fmt.Errorf("failed to load TLS keys: %v", err)
	}

	if err := gateway.Start(cmd.Context(), &gateway.Options{
		Logger:   logger,
		Creds:    creds,
		Port:     port,
		RelayUrl: relayUrl,
	}); err != nil {
		return fmt.Errorf("could not start gateway: %w", err)
	}

	return nil
}

func setupEnv() error {
	if err := viper.BindEnv("DB_URL"); err != nil {
		return fmt.Errorf("could not bind db_url: %w", err)
	}

	if err := viper.BindEnv("PORT"); err != nil {
		return fmt.Errorf("could not bind port: %w", err)
	}

	if err := viper.BindEnv("TLS_CERT_FILE"); err != nil {
		return fmt.Errorf("could not bind tls_cert_file: %w", err)
	}

	if err := viper.BindEnv("TLS_KEY_FILE"); err != nil {
		return fmt.Errorf("could not bind tls_key_file: %w", err)
	}

	if err := viper.BindEnv("RELAY_URL"); err != nil {
		return fmt.Errorf("could not bind relay_url: %w", err)
	}

	if err := viper.BindPFlags(serveCmd.Flags()); err != nil {
		return fmt.Errorf("could not bind flags: %w", err)
	}

	return nil
}

func init() {
	// serve flags
	serveCmd.Flags().IntP("port", "p", defaultPort, "Port to listen on")

	// relay flags
	relayCmd.Flags().StringP("db_url", "d", "", "Database connection string")

	// gateway flags
	gatewayCmd.Flags().StringP("relay-url", "r", "", "address of the relay service")

	if err := setupEnv(); err != nil {
		panic(err)
	}

	serveCmd.AddCommand(relayCmd)
	serveCmd.AddCommand(gatewayCmd)
	rootCmd.AddCommand(serveCmd)
}
