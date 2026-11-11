package money

import "testing"

func TestParse(t *testing.T) {
	n, err := Parse("100000")
	if err != nil || n != 100000 {
		t.Fatalf("parse: %v %d", err, n)
	}
	for _, bad := range []string{"", "-1", "+1", "01", "1.2", "1e2", "9223372036854775808", " 1"} {
		if _, err := Parse(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	if _, err := Mul(1<<62, 4); err == nil {
		t.Fatal("overflow accepted")
	}
	if v, err := Mul(20, 30); err != nil || v != 600 {
		t.Fatalf("mul %v %d", err, v)
	}
}
