# Tasks: Bidirectional Syn- [x] T002 Initialize go.mod with Go 1.25+ and dependencies: jackc/pgx/v5, etcd-io/etcd/client/v3, jessevdk/go-flags, logrushronization Between etcd and PostgreSQL

**Input**: Design documents from `/specs/001-describe-building-a/`
**Prerequisites**: plan.md, research.md, data-model.md, contracts/cli.md, quickstart.md

## Execution Flow (main)

1. Load plan.md → Tech stack: Go 1.25+, jackc/pgx/v5, etcd-io/etcd/client/v3, jessevdk/go-flags
2. Load data-model.md → Entities: KeyValueRecord, WriteAheadLogEntry, SynchronizationEvent  
3. Load contracts/cli.md → CLI: etcd_fdw command with DSN parameters
4. Load quickstart.md → Test scenarios: bidirectional sync, conflict resolution
5. Generate tasks: Setup → Tests → Core → Integration → Polish
6. Apply TDD: Tests before implementation, parallel where possible

## Format: `[ID] [P?] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- Include exact file paths in descriptions

## Path Conventions

- Single Go project at repository root
- `cmd/etcd_fdw/` for main binary
- `internal/` for core logic
- `tests/` for integration tests

## Phase 3.1: Setup

- [x] T001 Create Go project structure: cmd/etcd_fdw/, internal/{sync,db,etcd}/, tests/
- [x] T002 Initialize go.mod with Go 1.25+ and dependencies: jackc/pgx/v5, etcd-io/etcd/client/v3, jessevdk/go-flags, logrus
- [x] T003 [P] Configure linting: golangci-lint.yml with deadcode, golint, govet
- [x] T004 [P] Configure GitHub Actions: go test, go build, linting workflows

## Phase 3.2: Database Schema & Infrastructure

- [x] T005 Create PostgreSQL schema file: migrations/001_create_tables.sql with etcd and etcd_wal tables
- [x] T006 Create PostgreSQL functions: migrations/002_create_functions.sql for get/set/delete with revision enforcement
- [x] T007 Create trigger and NOTIFY: migrations/003_create_triggers.sql for etcd_wal table notifications
- [x] T008 [P] Create testcontainers setup: tests/infrastructure_test.go for PostgreSQL and etcd containers

## Phase 3.3: Tests First (TDD) ⚠️ MUST COMPLETE BEFORE 3.4

**CRITICAL**: These tests MUST be written and MUST FAIL before ANY implementation

- [x] T009 [P] CLI parsing test: tests/cli_test.go verify DSN parsing and flag validation
- [x] T010 [P] PostgreSQL connection test: tests/postgres_test.go verify connection, schema setup
- [x] T011 [P] etcd connection test: tests/etcd_test.go verify watch setup and key operations
- [x] T012 [P] Bidirectional sync test: tests/sync_test.go etcd→PostgreSQL sync scenario
- [x] T013 [P] Reverse sync test: tests/reverse_sync_test.go PostgreSQL→etcd sync scenario  
- [x] T014 [P] Conflict resolution test: tests/conflict_test.go verify "etcd wins" logic
- [x] T015 [P] Compaction handling test: tests/compaction_test.go verify resync on etcd watch errors

## Phase 3.4: Core Implementation (ONLY after tests are failing)

- [x] T016 CLI configuration: cmd/etcd_fdw/main.go with go-flags DSN parsing and help
- [x] T017 [P] PostgreSQL client: internal/db/postgres.go with pgx connection and LISTEN setup
- [x] T018 [P] etcd client: internal/etcd/client.go with watch setup and revision handling
- [x] T019 [P] Sync: internal/sync/sync.go use etcd_client.Watch() and pgx.CopyFrom() for sync
- [x] T020 etcd→PostgreSQL sync: with COPY operations
- [x] T021 PostgreSQL→etcd sync: with NOTIFY handling
- [x] T022 Conflict resolution: Simplified "etcd wins" strategy (ConflictResolver removed as overkill)
- [x] T023 Main sync loop: cmd/etcd_fdw/sync.go coordinate watchers and graceful shutdown

## Phase 3.5: Error Handling & Resilience

- [ ] T024 Connection retry logic: internal/db/retry.go exponential backoff for PostgreSQL
- [ ] T025 etcd reconnection: internal/etcd/retry.go handle watch failures and compaction
- [ ] T026 Sync error handling: internal/sync/errors.go classify and retry transient failures
- [ ] T027 Logging integration: cmd/etcd_fdw/logging.go structured logging with logrus
- [ ] T028 Graceful shutdown: cmd/etcd_fdw/signals.go handle SIGINT/SIGTERM

## Phase 3.6: Integration & Validation

- [ ] T029 Integration test runner: tests/integration_test.go orchestrate full sync scenarios
- [ ] T030 Performance validation: tests/performance_test.go verify sync latency under load
- [ ] T031 [P] End-to-end quickstart: tests/e2e_test.go automate quickstart.md scenarios
- [ ] T032 [P] Error injection tests: tests/fault_injection_test.go network failures, restarts

## Phase 3.7: Polish

- [ ] T033 [P] Unit tests for models: tests/unit/models_test.go validation rules
- [ ] T034 [P] Unit tests for resolver: tests/unit/resolver_test.go conflict scenarios
- [ ] T035 [P] CLI help and version: cmd/etcd_fdw/version.go --help and --version output
- [ ] T036 [P] README documentation: README.md installation and usage examples
- [ ] T037 Code cleanup: remove TODO comments, unused imports, dead code
- [ ] T038 Final integration test: run quickstart.md end-to-end manually

## Dependencies

- Setup (T001-T004) before everything
- Database setup (T005-T008) before tests and implementation  
- Tests (T009-T015) before implementation (T016-T023)
- Core models (T019) before sync implementations (T020-T022)
- Sync implementations before error handling (T024-T028)
- Error handling before integration tests (T029-T032)
- Everything before polish (T033-T038)
