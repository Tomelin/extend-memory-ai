ALTER TABLE memories ADD COLUMN idempotency_hash bytea;

ALTER TABLE memories ADD CONSTRAINT memories_idempotency_hash_requires_key
  CHECK (idempotency_hash IS NULL OR idempotency_key IS NOT NULL);
