CREATE TABLE IF NOT EXISTS memory_items (
    id                            VARCHAR(32) PRIMARY KEY,
    scope                         VARCHAR(32) NOT NULL,
    scope_id                      VARCHAR(32) NOT NULL,
    title                         TEXT NULL,
    content                       TEXT NOT NULL DEFAULT '',
    tags                          JSONB NULL,
    archived_at                   TIMESTAMP WITHOUT TIME ZONE NULL,
    created_at                    TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT NOW(),
    modified_at                   TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT NOW(),
    embedding_provider_model_name TEXT NULL,
    embedding                     JSONB NULL,
    embedded_at                   TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS memory_items_scope_scope_id_index
    ON memory_items (scope, scope_id);
CREATE INDEX IF NOT EXISTS memory_items_scope_scope_id_modified_at_index
    ON memory_items (scope, scope_id, modified_at DESC);
