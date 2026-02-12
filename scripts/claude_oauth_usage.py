#!/usr/bin/env python3
import argparse
import json
import os
import sys
import urllib.error
import urllib.request


def _http_json(method: str, url: str, headers: dict, payload: dict | None = None, timeout_s: int = 20):
    data = None
    req_headers = dict(headers)
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
        req_headers.setdefault("Content-Type", "application/json")

    req = urllib.request.Request(url, data=data, method=method, headers=req_headers)
    try:
        with urllib.request.urlopen(req, timeout=timeout_s) as resp:
            raw = resp.read()
            text = raw.decode("utf-8", errors="replace").strip()
            return json.loads(text) if text else None
    except urllib.error.HTTPError as e:
        raw = e.read()
        text = raw.decode("utf-8", errors="replace").strip()
        try:
            body = json.loads(text) if text else None
        except json.JSONDecodeError:
            body = text
        raise RuntimeError(f"HTTP {e.code} for {url}: {body}") from None
    except urllib.error.URLError as e:
        raise RuntimeError(f"Network error for {url}: {e}") from None
    except json.JSONDecodeError as e:
        raise RuntimeError(f"Non-JSON response from {url}: {e}") from None


def _normalize_base_url(base_url: str) -> str:
    return base_url.rstrip("/")


def _pick_auth_index(entry: dict, fallback_index: int) -> str:
    for key in ("auth_index", "authIndex", "index"):
        if key in entry and entry[key] is not None:
            return str(entry[key])
    return str(fallback_index)


def _extract_provider(entry: dict) -> str | None:
    for key in ("provider", "auth_provider", "authProvider"):
        v = entry.get(key)
        if isinstance(v, str):
            return v
    return None


def _display_name(entry: dict) -> str:
    for key in ("account", "email", "name", "username", "id"):
        v = entry.get(key)
        if isinstance(v, str) and v.strip():
            return v.strip()
    return "unknown"


def _unwrap_api_call_body(api_call_resp: dict):
    if not isinstance(api_call_resp, dict):
        return api_call_resp

    body = api_call_resp.get("body", api_call_resp)
    if isinstance(body, (dict, list)):
        return body
    if isinstance(body, str):
        body_str = body.strip()
        if not body_str:
            return None
        try:
            return json.loads(body_str)
        except json.JSONDecodeError:
            return body_str
    return body


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(
        description="Query Claude Code OAuth usage (five_hour / seven_day) via CLIProxyAPI management endpoints."
    )
    parser.add_argument(
        "--base-url",
        default=os.environ.get("CLAUDE_PROXY_BASE_URL") or "http://100.27.154.139:8001",
        help="CLIProxyAPI base URL (env: CLAUDE_PROXY_BASE_URL). Default: http://100.27.154.139:8001",
    )
    parser.add_argument(
        "--provider",
        default="claude",
        help='Provider filter in /v0/management/auth-files (default: "claude")',
    )
    parser.add_argument(
        "--auth-index",
        action="append",
        default=[],
        help="Limit query to specific auth_index (repeatable, or comma-separated).",
    )
    parser.add_argument(
        "--timeout",
        type=int,
        default=int(os.environ.get("CLAUDE_PROXY_TIMEOUT_S") or "20"),
        help="HTTP timeout seconds (env: CLAUDE_PROXY_TIMEOUT_S). Default: 20",
    )
    parser.add_argument(
        "--json",
        action="store_true",
        help="Print full JSON response per auth entry (includes raw api-call wrapper).",
    )
    parser.add_argument(
        "--pretty-body",
        action="store_true",
        help="Pretty-print the parsed upstream usage JSON per auth entry.",
    )
    args = parser.parse_args(argv)

    management_key = os.environ.get("CLAUDE_MANAGEMENT_KEY")
    if not management_key:
        print("Missing env var: CLAUDE_MANAGEMENT_KEY", file=sys.stderr)
        return 2

    base_url = _normalize_base_url(args.base_url)
    headers = {"Authorization": f"Bearer {management_key}"}

    auth_files_url = f"{base_url}/v0/management/auth-files"
    auth_files_raw = _http_json("GET", auth_files_url, headers=headers, timeout_s=args.timeout)

    auth_files = auth_files_raw
    if isinstance(auth_files_raw, dict):
        for key in ("files", "data", "items", "auth_files", "authFiles"):
            v = auth_files_raw.get(key)
            if isinstance(v, list):
                auth_files = v
                break

    if not isinstance(auth_files, list):
        print(f"Unexpected /auth-files response type: {type(auth_files_raw).__name__}", file=sys.stderr)
        return 3

    provider = args.provider.lower().strip()
    wanted_auth_indexes: set[str] = set()
    for raw in args.auth_index:
        if not raw:
            continue
        for part in str(raw).split(","):
            part = part.strip()
            if part:
                wanted_auth_indexes.add(part)

    entries: list[tuple[str, dict]] = []
    for idx, entry in enumerate(auth_files):
        if not isinstance(entry, dict):
            continue
        p = (_extract_provider(entry) or "").lower().strip()
        if p == provider:
            auth_index = _pick_auth_index(entry, idx)
            if wanted_auth_indexes and auth_index not in wanted_auth_indexes:
                continue
            entries.append((auth_index, entry))

    if not entries:
        if wanted_auth_indexes:
            print(
                f'No auth entries found for provider "{args.provider}" with auth_index in {sorted(wanted_auth_indexes)}',
                file=sys.stderr,
            )
        else:
            print(f'No auth entries found for provider "{args.provider}"', file=sys.stderr)
        return 4

    api_call_url = f"{base_url}/v0/management/api-call"
    results = []
    for auth_index, entry in entries:
        payload = {
            "auth_index": auth_index,
            "method": "GET",
            "url": "https://api.anthropic.com/api/oauth/usage",
            "header": {
                "Authorization": "Bearer $TOKEN$",
                "anthropic-beta": "oauth-2025-04-20",
                "Content-Type": "application/json",
                "Accept": "application/json, text/plain, */*",
                "User-Agent": "claude-code/2 (CLIProxyAPI)",
            },
        }
        wrapped = _http_json("POST", api_call_url, headers=headers, payload=payload, timeout_s=args.timeout)
        body = _unwrap_api_call_body(wrapped if isinstance(wrapped, dict) else {"body": wrapped})
        results.append((auth_index, _display_name(entry), wrapped, body))

    if args.json:
        out = [
            {
                "auth_index": auth_index,
                "display": display,
                "wrapped": wrapped,
                "body": body,
            }
            for auth_index, display, wrapped, body in results
        ]
        print(json.dumps(out, ensure_ascii=False, indent=2))
        return 0

    # Human-friendly summary
    for auth_index, display, _wrapped, body in results:
        print(f"[{args.provider}] auth_index={auth_index} display={display}")
        if not isinstance(body, dict):
            print(f"  body={body}")
            continue
        preferred = ["five_hour", "seven_day"]
        keys = [k for k in body.keys() if isinstance(k, str)]
        ordered = preferred + sorted([k for k in keys if k not in preferred])
        for k in ordered:
            window = body.get(k)
            if not isinstance(window, dict) or "utilization" not in window:
                continue
            utilization = window.get("utilization")
            resets_at = window.get("resets_at")
            print(f"  {k}: utilization={utilization} resets_at={resets_at}")
        if args.pretty_body:
            print(json.dumps(body, ensure_ascii=False, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
