-- NOTE: keep this file ASCII-only. sqlc miscomputes query boundaries when a
-- .sql file contains multi-byte characters and silently truncates the SQL.
-- Explanations live in pkg/repo/jobs.go instead.

-- name: CreateJob :exec
INSERT INTO jobs (id, tenant_id, type, trigger, status, created_by, total_count, params)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- Every read is scoped by tenant_id, admins included: the admin routes reach
-- this through the same handler with a tenant_id taken from the URL.
-- name: GetJob :one
SELECT * FROM jobs WHERE tenant_id = ? AND id = ? LIMIT 1;

-- name: ListJobsPage :many
SELECT * FROM jobs
WHERE tenant_id = sqlc.arg(tenant_id)
  AND (sqlc.narg(type) IS NULL OR type = sqlc.narg(type))
  AND (sqlc.narg(status) IS NULL OR status = sqlc.narg(status))
ORDER BY created_at DESC, id
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: CountJobs :one
SELECT COUNT(*) FROM jobs
WHERE tenant_id = sqlc.arg(tenant_id)
  AND (sqlc.narg(type) IS NULL OR type = sqlc.narg(type))
  AND (sqlc.narg(status) IS NULL OR status = sqlc.narg(status));

-- name: StartJob :execrows
UPDATE jobs
SET status = 'running', started_at = CURRENT_TIMESTAMP, heartbeat_at = CURRENT_TIMESTAMP
WHERE id = ? AND status = 'pending';

-- name: TouchJobHeartbeat :exec
UPDATE jobs SET heartbeat_at = CURRENT_TIMESTAMP WHERE id = ?;

-- Counters are bumped with a relative UPDATE rather than read-modify-write:
-- N workers finish items concurrently, and "SELECT then SET" loses increments.
-- name: BumpJobCounts :exec
UPDATE jobs
SET success_count = success_count + ?, failed_count = failed_count + ?
WHERE id = ?;

-- name: FinishJob :execrows
UPDATE jobs
SET status = ?, error_summary = ?, finished_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- Only a job that has not finished yet can be asked to stop.
-- name: RequestJobStop :execrows
UPDATE jobs SET status = 'stopping'
WHERE tenant_id = ? AND id = ? AND status IN ('pending', 'running');

-- name: GetJobStatus :one
SELECT status FROM jobs WHERE id = ? LIMIT 1;

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
VALUES (?, ?, ?, ?, ?, 'pending');

-- name: ListJobItemsPage :many
SELECT * FROM job_items
WHERE job_id = sqlc.arg(job_id)
  AND (sqlc.narg(status) IS NULL OR status = sqlc.narg(status))
ORDER BY position
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: CountJobItems :one
SELECT COUNT(*) FROM job_items
WHERE job_id = sqlc.arg(job_id)
  AND (sqlc.narg(status) IS NULL OR status = sqlc.narg(status));

-- name: ListPendingJobItems :many
SELECT * FROM job_items WHERE job_id = ? AND status = 'pending' ORDER BY position;

-- name: StartJobItem :exec
UPDATE job_items SET status = 'running', started_at = CURRENT_TIMESTAMP WHERE id = ?;

-- name: FinishJobItem :exec
UPDATE job_items
SET status = ?, error_kind = ?, error = ?, finished_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: CreateJobEvent :exec
INSERT INTO job_events (id, job_id, seq, kind, payload) VALUES (?, ?, ?, ?, ?);

-- Replay after a dropped SSE connection: the client sends its last seq back.
-- name: ListJobEventsAfter :many
SELECT * FROM job_events WHERE job_id = ? AND seq > ? ORDER BY seq LIMIT ?;

-- name: MaxJobEventSeq :one
SELECT COALESCE(MAX(seq), 0) FROM job_events WHERE job_id = ?;

-- name: DeleteJobEventsBefore :exec
DELETE FROM job_events WHERE created_at < ?;
