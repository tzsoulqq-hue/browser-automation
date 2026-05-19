package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	browserautomationv1 "github.com/byte-v-forge/browser-automation/gen/go/byte/v/forge/contracts/browserautomation/v1"
	grpcadapter "github.com/byte-v-forge/browser-automation/internal/adapters/grpc"
	"github.com/byte-v-forge/browser-automation/internal/adapters/repository/postgres"
	"github.com/byte-v-forge/browser-automation/internal/adapters/runtime/camoufox"
	"github.com/byte-v-forge/browser-automation/internal/app"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
)

const (
	defaultListenAddr           = ":50051"
	defaultRuntime              = "camoufox"
	defaultMigrationsDir        = "migrations"
	defaultArtifactsDir         = "/tmp/browser-automation-artifacts"
	defaultPostgresMaxConns     = 8
	defaultConnectTimeout       = 10 * time.Second
	defaultStatementTimeout     = 10 * time.Second
	defaultShutdownGrace        = 10 * time.Second
	defaultCamoufoxStartup      = 30 * time.Second
	defaultCamoufoxShutdown     = 5 * time.Second
	defaultCamoufoxTaskTimeout  = 2 * time.Minute
	defaultCamoufoxWSPathPrefix = "browser-session-"
)

type config struct {
	ListenAddr               string
	PostgresDSN              string
	PostgresMaxConns         int32
	PostgresConnectTimeout   time.Duration
	PostgresStatementTimeout time.Duration
	ApplyMigrations          bool
	MigrationsDir            string
	ShutdownGrace            time.Duration

	Runtime string

	CamoufoxPythonPath      string
	CamoufoxArtifactsDir    string
	CamoufoxStartupTimeout  time.Duration
	CamoufoxShutdownTimeout time.Duration
	CamoufoxTaskTimeout     time.Duration
	CamoufoxHeadless        bool
	CamoufoxServerPort      int
	CamoufoxWSPathPrefix    string
	CamoufoxExtraEnv        []string
}

func main() {
	if err := run(); err != nil {
		slog.Error("browser automation service stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	rootCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	pool, err := postgres.NewPool(rootCtx, cfg.PostgresDSN, cfg.PostgresMaxConns, cfg.PostgresConnectTimeout)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	if cfg.ApplyMigrations {
		if err := applyMigrations(rootCtx, pool, cfg.MigrationsDir); err != nil {
			return err
		}
	}

	runtime, err := newRuntime(cfg)
	if err != nil {
		return err
	}
	store := postgres.NewRepository(pool, cfg.PostgresStatementTimeout)
	service := app.NewAutomationService(store, runtime, app.SystemClock{}, app.RandomIDGenerator{})

	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.ListenAddr, err)
	}
	defer listener.Close()

	server := grpc.NewServer()
	browserautomationv1.RegisterBrowserAutomationServiceServer(server, grpcadapter.NewAutomationServer(service))

	healthServer := health.NewServer()
	healthv1.RegisterHealthServer(server, healthServer)
	healthServer.SetServingStatus("", healthv1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus(browserautomationv1.BrowserAutomationService_ServiceDesc.ServiceName, healthv1.HealthCheckResponse_SERVING)

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("browser automation service listening", "addr", cfg.ListenAddr, "runtime", cfg.Runtime)
		serveErr <- server.Serve(listener)
	}()

	select {
	case <-rootCtx.Done():
		healthServer.SetServingStatus("", healthv1.HealthCheckResponse_NOT_SERVING)
		healthServer.SetServingStatus(browserautomationv1.BrowserAutomationService_ServiceDesc.ServiceName, healthv1.HealthCheckResponse_NOT_SERVING)
		stopped := make(chan struct{})
		go func() {
			server.GracefulStop()
			close(stopped)
		}()
		select {
		case <-stopped:
			return nil
		case <-time.After(cfg.ShutdownGrace):
			server.Stop()
			return nil
		}
	case err := <-serveErr:
		if errors.Is(err, grpc.ErrServerStopped) {
			return nil
		}
		return err
	}
}

func loadConfig() (config, error) {
	cfg := config{
		ListenAddr:               envDefault("BROWSER_AUTOMATION_LISTEN_ADDR", defaultListenAddr),
		PostgresDSN:              requiredEnv("BROWSER_AUTOMATION_POSTGRES_DSN"),
		PostgresMaxConns:         int32(envInt("BROWSER_AUTOMATION_POSTGRES_MAX_CONNS", defaultPostgresMaxConns)),
		PostgresConnectTimeout:   envDurationSeconds("BROWSER_AUTOMATION_POSTGRES_CONNECT_TIMEOUT_SECONDS", defaultConnectTimeout),
		PostgresStatementTimeout: envDurationSeconds("BROWSER_AUTOMATION_POSTGRES_STATEMENT_TIMEOUT_SECONDS", defaultStatementTimeout),
		ApplyMigrations:          envBool("BROWSER_AUTOMATION_APPLY_MIGRATIONS", false),
		MigrationsDir:            envDefault("BROWSER_AUTOMATION_MIGRATIONS_DIR", defaultMigrationsDir),
		ShutdownGrace:            envDurationSeconds("BROWSER_AUTOMATION_SHUTDOWN_GRACE_SECONDS", defaultShutdownGrace),
		Runtime:                  strings.ToLower(envDefault("BROWSER_AUTOMATION_RUNTIME", defaultRuntime)),

		CamoufoxPythonPath:      envDefault("BROWSER_AUTOMATION_CAMOUFOX_PYTHON_PATH", "python3"),
		CamoufoxArtifactsDir:    envDefault("BROWSER_AUTOMATION_ARTIFACTS_DIR", defaultArtifactsDir),
		CamoufoxStartupTimeout:  envDurationSeconds("BROWSER_AUTOMATION_CAMOUFOX_STARTUP_TIMEOUT_SECONDS", defaultCamoufoxStartup),
		CamoufoxShutdownTimeout: envDurationSeconds("BROWSER_AUTOMATION_CAMOUFOX_SHUTDOWN_TIMEOUT_SECONDS", defaultCamoufoxShutdown),
		CamoufoxTaskTimeout:     envDurationSeconds("BROWSER_AUTOMATION_CAMOUFOX_TASK_TIMEOUT_SECONDS", defaultCamoufoxTaskTimeout),
		CamoufoxHeadless:        envBool("BROWSER_AUTOMATION_CAMOUFOX_HEADLESS", true),
		CamoufoxServerPort:      envInt("BROWSER_AUTOMATION_CAMOUFOX_SERVER_PORT", 0),
		CamoufoxWSPathPrefix:    envDefault("BROWSER_AUTOMATION_CAMOUFOX_WS_PATH_PREFIX", defaultCamoufoxWSPathPrefix),
		CamoufoxExtraEnv:        envList("BROWSER_AUTOMATION_CAMOUFOX_EXTRA_ENV"),
	}
	if strings.TrimSpace(cfg.PostgresDSN) == "" {
		return cfg, fmt.Errorf("BROWSER_AUTOMATION_POSTGRES_DSN is required")
	}
	if cfg.PostgresMaxConns < 1 {
		return cfg, fmt.Errorf("BROWSER_AUTOMATION_POSTGRES_MAX_CONNS must be positive")
	}
	if cfg.Runtime != defaultRuntime {
		return cfg, fmt.Errorf("unsupported BROWSER_AUTOMATION_RUNTIME %q", cfg.Runtime)
	}
	return cfg, nil
}

func newRuntime(cfg config) (*camoufox.Runtime, error) {
	runtime, err := camoufox.NewRuntime(camoufox.Config{
		PythonPath:      cfg.CamoufoxPythonPath,
		ArtifactsDir:    cfg.CamoufoxArtifactsDir,
		StartupTimeout:  cfg.CamoufoxStartupTimeout,
		ShutdownTimeout: cfg.CamoufoxShutdownTimeout,
		TaskTimeout:     cfg.CamoufoxTaskTimeout,
		Headless:        cfg.CamoufoxHeadless,
		ServerPort:      cfg.CamoufoxServerPort,
		WSPathPrefix:    cfg.CamoufoxWSPathPrefix,
		ExtraEnv:        cfg.CamoufoxExtraEnv,
	})
	if err != nil {
		return nil, fmt.Errorf("configure camoufox runtime: %w", err)
	}
	return runtime, nil
}

func applyMigrations(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return fmt.Errorf("list browser automation migrations: %w", err)
	}
	sort.Strings(files)
	if len(files) == 0 {
		return fmt.Errorf("browser automation migrations not found in %s", dir)
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire postgres connection for migrations: %w", err)
	}
	defer conn.Release()

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", file, err)
		}
		result := conn.Conn().PgConn().Exec(ctx, string(data))
		if _, err := result.ReadAll(); err != nil {
			return fmt.Errorf("apply migration %s: %w", file, err)
		}
		slog.Info("applied browser automation migration", "file", filepath.Base(file))
	}
	return nil
}

func requiredEnv(name string) string {
	return strings.TrimSpace(os.Getenv(name))
}

func envDefault(name string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func envBool(name string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if value == "" {
		return fallback
	}
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		slog.Warn("invalid integer env; using fallback", "name", name, "value", value, "fallback", fallback)
		return fallback
	}
	return parsed
}

func envDurationSeconds(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	seconds, err := strconv.Atoi(value)
	if err != nil {
		slog.Warn("invalid duration env; using fallback", "name", name, "value", value, "fallback", fallback.String())
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

func envList(name string) []string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return nil
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '\n' || r == ','
	})
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		if item := strings.TrimSpace(part); item != "" {
			items = append(items, item)
		}
	}
	return items
}
