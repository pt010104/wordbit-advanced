DROP TRIGGER IF EXISTS trg_llm_generation_runs_updated_at ON llm_generation_runs;
DROP TRIGGER IF EXISTS trg_daily_learning_pool_items_updated_at ON daily_learning_pool_items;
DROP TRIGGER IF EXISTS trg_daily_learning_pools_updated_at ON daily_learning_pools;
DROP TRIGGER IF EXISTS trg_user_word_states_updated_at ON user_word_states;
DROP TRIGGER IF EXISTS trg_words_updated_at ON words;
DROP TRIGGER IF EXISTS trg_user_settings_updated_at ON user_settings;
DROP TRIGGER IF EXISTS trg_users_updated_at ON users;
DROP FUNCTION IF EXISTS set_updated_at;

DROP TABLE IF EXISTS llm_generation_runs;
DROP TABLE IF EXISTS learning_events;
DROP TABLE IF EXISTS daily_learning_pool_items;
DROP TABLE IF EXISTS daily_learning_pools;
DROP TABLE IF EXISTS user_word_states;
DROP TABLE IF EXISTS words;
DROP TABLE IF EXISTS user_settings;
DROP TABLE IF EXISTS users;
