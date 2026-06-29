"""ASR metrics: WER, CER, entity_recall, language_confusion. Run with --self-test."""
import sys

def edit_distance(a, b):
    m, n = len(a), len(b)
    dp = list(range(n+1))
    for i in range(1, m+1):
        prev, dp[0] = dp[0], i
        for j in range(1, n+1):
            temp = dp[j]
            dp[j] = prev if a[i-1]==b[j-1] else 1 + min(prev, dp[j], dp[j-1])
            prev = temp
    return dp[n]

def wer(reference, hypothesis):
    ref_words = reference.lower().split()
    hyp_words = hypothesis.lower().split()
    if not ref_words: return 0.0
    return edit_distance(ref_words, hyp_words) / len(ref_words)

def cer(reference, hypothesis):
    ref_chars = list(reference.replace(" ", ""))
    hyp_chars = list(hypothesis.replace(" ", ""))
    if not ref_chars: return 0.0
    return edit_distance(ref_chars, hyp_chars) / len(ref_chars)

def entity_recall(known_entities, hypothesis):
    if not known_entities: return None
    hyp_lower = hypothesis.lower()
    hits = sum(1 for e in known_entities if e.lower() in hyp_lower)
    return hits / len(known_entities)

def language_confusion(reference, hypothesis):
    """Proxy: abs diff in ASCII char ratio between ref and hyp."""
    def ascii_ratio(s):
        if not s: return 0.0
        return sum(1 for c in s if ord(c) < 128 and c.isalpha()) / max(len(s), 1)
    return abs(ascii_ratio(reference) - ascii_ratio(hypothesis))

if __name__ == "__main__":
    if "--self-test" not in sys.argv:
        print("Usage: python metrics.py --self-test")
        sys.exit(1)

    # WER tests
    assert wer("hello world", "hello world") == 0.0
    assert wer("hello world", "hello") == 0.5
    assert wer("a b c", "a b c d") > 0

    # CER tests
    assert cer("abc", "abc") == 0.0
    assert cer("abc", "ab") > 0

    # entity_recall tests
    assert entity_recall(["BuildContext"], "fixed BuildContext bug") == 1.0
    assert entity_recall(["BuildContext"], "fixed something") == 0.0
    assert entity_recall([], "anything") is None

    # language_confusion tests
    score = language_confusion("push task to ready", "把任务推到 ready 状态")
    assert score >= 0.0

    print("All self-tests passed.")
