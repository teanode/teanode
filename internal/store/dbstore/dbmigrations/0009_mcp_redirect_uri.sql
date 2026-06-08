ALTER TABLE mcp_connections
	ADD COLUMN IF NOT EXISTS oauth_redirect_uri TEXT NULL;
