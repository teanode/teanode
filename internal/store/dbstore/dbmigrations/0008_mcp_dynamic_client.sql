ALTER TABLE mcp_connections
	ADD COLUMN IF NOT EXISTS oauth_client_id TEXT NULL,
	ADD COLUMN IF NOT EXISTS oauth_client_secret TEXT NULL;
