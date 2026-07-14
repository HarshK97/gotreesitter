package realcorpus

import "testing"

func TestFileMatcherAddsCanonicalNamesToRegistryFallback(t *testing.T) {
	tests := map[string][]string{
		"awk":        {"script.awk", "script.gawk"},
		"cmake":      {"CMakeLists.txt", "cmake/modules/a.cmake"},
		"dockerfile": {"Dockerfile", "images/3", "build.dockerfile"},
		"git_rebase": {"git-rebase-todo", "test/highlight/a.git-rebase-todo"},
		"gomod":      {"go.mod", "internal/tool/go.mod"},
		"kconfig":    {"Kconfig", "arch/Kconfig.board"},
		"make":       {"Makefile", "rules.mk"},
		"markdown":   {"README.md"},
		"ssh_config": {"ssh_config", "test/authorized_keys"},
	}
	for language, paths := range tests {
		t.Run(language, func(t *testing.T) {
			matcher := NewFileMatcher(language, nil, nil)
			for _, path := range paths {
				if !matcher.Matches(path) {
					t.Errorf("canonical matcher rejected %q", path)
				}
			}
		})
	}
}

func TestFileMatcherTreatsExplicitLockMatchersAsAuthoritative(t *testing.T) {
	matcher := NewFileMatcher("gomod", []string{"fixtures/input.txt"}, []string{".mod"})
	if !matcher.Matches("fixtures/input.txt") {
		t.Fatal("explicit path matcher did not match")
	}
	for _, path := range []string{"go.mod", "nested/input.txt", "other.mod"} {
		if matcher.Matches(path) {
			t.Errorf("explicit lock matcher unexpectedly allowed %q", path)
		}
	}
}

func TestFileMatcherKconfigWildcardRejectsDocumentation(t *testing.T) {
	matcher := NewFileMatcher("kconfig", nil, nil)
	for _, path := range []string{"Kconfig.txt", "Kconfig.md", "Kconfig.rst"} {
		if matcher.Matches(path) {
			t.Errorf("kconfig wildcard unexpectedly allowed %q", path)
		}
	}
}

func TestFileMatcherReturnsDeterministicMatcherLists(t *testing.T) {
	matcher := NewFileMatcher("", []string{"ZED", ".z", "dir\\input.z", ".a", "alpha"}, nil)
	extensions, basenames, paths := matcher.Matchers()
	if got, want := joinMatchers(extensions), ".a,.z"; got != want {
		t.Fatalf("extensions = %q, want %q", got, want)
	}
	if got, want := joinMatchers(basenames), "alpha,zed"; got != want {
		t.Fatalf("basenames = %q, want %q", got, want)
	}
	if got, want := joinMatchers(paths), "dir/input.z"; got != want {
		t.Fatalf("paths = %q, want %q", got, want)
	}
}

func joinMatchers(values []string) string {
	if len(values) == 0 {
		return ""
	}
	result := values[0]
	for _, value := range values[1:] {
		result += "," + value
	}
	return result
}
