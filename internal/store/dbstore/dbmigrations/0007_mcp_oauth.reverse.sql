DROP INDEX IF EXISTS mcp_connections_oauth_state_index;

ALTER TABLE mcp_connections
	DROP COLUMN IF EXISTS access_token,
	DROP COLUMN IF EXISTS refresh_token,
	DROP COLUMN IF EXISTS token_type,
	DROP COLUMN IF EXISTS token_expires_at,
	DROP COLUMN IF EXISTS scope,
	DROP COLUMN IF EXISTS oauth_state,
	DROP COLUMN IF EXISTS code_verifier;
