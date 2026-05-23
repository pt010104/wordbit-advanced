ALTER TABLE user_word_states DROP CONSTRAINT IF EXISTS user_word_states_last_mode_check;
ALTER TABLE user_word_states ADD CONSTRAINT user_word_states_last_mode_check CHECK (last_mode IN ('', 'hidden_meaning', 'multiple_choice', 'fill_in_blank'));

ALTER TABLE daily_learning_pool_items DROP CONSTRAINT IF EXISTS daily_learning_pool_items_review_mode_check;
ALTER TABLE daily_learning_pool_items ADD CONSTRAINT daily_learning_pool_items_review_mode_check CHECK (review_mode IN ('hidden_meaning', 'multiple_choice', 'fill_in_blank'));

ALTER TABLE learning_events DROP CONSTRAINT IF EXISTS learning_events_mode_used_check;
ALTER TABLE learning_events ADD CONSTRAINT learning_events_mode_used_check CHECK (mode_used IN ('', 'hidden_meaning', 'multiple_choice', 'fill_in_blank'));
