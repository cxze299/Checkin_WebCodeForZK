UPDATE checkin_records AS older
JOIN checkin_records AS newer
  ON newer.group_id = older.group_id
 AND newer.user_id = older.user_id
 AND newer.task_id = older.task_id
 AND newer.logical_date = older.logical_date
 AND newer.deleted_at IS NULL
 AND newer.id > older.id
SET older.deleted_at = NOW(3),
    older.active_key = older.id,
    older.updated_at = NOW(3)
WHERE older.task_id IS NOT NULL
  AND older.deleted_at IS NULL;

ALTER TABLE checkin_records
  ADD UNIQUE KEY uk_one_active_task_checkin (group_id, user_id, task_id, logical_date, active_key);
