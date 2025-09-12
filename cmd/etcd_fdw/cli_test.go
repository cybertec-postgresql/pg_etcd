// Package main provides CLI testing for etcd_fdw command-line interface.
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCLIParsing tests DSN parsing and flag validation for etcd_fdw CLI
// This test MUST FAIL until CLI implementation is complete (TDD approach)
func TestCLIParsing(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantErr  bool
		errMsg   string
		expected Config
	}{
		{
			name: "valid DSN and etcd DSN",
			args: []string{
				"--postgres-dsn", "postgres://user:pass@localhost:5432/db",
				"--etcd-dsn", "etcd://localhost:2379/",
			},
			wantErr: false,
			expected: Config{
				PostgresDSN:     "postgres://user:pass@localhost:5432/db",
				EtcdDSN:         "etcd://localhost:2379/",
				LogLevel:        "info", // default value
				PollingInterval: "1s",   // default value
			},
		},
		{
			name: "multiple etcd endpoints in DSN",
			args: []string{
				"--postgres-dsn", "postgres://user:pass@localhost:5432/db",
				"--etcd-dsn", "etcd://localhost:2379,localhost:2380,localhost:2381/",
			},
			wantErr: false,
			expected: Config{
				PostgresDSN:     "postgres://user:pass@localhost:5432/db",
				EtcdDSN:         "etcd://localhost:2379,localhost:2380,localhost:2381/",
				LogLevel:        "info", // default value
				PollingInterval: "1s",   // default value
			},
		},
		{
			name:    "version flag",
			args:    []string{"--version"},
			wantErr: false,
			expected: Config{
				Version:         true,
				LogLevel:        "info", // default value
				PollingInterval: "1s",   // default value
			},
		},
		{
			name: "with log level and dry run",
			args: []string{
				"--postgres-dsn", "postgres://user:pass@localhost:5432/db",
				"--etcd-dsn", "etcd://localhost:2379/",
				"--log-level", "debug",
				"--dry-run",
			},
			wantErr: true, // dry-run not implemented
			expected: Config{
				PostgresDSN:     "postgres://user:pass@localhost:5432/db",
				EtcdDSN:         "etcd://localhost:2379/",
				LogLevel:        "debug",
				PollingInterval: "1s", // default value
			},
		},
		{
			name: "etcd DSN with prefix and TLS params",
			args: []string{
				"--postgres-dsn", "postgres://user:pass@localhost:5432/db",
				"--etcd-dsn", "etcd://localhost:2379/config/?tls=enabled&dial_timeout=5s",
			},
			wantErr: false,
			expected: Config{
				PostgresDSN:     "postgres://user:pass@localhost:5432/db",
				EtcdDSN:         "etcd://localhost:2379/config/?tls=enabled&dial_timeout=5s",
				LogLevel:        "info", // default value
				PollingInterval: "1s",   // default value
			},
		},
		{
			name: "short flag aliases",
			args: []string{
				"-p", "postgres://user:pass@localhost:5432/db",
				"-e", "etcd://localhost:2379/",
				"-l", "warn",
			},
			wantErr: false,
			expected: Config{
				PostgresDSN:     "postgres://user:pass@localhost:5432/db",
				EtcdDSN:         "etcd://localhost:2379/",
				LogLevel:        "warn",
				PollingInterval: "1s", // default value
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := ParseCLI(tt.args)

			if tt.wantErr {
				require.Error(t, err, "Expected error for test case: %s", tt.name)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg, "Error message should contain expected text")
				}
			} else {
				require.NoError(t, err, "Expected no error for test case: %s", tt.name)
				require.NotNil(t, config, "Config should not be nil")
				assert.Equal(t, tt.expected, *config, "Parsed config should match expected")
			}
		})
	}
}

// TestCLIEnvironmentVariables tests that CLI can read from environment variables
func TestCLIEnvironmentVariables(t *testing.T) {
	// Set environment variables
	t.Setenv("ETCD_FDW_POSTGRES_DSN", "postgres://env:pass@localhost:5432/envdb")
	t.Setenv("ETCD_FDW_ETCD_DSN", "etcd://localhost:2379,localhost:2380/")

	// This will fail because ParseCLI function doesn't exist yet
	config, err := ParseCLI([]string{})

	require.NoError(t, err, "Should parse from environment variables")
	require.NotNil(t, config, "Config should not be nil")
	assert.Equal(t, "postgres://env:pass@localhost:5432/envdb", config.PostgresDSN)
	assert.Equal(t, "etcd://localhost:2379,localhost:2380/", config.EtcdDSN)
}

// TestCLIFlagPrecedence tests that command-line flags override environment variables
func TestCLIFlagPrecedence(t *testing.T) {
	// Set environment variables
	t.Setenv("ETCD_FDW_POSTGRES_DSN", "postgres://env:pass@localhost:5432/envdb")
	t.Setenv("ETCD_FDW_ETCD_DSN", "etcd://localhost:2379/")

	// Command-line flags should override environment
	args := []string{
		"--postgres-dsn", "postgres://flag:pass@localhost:5432/flagdb",
		"--etcd-dsn", "etcd://localhost:2380/",
	}

	// This will fail because ParseCLI function doesn't exist yet
	config, err := ParseCLI(args)

	require.NoError(t, err, "Should parse with flag precedence")
	require.NotNil(t, config, "Config should not be nil")
	assert.Equal(t, "postgres://flag:pass@localhost:5432/flagdb", config.PostgresDSN)
	assert.Equal(t, "etcd://localhost:2380/", config.EtcdDSN)
}
