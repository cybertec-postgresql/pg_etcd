// Package main implements the etcd_fdw binary for bidirectional synchronization
// between etcd and PostgreSQL.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jessevdk/go-flags"
	"github.com/sirupsen/logrus"

	"github.com/cybertec-postgresql/etcd_fdw/internal/db"
	"github.com/cybertec-postgresql/etcd_fdw/internal/etcd"
	"github.com/cybertec-postgresql/etcd_fdw/internal/sync"
)

// Config holds the application configuration
type Config struct {
	PostgresDSN string `short:"p" long:"postgres-dsn" description:"PostgreSQL connection string"`
	EtcdDSN     string `short:"e" long:"etcd-dsn" description:"etcd connection string"`
	LogLevel    string `short:"l" long:"log-level" description:"Log level: debug|info|warn|error" default:"info"`
	DryRun      bool   `long:"dry-run" description:"Show what would be done without executing"`
	Version     bool   `short:"v" long:"version" description:"Show version information"`
	Help        bool   `short:"h" long:"help" description:"Show help message"`
}

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// ParseCLI parses command-line arguments and returns the configuration
func ParseCLI(args []string) (*Config, error) {
	var config Config
	// Set default log level
	config.LogLevel = "info"

	parser := flags.NewParser(&config, flags.Default)
	parser.Name = "etcd_fdw"
	parser.Usage = "[options]"

	// Check for environment variables
	if envPostgresDSN := os.Getenv("ETCD_FDW_POSTGRES_DSN"); envPostgresDSN != "" {
		config.PostgresDSN = envPostgresDSN
	}
	if envEtcdDSN := os.Getenv("ETCD_FDW_ETCD_DSN"); envEtcdDSN != "" {
		config.EtcdDSN = envEtcdDSN
	}
	if envLogLevel := os.Getenv("ETCD_FDW_LOG_LEVEL"); envLogLevel != "" {
		config.LogLevel = envLogLevel
	}
	if envDryRun := os.Getenv("ETCD_FDW_DRY_RUN"); envDryRun == "true" {
		config.DryRun = true
	}

	// Parse the provided arguments instead of os.Args
	if args != nil {
		// Check for help/version flags first to avoid required flag errors
		for _, arg := range args {
			if arg == "--help" || arg == "-h" {
				config.Help = true
				return &config, nil
			}
			if arg == "--version" || arg == "-v" {
				config.Version = true
				return &config, nil
			}
		}

		_, err := parser.ParseArgs(args)
		if err != nil {
			return nil, err
		}
	} else {
		_, err := parser.Parse()
		if err != nil {
			return nil, err
		}
	}

	return &config, nil
}

// ShowVersion prints version information and exits
func ShowVersion() {
	fmt.Printf("etcd_fdw version %s\n", version)
	if commit != "none" && commit != "" {
		fmt.Printf("commit: %s\n", commit)
	}
	if date != "unknown" && date != "" {
		fmt.Printf("built: %s\n", date)
	}
}

// SetupLogging configures the logging system based on the log level
func SetupLogging(logLevel string) error {
	logrus.SetFormatter(&logrus.JSONFormatter{})
	level, err := logrus.ParseLevel(logLevel)
	if err != nil {
		return fmt.Errorf("invalid log level: %w", err)
	}
	logrus.SetLevel(level)
	return nil
}

func main() {
	// Quick check for version/help flags before full parsing
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-v" {
			ShowVersion()
			os.Exit(0)
		}
	}

	// Parse CLI arguments
	config, err := ParseCLI(nil) // nil means use os.Args
	if err != nil {
		if flagsErr, ok := err.(*flags.Error); ok && flagsErr.Type == flags.ErrHelp {
			os.Exit(0)
		}
		logrus.WithError(err).Fatal("Failed to parse command line arguments")
	}

	// Setup logging
	if err := SetupLogging(config.LogLevel); err != nil {
		logrus.WithError(err).Fatal("Failed to setup logging")
	}

	ctx := context.Background()

	// Connect to PostgreSQL
	var pgPool db.PgxPoolIface
	if pgPool, err = db.New(ctx, config.PostgresDSN); err != nil {
		logrus.WithError(err).Fatal("Failed to connect to PostgreSQL")
	}
	defer pgPool.Close()

	// Connect to etcd
	var etcdClient *etcd.EtcdClient
	if config.EtcdDSN != "" {
		var err error
		etcdClient, err = etcd.NewEtcdClient(config.EtcdDSN)
		if err != nil {
			logrus.WithError(err).Fatal("Failed to connect to etcd")
		}
		defer etcdClient.Close()
	}

	// Get prefix from etcd DSN
	prefix := etcd.GetPrefix(config.EtcdDSN)

	// Create and start sync service
	syncService := sync.NewService(pgPool, etcdClient, prefix, config.DryRun)
	if err := syncService.Start(ctx); err != nil {
		logrus.WithError(err).Fatal("Synchronization failed")
	}
}
