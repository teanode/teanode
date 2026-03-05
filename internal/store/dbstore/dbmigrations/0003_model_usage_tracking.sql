CREATE TABLE IF NOT EXISTS model_usage_events (
    id                    VARCHAR(32) PRIMARY KEY,
    user_id               VARCHAR(32) NOT NULL,
    conversation_id       VARCHAR(32) NOT NULL,
    message_id            VARCHAR(32) NOT NULL,
    run_id                VARCHAR(32) NOT NULL,
    provider_name         VARCHAR(128) NOT NULL,
    model_name            VARCHAR(128) NOT NULL,
    prompt_tokens         BIGINT NOT NULL DEFAULT 0,
    completion_tokens     BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens     BIGINT NOT NULL DEFAULT 0,
    total_tokens          BIGINT NOT NULL DEFAULT 0,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_model_usage_events_user_created
    ON model_usage_events (user_id, created_at);

CREATE INDEX idx_model_usage_events_conversation
    ON model_usage_events (conversation_id, created_at);

CREATE UNIQUE INDEX idx_model_usage_events_message_id
    ON model_usage_events (message_id);

CREATE TABLE IF NOT EXISTS model_usage_stat_entries (
    user_id               VARCHAR(32) NOT NULL,
    provider_name         VARCHAR(128) NOT NULL,
    model_name            VARCHAR(128) NOT NULL,
    interval_type         TEXT NOT NULL CHECK (interval_type IN ('hourly', 'daily')),
    started_at            TIMESTAMP WITHOUT TIME ZONE NOT NULL,
    prompt_tokens         BIGINT NOT NULL DEFAULT 0,
    completion_tokens     BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens     BIGINT NOT NULL DEFAULT 0,
    total_tokens          BIGINT NOT NULL DEFAULT 0,
    request_count         BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, provider_name, model_name, interval_type, started_at)
);

CREATE INDEX idx_model_usage_stat_entries_user_interval_time
    ON model_usage_stat_entries (user_id, interval_type, started_at);

CREATE INDEX idx_model_usage_stat_entries_interval_time_model
    ON model_usage_stat_entries (interval_type, started_at, model_name);
