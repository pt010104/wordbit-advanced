ALTER TABLE user_word_states
ADD COLUMN last_memory_cause TEXT CHECK (
    last_memory_cause IS NULL OR last_memory_cause IN (
        'forgot_meaning',
        'spelling_issue',
        'mixed_up_word',
        'slow_recall',
        'guessed_correct'
    )
),
ADD COLUMN last_response_time_ms INTEGER NOT NULL DEFAULT 0,
ADD COLUMN last_answer_correct BOOLEAN,
ADD COLUMN meaning_forget_count INTEGER NOT NULL DEFAULT 0,
ADD COLUMN spelling_issue_count INTEGER NOT NULL DEFAULT 0,
ADD COLUMN confusable_mixup_count INTEGER NOT NULL DEFAULT 0,
ADD COLUMN slow_recall_count INTEGER NOT NULL DEFAULT 0,
ADD COLUMN guessed_correct_count INTEGER NOT NULL DEFAULT 0;
