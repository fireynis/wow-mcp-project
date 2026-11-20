# ForeverDB fixtures

These files are SYNTHETIC. ForeverDB's developer page documents no example
responses and no API key existed when they were written, so they follow only the
documented envelope (`{"data": ..., "meta": {...}}`) and field names. Their `meta`
keys and most row keys are guesses. Replace them with scrubbed live captures once
a key exists. The tools pass ForeverDB JSON through unparsed, so the tests rely
only on the envelope shape.
