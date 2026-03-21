ALTER TABLE words
    ADD COLUMN common_rate TEXT
    CHECK (common_rate IN ('common', 'formal', 'rare'));

