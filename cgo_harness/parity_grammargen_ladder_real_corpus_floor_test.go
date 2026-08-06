//go:build cgo && treesitter_c_parity

package cgoharness

// TestGrammargenLadderRealCorpusParity is the regression gate that commit
// c088f6c4 ("fix(grammargen): Fix Swift optional binding vs trailing closure
// resolution", #542) shipped without. That commit added a shift/reduce
// conflict-resolution branch to grammargen/lr.go's resolveActionConflict
// ahead of the declared-conflict retention check, scoped to Swift's need. It
// also mis-resolved an ordinary, undeclared shift/reduce conflict in
// JavaScript's generated table (update_expression vs binary_expression),
// silently regressing every grammar whose tables share that ladder. Swift's
// own regression tests stayed green throughout because they never exercise
// another grammar's tables, and the existing generic
// TestGrammargenCGOParity gate stayed green too: its corpus comes from each
// grammar repository's own small test-corpus/example files, which happen
// not to exercise this ambiguity. Only large real-world source files (for
// example jQuery's source) trigger it.
//
// This test regenerates javascript and typescript directly from grammargen's
// native Go grammar definitions (no cloned grammar repo required) and
// compares the parse of real-world corpus files against the pinned C
// reference parser, node by node. Any conflict-resolution ladder change that
// silently regresses another grammar's precedence resolution — the exact
// failure mode of #542 — fails this test once real corpus is present.
//
// Real-world corpus files are intentionally not checked into the
// repository. Point GTS_GRAMMARGEN_LADDER_CORPUS_ROOT at a directory with
// <root>/javascript and <root>/typescript subdirectories (matching the
// corpus_manifest.json "javascript_real"/"typescript_real" layout;
// cgo_harness/corpus_real is the default — see cgo_harness/README.md and
// scripts/seed_real_corpus_from_lock.sh). The test skips per-language when
// no corpus is present so a bare checkout, and `go test ./...` without the
// cgo/treesitter_c_parity build tags, both stay green.
//
// testdata/grammargen_ladder_real_corpus_floors.json ratchets all four
// metrics (eligible/no_error/tree_parity/divergences), not divergences
// alone: a regression can also show up as a grammargen tree collapsing to
// HasError=true (one flat divergence) rather than many per-node structural
// mismatches, which unioned into a single tally can look numerically better
// than the previously-passing structural-mismatch shape even though the
// tree is more broken. The floor was captured with this fix applied against
// a fixed corpus; regenerate it (matching sample count and file identity)
// whenever the corpus source changes, exactly as
// testdata/grammargen_cgo_parity_floors.json already requires for
// TestGrammargenCGOParity.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammargen"
	"github.com/odvcencio/gotreesitter/grammars"
)

const (
	grammargenLadderCorpusRootEnv = "GTS_GRAMMARGEN_LADDER_CORPUS_ROOT"
	grammargenLadderMaxFiles      = 5
	grammargenLadderMaxFileBytes  = 500_000
)

type grammargenLadderGrammar struct {
	name    string
	gram    func() *grammargen.Grammar
	refLang func() *gotreesitter.Language
	exts    []string
}

var grammargenLadderGrammars = []grammargenLadderGrammar{
	{name: "javascript", gram: grammargen.JavaScriptGrammar, refLang: grammars.JavascriptLanguage, exts: []string{".js", ".mjs", ".cjs"}},
	{name: "typescript", gram: grammargen.TypeScriptGrammar, refLang: grammars.TypescriptLanguage, exts: []string{".ts"}},
}

func TestGrammargenLadderRealCorpusParity(t *testing.T) {
	floorsPath := defaultGrammargenLadderFloorsPath()
	floors, foundFloors, err := loadGrammargenCGOFloors(floorsPath)
	if err != nil {
		t.Fatalf("load floor file %s: %v", floorsPath, err)
	}

	for _, g := range grammargenLadderGrammars {
		g := g
		t.Run(g.name, func(t *testing.T) {
			files := grammargenLadderCorpusFiles(g)
			if len(files) == 0 {
				t.Skipf("no real corpus for %s under %s (set %s or run scripts/seed_real_corpus_from_lock.sh)",
					g.name, grammargenLadderCorpusRoot(), grammargenLadderCorpusRootEnv)
				return
			}

			genLang, err := grammargen.GenerateLanguage(g.gram())
			if err != nil {
				t.Fatalf("GenerateLanguage(%s): %v", g.name, err)
			}
			refLang := g.refLang()
			adaptGrammargenCGOExternalScanner(g.name, refLang, genLang)

			cLang, err := ParityCLanguage(g.name)
			if err != nil {
				if skipReason := parityReferenceSkipReason(err); skipReason != "" {
					t.Skipf("skip C reference: %s", skipReason)
					return
				}
				t.Fatalf("load C parser: %v", err)
			}
			cParser := sitter.NewParser()
			defer cParser.Close()
			if err := cParser.SetLanguage(cLang); err != nil {
				t.Fatalf("C parser SetLanguage: %v", err)
			}
			genParser := gotreesitter.NewParser(genLang)

			metrics := grammargenCGOFloorEntry{}
			for _, path := range files {
				src, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read %s: %v", path, err)
				}

				cTree := cParser.Parse(src, nil)
				if cTree == nil || cTree.RootNode() == nil {
					continue
				}
				cRoot := cTree.RootNode()
				if cRoot.HasError() {
					// Not usable as a reference; does not count toward
					// eligible samples either direction.
					cTree.Close()
					continue
				}
				metrics.Eligible++

				genTree, err := genParser.Parse(src)
				if err != nil {
					t.Fatalf("gen parse %s: %v", path, err)
				}
				genRoot := genTree.RootNode()
				if genRoot.HasError() {
					metrics.Divergences++
					t.Logf("%s: grammargen tree has an ERROR, C reference is clean", path)
					cTree.Close()
					genTree.Release()
					continue
				}
				metrics.NoError++

				var errs []grammargenCGODivergence
				compareGrammargenVsC(genRoot, genLang, cRoot, "root", &errs)
				if len(errs) == 0 {
					metrics.TreeParity++
				} else {
					metrics.Divergences += len(errs)
					shown := errs
					if len(shown) > 5 {
						shown = shown[:5]
					}
					for _, e := range shown {
						t.Logf("%s: DIVERGENCE %s", path, e.String())
					}
				}
				cTree.Close()
				genTree.Release()
			}

			if metrics.Eligible == 0 {
				t.Skip("no clean C-reference samples in corpus")
				return
			}

			if foundFloors {
				if floor, ok := floors.Metrics[g.name]; ok {
					enforceGrammargenCGORatchet(t, g.name, floor, metrics)
				}
			}

			t.Logf("grammargen-ladder real-corpus vs C: eligible=%d no_error=%d tree_parity=%d divergences=%d",
				metrics.Eligible, metrics.NoError, metrics.TreeParity, metrics.Divergences)
		})
	}
}

func grammargenLadderCorpusRoot() string {
	if raw := strings.TrimSpace(os.Getenv(grammargenLadderCorpusRootEnv)); raw != "" {
		return raw
	}
	return "corpus_real"
}

// grammargenLadderCorpusFiles returns up to grammargenLadderMaxFiles files
// for the language, largest first (largest real-world files carry the most
// operator/precedence ambiguity and are most likely to expose a ladder
// regression), skipping anything above grammargenLadderMaxFileBytes to keep
// the gate fast.
func grammargenLadderCorpusFiles(g grammargenLadderGrammar) []string {
	dir := filepath.Join(grammargenLadderCorpusRoot(), g.name)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type sized struct {
		path string
		size int64
	}
	var candidates []sized
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !grammargenLadderHasExt(entry.Name(), g.exts) {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.Size() == 0 || info.Size() > grammargenLadderMaxFileBytes {
			continue
		}
		candidates = append(candidates, sized{path: filepath.Join(dir, entry.Name()), size: info.Size()})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].size > candidates[j].size })
	if len(candidates) > grammargenLadderMaxFiles {
		candidates = candidates[:grammargenLadderMaxFiles]
	}
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, c.path)
	}
	return out
}

func grammargenLadderHasExt(name string, exts []string) bool {
	lower := strings.ToLower(name)
	for _, ext := range exts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

func defaultGrammargenLadderFloorsPath() string {
	return filepath.Join("testdata", "grammargen_ladder_real_corpus_floors.json")
}
