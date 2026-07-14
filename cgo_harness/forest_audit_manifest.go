package cgoharness

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/gotreesitter/cgo_harness/internal/realcorpus"
)

func MaterializeForestCorpusManifest(options ForestCorpusMaterializeOptions) (ForestCorpusManifest, error) {
	var manifest ForestCorpusManifest
	if err := forestManifestRevision(options.GotreesitterRevision); err != nil {
		return manifest, err
	}
	if strings.TrimSpace(options.CorpusLockPath) == "" {
		return manifest, fmt.Errorf("corpus lock path is required")
	}
	lockPath, err := filepath.Abs(strings.TrimSpace(options.CorpusLockPath))
	if err != nil {
		return manifest, fmt.Errorf("resolve corpus lock: %w", err)
	}
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		return manifest, fmt.Errorf("read corpus lock: %w", err)
	}
	entries, err := parseForestCorpusLock(lockPath, lockData)
	if err != nil {
		return manifest, err
	}
	if strings.TrimSpace(options.CorpusRoot) == "" {
		return manifest, fmt.Errorf("corpus root is required")
	}
	root, err := filepath.Abs(strings.TrimSpace(options.CorpusRoot))
	if err != nil {
		return manifest, fmt.Errorf("resolve corpus root: %w", err)
	}
	selection, err := normalizeForestCorpusSelection(options.Selection)
	if err != nil {
		return manifest, err
	}
	languages, err := selectForestCorpusLanguages(entries, options.Languages)
	if err != nil {
		return manifest, err
	}

	manifest = ForestCorpusManifest{
		Schema:               ForestCorpusManifestSchema,
		GotreesitterRevision: options.GotreesitterRevision,
		CorpusLock: ForestCorpusManifestLock{
			Path:   filepath.Base(lockPath),
			SHA256: fmt.Sprintf("%x", sha256.Sum256(lockData)),
		},
		Selection: selection,
	}
	for _, language := range languages {
		entry := entries[language]
		if err := verifyForestCorpusCheckout(root, entry); err != nil {
			return ForestCorpusManifest{}, err
		}
		matcher := realcorpus.NewFileMatcher(language, entry.Matchers, options.RegistryExtensions[language])
		files, err := selectForestCorpusFiles(root, entry, matcher, selection)
		if err != nil {
			return ForestCorpusManifest{}, err
		}
		manifest.Files = append(manifest.Files, files...)
	}
	sort.Slice(manifest.Files, func(i, j int) bool {
		if manifest.Files[i].Language != manifest.Files[j].Language {
			return manifest.Files[i].Language < manifest.Files[j].Language
		}
		return manifest.Files[i].Path < manifest.Files[j].Path
	})
	return manifest, nil
}

func LoadForestCorpusManifest(manifestPath, corpusRoot, corpusLockPath, expectedRevision, expectedLockSHA256 string) (ForestCorpusManifest, map[string][]string, error) {
	manifest, err := decodeForestCorpusManifest(manifestPath)
	if err != nil {
		return ForestCorpusManifest{}, nil, err
	}
	if err := validateForestExecutingRevision(manifest.GotreesitterRevision, expectedRevision); err != nil {
		return manifest, nil, err
	}
	root, entries, err := authenticateForestCorpusLock(manifest, corpusRoot, corpusLockPath, expectedLockSHA256)
	if err != nil {
		return manifest, nil, err
	}
	files, err := authenticateForestCorpusManifestFiles(manifest, root, entries)
	if err != nil {
		return manifest, nil, err
	}
	return manifest, files, nil
}

func decodeForestCorpusManifest(manifestPath string) (ForestCorpusManifest, error) {
	var manifest ForestCorpusManifest
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return manifest, fmt.Errorf("read forest corpus manifest: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, fmt.Errorf("decode forest corpus manifest: %w", err)
	}
	if err := forestRequireJSONEOF(decoder); err != nil {
		return manifest, err
	}
	if err := validateForestCorpusManifestMetadata(manifest); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func validateForestExecutingRevision(manifestRevision, expectedRevision string) error {
	if err := forestManifestRevision(expectedRevision); err != nil {
		return fmt.Errorf("executing %w", err)
	}
	if manifestRevision != expectedRevision {
		return fmt.Errorf("forest corpus manifest gotreesitter revision %s, executing %s", manifestRevision, expectedRevision)
	}
	return nil
}

func authenticateForestCorpusLock(manifest ForestCorpusManifest, corpusRoot, corpusLockPath, expectedLockSHA256 string) (string, map[string]forestCorpusLockEntry, error) {
	if strings.TrimSpace(corpusRoot) == "" {
		return "", nil, fmt.Errorf("forest corpus root is required")
	}
	root, err := filepath.Abs(corpusRoot)
	if err != nil {
		return "", nil, fmt.Errorf("resolve forest corpus root: %w", err)
	}
	if strings.TrimSpace(corpusLockPath) == "" {
		return "", nil, fmt.Errorf("forest corpus lock is required")
	}
	lockPath, err := filepath.Abs(corpusLockPath)
	if err != nil {
		return "", nil, fmt.Errorf("resolve forest corpus lock: %w", err)
	}
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		return "", nil, fmt.Errorf("read authenticated corpus lock: %w", err)
	}
	if err := verifyForestCorpusLockDigests(manifest.CorpusLock.SHA256, expectedLockSHA256, lockData); err != nil {
		return "", nil, err
	}
	entries, err := parseForestCorpusLock(lockPath, lockData)
	if err != nil {
		return "", nil, err
	}
	return root, entries, nil
}

func verifyForestCorpusLockDigests(manifestDigest, expectedDigest string, lockData []byte) error {
	manifestDigest, err := forestManifestDigest("corpus lock", manifestDigest)
	if err != nil {
		return err
	}
	expectedDigest, err = forestManifestDigest("expected corpus lock", expectedDigest)
	if err != nil {
		return err
	}
	actualDigest := fmt.Sprintf("%x", sha256.Sum256(lockData))
	if manifestDigest != expectedDigest || actualDigest != manifestDigest {
		return fmt.Errorf("forest corpus lock sha256 manifest=%s actual=%s expected=%s", manifestDigest, actualDigest, expectedDigest)
	}
	return nil
}

func authenticateForestCorpusManifestFiles(manifest ForestCorpusManifest, root string, entries map[string]forestCorpusLockEntry) (map[string][]string, error) {
	verifiedCheckouts := make(map[string]bool)
	filesByLanguage := make(map[string][]string)
	for i, file := range manifest.Files {
		filePath, err := authenticateForestCorpusManifestFile(root, file, entries, verifiedCheckouts)
		if err != nil {
			return nil, fmt.Errorf("forest corpus manifest file %d: %w", i, err)
		}
		filesByLanguage[file.Language] = append(filesByLanguage[file.Language], filePath)
	}
	for language := range filesByLanguage {
		sort.Strings(filesByLanguage[language])
	}
	return filesByLanguage, nil
}

func authenticateForestCorpusManifestFile(root string, file ForestCorpusManifestFile, entries map[string]forestCorpusLockEntry, verifiedCheckouts map[string]bool) (string, error) {
	entry, ok := entries[file.Language]
	if !ok {
		return "", fmt.Errorf("language %q absent from corpus lock", file.Language)
	}
	if file.Repository != entry.Repository || file.Revision != entry.Revision {
		return "", fmt.Errorf("source identity %s@%s, lock has %s@%s", file.Repository, file.Revision, entry.Repository, entry.Revision)
	}
	if !verifiedCheckouts[file.Language] {
		if err := verifyForestCorpusCheckout(root, entry); err != nil {
			return "", err
		}
		verifiedCheckouts[file.Language] = true
	}
	languageRoot := filepath.Join(root, file.Language)
	filePath, err := forestManifestPath(languageRoot, "corpus file", file.Path)
	if err != nil {
		return "", err
	}
	digest, err := forestManifestDigest("corpus file", file.SHA256)
	if err != nil {
		return "", err
	}
	if err := forestVerifyManifestFile(filePath, file.Bytes, digest, file.Language+"/"+file.Path); err != nil {
		return "", err
	}
	return filePath, nil
}

func WriteForestCorpusManifest(path string, manifest ForestCorpusManifest) error {
	if err := validateForestCorpusManifestMetadata(manifest); err != nil {
		return err
	}
	return writeForestAuditJSON(path, manifest)
}
