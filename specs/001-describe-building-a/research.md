# Research Results: Bidirectional Synchronization Between etcd and PostgreSQL

## Technology Stack Analysis

### Go Libraries

**Decision**: Use jackc/pgx/v5 for PostgreSQL operations
**Rationale**: 
- High-performance PostgreSQL driver with async support
- Native LISTEN/NOTIFY support for real-time change detection
- Connection pooling and prepared statement support
- Type-safe parameter binding

**Alternatives considered**: 
- database/sql with pq driver - less efficient, no native async support
- GORM - adds unnecessary ORM overhead for key-value operations

**Decision**: Use etcd-io/etcd/client/v3 for etcd operations
**Rationale**:
- Official etcd client library
- Built-in watch functionality for real-time updates
- Revision-based operations for conflict detection
- Lease and transaction support

**Alternatives considered**:
- go.etcd.io/etcd/clientv3 (older import path) - same library, different import

**Decision**: Use jessevdk/go-flags for configuration
**Rationale**:
- Clean flag parsing with struct tags
- Environment variable support
- Help generation
- Type safety

**Alternatives considered**:
- cobra/viper - more complex than needed for simple config
- flag package - basic but lacks advanced features

### Architecture Patterns

**Decision**: Use PostgreSQL triggers with NOTIFY for change detection
**Rationale**:
- Immediate notification of changes without polling
- Atomic operations ensure consistency
- Native PostgreSQL feature, reliable and tested
- Low latency for real-time synchronization

**Alternatives considered**:
- Polling database for changes - higher latency, more resource intensive
- WAL-based replication - complex setup, overkill for key-value sync

**Decision**: Use revision-based conflict resolution
**Rationale**:
- etcd's native revision system provides ordering
- Compare-and-swap operations prevent lost updates
- Deterministic conflict resolution
- Handles concurrent modifications safely

**Alternatives considered**:
- Timestamp-based resolution - clock skew issues
- Last-write-wins - potential data loss
- Manual conflict resolution - complexity for operators

### Database Schema Design

**Decision**: Separate etcd and etcd_wal tables
**Rationale**:
- etcd table holds current state with full revision history
- etcd_wal table tracks pending changes for retry logic
- Clean separation of concerns
- Enables audit trail and rollback capabilities

**Alternatives considered**:
- Single table design - harder to track pending operations
- Event sourcing pattern - added complexity for simple key-value store

### Testing Strategy

**Decision**: Use testcontainers for integration tests
**Rationale**:
- Real PostgreSQL and etcd instances in tests
- Isolated test environments
- No mocking of critical database operations
- Validates actual integration behavior

**Alternatives considered**:
- Mocked dependencies - doesn't test real integration
- Docker compose - less portable, harder CI integration
- In-memory databases - different behavior than production

### Synchronization Flow

**Decision**: Bidirectional sync with etcd as source of truth for conflicts
**Rationale**:
- etcd designed for distributed consensus
- Revision numbers provide clear ordering
- PostgreSQL acts as durable cache/query layer
- Supports both real-time sync and recovery scenarios

**Alternatives considered**:
- PostgreSQL as source of truth - loses etcd's consensus guarantees
- Peer-to-peer sync - complex conflict resolution

## Implementation Approach

### Service Architecture
1. **Sync Service**: Main orchestrator with CLI interface
2. **PostgreSQL Library**: Database operations with LISTEN/NOTIFY
3. **etcd Library**: Watch operations and key-value management
4. **Conflict Resolver**: Revision-based conflict resolution logic

### Error Handling
1. **Retry Logic**: Exponential backoff for transient failures
2. **Dead Letter Queue**: Failed operations logged for manual review
3. **Circuit Breaker**: Prevent cascade failures during outages
4. **Health Checks**: Monitor sync lag and connection status

### Performance Considerations
1. **Batching**: Group multiple operations for efficiency
2. **Connection Pooling**: Reuse database connections
3. **Parallel Processing**: Handle independent operations concurrently
4. **Memory Management**: Stream large datasets instead of loading all

## Risk Mitigation

### Compaction Handling
- Detect compaction events via etcd watch errors
- Full snapshot resync when compaction detected
- Maintain compaction log for debugging

### Split-Brain Scenarios
- etcd consensus prevents split-brain at etcd level
- PostgreSQL triggers ensure atomic operations
- Revision checks prevent overwriting newer data

### Data Consistency
- Transaction boundaries around related operations
- Idempotent operations for safe retries
- Comprehensive integration tests with failure scenarios
