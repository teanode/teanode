# Jobs and Scheduler

The `internal/jobs` package provides a cron-like scheduler used by TeaNode for background automations and reminders.

## Overview

- Jobs are identified by an ID and have:
  - A schedule, either:
    - A cron expression (minute, hour, day-of-month, month, day-of-week), or
    - A one-shot delay for reminder-style jobs.
  - A payload describing what should happen when the job fires (typically an agent message or RPC call).
- The scheduler wakes up on an internal polling tick (**currently every 5 seconds**) and checks due jobs.
- When a job is due, the scheduler enqueues work to be handled by the coordinators / runners layer; the exact integration is handled in the coordinator code rather than in this package.

## Concepts

At a high level, `internal/jobs` is split into:

- **Store**: persistent storage for job definitions and last-run metadata (via `internal/store` backends).
- **Scheduler**: an in-memory loop that:
  - Computes the next run time for each job.
  - Deduplicates runs to avoid double-firing (e.g. if multiple polling ticks occur within the same scheduled minute).
  - Triggers jobs once per scheduled time.
- **Tools API**: helper functions that back the `jobs` agent tool, exposing operations like:
  - `list`: enumerate jobs with IDs, schedules, and enabled/disabled state.
  - `create` / `update`: write jobs into the store.
  - `delete`: remove jobs from the store.
  - `trigger`: fire a job immediately regardless of schedule.

For concrete type and field names, see the Go types under `internal/jobs` (scheduler and context) and `internal/store` (job operations).

## Relation to Agents and Conversations

- Jobs typically send a message into a specific agent (via the coordinator), optionally pointing at a conversation ID.
- This makes it possible to build features like:
  - Periodic summaries ("summarize this conversation every evening").
  - One-shot reminders ("remind me in 30 minutes").
  - Recurring automations ("run this maintenance task every night").

For more on how jobs interact with the rest of TeaNode, see `docs/architecture.md` and the `jobs` tool section in the agent skills documentation.
