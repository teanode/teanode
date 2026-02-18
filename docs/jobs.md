# Jobs and Scheduler

The `internal/jobs` package provides a minimal cron-like scheduler used by TeaNode for background automations and reminders.

## Overview

- Jobs are identified by an ID and have:
  - A schedule (cron expression or one-shot delay, configured elsewhere in the codebase).
  - A payload describing what should happen when the job fires (typically an agent message or RPC call).
- The scheduler wakes up on a **per-minute tick** (see package comment in `internal/jobs/jobs.go`).
- When a job is due, the scheduler enqueues work to be handled by the gateway / agents layer; the exact integration is handled in the gateway code rather than in this package.

Although the current `jobs.go` file only declares the package and logger, the surrounding architecture (see `docs/architecture.md` and TODOs under **Automation**) assumes:

- A persistent store layer for jobs.
- A scheduler loop that:
  - Computes the next run time for each job.
  - Triggers jobs on schedule.
- Tool-facing functions that expose job operations to agents (e.g. list, create, update, delete, trigger).

This document captures that intended design so future implementations under `internal/jobs` remain consistent with the rest of the system docs.
