ALTER TABLE memories DROP CONSTRAINT IF EXISTS memories_idempotency_hash_requires_key;
ALTER TABLE memories DROP COLUMN IF EXISTS idempotency_hash;
