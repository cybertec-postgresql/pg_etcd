# Implementation Plan: [FEATURE]

**Branch**: `[###-feature-name]` | **Date**: [DATE] | **Spec**: [link]
**Input**: Feature specification from `/specs/[###-feature-name]/spec.md`

## Execution Flow (/plan command scope)
```
1. Load feature spec from Input path
   → If not found: ERROR "No feature spec at {path}"
2. Fill Technical Context (scan for NEEDS CLARIFICATION)
   → Detect Project Type from context (web=frontend+backend, mobile=app+api)
   → Set Structure Decision based on project type
3. Evaluate Constitution Check section below
   → If violations exist: Document in Complexity Tracking
   → If no justification possible: ERROR "Simplify approach first"
   → Update Progress Tracking: Initial Constitution Check
4. Execute Phase 0 → research.md
   → If NEEDS CLARIFICATION remain: ERROR "Resolve unknowns"
5. Execute Phase 1 → contracts, data-model.md, quickstart.md, agent-specific template file (e.g., `CLAUDE.md` for Claude Code, `.github/copilot-instructions.md` for GitHub Copilot, or `GEMINI.md` for Gemini CLI).
6. Re-evaluate Constitution Check section
   → If new violations: Refactor design, return to Phase 1
   → Update Progress Tracking: Post-Design Constitution Check
7. Plan Phase 2 → Describe task generation approach (DO NOT create tasks.md)
8. STOP - Ready for /tasks command
```

**IMPORTANT**: The /plan command STOPS at step 7. Phases 2-4 are executed by other commands:
- Phase 2: /tasks command creates tasks.md
- Phase 3-4: Implementation execution (manual or via tools)

## Summary
Simple etcd_fdw program that performs bidirectional synchronization between etcd and PostgreSQL. Uses direct connections via DSN strings, PostgreSQL COPY for bulk operations, etcd Watch for changes, and PostgreSQL LISTEN/NOTIFY for WAL events. Conflict resolution always favors etcd (etcd wins).

## Technical Context
**Language/Version**: Go 1.21+  
**Primary Dependencies**: jackc/pgx/v5 (PostgreSQL driver), etcd-io/etcd/client/v3 (etcd client), jessevdk/go-flags (command line)  
**Storage**: PostgreSQL with etcd and etcd_wal tables, etcd cluster  
**Testing**: github.com/stretchr/testify, testcontainers/testcontainers-go (integration tests)  
**Target Platform**: Linux server  
**Project Type**: single - Simple Go binary with minimal packages  
**Performance Goals**: Real-time synchronization, handle typical etcd workloads  
**Constraints**: Dead simple design, no overcomplication, direct database access only  
**Scale/Scope**: Single binary, no sub-commands, configuration via CLI flags and environment variables only

## Constitution Check
*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

**Simplicity**:
- Projects: 1 (single etcd_fdw binary)
- Using framework directly? YES (direct etcd client.Watch(), pgx.Conn, no wrappers)
- Single data model? YES (etcd key-value pairs with revisions)
- Avoiding patterns? YES (no Repository/UoW/interfaces, direct client access)

**Architecture**:
- EVERY feature as library? NO (single main package with minimal helpers)
- Libraries listed: N/A (direct dependencies only)
- CLI per library: N/A (single etcd_fdw command)
- Library docs: N/A (simple README)

**Testing (NON-NEGOTIABLE)**:

- RED-GREEN-Refactor cycle enforced? YES (integration tests fail first)
- Git commits show tests before implementation? YES
- Order: Contract→Integration→E2E→Unit strictly followed? YES
- Real dependencies used? YES (testcontainers for PostgreSQL and etcd)
- Integration tests for: new features, contract changes, shared schemas? YES
- FORBIDDEN: Implementation before test, skipping RED phase

**Observability**:

- Structured logging included? YES (logrus)
- Frontend logs → backend? N/A (single binary)
- Error context sufficient? YES (connection errors, sync failures)

**Versioning**:

- Version number assigned? YES (1.0.0)
- BUILD increments on every change? YES
- Breaking changes handled? YES (parallel tests, migration plan)

## Project Structure

### Documentation (this feature)
```
specs/[###-feature]/
├── plan.md              # This file (/plan command output)
├── research.md          # Phase 0 output (/plan command)
├── data-model.md        # Phase 1 output (/plan command)
├── quickstart.md        # Phase 1 output (/plan command)
├── contracts/           # Phase 1 output (/plan command)
└── tasks.md             # Phase 2 output (/tasks command - NOT created by /plan)
```

### Source Code (repository root)
```
# Option 1: Single project (DEFAULT)
src/
├── models/
├── services/
├── cli/
└── lib/

tests/
├── contract/
├── integration/
└── unit/

# Option 2: Web application (when "frontend" + "backend" detected)
backend/
├── src/
│   ├── models/
│   ├── services/
│   └── api/
└── tests/

frontend/
├── src/
│   ├── components/
│   ├── pages/
│   └── services/
└── tests/

# Option 3: Mobile + API (when "iOS/Android" detected)
api/
└── [same as backend above]

ios/ or android/
└── [platform-specific structure]
```

**Structure Decision**: [DEFAULT to Option 1 unless Technical Context indicates web/mobile app]

## Phase 0: Outline & Research
1. **Extract unknowns from Technical Context** above:
   - For each NEEDS CLARIFICATION → research task
   - For each dependency → best practices task
   - For each integration → patterns task

2. **Generate and dispatch research agents**:
   ```
   For each unknown in Technical Context:
     Task: "Research {unknown} for {feature context}"
   For each technology choice:
     Task: "Find best practices for {tech} in {domain}"
   ```

3. **Consolidate findings** in `research.md` using format:
   - Decision: [what was chosen]
   - Rationale: [why chosen]
   - Alternatives considered: [what else evaluated]

**Output**: research.md with all NEEDS CLARIFICATION resolved

## Phase 1: Design & Contracts
*Prerequisites: research.md complete*

1. **Extract entities from feature spec** → `data-model.md`:
   - etcd key-value pairs with revision metadata
   - PostgreSQL table schema (etcd, etcd_wal)
   - No DTOs or complex entities - direct mapping

2. **Generate CLI contract** → `/contracts/cli.md`:
   - Single etcd_fdw command with DSN parameters
   - Environment variable support
   - Exit codes for monitoring

3. **Generate integration tests** → quickstart validation:
   - Set up PostgreSQL and etcd containers
   - Test bidirectional sync scenarios
   - Conflict resolution validation (etcd wins)

4. **Keep it simple**:
   - No REST/GraphQL endpoints (single binary)
   - No complex API contracts
   - Direct database/etcd operations only

5. **Update agent file incrementally**:
   - Run `/scripts/update-agent-context.sh` for GitHub Copilot
   - Add simplified architecture info
   - Document "dead simple" constraint

**Output**: data-model.md, /contracts/*, failing tests, quickstart.md, agent-specific file

## Phase 2: Task Planning Approach
*This section describes what the /tasks command will do - DO NOT execute during /plan*

**Task Generation Strategy**:

- Load `/templates/tasks-template.md` as base
- Generate tasks from simplified design (no complex contracts)
- Basic integration test tasks (testcontainers setup)
- Direct etcd and PostgreSQL connection tasks
- Bidirectional sync implementation tasks
- No interface/abstraction tasks (direct client usage)

**Ordering Strategy**:

- TDD order: Integration tests before implementation
- Simple dependency order: Connection setup → sync logic → monitoring
- Mark [P] for parallel execution where possible

**Estimated Output**: 15-20 numbered, ordered tasks in tasks.md (simplified from typical 25-30)

**IMPORTANT**: This phase is executed by the /tasks command, NOT by /plan

## Phase 3+: Future Implementation
*These phases are beyond the scope of the /plan command*

**Phase 3**: Task execution (/tasks command creates tasks.md)  
**Phase 4**: Implementation (execute tasks.md following constitutional principles)  
**Phase 5**: Validation (run tests, execute quickstart.md, performance validation)

## Complexity Tracking
*Fill ONLY if Constitution Check has violations that must be justified*

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| NONE | Single binary approach | N/A - complies with constitution |

**Note**: Simplified design eliminates complexity violations. Single binary with direct client usage avoids interfaces, patterns, and over-abstraction.


## Progress Tracking
*This checklist is updated during execution flow*

**Phase Status**:
- [ ] Phase 0: Research complete (/plan command)
- [ ] Phase 1: Design complete (/plan command)
- [ ] Phase 2: Task planning complete (/plan command - describe approach only)
- [ ] Phase 3: Tasks generated (/tasks command)
- [ ] Phase 4: Implementation complete
- [ ] Phase 5: Validation passed

**Gate Status**:

- [x] Initial Constitution Check: PASS
- [ ] Post-Design Constitution Check: PASS
- [ ] All NEEDS CLARIFICATION resolved
- [ ] Complexity deviations documented

---
*Based on Constitution v2.1.1 - See `/memory/constitution.md`*
