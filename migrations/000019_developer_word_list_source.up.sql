ALTER TABLE word_sets
    ADD COLUMN new_word_source TEXT NOT NULL DEFAULT 'llm'
    CHECK (new_word_source IN ('llm', 'developer_list'));

COMMENT ON COLUMN word_sets.new_word_source IS
    'Source for scheduled Default-set words. developer_list reads only words curated by developers.';
