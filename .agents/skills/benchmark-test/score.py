#!/usr/bin/env python3
"""Score a cartograph battery run against expected symbols.

Usage:
    python3 score.py <results_file> <battery_file>

The results file is the raw output from running the battery queries.
The battery file is a markdown file defining investigations with expected symbols.

Expects results structured with:
    === Investigation N: <name> ===
    --- KW ---
    <keyword query output>
    --- INT ---
    <intent query output>

And battery files with:
    ## Investigation N: <name> (<count> symbols)
    Expected symbols:
    - `SymbolName` — ...
"""
import re
import sys


def parse_battery(path: str) -> dict[str, list[str]]:
    """Parse a battery markdown file into {investigation_key: [symbols]}."""
    battery = {}
    current_inv = None
    with open(path) as f:
        for line in f:
            m = re.match(r"## Investigation (\d+):", line)
            if m:
                current_inv = f"Investigation {m.group(1)}"
                battery[current_inv] = []
                continue
            if current_inv and line.startswith("- `"):
                sym = re.match(r"- `(\w+)`", line)
                if sym:
                    battery[current_inv].append(sym.group(1))
    return battery


def score(results_path: str, battery: dict[str, list[str]], pass_threshold: int = 4):
    """Score results against battery, print per-investigation recall and totals.

    pass_threshold: minimum KW∪INT combined hits for an investigation to PASS.
    """
    results = open(results_path).read()
    sections = re.split(r"=== (Investigation \d+):", results)

    kw_total = kw_hit = int_total = int_hit = 0
    crit_pass = crit_total = 0

    for i in range(1, len(sections), 2):
        inv = sections[i].strip()
        block = sections[i + 1]
        parts = re.split(r"--- (KW|INT) ---", block)

        kw_text = int_text = ""
        for j in range(1, len(parts), 2):
            if parts[j] == "KW":
                kw_text = parts[j + 1]
            elif parts[j] == "INT":
                int_text = parts[j + 1]

        expected = battery.get(inv, [])
        kw_found = [s for s in expected if s in kw_text]
        int_found = [s for s in expected if s in int_text]

        kw_total += len(expected)
        kw_hit += len(kw_found)
        int_total += len(expected)
        int_hit += len(int_found)

        combined = set(kw_found) | set(int_found)
        passes = len(combined) >= pass_threshold
        crit_total += 1
        if passes:
            crit_pass += 1

        status = "PASS" if passes else "FAIL"
        print(
            f"{inv}: KW {len(kw_found)}/{len(expected)} "
            f"INT {len(int_found)}/{len(expected)} "
            f"Combined {len(combined)}/{len(expected)} {status}"
        )

        int_miss = set(expected) - set(int_found)
        if int_miss:
            print(f"  INT missing: {', '.join(sorted(int_miss))}")

    print(f"\n{'=' * 50}")
    if kw_total > 0:
        print(f"KW total: {kw_hit}/{kw_total} ({100 * kw_hit // kw_total}%)")
    if int_total > 0:
        print(f"INT total: {int_hit}/{int_total} ({100 * int_hit // int_total}%)")
    print(f"Criteria: {crit_pass}/{crit_total}")

    return {
        "kw_hit": kw_hit,
        "kw_total": kw_total,
        "int_hit": int_hit,
        "int_total": int_total,
        "crit_pass": crit_pass,
        "crit_total": crit_total,
    }


if __name__ == "__main__":
    if len(sys.argv) != 3:
        print(f"Usage: {sys.argv[0]} <results_file> <battery_file>")
        sys.exit(1)

    battery = parse_battery(sys.argv[2])
    score(sys.argv[1], battery)
