"""Unified cross-model ASR comparison report.

Usage:
    python3 compare_models.py --out <dir>
"""
import argparse, json, pathlib
from datetime import datetime

CATEGORIES = ["zh-technical", "zh-mixed"]
MODELS = ["telespeech", "whisper-baseline", "sensevoice", "gemma4"]
NEW_PROVIDERS = ["openrouter", "gemini"]


def mean(values):
    vals = [v for v in values if v is not None]
    return sum(vals) / len(vals) if vals else None


def fmt(val, digits=3):
    if val is None:
        return "N/A"
    return f"{val:.{digits}f}"


def load_rows(path, model_label):
    rows = []
    with open(path) as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            r = json.loads(line)
            # Normalize category list → string
            cat = r.get("category", "")
            if isinstance(cat, list):
                cat = cat[0] if cat else ""
            r["category"] = cat
            # Remap baseline field names
            if "entity_recall" in r and "entity_recall_exact" not in r:
                r["entity_recall_exact"] = r["entity_recall"]
                r["entity_recall_fuzzy"] = None
            # Remap wer/cer lowercase fields from asr-bench (stored as 'wer'/'cer' not 'WER'/'CER')
            if "wer" in r and "WER" not in r:
                r["WER"] = r["wer"]
            if "cer" in r and "CER" not in r:
                r["CER"] = r["cer"]
            r["_model"] = model_label
            rows.append(r)
    return rows


def compute_stats(rows):
    return {
        "n": len(rows),
        "WER": mean([r.get("WER") for r in rows]),
        "CER": mean([r.get("CER") for r in rows]),
        "entity_recall_exact": mean([r.get("entity_recall_exact") for r in rows]),
        "entity_recall_fuzzy": mean([r.get("entity_recall_fuzzy") for r in rows]),
        "latency_s": mean([r.get("latency_s") for r in rows]),
    }


def build_table(groups, model_names):
    header = "| model | group | N | WER | CER | entity_recall_exact | entity_recall_fuzzy | latency_s |"
    sep    = "|---|---|---|---|---|---|---|---|"
    lines = [header, sep]
    for group in ["all"] + CATEGORIES:
        for model in model_names:
            s = groups.get((model, group))
            if s is None:
                continue
            lines.append(
                f"| {model} | {group} | {s['n']} "
                f"| {fmt(s['WER'])} | {fmt(s['CER'])} "
                f"| {fmt(s['entity_recall_exact'])} "
                f"| {fmt(s['entity_recall_fuzzy'])} "
                f"| {fmt(s['latency_s'])} |"
            )
    return "\n".join(lines)


def auto_discover(results_dir, model_eval_dir, cb_dir, asr_bench_dir):
    rows_by_model = {}

    # SenseVoiceSmall
    sv_files = sorted(pathlib.Path(results_dir).glob("sensevoice-*.jsonl"))
    if sv_files:
        rows_by_model["sensevoice"] = load_rows(sv_files[-1], "sensevoice")

    # gemma4:e4b
    g4_files = sorted(pathlib.Path(results_dir).glob("gemma4-*.jsonl"))
    if g4_files:
        rows_by_model["gemma4"] = load_rows(g4_files[-1], "gemma4")

    # whisper-large-v3 baseline (from contextual-biasing)
    wb_files = sorted(pathlib.Path(cb_dir).glob("run-baseline-*.jsonl"))
    if wb_files:
        rows_by_model["whisper-baseline"] = load_rows(wb_files[-1], "whisper-baseline")

    # TeleSpeechASR (from asr-bench results — filter by model field)
    ts_candidates = sorted(pathlib.Path(asr_bench_dir).glob("*.jsonl"))
    for f in reversed(ts_candidates):
        sample = []
        with open(f) as fh:
            for line in fh:
                line = line.strip()
                if line:
                    sample.append(json.loads(line))
        if sample and sample[0].get("model", "").lower().find("telespeech") >= 0:
            rows_by_model["telespeech"] = load_rows(f, "telespeech")
            break

    # OpenRouter results: openrouter-<model-slug>-<method>-<ts>.jsonl
    for f in sorted(pathlib.Path(results_dir).glob("openrouter-*.jsonl")):
        rows = []
        with open(f) as fh:
            for line in fh:
                line = line.strip()
                if not line:
                    continue
                r = json.loads(line)
                cat = r.get("category", "")
                if isinstance(cat, list):
                    cat = cat[0] if cat else ""
                r["category"] = cat
                rows.append(r)
        if rows:
            model_id = rows[0].get("model", "unknown")
            method = rows[0].get("method", "baseline")
            key = f"openrouter/{model_id}/{method}"
            # keep latest file per key
            rows_by_model[key] = rows

    # Gemini results: gemini-<model-slug>-<method>-<ts>.jsonl
    for f in sorted(pathlib.Path(results_dir).glob("gemini-*.jsonl")):
        rows = []
        with open(f) as fh:
            for line in fh:
                line = line.strip()
                if not line:
                    continue
                r = json.loads(line)
                cat = r.get("category", "")
                if isinstance(cat, list):
                    cat = cat[0] if cat else ""
                r["category"] = cat
                rows.append(r)
        if rows:
            model_id = rows[0].get("model", "unknown")
            method = rows[0].get("method", "baseline")
            key = f"gemini/{model_id}/{method}"
            rows_by_model[key] = rows

    return rows_by_model


def hint_effectiveness_table(groups, new_model_keys):
    """Compare baseline vs hinted entity_recall_exact for each new-provider model."""
    # Collect unique (provider, model_id) pairs
    pairs = {}
    for key in new_model_keys:
        parts = key.split("/", 2)
        if len(parts) == 3:
            provider, model_id, method = parts
            pm = f"{provider}/{model_id}"
            pairs.setdefault(pm, {})
            s = groups.get((key, "all"), {})
            pairs[pm][method] = s

    if not pairs:
        return "No new-provider results found."

    header = "| model | group | baseline entity_recall_exact | hinted entity_recall_exact | delta |"
    sep    = "|---|---|---|---|---|"
    lines = [header, sep]
    for pm in sorted(pairs):
        for group in ["all"] + CATEGORIES:
            base_key = pm + "/baseline"
            hint_key = pm + "/hinted"
            base_s = groups.get((base_key, group))
            hint_s = groups.get((hint_key, group))
            base_er = base_s.get("entity_recall_exact") if base_s else None
            hint_er = hint_s.get("entity_recall_exact") if hint_s else None
            if base_er is None and hint_er is None:
                continue
            delta_str = ""
            if base_er is not None and hint_er is not None:
                delta = hint_er - base_er
                sign = "+" if delta >= 0 else ""
                delta_str = f"{sign}{delta:.3f}"
            lines.append(
                f"| {pm} | {group} | {fmt(base_er)} | {fmt(hint_er)} | {delta_str} |"
            )
    return "\n".join(lines)


def recommendations(groups, model_names):
    lines = []

    def best_by(metric, lower_is_better=False):
        best_model, best_val = None, None
        for m in model_names:
            s = groups.get((m, "all"))
            if s is None:
                continue
            v = s.get(metric)
            if v is None:
                continue
            if best_val is None or (lower_is_better and v < best_val) or (not lower_is_better and v > best_val):
                best_val, best_model = v, m
        return best_model, best_val

    er_model, er_val = best_by("entity_recall_exact", lower_is_better=False)
    wer_model, wer_val = best_by("WER", lower_is_better=True)
    lat_model, lat_val = best_by("latency_s", lower_is_better=True)

    if er_model:
        lines.append(f"- **Best entity recall (exact)**: `{er_model}` ({fmt(er_val)})")
    if wer_model:
        lines.append(f"- **Best WER**: `{wer_model}` ({fmt(wer_val)})")
    if lat_model:
        lines.append(f"- **Lowest latency**: `{lat_model}` ({fmt(lat_val)}s)")

    # SenseVoice vs whisper-baseline on entity_recall_exact
    sv_all = groups.get(("sensevoice", "all"))
    wb_all = groups.get(("whisper-baseline", "all"))
    if sv_all and wb_all:
        sv_er = sv_all.get("entity_recall_exact")
        wb_er = wb_all.get("entity_recall_exact")
        if sv_er is not None and wb_er is not None:
            delta = sv_er - wb_er
            direction = "improved" if delta > 0 else ("matched" if delta == 0 else "degraded")
            lines.append(
                f"- **SenseVoice hotword vs whisper-baseline**: entity_recall_exact {direction} "
                f"by {abs(delta):.3f} ({fmt(wb_er)} → {fmt(sv_er)})"
            )

    return "\n".join(lines) if lines else "- Insufficient data for recommendations."


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--out", required=True)
    args = parser.parse_args()

    _this_dir = pathlib.Path(__file__).resolve().parent
    results_dir = _this_dir / "results"
    cb_dir = (_this_dir / ".." / "contextual-biasing" / "results").resolve()
    asr_bench_dir = (_this_dir / ".." / "asr-bench" / "results").resolve()

    rows_by_model = auto_discover(results_dir, _this_dir, cb_dir, asr_bench_dir)
    model_names = [m for m in MODELS if m in rows_by_model]
    new_model_keys = sorted(k for k in rows_by_model if any(k.startswith(p + "/") for p in NEW_PROVIDERS))
    all_model_keys = model_names + new_model_keys

    # Compute stats
    groups = {}
    for model_label, rows in rows_by_model.items():
        groups[(model_label, "all")] = compute_stats(rows)
        for cat in CATEGORIES:
            cat_rows = [r for r in rows if r.get("category") == cat]
            if cat_rows:
                groups[(model_label, cat)] = compute_stats(cat_rows)

    table = build_table(groups, model_names)
    new_table = build_table(groups, new_model_keys)
    hint_table = hint_effectiveness_table(groups, new_model_keys)
    rec_text = recommendations(groups, all_model_keys)

    ts = datetime.now().strftime("%Y%m%d-%H%M%S")
    out_dir = pathlib.Path(args.out)
    out_dir.mkdir(parents=True, exist_ok=True)
    report_path = out_dir / f"report-{ts}.md"

    report = f"""# ASR Model Comparison Report

Generated: {datetime.now().isoformat()}

## Models Compared (Legacy)

- **telespeech**: TeleSpeechASR via SiliconFlow (TASK-34 baseline)
- **whisper-baseline**: openai/whisper-large-v3, no prompt (TASK-36 baseline)
- **sensevoice**: FunAudioLLM/SenseVoiceSmall via SiliconFlow with hotword prompt (TASK-37)
- **gemma4**: gemma4:e4b via Ollama local (TASK-37)

## Legacy Results

{table}

## New Provider Results (OpenRouter & Gemini)

{new_table}

## Hint 有效性分析 (baseline vs hinted entity_recall_exact)

{hint_table}

## Recommendations

{rec_text}

## Notes

- `WER`: Word Error Rate (lower is better); computed only for cases with `reference` field
- `CER`: Character Error Rate (lower is better); same
- `entity_recall_exact`: fraction of known_entities found verbatim (case-insensitive substring)
- `entity_recall_fuzzy`: fraction matched via difflib fuzzy similarity (threshold=0.3)
- `latency_s`: mean wall-clock seconds per transcription call
- Categories: `zh-technical` = technical Chinese terms with English entity names, `zh-mixed` = mixed Chinese+English speech
- `method`: `baseline` = no entity hint; `hinted` = known_entities injected into prompt/text field
"""

    report_path.write_text(report, encoding="utf-8")
    print(f"Wrote {report_path}")
    print(table)


if __name__ == "__main__":
    main()
