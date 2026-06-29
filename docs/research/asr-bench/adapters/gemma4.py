import base64, time, requests
from .base import ModelAdapter, TranscribeOpts

class Gemma4Adapter(ModelAdapter):
    supports_hints = True

    def __init__(self, ollama_url="http://localhost:11434", model="gemma4:e4b"):
        self.ollama_url = ollama_url
        self.model = model

    @property
    def name(self): return "gemma4"

    def transcribe(self, wav_path, opts):
        with open(wav_path, "rb") as f:
            audio_b64 = base64.b64encode(f.read()).decode()

        system = "Transcribe the audio accurately."
        if opts.known_entities:
            system += f" Known technical terms: {', '.join(opts.known_entities)}"

        # Use Ollama's native images array format for multimodal input
        messages = [{"role": "user", "content": "Please transcribe this audio file exactly as spoken.", "images": [audio_b64]}]

        start = time.time()
        resp = requests.post(f"{self.ollama_url}/api/chat", json={
            "model": self.model,
            "messages": messages,
            "system": system,
            "stream": False
        }, timeout=120)
        latency = time.time() - start

        resp.raise_for_status()
        text = resp.json()["message"]["content"].strip()
        return text, latency
