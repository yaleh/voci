import sys, time, base64, json, pathlib, importlib.util, urllib.request

_this_dir = pathlib.Path(__file__).resolve().parent
_base_path = (_this_dir / '..' / '..' / '..' / 'asr-bench' / 'adapters' / 'base.py').resolve()
if 'base' not in sys.modules:
    _spec = importlib.util.spec_from_file_location("base", str(_base_path))
    _mod = importlib.util.module_from_spec(_spec)
    sys.modules['base'] = _mod
    _spec.loader.exec_module(_mod)
from base import ModelAdapter, TranscribeOpts

_PROMPT = "Transcribe the audio exactly as spoken. Output only the transcribed text, no explanation, no punctuation changes."


class Gemma4Adapter(ModelAdapter):
    name = "gemma4"
    supports_hints = False

    def transcribe(self, wav_path, opts: TranscribeOpts):
        audio_b64 = base64.b64encode(pathlib.Path(wav_path).read_bytes()).decode()
        payload = json.dumps({
            "model": "gemma4:e4b",
            "messages": [{"role": "user", "content": _PROMPT, "images": [audio_b64]}],
            "stream": False,
        }).encode()
        req = urllib.request.Request(
            "http://localhost:11434/api/chat",
            data=payload,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        t0 = time.time()
        with urllib.request.urlopen(req, timeout=120) as resp:
            result = json.loads(resp.read())
        latency = time.time() - t0
        hypothesis = result["message"]["content"].strip()
        return hypothesis, latency
