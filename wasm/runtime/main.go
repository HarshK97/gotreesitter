//go:build js && wasm

package main

import (
	"encoding/json"
	"regexp"
	"strconv"
	"syscall/js"
	"unicode/utf16"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func main() {
	js.Global().Set("gotreesitter", js.ValueOf(map[string]interface{}{
		"parse":     js.FuncOf(parse),
		"query":     js.FuncOf(query),
		"highlight": js.FuncOf(highlight),
		"loadBlob":  js.FuncOf(loadBlob),
		"version":   js.ValueOf("0.1.0-runtime"),
		"mode":      js.ValueOf("runtime"),
	}))
	select {}
}

var languages = map[string]*gotreesitter.Language{}
var highlighters = map[string]*gotreesitter.Highlighter{}

func loadBlob(this js.Value, args []js.Value) interface{} {
	if len(args) < 3 {
		return err("usage: loadBlob(name, blobUint8Array, highlightQuery)")
	}
	name := args[0].String()
	jsArr := args[1]
	queryText := args[2].String()

	length := jsArr.Get("length").Int()
	blob := make([]byte, length)
	js.CopyBytesToGo(blob, jsArr)

	lang, langErr := grammars.LoadLanguage(name, blob)
	if langErr != nil {
		return err("load blob: " + langErr.Error())
	}
	languages[name] = lang

	if queryText != "" {
		hl, hlErr := gotreesitter.NewHighlighter(lang, queryText)
		if hlErr != nil {
			return err("highlighter: " + hlErr.Error())
		}
		highlighters[name] = hl
	}

	return ok(map[string]interface{}{"name": name})
}

// parseUTF16 parses source through the engine's UTF-16 entry point so the
// resulting tree carries a byte<->UTF-16 source map. The client is a JS
// string world (UTF-16 code units) while the engine speaks canonical UTF-8
// bytes; every span we hand back carries both coordinate systems.
func parseUTF16(lang *gotreesitter.Language, source string) (*gotreesitter.Tree, error) {
	parser := gotreesitter.NewParser(lang)
	return parser.ParseUTF16(utf16.Encode([]rune(source)))
}

// jsonNode is the wire shape of one syntax node in the structured tree the
// parse global returns. start/end are canonical UTF-8 byte offsets;
// start16/end16 are UTF-16 code-unit offsets into the original JS string.
type jsonNode struct {
	Type      string      `json:"type"`
	Start     uint32      `json:"start"`
	End       uint32      `json:"end"`
	Start16   uint32      `json:"start16"`
	End16     uint32      `json:"end16"`
	Named     bool        `json:"named"`
	Missing   bool        `json:"missing,omitempty"`
	Error     bool        `json:"error,omitempty"`
	Field     string      `json:"field,omitempty"`
	Children  []*jsonNode `json:"children,omitempty"`
	Truncated bool        `json:"truncated,omitempty"` // set on the root when the node cap hit
}

// maxTreeNodes caps the structured tree payload. Past this we stop
// descending and mark the root truncated; the client renders what it got.
const maxTreeNodes = 20000

func spans16(tree *gotreesitter.Tree, startByte, endByte uint32) (uint32, uint32) {
	if rng, has := tree.UTF16RangeForByteRange(startByte, endByte); has {
		return rng.StartCodeUnit, rng.EndCodeUnit
	}
	// Unreachable for ParseUTF16 trees; degrade to byte offsets rather
	// than lying with zeros.
	return startByte, endByte
}

func buildJSONNode(tree *gotreesitter.Tree, lang *gotreesitter.Language, n *gotreesitter.Node, field string, budget *int) *jsonNode {
	if n == nil || *budget <= 0 {
		return nil
	}
	*budget--
	start16, end16 := spans16(tree, n.StartByte(), n.EndByte())
	out := &jsonNode{
		Type:    n.Type(lang),
		Start:   n.StartByte(),
		End:     n.EndByte(),
		Start16: start16,
		End16:   end16,
		Named:   n.IsNamed(),
		Missing: n.IsMissing(),
		Error:   n.IsError(),
		Field:   field,
	}
	count := n.ChildCount()
	for i := 0; i < count; i++ {
		if *budget <= 0 {
			break
		}
		child := buildJSONNode(tree, lang, n.Child(i), n.FieldNameForChild(i, lang), budget)
		if child != nil {
			out.Children = append(out.Children, child)
		}
	}
	return out
}

func parse(this js.Value, args []js.Value) interface{} {
	if len(args) < 2 {
		return err("usage: parse(name, source)")
	}
	lang, has := languages[args[0].String()]
	if !has {
		return err("language not loaded: " + args[0].String())
	}
	tree, parseErr := parseUTF16(lang, args[1].String())
	if parseErr != nil {
		return err(parseErr.Error())
	}
	root := tree.RootNode()

	budget := maxTreeNodes
	jsonRoot := buildJSONNode(tree, lang, root, "", &budget)
	if jsonRoot != nil && budget <= 0 {
		jsonRoot.Truncated = true
	}
	treeJSON, marshalErr := json.Marshal(jsonRoot)
	if marshalErr != nil {
		return err("tree marshal: " + marshalErr.Error())
	}

	return ok(map[string]interface{}{
		"sexp":     root.SExpr(lang),
		"hasError": root.HasError(),
		// One JSON string over the JS boundary; the client JSON.parses it.
		"tree": string(treeJSON),
	})
}

// maxQueryMatches caps the query result payload.
const maxQueryMatches = 500

// queryErrorPos extracts the byte offset embedded in NewQuery error text
// ("... at position 17 ..."). The engine reports positions inside the
// message rather than as a structured field; most compile errors carry one.
var queryErrorPos = regexp.MustCompile(`at position (\d+)`)

func query(this js.Value, args []js.Value) interface{} {
	if len(args) < 3 {
		return err("usage: query(name, source, queryText)")
	}
	lang, has := languages[args[0].String()]
	if !has {
		return err("language not loaded: " + args[0].String())
	}

	q, qErr := gotreesitter.NewQuery(args[2].String(), lang)
	if qErr != nil {
		res := map[string]interface{}{"ok": false, "error": qErr.Error()}
		if m := queryErrorPos.FindStringSubmatch(qErr.Error()); m != nil {
			if off, convErr := strconv.Atoi(m[1]); convErr == nil {
				res["errorOffset"] = off
			}
		}
		return res
	}

	tree, parseErr := parseUTF16(lang, args[1].String())
	if parseErr != nil {
		return err(parseErr.Error())
	}
	source := tree.Source()

	matches := q.Execute(tree)
	truncated := false
	if len(matches) > maxQueryMatches {
		matches = matches[:maxQueryMatches]
		truncated = true
	}

	jsMatches := make([]interface{}, len(matches))
	for i, m := range matches {
		jsCaptures := make([]interface{}, len(m.Captures))
		for j, c := range m.Captures {
			var startByte, endByte uint32
			if c.Node != nil {
				startByte, endByte = c.Node.StartByte(), c.Node.EndByte()
			}
			start16, end16 := spans16(tree, startByte, endByte)
			capture := map[string]interface{}{
				"name":    c.Name,
				"start":   startByte,
				"end":     endByte,
				"start16": start16,
				"end16":   end16,
				"text":    c.Text(source),
			}
			if c.Node != nil {
				capture["type"] = c.Node.Type(lang)
			}
			jsCaptures[j] = capture
		}
		jsMatches[i] = map[string]interface{}{
			"pattern":  m.PatternIndex,
			"captures": jsCaptures,
		}
	}

	return ok(map[string]interface{}{
		"matches":   jsMatches,
		"truncated": truncated,
	})
}

func highlight(this js.Value, args []js.Value) interface{} {
	if len(args) < 2 {
		return err("usage: highlight(name, source)")
	}
	hl, has := highlighters[args[0].String()]
	if !has {
		return err("no highlighter for: " + args[0].String())
	}
	ranges := hl.Highlight([]byte(args[1].String()))
	jsRanges := make([]interface{}, len(ranges))
	for i, r := range ranges {
		jsRanges[i] = map[string]interface{}{
			"startByte": r.StartByte,
			"endByte":   r.EndByte,
			"capture":   r.Capture,
		}
	}
	return ok(map[string]interface{}{"ranges": jsRanges})
}

func ok(extra map[string]interface{}) interface{} {
	extra["ok"] = true
	return extra
}

func err(msg string) interface{} {
	return map[string]interface{}{"ok": false, "error": msg}
}
