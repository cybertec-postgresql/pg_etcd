# Execute tasks from the plan

Execute tasks from the plan.

This is the fourth step in the Spec-Driven Development lifecycle.

Given the context provided as an argument, do this:

1. Run `scripts/check-task-prerequisites.sh --json` from repo root and parse FEATURE_DIR and AVAILABLE_DOCS list. All paths must be absolute.
2. Load and analyze available design documents:
   - Always read plan.md for tech stack and libraries
   - Read tasks.md for existing tasks
   - IF EXISTS: Read data-model.md for entities
   - IF EXISTS: Read contracts/ for API endpoints
   - IF EXISTS: Read research.md for technical decisions
   - IF EXISTS: Read quickstart.md for test scenarios

   Note: Not all projects have all documents. For example:
   - CLI tools might not have contracts/
   - Simple libraries might not need data-model.md

3. Implement tasks according to tasks.md and the context provided as an argument.

4. If several tasks specified for implementing, order tasks by dependencies:
   - Setup before everything
   - Tests before implementation (TDD)
   - Models before services
   - Services before endpoints
   - Core before integration
   - Everything before polish

Context for task implementation: $ARGUMENTS