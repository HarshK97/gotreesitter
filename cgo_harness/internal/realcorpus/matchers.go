package realcorpus

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// FileMatcher is the shared real-corpus file-selection policy. Explicit lock
// matchers are authoritative. When the lock does not declare matchers, registry
// matchers and canonical language filenames are combined.
type FileMatcher struct {
	allowAll   bool
	extensions map[string]struct{}
	basenames  map[string]struct{}
	paths      map[string]struct{}
}

// AllowAllFiles returns a matcher that accepts every regular file. This is
// reserved for benchmark roots that are not governed by a corpus lock.
func AllowAllFiles() FileMatcher {
	return FileMatcher{allowAll: true}
}

// NewFileMatcher constructs the canonical real-corpus matcher for language.
// Non-empty explicit matchers suppress fallback and language-default matchers.
func NewFileMatcher(language string, explicit, fallback []string) FileMatcher {
	matcher := emptyFileMatcher()
	matcher.addAll(explicit)
	if !matcher.Empty() {
		return matcher
	}
	matcher.addAll(fallback)
	matcher.addLanguageDefaults(language)
	return matcher
}

// NewFileMatcherFromParts reconstructs a matcher from already classified
// matcher lists without losing exact root paths whose text has no slash.
func NewFileMatcherFromParts(extensions, basenames, paths []string) FileMatcher {
	matcher := emptyFileMatcher()
	for _, extension := range extensions {
		matcher.addExtension(extension)
	}
	for _, basename := range basenames {
		matcher.addBasename(basename)
	}
	for _, path := range paths {
		matcher.addPath(path)
	}
	return matcher
}

func emptyFileMatcher() FileMatcher {
	return FileMatcher{
		extensions: make(map[string]struct{}),
		basenames:  make(map[string]struct{}),
		paths:      make(map[string]struct{}),
	}
}

// Empty reports whether the matcher selects no files.
func (m FileMatcher) Empty() bool {
	return !m.allowAll && len(m.extensions) == 0 && len(m.basenames) == 0 && len(m.paths) == 0
}

// Matchers returns deterministic extension, basename, and exact-path lists.
func (m FileMatcher) Matchers() (extensions, basenames, paths []string) {
	extensions = sortedMatcherKeys(m.extensions)
	basenames = sortedMatcherKeys(m.basenames)
	paths = sortedMatcherKeys(m.paths)
	return extensions, basenames, paths
}

// Matches reports whether relativePath is selected by this matcher.
func (m FileMatcher) Matches(relativePath string) bool {
	if m.allowAll {
		return true
	}
	path := strings.ToLower(filepath.ToSlash(relativePath))
	if _, ok := m.paths[normalizeRelativeMatcherPath(path)]; ok {
		return true
	}
	for extension := range m.extensions {
		if strings.HasSuffix(path, extension) {
			return true
		}
	}
	return basenameMatches(strings.ToLower(filepath.Base(path)), m.basenames)
}

func (m *FileMatcher) addAll(values []string) {
	for _, value := range values {
		m.add(value)
	}
}

func (m *FileMatcher) add(value string) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return
	}
	if strings.ContainsAny(value, `/\`) {
		m.addPath(value)
		return
	}
	if strings.HasPrefix(value, ".") {
		m.addExtension(value)
		return
	}
	m.addBasename(value)
}

func (m *FileMatcher) addExtension(value string) {
	if value = strings.ToLower(strings.TrimSpace(value)); value != "" {
		m.extensions[value] = struct{}{}
	}
}

func (m *FileMatcher) addBasename(value string) {
	if value = strings.ToLower(strings.TrimSpace(value)); value != "" {
		m.basenames[value] = struct{}{}
	}
}

func (m *FileMatcher) addPath(value string) {
	if path := normalizeRelativeMatcherPath(value); path != "" {
		m.paths[path] = struct{}{}
	}
}

func (m *FileMatcher) addLanguageDefaults(language string) {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "awk":
		m.addExtension(".awk")
		m.addExtension(".gawk")
	case "caddy":
		m.addBasename("caddyfile")
	case "cmake":
		m.addExtension(".cmake")
		m.addBasename("cmakelists.txt")
	case "dockerfile":
		m.addExtension(".dockerfile")
		m.addBasename("dockerfile")
		m.addBasename("containerfile")
		for i := 1; i <= 9; i++ {
			m.addBasename(strconv.Itoa(i))
		}
	case "earthfile":
		m.addExtension(".earth")
		m.addBasename("earthfile")
	case "git_rebase":
		m.addExtension(".git-rebase-todo")
		m.addBasename("git-rebase-todo")
		m.addBasename("rebase-todo")
	case "gomod":
		m.addBasename("go.mod")
	case "kconfig":
		m.addBasename("kconfig")
		m.addBasename("kconfig.*")
	case "make":
		m.addExtension(".mk")
		m.addBasename("makefile")
	case "markdown":
		m.addExtension(".md")
		m.addBasename("readme.md")
	case "meson":
		m.addBasename("meson.build")
		m.addBasename("meson_options.txt")
	case "nginx":
		m.addExtension(".nginx")
		m.addBasename("nginx.conf")
		m.addBasename("conf.nginx")
	case "requirements":
		m.addBasename("requirements.txt")
	case "ssh_config":
		m.addBasename("ssh_config")
		m.addBasename("sshd_config")
		m.addBasename("known_hosts")
		m.addBasename("authorized_keys")
	case "tmux":
		m.addBasename("tmux.conf")
		m.addBasename(".tmux.conf")
	case "todotxt":
		m.addBasename("todo.txt")
	}
}

func basenameMatches(base string, basenames map[string]struct{}) bool {
	if _, ok := basenames[base]; ok {
		return true
	}
	for pattern := range basenames {
		if pattern == "kconfig.*" {
			if strings.HasPrefix(base, "kconfig.") && !strings.HasSuffix(base, ".txt") && !strings.HasSuffix(base, ".md") && !strings.HasSuffix(base, ".rst") {
				return true
			}
			continue
		}
		if strings.HasSuffix(pattern, "*") && strings.HasPrefix(base, strings.TrimSuffix(pattern, "*")) {
			return true
		}
	}
	return false
}

func normalizeRelativeMatcherPath(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	value = strings.ToLower(filepath.ToSlash(filepath.Clean(filepath.FromSlash(value))))
	if value == "." {
		return ""
	}
	return value
}

func sortedMatcherKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	return keys
}
