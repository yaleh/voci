import sys, os, time, base64, json, pathlib, importlib.util

_this_dir = pathlib.Path(__file__).resolve().parent
_base_path = (_this_dir / '..' / '..' / '..' / 'asr-bench' / 'adapters' / 'base.py').resolve()
if 'base' not in sys.modules:
    _spec = importlib.util.spec_from_file_location("base", str(_base_path))
    _mod = importlib.util.module_from_spec(_spec)
    sys.modules['base'] = _mod
    _spec.loader.exec_module(_mod)
from base import ModelAdapter, TranscribeOpts

_DEFAULT_URL = "https://openrouter.ai/api/v1/audio/transcriptions"


class OpenRouterAdapter(ModelAdapter):
    supports_hints = True

    def __init__(self, model: str = "openai/whisper-large-v3-turbo"):
        self.model = model
        self.api_key = (
            os.environ.get("OPENROUTER_API_KEY")
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
                if data.get("asr_provider") == "openrouter":
                    return data.get("asr_api_key", "")
        except Exception:
            pass
        return ""

    @property
    def name(self) -> str:
        return f"openrouter/{self.model}"

    def transcribe(self, wav_path: str, opts: TranscribeOpts):
        audio_b64 = base64.b64encode(pathlib.Path(wav_path).read_bytes()).decode()
        body = {
            "model": self.model,
            "input_audio": {"data": audio_b64, "format": "wav"},
        }
        if opts.known_entities:
            body["prompt"] = "Known technical terms: " + ", ".join(opts.known_entities)

        import urllib.request, urllib.error
        payload = json.dumps(body).encode()
        req = urllib.request.Request(
            _DEFAULT_URL,
            data=payload,
            headers={
                "Authorization": f"Bearer {self.api_key}",
                "Content-Type": "application/json",
            },
            method="POST",
        )
        t0 = time.time()
        with urllib.request.urlopen(req, timeout=60) as resp:
            result = json.loads(resp.read())
        latency = time.time() - t0
        return result.get("text", ""), latency
