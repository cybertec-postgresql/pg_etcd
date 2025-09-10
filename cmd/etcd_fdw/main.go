// Package main implements the etcd_fdw binary for bidirectional synchronization
// between etcd and PostgreSQL.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jessevdk/go-flags"
	"github.com/sirupsen/logrus"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// Config holds the application configuration
type Config struct {
	PostgresDSN string `short:"p" long:"postgres-dsn" description:"PostgreSQL connection string" required:"true"`
	EtcdDSN     string `short:"e" long:"etcd-dsn" description:"etcd connection string" required:"true"`
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

func main() {
	// Quick check for version/help flags before full parsing
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-v" {
			fmt.Printf("etcd_fdw version %s\n", version)
			if commit != "none" && commit != "" {
				fmt.Printf("commit: %s\n", commit)
			}
			if date != "unknown" && date != "" {
				fmt.Printf("built: %s\n", date)
			}
			os.Exit(0)
		}
	}

	var config Config
	parser := flags.NewParser(&config, flags.Default)
	parser.Name = "etcd_fdw"
	parser.Usage = "[options]"

	_, err := parser.Parse()
	if err != nil {
		if flagsErr, ok := err.(*flags.Error); ok && flagsErr.Type == flags.ErrHelp {
			os.Exit(0)
		}
		os.Exit(1)
	}

	// Setup logging
	logrus.SetFormatter(&logrus.JSONFormatter{})
	level, err := logrus.ParseLevel(config.LogLevel)
	if err != nil {
		logrus.WithError(err).Fatal("Invalid log level")
	}
	logrus.SetLevel(level)

	ctx := context.Background()

	// TODO: Connect to PostgreSQL
	if config.PostgresDSN != "" {
		_, err := pgxpool.New(ctx, config.PostgresDSN)
		if err != nil {
			logrus.WithError(err).Fatal("Failed to connect to PostgreSQL")
		}
	}

	// TODO: Connect to etcd
	if config.EtcdDSN != "" {
		_, err := clientv3.New(clientv3.Config{
			Endpoints: []string{"localhost:2379"}, // TODO: parse from DSN
		})
		if err != nil {
			logrus.WithError(err).Fatal("Failed to connect to etcd")
		}
	}

	if config.DryRun {
		logrus.Info("Dry run mode - would start bidirectional sync")
		return
	}

	logrus.Info("Starting etcd_fdw bidirectional synchronization")
	// TODO: Implement sync logic
}
