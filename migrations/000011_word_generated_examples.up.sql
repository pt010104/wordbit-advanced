ALTER TABLE words ADD COLUMN IF NOT EXISTS generated_examples JSONB NOT NULL DEFAULT '[]'::jsonb;
