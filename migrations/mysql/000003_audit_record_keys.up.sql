CREATE TABLE audit_record_keys (
    id CHAR(36) PRIMARY KEY,
    occurred_at DATETIME(6) NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    created_by TEXT NOT NULL,
    updated_by TEXT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
