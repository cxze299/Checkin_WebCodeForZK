ALTER TABLE users
  ADD COLUMN email_normalized VARCHAR(255) NULL AFTER email,
  ADD COLUMN avatar_path VARCHAR(1024) NOT NULL DEFAULT '' AFTER phone,
  ADD COLUMN profile_updated_at DATETIME(3) NULL AFTER avatar_path;

UPDATE users SET email_normalized = NULLIF(LOWER(TRIM(email)), '');
UPDATE users u JOIN users older ON older.email_normalized=u.email_normalized AND older.id<u.id
SET u.email_normalized=NULL WHERE u.email_normalized IS NOT NULL;
CREATE UNIQUE INDEX uk_users_email_normalized ON users (email_normalized);

CREATE TABLE IF NOT EXISTS roster_imports (
  id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  filename VARCHAR(255) NOT NULL,
  checksum_sha256 CHAR(64) NOT NULL,
  imported_by BIGINT UNSIGNED NULL,
  row_count INT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  KEY idx_roster_import_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS roster_entries (
  id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  import_id BIGINT UNSIGNED NULL,
  group_id BIGINT UNSIGNED NOT NULL,
  canonical_name VARCHAR(128) NOT NULL,
  identity_key VARCHAR(128) NOT NULL,
  is_leader TINYINT NOT NULL DEFAULT 0,
  is_minor TINYINT NOT NULL DEFAULT 0,
  claimed_by_user_id BIGINT UNSIGNED NULL,
  status TINYINT NOT NULL DEFAULT 1,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  UNIQUE KEY uk_roster_group_identity (group_id, identity_key),
  KEY idx_roster_identity_status (identity_key, status),
  KEY idx_roster_claimed (claimed_by_user_id),
  KEY idx_roster_group_status (group_id, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
