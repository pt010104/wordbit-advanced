-- Word-set preferences are independent of the legacy mode column. Only the
-- Default set may receive scheduled LLM-generated words; every set keeps a
-- non-empty list of review modes with hidden_meaning (Mode 1) always present.
ALTER TABLE word_sets
    ADD COLUMN auto_generate_new_words BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN recording_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN enabled_review_modes TEXT[] NOT NULL DEFAULT ARRAY[
        'hidden_meaning', 'multiple_choice', 'build_word', 'fill_in_blank', 'listening_sentence'
    ]::TEXT[];

UPDATE word_sets
SET auto_generate_new_words = TRUE
WHERE is_default = TRUE;

ALTER TABLE word_sets
    ADD CONSTRAINT word_sets_auto_generation_default_only
        CHECK (NOT auto_generate_new_words OR is_default),
    ADD CONSTRAINT word_sets_enabled_review_modes_valid
        CHECK (
            'hidden_meaning' = ANY(enabled_review_modes)
            AND enabled_review_modes <@ ARRAY[
                'hidden_meaning', 'multiple_choice', 'build_word', 'fill_in_blank', 'listening_sentence'
            ]::TEXT[]
        );
