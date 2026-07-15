ALTER TABLE users
  ADD COLUMN auth_version BIGINT UNSIGNED NOT NULL DEFAULT 1 AFTER must_change_password;
