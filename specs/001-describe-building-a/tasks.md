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
- [ ] T006 Create PostgreSQL functions: migrations/002_create_functions.sql for get/set/delete with revision enforcement
- [ ] T007 Create trigger and NOTIFY: migrations/003_create_triggers.sql for etcd_wal table notifications
- [ ] T008 [P] Create testcontainers setup: tests/infrastructure_test.go for PostgreSQL and etcd containers

## Phase 3.3: Tests First (TDD) ⚠️ MUST COMPLETE BEFORE 3.4
**CRITICAL: These tests MUST be written and MUST FAIL before ANY implementation**
- [ ] T009 [P] CLI parsing test: tests/cli_test.go verify DSN parsing and flag validation
- [ ] T010 [P] PostgreSQL connection test: tests/postgres_test.go verify connection, schema setup
- [ ] T011 [P] etcd connection test: tests/etcd_test.go verify watch setup and key operations
- [ ] T012 [P] Bidirectional sync test: tests/sync_test.go etcd→PostgreSQL sync scenario
- [ ] T013 [P] Reverse sync test: tests/reverse_sync_test.go PostgreSQL→etcd sync scenario  
- [ ] T014 [P] Conflict resolution test: tests/conflict_test.go verify "etcd wins" logic
- [ ] T015 [P] Compaction handling test: tests/compaction_test.go verify resync on etcd watch errors

## Phase 3.4: Core Implementation (ONLY after tests are failing)
- [ ] T016 CLI configuration: cmd/etcd_fdw/main.go with go-flags DSN parsing and help
- [ ] T017 [P] PostgreSQL client: internal/db/postgres.go with pgx connection and LISTEN setup
- [ ] T018 [P] etcd client: internal/etcd/client.go with watch setup and revision handling
- [ ] T019 [P] Sync models: internal/sync/models.go for KeyValueRecord and revision tracking
- [ ] T020 etcd→PostgreSQL sync: internal/sync/etcd_to_postgres.go with COPY operations
- [ ] T021 PostgreSQL→etcd sync: internal/sync/postgres_to_etcd.go with NOTIFY handling
- [ ] T022 Conflict resolution: internal/sync/resolver.go implement "etcd wins" strategy
- [ ] T023 Main sync loop: cmd/etcd_fdw/sync.go coordinate watchers and graceful shutdown

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

## Parallel Execution Examples
```bash
# Phase 3.1 Setup (parallel)
Task: "Configure linting: golangci-lint.yml" 
Task: "Configure GitHub Actions: workflows"

# Phase 3.3 Tests (parallel - different files)
Task: "CLI parsing test: tests/cli_test.go"
Task: "PostgreSQL connection test: tests/postgres_test.go" 
Task: "etcd connection test: tests/etcd_test.go"
Task: "Bidirectional sync test: tests/sync_test.go"
Task: "Reverse sync test: tests/reverse_sync_test.go"

# Phase 3.4 Core Models (parallel - different packages)
Task: "PostgreSQL client: internal/db/postgres.go"
Task: "etcd client: internal/etcd/client.go"
Task: "Sync models: internal/sync/models.go"
```

## Task Generation Rules Applied
- Different files/packages marked [P] for parallel execution
- TDD order enforced: Tests (T009-T015) before implementation (T016-T023)
- Database schema before both tests and implementation
- Models before services, services before main coordination
- Error handling after core functionality
- Integration tests after error handling
- Polish tasks at the end

## Notes
- All tests must fail initially (no implementation exists)
- Commit after each completed task
- Use testcontainers for real PostgreSQL and etcd in tests
- Focus on simplicity: direct client usage, no abstractions
- Conflict resolution always favors etcd ("etcd wins")
- Handle etcd watch compaction with full resync

### Integration Tests
- [ ] T014 [P] Integration test for etcd to PostgreSQL sync in tests/integration/test_etcd_to_pg_sync.go
- [ ] T015 [P] Integration test for PostgreSQL to etcd sync in tests/integration/test_pg_to_etcd_sync.go
- [ ] T016 [P] Integration test for conflict resolution scenarios in tests/integration/test_conflict_resolution.go
- [ ] T017 [P] Integration test for service recovery after restart in tests/integration/test_service_recovery.go
- [ ] T018 [P] Integration test for etcd compaction handling in tests/integration/test_compaction_handling.go

## Phase 3.3: Core Implementation (ONLY after tests are failing)

### Data Models
- [ ] T019 [P] KeyValueRecord model with validation in src/models/keyvalue.go
- [ ] T020 [P] WriteAheadLogEntry model with state transitions in src/models/wal.go
- [ ] T021 [P] SynchronizationEvent model for audit logging in src/models/sync_event.go
- [ ] T022 [P] Configuration structs with validation in src/models/config.go

### Database Layer  
- [ ] T023 [P] PostgreSQL connection manager in src/lib/postgres/connection.go
- [ ] T024 PostgreSQL key-value operations (Get, Set, Delete, List) in src/lib/postgres/keyvalue.go
- [ ] T025 PostgreSQL WAL operations (GetPending, MarkResult) in src/lib/postgres/wal.go
- [ ] T026 PostgreSQL LISTEN/NOTIFY implementation in src/lib/postgres/notify.go
- [ ] T027 Database schema migration setup in src/lib/postgres/migrations.go

### etcd Layer
- [ ] T028 [P] etcd connection manager in src/lib/etcd/connection.go
- [ ] T029 etcd key-value operations (Get, Put, Delete, List) in src/lib/etcd/keyvalue.go
- [ ] T030 etcd watch implementation for change detection in src/lib/etcd/watch.go
- [ ] T031 etcd revision handling and compaction detection in src/lib/etcd/revision.go

### Synchronization Service
- [ ] T032 Conflict resolution logic with revision comparison in src/lib/sync/conflict.go
- [ ] T033 Bidirectional sync orchestrator in src/lib/sync/service.go
- [ ] T034 Retry mechanism with exponential backoff in src/lib/sync/retry.go
- [ ] T035 Health monitoring and metrics collection in src/lib/sync/health.go

### CLI Implementation
- [ ] T036 Main CLI entry point with global flags in cmd/etcd-sync/main.go
- [ ] T037 Start command implementation in cmd/etcd-sync/start.go
- [ ] T038 Status command with JSON output in cmd/etcd-sync/status.go
- [ ] T039 Sync command for manual synchronization in cmd/etcd-sync/sync.go
- [ ] T040 Validate command for consistency checking in cmd/etcd-sync/validate.go
- [ ] T041 Config command for configuration management in cmd/etcd-sync/config.go

## Phase 3.4: Integration

- [ ] T042 Wire PostgreSQL services with dependency injection in src/services/postgres.go
- [ ] T043 Wire etcd services with connection pooling in src/services/etcd.go
- [ ] T044 Integrate structured logging throughout all components
- [ ] T045 Add graceful shutdown handling for all services
- [ ] T046 Implement configuration loading from YAML and environment variables
- [ ] T047 Set up signal handling for daemon mode operation

## Phase 3.5: Polish

### Unit Tests
- [ ] T048 [P] Unit tests for conflict resolution logic in tests/unit/test_conflict_test.go
- [ ] T049 [P] Unit tests for retry mechanism in tests/unit/test_retry_test.go
- [ ] T050 [P] Unit tests for configuration validation in tests/unit/test_config_test.go
- [ ] T051 [P] Unit tests for model validation in tests/unit/test_models_test.go

### Performance & Quality
- [ ] T052 Performance tests for sync throughput (>1000 ops/sec) in tests/performance/test_throughput.go
- [ ] T053 Performance tests for sync latency (<1s) in tests/performance/test_latency.go
- [ ] T054 [P] Create comprehensive README.md with installation and usage
- [ ] T055 [P] Generate API documentation for library interfaces
- [ ] T056 Code cleanup: remove duplication and optimize imports
- [ ] T057 Execute quickstart.md scenarios for end-to-end validation

## Dependencies

### Critical Path
```
Setup (T001-T005) 
→ Contract Tests (T006-T013) 
→ Integration Tests (T014-T018)
→ Models (T019-T022)
→ Database Layer (T023-T027)
→ etcd Layer (T028-T031)
→ Sync Service (T032-T035)
→ CLI (T036-T041)
→ Integration (T042-T047)
→ Polish (T048-T057)
```

### Blocking Dependencies
- All tests (T006-T018) MUST be written and failing before ANY implementation
- T023 (PostgreSQL connection) blocks T024-T027
- T028 (etcd connection) blocks T029-T031
- T019-T022 (models) block all service implementations
- T032-T035 (sync service) blocks T036-T041 (CLI)
- Implementation (T019-T041) blocks integration (T042-T047)

## Parallel Execution Examples

### Phase 3.2: All Contract Tests (Run Together)
```bash
# Terminal 1
Task: "Contract test for CLI start command in tests/contract/test_cli_start.go"

# Terminal 2  
Task: "Contract test for CLI status command in tests/contract/test_cli_status.go"

# Terminal 3
Task: "Contract test for PostgreSQL KeyValueStore interface in tests/contract/test_postgresql_interface.go"

# Terminal 4
Task: "Contract test for etcd EtcdStore interface in tests/contract/test_etcd_interface.go"
```

### Phase 3.3: Models (Run Together)
```bash
# Terminal 1
Task: "KeyValueRecord model with validation in src/models/keyvalue.go"

# Terminal 2
Task: "WriteAheadLogEntry model with state transitions in src/models/wal.go"

# Terminal 3
Task: "SynchronizationEvent model for audit logging in src/models/sync_event.go"

# Terminal 4
Task: "Configuration structs with validation in src/models/config.go"
```

### Phase 3.5: Unit Tests (Run Together)
```bash
# Terminal 1
Task: "Unit tests for conflict resolution logic in tests/unit/test_conflict_test.go"

# Terminal 2
Task: "Unit tests for retry mechanism in tests/unit/test_retry_test.go"

# Terminal 3
Task: "Unit tests for configuration validation in tests/unit/test_config_test.go"
```

## Notes

- **[P] tasks**: Different files, no shared dependencies - safe for parallel execution
- **Sequential tasks**: Same file or dependent components - must run in order
- **TDD Enforcement**: All tests in Phase 3.2 MUST be written and failing before Phase 3.3
- **Testcontainers**: Integration tests use real PostgreSQL and etcd instances
- **File Paths**: All paths relative to repository root (C:/Users/pasha/Code/etcd_fdw/)

## Validation Checklist

- [x] All CLI commands have corresponding contract tests
- [x] All library interfaces have contract tests  
- [x] All entities have model implementation tasks
- [x] All tests come before implementation (TDD enforced)
- [x] Parallel tasks are truly independent (different files)
- [x] Each task specifies exact file path
- [x] No [P] task conflicts with another [P] task on same file
- [x] Integration tests cover all user scenarios from quickstart.md
- [x] Performance requirements addressed (>1000 ops/sec, <1s latency)

## Task Execution Ready
Total: 57 tasks organized in 5 phases with clear dependencies and parallel execution opportunities.
