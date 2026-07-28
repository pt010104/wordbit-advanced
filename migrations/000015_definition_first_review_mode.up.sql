-- Mode 6 shows a definition first, then asks the learner to reveal the word.
ALTER TABLE daily_learning_pool_items
    DROP CONSTRAINT IF EXISTS daily_learning_pool_items_review_mode_check;

ALTER TABLE daily_learning_pool_items
    ADD CONSTRAINT daily_learning_pool_items_review_mode_check
    CHECK (review_mode IN (
        'hidden_meaning', 'multiple_choice', 'build_word',
        'fill_in_blank', 'listening_sentence', 'definition_first'
    ));

ALTER TABLE word_sets
    DROP CONSTRAINT IF EXISTS word_sets_enabled_review_modes_valid;

ALTER TABLE word_sets
    ALTER COLUMN enabled_review_modes SET DEFAULT ARRAY[
        'hidden_meaning', 'multiple_choice', 'build_word',
        'fill_in_blank', 'listening_sentence', 'definition_first'
    ]::TEXT[];

-- Existing Default sets and sets still using the old all-five default gain
-- Mode 6. A set with deliberately customised modes keeps that choice.
UPDATE word_sets
SET enabled_review_modes = array_append(enabled_review_modes, 'definition_first')
WHERE NOT ('definition_first' = ANY(enabled_review_modes))
  AND (
      is_default
      OR enabled_review_modes = ARRAY[
          'hidden_meaning', 'multiple_choice', 'build_word',
          'fill_in_blank', 'listening_sentence'
      ]::TEXT[]
  );

ALTER TABLE word_sets
    ADD CONSTRAINT word_sets_enabled_review_modes_valid
    CHECK (
        'hidden_meaning' = ANY(enabled_review_modes)
        AND enabled_review_modes <@ ARRAY[
            'hidden_meaning', 'multiple_choice', 'build_word',
            'fill_in_blank', 'listening_sentence', 'definition_first'
        ]::TEXT[]
    );
