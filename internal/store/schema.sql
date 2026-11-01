-- AI-DB server schema.
-- users + connections (Phase 0/1) and llm_profiles + user_settings (Phase 2).
-- There is no sessions table: the server uses stateless HMAC cookies, so logout
-- clears the client cookie and server-side revocation is a later phase. All
-- tenant data carries owner_id and every query MUST filter by it.

CREATE TABLE IF NOT EXISTS users (
  id            VARCHAR(36)  NOT NULL,
  username      VARCHAR(64)  NOT NULL,
  password_hash VARCHAR(255) NOT NULL,
  role          VARCHAR(16)  NOT NULL DEFAULT 'user',
  created_at    DATETIME     NOT NULL,
  updated_at    DATETIME     NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_users_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS connections (
  id          VARCHAR(36)   NOT NULL,
  owner_id    VARCHAR(36)   NOT NULL,
  name        VARCHAR(128)  NOT NULL,
  `type`      VARCHAR(16)   NOT NULL,
  host        VARCHAR(255)  NOT NULL,
  port        INT           NOT NULL,
  `user`      VARCHAR(128)  NOT NULL DEFAULT '',
  password    VARCHAR(1024) NOT NULL DEFAULT '',
  `database`  VARCHAR(128)  NOT NULL DEFAULT '',
  auth_source VARCHAR(64)   NOT NULL DEFAULT '',
  ssl_mode    VARCHAR(16)   NOT NULL DEFAULT '',
  -- DATETIME(6): Save assigns each row a per-index microsecond created_at so
  -- Load's ORDER BY created_at reproduces the saved slice order.
  created_at  DATETIME(6)   NOT NULL,
  updated_at  DATETIME(6)   NOT NULL,
  PRIMARY KEY (id),
  KEY idx_conn_owner (owner_id),
  CONSTRAINT fk_conn_owner FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS llm_profiles (
  id         VARCHAR(36)   NOT NULL,
  owner_id   VARCHAR(36)   NOT NULL,
  name       VARCHAR(128)  NOT NULL,
  base_url   VARCHAR(255)  NOT NULL,
  model      VARCHAR(128)  NOT NULL,
  api_key    VARCHAR(1024) NOT NULL DEFAULT '',   -- AES-GCM ciphertext (enc:base64…)
  -- DATETIME(6): Save stamps a per-index microsecond created_at so Load's
  -- ORDER BY created_at reproduces the saved slice order (ProfileViews order).
  created_at DATETIME(6)   NOT NULL,
  updated_at DATETIME(6)   NOT NULL,
  PRIMARY KEY (id),
  KEY idx_llm_owner (owner_id),
  CONSTRAINT fk_llm_owner FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Per-user settings: the active profile id plus the safety knobs that live on
-- AppConfig.LLM. Endpoint fields (base_url/api_key/model) are NOT stored here —
-- SyncActiveLLM derives them from the active llm_profiles row (single source of
-- truth), so only active_llm_id + the three knobs are persisted.
CREATE TABLE IF NOT EXISTS user_settings (
  owner_id        VARCHAR(36) NOT NULL,
  active_llm_id   VARCHAR(36) NOT NULL DEFAULT '',
  allow_writes    TINYINT(1)  NOT NULL DEFAULT 0,
  max_rows        INT         NOT NULL DEFAULT 500,
  max_agent_steps INT         NOT NULL DEFAULT 12,
  PRIMARY KEY (owner_id),
  CONSTRAINT fk_settings_owner FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
