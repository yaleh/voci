"""Baseline measurement: run actual 3-step pipeline on all 35 testcases.

Steps:
1. Gemini ASR (Config C hint format)
2. Gemini Rewrite (gemini-2.5-flash)
3. Gemini Classify (gemini-2.5-flash)
"""
import base64, json, pathlib, sys, time, urllib.request

# ── Config ───────────────────────────────────────────────────────────────────
_this_dir = pathlib.Path(__file__).resolve().parent
_root = (_this_dir / ".." / ".." / "..").resolve()
TESTDATA_DIR = _root / "testdata"
ANNOTATED_PATH = _this_dir / "testcases-annotated.json"
BASELINE_PATH = _this_dir / "baseline.json"

GEMINI_URL = "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent"
GEMINI_TEXT_URL = "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent"

# Read API key
cfg_path = pathlib.Path.home() / ".config" / "voci" / "config.yaml"
API_KEY = ""
for line in cfg_path.read_text().splitlines():
    if line.startswith("asr_api_key:"):
        API_KEY = line.split(":", 1)[1].strip().strip('"\'')
        break

if not API_KEY:
    print("ERROR: no asr_api_key found in config.yaml", file=sys.stderr)
    sys.exit(1)

# ── Rewrite system prompt (from internal/pipeline/pipeline.go) ────────────────
REWRITE_SYSTEM = (
    "You normalize a voice transcription into a clean instruction.\n"
    "## Rules\n"
    "- Preserve the speaker's exact scope, intent, and LANGUAGE. Do NOT translate.\n"
    "- Only fix grammar/disfluency and resolve entity references the speaker explicitly made (using Known Entities).\n"
    "- Do NOT add details, steps, or specific targets the speaker did not say.\n"
    "- Do NOT pick a specific task/file/feature when the speaker spoke generally.\n"
    "- Do NOT answer, plan, or act on the instruction — only clean it up.\n"
    "- If genuinely too vague to act on, start your response with [ambiguous].\n"
    "Return only the normalized instruction, nothing else."
)

# ── Classify system prompt (from internal/intent/classify.go) ─────────────────
CLASSIFY_SYSTEM = """You are an intent classifier for a voice-driven developer assistant.

Classify the given text into exactly one of these four intent categories:
- direct_prompt: a direct programming instruction to be executed (e.g. "add logging to auth.go", "fix the bug in parser.go")
- backlog_action: an action targeting the task backlog (e.g. "mark TASK-5 as done", "create a task for refactoring")
- query: an information request about the project (e.g. "what tasks are open?", "what does the auth module do?")
- ambiguous: the intent cannot be determined with confidence

Respond with a JSON object containing exactly two keys:
- "kind": one of "direct_prompt", "backlog_action", "query", "ambiguous"
- "confidence": a float between 0.0 and 1.0 representing your confidence

Example: {"kind":"direct_prompt","confidence":0.92}

Return ONLY the JSON object, no other text."""


def gemini_asr(wav_path: str, known_entities: list) -> tuple:
    """Call Gemini with Config C hint format. Returns (transcript, latency_ms)."""
    audio_b64 = base64.b64encode(pathlib.Path(wav_path).read_bytes()).decode()
    if known_entities:
        text_prompt = (
            "Transcribe the following audio. Below is an example of correct output format:\n\n"
            "Example — if the audio contains the phrase \"我们用 Sentry 来监控\" and the "
            "known term is \"Sentry\", the correct transcript is:\n"
            "\"我们用 Sentry 来监控\"\n\n"
            "Known technical terms: " + ", ".join(known_entities) + "\n\n"
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
    payload = json.dumps(body).encode()
    req = urllib.request.Request(
        GEMINI_URL,
        data=payload,
        headers={"x-goog-api-key": API_KEY, "Content-Type": "application/json"},
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
    return (parts[0].get("text", "").strip() if parts else ""), latency_ms


def gemini_text(system_prompt: str, user_msg: str) -> tuple:
    """Call Gemini text API. Returns (response_text, latency_ms)."""
    body = {
        "system_instruction": {"parts": [{"text": system_prompt}]},
        "contents": [{"parts": [{"text": user_msg}]}],
    }
    payload = json.dumps(body).encode()
    req = urllib.request.Request(
        GEMINI_TEXT_URL,
        data=payload,
        headers={"x-goog-api-key": API_KEY, "Content-Type": "application/json"},
        method="POST",
    )
    t0 = time.time()
    with urllib.request.urlopen(req, timeout=120) as resp:
        result = json.loads(resp.read())
    latency_ms = (time.time() - t0) * 1000
    candidates = result.get("candidates", [])
    if not candidates:
        return "", latency_ms
    parts = candidates[0].get("content", {}).get("parts", [])
    return (parts[0].get("text", "").strip() if parts else ""), latency_ms


def parse_classify(raw: str) -> str:
    """Extract 'kind' from JSON classify response."""
    cleaned = raw.strip()
    if "{" in cleaned:
        cleaned = cleaned[cleaned.index("{"):]
    if "}" in cleaned:
        cleaned = cleaned[:cleaned.rindex("}") + 1]
    try:
        cr = json.loads(cleaned)
        kind = cr.get("kind", "ambiguous")
        if kind in ("direct_prompt", "backlog_action", "query", "ambiguous"):
            return kind
    except Exception:
        pass
    return "ambiguous"


def entity_recall(entities: list, text: str) -> float:
    if not entities:
        return 1.0
    tl = text.lower()
    hits = sum(1 for e in entities if e.lower() in tl)
    return hits / len(entities)


def main():
    cases = json.loads(ANNOTATED_PATH.read_text())
    per_case = []
    total = len(cases)

    for i, c in enumerate(cases, 1):
        sid = c["id"]
        wav_path = str(TESTDATA_DIR / f"{sid}.wav")
        # Use known_entities if non-empty, else fall back to expected_entities
        known_entities = c.get("known_entities") or c.get("expected_entities", [])
        expected_rewrite = c.get("expected_rewrite", "")
        expected_entities = c.get("expected_entities", [])
        expected_kind = c.get("expected_kind", "ambiguous")

        print(f"[{i}/{total}] {sid} ...", flush=True)

        # Step 1: Gemini ASR
        try:
            transcript, asr_lat = gemini_asr(wav_path, known_entities)
        except Exception as e:
            print(f"  ASR ERROR: {e}")
            transcript, asr_lat = "", 0.0

        # Step 2: Rewrite via Gemini
        try:
            rewritten, rw_lat = gemini_text(REWRITE_SYSTEM, f"Normalize this transcription: {transcript}")
        except Exception as e:
            print(f"  Rewrite ERROR: {e}")
            rewritten, rw_lat = transcript, 0.0

        # Step 3: Classify via Gemini
        try:
            raw_cls, cls_lat = gemini_text(CLASSIFY_SYSTEM, f"Classify this text: {rewritten}")
            predicted_kind = parse_classify(raw_cls)
        except Exception as e:
            print(f"  Classify ERROR: {e}")
            predicted_kind, cls_lat = "ambiguous", 0.0

        # Metrics per case
        exact_match = int(rewritten.strip() == expected_rewrite.strip()) if expected_rewrite else 0
        recall = entity_recall(expected_entities, rewritten)
        kind_correct = int(predicted_kind == expected_kind)
        total_lat = asr_lat + rw_lat + cls_lat

        print(f"  ASR: {transcript[:60]!r}")
        print(f"  Rewrite: {rewritten[:60]!r}")
        print(f"  Kind: {predicted_kind} (expected: {expected_kind}) {'OK' if kind_correct else 'MISS'}")
        print(f"  entity_recall={recall:.3f}  exact_match={exact_match}  lat={total_lat:.0f}ms")

        per_case.append({
            "id": sid,
            "transcript": transcript,
            "rewritten": rewritten,
            "predicted_kind": predicted_kind,
            "expected_kind": expected_kind,
            "exact_match": exact_match,
            "entity_recall": round(recall, 4),
            "kind_correct": kind_correct,
            "latency_asr_ms": round(asr_lat, 1),
            "latency_rewrite_ms": round(rw_lat, 1),
            "latency_classify_ms": round(cls_lat, 1),
            "latency_total_ms": round(total_lat, 1),
        })

    n = len(per_case)
    rewrite_exact_match = sum(c["exact_match"] for c in per_case) / n
    rewrite_entity_recall = sum(c["entity_recall"] for c in per_case) / n
    classify_accuracy = sum(c["kind_correct"] for c in per_case) / n
    latency_total_ms = sum(c["latency_total_ms"] for c in per_case) / n

    baseline = {
        "rewrite_exact_match": round(rewrite_exact_match, 4),
        "rewrite_entity_recall": round(rewrite_entity_recall, 4),
        "classify_accuracy": round(classify_accuracy, 4),
        "latency_total_ms": round(latency_total_ms, 1),
        "n": n,
        "per_case": per_case,
    }

    BASELINE_PATH.write_text(json.dumps(baseline, ensure_ascii=False, indent=2))
    print(f"\n=== Baseline Results ===")
    print(f"rewrite_exact_match:   {rewrite_exact_match:.4f}")
    print(f"rewrite_entity_recall: {rewrite_entity_recall:.4f}")
    print(f"classify_accuracy:     {classify_accuracy:.4f}")
    print(f"latency_total_ms:      {latency_total_ms:.1f}")
    print(f"Wrote {BASELINE_PATH}")


if __name__ == "__main__":
    main()
