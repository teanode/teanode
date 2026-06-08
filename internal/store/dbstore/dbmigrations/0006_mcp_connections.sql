CREATE TABLE IF NOT EXISTS mcp_connections (
	id VARCHAR(32) PRIMARY KEY,
	user_id VARCHAR(32) NULL,
	server_name VARCHAR(256) NULL,
	status VARCHAR(32) NULL,
	auth_value TEXT NULL,
	last_error TEXT NULL,
	last_connected_at TIMESTAMPTZ NULL,
	created_at TIMESTAMPTZ NOT NULL,
	modified_at TIMESTAMPTZ NOT NULL,
	CONSTRAINT mcp_connections_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS mcp_connections_user_id_index ON mcp_connections (user_id);
CREATE UNIQUE INDEX IF NOT EXISTS mcp_connections_user_server_unique ON mcp_connections (user_id, server_name);
