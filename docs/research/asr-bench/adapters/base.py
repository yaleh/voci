from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from typing import List, Tuple

@dataclass
class TranscribeOpts:
    language: str = ""
    known_entities: List[str] = field(default_factory=list)
    prompt: str = ""
    system_prompt: str = ""

class ModelAdapter(ABC):
    supports_hints: bool = False

    @property
    @abstractmethod
    def name(self) -> str: ...

    @abstractmethod
    def transcribe(self, wav_path: str, opts: TranscribeOpts) -> Tuple[str, float]:
        """Returns (hypothesis, latency_seconds)"""
        ...
