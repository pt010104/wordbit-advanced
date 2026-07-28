UPDATE daily_learning_pool_items
SET review_mode = 'hidden_meaning'
WHERE review_mode = 'definition_first';

ALTER TABLE daily_learning_pool_items
    DROP CONSTRAINT IF EXISTS daily_learning_pool_items_review_mode_check;

ALTER TABLE daily_learning_pool_items
    ADD CONSTRAINT daily_learning_pool_items_review_mode_check
    CHECK (review_mode IN (
        'hidden_meaning', 'multiple_choice', 'build_word',
        'fill_in_blank', 'listening_sentence'
    ));

ALTER TABLE word_sets
    DROP CONSTRAINT IF EXISTS word_sets_enabled_review_modes_valid;

ALTER TABLE word_sets
    ALTER COLUMN enabled_review_modes SET DEFAULT ARRAY[
        'hidden_meaning', 'multiple_choice', 'build_word',
        'fill_in_blank', 'listening_sentence'
    ]::TEXT[];

UPDATE word_sets
SET enabled_review_modes = array_remove(enabled_review_modes, 'definition_first');

ALTER TABLE word_sets
    ADD CONSTRAINT word_sets_enabled_review_modes_valid
    CHECK (
        'hidden_meaning' = ANY(enabled_review_modes)
        AND enabled_review_modes <@ ARRAY[
            'hidden_meaning', 'multiple_choice', 'build_word',
            'fill_in_blank', 'listening_sentence'
        ]::TEXT[]
    );
