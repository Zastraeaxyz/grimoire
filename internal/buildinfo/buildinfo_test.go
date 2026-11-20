package buildinfo

import "testing"

func TestVersion(t *testing.T) {
	cases := []struct {
		name    string
		version string
		sha     string
		want    string
	}{
		{name: "BUILD_VERSION wins", version: "v1.2.3", sha: "abc123", want: "v1.2.3"},
		{name: "trims whitespace", version: "  30675d0  ", want: "30675d0"},
		{name: "railway sha fallback", sha: "abc123", want: "abc123"},
		{name: "unknown when unset", want: "unknown"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BUILD_VERSION", tc.version)
			t.Setenv("RAILWAY_GIT_COMMIT_SHA", tc.sha)
			if got := Version(); got != tc.want {
				t.Fatalf("Version = %q, want %q", got, tc.want)
			}
		})
	}
}
