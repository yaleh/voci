import sys, os, time, requests, importlib.util, pathlib

# Load base from asr-bench/adapters (resolved relative to this file)
_adapters_dir = pathlib.Path(__file__).resolve().parent
_base_path = (_adapters_dir / '..' / '..' / '..' / 'asr-bench' / 'adapters' / 'base.py').resolve()
if 'base' not in sys.modules:
    _spec = importlib.util.spec_from_file_location("base", str(_base_path))
    _mod = importlib.util.module_from_spec(_spec)
    sys.modules['base'] = _mod
    _spec.loader.exec_module(_mod)
from base import ModelAdapter, TranscribeOpts

# Default model is openai/whisper-large-v3; override with ASR_MODEL env var
_DEFAULT_MODEL = "openai/whisper-large-v3"


class WhisperBiasedAdapter(ModelAdapter):
    name = "whisper-biased"
    supports_hints = True

    def __init__(self):
        self.api_key = os.environ.get("SILICONFLOW_API_KEY", "")
        if not self.api_key:
            # Fallback: read from voci config file
            try:
                import yaml
                cfg_path = pathlib.Path.home() / ".config" / "voci" / "config.yaml"
                if cfg_path.exists():
                    with open(cfg_path) as f:
                        cfg = yaml.safe_load(f)
                        self.api_key = cfg.get("siliconflow_api_key", "")
            except Exception:
                pass
        self.model = os.environ.get("ASR_MODEL", _DEFAULT_MODEL)

    def transcribe(self, wav_path, opts: TranscribeOpts):
        prompt_str = ", ".join(opts.known_entities) if opts.known_entities else ""
        url = "https://api.siliconflow.cn/v1/audio/transcriptions"
        headers = {"Authorization": f"Bearer {self.api_key}"}
        data = {"model": self.model}
        if prompt_str:
            data["prompt"] = prompt_str
        t0 = time.time()
        with open(wav_path, "rb") as f:
            audio_bytes = f.read()
        resp = requests.post(url, headers=headers, data=data,
                             files={"file": ("audio.wav", audio_bytes, "audio/wav")})
        latency = time.time() - t0
        resp.raise_for_status()
        return resp.json().get("text", ""), latency
