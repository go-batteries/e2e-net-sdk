#!/usr/bin/env python3
"""Verify locations.json/.yaml against the actual `location` enums declared
across every myaccount/*/openapi.json module spec.

There's no "list regions" API endpoint (see locations.yaml for why this
file exists at all) -- this script is the guard against it silently going
stale as E2E adds or drops a region. Run it after scripts/generate.sh
whenever you refresh specs/e2e.openapi.json.

Usage: python3 scripts/check_locations.py
Exit 0 if locations.json's "active" set matches every enum found; exit 1
and print the diff otherwise.
"""
import glob
import json
import sys


def enums_in_spec(path):
    spec = json.load(open(path, encoding="utf-8"))
    found = set()
    for methods in spec.get("paths", {}).values():
        for op in methods.values():
            if not isinstance(op, dict):
                continue
            for param in op.get("parameters", []):
                if param.get("name", "").lower() != "location":
                    continue
                enum = param.get("schema", {}).get("enum")
                if enum:
                    found.add(tuple(sorted(enum)))
    return found


def main():
    declared = json.load(open("locations.json", encoding="utf-8"))
    active = {loc["name"] for loc in declared["locations"] if loc["status"] == "active"}

    seen = set()
    for path in sorted(glob.glob("myaccount/*/openapi.json")):
        seen |= enums_in_spec(path)

    if not seen:
        print("no location enums found in myaccount/*/openapi.json -- did you run scripts/generate.sh?")
        sys.exit(1)

    ok = True
    for enum_tuple in seen:
        enum_set = set(enum_tuple)
        if enum_set != active:
            print(f"mismatch: spec enum {sorted(enum_set)} != locations.json active set {sorted(active)}")
            ok = False

    if ok:
        print(f"ok: locations.json matches every location enum found ({sorted(active)})")
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
