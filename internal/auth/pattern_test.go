package auth

import "testing"

func TestMatchesAny(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		value    string
		patterns []string
		want     bool
	}{
		{name: "exact", value: "/api/v1/version", patterns: []string{"/api/v1/version"}, want: true},
		{name: "http wildcard", value: "/api/v1/public/status", patterns: []string{"/api/v1/public/*"}, want: true},
		{name: "grpc wildcard", value: "/grpc.health.v1.Health/Check", patterns: []string{"/grpc.health.v1.Health/*"}, want: true},
		{name: "does not cross slash", value: "/api/v1/public/nested/status", patterns: []string{"/api/v1/public/*"}},
		{name: "no match", value: "/api/v1/users/list", patterns: []string{"/api/v1/public/*"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := MatchesAny(test.value, test.patterns); got != test.want {
				t.Fatalf("MatchesAny() = %v, want %v", got, test.want)
			}
		})
	}
}
