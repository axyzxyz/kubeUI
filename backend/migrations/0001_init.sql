-- 0001_init.sql: 参考 schema(GORM AutoMigrate 为实际来源)。
-- 目标数据库:SQLite(默认)/ PostgreSQL。

CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      VARCHAR(64)  NOT NULL UNIQUE,
    password_hash VARCHAR(128) NOT NULL,
    role          VARCHAR(32)  NOT NULL DEFAULT 'viewer',
    disabled      BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at    DATETIME,
    updated_at    DATETIME
);

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER     NOT NULL,
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    expires_at DATETIME    NOT NULL,
    revoked    BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens(user_id);

CREATE TABLE IF NOT EXISTS clusters (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    name                 VARCHAR(128) NOT NULL UNIQUE,
    description          VARCHAR(256),
    kubeconfig_encrypted TEXT       NOT NULL, -- AES-256-GCM 密文 v1:<nonce>:<ciphertext>
    access_mode          VARCHAR(16) NOT NULL DEFAULT 'direct',
    status               VARCHAR(32) NOT NULL DEFAULT 'offline',
    version              VARCHAR(32),
    message              VARCHAR(256),
    last_transition_time DATETIME,
    created_at           DATETIME,
    updated_at           DATETIME
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id VARCHAR(64),
    user_id    INTEGER,
    username   VARCHAR(64),
    action     VARCHAR(64),
    resource   VARCHAR(128),
    cluster    VARCHAR(128),
    namespace  VARCHAR(128),
    name       VARCHAR(256),
    source_ip  VARCHAR(64),
    user_agent VARCHAR(256),
    result     VARCHAR(16), -- allow|deny
    created_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_audit_logs_user ON audit_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_cluster ON audit_logs(cluster);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created ON audit_logs(created_at);
-- 审计表只追加:不提供 UPDATE/DELETE 路径,应用层亦无对应接口。

CREATE TABLE IF NOT EXISTS roles (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        VARCHAR(32)  NOT NULL UNIQUE,
    description VARCHAR(256),
    builtin     BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at  DATETIME
);

CREATE TABLE IF NOT EXISTS role_bindings (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      VARCHAR(64)  NOT NULL,
    role_name     VARCHAR(32)  NOT NULL,
    cluster_scope VARCHAR(128) NOT NULL DEFAULT '*',
    created_at    DATETIME
);
CREATE INDEX IF NOT EXISTS idx_role_bindings_user ON role_bindings(username);

CREATE TABLE IF NOT EXISTS enroll_tokens (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    cluster    VARCHAR(128) NOT NULL,
    token_hash VARCHAR(64)  NOT NULL UNIQUE,
    expires_at DATETIME     NOT NULL,
    revoked    BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at DATETIME
);

CREATE TABLE IF NOT EXISTS issued_kubeconfigs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER      NOT NULL,
    cluster     VARCHAR(128) NOT NULL,
    token_hash  VARCHAR(64)  NOT NULL UNIQUE,
    description VARCHAR(256),
    expires_at  DATETIME     NOT NULL,
    revoked     BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at  DATETIME
);
CREATE INDEX IF NOT EXISTS idx_issued_kubeconfigs_user ON issued_kubeconfigs(user_id);
