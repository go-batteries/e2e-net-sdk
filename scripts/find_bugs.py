#!/usr/bin/env python3
"""Call every safe (no-path-param) GET list endpoint against the real API,
diff the live JSON's types against what myaccount's *fixed* spec declares,
and auto-append a properly-formed patch entry for every mismatch found to
scripts/patches/myaccount.json.

This is the checked-in, reusable version of the one-off sweep that found
6 bugs on 2026-09-07 (appliance-type pricing fields, zabbix_host_id_v2,
bitninja_discount_percentage). Re-run it any time you refresh
specs/e2e.openapi.json -- after scripts/generate.sh -- to catch drift
before it crashes a real run.

Runs against your live account and costs nothing (GET-only, no resources
created), but does need real credentials.

Usage:
    E2E_API_KEY=... E2E_AUTH_TOKEN=... E2E_PROJECT_ID=58489 \
        python3 scripts/find_bugs.py [--dry-run]

--dry-run prints found issues without writing patches/myaccount.json.
"""
import json
import os
import re
import subprocess
import sys
import urllib.parse
from datetime import date

REPO_ROOT = os.path.join(os.path.dirname(__file__), "..")
SPEC_PATH = os.path.join(REPO_ROOT, "specs", "e2e.openapi.fixed.json")
PATCH_PATH = os.path.join(REPO_ROOT, "scripts", "patches", "myaccount.json")
LOCATIONS_PATH = os.path.join(REPO_ROOT, "locations.json")
BASE = "https://api.e2enetworks.com/myaccount"


def active_locations():
    data = json.load(open(LOCATIONS_PATH))
    return [loc["name"] for loc in data["locations"] if loc["status"] == "active"]


def safe_get_candidates(spec):
    """Every GET op with no {path param} and no required param beyond
    project_id/location/apikey/crn -- the endpoints find_bugs.py can call
    without needing a specific resource id it doesn't have."""
    out = []
    for path, methods in spec["paths"].items():
        if "{" in path:
            continue
        op = methods.get("get")
        if not op or "200" not in op.get("responses", {}):
            continue
        params = op.get("parameters", [])
        required_extra = [
            p["name"] for p in params
            if p.get("required") and p["name"] not in ("project_id", "location", "apikey", "crn")
        ]
        if required_extra:
            continue
        out.append((path, op, params))
    return out


def call(path, params, location, api_key, auth_token, project_id):
    q = {}
    for p in params:
        name = p["name"]
        if name == "project_id":
            q[name] = project_id
        elif name == "location":
            q[name] = location
        elif name == "apikey":
            q[name] = api_key
        elif name == "crn":
            q[name] = "42539"
    url = f"{BASE}{path}?{urllib.parse.urlencode(q)}"
    try:
        result = subprocess.run(
            ["curl", "-s", "-H", f"Authorization: Bearer {auth_token}", "-H", "User-Agent: cli-e2e", url],
            capture_output=True, timeout=45, text=True,
        )
        return json.loads(result.stdout), None
    except json.JSONDecodeError:
        return None, f"non-JSON response (likely a backend routing issue, not a spec bug): {result.stdout[:150]!r}"
    except Exception as e:
        return None, f"error: {e}"


ISO_DATE = re.compile(r"^\d{4}-\d{2}-\d{2}$")
ISO_DATETIME = re.compile(r"^\d{4}-\d{2}-\d{2}T")
EMAIL = re.compile(r"^[^@\s]+@[^@\s]+\.[^@\s]+$")


def diff_types(schema, value, path=""):
    """Returns a list of (schema_path_list, issue_dict) for every mismatch."""
    issues = []
    if value is None or not isinstance(schema, dict):
        return issues

    stype = schema.get("type")
    if stype == "integer" and isinstance(value, float):
        issues.append((path, {
            "fix": "set_response_field_type", "new_type": "number",
            "bug_kind": "int-declared-float-actual", "value": value,
        }))
    elif stype == "string":
        fmt = schema.get("format")
        if isinstance(value, (int, float)):
            new_type = "number" if isinstance(value, float) else "integer"
            issues.append((path, {
                "fix": "set_response_field_type", "new_type": new_type,
                "bug_kind": "string-declared-number-actual", "value": value,
            }))
        elif fmt == "date" and isinstance(value, str) and not ISO_DATE.match(value):
            issues.append((path, {
                "fix": "drop_response_field_format",
                "bug_kind": "bad-date-format", "value": value,
            }))
        elif fmt == "date-time" and isinstance(value, str) and not ISO_DATETIME.match(value):
            issues.append((path, {
                "fix": "drop_response_field_format",
                "bug_kind": "bad-datetime-format", "value": value,
            }))
        elif fmt == "email" and isinstance(value, str) and not EMAIL.match(value):
            issues.append((path, {
                "fix": "drop_response_field_format",
                "bug_kind": "bad-email-format", "value": value,
            }))
    elif stype == "object" and isinstance(value, dict) and "properties" in schema:
        for k, sub in schema["properties"].items():
            if k in value:
                issues += diff_types(sub, value[k], f"{path}.{k}")
    elif stype == "array" and isinstance(value, list) and "items" in schema:
        for item in value[:5]:
            issues += diff_types(schema["items"], item, f"{path}[]")

    return issues


def build_patch_entry(method, path, schema_path_str, issue, endpoint_desc):
    schema_path = [p for p in schema_path_str.strip(".").replace("[]", ".items").split(".") if p]
    # translate ".items" markers into the {"properties": ..., "items": ...}
    # walk shape fix_spec.py expects: alternate "properties"/key, and
    # "items" stands alone before the next key.
    walk = ["properties", "data"]
    for seg in schema_path[1:]:  # skip leading "data", already added
        if seg == "items":
            walk.append("items")
        else:
            walk += ["properties", seg]

    entry = {
        "method": method,
        "path": path,
        "bug": (
            f"response field 'data{schema_path_str[len('.data'):] if schema_path_str.startswith('.data') else schema_path_str}' "
            f"{issue['bug_kind'].replace('-', ' ')} (value seen: {issue['value']!r}) "
            f"-- found by scripts/find_bugs.py sweep on {date.today().isoformat()}"
        ),
        "fix": issue["fix"],
        "status": "200",
        "media_type": "application/json",
        "schema_path": walk,
    }
    if issue["fix"] == "set_response_field_type":
        entry["new_type"] = issue["new_type"]
    return entry


def main():
    dry_run = "--dry-run" in sys.argv

    api_key = os.environ["E2E_API_KEY"]
    auth_token = os.environ["E2E_AUTH_TOKEN"]
    project_id = os.environ["E2E_PROJECT_ID"]

    spec = json.load(open(SPEC_PATH))
    locations = active_locations()
    candidates = safe_get_candidates(spec)

    print(f"sweeping {len(candidates)} endpoints x {len(locations)} locations...")

    new_entries = []
    seen_schema_paths = set()

    for path, op, params in candidates:
        needs_location = any(p["name"] == "location" for p in params)
        locs = locations if needs_location else [locations[0]]
        schema = op["responses"]["200"]["content"]["application/json"]["schema"]

        for loc in locs:
            data, err = call(path, params, loc, api_key, auth_token, project_id)
            tag = f"{path} [{loc}]"
            if err:
                print(f"SKIP  {tag}: {err}")
                continue
            issues = diff_types(schema, data)
            if not issues:
                print(f"ok    {tag}")
                continue
            print(f"BUG   {tag}: {len(issues)} issue(s)")
            for schema_path_str, issue in issues:
                key = (path, schema_path_str, issue["fix"])
                if key in seen_schema_paths:
                    continue
                seen_schema_paths.add(key)
                print(f"        {schema_path_str}  {issue['bug_kind']}  value={issue['value']!r}")
                new_entries.append(build_patch_entry("get", path, schema_path_str, issue, path))

    print()
    print(f"=== {len(new_entries)} new patch entries ===")

    if not new_entries:
        return
    if dry_run:
        print(json.dumps(new_entries, indent=2))
        return

    existing = json.load(open(PATCH_PATH))
    existing += new_entries
    json.dump(existing, open(PATCH_PATH, "w"), indent=2)
    print(f"appended to {PATCH_PATH} -- review the diff, then run scripts/generate.sh")


if __name__ == "__main__":
    main()
