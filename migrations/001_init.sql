CREATE TABLE IF NOT EXISTS api_keys (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_hash    TEXT NOT NULL UNIQUE,
    app_name    TEXT NOT NULL,
    is_active   BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE SEQUENCE IF NOT EXISTS link_id_seq START 10000;

CREATE TABLE IF NOT EXISTS links (
    id          BIGINT PRIMARY KEY DEFAULT nextval('link_id_seq'),
    code        TEXT NOT NULL UNIQUE,
    original_url TEXT NOT NULL,
    is_alias    BOOLEAN NOT NULL DEFAULT false,
    expires_at  TIMESTAMPTZ,
    click_count BIGINT NOT NULL DEFAULT 0,
    api_key_id  UUID NOT NULL REFERENCES api_keys(id),
    og_title    TEXT,
    og_desc     TEXT,
    og_image    TEXT,
    og_site     TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_links_code ON links(code);
CREATE INDEX IF NOT EXISTS idx_links_expires_at ON links(expires_at) WHERE expires_at IS NOT NULL;
