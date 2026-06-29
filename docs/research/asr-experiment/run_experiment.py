"""Phase 2: Run 4 hint format configs (A/B/C/D) against the 30-entry corpus.

Usage:
  python3 docs/research/asr-experiment/run_experiment.py
"""
import sys, os, json, pathlib, importlib.util, time

# ── Path setup ──────────────────────────────────────────────────────────────
_this_dir = pathlib.Path(__file__).resolve().parent
_project_root = (_this_dir / '..' / '..' / '..').resolve()
_adapters_dir = (_project_root / 'docs' / 'research' / 'model-eval' / 'adapters').resolve()
_asr_bench_dir = (_project_root / 'docs' / 'research' / 'asr-bench').resolve()

# Load base.py into sys.modules so gemini_adapter.py can import it
_base_path = str(_asr_bench_dir / 'adapters' / 'base.py')
if 'base' not in sys.modules:
    _spec = importlib.util.spec_from_file_location("base", _base_path)
    _mod = importlib.util.module_from_spec(_spec)
    sys.modules['base'] = _mod
    _spec.loader.exec_module(_mod)
from base import TranscribeOpts  # noqa: E402

# Load GeminiAdapter
_ga_spec = importlib.util.spec_from_file_location(
    "gemini_adapter", str(_adapters_dir / 'gemini_adapter.py'))
_ga_mod = importlib.util.module_from_spec(_ga_spec)
_ga_spec.loader.exec_module(_ga_mod)
GeminiAdapter = _ga_mod.GeminiAdapter

# ── Config subclasses ────────────────────────────────────────────────────────

class ConfigA(GeminiAdapter):
    """Plain-text entity list (TASK-40 reproduction baseline)."""
    config_name = "A"

    def transcribe(self, wav_path: str, opts: TranscribeOpts):
        import base64, json as _json, urllib.request, pathlib as _pathlib
        audio_b64 = base64.b64encode(_pathlib.Path(wav_path).read_bytes()).decode()
        if opts.known_entities:
            text_prompt = (
                "Transcribe the following audio. Known technical terms: "
                + ", ".join(opts.known_entities)
            )
        else:
            text_prompt = "Transcribe the following audio."

        body = {
            "contents": [{
                "parts": [
                    {"text": text_prompt},
                    {"inlineData": {"mimeType": "audio/wav", "data": audio_b64}},
                ]
            }]
        }

        url = _ga_mod._URL_TEMPLATE.format(model=self.model)
        payload = _json.dumps(body).encode()
        req = urllib.request.Request(
            url,
            data=payload,
            headers={"x-goog-api-key": self.api_key, "Content-Type": "application/json"},
            method="POST",
        )
        t0 = time.time()
        with urllib.request.urlopen(req, timeout=60) as resp:
            result = _json.loads(resp.read())
        latency = time.time() - t0
        candidates = result.get("candidates", [])
        prompt_tokens = result.get("usageMetadata", {}).get("promptTokenCount", None)
        if not candidates:
            return "", latency, prompt_tokens
        parts = candidates[0].get("content", {}).get("parts", [])
        return parts[0].get("text", "") if parts else "", latency, prompt_tokens


class ConfigB(GeminiAdapter):
    """XML-tagged entities + explicit instruction prefix."""
    config_name = "B"

    def transcribe(self, wav_path: str, opts: TranscribeOpts):
        import base64, json as _json, urllib.request, pathlib as _pathlib
        audio_b64 = base64.b64encode(_pathlib.Path(wav_path).read_bytes()).decode()
        if opts.known_entities:
            entities_xml = "\n".join(f"  <entity>{e}</entity>" for e in opts.known_entities)
            text_prompt = (
                "Transcribe the following audio exactly. The transcript MUST preserve the "
                "spelling of the following technical terms if they appear in the audio:\n"
                f"<entities>\n{entities_xml}\n</entities>"
            )
        else:
            text_prompt = "Transcribe the following audio exactly."

        body = {
            "contents": [{
                "parts": [
                    {"text": text_prompt},
                    {"inlineData": {"mimeType": "audio/wav", "data": audio_b64}},
                ]
            }]
        }

        url = _ga_mod._URL_TEMPLATE.format(model=self.model)
        payload = _json.dumps(body).encode()
        req = urllib.request.Request(
            url,
            data=payload,
            headers={"x-goog-api-key": self.api_key, "Content-Type": "application/json"},
            method="POST",
        )
        t0 = time.time()
        with urllib.request.urlopen(req, timeout=60) as resp:
            result = _json.loads(resp.read())
        latency = time.time() - t0
        candidates = result.get("candidates", [])
        prompt_tokens = result.get("usageMetadata", {}).get("promptTokenCount", None)
        if not candidates:
            return "", latency, prompt_tokens
        parts = candidates[0].get("content", {}).get("parts", [])
        return parts[0].get("text", "") if parts else "", latency, prompt_tokens


class ConfigC(GeminiAdapter):
    """Few-shot example showing correct entity preservation."""
    config_name = "C"

    def transcribe(self, wav_path: str, opts: TranscribeOpts):
        import base64, json as _json, urllib.request, pathlib as _pathlib
        audio_b64 = base64.b64encode(_pathlib.Path(wav_path).read_bytes()).decode()
        if opts.known_entities:
            text_prompt = (
                "Transcribe the following audio. Below is an example of correct output format:\n\n"
                "Example — if the audio contains the phrase \"我们用 Sentry 来监控\" and the "
                "known term is \"Sentry\", the correct transcript is:\n"
                "\"我们用 Sentry 来监控\"\n\n"
                "Known technical terms: " + ", ".join(opts.known_entities) + "\n\n"
                "Now transcribe the actual audio:"
            )
        else:
            text_prompt = (
                "Transcribe the following audio. Below is an example of correct output format:\n\n"
                "Example — if the audio contains the phrase \"我们用 Sentry 来监控\", the correct "
                "transcript is:\n\"我们用 Sentry 来监控\"\n\n"
                "Now transcribe the actual audio:"
            )

        body = {
            "contents": [{
                "parts": [
                    {"text": text_prompt},
                    {"inlineData": {"mimeType": "audio/wav", "data": audio_b64}},
                ]
            }]
        }

        url = _ga_mod._URL_TEMPLATE.format(model=self.model)
        payload = _json.dumps(body).encode()
        req = urllib.request.Request(
            url,
            data=payload,
            headers={"x-goog-api-key": self.api_key, "Content-Type": "application/json"},
            method="POST",
        )
        t0 = time.time()
        with urllib.request.urlopen(req, timeout=60) as resp:
            result = _json.loads(resp.read())
        latency = time.time() - t0
        candidates = result.get("candidates", [])
        prompt_tokens = result.get("usageMetadata", {}).get("promptTokenCount", None)
        if not candidates:
            return "", latency, prompt_tokens
        parts = candidates[0].get("content", {}).get("parts", [])
        return parts[0].get("text", "") if parts else "", latency, prompt_tokens


class ConfigD(GeminiAdapter):
    """Chinese-language instruction + entity list."""
    config_name = "D"

    def transcribe(self, wav_path: str, opts: TranscribeOpts):
        import base64, json as _json, urllib.request, pathlib as _pathlib
        audio_b64 = base64.b64encode(_pathlib.Path(wav_path).read_bytes()).decode()
        if opts.known_entities:
            entity_str = "、".join(opts.known_entities)
            text_prompt = (
                "请准确转录以下音频内容。下列专有名词和技术术语必须按原样保留，不得翻译或修改：\n"
                + entity_str
            )
        else:
            text_prompt = "请准确转录以下音频内容。"

        body = {
            "contents": [{
                "parts": [
                    {"text": text_prompt},
                    {"inlineData": {"mimeType": "audio/wav", "data": audio_b64}},
                ]
            }]
        }

        url = _ga_mod._URL_TEMPLATE.format(model=self.model)
        payload = _json.dumps(body).encode()
        req = urllib.request.Request(
            url,
            data=payload,
            headers={"x-goog-api-key": self.api_key, "Content-Type": "application/json"},
            method="POST",
        )
        t0 = time.time()
        with urllib.request.urlopen(req, timeout=60) as resp:
            result = _json.loads(resp.read())
        latency = time.time() - t0
        candidates = result.get("candidates", [])
        prompt_tokens = result.get("usageMetadata", {}).get("promptTokenCount", None)
        if not candidates:
            return "", latency, prompt_tokens
        parts = candidates[0].get("content", {}).get("parts", [])
        return parts[0].get("text", "") if parts else "", latency, prompt_tokens


# ── Metric helpers ────────────────────────────────────────────────────────────

def entity_recall_exact(entities: list, transcript: str) -> float:
    """Fraction of entities found as case-insensitive substrings in transcript."""
    if not entities:
        return 1.0
    t_lower = transcript.lower()
    hits = sum(1 for e in entities if e.lower() in t_lower)
    return round(hits / len(entities), 4)


# ── Main ──────────────────────────────────────────────────────────────────────

def main():
    corpus_path = _this_dir / 'asr-test-corpus.jsonl'
    audio_dir = _this_dir / 'audio'
    results_path = _this_dir / 'results.jsonl'

    corpus = [json.loads(line) for line in corpus_path.read_text().splitlines() if line.strip()]
    print(f"Loaded {len(corpus)} corpus entries.")

    configs = [ConfigA(), ConfigB(), ConfigC(), ConfigD()]

    # Run Config A first for sanity check
    config_a_recalls = []

    with results_path.open("w") as out:
        for adapter in configs:
            cfg = adapter.config_name
            print(f"\n=== Config {cfg} ===")
            recalls = []

            for entry in corpus:
                test_id = entry["id"]
                wav_path = str(audio_dir / f"{test_id}.wav")
                entities = entry.get("expected_entities", [])
                category = entry.get("category", "")

                opts = TranscribeOpts(
                    language="zh",
                    known_entities=entities,
                )

                try:
                    transcript, latency, prompt_tokens = adapter.transcribe(wav_path, opts)
                except Exception as e:
                    print(f"  ERROR {test_id}: {e}")
                    transcript, latency, prompt_tokens = "", 0.0, None

                recall = entity_recall_exact(entities, transcript)
                recalls.append(recall)

                row = {
                    "config": cfg,
                    "test_id": test_id,
                    "transcript": transcript,
                    "entity_recall_exact": recall,
                    "latency_s": round(latency, 3),
                    "prompt_tokens": prompt_tokens,
                    "expected_entities": entities,
                    "category": category,
                }
                out.write(json.dumps(row, ensure_ascii=False, separators=(',', ':')) + "\n")
                out.flush()
                print(f"  {test_id}: recall={recall:.3f} latency={latency:.2f}s  {transcript[:60]!r}")

            mean_recall = sum(recalls) / len(recalls) if recalls else 0.0
            print(f"Config {cfg} mean entity_recall_exact = {mean_recall:.4f}")

            if cfg == "A":
                config_a_recalls = recalls
                if mean_recall < 0.30:
                    print(
                        f"\n[DIAGNOSTIC] Config A mean entity_recall_exact = {mean_recall:.4f} "
                        f"is below the 0.30 threshold.\n"
                        f"This indicates a systematic problem (bad TTS audio, wrong entity "
                        f"annotation, or API key failure).\n"
                        f"Halting before running configs B, C, D."
                    )
                    sys.exit(1)
                else:
                    print(
                        f"Config A sanity check passed "
                        f"(mean={mean_recall:.4f} >= 0.30, TASK-40 reference=0.643)"
                    )

    # Summary
    print("\n=== Summary ===")
    results = [json.loads(l) for l in results_path.read_text().splitlines() if l.strip()]
    for cfg_name in ["A", "B", "C", "D"]:
        rows = [r for r in results if r["config"] == cfg_name]
        if rows:
            m = sum(r["entity_recall_exact"] for r in rows) / len(rows)
            print(f"Config {cfg_name}: mean entity_recall_exact = {m:.4f}  (n={len(rows)})")

    print(f"\nWrote {len(results)} rows to {results_path}")


if __name__ == "__main__":
    main()
