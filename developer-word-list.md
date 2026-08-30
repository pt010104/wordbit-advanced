# Developer word list

The Default word set can use a developer-curated list instead of the LLM. Export
the maintained Google Sheet as CSV or XLSX, validate it with the importer below, then
select **Get from word set** in the Default set's settings. The scheduler
selects only unused rows whose `cefr_level` and `topic` match the user and the
day. If the curated list is short, it generates only the missing words with
the LLM and keeps accepted surplus as a reusable buffer for this source.

Start a Sheet with the exact header and sample row in
[`developer-word-list.template.csv`](developer-word-list.template.csv). One row
is one card; do not merge cells.

| Field | Required | Rules |
| --- | --- | --- |
| `word` | Yes | The displayed word, phrase, phrasal verb, or collocation. |
| `canonical_form` | No | Dictionary headword; defaults to `word`. |
| `lemma` | No | Defaults to `canonical_form`. |
| `word_family` | No | Related-family label, e.g. `allocation`. |
| `confusable_group_key` | No | Stable grouping key for variants that must not be scheduled together. Leave blank to derive it. |
| `part_of_speech` | Recommended | e.g. `noun`, `verb`, `adjective`; required when the same spelling has multiple meanings. |
| `cefr_level` | Yes | Exactly `B1`, `B2`, `C1`, or `C2`. |
| `topic` | Yes | One of the scheduler topics: `Education`, `Environment`, `Technology`, `Work/Career`, `Society`, `Health`, `Business`, `Finance`, `Communication`, `Travel`, `Science`, `Media`, `Culture`, `Law/Government`, `Psychology`, `Relationships`, `Daily Life`, or `Mixed Review/Weak`. |
| `ipa` | No | IPA pronunciation. |
| `pronunciation_hint` | No | Plain-text pronunciation help. |
| `vietnamese_meaning` | Yes | Concise Vietnamese definition. |
| `english_meaning` | Yes | Concise English definition. |
| `example_sentence_1` | Recommended | Natural sentence using the target exactly. |
| `example_sentence_2` | No | A second, meaningfully different sentence. |
| `common_rate` | No | `common`, `formal`, or `rare`. |
| `sort_order` | No | Non-negative whole number; lower rows are offered first. Rows without it follow all ordered rows. |

Import from the repository root (with `DATABASE_URL` set):

```sh
cd backend
go run ./cmd/import-developer-word-list --file /path/to/export.xlsx

# Optional legacy list metadata; per-word important_score controls selection.
go run ./cmd/import-developer-word-list --file /path/to/ielts_reading_1000_single_words.xlsx \
  --list-name ielts_reading_1000_single_words --priority 100

# Optional: validate the file without changing the database.
go run ./cmd/import-developer-word-list --file /path/to/export.xlsx --validate-only
```

The importer stops before writing if any row is invalid. It upserts by
`word + part_of_speech`, records `source_provider=developer_list`, and is safe
to rerun after editing a row. `--list-name` is stored with each card. The legacy
`--priority` value is retained as metadata but no longer controls selection.
Keep a sufficiently deep list for every topic/level combination that users may
select.

## Global importance scores

Import a two-column CSV or XLSX containing `word` and `important_score`:

```sh
go run ./cmd/import-developer-word-list \
  --score-file /path/to/ielts_word_priority_scores.xlsx
```

The score is applied by normalized spelling to all matching curated cards.
Selection combines all curated lists and offers higher scores first; list-level
priority does not affect ordering once scores are present.
