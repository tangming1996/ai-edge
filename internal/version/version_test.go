package version_test

import (
	"strings"
	"testing"

	"github.com/edgeai-platform/ai-edge/internal/version"
)

// withVars swaps the package-level version vars for the duration of a test
// and restores them afterwards. This lets us exercise String/ShouldPrint/
// EffectiveVersion without touching build flags.
func withVars(t *testing.T, v, c, bd string) {
	t.Helper()
	origV, origC, origBD := version.Version, version.Commit, version.BuildDate
	version.Version = v
	version.Commit = c
	version.BuildDate = bd
	t.Cleanup(func() {
		version.Version = origV
		version.Commit = origC
		version.BuildDate = origBD
	})
}

func TestString(t *testing.T) {
	withVars(t, "1.2.3", "abc1234", "2024-01-01T00:00:00Z")
	got := version.String()
	for _, want := range []string{"version=1.2.3", "commit=abc1234", "build_date=2024-01-01T00:00:00Z"} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q missing %q", got, want)
		}
	}
}

func TestString_Defaults(t *testing.T) {
	// When the package-level vars are at their zero values (e.g. no ldflags
	// injection), the output still contains all three fields.
	withVars(t, "", "", "")
	got := version.String()
	for _, want := range []string{"version=", "commit=", "build_date="} {
		if !strings.Contains(got, want) {
			t.Errorf("default String() = %q missing %q", got, want)
		}
	}
}

func TestInfo(t *testing.T) {
	withVars(t, "v9", "c9", "d9")
	got := version.Info("edgectl")
	if !strings.HasPrefix(got, "edgectl ") {
		t.Errorf("Info should start with the program name; got %q", got)
	}
	if !strings.Contains(got, "version=v9") {
		t.Errorf("Info missing version: %q", got)
	}
}

func TestShouldPrint(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"version", []string{"version"}, true},
		{"--version", []string{"--version"}, true},
		{"-version", []string{"-version"}, true},
		{"with surrounding whitespace", []string{"  version  "}, true},
		{"non-matching flag", []string{"--help"}, false},
		{"empty", []string{}, false},
		{"nil", nil, false},
		{"version in middle", []string{"run", "version", "now"}, true},
		{"unrelated args", []string{"a", "b", "c"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := version.ShouldPrint(tc.args)
			if got != tc.want {
				t.Fatalf("ShouldPrint(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestEffectiveVersion(t *testing.T) {
	cases := []struct {
		name     string
		ver      string
		fallback string
		want     string
	}{
		{
			name:     "real version wins over fallback",
			ver:      "1.2.3",
			fallback: "0.0.0-fallback",
			want:     "1.2.3",
		},
		{
			name:     "dev version falls back to fallback",
			ver:      "dev",
			fallback: "edgeai-2024.01",
			want:     "edgeai-2024.01",
		},
		{
			name:     "dev version with empty fallback returns dev",
			ver:      "dev",
			fallback: "",
			want:     "dev",
		},
		{
			name:     "empty version with non-empty fallback returns dev (current Version)",
			ver:      "",
			fallback: "edgeai-2024.01",
			want:     "edgeai-2024.01",
		},
		{
			name:     "empty version and empty fallback returns dev (current Version)",
			ver:      "",
			fallback: "",
			want:     "",
		},
		{
			name: "whitespace-only version falls through to fallback",
			// EffectiveVersion uses TrimSpace to decide emptiness, so a
			// whitespace-only value behaves like an unset version.
			ver:      "   ",
			fallback: "fb",
			want:     "fb",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withVars(t, tc.ver, "c", "d")
			got := version.EffectiveVersion(tc.fallback)
			if got != tc.want {
				t.Fatalf("EffectiveVersion(%q) = %q, want %q", tc.fallback, got, tc.want)
			}
		})
	}
}
