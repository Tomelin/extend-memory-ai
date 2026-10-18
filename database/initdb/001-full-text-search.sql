-- Full-text search is built into PostgreSQL. These extensions add support for
-- accent-insensitive searches and fuzzy/partial matching.
CREATE EXTENSION IF NOT EXISTS unaccent;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Portuguese stemming with accents removed before tokenization.
CREATE TEXT SEARCH CONFIGURATION portuguese_unaccent (COPY = portuguese);

ALTER TEXT SEARCH CONFIGURATION portuguese_unaccent
  ALTER MAPPING FOR hword, hword_part, word
  WITH unaccent, portuguese_stem;
