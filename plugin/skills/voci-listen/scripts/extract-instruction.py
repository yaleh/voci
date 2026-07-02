#!/usr/bin/env python3
"""Extract the Rewritten instruction from a voci serve JSON event line.

Reads one line from stdin. If it is valid JSON with a non-empty 'rewritten'
field, prints that value. Otherwise prints the raw line (stripped of trailing
newline) as a fallback.
"""
import sys
import json

line = sys.stdin.read()
try:
    obj = json.loads(line)
    rewritten = obj.get('rewritten', '').strip()
    if rewritten:
        print(rewritten)
    else:
        print(line.rstrip('\n'))
except Exception:
    print(line.rstrip('\n'))
