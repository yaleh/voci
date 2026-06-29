"""Merged pipeline experiment: single Gemini call replaces 3-step ASR+Rewrite+Classify.

Usage:
  python3 docs/research/pipeline-merge/run_experiment.py

Appends rows to results.jsonl. Reads API key from ~/.config/voci/config.yaml.
"""
import base64, json, pathlib, sys, time, urllib.request

# ── Paths ─────────────────────────────────────────────────────────────────────
_this_dir = pathlib.Path(__file__).resolve().parent
_root = (_this_dir / ".." / ".." / "..").resolve()

ANNOTATED_PATH = _this_dir / "testcases-annotated.json"
MERGED_PROMPT_PATH = _this_dir / "merged_prompt.txt"
RESULTS_PATH = _this_dir / "results.jsonl"
TESTDATA_DIR = _root / "testdata"

GEMINI_URL = "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent"


def load_api_key() -> str:
    """Read asr_api_key from ~/.config/voci/config.yaml."""
    cfg_path = pathlib.Path.home() / ".config" / "voci" / "config.yaml"
    for line in cfg_path.read_text().splitlines():
        if line.startswith("asr_api_key:"):
            return line.split(":", 1)[1].strip().strip('"\'')
    return ""


def build_prompt(template: str, entities: list) -> str:
    """Fill {ENTITIES_PLACEHOLDER} in the template with the entity list."""
    if entities:
        entity_str = ", ".join(entities)
    else:
        entity_str = "(none)"
    return template.replace("{ENTITIES_PLACEHOLDER}", entity_str)


def call_gemini(wav_path: str, prompt_text: str, api_key: str) -> tuple:
    """
    Call Gemini Audio API with a merged prompt.
    Returns (raw_response_text, latency_ms).
    """
    audio_b64 = base64.b64encode(pathlib.Path(wav_path).read_bytes()).decode()
    body = {
        "contents": [{
            "parts": [
                {"text": prompt_text},
                {"inlineData": {"mimeType": "audio/wav", "data": audio_b64}},
            ]
        }],
        "generationConfig": {
            "response_mime_type": "application/json",
        },
    }
    payload = json.dumps(body).encode()
    req = urllib.request.Request(
        GEMINI_URL,
        data=payload,
        headers={"x-goog-api-key": api_key, "Content-Type": "application/json"},
        method="POST",
    )
    t0 = time.time()
    with urllib.request.urlopen(req, timeout=90) as resp:
        result = json.loads(resp.read())
    latency_ms = (time.time() - t0) * 1000

    candidates = result.get("candidates", [])
    if not candidates:
        return "", latency_ms
    parts = candidates[0].get("content", {}).get("parts", [])
    raw_text = parts[0].get("text", "").strip() if parts else ""
    return raw_text, latency_ms


def parse_response(raw: str) -> tuple:
    """
    Parse JSON response from Gemini merged call.
    Returns (transcript, rewritten, kind, confidence, parse_error).
    """
    cleaned = raw.strip()
    # Strip markdown code fences if present
    if cleaned.startswith("```"):
        lines = cleaned.splitlines()
        cleaned = "\n".join(lines[1:-1] if lines[-1].strip() == "```" else lines[1:])
    # Find first { ... }
    if "{" in cleaned:
        start = cleaned.index("{")
        end = cleaned.rindex("}") + 1
        cleaned = cleaned[start:end]
    try:
        data = json.loads(cleaned)
        transcript = data.get("transcript", "")
        rewritten = data.get("rewritten", "")
        kind = data.get("kind", "ambiguous")
        if kind not in ("direct_prompt", "backlog_action", "query", "ambiguous"):
            kind = "ambiguous"
        confidence = float(data.get("confidence", 0.0))
        return transcript, rewritten, kind, confidence, False
    except Exception:
        return "", "", "ambiguous", 0.0, True


def main():
    api_key = load_api_key()
    if not api_key:
        print("ERROR: no asr_api_key found in config.yaml", file=sys.stderr)
        sys.exit(1)

    cases = json.loads(ANNOTATED_PATH.read_text())
    prompt_template = MERGED_PROMPT_PATH.read_text()

    print(f"Loaded {len(cases)} cases. Writing to {RESULTS_PATH}")

    with RESULTS_PATH.open("w", encoding="utf-8") as out:
        for i, c in enumerate(cases, 1):
            case_id = c["id"]
            wav_path = str(TESTDATA_DIR / f"{case_id}.wav")
            known_entities = c.get("known_entities") or c.get("expected_entities", [])

            prompt_text = build_prompt(prompt_template, known_entities)

            print(f"[{i}/{len(cases)}] {case_id} ...", flush=True)

            try:
                raw_response, latency_ms = call_gemini(wav_path, prompt_text, api_key)
                transcript, rewritten, kind, confidence, parse_error = parse_response(raw_response)
            except Exception as e:
                print(f"  ERROR: {e}")
                transcript, rewritten, kind, confidence, latency_ms, parse_error = "", "", "ambiguous", 0.0, 0.0, True

            print(f"  transcript: {transcript[:60]!r}")
            print(f"  rewritten:  {rewritten[:60]!r}")
            print(f"  kind: {kind}  confidence: {confidence:.2f}  latency: {latency_ms:.0f}ms  parse_error: {parse_error}")

            row = {
                "case_id": case_id,
                "transcript": transcript,
                "rewritten": rewritten,
                "kind": kind,
                "confidence": round(confidence, 4),
                "latency_ms": round(latency_ms, 1),
                "parse_error": parse_error,
            }
            out.write(json.dumps(row, ensure_ascii=False, separators=(",", ":")) + "\n")
            out.flush()

    print(f"\nDone. Wrote {len(cases)} rows to {RESULTS_PATH}")


if __name__ == "__main__":
    main()
