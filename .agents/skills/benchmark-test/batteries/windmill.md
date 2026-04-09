# Windmill Query Battery — Grounded Expected Symbols

All symbols verified against windmill-labs/windmill source on GitHub (2026-05-05).

## Investigation 1: Job submission and cancellation APIs (8 symbols)

Query keyword: `"job run queue cancel result flow script path"`
Query intent: `"how does Windmill submit jobs and cancel running or queued work"`

Expected symbols:
- `run_flow_by_path` — backend/windmill-api/src/jobs.rs — API handler that submits a flow run by path with trigger metadata and args
- `run_script_by_path` — backend/windmill-api/src/jobs.rs — API handler that submits a script run by path
- `list_queue_jobs` — backend/windmill-api/src/jobs.rs — queue-focused API endpoint for listing queued and running jobs
- `list_jobs` — backend/windmill-api/src/jobs.rs — unified jobs listing endpoint across queue and completed jobs
- `cancel_selection` — backend/windmill-api/src/jobs.rs — bulk cancellation API that groups and validates selected jobs
- `cancel_jobs` — backend/windmill-api-jobs/src/execution.rs — API-side cancellation routine that fast-cancels simple jobs and delegates deeper queue cancellation
- `run_wait_result_internal` — backend/windmill-api-jobs/src/execution.rs — helper that waits for completion and can auto-cancel on disconnect or timeout
- `cancel_job` — backend/windmill-queue/src/jobs.rs — queue-layer cancellation primitive that recursively cancels child work when needed

## Investigation 2: Flow definitions and runtime state (9 symbols)

Query keyword: `"flow create update version status module worker next job"`
Query intent: `"how does Windmill create flows and advance flow execution state at runtime"`

Expected symbols:
- `create_flow` — backend/windmill-api-flows/src/flows.rs — API handler that creates a flow, inserts its first version, and queues dependency processing
- `update_flow` — backend/windmill-api-flows/src/flows.rs — API handler that updates or renames a flow and appends a new version
- `get_flow_history` — backend/windmill-api-flows/src/flows.rs — API endpoint returning version history and deployment metadata for a flow
- `FlowValue` — backend/windmill-types/src/flows.rs — canonical stored flow definition with modules, settings, and execution options
- `FlowStatus` — backend/windmill-types/src/flow_status.rs — top-level persisted runtime state for a flow run
- `FlowStatusModule` — backend/windmill-types/src/flow_status.rs — per-module runtime state machine for branches, loops, waits, and job outcomes
- `handle_flow` — backend/windmill-worker/src/worker_flow.rs — worker entrypoint that executes or resumes a flow job
- `push_next_flow_job` — backend/windmill-worker/src/worker_flow.rs — scheduler that decides the next module/subflow job to enqueue
- `update_flow_status_after_job_completion_internal` — backend/windmill-worker/src/worker_flow.rs — core state-transition logic after a flow step finishes

## Investigation 3: Queue pulling and scheduling (8 symbols)

Query keyword: `"queue pull push scheduled concurrency key worker priority"`
Query intent: `"how does Windmill queue jobs, pull runnable work, and enforce scheduling constraints"`

Expected symbols:
- `push` — backend/windmill-queue/src/jobs.rs — central enqueue path that persists queued jobs with scheduling and auth metadata
- `pull` — backend/windmill-queue/src/jobs.rs — main dequeue loop that pulls runnable jobs and applies concurrency checks
- `pull_single_job_and_mark_as_running_no_concurrency_limit` — backend/windmill-queue/src/jobs.rs — lower-level pull routine that picks a runnable job and marks it running
- `try_schedule_next_job` — backend/windmill-queue/src/jobs.rs — scheduler helper that enqueues the next scheduled tick after completion
- `push_scheduled_job` — backend/windmill-queue/src/schedule.rs — computes the next cron occurrence and pushes a schedule-triggered job
- `get_schedule_opt` — backend/windmill-queue/src/schedule.rs — loads persisted schedule definitions for queue follow-up logic
- `insert_concurrency_key` — backend/windmill-queue/src/jobs.rs — stores a resolved concurrency key and ensures counter rows exist
- `ConcurrencySettings` — backend/windmill-types/src/runnable_settings.rs — shared concurrency configuration for jobs and flows

## Investigation 4: Worker execution and result processing (8 symbols)

Query keyword: `"worker execute dispatch result processor dependency flow"`
Query intent: `"how does a Windmill worker pull work, execute it, and process completed job results"`

Expected symbols:
- `run_worker` — backend/windmill-worker/src/worker.rs — main worker loop that pings, pulls jobs, and dispatches execution
- `handle_queued_job` — backend/windmill-worker/src/worker.rs — central dispatcher that routes jobs to the correct executor or flow handler
- `start_background_processor` — backend/windmill-worker/src/result_processor.rs — starts the completion-processing loop for finished jobs
- `process_completed_job` — backend/windmill-worker/src/result_processor.rs — persists job results and triggers downstream flow state updates
- `handle_job_error` — backend/windmill-worker/src/result_processor.rs — records execution failures and propagates them into flow state
- `update_flow_status_after_job_completion` — backend/windmill-worker/src/worker_flow.rs — public flow completion hook used after each job finishes
- `handle_dependency_job` — backend/windmill-worker/src/worker_lockfiles.rs — executes script dependency jobs and updates lockfiles/metadata
- `handle_flow_dependency_job` — backend/windmill-worker/src/worker_lockfiles.rs — re-locks and refreshes flow dependency state and versions

## Investigation 5: Trigger listeners and trigger-to-job dispatch (8 symbols)

Query keyword: `"trigger listener http route event dispatch flow script"`
Query intent: `"how does Windmill listen for triggers and turn incoming events into jobs or flow runs"`

Expected symbols:
- `TriggerCrud` — backend/windmill-trigger/src/handler.rs — core trait implemented by trigger handlers for CRUD, validation, and route wiring
- `Listener` — backend/windmill-trigger/src/listener.rs — trait for long-lived trigger consumers that connect to upstream systems and process events
- `listen_to_unlistened_events` — backend/windmill-trigger/src/listener.rs — scans for enabled triggers that are not currently being listened to and spawns listeners
- `TriggerJobArgs` — backend/windmill-trigger/src/trigger_helpers.rs — compatibility layer that shapes trigger payloads into runnable job args
- `trigger_runnable_inner` — backend/windmill-trigger/src/trigger_helpers.rs — central dispatcher that converts a trigger event into a queued flow or script run
- `get_logical_replication_stream` — backend/windmill-trigger-postgres/src/listener.rs — opens the PostgreSQL logical replication stream for Postgres triggers
- `refresh_routers` — backend/windmill-trigger-http/src/lib.rs — rebuilds the in-memory router cache for HTTP triggers
- `route_job` — backend/windmill-api/src/triggers/http/handler.rs — HTTP trigger entrypoint that authenticates, resolves the route, and dispatches job execution
