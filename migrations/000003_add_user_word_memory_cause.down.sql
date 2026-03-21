ALTER TABLE user_word_states
DROP COLUMN IF EXISTS guessed_correct_count,
DROP COLUMN IF EXISTS slow_recall_count,
DROP COLUMN IF EXISTS confusable_mixup_count,
DROP COLUMN IF EXISTS spelling_issue_count,
DROP COLUMN IF EXISTS meaning_forget_count,
DROP COLUMN IF EXISTS last_answer_correct,
DROP COLUMN IF EXISTS last_response_time_ms,
DROP COLUMN IF EXISTS last_memory_cause;
