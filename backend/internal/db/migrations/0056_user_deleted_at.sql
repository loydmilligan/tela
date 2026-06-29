-- GDPR right to erasure: stamp deleted_at on anonymised user rows so the
-- anonymisation is visible at the SQL layer without hard-deleting the row
-- (page authorship FKs must survive). is_active = 0 blocks future logins.
ALTER TABLE users ADD COLUMN deleted_at TEXT;
