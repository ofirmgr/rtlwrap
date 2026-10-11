#!/usr/bin/env python3
"""Print visual-order bracket probes directly in Warp, without rtlwrap.

The Hebrew is intentionally in visual order. All four rows should display
balanced parentheses around the same Hebrew word. This does not alter settings.
"""

import sys


def protect(text, start, end):
    return "".join(start + c + end if c in "()" else c for c in text)


plain = ":(ריהמ) Option A"
variants = [
    ("1 standard", plain),
    ("2 current compensation", ":)ריהמ( Option A"),
    ("3 LTR marks", protect(plain, "\u200e", "\u200e")),
    ("4 LTR override", protect(plain, "\u202d", "\u202c")),
]
for label, heading in variants:
    sys.stdout.write(label + "\n")
    sys.stdout.write(
        "\033[34misContextChanged = true\033[0m עונמל ידכ "
        "\033[34mmonitorContextHash\033[0m ףסוה "
        "\033[1m" + heading + "\033[0m\n\n"
    )
