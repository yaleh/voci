"""ASR benchmark runner. Outputs JSONL results."""
import argparse, json, os, sys, pathlib, time, glob
sys.path.insert(0, str(pathlib.Path(__file__).parent))

from adapters.gemma4 import Gemma4Adapter
from adapters.telespeech import TeleSpeechAdapter
from adapters.volcengine import VolcengineASRAdapter
from metrics import wer, cer, entity_recall, language_confusion

def load_cases(cases_path):
    with open(cases_path) as f:
        return json.load(f)

def run_benchmark(args):
    cases = load_cases(args.cases)

    if args.dry_run:
        print(f"{len(cases)} cases loaded")
        return

    adapters = []
    if args.models in ("all", "telespeech"):
        try:
            adapters.append(TeleSpeechAdapter())
        except RuntimeError as e:
            print(f"WARNING: telespeech unavailable: {e}", file=sys.stderr)
    if args.models in ("all", "gemma4"):
        adapters.append(Gemma4Adapter())
    if args.models in ("all", "volcengine"):
        try:
            adapters.append(VolcengineASRAdapter())
        except RuntimeError as e:
            print(f"WARNING: volcengine unavailable: {e}", file=sys.stderr)

    timestamp = time.strftime("%Y%m%d-%H%M%S")
    out_dir = pathlib.Path(args.out)
    out_dir.mkdir(parents=True, exist_ok=True)
    out_file = out_dir / f"run-{timestamp}.jsonl"

    results = []
    wav_dir = pathlib.Path(args.cases).parent

    for adapter in adapters:
        hint_modes = ["off", "on"] if adapter.supports_hints else ["off"]
        for hint_mode in hint_modes:
            for case in cases:
                wav_path = str(wav_dir / f"{case['id']}.wav")
                if not os.path.exists(wav_path):
                    continue

                from adapters.base import TranscribeOpts
                opts = TranscribeOpts(
                    language=case.get("language", ""),
                    known_entities=case.get("known_entities", []) if hint_mode == "on" else [],
                    system_prompt="" if hint_mode == "off" else "",
                )

                try:
                    hypothesis, latency = adapter.transcribe(wav_path, opts)
                    reference = case.get("reference") or case.get("text", "")
                    row = {
                        "case_id": case["id"],
                        "model": adapter.name,
                        "hint_mode": hint_mode,
                        "hypothesis": hypothesis,
                        "latency_s": round(latency, 3),
                        "wer": round(wer(reference, hypothesis), 4) if reference else None,
                        "cer": round(cer(reference, hypothesis), 4) if reference else None,
                        "entity_recall": entity_recall(case.get("known_entities", []), hypothesis),
                        "language_confusion": language_confusion(reference, hypothesis) if reference else None,
                        "category": case.get("category", []),
                    }
                except Exception as e:
                    row = {
                        "case_id": case["id"], "model": adapter.name,
                        "hint_mode": hint_mode, "error": str(e),
                        "category": case.get("category", []),
                    }

                results.append(row)
                with open(out_file, "a") as f:
                    f.write(json.dumps(row, ensure_ascii=False) + "\n")
                print(f"  {adapter.name}/{hint_mode} {case['id']}: {row.get('hypothesis','ERROR')[:50]}")

    print(f"\nResults written to {out_file} ({len(results)} rows)")

if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--models", default="all", choices=["all", "telespeech", "gemma4", "volcengine"])
    parser.add_argument("--cases", default="testdata/testcases.json")
    parser.add_argument("--out", default="docs/research/asr-bench/results/")
    parser.add_argument("--config", default="", metavar="PATH",
                        help="Path to voci config YAML (overrides ~/.config/voci/config.yaml)")
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()
    if args.config:
        os.environ["VOCI_CONFIG"] = args.config
    run_benchmark(args)
