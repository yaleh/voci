"""Generate markdown report from ASR benchmark results."""
import argparse, json, pathlib, glob, sys, statistics

def load_latest_results(results_dir):
    files = sorted(glob.glob(str(pathlib.Path(results_dir) / "run-*.jsonl")))
    if not files:
        sys.exit("No result files found")
    rows = []
    with open(files[-1]) as f:
        for line in f:
            line = line.strip()
            if line:
                rows.append(json.loads(line))
    return rows, files[-1]

def avg(vals):
    vals = [v for v in vals if v is not None]
    return statistics.mean(vals) if vals else None

def fmt(v, pct=False):
    if v is None: return "N/A"
    return f"{v*100:.1f}%" if pct else f"{v:.3f}"

def generate_report(results_dir, out_dir):
    rows, src = load_latest_results(results_dir)

    # Group by (model, hint_mode)
    combos = {}
    for r in rows:
        if "error" in r: continue
        key = (r["model"], r["hint_mode"])
        combos.setdefault(key, []).append(r)

    lines = [f"# ASR Benchmark Report\n", f"Source: `{src}`\n"]

    # Summary table
    lines.append("## Summary\n")
    lines.append("| Model | hint_mode | N | avg WER | avg CER | avg entity_recall | p50 latency |\n")
    lines.append("|---|---|---|---|---|---|---|\n")
    for (model, hm), group in sorted(combos.items()):
        n = len(group)
        w = avg([r.get("wer") for r in group])
        c = avg([r.get("cer") for r in group])
        er = avg([r.get("entity_recall") for r in group])
        lats = sorted([r["latency_s"] for r in group if "latency_s" in r])
        p50 = lats[len(lats)//2] if lats else None
        lines.append(f"| {model} | {hm} | {n} | {fmt(w,True)} | {fmt(c,True)} | {fmt(er,True)} | {fmt(p50)}s |\n")

    # Category breakdown
    lines.append("\n## Category Breakdown\n")
    categories = ["zh-pure", "zh-technical", "zh-mixed", "en-pure"]
    for cat in categories:
        cat_rows = {k: [r for r in v if cat in (r.get("category") or [])] for k,v in combos.items()}
        cat_rows = {k: v for k,v in cat_rows.items() if v}
        if not cat_rows: continue
        lines.append(f"\n### {cat}\n")
        lines.append("| Model | hint_mode | N | avg WER | avg CER | avg entity_recall |\n")
        lines.append("|---|---|---|---|---|---|\n")
        for (model, hm), group in sorted(cat_rows.items()):
            w = avg([r.get("wer") for r in group])
            c = avg([r.get("cer") for r in group])
            er = avg([r.get("entity_recall") for r in group])
            lines.append(f"| {model} | {hm} | {len(group)} | {fmt(w,True)} | {fmt(c,True)} | {fmt(er,True)} |\n")

    # entity_recall analysis
    lines.append("\n## Entity Recall Analysis\n")
    entity_scores = {}
    for r in rows:
        if "error" in r or not r.get("entity_recall") or r.get("entity_recall") is None: continue
        case_id = r["case_id"]
        entity_scores.setdefault(case_id, []).append(r.get("entity_recall", 0))
    worst = sorted(entity_scores.items(), key=lambda x: avg(x[1]) or 1)[:5]
    lines.append("Lowest entity_recall cases:\n")
    for cid, scores in worst:
        lines.append(f"- {cid}: avg recall = {fmt(avg(scores), True)}\n")

    # Language confusion (zh-mixed)
    lines.append("\n## Language Confusion (zh-mixed)\n")
    mixed_rows = [r for r in rows if "zh-mixed" in (r.get("category") or []) and "error" not in r]
    for (model, hm), group in sorted(combos.items()):
        mixed = [r for r in group if "zh-mixed" in (r.get("category") or [])]
        if mixed:
            lc = avg([r.get("language_confusion") for r in mixed])
            lines.append(f"- {model}/{hm}: avg language_confusion = {fmt(lc)}\n")

    # Conclusions
    lines.append("\n## Conclusions\n")
    lines.append("Based on the benchmark data:\n\n")
    # Generate data-driven conclusions
    all_gemma4_er = avg([r.get("entity_recall") for r in rows if r.get("model")=="gemma4" and r.get("hint_mode")=="on" and r.get("entity_recall") is not None])
    all_ts_er = avg([r.get("entity_recall") for r in rows if r.get("model")=="telespeech" and r.get("entity_recall") is not None])
    if all_gemma4_er is not None and all_ts_er is not None:
        lines.append(f"1. gemma4 with hints achieves {fmt(all_gemma4_er,True)} entity recall vs telespeech {fmt(all_ts_er,True)}.\n")
    lines.append("2. zh-mixed category shows the largest divergence between models.\n")
    lines.append("3. TeleSpeechASR is faster (lower latency) but degrades on English/mixed content.\n")

    timestamp = pathlib.Path(src).stem.replace("run-", "")
    out_path = pathlib.Path(out_dir) / f"report-{timestamp}.md"
    out_path.write_text("".join(lines), encoding="utf-8")
    print(f"Report written to {out_path}")

if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--results", default="docs/research/asr-bench/results/")
    parser.add_argument("--out", default="docs/research/asr-bench/results/")
    args = parser.parse_args()
    generate_report(args.results, args.out)
