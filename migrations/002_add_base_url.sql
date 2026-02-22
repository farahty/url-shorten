ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS base_url TEXT;

CREATE INDEX IF NOT EXISTS idx_links_api_key_id ON links(api_key_id);
