CREATE TABLE IF NOT EXISTS usages (
    user_id               VARCHAR(32) NOT NULL,
    provider_name         VARCHAR(128) NOT NULL,
    model_name            VARCHAR(128) NOT NULL,
    interval_type         TEXT NOT NULL CHECK (interval_type IN ('hour', 'day', 'week', 'month', 'year')),
    started_at            TIMESTAMP WITHOUT TIME ZONE NOT NULL,
    prompt_tokens         BIGINT NOT NULL DEFAULT 0,
    completion_tokens     BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens     BIGINT NOT NULL DEFAULT 0,
    total_tokens          BIGINT NOT NULL DEFAULT 0,
    request_count         BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, provider_name, model_name, interval_type, started_at)
);

CREATE INDEX idx_usages_user_interval_time
    ON usages (user_id, interval_type, started_at);

CREATE INDEX idx_usages_interval_time_model
    ON usages (interval_type, started_at, model_name);
