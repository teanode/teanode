CREATE TABLE IF NOT EXISTS configurations (
	id VARCHAR(32) PRIMARY KEY,
	data JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	modified_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS migrations (
	id VARCHAR(256) PRIMARY KEY,
	migrated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	reverse_sql TEXT NULL
);

CREATE TABLE IF NOT EXISTS agents (
	id VARCHAR(32) PRIMARY KEY,
	name VARCHAR(256) NULL,
	model VARCHAR(128) NULL,
	skills JSONB NULL,
	tools JSONB NULL,
	description TEXT NULL,
	avatar_media_id VARCHAR(32) NULL,
	summarized_at TIMESTAMPTZ NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	modified_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS users (
	id VARCHAR(32) PRIMARY KEY,
	username VARCHAR(128) NULL,
	password VARCHAR(128) NULL,
	admin BOOLEAN NULL,
	default_agent_id VARCHAR(32) NULL,
	telegram_chat_id BIGINT NULL,
	discord_user_id VARCHAR(128) NULL,
	avatar_media_id VARCHAR(32) NULL,
	description TEXT NULL,
	summarized_at TIMESTAMPTZ NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	modified_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	CONSTRAINT users_default_agent_id_fkey FOREIGN KEY (default_agent_id) REFERENCES agents(id) ON DELETE SET NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS users_username_unique_index ON users (username) WHERE username IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS users_telegram_chat_id_unique_index ON users (telegram_chat_id) WHERE telegram_chat_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS users_discord_user_id_unique_index ON users (discord_user_id) WHERE discord_user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS users_telegram_chat_id_index ON users (telegram_chat_id);
CREATE INDEX IF NOT EXISTS users_discord_user_id_index ON users (discord_user_id);

CREATE TABLE IF NOT EXISTS projects (
	id VARCHAR(32) PRIMARY KEY,
	name VARCHAR(256) NULL,
	description TEXT NULL,
	summarized_at TIMESTAMPTZ NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	modified_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS tokens (
	id VARCHAR(32) PRIMARY KEY,
	user_id VARCHAR(32) NULL,
	token VARCHAR(128) NULL,
	last_used_at TIMESTAMPTZ NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	modified_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	CONSTRAINT tokens_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS tokens_token_unique_index ON tokens (token) WHERE token IS NOT NULL;
CREATE INDEX IF NOT EXISTS tokens_user_id_index ON tokens (user_id);

CREATE TABLE IF NOT EXISTS workspace_files (
	id VARCHAR(32) PRIMARY KEY,
	scope VARCHAR(32) NOT NULL,
	scope_id VARCHAR(32) NOT NULL,
	path VARCHAR(512) NOT NULL,
	content BYTEA NOT NULL DEFAULT ''::bytea,
	content_type VARCHAR(128) NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	modified_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS workspace_files_scope_scope_id_path_unique_index ON workspace_files (scope, scope_id, path);
CREATE INDEX IF NOT EXISTS workspace_files_scope_scope_id_index ON workspace_files (scope, scope_id);
CREATE INDEX IF NOT EXISTS workspace_files_scope_scope_id_path_index ON workspace_files (scope, scope_id, path);

CREATE TABLE IF NOT EXISTS conversations (
	id VARCHAR(32) PRIMARY KEY,
	user_id VARCHAR(32) NULL,
	agent_id VARCHAR(32) NULL,
	"default" BOOLEAN NULL,
	title VARCHAR(256) NULL,
	summary TEXT NULL,
	summarized_at TIMESTAMPTZ NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	modified_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	CONSTRAINT conversations_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
	CONSTRAINT conversations_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS conversations_user_id_agent_id_index ON conversations (user_id, agent_id);
CREATE UNIQUE INDEX IF NOT EXISTS conversations_default_unique_index ON conversations (user_id, agent_id) WHERE "default" IS TRUE;

CREATE TABLE IF NOT EXISTS conversation_messages (
	id VARCHAR(32) PRIMARY KEY,
	conversation_id VARCHAR(32) NOT NULL,
	role VARCHAR(32) NULL,
	content BYTEA NOT NULL DEFAULT ''::bytea,
	tool_calls BYTEA NOT NULL DEFAULT ''::bytea,
	usage BYTEA NOT NULL DEFAULT ''::bytea,
	metadata BYTEA NOT NULL DEFAULT ''::bytea,
	stop_reason VARCHAR(32) NULL,
	model VARCHAR(128) NULL,
	provider VARCHAR(128) NULL,
	tool_call_id VARCHAR(128) NULL,
	tool_name VARCHAR(128) NULL,
	sequence BIGINT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	modified_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	CONSTRAINT conversation_messages_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS conversation_messages_conversation_id_sequence_unique_index ON conversation_messages (conversation_id, sequence);
CREATE INDEX IF NOT EXISTS conversation_messages_conversation_id_index ON conversation_messages (conversation_id);

CREATE TABLE IF NOT EXISTS jobs (
	id VARCHAR(32) PRIMARY KEY,
	user_id VARCHAR(32) NULL,
	model VARCHAR(128) NULL,
	agent_id VARCHAR(32) NULL,
	conversation_id VARCHAR(32) NULL,
	name VARCHAR(256) NULL,
	schedule VARCHAR(128) NULL,
	prompt TEXT NULL,
	enabled BOOLEAN NULL,
	one_shot BOOLEAN NULL,
	last_status TEXT NULL,
	last_error TEXT NULL,
	run_at TIMESTAMPTZ NULL,
	last_run_at TIMESTAMPTZ NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	modified_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	CONSTRAINT jobs_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
	CONSTRAINT jobs_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE SET NULL,
	CONSTRAINT jobs_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS jobs_user_id_index ON jobs (user_id);
CREATE INDEX IF NOT EXISTS jobs_run_at_index ON jobs (run_at);

CREATE TABLE IF NOT EXISTS sessions (
	id VARCHAR(32) PRIMARY KEY,
	user_id VARCHAR(32) NULL,
	user_agent VARCHAR(256) NULL,
	remote_address VARCHAR(128) NULL,
	expires_at TIMESTAMPTZ NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	modified_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	CONSTRAINT sessions_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS sessions_user_id_index ON sessions (user_id);
CREATE INDEX IF NOT EXISTS sessions_expires_at_index ON sessions (expires_at);

CREATE TABLE IF NOT EXISTS media (
	id VARCHAR(32) PRIMARY KEY,
	user_id VARCHAR(32) NULL,
	format VARCHAR(32) NULL,
	content_type VARCHAR(128) NULL,
	source VARCHAR(32) NULL,
	source_agent_id VARCHAR(32) NULL,
	conversation_id VARCHAR(32) NULL,
	tool_name VARCHAR(128) NULL,
	tool_call_id VARCHAR(128) NULL,
	original_name VARCHAR(256) NULL,
	size BIGINT NULL,
	large_object_id OID NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	modified_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	CONSTRAINT media_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
	CONSTRAINT media_source_agent_id_fkey FOREIGN KEY (source_agent_id) REFERENCES agents(id) ON DELETE SET NULL,
	CONSTRAINT media_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS media_user_id_index ON media (user_id);
CREATE INDEX IF NOT EXISTS media_conversation_id_index ON media (conversation_id);

ALTER TABLE agents
	ADD CONSTRAINT agents_avatar_media_id_fkey FOREIGN KEY (avatar_media_id) REFERENCES media(id) ON DELETE SET NULL;

ALTER TABLE users
	ADD CONSTRAINT users_avatar_media_id_fkey FOREIGN KEY (avatar_media_id) REFERENCES media(id) ON DELETE SET NULL;

CREATE TABLE IF NOT EXISTS skills (
	id VARCHAR(32) PRIMARY KEY,
	name VARCHAR(256) NULL,
	version VARCHAR(128) NULL,
	source VARCHAR(256) NULL,
	metadata JSONB NULL,
	prompt TEXT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	modified_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
