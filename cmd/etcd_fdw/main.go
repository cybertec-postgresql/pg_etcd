// Package main implements the etcd_fdw binary for bidirectional synchronization
// between etcd and PostgreSQL.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jessevdk/go-flags"
	"github.com/sirupsen/logrus"

	"github.com/cybertec-postgresql/etcd_fdw/internal/log"
	"github.com/cybertec-postgresql/etcd_fdw/internal/sync"
)

// Config holds the application configuration
type Config struct {
	PostgresDSN     string `short:"p" env:"ETCD_FDW_POSTGRES_DSN" long:"postgres-dsn" description:"PostgreSQL connection string"`
	EtcdDSN         string `short:"e" env:"ETCD_FDW_ETCD_DSN" long:"etcd-dsn" description:"etcd connection string"`
	LogLevel        string `short:"l" env:"ETCD_FDW_LOG_LEVEL" long:"log-level" description:"Log level: debug|info|warn|error" default:"info"`
	PollingInterval string `long:"polling-interval" description:"Polling interval for PostgreSQL to etcd sync" default:"1s"`
	Version         bool   `short:"v" long:"version" description:"Show version information"`
	Help            bool
}

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// ParseCLI parses command-line arguments and returns the configuration
func ParseCLI(args []string) (cmdOpts *Config, err error) {
	cmdOpts = new(Config)
	parser := flags.NewParser(cmdOpts, flags.HelpFlag)
	parser.SubcommandsOptional = true            // if not command specified, start monitoring
	nonParsedArgs, err := parser.ParseArgs(args) // parse and execute subcommand if any
	if err != nil {
		if flagsErr, ok := err.(*flags.Error); ok && flagsErr.Type == flags.ErrHelp {
			cmdOpts.Help = true
		}
		if !flags.WroteHelp(err) {
			parser.WriteHelp(os.Stdout)
		}
		return cmdOpts, err
	}
	if len(nonParsedArgs) > 0 { // we don't expect any non-parsed arguments
		return cmdOpts, fmt.Errorf("unknown argument(s): %v", nonParsedArgs)
	}
	return
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

// SetupLogging configures the logging system with structured output
func SetupLogging(logLevel string) error {
	// Parse log level
	level, err := logrus.ParseLevel(logLevel)
	if err != nil {
		return fmt.Errorf("invalid log level: %w", err)
	}
	logrus.SetLevel(level)

	// Configure formatter with consistent structure
	logrus.SetFormatter(log.NewFormatter(false))

	// Add common fields to all log entries
	logrus.SetReportCaller(false) // Keep simple, don't include caller info

	// Add process info to context
	logrus.WithFields(logrus.Fields{
		"version": version,
		"commit":  commit,
		"pid":     os.Getpid(),
	}).Info("etcd_fdw logging initialized")

	return nil
}

// SetupCloseHandler creates a 'listener' on a new goroutine which will notify the
// program if it receives an interrupt from the OS. We then handle this by calling
// our clean up procedure and exiting the program.
func SetupCloseHandler(cancel context.CancelFunc) {
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		logrus.Debug("SetupCloseHandler received an interrupt from OS. Closing session...")
		cancel()
	}()
}

func main() {
	// Quick check for version flags before full parsing
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-v" {
			ShowVersion()
			os.Exit(0)
		}
	}

	// Parse CLI arguments
	config, err := ParseCLI(os.Args)
	if err != nil {
		if flagsErr, ok := err.(*flags.Error); ok && flagsErr.Type == flags.ErrHelp {
			os.Exit(0)
		}
		fmt.Printf("Error: %s\n", err)
		os.Exit(1)
	}

	// Setup logging
	if err := SetupLogging(config.LogLevel); err != nil {
		logrus.WithError(err).Fatal("Failed to setup logging")
	}

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	SetupCloseHandler(cancel)

	// Connect to PostgreSQL with retry logic
	pgPool, err := sync.NewWithRetry(ctx, config.PostgresDSN)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to connect to PostgreSQL after retries")
	}
	defer pgPool.Close()

	// Connect to etcd with retry logic
	etcdClient, err := sync.NewEtcdClientWithRetry(ctx, config.EtcdDSN)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to connect to etcd after retries")
	}
	defer etcdClient.Close()

	// Parse polling interval
	pollingInterval, err := time.ParseDuration(config.PollingInterval)
	if err != nil {
		logrus.WithError(err).Fatal("Invalid polling interval format")
	}

	// Create and start sync service
	syncService := sync.NewService(pgPool, etcdClient, pollingInterval)
	if err := syncService.Start(ctx); err != nil && ctx.Err() == nil {
		logrus.WithError(err).Fatal("Synchronization failed")
	}

	logrus.Info("Graceful shutdown completed")
}
