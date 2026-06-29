#!/usr/bin/env python3
"""
Quick benchmark: gemma4:e4b as ASR via Ollama chat API (base64 audio in images field).
Runs against testdata/testcases.json WAV files and reports quality + latency.
"""
import base64, json, time, urllib.request, pathlib, sys

OLLAMA_URL = "http://localhost:11434/api/chat"
MODEL      = "gemma4:e4b"
TESTDATA   = pathlib.Path(__file__).parent.parent.parent / "testdata"
CASES_FILE = TESTDATA / "testcases.json"

PROMPT = (
    "Transcribe the audio exactly as spoken. "
    "Output only the transcribed text, no explanation, no punctuation changes."
)

def transcribe(wav_path: pathlib.Path) -> tuple[str, float]:
    audio_b64 = base64.b64encode(wav_path.read_bytes()).decode()
    payload = json.dumps({
        "model": MODEL,
        "messages": [{"role": "user", "content": PROMPT, "images": [audio_b64]}],
        "stream": False,
    }).encode()
    req = urllib.request.Request(
        OLLAMA_URL,
        data=payload,
        headers={"Content-Type": "application/json"},
    )
    t0 = time.time()
    with urllib.request.urlopen(req, timeout=120) as r:
        result = json.load(r)
    elapsed = time.time() - t0
    text = result.get("message", {}).get("content", "").strip()
    return text, elapsed

def simple_wer(ref: str, hyp: str) -> float:
    """Word error rate (rough)."""
    ref_words = ref.lower().split()
    hyp_words = hyp.lower().split()
    if not ref_words:
        return 0.0
    # DP edit distance
    dp = list(range(len(hyp_words) + 1))
    for i, rw in enumerate(ref_words):
        ndp = [i + 1]
        for j, hw in enumerate(hyp_words):
            ndp.append(min(dp[j] + (0 if rw == hw else 1), dp[j+1] + 1, ndp[-1] + 1))
        dp = ndp
    return dp[-1] / len(ref_words)

def main():
    cases = json.loads(CASES_FILE.read_text())
    # Test first 5 cases (or all if fewer)
    cases = cases[:5]

    print(f"Model: {MODEL}")
    print(f"Cases: {len(cases)}\n")
    print(f"{'ID':<12} {'Latency':>8}  {'WER':>6}  Expected vs Got")
    print("-" * 80)

    total_wer, total_latency = 0.0, 0.0
    for c in cases:
        wav = TESTDATA / f"{c['id']}.wav"
        if not wav.exists():
            print(f"{c['id']:<12}  SKIP (no wav)")
            continue
        expected = c["tts_input"]
        try:
            got, latency = transcribe(wav)
        except Exception as e:
            print(f"{c['id']:<12}  ERROR: {e}")
            continue
        wer = simple_wer(expected, got)
        total_wer += wer
        total_latency += latency
        print(f"{c['id']:<12} {latency:>7.1f}s  {wer:>5.0%}  exp: {expected}")
        print(f"{'':12}          {'':6}  got: {got}")
        print()

    n = len(cases)
    if n:
        print("-" * 80)
        print(f"Avg latency: {total_latency/n:.1f}s   Avg WER: {total_wer/n:.0%}")

if __name__ == "__main__":
    main()
