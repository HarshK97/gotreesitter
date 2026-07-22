#!/usr/bin/env python3
"""Warning-only maintainability hygiene scan.

This checker intentionally does not enforce behavior. It flags patterns that
make Tier IV work harder to review:

* direct child/field mutation in parser_result*.go outside approved helper
  files; and
* language-name conditionals in core parser files.

Default exit status is 0. Pass --strict to exit 1 when warnings are found.
"""
import argparse
import json
import os
import re
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]

APPROVED_RESULT_HELPERS = {
    "parser_result.go",
    "parser_result_compat.go",
    "parser_result_helpers.go",
    "parser_result_node_helpers.go",
    "parser_result_root_build.go",
    "parser_result_trivia_helpers.go",
}

CORE_PARSER_FILES = [
    "parser.go",
    "parser_retry.go",
    "parser_dfa_token_source.go",
    "parser_default_reduce.go",
    "parser_reduce.go",
    "parser_recover_c.go",
    "incremental.go",
    "language.go",
    "load_language.go",
]

MUTATION_PATTERNS = [
    re.compile(r"\.\s*Children\s*=(?!=)"),
    re.compile(r"\.\s*NamedChildren\s*=(?!=)"),
    re.compile(r"\.\s*Fields\s*=(?!=)"),
    re.compile(r"\.\s*FieldName\s*=(?!=)"),
    re.compile(r"\bappend\s*\([^)]*\.\s*(?:Children|NamedChildren|Fields)\b"),
    re.compile(r"\.\s*(?:children|fieldIDs|fieldSources)\s*=(?!=)"),
    re.compile(r"\.\s*(?:children|fieldIDs|fieldSources)\s*\[[^\]]+\]\s*=(?!=)"),
    re.compile(r"\bappend\s*\(\s*[^,\n]*\.\s*(?:children|fieldIDs|fieldSources)\b"),
]

LANGUAGE_SWITCH_PATTERNS = [
    re.compile(r"\b(?:LanguageName|GrammarName)\s*\("),
    re.compile(r"\b(?:languageName|grammarName|langName|grammar)\s*(?:==|!=)\s*\"[A-Za-z0-9_+-]+\""),
]

LANGUAGE_SWITCH_START = re.compile(
    r"\bswitch\b.*\b(?:languageName|grammarName|langName|grammar)\b"
)
STRING_CASE = re.compile(r"\bcase\s+\"[A-Za-z0-9_+-]+\"\s*:")


def iter_go_lines(path):
    try:
        with path.open(encoding="utf-8") as f:
            for lineno, line in enumerate(f, 1):
                yield lineno, line.rstrip("\n")
    except UnicodeDecodeError:
        with path.open(encoding="utf-8", errors="replace") as f:
            for lineno, line in enumerate(f, 1):
                yield lineno, line.rstrip("\n")


def result_mutation_warnings():
    warnings = []
    for path in sorted(ROOT.glob("parser_result*.go")):
        if path.name in APPROVED_RESULT_HELPERS:
            continue
        for lineno, line in iter_go_lines(path):
            stripped = line.strip()
            if not stripped or stripped.startswith("//"):
                continue
            if any(pattern.search(line) for pattern in MUTATION_PATTERNS):
                warnings.append(("result-mutation", path, lineno, stripped))
    return warnings


def language_switch_warnings():
    warnings = []
    for rel in CORE_PARSER_FILES:
        path = ROOT / rel
        if not path.exists():
            continue
        in_language_switch = False
        switch_depth = 0
        for lineno, line in iter_go_lines(path):
            stripped = line.strip()
            if not stripped or stripped.startswith("//"):
                continue
            if any(pattern.search(line) for pattern in LANGUAGE_SWITCH_PATTERNS):
                warnings.append(("language-switch", path, lineno, stripped))
            if LANGUAGE_SWITCH_START.search(line):
                warnings.append(("language-switch", path, lineno, stripped))
                in_language_switch = True
                switch_depth = line.count("{") - line.count("}")
                continue
            if in_language_switch and STRING_CASE.search(line):
                warnings.append(("language-switch", path, lineno, stripped))
            if in_language_switch:
                switch_depth += line.count("{") - line.count("}")
                if switch_depth <= 0:
                    in_language_switch = False
    return warnings


def warning_to_record(warning):
    kind, path, lineno, line = warning
    return {
        "kind": kind,
        "file": os.path.relpath(path, ROOT),
        "line": lineno,
        "text": line,
    }


def build_report(warnings):
    by_kind = {}
    by_file = {}
    for kind, path, _, _ in warnings:
        by_kind[kind] = by_kind.get(kind, 0) + 1
        rel = os.path.relpath(path, ROOT)
        entry = by_file.setdefault(rel, {"file": rel, "total": 0, "by_kind": {}})
        entry["total"] += 1
        entry["by_kind"][kind] = entry["by_kind"].get(kind, 0) + 1

    return {
        "schema_version": 1,
        "total": len(warnings),
        "by_kind": dict(sorted(by_kind.items())),
        "by_file": sorted(
            by_file.values(),
            key=lambda item: (-item["total"], item["file"]),
        ),
        "warnings": [warning_to_record(warning) for warning in warnings],
    }


def print_warnings(report, max_examples):
    warnings = report["warnings"]
    for warning in warnings[:max_examples]:
        print(f"{warning['kind']}: {warning['file']}:{warning['line']}: {warning['text']}")
    if len(warnings) > max_examples:
        print(f"... {len(warnings) - max_examples} more warning(s) omitted; "
              "raise --max-examples to inspect more")

    print("\nSummary by kind:")
    for kind in ("result-mutation", "language-switch"):
        print(f"  {kind}: {report['by_kind'].get(kind, 0)}")

    print("\nSummary by file:")
    for entry in report["by_file"]:
        kind_counts = " ".join(
            f"{kind}={count}" for kind, count in sorted(entry["by_kind"].items())
        )
        print(f"  {entry['file']}: {entry['total']} ({kind_counts})")

    by_kind = report["by_kind"]
    print("maint_hygiene_check warnings: " +
          " ".join(f"{kind}={by_kind.get(kind, 0)}"
                   for kind in ("result-mutation", "language-switch")) +
          f" total={report['total']}")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--strict", action="store_true",
                        help="exit 1 if any warnings are found")
    parser.add_argument("--max-examples", type=int, default=40,
                        help="maximum warning examples to print before the summary")
    parser.add_argument("--json", action="store_true",
                        help="emit machine-readable JSON instead of text")
    args = parser.parse_args()

    warnings = result_mutation_warnings() + language_switch_warnings()
    report = build_report(warnings)
    if args.json:
        print(json.dumps(report, indent=2, sort_keys=True))
    else:
        print_warnings(report, max(0, args.max_examples))
    if args.strict and warnings:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
