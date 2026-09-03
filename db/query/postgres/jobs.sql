-- NOTE: keep this file ASCII-only. sqlc miscomputes query boundaries when a
-- .sql file contains multi-byte characters and silently truncates the SQL.
-- Explanations live in pkg/repo/jobs.go instead.

-- name: CreateJob :exec
INSERT INTO jobs (id, tenant_id, type, trigger, status, created_by, total_count, params)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- Every read is scoped by tenant_id, admins included: the admin routes reach
-- this through the same handler with a tenant_id taken from the URL.
-- name: GetJob :one
SELECT * FROM jobs WHERE tenant_id = $1 AND id = $2 LIMIT 1;

-- name: ListJobsPage :many
SELECT * FROM jobs
WHERE tenant_id = sqlc.arg(tenant_id)
  AND (sqlc.narg(type)::text IS NULL OR type = sqlc.narg(type)::text)
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
ORDER BY created_at DESC, id
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: CountJobs :one
SELECT COUNT(*) FROM jobs
WHERE tenant_id = sqlc.arg(tenant_id)
  AND (sqlc.narg(type)::text IS NULL OR type = sqlc.narg(type)::text)
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text);

-- name: StartJob :execrows
UPDATE jobs
SET status = 'running', started_at = CURRENT_TIMESTAMP, heartbeat_at = CURRENT_TIMESTAMP
WHERE id = $1 AND status = 'pending';

-- name: TouchJobHeartbeat :exec
UPDATE jobs SET heartbeat_at = CURRENT_TIMESTAMP WHERE id = $1;

-- Counters are bumped with a relative UPDATE rather than read-modify-write:
-- N workers finish items concurrently, and "SELECT then SET" loses increments.
-- name: BumpJobCounts :exec
UPDATE jobs
SET success_count = success_count + $1, failed_count = failed_count + $2
WHERE id = $3;

-- name: FinishJob :execrows
UPDATE jobs
SET status = $1, error_summary = $2, finished_at = CURRENT_TIMESTAMP
WHERE id = $3;

-- Only a job that has not finished yet can be asked to stop.
-- name: RequestJobStop :execrows
UPDATE jobs SET status = 'stopping'
WHERE tenant_id = $1 AND id = $2 AND status IN ('pending', 'running');

-- name: GetJobStatus :one
SELECT status FROM jobs WHERE id = $1 LIMIT 1;

-- Zombie reaping on startup: a killed process leaves rows stuck in running.
-- name: ListStaleJobs :many
SELECT * FROM jobs
WHERE status IN ('running', 'stopping')
  AND (heartbeat_at IS NULL OR heartbeat_at < sqlc.arg(cutoff));

-- name: MarkJobInterrupted :exec
UPDATE jobs
SET status = 'interrupted', finished_at = CURRENT_TIMESTAMP,
    error_summary = sqlc.arg(error_summary)
WHERE id = sqlc.arg(id);

-- name: CreateJobItem :exec
INSERT INTO job_items (id, job_id, account_id, email, position, status)
VALUES ($1, $2, $3, $4, $5, 'pending');

-- name: ListJobItemsPage :many
SELECT * FROM job_items
WHERE job_id = sqlc.arg(job_id)
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
ORDER BY position
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: CountJobItems :one
SELECT COUNT(*) FROM job_items
WHERE job_id = sqlc.arg(job_id)
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text);

-- name: ListPendingJobItems :many
SELECT * FROM job_items WHERE job_id = $1 AND status = 'pending' ORDER BY position;

-- name: StartJobItem :exec
UPDATE job_items SET status = 'running', started_at = CURRENT_TIMESTAMP WHERE id = $1;

-- name: FinishJobItem :exec
UPDATE job_items
SET status = $1, error_kind = $2, error = $3, finished_at = CURRENT_TIMESTAMP
WHERE id = $4;

-- name: CreateJobEvent :exec
INSERT INTO job_events (id, job_id, seq, kind, payload) VALUES ($1, $2, $3, $4, $5);

-- Replay after a dropped SSE connection: the client sends its last seq back.
-- name: ListJobEventsAfter :many
SELECT * FROM job_events WHERE job_id = $1 AND seq > $2 ORDER BY seq LIMIT $3;

-- name: MaxJobEventSeq :one
SELECT COALESCE(MAX(seq), 0) FROM job_events WHERE job_id = $1;

-- name: DeleteJobEventsBefore :exec
DELETE FROM job_events WHERE created_at < $1;

-- Used by the scheduler to avoid stacking refresh jobs inside one tenant.
-- Two concurrent jobs mean 2 x JOB_WORKERS connections hitting the same
-- provider, which is exactly what the per-account delay exists to prevent.
-- name: CountActiveJobsByType :one
SELECT COUNT(*) FROM jobs
WHERE tenant_id = $1 AND type = $2 AND status IN ('pending', 'running', 'stopping');

-- Retention sweep. job_items and job_events go with the row via ON DELETE
-- CASCADE; mail_refresh_logs.job_id is ON DELETE SET NULL, so log rows survive
-- their job and are swept on their own schedule.
--
-- COALESCE rather than a bare finished_at test: every current writer sets that
-- column when it moves a job to a terminal state, but a row that somehow got
-- there without one would otherwise be undeletable forever.
-- name: DeleteFinishedJobsBefore :exec
DELETE FROM jobs
WHERE status IN ('succeeded', 'partial', 'failed', 'stopped', 'interrupted')
  AND COALESCE(finished_at, created_at) < sqlc.arg(cutoff);
