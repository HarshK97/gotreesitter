package gotreesitter_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/odvcencio/gotreesitter/internal/benchfixtures"
)

const (
	workCountFixtureID       = "query_compile"
	workCountSourcePathEnv   = "GTS_WORK_COUNT_SOURCE"
	workCountResultPathEnv   = "GTS_WORK_COUNT_RESULT"
	workCountGoChildSchema   = "gts-work-count-go-child/v3"
	workCountAdmissionEngine = "go-production-glr-untagged"
	workCountTaggedEngine    = "go-production-glr-tagged-diagnostic"
)

type workCountGoChildResult struct {
	Schema            string `json:"schema"`
	Engine            string `json:"engine"`
	Fixture           string `json:"fixture"`
	SourceSHA256      string `json:"source_sha256"`
	SourceBytes       uint32 `json:"source_bytes"`
	GrammarCommit     string `json:"grammar_commit"`
	GrammarBlobSHA256 string `json:"grammar_blob_sha256"`
	DigestFormat      string `json:"digest_format"`
	DeepTreeSHA256    string `json:"deep_tree_sha256"`
	RootEndByte       uint32 `json:"root_end_byte"`
	RootHasError      bool   `json:"root_has_error"`
	MaxStacksSeen     int    `json:"max_stacks_seen"`
	MultiStackIters   int    `json:"multi_stack_iterations"`
	MultiStackTokens  uint64 `json:"multi_stack_tokens"`
}

type workCountChildInput struct {
	fixture benchfixtures.LoadedFixture
	source  []byte
	lang    *gotreesitter.Language
	blobSHA string
}

func loadWorkCountChildInput() (workCountChildInput, error) {
	sourcePath := os.Getenv(workCountSourcePathEnv)
	if sourcePath == "" {
		return workCountChildInput{}, fmt.Errorf("%s is empty", workCountSourcePathEnv)
	}
	fixtures, err := benchfixtures.LoadGoFullParseFixtures()
	if err != nil {
		return workCountChildInput{}, fmt.Errorf("load frozen fixtures: %w", err)
	}
	var fixture benchfixtures.LoadedFixture
	found := false
	for _, candidate := range fixtures {
		if candidate.Fixture.ID == workCountFixtureID {
			fixture = candidate
			found = true
			break
		}
	}
	if !found {
		return workCountChildInput{}, fmt.Errorf("fixture %s is absent", workCountFixtureID)
	}
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		return workCountChildInput{}, fmt.Errorf("read source: %w", err)
	}
	if err := fixture.Fixture.VerifySource(source); err != nil {
		return workCountChildInput{}, err
	}
	blob := grammars.BlobByName("go")
	if len(blob) == 0 {
		return workCountChildInput{}, fmt.Errorf("embedded Go grammar blob is empty")
	}
	blobHash := sha256.Sum256(blob)
	blobSHA := hex.EncodeToString(blobHash[:])
	if err := benchfixtures.VerifyGoGrammarIdentity(benchfixtures.GoGrammarCommit, blobSHA); err != nil {
		return workCountChildInput{}, err
	}
	lang := grammars.GoLanguage()
	if lang == nil {
		return workCountChildInput{}, fmt.Errorf("load embedded Go language: nil")
	}
	return workCountChildInput{fixture: fixture, source: source, lang: lang, blobSHA: blobSHA}, nil
}

func authenticateWorkCountTree(input workCountChildInput, tree *gotreesitter.Tree) (workCountGoChildResult, error) {
	if tree == nil || tree.RootNode() == nil {
		return workCountGoChildResult{}, fmt.Errorf("parse returned no root")
	}
	root := tree.RootNode()
	if root.EndByte() != uint32(len(input.source)) || root.HasError() {
		return workCountGoChildResult{}, fmt.Errorf("tree span/error: end=%d bytes=%d error=%v", root.EndByte(), len(input.source), root.HasError())
	}
	inspection, err := benchfixtures.InspectGoTree(root, input.lang)
	if err != nil {
		return workCountGoChildResult{}, fmt.Errorf("deep tree digest: %w", err)
	}
	if err := input.fixture.Fixture.VerifyDeepTreeDigest(inspection.SHA256); err != nil {
		return workCountGoChildResult{}, err
	}
	runtime := tree.ParseRuntime()
	if err := input.fixture.Fixture.VerifyWorkloadIdentity(runtime, inspection.NodeKinds); err != nil {
		return workCountGoChildResult{}, err
	}
	return workCountGoChildResult{
		Schema:            workCountGoChildSchema,
		Fixture:           input.fixture.Fixture.ID,
		SourceSHA256:      input.fixture.Fixture.SHA256,
		SourceBytes:       uint32(len(input.source)),
		GrammarCommit:     benchfixtures.GoGrammarCommit,
		GrammarBlobSHA256: input.blobSHA,
		DigestFormat:      benchfixtures.DeepTreeDigestVersion,
		DeepTreeSHA256:    inspection.SHA256,
		RootEndByte:       root.EndByte(),
		RootHasError:      root.HasError(),
		MaxStacksSeen:     runtime.MaxStacksSeen,
		MultiStackIters:   runtime.MultiStackIterations,
		MultiStackTokens:  runtime.MultiStackTokens,
	}, nil
}

func writeWorkCountChildResult(value any) error {
	resultPath := os.Getenv(workCountResultPathEnv)
	if resultPath == "" {
		return fmt.Errorf("%s is empty", workCountResultPathEnv)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode result: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(resultPath, data, 0o600); err != nil {
		return fmt.Errorf("write result: %w", err)
	}
	return nil
}
