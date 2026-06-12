-- 0035_attachment_uploads.sql — staging records for the signed-PUT upload
-- handshake (request_attachment_upload → PUT /api/uploads/{token} →
-- confirm_attachment_upload). The handshake lets an MCP agent upload a file
-- out-of-band so the bytes never ride through the model's context (the tier
-- above the inline-base64 upload_attachment, which is capped at 5 MiB).
--
-- The PUT token is a stateless HMAC (no lookup needed to authorize the write);
-- this row exists only to map the opaque upload_id → the stored space_file, so
-- confirm_attachment_upload can return the ref on hosts that can't read the PUT
-- response. Rows are tiny and swept opportunistically (>24h) on the next
-- request, so no cron is needed.
CREATE TABLE attachment_uploads (
    id            BIGSERIAL PRIMARY KEY,
    upload_id     TEXT   NOT NULL UNIQUE,
    page_id       BIGINT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
    space_file_id BIGINT REFERENCES space_files(id) ON DELETE SET NULL,
    created_at    TEXT   NOT NULL DEFAULT tela_now(),
    completed_at  TEXT,  -- the PUT stored the bytes
    confirmed_at  TEXT   -- confirm_attachment_upload returned the ref
);

CREATE INDEX attachment_uploads_created ON attachment_uploads (created_at);
