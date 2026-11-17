package config

import "testing"

func TestParseBytes(t *testing.T) {
	cases := map[string]int64{
		"10485760": 10485760,
		"10MiB":    10485760,
		"10MB":     10485760,
		"512KiB":   524288,
		"512KB":    524288,
		"1MiB":     1048576,
	}
	for input, want := range cases {
		got, err := parseBytes(input)
		if err != nil {
			t.Fatalf("parseBytes(%q): %v", input, err)
		}
		if got != want {
			t.Errorf("parseBytes(%q) = %d, want %d", input, got, want)
		}
	}

	for _, input := range []string{"", "0", "-5", "abc", "10GB"} {
		if _, err := parseBytes(input); err == nil {
			t.Errorf("parseBytes(%q) unexpectedly succeeded", input)
		}
	}
}
