package eval

// PassK implements pass^k: true iff the LAST k attempts are all consecutive
// passes. This is deliberate — pass^k measures stable success, not "one lucky
// run" (that would be Pass@k).
//
// k <= 0 is invalid and returns false (documented choice: false, not an
// error). An empty attempt list returns false.
func PassK(attempts []Attempt, k int) bool {
	if k <= 0 || len(attempts) < k {
		return false
	}
	for i := len(attempts) - k; i < len(attempts); i++ {
		if !attempts[i].Passed {
			return false
		}
	}
	return true
}
