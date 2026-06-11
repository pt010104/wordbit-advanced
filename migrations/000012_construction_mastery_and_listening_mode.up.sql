ALTER TABLE user_word_states
ADD COLUMN IF NOT EXISTS word_construction_success_streak INTEGER NOT NULL DEFAULT 0 CHECK (word_construction_success_streak >= 0),
ADD COLUMN IF NOT EXISTS word_construction_struggle_count INTEGER NOT NULL DEFAULT 0 CHECK (word_construction_struggle_count >= 0);

ALTER TABLE user_word_states DROP CONSTRAINT IF EXISTS user_word_states_last_mode_check;
ALTER TABLE user_word_states ADD CONSTRAINT user_word_states_last_mode_check CHECK (last_mode IN ('', 'hidden_meaning', 'multiple_choice', 'build_word', 'fill_in_blank', 'listening_sentence'));

ALTER TABLE daily_learning_pool_items DROP CONSTRAINT IF EXISTS daily_learning_pool_items_review_mode_check;
ALTER TABLE daily_learning_pool_items ADD CONSTRAINT daily_learning_pool_items_review_mode_check CHECK (review_mode IN ('hidden_meaning', 'multiple_choice', 'build_word', 'fill_in_blank', 'listening_sentence'));

ALTER TABLE learning_events DROP CONSTRAINT IF EXISTS learning_events_mode_used_check;
ALTER TABLE learning_events ADD CONSTRAINT learning_events_mode_used_check CHECK (mode_used IN ('', 'hidden_meaning', 'multiple_choice', 'build_word', 'fill_in_blank', 'listening_sentence'));

ALTER TABLE daily_dynamic_review_prompts DROP CONSTRAINT IF EXISTS daily_dynamic_review_prompts_review_mode_check;
ALTER TABLE daily_dynamic_review_prompts ADD CONSTRAINT daily_dynamic_review_prompts_review_mode_check CHECK (review_mode IN ('multiple_choice', 'fill_in_blank', 'listening_sentence'));
