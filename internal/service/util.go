package service

// compareInts returns -1 if a < b, 0 if a == b, or 1 if a > b.
// Used for multi-key sorting comparators.
func compareInts(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
