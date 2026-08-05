# Agent Groups

Agent Groups run multi-agent workflows inside a single DEEIX Chat conversation. A group has one **supervisor** and up to 31 **worker members** (32 members total). Members are built on existing conversation roles; the supervisor plans and dispatches steps, workers execute them serially, and the whole run shares one conversation context.

This guide covers enabling, using, sharing/exporting, auditing, deployment, and rollback of Agent Groups.

## Enabling the feature

Agent Groups are controlled by a runtime business setting (Feature Flag), stored in `system_settings` and maintained from the admin console:

| Setting namespace | Key | Default | Effect |
| --- | --- | --- | --- |
| `agent_group` | `enabled` | off | Master switch for the whole feature. |

> **Note**: the flag defaults to **off**. On fresh installs you must enable it before users can create groups — otherwise the create dialog fails with "save failed" (API returns `403 FEATURE_DISABLED`).

### How to enable

1. **Admin console (recommended)**: sign in as an admin, open **Admin → Agent Groups**, flip *Enable agent groups* to on, and save. The same page also exposes the three runtime limits below.
2. **Admin API**: `PATCH /api/v1/admin/settings` with an admin bearer token:

   ```json
   {
     "items": [
       { "namespace": "agent_group", "key": "enabled", "value": "true" }
     ]
   }
   ```

3. **Direct SQL** (only when the API is unreachable, e.g. during bootstrap):

   ```sql
   INSERT INTO system_settings (namespace, setting_key, setting_value, value_type, description, created_at, updated_at)
   VALUES ('agent_group', 'enabled', 'true', 'bool', 'Enable agent groups', now(), now())
   ON CONFLICT (namespace, setting_key) DO UPDATE SET setting_value = 'true', updated_at = now();
   ```

   The exact column names depend on the configured storage backend; adapt to your schema if needed.

When the flag is off:

- Backend rejects creating or modifying groups, creating group conversations, sending new group messages, and retrying steps.
- The frontend hides group entry points.
- Existing group conversations and historical runs stay read-only.
- An in-flight attempt is allowed to finish its current model/tool call and submit its checkpoint, but no next Step is created; the run transitions to `paused_retryable` with error code `FEATURE_DISABLED`.
- After re-enabling, the run can be resumed from that checkpoint with a retry.
- Regular conversations are unaffected.

Additional runtime settings (same `agent_group` namespace, admin console → Agent Groups):

| Key | Default | Meaning |
| --- | --- | --- |
| `max_steps_per_run` | `64` | Maximum supervisor decision steps per run. |
| `max_attempts_per_step` | `8` | Maximum attempts per step before the run is blocked. |
| `attempt_lease_seconds` | `600` | Lease duration for in-flight attempts (crash recovery window). |

## User guide

### Creating a group

1. Create the conversation roles that the members will be based on (a role provides system prompt, model, provider, and default tools/skills).
2. Open the project's group configuration and create a group: pick a name, an optional description, and a coordination prompt.
3. Select one supervisor role and one or more worker roles. Members are added from existing roles; the same role cannot be used by two members of the same group.
4. Optionally set a **model override** per member (overrides the role's default model) and a **duty instruction** (member-specific instructions injected on top of the role prompt).

Members can later be enabled/disabled, re-ordered, have their override/duty instruction updated, or be removed. The supervisor can be changed at any time (the old supervisor becomes a regular worker). The supervisor cannot be disabled or removed directly.

### Group conversations

- Group runs only in conversations bound to the group. Binding is set at conversation creation and is immutable afterwards.
- Runs are serial per conversation: one active run at a time. Different conversations of the same group can run concurrently.
- Each run freezes a configuration snapshot (member overrides > role defaults > platform defaults; credentials are never saved), so later group edits do not affect a paused run's retry.
- Execution is a supervisor/member loop: the supervisor decides who works next, the member executes (with its own thinking/tool events), and this repeats until the supervisor signals completion or a limit is reached.

### Runs, retries, and control

- Runs expose per-step **attempt history**; a failed step is retried **in place** (its original config snapshot is reused and a new attempt is appended).
- Retrying after a tool call with unknown side effects requires confirmation before the step is replayed.
- `Cancel` interrupts the current attempt and returns the run to `paused_retryable`.
- `Abandon` marks the run ended and no longer retryable.
- A run blocked by a step/attempt limit or a concurrent state change stays visible with its error code and can be retried or abandoned.
- Refreshing the page resumes the same run timeline (state and events are persisted).

### Limits

- 32 members per group.
- Role references protect deletion: a role used by a non-removed group member cannot be deleted, a project that still contains groups cannot be deleted, and a group with conversation/run history cannot be deleted (all return `409 Conflict`).

## Sharing and exporting

Public shares and default exports of group conversations contain only:

- The user's messages.
- The supervisor's final assistant message.
- The group name and necessary result metadata.

They never contain member internal instructions, full intermediate member outputs, explicit thinking/reasoning content, tool inputs, credentials or sensitive outputs, failure diagnostics or upstream debug information, or the run configuration snapshot. Trace payloads are additionally scrubbed of API keys, authorization/cookie fields, and upstream debug fields on public read paths.

## Auditing

Every group change and run event is recorded in the audit log with the acting user ID and resource ID:

| Action | Resource |
| --- | --- |
| `create_agent_group` / `update_agent_group` / `delete_agent_group` | `agent_group` |
| `add_agent_group_member` / `update_agent_group_member` / `remove_agent_group_member` / `reorder_agent_group_members` | `agent_group` |
| `change_agent_group_supervisor` | `agent_group` |
| `create_agent_group_run` / `complete_agent_group_run` | `agent_group_run` |
| `cancel_agent_group_run` / `abandon_agent_group_run` / `retry_agent_group_run_step` | `agent_group_run` |

Member update events record which fields changed (enabled state, model override, duty instruction). Retry events record the step ID; run events record the group public ID, revision, and conversation.

## Data model and migration

The feature adds tables only; no existing fields are removed. Old conversations keep a null `AgentGroupID`.

- `chat_agent_groups` — group configuration (name, description, coordination prompt, revision).
- `chat_agent_group_members` — members (role link, model override, duty instruction, enabled, sort order).
- `chat_agent_group_runs` — run state machine (status, state version, frozen config snapshot, error code).
- `chat_agent_group_steps` / `chat_agent_group_attempts` — step timeline and attempt history with frozen actor data.

PostgreSQL deployments should add table comments and needed indexes. SQLite deployments should verify indexes, CAS, and single-writer behavior (the runtime already defaults `SQLITE_MAX_OPEN_CONNS` to `1` with a busy timeout).

The schema migrates automatically on startup; no manual SQL is required for SQLite or PostgreSQL.

## Rollback and emergency shutdown

- **Code rollback**: older binaries ignore the new tables and nullable columns. No breaking conversation-field change is introduced, so downgrades are safe.
- **Emergency shutdown**: turn off the `agent_group.enabled` runtime setting first, then stop creating new GroupRuns. Historical data is retained for later recovery or diagnosis; in-flight runs pause at their last checkpoint and can be resumed after re-enabling.

## API

Agent Group APIs are documented in the generated Swagger UI (`/swagger/index.html`) and the TypeScript API contract (`packages/api-contract/src/types.generated.ts`):

- Group management: `/api/v1/conversation-agent-groups` (CRUD, members, supervisor).
- Run control: `/api/v1/agent-group-runs/{run_id}/cancel`, `/abandon`, and `/steps/{step_id}/retry` (NDJSON stream).
- Run queries: runs, steps, and attempts with frozen actor data.
