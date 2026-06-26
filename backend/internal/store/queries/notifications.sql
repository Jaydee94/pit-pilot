-- name: InsertNotification :exec
INSERT INTO notifications (user_id, type, title, body, url, dedup_key)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (dedup_key) DO NOTHING;

-- name: ClaimPendingNotifications :many
SELECT * FROM notifications
WHERE status = 'pending'
ORDER BY created_at
FOR UPDATE SKIP LOCKED
LIMIT $1;

-- name: MarkNotificationSent :exec
UPDATE notifications SET status = 'sent', sent_at = now(), attempts = attempts + 1 WHERE id = $1;

-- name: MarkNotificationFailed :exec
UPDATE notifications SET status = 'failed', attempts = attempts + 1 WHERE id = $1;

-- name: ScanDeadlineReminders :exec
INSERT INTO notifications (user_id, type, title, body, url, dedup_key)
SELECT gm.user_id, 'deadline_soon',
       'Anmeldeschluss bald: ' || c.artist,
       'Der Anmeldeschluss für ' || c.artist || ' ist bald. Kommst du mit?',
       '/concerts/' || c.id::text,
       'deadline_soon:' || c.id::text || ':' || gm.user_id::text
FROM concerts c
JOIN group_members gm ON gm.group_id = c.group_id
LEFT JOIN rsvps r ON r.concert_id = c.id AND r.user_id = gm.user_id
WHERE c.rsvp_deadline > now() AND c.rsvp_deadline <= now() + interval '24 hours'
  AND r.user_id IS NULL
ON CONFLICT (dedup_key) DO NOTHING;

-- name: ScanConcertReminders :exec
INSERT INTO notifications (user_id, type, title, body, url, dedup_key)
SELECT r.user_id, 'concert_soon',
       c.artist || ' ist bald!',
       c.artist || ' steht kurz bevor. Bis dann!',
       '/concerts/' || c.id::text,
       'concert_soon:' || c.id::text || ':' || r.user_id::text
FROM concerts c
JOIN rsvps r ON r.concert_id = c.id AND r.status = 'yes'
WHERE c.event_at > now() AND c.event_at <= now() + interval '24 hours'
ON CONFLICT (dedup_key) DO NOTHING;

-- name: PruneOldNotifications :exec
DELETE FROM notifications
WHERE status IN ('sent','failed') AND created_at < now() - interval '30 days';
