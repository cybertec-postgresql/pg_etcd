# GitHub Copilot Instructions: etcd_fdw Single Package Architecture

## Project Context

**Tool**: etcd_fdw - Bidirectional synchronization between etcd and PostgreSQL  
**Language**: Go 1.25  
**Architecture**: Single package consolidation (refactoring from separate internal/etcd and internal/db packages)  
**Database**: PostgreSQL with single `etcd` table using revision status encoding  
**Key Principle**: KISS (Keep It Simple, Stupid) - maximum simplicity, minimal complexity

## Current Refactor Context

**Objective**: Consolidate `internal/etcd` and `internal/db` packages into single `internal/sync` package  
**Dependencies**: Only use existing packages from go.mod (pgx/v5, etcd client/v3, logrus, go-retry, testcontainers)  
**Constraints**: No new dependencies, use INSERT ON CONFLICT with pgx.Batch instead of COPY, minimal test cases

## Architecture Overview

### Single Table Design
```sql
CREATE TABLE etcd (
    ts timestamp with time zone NOT NULL DEFAULT now(),
    key text NOT NULL,
    value text,
    revision bigint NOT NULL,
    tombstone boolean NOT NULL DEFAULT false,
    PRIMARY KEY(key, revision)
);
```

**Revision Encoding**:
- `revision = -1`: Pending sync to etcd (PostgreSQL → etcd)
- `revision > 0`: Real etcd revision (etcd → PostgreSQL)

### Package Structure (Target)
```
internal/sync/
├── sync.go          # Main service orchestration
├── postgresql.go    # PostgreSQL operations (consolidated from internal/db)
├── etcd.go         # etcd client operations (consolidated from internal/etcd) 
├── config.go       # Connection management
└── sync_test.go    # Consolidated minimal tests
```

## Key Functions to Consolidate

### From internal/db/postgres.go:
- `BulkInsert()` - Replace COPY with INSERT ON CONFLICT using pgx.Batch
- `GetPendingRecords()` - Get records with revision = -1
- `UpdateRevision()` - Update revision after etcd sync
- `GetLatestRevision()` - For watch resume points

### From internal/etcd/client.go:
- `NewEtcdClient()` - etcd connection management
- `GetAllKeys()` - Initial sync from etcd
- `WatchPrefix()` - Continuous etcd monitoring
- `Put()`, `Delete()` - etcd operations

### From internal/sync/sync.go (existing):
- `Service` - Main orchestration service
- `Start()` - Bidirectional sync coordination

## Code Style Preferences

**Simplicity First**:
- No nested function calls unless necessary
- Direct error handling (no complex wrapping)
- Minimal logging (only essential events)
- Concrete types over interfaces within package
- pgx.Batch for bulk operations instead of COPY

**Testing**:
- Minimal test cases covering maximum functionality
- Use testcontainers for integration tests
- No bloated test code
- Focus on essential behavior validation

**Error Handling**:
- Use existing retry logic from internal/retry package
- Return errors directly with context
- Log errors at appropriate levels only

## Recent Changes

1. **Specification Created**: Single package architecture refactor defined
2. **Research Completed**: Package consolidation strategy determined
3. **Data Model Designed**: Unified structures for sync package
4. **Contracts Defined**: Public API for consolidated package
5. **Quickstart Created**: Step-by-step refactor process

## Current Task Context

**Phase**: Implementation planning complete, ready for /tasks command
**Next**: Generate specific implementation tasks for package consolidation
**Focus**: Maintain identical behavior while simplifying architecture

## Key Principles for Implementation

- Preserve all existing functionality exactly
- Maintain test coverage without bloating
- Use only existing dependencies from go.mod
- Follow KISS principle throughout
- No performance regression
- Simplify imports and reduce cognitive complexity