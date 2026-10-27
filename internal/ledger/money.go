package ledger

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var dollarsPattern = regexp.MustCompile(`^(\d+)(?:\.(\d{1,2}))?$`)

// ParseDollars converts user input like "3200", "3,200.5" or "$3,200.50" to
// cents without going through floating point. Negative amounts are rejected:
// entries carry their sign in Direction.
func ParseDollars(s string) (Amount, error) {
	clean := strings.ReplaceAll(strings.TrimPrefix(strings.TrimSpace(s), "$"), ",", "")
	m := dollarsPattern.FindStringSubmatch(clean)
	if m == nil {
		return 0, fmt.Errorf("%w: %q is not a dollar amount like 1,234.56", ErrInvalidTransaction, s)
	}
	whole, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || whole > math.MaxInt64/100-1 {
		return 0, fmt.Errorf("%w: %q is too large", ErrInvalidTransaction, s)
	}
	cents := int64(0)
	if m[2] != "" {
		cents, _ = strconv.ParseInt((m[2] + "0")[:2], 10, 64)
	}
	return Amount(whole*100 + cents), nil
}

// String formats an amount as dollars, e.g. "$1,234.56" or "-$5.00".
func (a Amount) String() string {
	sign := ""
	n := int64(a)
	if n < 0 {
		sign = "-"
		if n == math.MinInt64 {
			return "-$92,233,720,368,547,758.08"
		}
		n = -n
	}
	whole := strconv.FormatInt(n/100, 10)
	for i := len(whole) - 3; i > 0; i -= 3 {
		whole = whole[:i] + "," + whole[i:]
	}
	return fmt.Sprintf("%s$%s.%02d", sign, whole, n%100)
}
