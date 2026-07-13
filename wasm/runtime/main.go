//go:build js && wasm

package main

import (
	"regexp"
	"strconv"
	"syscall/js"

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

var languages = map[string]runtimeLanguage{}
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
	loaded := newRuntimeLanguage(name, lang)

	var hl *gotreesitter.Highlighter
	if queryText != "" {
		var hlErr error
		hl, hlErr = loaded.newHighlighter(queryText)
		if hlErr != nil {
			return err("highlighter: " + hlErr.Error())
		}
	}

	// Publish the language and its optional highlighter together only after all
	// validation succeeds. Reloading without a query intentionally clears an
	// older highlighter bound to the previous language instance.
	languages[name] = loaded
	if hl != nil {
		highlighters[name] = hl
	} else {
		delete(highlighters, name)
	}

	return ok(map[string]interface{}{"name": name})
}

func parse(this js.Value, args []js.Value) interface{} {
	if len(args) < 2 {
		return err("usage: parse(name, source)")
	}
	loaded, has := languages[args[0].String()]
	if !has {
		return err("language not loaded: " + args[0].String())
	}
	tree, parseErr := loaded.parseUTF16(args[1].String())
	if parseErr != nil {
		return err(parseErr.Error())
	}
	defer tree.Release()

	result, marshalErr := buildJSONParseResult(tree, loaded.language, tree.RootNode(), maxTreeNodes)
	if marshalErr != nil {
		return err("tree marshal: " + marshalErr.Error())
	}

	return ok(map[string]interface{}{
		"sexp":     result.SExpr,
		"hasError": result.HasError,
		// One JSON string over the JS boundary; the client JSON.parses it.
		"tree": result.Tree,
	})
}

// queryErrorPos extracts the byte offset embedded in NewQuery error text
// ("... at position 17 ..."). The engine reports positions inside the
// message rather than as a structured field; most compile errors carry one.
var queryErrorPos = regexp.MustCompile(`at position (\d+)`)

func query(this js.Value, args []js.Value) interface{} {
	if len(args) < 3 {
		return err("usage: query(name, source, queryText)")
	}
	loaded, has := languages[args[0].String()]
	if !has {
		return err("language not loaded: " + args[0].String())
	}

	q, qErr := gotreesitter.NewQuery(args[2].String(), loaded.language)
	if qErr != nil {
		res := map[string]interface{}{"ok": false, "error": qErr.Error()}
		if m := queryErrorPos.FindStringSubmatch(qErr.Error()); m != nil {
			if off, convErr := strconv.Atoi(m[1]); convErr == nil {
				res["errorOffset"] = off
			}
		}
		return res
	}

	tree, parseErr := loaded.parseUTF16(args[1].String())
	if parseErr != nil {
		return err(parseErr.Error())
	}
	defer tree.Release()

	matches, truncated := executeQueryJSON(q, tree, loaded.language, maxQueryMatches)

	return ok(map[string]interface{}{
		"matches":   queryMatchesForJS(matches),
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
