package policy

import "testing"

func TestConfiguredDenyPaths(t *testing.T) {
	for _, tc := range []struct {
		name, rule string
		denied     bool
	}{
		{"src/main.go", "*.go", true}, {"src/key.txt", "**/key.*", true}, {"key.txt", "**/key.*", true},
		{"vendor/a/b.go", "vendor/", true}, {"src/vendor/a.go", "vendor/", true},
		{"src/vendor/a.go", "/vendor/", false}, {"vendor/a.go", "/vendor/", true},
		{"src/main.go", "src/**", true}, {"src/a/b/main.go", "src/**/main.go", true},
		{"src/main.go", "src/*/main.go", false}, {"main.go", "/main.go", true}, {"src/main.go", "/main.go", false},
		{"src/x.go", "**/[xy].go", true}, {"src/z.go", "**/[xy].go", false},
		{"src/main.go", "[", true}, {"src/main.go", "!main.go", true},
	} {
		t.Run(tc.name+"/"+tc.rule, func(t *testing.T) {
			err := CheckConfiguredPath(tc.name, []string{tc.rule})
			if (err != nil) != tc.denied {
				t.Fatalf("denied=%t err=%v", tc.denied, err)
			}
		})
	}
	if CheckConfiguredPath(".env", nil) == nil {
		t.Fatal("custom rules widened baseline")
	}
}
