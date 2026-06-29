"""Run Gemini ASR against all 35 testcases. Execute from project root.

Usage:
  python3 docs/research/model-eval/run_gemini.py --model gemini-2.5-flash --method baseline
  python3 docs/research/model-eval/run_gemini.py --model gemini-2.5-pro --method hinted
"""
import argparse, json, pathlib, sys, importlib.util
from datetime import datetime

_this_dir = pathlib.Path(__file__).resolve().parent
_asr_bench_dir = (_this_dir / '..' / 'asr-bench').resolve()
_cb_dir = (_this_dir / '..' / 'contextual-biasing').resolve()

_base_spec = importlib.util.spec_from_file_location(
    "base", str(_asr_bench_dir / 'adapters' / 'base.py'))
_base_mod = importlib.util.module_from_spec(_base_spec)
sys.modules['base'] = _base_mod
_base_spec.loader.exec_module(_base_mod)
TranscribeOpts = _base_mod.TranscribeOpts

_g_spec = importlib.util.spec_from_file_location(
    "gemini_adapter", str(_this_dir / 'adapters' / 'gemini_adapter.py'))
_g_mod = importlib.util.module_from_spec(_g_spec)
_g_spec.loader.exec_module(_g_mod)
GeminiAdapter = _g_mod.GeminiAdapter

sys.path.insert(0, str(_asr_bench_dir))
sys.path.insert(0, str(_cb_dir))
from metrics import wer, cer, entity_recall as exact_entity_recall
from metrics_ext import fuzzy_entity_recall


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--model", default="gemini-2.5-flash")
    parser.add_argument("--method", choices=["baseline", "hinted"], default="baseline")
    args = parser.parse_args()

    cases = json.loads(pathlib.Path("testdata/testcases.json").read_text())
    adapter = GeminiAdapter(model=args.model)
    results_dir = _this_dir / 'results'
    results_dir.mkdir(parents=True, exist_ok=True)

    model_slug = args.model.replace("/", "-")
    ts = datetime.now().strftime("%Y%m%d-%H%M%S")
    out_path = results_dir / f"gemini-{model_slug}-{args.method}-{ts}.jsonl"

    with out_path.open("w") as out:
        for case in cases:
            wav_path = f"testdata/{case['id']}.wav"
            entities = case.get("known_entities", [])
            opts = TranscribeOpts(
                language=case.get("language", ""),
                known_entities=entities if args.method == "hinted" else [],
            )
            try:
                hypothesis, latency = adapter.transcribe(wav_path, opts)
            except Exception as e:
                print(f"ERROR {case['id']}: {e}")
                hypothesis, latency = "", 0.0

            ref = case.get("reference", "")
            cat = case.get("category", [])
            category_str = cat[0] if isinstance(cat, list) and cat else (cat or "")

            row = {
                "case_id": case["id"],
                "provider": "gemini",
                "model": args.model,
                "method": args.method,
                "hypothesis": hypothesis,
                "latency_s": round(latency, 3),
                "WER": round(wer(ref, hypothesis), 3) if ref else None,
                "CER": round(cer(ref, hypothesis), 3) if ref else None,
                "entity_recall_exact": exact_entity_recall(entities, hypothesis),
                "entity_recall_fuzzy": fuzzy_entity_recall(entities, hypothesis),
                "category": category_str,
                "reference": ref,
            }
            out.write(json.dumps(row, ensure_ascii=False) + "\n")
            print(f"{case['id']} WER={row['WER']} entity_recall_exact={row['entity_recall_exact']}")

    print(f"Wrote {out_path}")


if __name__ == "__main__":
    main()
