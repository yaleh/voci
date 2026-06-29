import sys, os, time, difflib, requests, importlib.util, pathlib

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


class WhisperTwoStepAdapter(ModelAdapter):
    name = "whisper-twostep"
    supports_hints = True

    CANDIDATE_THRESHOLD = 0.4

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

    def _call_api(self, wav_path, prompt_str=None):
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

    def transcribe(self, wav_path, opts: TranscribeOpts):
        raw, lat1 = self._call_api(wav_path)
        if not opts.known_entities:
            return raw, lat1
        # Fuzzy match raw words against known_entities
        raw_words = raw.lower().split()
        candidates = []
        seen = set()
        for entity in opts.known_entities:
            entity_lower = entity.lower()
            for word in raw_words:
                ratio = difflib.SequenceMatcher(None, word, entity_lower).ratio()
                if 1 - ratio <= self.CANDIDATE_THRESHOLD and entity not in seen:
                    candidates.append(entity)
                    seen.add(entity)
                    break
        if not candidates:
            return raw, lat1
        prompt_str = ", ".join(candidates)
        refined, lat2 = self._call_api(wav_path, prompt_str)
        return refined, lat1 + lat2
