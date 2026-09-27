package suite

import "strings"

// SameAccount reports whether an Assistant user and a sibling session
// are the same person. Two filled subjects must match (emails may differ).
// If either subject is empty, the emails must match, case-insensitively.
func SameAccount(assistantSub, assistantEmail, siblingSub, siblingEmail string) bool {
	assistantSub = strings.TrimSpace(assistantSub)
	siblingSub = strings.TrimSpace(siblingSub)
	if assistantSub != "" && siblingSub != "" {
		return assistantSub == siblingSub
	}
	a := strings.ToLower(strings.TrimSpace(assistantEmail))
	b := strings.ToLower(strings.TrimSpace(siblingEmail))
	return a != "" && a == b
}
