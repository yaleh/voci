"""
RunHinted prompt format experiment.

Tests three prompt format variants (A: output-only JSON, B: full JSON I/O,
C: XML tags) against the boundary-violation scenario where the LLM tends to
"answer" a user question instead of correcting the ASR transcription.

Calls Google Gemini API directly. API key is read from a YAML config file
(default: ~/.config/voci/config.yaml, key: asr_api_key) or GEMINI_API_KEY env var.

Usage:
  python run_experiment.py                         # default config, 3 reps
  python run_experiment.py --config ~/.config/voci/config-gemini.yaml
  python run_experiment.py --dry-run               # print prompts, no API calls
  python run_experiment.py --reps 5                # repeat each scenario N times
  python run_experiment.py --model gemini-2.5-pro  # override model
"""

import argparse
import json
import os
import pathlib
import sys
import time

import requests

DEFAULT_CONFIG = pathlib.Path.home() / ".config" / "voci" / "config.yaml"
DEFAULT_MODEL = "gemini-2.5-flash"
GEMINI_API_BASE = "https://generativelanguage.googleapis.com/v1beta/models"

OUT_DIR = pathlib.Path(__file__).parent
RESULTS_FILE = OUT_DIR / "results.jsonl"

# ── Config loading ─────────────────────────────────────────────────────────────

def load_api_key(config_path: pathlib.Path) -> str:
    key = os.environ.get("GEMINI_API_KEY", "")
    if key:
        return key
    try:
        import yaml
        data = yaml.safe_load(config_path.read_text())
        return data.get("asr_api_key", "") or ""
    except Exception as exc:
        print(f"WARNING: could not load config {config_path}: {exc}", file=sys.stderr)
        return ""

# ── Hint shared across all scenarios ─────────────────────────────────────────

BOUNDARY_HINT = """## Active Tasks
- TASK-49: Run RunHinted prompt format experiment
- TASK-48: Add Cloudflare named tunnel support
- TASK-47: Volcengine ASR adapter

## Known Entities
- task forty nine: TASK-49
- task forty eight: TASK-48
- task forty seven: TASK-47
"""

NORMAL_HINT = """## Known Entities
- task forty nine: TASK-49
- voci: voci
- asr bench: asr-bench
"""

# ── Scenarios ─────────────────────────────────────────────────────────────────

SCENARIOS = [
    {
        "scenario_id": "boundary_violation",
        "description": "LLM should correct ASR, not list tasks",
        "raw": "列出现有 task，并建议执行顺序",
        "hint": BOUNDARY_HINT,
    },
    {
        "scenario_id": "normal_correction",
        "description": "Correct spoken task ID to canonical form",
        "raw": "修复 task forty nine 的 bug",
        "hint": NORMAL_HINT,
    },
    {
        "scenario_id": "empty_hint",
        "description": "No hint — minimal intervention expected",
        "raw": "今天天气怎么样",
        "hint": "",
    },
]

# ── Prompt builders (mirrors Go implementation) ───────────────────────────────

def build_variant_a(raw: str, hint: str) -> dict:
    system = (
        "You are an ASR correction assistant.\n\n"
        "## Instructions\n"
        "Correct ASR transcription errors using the hint context below.\n"
        "For each entry in '## Known Entities' formatted as `spoken-form: canonical-form`,\n"
        "replace occurrences of the spoken-form with the exact canonical spelling.\n"
        "Apply all substitutions first, then fix remaining grammar.\n"
        "When multiple candidates of the same kind could match a phrase, choose the candidate "
        "whose spoken form most closely matches the exact words in the transcription.\n"
        "Only substitute a package path such as 'internal/xxx' when the transcription explicitly "
        "refers to a Go package or import path.\n\n"
        "## Output Format\n"
        'Return ONLY a JSON object with this exact structure:\n{"corrected": "<corrected transcription here>"}\n'
        "Do not include any other text, explanation, or markdown.\n"
    )
    if hint:
        system += "\n" + hint
    return {"system": system, "user": f"Transcription: {raw}"}


def build_variant_b(raw: str, hint: str) -> dict:
    system = (
        "You are an ASR correction assistant.\n\n"
        "## Instructions\n"
        "You receive a JSON object with a 'raw_transcript' field (the ASR output to correct)\n"
        "and a 'context' field (the correction hint with Known Entities).\n"
        "For each entry in Known Entities formatted as `spoken-form: canonical-form`,\n"
        "replace occurrences of the spoken-form in raw_transcript with the exact canonical spelling.\n"
        "Apply all substitutions first, then fix remaining grammar.\n"
        "When multiple candidates of the same kind could match a phrase, choose the candidate "
        "whose spoken form most closely matches the exact words in raw_transcript.\n"
        "Only substitute a package path such as 'internal/xxx' when raw_transcript explicitly "
        "refers to a Go package or import path.\n\n"
        "## Output Format\n"
        'Return ONLY a JSON object with this exact structure:\n{"corrected": "<corrected transcription here>"}\n'
        "Do not include any other text, explanation, or markdown.\n"
    )
    user = json.dumps({"raw_transcript": raw, "context": hint}, ensure_ascii=False)
    return {"system": system, "user": user}


def build_variant_c(raw: str, hint: str) -> dict:
    system = (
        "You are an ASR correction assistant.\n\n"
        "## Instructions\n"
        "You receive input wrapped in XML tags:\n"
        "- <raw_transcript>: the ASR output to correct\n"
        "- <context>: the correction hint with Known Entities\n"
        "For each entry in Known Entities formatted as `spoken-form: canonical-form`,\n"
        "replace occurrences of the spoken-form in raw_transcript with the exact canonical spelling.\n"
        "Apply all substitutions first, then fix remaining grammar.\n"
        "When multiple candidates of the same kind could match a phrase, choose the candidate "
        "whose spoken form most closely matches the exact words in raw_transcript.\n"
        "Only substitute a package path such as 'internal/xxx' when raw_transcript explicitly "
        "refers to a Go package or import path.\n\n"
        "## Output Format\n"
        "Return ONLY a single XML element with this exact structure:\n"
        "<corrected>corrected transcription here</corrected>\n"
        "Do not include any other text, explanation, or markdown.\n"
    )
    user = f"<raw_transcript>{raw}</raw_transcript>"
    if hint:
        user += f"\n<context>{hint}</context>"
    return {"system": system, "user": user}


VARIANT_BUILDERS = {
    "A": build_variant_a,
    "B": build_variant_b,
    "C": build_variant_c,
}

# ── Gemini API call ───────────────────────────────────────────────────────────

def call_gemini(prompt: dict, api_key: str, model: str) -> tuple[str, float]:
    url = f"{GEMINI_API_BASE}/{model}:generateContent?key={api_key}"
    body = {
        "system_instruction": {"parts": [{"text": prompt["system"]}]},
        "contents": [{"role": "user", "parts": [{"text": prompt["user"]}]}],
        "generationConfig": {"temperature": 0.0},
    }
    start = time.time()
    resp = requests.post(url, json=body, timeout=60)
    latency = time.time() - start
    resp.raise_for_status()
    data = resp.json()
    text = data["candidates"][0]["content"]["parts"][0]["text"]
    return text, latency

# ── Response analysis ─────────────────────────────────────────────────────────

TASK_LIST_MARKERS = ["TASK-4", "执行顺序", "建议顺序", "1.", "2.", "以下", "如下", "•"]


def contains_task_list(output: str, scenario_id: str) -> bool:
    if scenario_id != "boundary_violation":
        return False
    has_task_id = any(m in output for m in ["TASK-49", "TASK-48", "TASK-47"])
    has_list_structure = any(k in output for k in TASK_LIST_MARKERS)
    return has_task_id and has_list_structure


def parse_output(variant: str, raw_output: str) -> str:
    text = raw_output.strip()
    if variant in ("A", "B"):
        if text.startswith("```"):
            lines = text.strip("`").splitlines()
            text = "\n".join(lines[1:] if lines and lines[0].startswith(("json", "")) else lines)
        try:
            obj = json.loads(text)
            return obj.get("corrected", text)
        except json.JSONDecodeError:
            return text
    elif variant == "C":
        import re
        m = re.search(r"<corrected>(.*?)</corrected>", text, re.DOTALL)
        if m:
            return m.group(1).strip()
        return text
    return text

# ── Main ──────────────────────────────────────────────────────────────────────

def run_experiment(args, api_key: str):
    results = []

    for scenario in SCENARIOS:
        for variant_name, builder in VARIANT_BUILDERS.items():
            for rep in range(args.reps):
                prompt = builder(scenario["raw"], scenario["hint"])

                if args.dry_run:
                    print(f"\n{'='*60}")
                    print(f"Variant {variant_name} | {scenario['scenario_id']} | rep {rep}")
                    print(f"[system] {prompt['system'][:300]}")
                    print(f"[user]   {prompt['user'][:300]}")
                    continue

                try:
                    raw_output, latency = call_gemini(prompt, api_key, args.model)
                except Exception as exc:
                    print(f"ERROR variant={variant_name} scenario={scenario['scenario_id']}: {exc}",
                          file=sys.stderr)
                    raw_output = ""
                    latency = 0.0

                corrected = parse_output(variant_name, raw_output)
                boundary_violated = contains_task_list(raw_output, scenario["scenario_id"])

                record = {
                    "variant": variant_name,
                    "scenario_id": scenario["scenario_id"],
                    "rep": rep,
                    "raw": scenario["raw"],
                    "hint_len": len(scenario["hint"]),
                    "output": raw_output,
                    "corrected": corrected,
                    "contains_task_list": boundary_violated,
                    "latency_s": round(latency, 3),
                    "model": args.model,
                }
                results.append(record)
                status = "VIOLATED" if boundary_violated else "ok"
                print(f"variant={variant_name} scenario={scenario['scenario_id']} rep={rep} "
                      f"boundary={status} latency={latency:.2f}s corrected={corrected!r}")

    if args.dry_run:
        return

    RESULTS_FILE.parent.mkdir(parents=True, exist_ok=True)
    with open(RESULTS_FILE, "w") as f:
        for r in results:
            f.write(json.dumps(r, ensure_ascii=False) + "\n")
    print(f"\nWrote {len(results)} records to {RESULTS_FILE}")


def main():
    parser = argparse.ArgumentParser(description="RunHinted format experiment (Gemini API)")
    parser.add_argument("--config", type=pathlib.Path, default=DEFAULT_CONFIG,
                        help=f"YAML config file with asr_api_key (default: {DEFAULT_CONFIG})")
    parser.add_argument("--model", default=DEFAULT_MODEL,
                        help=f"Gemini model name (default: {DEFAULT_MODEL})")
    parser.add_argument("--dry-run", action="store_true",
                        help="Print prompts without calling API")
    parser.add_argument("--reps", type=int, default=3,
                        help="Repetitions per scenario×variant (default: 3)")
    args = parser.parse_args()

    api_key = load_api_key(args.config)
    if not api_key and not args.dry_run:
        print(f"ERROR: no API key found. Set GEMINI_API_KEY or asr_api_key in {args.config}",
              file=sys.stderr)
        sys.exit(1)

    run_experiment(args, api_key)


if __name__ == "__main__":
    main()
