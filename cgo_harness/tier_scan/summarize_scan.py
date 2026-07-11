#!/usr/bin/env python3
"""Summarize tier-scan artifacts without invoking parser machinery."""

from __future__ import annotations

import argparse
import json
import re
import shlex
import sys
from collections import defaultdict
from pathlib import Path
from typing import Any, Callable


DTIER_PREFIX = "MEASURE-DTIER "
PROGRESS_PREFIX = "MEASURE-PROGRESS "
RATIO_FIELDS = ("aggRatio", "medianRatio")
SIGNATURE_FIELDS = ("files", "diverge", "oracleBetter", "trunc", "errTree", "panics")
SLOW_RATIO_THRESHOLD = 8.0
TERMINAL_LIFECYCLES = {"timeout", "fail", "failed", "panic", "panicked"}
FRONTIER_STOP_REASONS = {"no_stacks_alive", "node_limit", "memory_budget"}
RUNTIME_FIELD_RE = re.compile(r"\b([A-Za-z][A-Za-z0-9]*)=([^ \t{}]+)")
COMPARISON_DIAG_PHASE = "comparison_diag"
COMPARISON_DIAG_FIELDS = (
    "diff",
    "firstDiffPath",
    "goType",
    "cType",
    "goSpan",
    "cSpan",
    "goCC",
    "cCC",
    "goRoot",
    "goRootSpan",
    "goRootCC",
    "goRootErr",
    "cRoot",
    "cRootSpan",
    "cRootCC",
    "cRootErr",
    "goErrors",
    "cErrors",
    "goMissing",
    "cMissing",
    "oracle",
    "goStop",
    "runtime",
    "goRuntime",
)


def repo_root() -> Path:
    return Path(__file__).resolve().parents[2]


def read_lines(path: Path) -> list[str]:
    if not path.exists():
        return []
    return [line.strip() for line in path.read_text(encoding="utf-8").splitlines() if line.strip()]


def read_grammar_set(path: Path) -> set[str]:
    grammars = set()
    for line in read_lines(path):
        grammar = line.split(None, 1)[0]
        if grammar:
            grammars.add(grammar)
    return grammars


def parse_kv_fields(text: str) -> dict[str, str]:
    fields: dict[str, str] = {}
    try:
        tokens = shlex.split(text)
    except ValueError:
        tokens = text.split()
    for token in tokens:
        if "=" not in token:
            continue
        key, value = token.split("=", 1)
        fields[key] = value
    return fields


def parse_ratio(value: str | None) -> float | None:
    if value is None:
        return None
    value = value.removesuffix("x")
    try:
        return float(value)
    except ValueError:
        return None


def parity_pair(value: str | None) -> str | None:
    if not value:
        return None
    return value.split("(", 1)[0]


def parity_counts(value: str | None) -> tuple[int, int] | None:
    pair = parity_pair(value)
    if not pair or "/" not in pair:
        return None
    matched, total = pair.split("/", 1)
    try:
        return int(matched), int(total)
    except ValueError:
        return None


def parity_is_clean(value: str | None) -> bool:
    counts = parity_counts(value)
    return counts is not None and counts[0] == counts[1]


def parse_measure_line(line: str) -> dict[str, Any] | None:
    if not line.startswith(DTIER_PREFIX):
        return None
    rest = line[len(DTIER_PREFIX) :].strip()
    if not rest:
        return None
    parts = rest.split(None, 1)
    grammar = parts[0]
    kv = parse_kv_fields(parts[1] if len(parts) > 1 else "")
    parsed: dict[str, Any] = {
        "grammar": grammar,
        "raw": line,
        "parity": parity_pair(kv.get("parityMatch")),
        "parityRaw": kv.get("parityMatch"),
    }
    for key, value in kv.items():
        parsed[key] = value
    for key in SIGNATURE_FIELDS:
        try:
            parsed[key] = int(str(parsed.get(key, "0")))
        except ValueError:
            parsed[key] = 0
    for key in RATIO_FIELDS:
        parsed[key + "Value"] = parse_ratio(kv.get(key))
    return parsed


def parse_int(value: str | None) -> int | None:
    if value is None:
        return None
    value = value.split("/", 1)[0]
    try:
        return int(value)
    except ValueError:
        return None


def parse_runtime_fields(runtime: str | None) -> dict[str, str]:
    if not runtime:
        return {}
    return {match.group(1): match.group(2).strip('"') for match in RUNTIME_FIELD_RE.finditer(runtime)}


def status_log_paths(events: list[dict[str, str]], corpus_kind: str | None = None) -> list[Path]:
    paths: list[Path] = []
    seen: set[Path] = set()
    for event in events:
        if corpus_kind and event.get("corpusKind") != corpus_kind:
            continue
        kv = parse_kv_fields(event.get("detail", ""))
        log = kv.get("log")
        if not log:
            continue
        path = Path(log)
        if path in seen:
            continue
        seen.add(path)
        paths.append(path)
    return paths


def load_measurements(scan_dir: Path, status_by_grammar: dict[str, list[dict[str, str]]] | None = None) -> dict[str, dict[str, Any]]:
    measurements: dict[str, dict[str, Any]] = {}
    report_path = scan_dir / "tier_scan.txt"
    status_by_grammar = status_by_grammar or {}
    for line in read_lines(report_path):
        parsed = parse_measure_line(line)
        if parsed is None:
            continue
        grammar = parsed["grammar"]
        corpus = parsed.get("corpus") or "external"
        for log_path in status_log_paths(status_by_grammar.get(grammar, []), corpus):
            if log_path.exists():
                parsed["log"] = str(log_path)
                break
        if "log" not in parsed:
            log_path = scan_dir / f"measure-{grammar}-{corpus}.log"
            if log_path.exists():
                parsed["log"] = str(log_path)
        measurements[grammar] = parsed
    return measurements


ProgressItem = tuple[Path, str, dict[str, str], dict[str, str]]


def load_frames_by_grammar(path: Path) -> dict[str, list[dict[str, Any]]]:
    by_grammar: dict[str, list[dict[str, Any]]] = defaultdict(list)
    if not path.exists():
        return {}
    text = path.read_text(encoding="utf-8", errors="replace")
    lines = text.splitlines()
    complete_tail = text.endswith(("\n", "\r"))
    for index, line in enumerate(lines):
        if not line.strip():
            continue
        try:
            row = json.loads(line)
        except json.JSONDecodeError:
            if index == len(lines) - 1 and not complete_tail:
                break
            raise
        if not isinstance(row, dict):
            continue
        grammar = row.get("grammar")
        if isinstance(grammar, str) and grammar:
            by_grammar[grammar].append(row)
    return dict(by_grammar)


def frame_value(value: Any) -> str:
    if value is None:
        return ""
    return str(value)


def frame_progress_items(frames: list[dict[str, Any]]) -> list[ProgressItem]:
    items: list[ProgressItem] = []
    for frame in frames:
        kv = {
            "lifecycle": frame_value(frame.get("lifecycle")),
            "phase": frame_value(frame.get("phase")),
            "result": frame_value(frame.get("result")),
            "rc": frame_value(frame.get("rc")),
            "file": frame_value(frame.get("file")),
            "base": frame_value(frame.get("base")),
            "path": frame_value(frame.get("path")),
            "elapsed_ms": frame_value(frame.get("elapsed_ms")),
            "duration_ms": frame_value(frame.get("duration_ms")),
            "runtime": frame_value(frame.get("runtime")),
        }
        for key in COMPARISON_DIAG_FIELDS:
            if key not in kv and frame.get(key) is not None:
                kv[key] = frame_value(frame.get(key))
        raw = PROGRESS_PREFIX + " ".join(f"{key}={shlex.quote(value)}" for key, value in kv.items() if value)
        log = Path(frame_value(frame.get("raw_log") or frame.get("source_artifact") or "frames.jsonl"))
        items.append((log, raw, kv, parse_runtime_fields(kv.get("runtime"))))
    return items


def frame_log_paths(frames: list[dict[str, Any]]) -> list[Path]:
    paths: list[Path] = []
    seen: set[Path] = set()
    for frame in frames:
        raw_log = frame_value(frame.get("raw_log"))
        if not raw_log:
            continue
        path = Path(raw_log)
        if path in seen or not path.exists():
            continue
        seen.add(path)
        paths.append(path)
    return paths


def compact_frame(frame: dict[str, Any]) -> dict[str, Any]:
    return {
        "lifecycle": frame_value(frame.get("lifecycle")),
        "phase": frame_value(frame.get("phase")),
        "result": frame_value(frame.get("result")),
        "rc": frame.get("rc"),
        "file": frame_value(frame.get("file")),
        "base": frame_value(frame.get("base")),
        "path": frame_value(frame.get("path")),
        "elapsedMs": frame.get("elapsed_ms"),
        "durationMs": frame.get("duration_ms"),
        "rawLog": frame_value(frame.get("raw_log")),
    }


def terminal_evidence(frames: list[dict[str, Any]]) -> dict[str, Any]:
    terminal_frames = [
        frame
        for frame in frames
        if frame_value(frame.get("lifecycle")).lower() in TERMINAL_LIFECYCLES
    ]
    comparison_frames = [
        frame
        for frame in frames
        if frame_value(frame.get("phase")) == "comparison_result"
    ]
    last_frame = frames[-1] if frames else {}
    terminal_statuses = []
    for frame in terminal_frames:
        lifecycle = frame_value(frame.get("lifecycle"))
        phase = frame_value(frame.get("phase"))
        rc = frame.get("rc")
        status = f"{lifecycle}:{phase}" if phase else lifecycle
        if rc not in (None, ""):
            status += f":rc={rc}"
        terminal_statuses.append(status)
    return {
        "hasTerminalTimeoutOrFail": bool(terminal_frames),
        "terminalStatuses": terminal_statuses,
        "terminalFrames": [compact_frame(frame) for frame in terminal_frames[:5]],
        "terminalCount": len(terminal_frames),
        "hasComparisonResult": bool(comparison_frames),
        "comparisonResults": sorted(
            {
                frame_value(frame.get("result"))
                for frame in comparison_frames
                if frame_value(frame.get("result"))
            }
        ),
        "comparisonResultCount": len(comparison_frames),
        "lastPhase": frame_value(last_frame.get("phase")),
        "lastLifecycle": frame_value(last_frame.get("lifecycle")),
        "lastRc": last_frame.get("rc"),
        "lastFrame": compact_frame(last_frame) if last_frame else None,
    }


def add_measurement_evidence(
    measurements: dict[str, dict[str, Any]],
    frames_by_grammar: dict[str, list[dict[str, Any]]] | None = None,
) -> None:
    frames_by_grammar = frames_by_grammar or {}
    for measurement in measurements.values():
        grammar = str(measurement.get("grammar", ""))
        frames = frames_by_grammar.get(grammar, [])
        measurement["terminalEvidence"] = terminal_evidence(frames)
        log_items = progress_items(frame_log_paths(frames))
        if frames:
            measurement["runtimeEvidence"] = progress_evidence(frame_progress_items(frames) + log_items)
            continue
        log = measurement.get("log")
        if not log:
            measurement["runtimeEvidence"] = progress_evidence([])
            continue
        measurement["runtimeEvidence"] = progress_evidence(progress_items([Path(str(log))]))


def signature(measurement: dict[str, Any]) -> str:
    return " ".join(f"{key}={measurement.get(key, 0)}" for key in SIGNATURE_FIELDS)


def load_status(path: Path) -> dict[str, list[dict[str, str]]]:
    by_grammar: dict[str, list[dict[str, str]]] = defaultdict(list)
    for line in read_lines(path):
        fields = line.split("\t", 4)
        while len(fields) < 5:
            fields.append("")
        ts, event, grammar, corpus_kind, detail = fields
        by_grammar[grammar].append(
            {
                "timestamp": ts,
                "event": event,
                "grammar": grammar,
                "corpusKind": corpus_kind,
                "detail": detail,
            }
        )
    return dict(by_grammar)


def progress_lines(log_path: Path) -> list[str]:
    if not log_path.exists():
        return []
    return [
        line.strip()
        for line in log_path.read_text(encoding="utf-8", errors="replace").splitlines()
        if line.startswith(PROGRESS_PREFIX)
    ]


def progress_items(logs: list[Path]) -> list[ProgressItem]:
    items: list[ProgressItem] = []
    for log in logs:
        for progress in progress_lines(log):
            kv = parse_kv_fields(progress[len(PROGRESS_PREFIX) :])
            items.append((log, progress, kv, parse_runtime_fields(kv.get("runtime"))))
    return items


def comparison_diag_row(log: Path, raw: str, kv: dict[str, str]) -> dict[str, Any]:
    return {
        "log": str(log),
        "file": kv.get("file", ""),
        "base": kv.get("base", ""),
        "path": kv.get("path", ""),
        "phase": kv.get("phase", ""),
        "result": kv.get("result", ""),
        "elapsedMs": kv.get("elapsed_ms", ""),
        "diff": kv.get("diff", ""),
        "firstDiffPath": kv.get("firstDiffPath", kv.get("path", "")),
        "goType": kv.get("goType", ""),
        "cType": kv.get("cType", ""),
        "goSpan": kv.get("goSpan", ""),
        "cSpan": kv.get("cSpan", ""),
        "goChildCount": parse_int(kv.get("goCC")),
        "cChildCount": parse_int(kv.get("cCC")),
        "goRoot": kv.get("goRoot", ""),
        "goRootSpan": kv.get("goRootSpan", ""),
        "goRootChildCount": parse_int(kv.get("goRootCC")),
        "goRootErr": kv.get("goRootErr", ""),
        "cRoot": kv.get("cRoot", ""),
        "cRootSpan": kv.get("cRootSpan", ""),
        "cRootChildCount": parse_int(kv.get("cRootCC")),
        "cRootErr": kv.get("cRootErr", ""),
        "goErrorCount": parse_int(kv.get("goErrors")),
        "cErrorCount": parse_int(kv.get("cErrors")),
        "goMissingCount": parse_int(kv.get("goMissing")),
        "cMissingCount": parse_int(kv.get("cMissing")),
        "oracle": kv.get("oracle", ""),
        "goStop": kv.get("goStop", ""),
        "runtime": kv.get("runtime", ""),
        "goRuntime": kv.get("goRuntime", ""),
        "raw": raw,
    }


def comparison_diag_evidence(items: list[ProgressItem]) -> dict[str, Any]:
    rows = [
        comparison_diag_row(log, raw, kv)
        for log, raw, kv, _ in items
        if kv.get("phase") == COMPARISON_DIAG_PHASE
    ]
    diffs = sorted({row["diff"] for row in rows if row.get("diff")})
    root_pairs = sorted(
        {
            f"{row.get('goRoot', '')}->{row.get('cRoot', '')}"
            for row in rows
            if row.get("goRoot") or row.get("cRoot")
        }
    )
    oracle_relations = sorted({row["oracle"] for row in rows if row.get("oracle")})
    return {
        "count": len(rows),
        "diffs": diffs,
        "rootPairs": root_pairs,
        "oracleRelations": oracle_relations,
        "rows": rows[:5],
    }


def progress_evidence(items: list[ProgressItem]) -> dict[str, Any]:
    runtime_items = [item for item in items if item[3]]
    stop_reasons = sorted({runtime.get("stopReason", "") for _, _, _, runtime in runtime_items if runtime.get("stopReason")})
    roots = sorted({runtime.get("root", "") for _, _, _, runtime in runtime_items if runtime.get("root")})
    max_stacks = [parse_int(runtime.get("maxStacks")) for _, _, _, runtime in runtime_items]
    max_stacks = [value for value in max_stacks if value is not None]
    nodes = [parse_int(runtime.get("nodes")) for _, _, _, runtime in runtime_items]
    nodes = [value for value in nodes if value is not None]
    tokens_zero = []
    accepted_divergences = []
    frontier_stops = []
    for log, raw, kv, runtime in runtime_items:
        tokens = parse_int(runtime.get("tokens"))
        iterations = parse_int(runtime.get("iterations"))
        accepted = runtime.get("stopReason") == "accepted"
        diverged = kv.get("result") == "diverge"
        comparison_result = kv.get("phase") in {"comparison_result", COMPARISON_DIAG_PHASE}
        if comparison_result and accepted and diverged:
            accepted_divergences.append(
                {
                    "log": str(log),
                    "file": kv.get("file", ""),
                    "base": kv.get("base", ""),
                    "path": kv.get("path", ""),
                    "phase": kv.get("phase", ""),
                    "elapsedMs": kv.get("elapsed_ms", ""),
                    "stopReason": runtime.get("stopReason", ""),
                    "tokens": runtime.get("tokens", ""),
                    "iterations": runtime.get("iterations", ""),
                    "nodes": runtime.get("nodes", ""),
                    "maxStacks": runtime.get("maxStacks", ""),
                    "truncated": runtime.get("truncated", ""),
                    "rootErr": runtime.get("rootErr", ""),
                    "raw": raw,
                }
            )
        if runtime.get("stopReason") in FRONTIER_STOP_REASONS or runtime.get("truncated") == "true":
            frontier_stops.append(
                {
                    "log": str(log),
                    "file": kv.get("file", ""),
                    "base": kv.get("base", ""),
                    "path": kv.get("path", ""),
                    "phase": kv.get("phase", ""),
                    "result": kv.get("result", ""),
                    "elapsedMs": kv.get("elapsed_ms", ""),
                    "stopReason": runtime.get("stopReason", ""),
                    "truncated": runtime.get("truncated", ""),
                    "rootErr": runtime.get("rootErr", ""),
                    "tokens": runtime.get("tokens", ""),
                    "nodes": runtime.get("nodes", ""),
                    "maxStacks": runtime.get("maxStacks", ""),
                    "raw": raw,
                }
            )
        if accepted and diverged and tokens == 0 and (iterations == 0 or iterations is None):
            tokens_zero.append(
                {
                    "log": str(log),
                    "file": kv.get("file", ""),
                    "base": kv.get("base", ""),
                    "path": kv.get("path", ""),
                    "phase": kv.get("phase", ""),
                    "elapsedMs": kv.get("elapsed_ms", ""),
                    "raw": raw,
                }
            )
    return {
        "stopReasons": stop_reasons,
        "rootErr": any(runtime.get("rootErr") == "true" for _, _, _, runtime in runtime_items),
        "roots": roots,
        "comparisonDiagnostic": comparison_diag_evidence(items),
        "tokensZeroAcceptedDivergence": tokens_zero[:5],
        "tokensZeroAcceptedDivergenceCount": len(tokens_zero),
        "acceptedDivergence": accepted_divergences[:5],
        "acceptedDivergenceCount": len(accepted_divergences),
        "frontierStops": frontier_stops[:5],
        "frontierStopCount": len(frontier_stops),
        "maxStacks": max(max_stacks) if max_stacks else None,
        "maxNodes": max(nodes) if nodes else None,
        "truncated": any(runtime.get("truncated") == "true" for _, _, _, runtime in runtime_items),
    }


def log_paths_for(scan_dir: Path, grammar: str) -> list[Path]:
    return sorted(scan_dir.glob(f"measure-{grammar}-*.log"))


def evidence_log_paths(scan_dir: Path, grammar: str, events: list[dict[str, str]]) -> list[Path]:
    paths: list[Path] = []
    seen: set[Path] = set()
    for path in status_log_paths(events):
        if path in seen:
            continue
        seen.add(path)
        paths.append(path)
    for path in log_paths_for(scan_dir, grammar):
        if path in seen:
            continue
        seen.add(path)
        paths.append(path)
    return paths


def status_reason(events: list[dict[str, str]]) -> dict[str, str]:
    interesting = [event for event in events if event["event"] in {"TIMEOUT", "FAIL", "SKIP"}]
    if not interesting:
        interesting = events[-1:] if events else []
    if not interesting:
        return {"event": "", "corpusKind": "", "detail": ""}
    event = interesting[-1]
    return {
        "event": event["event"],
        "corpusKind": event["corpusKind"],
        "detail": event["detail"],
        "timestamp": event["timestamp"],
    }


def summarize_unmeasured(
    scan_dir: Path,
    status_by_grammar: dict[str, list[dict[str, str]]],
    frames_by_grammar: dict[str, list[dict[str, Any]]] | None = None,
) -> list[dict[str, Any]]:
    frames_by_grammar = frames_by_grammar or {}
    entries: list[dict[str, Any]] = []
    for line in read_lines(scan_dir / "unmeasured.txt"):
        parts = line.split()
        if not parts:
            continue
        grammar = parts[0]
        events = status_by_grammar.get(grammar, [])
        logs = evidence_log_paths(scan_dir, grammar, events)
        frames = frames_by_grammar.get(grammar, [])
        all_progress = frame_progress_items(frames) + progress_items(frame_log_paths(frames)) if frames else progress_items(logs)
        runtime_evidence = progress_evidence(all_progress)
        terminal = terminal_evidence(frames)
        selected = [item for item in all_progress if item[2].get("phase") == "selected_file"]
        last_progress = all_progress[-1] if all_progress else None
        selected_item = selected[-1] if selected else None
        reason = status_reason(events)
        entry: dict[str, Any] = {
            "grammar": grammar,
            "unmeasuredLine": line,
            "reason": " ".join(parts[1:]) if len(parts) > 1 else "",
            "status": reason,
            "logs": [str(path) for path in logs],
            "selectedFile": None,
            "lastProgress": None,
            "runtimeEvidence": runtime_evidence,
            "terminalEvidence": terminal,
        }
        if selected_item is not None:
            _, _, kv, _ = selected_item
            entry["selectedFile"] = {
                "base": kv.get("base", ""),
                "path": kv.get("path", ""),
                "bytes": kv.get("bytes", ""),
                "elapsedMs": kv.get("elapsed_ms", ""),
            }
        if last_progress is not None:
            log, raw, kv, runtime = last_progress
            entry["lastProgress"] = {
                "log": str(log),
                "phase": kv.get("phase", ""),
                "elapsedMs": kv.get("elapsed_ms", ""),
                "durationMs": kv.get("duration_ms", ""),
                "file": kv.get("file", ""),
                "base": kv.get("base", ""),
                "path": kv.get("path", ""),
                "stopReason": runtime.get("stopReason", kv.get("stopReason", "")),
                "rootErr": runtime.get("rootErr", ""),
                "tokens": runtime.get("tokens", ""),
                "maxStacks": runtime.get("maxStacks", ""),
                "raw": raw,
            }
        entries.append(entry)
    return entries


def compact_measurement(item: dict[str, Any]) -> dict[str, Any]:
    runtime = item.get("runtimeEvidence") or {}
    terminal = item.get("terminalEvidence") or {}
    return {
        "grammar": item["grammar"],
        "files": item.get("files", 0),
        "parity": item.get("parity", ""),
        "medianRatio": item.get("medianRatio", ""),
        "aggRatio": item.get("aggRatio", ""),
        "diverge": item.get("diverge", 0),
        "oracleBetter": item.get("oracleBetter", 0),
        "trunc": item.get("trunc", 0),
        "errTree": item.get("errTree", 0),
        "panics": item.get("panics", 0),
        "log": item.get("log", ""),
        "evidence": {
            "stopReasons": runtime.get("stopReasons", []),
            "rootErr": runtime.get("rootErr", False),
            "tokensZeroAcceptedDivergenceCount": runtime.get("tokensZeroAcceptedDivergenceCount", 0),
            "acceptedDivergenceCount": runtime.get("acceptedDivergenceCount", 0),
            "frontierStopCount": runtime.get("frontierStopCount", 0),
            "maxStacks": runtime.get("maxStacks"),
            "maxNodes": runtime.get("maxNodes"),
            "truncated": runtime.get("truncated", False),
            "comparisonDiagnosticCount": (runtime.get("comparisonDiagnostic") or {}).get("count", 0),
            "comparisonDiagnosticDiffs": (runtime.get("comparisonDiagnostic") or {}).get("diffs", []),
            "comparisonDiagnosticRootPairs": (runtime.get("comparisonDiagnostic") or {}).get("rootPairs", []),
            "comparisonDiagnosticOracleRelations": (runtime.get("comparisonDiagnostic") or {}).get("oracleRelations", []),
            "terminalStatuses": terminal.get("terminalStatuses", []),
            "terminalCount": terminal.get("terminalCount", 0),
            "lastPhase": terminal.get("lastPhase", ""),
            "lastLifecycle": terminal.get("lastLifecycle", ""),
            "lastRc": terminal.get("lastRc"),
            "hasComparisonResult": terminal.get("hasComparisonResult", False),
            "comparisonResults": terminal.get("comparisonResults", []),
        },
    }


def failure_family_entry(
    name: str,
    description: str,
    measurements: list[dict[str, Any]],
    top_n: int,
    sort_ratio: str,
) -> dict[str, Any]:
    ratio_value_key = sort_ratio + "Value"
    sorted_measurements = sorted(measurements, key=lambda item: item["grammar"])
    measured_items = [item for item in measurements if "unmeasuredLine" not in item]
    unmeasured_items = [item for item in measurements if "unmeasuredLine" in item]
    top_slow = sorted(
        [item for item in measured_items if int(item.get("files", 0)) > 0],
        key=lambda item: (
            item.get(ratio_value_key) is not None,
            item.get(ratio_value_key) or -1.0,
            item.get("aggRatioValue") or -1.0,
            item.get("medianRatioValue") or -1.0,
        ),
        reverse=True,
    )[:top_n]
    compact_evidence = [compact_measurement(item) for item in sorted_measurements[:top_n]]
    return {
        "name": name,
        "description": description,
        "count": len(sorted_measurements),
        "grammars": [item["grammar"] for item in sorted_measurements],
        "evidence": compact_evidence,
        "topSlowOverlap": [compact_measurement(item) for item in top_slow],
        "entries": sorted(unmeasured_items, key=lambda item: item["grammar"]),
    }


def failure_families(
    measurements: dict[str, dict[str, Any]],
    unmeasured_entries: list[dict[str, Any]],
    top_n: int,
    sort_ratio: str,
) -> list[dict[str, Any]]:
    measured = list(measurements.values())

    def select(predicate: Callable[[dict[str, Any]], bool]) -> list[dict[str, Any]]:
        return [item for item in measured if predicate(item)]

    def slow(item: dict[str, Any]) -> bool:
        ratios = [item.get("aggRatioValue"), item.get("medianRatioValue")]
        return any(value is not None and value > SLOW_RATIO_THRESHOLD for value in ratios)

    def clean(item: dict[str, Any]) -> bool:
        return parity_is_clean(str(item.get("parity", ""))) and int(item.get("diverge", 0)) == 0

    def runtime(item: dict[str, Any]) -> dict[str, Any]:
        return item.get("runtimeEvidence") or {}

    def terminal(item: dict[str, Any]) -> dict[str, Any]:
        return item.get("terminalEvidence") or {}

    def has_terminal_timeout_or_fail(item: dict[str, Any]) -> bool:
        return bool(terminal(item).get("hasTerminalTimeoutOrFail"))

    def has_comparison_result(item: dict[str, Any]) -> bool:
        return bool(terminal(item).get("hasComparisonResult"))

    def has_token_accounting_evidence(item: dict[str, Any]) -> bool:
        evidence = runtime(item)
        return int(evidence.get("tokensZeroAcceptedDivergenceCount") or 0) > 0

    def has_runtime_frontier_stop(item: dict[str, Any]) -> bool:
        evidence = runtime(item)
        return (
            any(reason in FRONTIER_STOP_REASONS for reason in evidence.get("stopReasons", []))
            or bool(evidence.get("truncated"))
            or int(evidence.get("frontierStopCount") or 0) > 0
        )

    def has_accepted_divergence_evidence(item: dict[str, Any]) -> bool:
        return int(runtime(item).get("acceptedDivergenceCount") or 0) > 0

    def has_oracle_improvement(item: dict[str, Any]) -> bool:
        if int(item.get("oracleBetter", 0) or 0) > 0:
            return True
        diag = (runtime(item).get("comparisonDiagnostic") or {})
        return "go_clean_c_error" in diag.get("oracleRelations", [])

    def has_accepted_shape_evidence(item: dict[str, Any]) -> bool:
        return (
            int(item.get("diverge", 0)) > 0
            and not has_oracle_improvement(item)
            and has_comparison_result(item)
            and has_accepted_divergence_evidence(item)
            and int(item.get("trunc", 0)) == 0
            and int(item.get("errTree", 0)) == 0
            and not bool(runtime(item).get("rootErr"))
            and not bool(runtime(item).get("truncated"))
            and not has_token_accounting_evidence(item)
            and not has_terminal_timeout_or_fail(item)
        )

    def has_accepted_divergence_cost(item: dict[str, Any]) -> bool:
        evidence = runtime(item)
        return (
            has_accepted_divergence_evidence(item)
            and not has_terminal_timeout_or_fail(item)
            and not has_token_accounting_evidence(item)
            and not bool(evidence.get("rootErr"))
            and not bool(evidence.get("truncated"))
            and (
                slow(item)
                or int(evidence.get("maxStacks") or 0) >= 64
                or int(evidence.get("maxNodes") or 0) >= 50000
            )
        )

    terminal_timeout_or_fail = [
        item
        for item in unmeasured_entries
        if has_terminal_timeout_or_fail(item) or (item.get("status") or {}).get("event") in {"TIMEOUT", "FAIL"}
    ]
    terminal_timeout_or_fail.extend(select(has_terminal_timeout_or_fail))

    return [
        failure_family_entry(
            "terminal_timeout_or_fail",
            "Rows with terminal lifecycle timeout/fail/panic frame evidence, with phase and rc carried from frames.jsonl. This is separated from runtime frontier stops and slow accepted divergences.",
            terminal_timeout_or_fail,
            top_n,
            sort_ratio,
        ),
        failure_family_entry(
            "runtime_frontier_stop",
            "Measured rows with runtime stopReason no_stacks_alive/node_limit/memory_budget or truncated=true evidence. Terminal timeouts/fails are excluded.",
            select(lambda item: not has_terminal_timeout_or_fail(item) and (int(item.get("trunc", 0)) > 0 or has_runtime_frontier_stop(item))),
            top_n,
            sort_ratio,
        ),
        failure_family_entry(
            "recovery_error_cost",
            "Measured rows with error-tree counts or root ERROR runtime evidence. This can overlap runtime frontier and accepted-divergence cost when aggregate/reporting signals differ.",
            select(
                lambda item: int(item.get("errTree", 0)) > 0
                or bool(runtime(item).get("rootErr"))
            ),
            top_n,
            sort_ratio,
        ),
        failure_family_entry(
            "oracle_improvement",
            "Measured rows where Go produced a clean tree while the C oracle produced ERROR/MISSING nodes. These remain non-parity rows but are separated from ordinary Go parser failures so generalized triage can decide whether to preserve Go-superior behavior or add explicit C-compatibility policy.",
            select(has_oracle_improvement),
            top_n,
            sort_ratio,
        ),
        failure_family_entry(
            "accepted_shape_materialization",
            "Measured rows with an actual comparison_result=diverge frame whose runtime stopReason is accepted, with no truncation, error-tree/root ERROR, token-accounting, or terminal timeout/fail evidence.",
            select(has_accepted_shape_evidence),
            top_n,
            sort_ratio,
        ),
        failure_family_entry(
            "accepted_divergence_cost",
            "Accepted comparison-result divergences with high stack/node/ratio cost and no token-zero or root ERROR evidence. This intentionally overlaps accepted_shape_materialization and recovery_error_cost where aggregate error-tree signals disagree with accepted runtime telemetry.",
            select(has_accepted_divergence_cost),
            top_n,
            sort_ratio,
        ),
        failure_family_entry(
            "scanner_token_accounting",
            "Measured rows with tokens=0, zero-iteration accepted divergence evidence. This can overlap slow-performance buckets but is excluded from accepted-shape labels.",
            select(has_token_accounting_evidence),
            top_n,
            sort_ratio,
        ),
        failure_family_entry(
            "unclear_needs_diagnostic",
            "Measured non-clean rows without truncation, error-tree, token-accounting, or accepted-shape evidence.",
            select(
                lambda item: not clean(item)
                and int(item.get("files", 0)) > 0
                and int(item.get("trunc", 0)) == 0
                and int(item.get("errTree", 0)) == 0
                and not has_terminal_timeout_or_fail(item)
                and not has_runtime_frontier_stop(item)
                and not has_token_accounting_evidence(item)
                and not has_oracle_improvement(item)
                and not has_accepted_shape_evidence(item)
                and not has_accepted_divergence_cost(item)
            ),
            top_n,
            sort_ratio,
        ),
        failure_family_entry(
            "clean",
            "Measured parity-clean rows without slow-ratio evidence.",
            select(lambda item: clean(item) and not slow(item)),
            top_n,
            sort_ratio,
        ),
        failure_family_entry(
            "clean_but_slow_perf",
            "Measured parity-clean rows whose aggregate or median ratio exceeds the tier-III performance threshold.",
            select(lambda item: clean(item) and slow(item)),
            top_n,
            sort_ratio,
        ),
        failure_family_entry(
            "zero_files_measured",
            "Measured rows with no files counted in the diagnostic scan.",
            select(lambda item: int(item.get("files", 0)) == 0),
            top_n,
            sort_ratio,
        ),
    ]


def load_classification(path: Path) -> dict[str, dict[str, str]]:
    rows: dict[str, dict[str, str]] = {}
    if not path.exists():
        return rows
    for i, line in enumerate(path.read_text(encoding="utf-8").splitlines()):
        if i == 0 and line.startswith("grammar\t"):
            continue
        if not line.strip():
            continue
        fields = line.rstrip("\n").split("\t")
        while len(fields) < 4:
            fields.append("")
        grammar, tier, parity, fix = fields[:4]
        if grammar:
            rows[grammar] = {"grammar": grammar, "tier": tier, "parity": parity, "fix": fix}
    return rows


def stale_hints(
    scan_dir: Path,
    measurements: dict[str, dict[str, Any]],
    classification_path: Path,
) -> dict[str, Any]:
    rows = load_classification(classification_path)
    clean = read_grammar_set(scan_dir / "clean.txt")
    tier_iv = read_grammar_set(scan_dir / "tier_iv.txt")
    unmeasured = read_grammar_set(scan_dir / "unmeasured.txt")
    non_clean = tier_iv | unmeasured

    clean_with_iv = []
    for grammar in sorted(clean):
        row = rows.get(grammar)
        if row and row["tier"].startswith("IV-"):
            clean_with_iv.append({"grammar": grammar, "tier": row["tier"], "tsvParity": row["parity"]})

    non_clean_stale = []
    for grammar in sorted(non_clean):
        row = rows.get(grammar)
        if row is None:
            non_clean_stale.append({"grammar": grammar, "problem": "missing", "tier": "", "source": "tier_iv" if grammar in tier_iv else "unmeasured"})
            continue
        tier = row["tier"]
        if tier == "CLEAN":
            problem = "clean-row"
        elif tier.startswith("IV-unassessed"):
            problem = "unassessed"
        elif not tier.startswith("IV-"):
            problem = "non-iv-row"
        else:
            continue
        non_clean_stale.append({"grammar": grammar, "problem": problem, "tier": tier, "source": "tier_iv" if grammar in tier_iv else "unmeasured"})

    parity_different_same_denominator = []
    parity_sample_size_different = []
    for grammar, measurement in sorted(measurements.items()):
        row = rows.get(grammar)
        current = measurement.get("parity")
        if not row or not current:
            continue
        tsv_parity = row["parity"]
        if not tsv_parity or tsv_parity == "unmeasured":
            continue
        current_counts = parity_counts(str(current))
        tsv_counts = parity_counts(tsv_parity)
        if current_counts is None or tsv_counts is None:
            continue
        current_matched, current_total = current_counts
        tsv_matched, tsv_total = tsv_counts
        if current_total != tsv_total:
            parity_sample_size_different.append(
                {
                    "grammar": grammar,
                    "currentParity": current,
                    "currentFiles": current_total,
                    "tsvParity": tsv_parity,
                    "tsvFiles": tsv_total,
                    "tier": row["tier"],
                }
            )
            continue
        if current_matched != tsv_matched:
            parity_different_same_denominator.append(
                {
                    "grammar": grammar,
                    "currentParity": current,
                    "tsvParity": tsv_parity,
                    "files": current_total,
                    "tier": row["tier"],
                }
            )

    return {
        "classificationPath": str(classification_path),
        "available": classification_path.exists(),
        "currentCleanWithIVRow": clean_with_iv,
        "currentNonCleanWithStaleRow": non_clean_stale,
        "currentMeasuredParityDiffersFromTSVSameDenominator": parity_different_same_denominator,
        "currentMeasuredParitySampleSizeDiffersFromTSV": parity_sample_size_different,
        "counts": {
            "currentCleanWithIVRow": len(clean_with_iv),
            "currentNonCleanWithStaleRow": len(non_clean_stale),
            "currentMeasuredParityDiffersFromTSVSameDenominator": len(parity_different_same_denominator),
            "currentMeasuredParitySampleSizeDiffersFromTSV": len(parity_sample_size_different),
        },
    }


def build_summary(scan_dir: Path, classification_path: Path, top_n: int, sort_ratio: str) -> dict[str, Any]:
    clean = read_grammar_set(scan_dir / "clean.txt")
    tier_iv = read_grammar_set(scan_dir / "tier_iv.txt")
    unmeasured = read_grammar_set(scan_dir / "unmeasured.txt")
    visited = read_grammar_set(scan_dir / "visited_grammars.txt")
    status_by_grammar = load_status(scan_dir / "status.tsv")
    frames_by_grammar = load_frames_by_grammar(scan_dir / "frames.jsonl")
    measurements = load_measurements(scan_dir, status_by_grammar)
    add_measurement_evidence(measurements, frames_by_grammar)
    unmeasured_entries = summarize_unmeasured(scan_dir, status_by_grammar, frames_by_grammar)

    buckets: dict[str, list[str]] = defaultdict(list)
    for grammar, measurement in measurements.items():
        buckets[signature(measurement)].append(grammar)

    non_clean_measured = []
    for grammar in sorted(tier_iv):
        measurement = measurements.get(grammar)
        if not measurement:
            continue
        if int(measurement.get("files", 0)) <= 0:
            continue
        non_clean_measured.append(measurement)
    ratio_value_key = sort_ratio + "Value"
    top_slow = sorted(
        non_clean_measured,
        key=lambda item: (
            item.get(ratio_value_key) is not None,
            item.get(ratio_value_key) or -1.0,
            item.get("aggRatioValue") or -1.0,
            item.get("medianRatioValue") or -1.0,
        ),
        reverse=True,
    )[:top_n]

    return {
        "scanDir": str(scan_dir),
        "counts": {
            "clean": len(clean),
            "tierIV": len(tier_iv),
            "unmeasured": len(unmeasured),
            "visited": len(visited),
            "measured": len(measurements),
            "signatureBuckets": len(buckets),
        },
        "classificationBuckets": [
            {"signature": key, "grammars": sorted(value), "count": len(value)}
            for key, value in sorted(buckets.items(), key=lambda item: (-len(item[1]), item[0]))
        ],
        "topSlowMeasuredNonClean": [
            compact_measurement(item)
            for item in top_slow
        ],
        "failureFamilies": failure_families(measurements, unmeasured_entries, top_n, sort_ratio),
        "unmeasured": unmeasured_entries,
        "staleClassificationHints": stale_hints(scan_dir, measurements, classification_path),
    }


def md_table(headers: list[str], rows: list[list[Any]]) -> str:
    if not rows:
        return "_None._\n"
    out = ["| " + " | ".join(headers) + " |", "| " + " | ".join("---" for _ in headers) + " |"]
    for row in rows:
        out.append("| " + " | ".join(str(cell).replace("\n", " ") for cell in row) + " |")
    return "\n".join(out) + "\n"


def evidence_cells(item: dict[str, Any]) -> list[Any]:
    evidence = item.get("evidence") or {}
    return [
        ",".join(evidence.get("stopReasons") or []),
        evidence.get("rootErr", ""),
        evidence.get("tokensZeroAcceptedDivergenceCount", ""),
        evidence.get("acceptedDivergenceCount", ""),
        evidence.get("frontierStopCount", ""),
        evidence.get("maxStacks", ""),
        evidence.get("maxNodes", ""),
        ",".join(evidence.get("terminalStatuses") or []),
        evidence.get("lastPhase", ""),
        evidence.get("lastLifecycle", ""),
        evidence.get("lastRc", ""),
        evidence.get("hasComparisonResult", ""),
    ]


def render_markdown(summary: dict[str, Any]) -> str:
    counts = summary["counts"]
    lines = [
        "# Tier Scan Diagnostic Summary",
        "",
        f"Scan directory: `{summary['scanDir']}`",
        "",
        "## Counts",
        "",
        md_table(
            ["clean", "tier IV", "unmeasured", "visited", "measured", "signature buckets"],
            [[counts["clean"], counts["tierIV"], counts["unmeasured"], counts["visited"], counts["measured"], counts["signatureBuckets"]]],
        ),
        "## Classification Buckets",
        "",
    ]
    bucket_rows = [
        [bucket["signature"], bucket["count"], ", ".join(bucket["grammars"])]
        for bucket in summary["classificationBuckets"]
    ]
    lines.append(md_table(["MEASURE-DTIER signature", "count", "grammars"], bucket_rows))
    lines.extend(["## Diagnostic Families", ""])
    family_rows = [
        [family["name"], family["count"], ", ".join(family["grammars"])]
        for family in summary.get("failureFamilies", [])
    ]
    lines.append(md_table(["family", "count", "grammars"], family_rows))
    for family in summary.get("failureFamilies", []):
        lines.extend([f"### {family['name']}", "", family["description"], ""])
        overlap = family.get("topSlowOverlap") or []
        if overlap:
            lines.append(
                md_table(
                    [
                        "grammar",
                        "aggRatio",
                        "medianRatio",
                        "parity",
                        "files",
                        "diverge",
                        "oracleBetter",
                        "trunc",
                        "errTree",
                        "panics",
                        "stop reasons",
                        "rootErr",
                        "tokens0",
                        "acceptedDiv",
                        "frontierStops",
                        "maxStacks",
                        "maxNodes",
                        "terminal",
                        "last phase",
                        "last lifecycle",
                        "last rc",
                        "comparison?",
                    ],
                    [
                        [
                            item["grammar"],
                            item["aggRatio"],
                            item["medianRatio"],
                            item["parity"],
                            item["files"],
                            item["diverge"],
                            item.get("oracleBetter", 0),
                            item["trunc"],
                            item["errTree"],
                            item["panics"],
                        ] + evidence_cells(item)
                        for item in overlap
                    ],
                )
            )
        if family.get("entries"):
            lines.append(
                md_table(
                    ["grammar", "reason", "status", "status detail", "terminal", "last phase", "stopReason", "rootErr", "tokens", "maxStacks"],
                    [
                        [
                            item["grammar"],
                            item.get("reason", ""),
                            (item.get("status") or {}).get("event", ""),
                            (item.get("status") or {}).get("detail", ""),
                            ",".join(((item.get("terminalEvidence") or {}).get("terminalStatuses") or [])),
                            (item.get("lastProgress") or {}).get("phase", ""),
                            (item.get("lastProgress") or {}).get("stopReason", ""),
                            (item.get("lastProgress") or {}).get("rootErr", ""),
                            (item.get("lastProgress") or {}).get("tokens", ""),
                            (item.get("lastProgress") or {}).get("maxStacks", ""),
                        ]
                        for item in family["entries"]
                    ],
                )
            )
        if not overlap and not family.get("entries"):
            lines.append("_No slow measured overlap._\n")
    lines.extend(["## Top Slow Measured Non-Clean", ""])
    slow_rows = [
        [
            item["grammar"],
            item["aggRatio"],
            item["medianRatio"],
            item["parity"],
            item["files"],
            item["diverge"],
            item.get("oracleBetter", 0),
            item["trunc"],
            item["errTree"],
            item["panics"],
        ]
        for item in summary["topSlowMeasuredNonClean"]
    ]
    lines.append(md_table(["grammar", "aggRatio", "medianRatio", "parity", "files", "diverge", "oracleBetter", "trunc", "errTree", "panics"], slow_rows))
    lines.extend(["## Unmeasured Evidence", ""])
    unmeasured_rows = []
    for item in summary["unmeasured"]:
        selected = item.get("selectedFile") or {}
        progress = item.get("lastProgress") or {}
        status = item.get("status") or {}
        terminal = item.get("terminalEvidence") or {}
        unmeasured_rows.append(
            [
                item["grammar"],
                item.get("reason", ""),
                status.get("event", ""),
                status.get("detail", ""),
                ",".join(terminal.get("terminalStatuses") or []),
                selected.get("path", ""),
                progress.get("phase", ""),
                progress.get("elapsedMs", ""),
                progress.get("stopReason", ""),
                progress.get("rootErr", ""),
                progress.get("tokens", ""),
                progress.get("maxStacks", ""),
            ]
        )
    lines.append(md_table(["grammar", "reason", "status", "status detail", "terminal", "selected file", "last phase", "elapsed ms", "stopReason", "rootErr", "tokens", "maxStacks"], unmeasured_rows))
    hints = summary["staleClassificationHints"]
    lines.extend(["## Stale Classification Hints", ""])
    lines.append(
        md_table(
            ["hint", "count"],
            [
                ["current clean with IV row", hints["counts"]["currentCleanWithIVRow"]],
                ["current non-clean with CLEAN/missing/unassessed/non-IV row", hints["counts"]["currentNonCleanWithStaleRow"]],
                ["current measured parity differs from TSV parity (same denominator only)", hints["counts"]["currentMeasuredParityDiffersFromTSVSameDenominator"]],
                ["current measured parity uses different sample size than TSV", hints["counts"]["currentMeasuredParitySampleSizeDiffersFromTSV"]],
            ],
        )
    )
    lines.extend(["### Current Clean With IV Row", ""])
    lines.append(md_table(["grammar", "tier", "tsv parity"], [[item["grammar"], item["tier"], item["tsvParity"]] for item in hints["currentCleanWithIVRow"]]))
    lines.extend(["### Current Non-Clean With Stale Row", ""])
    lines.append(md_table(["grammar", "problem", "tier", "source"], [[item["grammar"], item["problem"], item["tier"], item["source"]] for item in hints["currentNonCleanWithStaleRow"]]))
    lines.extend(["### Current Measured Parity Differs From TSV (Same Denominator Only)", ""])
    lines.append(md_table(["grammar", "current parity", "tsv parity", "files", "tier"], [[item["grammar"], item["currentParity"], item["tsvParity"], item["files"], item["tier"]] for item in hints["currentMeasuredParityDiffersFromTSVSameDenominator"]]))
    lines.extend(["### Current Measured Parity Sample Size Differs From TSV", ""])
    lines.append(md_table(["grammar", "current parity", "current files", "tsv parity", "tsv files", "tier"], [[item["grammar"], item["currentParity"], item["currentFiles"], item["tsvParity"], item["tsvFiles"], item["tier"]] for item in hints["currentMeasuredParitySampleSizeDiffersFromTSV"]]))
    return "\n".join(lines).rstrip() + "\n"


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("scan_dir", type=Path, help="tier scan artifact directory")
    parser.add_argument("--json", dest="json_name", default="diagnostic_summary.json", help="JSON output path or basename")
    parser.add_argument("--markdown", "--md", dest="markdown_name", default="diagnostic_summary.md", help="Markdown output path or basename")
    parser.add_argument("--classification", type=Path, default=repo_root() / "cgo_harness/tier_scan/tier_classification.tsv", help="tier_classification.tsv path")
    parser.add_argument("--top", type=int, default=20, help="number of slow non-clean measured grammars to include")
    parser.add_argument("--sort-ratio", choices=RATIO_FIELDS, default="aggRatio", help="ratio used for slow list ordering")
    parser.add_argument("--print", action="store_true", help="print Markdown summary to stdout")
    parser.add_argument("--no-write", action="store_true", help="do not write JSON or Markdown outputs")
    return parser.parse_args(argv)


def output_path(scan_dir: Path, value: str) -> Path:
    path = Path(value)
    if path.is_absolute() or path.parent != Path("."):
        return path
    return scan_dir / path


def main(argv: list[str]) -> int:
    args = parse_args(argv)
    scan_dir = args.scan_dir.resolve()
    if not scan_dir.is_dir():
        print(f"not a directory: {scan_dir}", file=sys.stderr)
        return 2
    summary = build_summary(scan_dir, args.classification.resolve(), args.top, args.sort_ratio)
    markdown = render_markdown(summary)
    if args.print:
        print(markdown, end="")
    if not args.no_write:
        json_path = output_path(scan_dir, args.json_name)
        markdown_path = output_path(scan_dir, args.markdown_name)
        json_path.write_text(json.dumps(summary, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        markdown_path.write_text(markdown, encoding="utf-8")
        print(f"diagnostic summary json: {json_path}")
        print(f"diagnostic summary markdown: {markdown_path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
