ALTER TABLE jobs
	ADD COLUMN IF NOT EXISTS trigger VARCHAR(32) NULL,
	ADD COLUMN IF NOT EXISTS webhook_secret VARCHAR(128) NULL;

CREATE TABLE IF NOT EXISTS job_runs (
	id VARCHAR(32) PRIMARY KEY,
	job_id VARCHAR(32) NULL,
	user_id VARCHAR(32) NULL,
	trigger VARCHAR(32) NULL,
	status VARCHAR(32) NULL,
	run_id VARCHAR(128) NULL,
	error TEXT NULL,
	started_at TIMESTAMPTZ NULL,
	completed_at TIMESTAMPTZ NULL,
	duration_milliseconds BIGINT NULL,
	request_method VARCHAR(16) NULL,
	request_path VARCHAR(512) NULL,
	remote_address VARCHAR(128) NULL,
	CONSTRAINT job_runs_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS job_runs_job_id_index ON job_runs (job_id);
CREATE INDEX IF NOT EXISTS job_runs_user_id_index ON job_runs (user_id);
CREATE INDEX IF NOT EXISTS job_runs_started_at_index ON job_runs (started_at);
