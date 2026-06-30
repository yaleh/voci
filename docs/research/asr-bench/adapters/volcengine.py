"""
Volcengine Seed-ASR 2.0 adapter — 大模型录音文件识别标准版 API
https://www.volcengine.com/docs/6561/1354868

Flow:
  1. POST /api/v3/auc/bigmodel/submit  (audio URL + optional inline hotwords)
  2. Poll  /api/v3/auc/bigmodel/query  until X-Api-Status-Code != processing
  3. Return result.text

Audio must be a publicly reachable URL.  Set volcengine_audio_base_url in
~/.config/voci/config-volcengine.yaml (or VOLCENGINE_AUDIO_BASE_URL env var)
to the base URL that serves the testdata/ directory, e.g.:

  cloudflared tunnel --url http://localhost:8888 &
  python3 -m http.server 8888 --directory testdata/

Then set:  volcengine_audio_base_url: "https://xxx.trycloudflare.com"
"""

import json, os, pathlib, sys, time, uuid
import requests

try:
    from .base import ModelAdapter, TranscribeOpts
except ImportError:
    sys.path.insert(0, str(pathlib.Path(__file__).parent))
    from base import ModelAdapter, TranscribeOpts

SUBMIT_URL = "https://openspeech.bytedance.com/api/v3/auc/bigmodel/submit"
QUERY_URL  = "https://openspeech.bytedance.com/api/v3/auc/bigmodel/query"
RESOURCE_ID = "volc.seedasr.auc"

POLL_INTERVAL = 2.0   # seconds between query retries
POLL_TIMEOUT  = 120.0 # max seconds to wait for a result


def _from_config(key: str) -> str:
    try:
        import yaml
        cfg_env = os.environ.get("VOCI_CONFIG", "")
        cfg = pathlib.Path(cfg_env) if cfg_env else pathlib.Path.home() / ".config" / "voci" / "config.yaml"
        return yaml.safe_load(cfg.read_text()).get(key, "") or ""
    except Exception:
        return ""


class VolcengineASRAdapter(ModelAdapter):
    supports_hints = True

    def __init__(self):
        self.api_key = (os.environ.get("VOLCENGINE_API_KEY")
                        or _from_config("volcengine_api_key"))
        self.audio_base_url = (os.environ.get("VOLCENGINE_AUDIO_BASE_URL")
                               or _from_config("volcengine_audio_base_url")).rstrip("/")
        if not self.api_key:
            raise RuntimeError(
                "Volcengine credentials missing. Set volcengine_api_key "
                "in ~/.config/voci/config-volcengine.yaml"
            )

    @property
    def name(self) -> str:
        return "volcengine"

    def _headers(self, task_id: str, include_sequence: bool = False) -> dict:
        h = {
            "X-Api-Key":        self.api_key,
            "X-Api-Resource-Id": RESOURCE_ID,
            "X-Api-Request-Id": task_id,
            "Content-Type": "application/json",
        }
        if include_sequence:
            h["X-Api-Sequence"] = "-1"
        return h

    def _submit(self, audio_url: str, entities: list) -> str:
        """Submit recognition task; return the task_id used."""
        task_id = str(uuid.uuid4())

        body: dict = {
            "user": {"uid": "bench"},
            "audio": {"format": "wav", "url": audio_url},
            "request": {"model_name": "bigmodel", "enable_itn": True},
        }

        # Inline hotword injection — no pre-registration needed (up to 5000 words)
        if entities:
            corpus_ctx = json.dumps({"hotwords": [{"word": e} for e in entities]})
            body["request"]["corpus"] = {"context": corpus_ctx}

        resp = requests.post(
            SUBMIT_URL,
            headers=self._headers(task_id, include_sequence=True),
            json=body,
            timeout=30,
        )
        resp.raise_for_status()
        status = resp.headers.get("X-Api-Status-Code", "")
        if status != "20000000":
            msg = resp.headers.get("X-Api-Message", resp.text)
            raise RuntimeError(f"Submit failed: {status} {msg}")
        return task_id

    def _poll(self, task_id: str) -> str:
        """Poll query endpoint until done; return recognised text."""
        deadline = time.time() + POLL_TIMEOUT
        while time.time() < deadline:
            resp = requests.post(
                QUERY_URL,
                headers=self._headers(task_id),
                json={},
                timeout=30,
            )
            resp.raise_for_status()
            status = resp.headers.get("X-Api-Status-Code", "")

            if status in ("20000001", "20000002"):   # processing / queued
                time.sleep(POLL_INTERVAL)
                continue
            if status == "20000000":                 # success
                data = resp.json()
                return data.get("result", {}).get("text", "").strip()
            if status == "20000003":                 # silent audio
                return ""
            raise RuntimeError(
                f"Recognition failed: {status} "
                f"{resp.headers.get('X-Api-Message', resp.text)}"
            )
        raise TimeoutError(f"Volcengine task {task_id} did not complete in {POLL_TIMEOUT}s")

    def transcribe(self, wav_path: str, opts: TranscribeOpts) -> "tuple[str, float]":
        if not self.audio_base_url:
            raise RuntimeError(
                "volcengine_audio_base_url is required. Start a local HTTP server "
                "and cloudflared tunnel, then set the HTTPS URL in config."
            )
        filename = pathlib.Path(wav_path).name
        audio_url = f"{self.audio_base_url}/{filename}"

        t0 = time.time()
        task_id = self._submit(audio_url, opts.known_entities)
        text = self._poll(task_id)
        latency = time.time() - t0

        return text, latency


# ---------------------------------------------------------------------------
# Smoke test — validates credentials and submit/query round-trip.
# Requires volcengine_audio_base_url to be set and a sample WAV to be served.
# ---------------------------------------------------------------------------

if __name__ == "__main__":
    import argparse
    parser = argparse.ArgumentParser(description="Volcengine ASR adapter utilities")
    parser.add_argument("--smoke-test", action="store_true",
                        help="Validate submit/query round-trip with a sample WAV")
    parser.add_argument("--config", default="", metavar="PATH",
                        help="Path to voci config YAML (overrides config.yaml symlink)")
    parser.add_argument("--wav", default="",
                        help="Path to a local WAV file to test (must be served by audio_base_url)")
    args = parser.parse_args()

    if args.config:
        os.environ["VOCI_CONFIG"] = args.config

    if not args.smoke_test:
        parser.print_help()
        sys.exit(0)

    try:
        adapter = VolcengineASRAdapter()
    except RuntimeError as e:
        print(f"[FAIL] {e}", file=sys.stderr)
        sys.exit(1)

    print(f"[OK] credentials loaded: api_key={adapter.api_key[:8]}***")
    print(f"[OK] audio_base_url={adapter.audio_base_url}")

    if not args.wav:
        print("[SKIP] no --wav supplied; skipping recognition round-trip")
        print("[OK] smoke test passed (credentials only)")
        sys.exit(0)

    wav_path = args.wav
    if not pathlib.Path(wav_path).exists():
        print(f"[FAIL] wav file not found: {wav_path}", file=sys.stderr)
        sys.exit(1)

    from base import TranscribeOpts
    opts = TranscribeOpts(known_entities=["测试实体", "WFST"])
    try:
        text, latency = adapter.transcribe(wav_path, opts)
    except Exception as e:
        print(f"[FAIL] transcribe error: {e}", file=sys.stderr)
        sys.exit(1)

    print(f"[OK] recognition succeeded: latency={latency:.1f}s text='{text}'")
    print("[OK] smoke test passed")
