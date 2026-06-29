"""Run whisper-large-v3 baseline (no prompt) for contextual-biasing experiment."""
import json, pathlib, sys, os, time, importlib.util
from datetime import datetime

_this_dir = pathlib.Path(__file__).resolve().parent
_asr_bench_dir = (_this_dir / '..' / 'asr-bench').resolve()
_asr_bench_adapters_dir = (_asr_bench_dir / 'adapters').resolve()

# Load asr-bench base adapter directly to avoid namespace collision
_base_spec = importlib.util.spec_from_file_location("base", str(_asr_bench_adapters_dir / 'base.py'))
_base_mod = importlib.util.module_from_spec(_base_spec)
sys.modules['base'] = _base_mod
_base_spec.loader.exec_module(_base_mod)
TranscribeOpts = _base_mod.TranscribeOpts

# Load contextual-biasing whisper_biased adapter (no prompt = no known_entities passed)
# The adapter auto-loads API key from ~/.config/voci/config.yaml as fallback
_biased_spec = importlib.util.spec_from_file_location(
    "whisper_biased", str(_this_dir / 'adapters' / 'whisper_biased.py'))
_biased_mod = importlib.util.module_from_spec(_biased_spec)
_biased_spec.loader.exec_module(_biased_mod)
WhisperBiasedAdapter = _biased_mod.WhisperBiasedAdapter

# Load metrics_ext
sys.path.insert(0, str(_this_dir))
from metrics_ext import fuzzy_entity_recall

# Load asr-bench metrics
sys.path.insert(0, str(_asr_bench_dir))
from metrics import entity_recall as exact_entity_recall


def main():
    cases_path = pathlib.Path("testdata/testcases.json")
    cases = json.loads(cases_path.read_text())
    cases = [c for c in cases if c.get("known_entities")]

    # Use WhisperBiasedAdapter but pass empty known_entities → no prompt
    adapter = WhisperBiasedAdapter()
    results_dir = pathlib.Path("docs/research/contextual-biasing/results")
    results_dir.mkdir(parents=True, exist_ok=True)
    ts = datetime.now().strftime("%Y%m%d-%H%M%S")
    out_path = results_dir / f"run-baseline-{ts}.jsonl"

    with out_path.open("w") as out:
        for case in cases:
            wav_path = f"testdata/{case['id']}.wav"
            if not pathlib.Path(wav_path).exists():
                print(f"SKIP {case['id']}: wav not found")
                continue
            # No known_entities → no prompt → baseline
            opts = TranscribeOpts(
                language=case.get("language", ""),
                known_entities=[],
                prompt="",
                system_prompt=""
            )
            try:
                hypothesis, latency = adapter.transcribe(wav_path, opts)
            except Exception as e:
                hypothesis, latency = "", 0.0
                print(f"ERROR {case['id']}: {e}")
            ref = case.get("reference", "")
            er_exact = exact_entity_recall(case.get("known_entities", []), hypothesis)
            er_fuzzy = fuzzy_entity_recall(case.get("known_entities", []), hypothesis)
            cat = case.get("category", [])
            category_str = cat[0] if isinstance(cat, list) and cat else (cat or "")
            row = {
                "case_id": case["id"],
                "method": "baseline",
                "model": "whisper-large-v3",
                "hint_mode": "off",
                "hypothesis": hypothesis,
                "latency_s": round(latency, 3),
                "entity_recall": er_exact,
                "entity_recall_exact": er_exact,
                "entity_recall_fuzzy": er_fuzzy,
                "category": category_str,
                "reference": ref,
            }
            out.write(json.dumps(row, ensure_ascii=False) + "\n")
            fuzzy_str = f"{er_fuzzy:.2f}" if er_fuzzy is not None else "N/A"
            print(f"{case['id']} entity_recall_exact={er_exact:.2f} entity_recall_fuzzy={fuzzy_str}")

    print(f"Wrote {out_path}")

if __name__ == "__main__":
    main()
