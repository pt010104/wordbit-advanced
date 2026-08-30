// import-developer-word-list imports the developer-maintained CSV export into
// the shared words catalog. It intentionally has no user-facing endpoint.
package main

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"wordbit-advanced-app/backend/internal/database"
	"wordbit-advanced-app/backend/internal/domain"
	"wordbit-advanced-app/backend/internal/repository/postgres"
	"wordbit-advanced-app/backend/internal/service"
)

var requiredColumns = []string{"word", "cefr_level", "topic", "vietnamese_meaning", "english_meaning"}
var scoreRequiredColumns = []string{"word", "important_score"}

func main() {
	filePath := flag.String("file", "", "path to a developer word-list CSV or XLSX export")
	csvPath := flag.String("csv", "", "deprecated alias for --file")
	scoreFile := flag.String("score-file", "", "path to a word/important_score CSV or XLSX file")
	listName := flag.String("list-name", "developer_list", "stable name recorded for this imported list")
	priority := flag.Int("priority", 0, "deprecated list metadata; selection uses per-word important_score")
	validateOnly := flag.Bool("validate-only", false, "validate the file without writing to the database")
	flag.Parse()
	inputPath := strings.TrimSpace(*filePath)
	if inputPath == "" {
		inputPath = strings.TrimSpace(*csvPath)
	}
	scorePath := strings.TrimSpace(*scoreFile)
	if inputPath != "" && scorePath != "" {
		log.Fatal("use either --file or --score-file, not both")
	}
	if inputPath == "" && scorePath == "" {
		log.Fatal("--file is required")
	}
	if scorePath != "" {
		importPriorityScores(scorePath, *validateOnly)
		return
	}
	list := strings.TrimSpace(*listName)
	if list == "" {
		log.Fatal("--list-name must not be blank")
	}
	if *priority < 0 {
		log.Fatal("--priority must be a non-negative whole number")
	}

	rows, err := readRows(inputPath, requiredColumns)
	if err != nil {
		log.Fatal(err)
	}
	candidates := make([]domain.CandidateWord, 0, len(rows))
	for index, row := range rows {
		candidate, err := candidateFromRow(row, index+2, list, *priority)
		if err != nil {
			log.Fatal(err)
		}
		candidates = append(candidates, candidate)
	}
	if *validateOnly {
		fmt.Printf("Validated %d developer-list words for %q (priority %d).\n", len(candidates), list, *priority)
		return
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	pool, err := database.OpenPool(context.Background(), databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	words := postgres.NewRepositories(pool).Words
	for index, candidate := range candidates {
		if _, err := words.UpsertWord(context.Background(), candidate); err != nil {
			log.Fatalf("row %d: upsert word: %v", index+2, err)
		}
	}
	fmt.Printf("Imported %d developer-list words for %q (priority %d).\n", len(rows), list, *priority)
}

func importPriorityScores(inputPath string, validateOnly bool) {
	rows, err := readRows(inputPath, scoreRequiredColumns)
	if err != nil {
		log.Fatal(err)
	}
	scores := make(map[string]float64, len(rows))
	mergedDuplicates := 0
	for index, row := range rows {
		word := strings.TrimSpace(row["word"])
		if word == "" {
			log.Fatalf("row %d: word is required", index+2)
		}
		score, err := strconv.ParseFloat(strings.TrimSpace(row["important_score"]), 64)
		if err != nil || math.IsNaN(score) || math.IsInf(score, 0) || score < 0 {
			log.Fatalf("row %d: important_score must be a non-negative number", index+2)
		}
		normalized := service.NormalizeWord(word)
		if previous, exists := scores[normalized]; exists {
			mergedDuplicates++
			if previous > score {
				score = previous
			}
		}
		scores[normalized] = score
	}
	if validateOnly {
		fmt.Printf("Validated %d score rows for %d unique words; merged %d duplicate rows using the higher score.\n", len(rows), len(scores), mergedDuplicates)
		return
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	pool, err := database.OpenPool(context.Background(), databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	words := postgres.NewRepositories(pool).Words
	updatedRecords := int64(0)
	unmatched := 0
	for normalized, score := range scores {
		count, err := words.UpdateDeveloperWordImportantScore(context.Background(), normalized, score)
		if err != nil {
			log.Fatalf("update score for %q: %v", normalized, err)
		}
		updatedRecords += count
		if count == 0 {
			unmatched++
		}
	}
	fmt.Printf("Imported scores for %d unique words; updated %d developer-list records; merged %d duplicate rows; %d words unmatched.\n", len(scores), updatedRecords, mergedDuplicates, unmatched)
}

func readRows(inputPath string, required []string) ([]map[string]string, error) {
	switch strings.ToLower(filepath.Ext(inputPath)) {
	case ".csv":
		return readCSVRows(inputPath, required)
	case ".xlsx":
		return readXLSXRows(inputPath, required)
	default:
		return nil, fmt.Errorf("unsupported word-list format %q; use CSV or XLSX", filepath.Ext(inputPath))
	}
}

func readCSVRows(csvPath string, required []string) ([]map[string]string, error) {
	file, err := os.Open(csvPath)
	if err != nil {
		return nil, fmt.Errorf("open CSV: %w", err)
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.TrimLeadingSpace = true
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read CSV header: %w", err)
	}
	columns := make([]string, len(header))
	seen := map[string]struct{}{}
	for i, name := range header {
		columns[i] = strings.ToLower(strings.TrimSpace(name))
		if columns[i] == "" {
			return nil, fmt.Errorf("CSV header column %d is blank", i+1)
		}
		if _, exists := seen[columns[i]]; exists {
			return nil, fmt.Errorf("CSV header duplicates %q", columns[i])
		}
		seen[columns[i]] = struct{}{}
	}
	for _, name := range required {
		if _, exists := seen[name]; !exists {
			return nil, fmt.Errorf("CSV is missing required column %q", name)
		}
	}

	var rows []map[string]string
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read CSV row %d: %w", len(rows)+2, err)
		}
		if len(record) != len(columns) {
			return nil, fmt.Errorf("row %d has %d cells; expected %d", len(rows)+2, len(record), len(columns))
		}
		row := make(map[string]string, len(columns))
		empty := true
		for i, value := range record {
			value = strings.TrimSpace(value)
			row[columns[i]] = value
			empty = empty && value == ""
		}
		if !empty {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

type xlsxWorksheet struct {
	SheetData struct {
		Rows []xlsxRow `xml:"row"`
	} `xml:"sheetData"`
}

type xlsxRow struct {
	Cells []xlsxCell `xml:"c"`
}

type xlsxCell struct {
	Reference string `xml:"r,attr"`
	Type      string `xml:"t,attr"`
	Value     string `xml:"v"`
	Inline    struct {
		Text string `xml:"t"`
	} `xml:"is"`
}

type xlsxSharedStrings struct {
	Items []xlsxSharedString `xml:"si"`
}

type xlsxSharedString struct {
	Text string `xml:"t"`
	Runs []struct {
		Text string `xml:"t"`
	} `xml:"r"`
}

func readXLSXRows(xlsxPath string, required []string) ([]map[string]string, error) {
	archive, err := zip.OpenReader(xlsxPath)
	if err != nil {
		return nil, fmt.Errorf("open XLSX: %w", err)
	}
	defer archive.Close()

	sharedStrings, err := readXLSXSharedStrings(archive.File)
	if err != nil {
		return nil, err
	}
	worksheetFile := findXLSXFile(archive.File, "xl/worksheets/sheet1.xml")
	if worksheetFile == nil {
		return nil, fmt.Errorf("XLSX has no first worksheet")
	}
	reader, err := worksheetFile.Open()
	if err != nil {
		return nil, fmt.Errorf("open XLSX worksheet: %w", err)
	}
	defer reader.Close()
	var worksheet xlsxWorksheet
	if err := xml.NewDecoder(reader).Decode(&worksheet); err != nil {
		return nil, fmt.Errorf("parse XLSX worksheet: %w", err)
	}
	if len(worksheet.SheetData.Rows) == 0 {
		return nil, fmt.Errorf("XLSX worksheet is empty")
	}

	headers := make([]string, 0)
	for _, cell := range worksheet.SheetData.Rows[0].Cells {
		column, err := xlsxColumnIndex(cell.Reference)
		if err != nil {
			return nil, err
		}
		for len(headers) <= column {
			headers = append(headers, "")
		}
		headers[column], err = xlsxCellValue(cell, sharedStrings)
		if err != nil {
			return nil, err
		}
		headers[column] = strings.ToLower(strings.TrimSpace(headers[column]))
	}
	seen := map[string]struct{}{}
	for index, name := range headers {
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("XLSX header duplicates %q", name)
		}
		seen[name] = struct{}{}
		_ = index
	}
	for _, name := range required {
		if _, exists := seen[name]; !exists {
			return nil, fmt.Errorf("XLSX is missing required column %q", name)
		}
	}

	rows := make([]map[string]string, 0, len(worksheet.SheetData.Rows)-1)
	for rowIndex, sourceRow := range worksheet.SheetData.Rows[1:] {
		row := make(map[string]string, len(headers))
		empty := true
		for _, cell := range sourceRow.Cells {
			column, err := xlsxColumnIndex(cell.Reference)
			if err != nil {
				return nil, err
			}
			if column >= len(headers) || headers[column] == "" {
				continue
			}
			value, err := xlsxCellValue(cell, sharedStrings)
			if err != nil {
				return nil, fmt.Errorf("XLSX row %d: %w", rowIndex+2, err)
			}
			value = strings.TrimSpace(value)
			row[headers[column]] = value
			empty = empty && value == ""
		}
		if !empty {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func readXLSXSharedStrings(files []*zip.File) ([]string, error) {
	file := findXLSXFile(files, "xl/sharedStrings.xml")
	if file == nil {
		return nil, nil
	}
	reader, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open XLSX shared strings: %w", err)
	}
	defer reader.Close()
	var document xlsxSharedStrings
	if err := xml.NewDecoder(reader).Decode(&document); err != nil {
		return nil, fmt.Errorf("parse XLSX shared strings: %w", err)
	}
	values := make([]string, len(document.Items))
	for index, item := range document.Items {
		values[index] = item.Text
		for _, run := range item.Runs {
			values[index] += run.Text
		}
	}
	return values, nil
}

func findXLSXFile(files []*zip.File, name string) *zip.File {
	for _, file := range files {
		if file.Name == name {
			return file
		}
	}
	return nil
}

func xlsxCellValue(cell xlsxCell, sharedStrings []string) (string, error) {
	if cell.Type == "s" {
		index, err := strconv.Atoi(cell.Value)
		if err != nil || index < 0 || index >= len(sharedStrings) {
			return "", fmt.Errorf("invalid XLSX shared-string index %q", cell.Value)
		}
		return sharedStrings[index], nil
	}
	if cell.Type == "inlineStr" {
		return cell.Inline.Text, nil
	}
	return cell.Value, nil
}

func xlsxColumnIndex(reference string) (int, error) {
	index := 0
	letters := 0
	for _, char := range reference {
		if char >= 'A' && char <= 'Z' {
			index = index*26 + int(char-'A'+1)
			letters++
			continue
		}
		if char >= 'a' && char <= 'z' {
			index = index*26 + int(char-'a'+1)
			letters++
			continue
		}
		break
	}
	if letters == 0 {
		return 0, fmt.Errorf("invalid XLSX cell reference %q", reference)
	}
	return index - 1, nil
}

func candidateFromRow(row map[string]string, rowNumber int, listName string, listPriority int) (domain.CandidateWord, error) {
	for _, name := range requiredColumns {
		if row[name] == "" {
			return domain.CandidateWord{}, fmt.Errorf("row %d: %s is required", rowNumber, name)
		}
	}
	level := domain.CEFRLevel(strings.ToUpper(row["cefr_level"]))
	switch level {
	case domain.CEFRB1, domain.CEFRB2, domain.CEFRC1, domain.CEFRC2:
	default:
		return domain.CandidateWord{}, fmt.Errorf("row %d: cefr_level must be B1, B2, C1, or C2", rowNumber)
	}
	if !validTopic(row["topic"]) {
		return domain.CandidateWord{}, fmt.Errorf("row %d: topic is not a scheduler topic", rowNumber)
	}
	var commonRate *domain.WordCommonRate
	if value := row["common_rate"]; value != "" {
		rate, ok := domain.ParseWordCommonRate(value)
		if !ok {
			return domain.CandidateWord{}, fmt.Errorf("row %d: common_rate must be common, formal, or rare", rowNumber)
		}
		commonRate = &rate
	}
	sortOrder := rowNumber
	if value := row["sort_order"]; value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 {
			return domain.CandidateWord{}, fmt.Errorf("row %d: sort_order must be a non-negative whole number", rowNumber)
		}
		sortOrder = parsed
	}
	canonical := firstNonEmpty(row["canonical_form"], row["word"])
	lemma := firstNonEmpty(row["lemma"], canonical)
	candidate := domain.CandidateWord{
		Word:               row["word"],
		CanonicalForm:      canonical,
		Lemma:              lemma,
		WordFamily:         row["word_family"],
		ConfusableGroupKey: row["confusable_group_key"],
		PartOfSpeech:       row["part_of_speech"],
		Level:              level,
		Topic:              row["topic"],
		IPA:                row["ipa"],
		PronunciationHint:  row["pronunciation_hint"],
		VietnameseMeaning:  row["vietnamese_meaning"],
		EnglishMeaning:     row["english_meaning"],
		ExampleSentence1:   row["example_sentence_1"],
		ExampleSentence2:   row["example_sentence_2"],
		CommonRate:         commonRate,
		SourceProvider:     "developer_list",
		SourceMetadata: domain.JSONMap{
			"source":        "developer_list",
			"list_name":     listName,
			"list_priority": listPriority,
			"sort_order":    sortOrder,
		},
		NormalizedForm: service.NormalizeWord(row["word"]),
	}
	if candidate.ConfusableGroupKey == "" {
		candidate.ConfusableGroupKey = service.ConfusableGroupFor(candidate.Word, candidate.CanonicalForm, candidate.Lemma)
	}
	return candidate, nil
}

func validTopic(topic string) bool {
	switch topic {
	case "Education", "Environment", "Technology", "Work/Career", "Society", "Health", "Business", "Finance", "Communication", "Travel", "Science", "Media", "Culture", "Law/Government", "Psychology", "Relationships", "Daily Life", "Mixed Review/Weak":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
