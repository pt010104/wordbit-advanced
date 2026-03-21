package domain

import "strings"

type WordCommonRate string

const (
	WordCommonRateCommon WordCommonRate = "common"
	WordCommonRateFormal WordCommonRate = "formal"
	WordCommonRateRare   WordCommonRate = "rare"
)

func ParseWordCommonRate(value string) (WordCommonRate, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(WordCommonRateCommon):
		return WordCommonRateCommon, true
	case string(WordCommonRateFormal):
		return WordCommonRateFormal, true
	case string(WordCommonRateRare):
		return WordCommonRateRare, true
	default:
		return "", false
	}
}
