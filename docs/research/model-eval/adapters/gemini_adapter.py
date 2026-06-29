import sys, os, time, base64, json, pathlib, importlib.util

_this_dir = pathlib.Path(__file__).resolve().parent
_base_path = (_this_dir / '..' / '..' / '..' / 'asr-bench' / 'adapters' / 'base.py').resolve()
if 'base' not in sys.modules:
    _spec = importlib.util.spec_from_file_location("base", str(_base_path))
    _mod = importlib.util.module_from_spec(_spec)
    sys.modules['base'] = _mod
    _spec.loader.exec_module(_mod)
from base import ModelAdapter, TranscribeOpts

_URL_TEMPLATE = "https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent"


class GeminiAdapter(ModelAdapter):
    supports_hints = True

    def __init__(self, model: str = "gemini-2.5-flash"):
        self.model = model
        self.api_key = (
            os.environ.get("GEMINI_API_KEY")
            or os.environ.get("ASR_API_KEY")
            or self._key_from_config()
        )

    @staticmethod
    def _key_from_config() -> str:
        try:
            import yaml
            cfg = pathlib.Path.home() / ".config" / "voci" / "config.yaml"
            if cfg.exists():
                data = yaml.safe_load(cfg.read_text())
                if data.get("asr_provider") == "gemini":
                    return data.get("asr_api_key", "")
        except Exception:
            pass
        return ""

    @property
    def name(self) -> str:
        return f"gemini/{self.model}"

    def transcribe(self, wav_path: str, opts: TranscribeOpts):
        audio_b64 = base64.b64encode(pathlib.Path(wav_path).read_bytes()).decode()
        if opts.known_entities:
            text_prompt = "Transcribe the following audio. Known technical terms: " + ", ".join(opts.known_entities)
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

        import urllib.request
        url = _URL_TEMPLATE.format(model=self.model)
        payload = json.dumps(body).encode()
        req = urllib.request.Request(
            url,
            data=payload,
            headers={
                "x-goog-api-key": self.api_key,
                "Content-Type": "application/json",
            },
            method="POST",
        )
        t0 = time.time()
        with urllib.request.urlopen(req, timeout=60) as resp:
            result = json.loads(resp.read())
        latency = time.time() - t0
        candidates = result.get("candidates", [])
        if not candidates:
            return "", latency
        parts = candidates[0].get("content", {}).get("parts", [])
        return parts[0].get("text", "") if parts else "", latency
