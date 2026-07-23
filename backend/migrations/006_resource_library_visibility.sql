CREATE TABLE IF NOT EXISTS resource_library_visibility (
  id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  group_id BIGINT UNSIGNED NOT NULL,
  resource_key VARCHAR(512) NOT NULL,
  visible TINYINT NOT NULL DEFAULT 0,
  updated_by BIGINT UNSIGNED NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  UNIQUE KEY uk_group_resource (group_id, resource_key),
  KEY idx_group_visible (group_id, visible)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
