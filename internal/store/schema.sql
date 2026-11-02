-- AI-DB server schema.
-- users + connections (Phase 0/1) and llm_profiles + user_settings (Phase 2).
-- There is no sessions table: the server uses stateless HMAC cookies. Logout
-- clears the client cookie; password changes bump users.token_version so older
-- cookies fail Gate. All tenant data carries owner_id and every query MUST filter by it.

CREATE TABLE IF NOT EXISTS users (
  id            VARCHAR(36)  NOT NULL,
  username      VARCHAR(64)  NOT NULL,
  password_hash VARCHAR(255) NOT NULL,
  role          VARCHAR(16)  NOT NULL DEFAULT 'user',
  -- display_name / avatar are self-service profile fields (login username stays fixed).
  display_name  VARCHAR(64)  NOT NULL DEFAULT '',
  avatar        MEDIUMTEXT   NULL,
  -- email / phone are optional contact fields maintained by admins.
  email         VARCHAR(128) NOT NULL DEFAULT '',
  phone         VARCHAR(32)  NOT NULL DEFAULT '',
  -- banned=1 blocks login; admin console can toggle without deleting the row.
  banned        TINYINT(1)   NOT NULL DEFAULT 0,
  -- token_version bumps on password change; session cookies carry the same ver.
  token_version INT          NOT NULL DEFAULT 0,
  created_at    DATETIME     NOT NULL,
  updated_at    DATETIME     NOT NULL,
  -- last_login_at is stamped on successful password login (nullable until first login).
  last_login_at DATETIME     NULL,
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

-- Durable query audit (server mutation gate). sql_text stores the original statement.
CREATE TABLE IF NOT EXISTS query_audit (
  id              VARCHAR(36)  NOT NULL,
  ts              DATETIME(3)  NOT NULL,
  actor_id        VARCHAR(36)  NOT NULL DEFAULT '',
  actor_name      VARCHAR(64)  NOT NULL DEFAULT '',
  actor_role      VARCHAR(16)  NOT NULL DEFAULT '',
  connection_id   VARCHAR(36)  NOT NULL DEFAULT '',
  sql_text        MEDIUMTEXT   NOT NULL,
  mutating        TINYINT(1)   NOT NULL DEFAULT 0,
  outcome         VARCHAR(32)  NOT NULL DEFAULT '',
  detail          VARCHAR(512) NOT NULL DEFAULT '',
  duration_ms     BIGINT       NOT NULL DEFAULT 0,
  mutation_id     VARCHAR(36)  NOT NULL DEFAULT '',
  PRIMARY KEY (id),
  KEY idx_qa_ts (ts),
  KEY idx_qa_actor (actor_id, ts),
  KEY idx_qa_conn (connection_id, ts)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- SQL change tickets (DBA review → execute → optional DML rollback).
CREATE TABLE IF NOT EXISTS mutation_requests (
  id              VARCHAR(36)  NOT NULL,
  created_at      DATETIME(3)  NOT NULL,
  updated_at      DATETIME(3)  NOT NULL,
  owner_id        VARCHAR(36)  NOT NULL,
  owner_name      VARCHAR(64)  NOT NULL DEFAULT '',
  connection_id   VARCHAR(36)  NOT NULL,
  conn_type       VARCHAR(16)  NOT NULL DEFAULT '',
  active_schema   VARCHAR(128) NOT NULL DEFAULT '',
  title           VARCHAR(128) NOT NULL DEFAULT '',
  remark          VARCHAR(512) NOT NULL DEFAULT '',
  demand_url      VARCHAR(512) NOT NULL DEFAULT '',
  sql_text        MEDIUMTEXT   NOT NULL,
  stmt_class      VARCHAR(16)  NOT NULL DEFAULT '',
  lint_result     VARCHAR(512) NOT NULL DEFAULT '',
  status          VARCHAR(16)  NOT NULL DEFAULT 'pending',
  rollbackable    TINYINT(1)   NOT NULL DEFAULT 0,
  before_image    LONGTEXT     NULL,
  reviewer_id     VARCHAR(36)  NOT NULL DEFAULT '',
  reviewer_name   VARCHAR(64)  NOT NULL DEFAULT '',
  review_note     VARCHAR(512) NOT NULL DEFAULT '',
  approved_at     DATETIME(3)  NULL,
  executed_at     DATETIME(3)  NULL,
  rolled_back_at  DATETIME(3)  NULL,
  PRIMARY KEY (id),
  KEY idx_mr_status (status, created_at),
  KEY idx_mr_owner (owner_id, created_at),
  CONSTRAINT fk_mr_owner FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Table-level query grants (grantee may use a connection they do not own).
CREATE TABLE IF NOT EXISTS query_grants (
  id              VARCHAR(36)  NOT NULL,
  created_at      DATETIME(3)  NOT NULL,
  grantee_id      VARCHAR(36)  NOT NULL,
  grantee_name    VARCHAR(64)  NOT NULL DEFAULT '',
  connection_id   VARCHAR(36)  NOT NULL,
  database_name   VARCHAR(128) NOT NULL,
  table_name      VARCHAR(128) NOT NULL,
  granted_by_id   VARCHAR(36)  NOT NULL DEFAULT '',
  granted_by_name VARCHAR(64)  NOT NULL DEFAULT '',
  PRIMARY KEY (id),
  UNIQUE KEY uq_grant (grantee_id, connection_id, database_name, table_name),
  KEY idx_grant_grantee (grantee_id),
  KEY idx_grant_conn (connection_id),
  CONSTRAINT fk_grant_grantee FOREIGN KEY (grantee_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Privilege application tickets (table-level query access).
CREATE TABLE IF NOT EXISTS query_privilege_requests (
  id              VARCHAR(36)  NOT NULL,
  created_at      DATETIME(3)  NOT NULL,
  updated_at      DATETIME(3)  NOT NULL,
  applicant_id    VARCHAR(36)  NOT NULL,
  applicant_name  VARCHAR(64)  NOT NULL DEFAULT '',
  connection_id   VARCHAR(36)  NOT NULL,
  database_name   VARCHAR(128) NOT NULL,
  table_name      VARCHAR(128) NOT NULL,
  remark          VARCHAR(512) NOT NULL DEFAULT '',
  status          VARCHAR(16)  NOT NULL DEFAULT 'pending',
  reviewer_id     VARCHAR(36)  NOT NULL DEFAULT '',
  reviewer_name   VARCHAR(64)  NOT NULL DEFAULT '',
  review_note     VARCHAR(512) NOT NULL DEFAULT '',
  PRIMARY KEY (id),
  KEY idx_qpr_status (status, created_at),
  KEY idx_qpr_applicant (applicant_id, created_at),
  CONSTRAINT fk_qpr_applicant FOREIGN KEY (applicant_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
