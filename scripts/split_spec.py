#!/usr/bin/env python3
"""Split one fixed OpenAPI spec into one sub-spec per Go module.

Usage:
    python3 scripts/split_spec.py myaccount specs/e2e.openapi.fixed.json myaccount/
    python3 scripts/split_spec.py tir specs/e2e-tir.openapi.fixed.json tir/

Writes <outdir>/<bucket>/openapi.json for every bucket that has at
least one operation. Safe to re-run: it only ever rewrites the
openapi.json files, never touches hand-written code.
"""
import json
import os
import sys
from collections import defaultdict

from classify_service import myaccount_bucket, tir_bucket

METHODS = ("get", "post", "put", "patch", "delete")


def split(kind, spec):
    by_bucket = defaultdict(dict)
    for path, methods in spec["paths"].items():
        for method, op in methods.items():
            if method not in METHODS:
                continue
            if kind == "myaccount":
                bucket = myaccount_bucket(path)
            else:
                bucket = tir_bucket(op.get("tags"))
            by_bucket[bucket].setdefault(path, {})[method] = op
    return by_bucket


def main():
    if len(sys.argv) != 4:
        print(__doc__)
        sys.exit(1)

    kind, src_path, outdir = sys.argv[1], sys.argv[2], sys.argv[3]
    if kind not in ("myaccount", "tir"):
        print("kind must be 'myaccount' or 'tir'")
        sys.exit(1)

    spec = json.load(open(src_path, encoding="utf-8"))
    by_bucket = split(kind, spec)

    for bucket, paths in by_bucket.items():
        sub = {
            "openapi": spec["openapi"],
            "info": {**spec["info"], "title": f"{spec['info']['title']} - {bucket}"},
            "servers": spec["servers"],
            "paths": paths,
        }
        bucket_dir = os.path.join(outdir, bucket)
        os.makedirs(bucket_dir, exist_ok=True)
        json.dump(sub, open(os.path.join(bucket_dir, "openapi.json"), "w", encoding="utf-8"))

    print(f"{kind}: split into {len(by_bucket)} modules under {outdir}")
    for bucket in sorted(by_bucket, key=lambda b: -len(by_bucket[b])):
        print(f"  {bucket:28} {len(by_bucket[bucket])} paths")


if __name__ == "__main__":
    main()
