ALTER TABLE word_sets
    DROP CONSTRAINT IF EXISTS word_sets_enabled_review_modes_valid,
    DROP CONSTRAINT IF EXISTS word_sets_auto_generation_default_only,
    DROP COLUMN IF EXISTS enabled_review_modes,
    DROP COLUMN IF EXISTS auto_generate_new_words;
