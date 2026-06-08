ALTER TABLE mcp_connections
	DROP COLUMN IF EXISTS oauth_client_id,
	DROP COLUMN IF EXISTS oauth_client_secret;
