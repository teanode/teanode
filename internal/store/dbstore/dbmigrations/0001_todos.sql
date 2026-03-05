CREATE TABLE IF NOT EXISTS todos (
    id VARCHAR(32) PRIMARY KEY,
    project_id VARCHAR(32) NULL,
    conversation_id VARCHAR(32) NULL,
    title VARCHAR(512) NOT NULL,
    description TEXT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'open',
    priority VARCHAR(16) NOT NULL DEFAULT 'medium',
    tags JSONB NOT NULL DEFAULT '[]'::jsonb,
    completed_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    modified_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT todos_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
    CONSTRAINT todos_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE,
    CONSTRAINT todos_scope_check CHECK (
        (project_id IS NOT NULL AND conversation_id IS NULL)
        OR (project_id IS NULL AND conversation_id IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS todos_project_id_index ON todos (project_id) WHERE project_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS todos_conversation_id_index ON todos (conversation_id) WHERE conversation_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS todos_project_id_status_index ON todos (project_id, status) WHERE project_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS todos_conversation_id_status_index ON todos (conversation_id, status) WHERE conversation_id IS NOT NULL;
