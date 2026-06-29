import os, time, requests, pathlib
from .base import ModelAdapter, TranscribeOpts

def _load_api_key():
    """Load API key from env var first, then ~/.config/voci/config.yaml."""
    key = os.environ.get("SILICONFLOW_API_KEY", "")
    if key:
        return key
    # Fallback: read from voci config file
    try:
        import yaml
        cfg_path = pathlib.Path.home() / ".config" / "voci" / "config.yaml"
        if cfg_path.exists():
            with open(cfg_path) as f:
                data = yaml.safe_load(f)
                return data.get("siliconflow_api_key", "")
    except Exception:
        pass
    return ""

class TeleSpeechAdapter(ModelAdapter):
    supports_hints = False
    API_URL = "https://api.siliconflow.cn/v1/audio/transcriptions"

    def __init__(self):
        self.api_key = _load_api_key()
        if not self.api_key:
            raise RuntimeError("SILICONFLOW_API_KEY not found in env or ~/.config/voci/config.yaml")

    @property
    def name(self): return "telespeech"

    def transcribe(self, wav_path, opts):
        with open(wav_path, "rb") as f:
            audio_data = f.read()

        start = time.time()
        resp = requests.post(
            self.API_URL,
            headers={"Authorization": f"Bearer {self.api_key}"},
            files={"file": ("audio.wav", audio_data, "audio/wav")},
            data={"model": "TeleAI/TeleSpeechASR"},
            timeout=60
        )
        latency = time.time() - start

        resp.raise_for_status()
        data = resp.json()
        # Handle both response formats
        if "text" in data:
            return data["text"].strip(), latency
        choices = data.get("choices", [])
        if choices:
            return choices[0].get("message", {}).get("content", "").strip(), latency
        return "", latency
