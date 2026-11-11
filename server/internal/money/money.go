package money

import (
	"math"
	"strconv"
	"strings"

	"github.com/maillunex-lang/UfaCapital/server/internal/domain"
)

// Parse accepts a canonical decimal integer string. No sign, no fraction, no leading zeros.
func Parse(s string) (int64, error) {
	if s == "" || len(s) > 19 {
		return 0, domain.E(400, "BAD_AMOUNT", "сумма должна быть целой десятичной строкой")
	}
	if s[0] < '0' || s[0] > '9' {
		return 0, domain.E(400, "BAD_AMOUNT", "сумма должна быть целой десятичной строкой")
	}
	if len(s) > 1 && s[0] == '0' {
		return 0, domain.E(400, "BAD_AMOUNT", "сумма должна быть целой десятичной строкой")
	}
	if strings.ContainsAny(s, "+-.,eE ") {
		return 0, domain.E(400, "BAD_AMOUNT", "сумма должна быть целой десятичной строкой")
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0, domain.E(400, "BAD_AMOUNT", "сумма вне допустимого диапазона")
	}
	return n, nil
}

func Format(n int64) string {
	return strconv.FormatInt(n, 10)
}

func Mul(a, b int64) (int64, error) {
	if a < 0 || b < 0 {
		return 0, domain.E(400, "BAD_AMOUNT", "отрицательное количество")
	}
	if a != 0 && b > math.MaxInt64/a {
		return 0, domain.E(400, "BAD_AMOUNT", "переполнение суммы")
	}
	return a * b, nil
}

func Add(a, b int64) (int64, error) {
	if b > 0 && a > math.MaxInt64-b {
		return 0, domain.E(400, "BAD_AMOUNT", "переполнение суммы")
	}
	if b < 0 && a < math.MinInt64-b {
		return 0, domain.E(400, "BAD_AMOUNT", "переполнение суммы")
	}
	return a + b, nil
}
