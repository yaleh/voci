import difflib

def fuzzy_entity_recall(known_entities, hypothesis, threshold=0.3):
    """Return fraction of known_entities matched in hypothesis (fuzzy). None if list is empty."""
    if not known_entities:
        return None
    hypothesis_words = hypothesis.lower().split()
    hits = 0
    for entity in known_entities:
        entity_lower = entity.lower()
        for word in hypothesis_words:
            ratio = difflib.SequenceMatcher(None, word, entity_lower).ratio()
            if 1 - ratio <= threshold:
                hits += 1
                break
    return hits / len(known_entities)
