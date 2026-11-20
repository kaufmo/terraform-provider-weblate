#!/usr/bin/env python3
"""PROTOTYPE for wayfinder ticket #6. Throwaway, never merge.

Measures Weblate cold starts in Docker and probes the open API assumptions.

    python3 prototype/weblate-docker/run.py [IMAGE ...]

Env: RUNS (cold starts per image, default 3), WEBLATE_PORT (default 8080).
Writes results-<tag>.json next to this file and prints a summary.
"""

import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
TOKEN = "wlu_prototypeacceptancetesttoken0001"
BASE = f"http://127.0.0.1:{os.environ.get('WEBLATE_PORT', '8080')}"
RUNS = int(os.environ.get("RUNS", "3"))
IMAGES = sys.argv[1:] or ["weblate/weblate:2026.8.1.0", "weblate/weblate:2026.11.2.0"]
REPO = "http://gitserver/demo.git"


def compose(*args, check=True):
    result = subprocess.run(["docker", "compose", *args], cwd=HERE, text=True, capture_output=True)
    if check and result.returncode:
        raise RuntimeError(f"docker compose {args[0]} failed: {result.stderr.strip()[-800:]}")
    return result


def api(method, path, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, data=data, method=method)
    req.add_header("Accept", "application/json")
    req.add_header("Authorization", f"Token {TOKEN}")
    if data:
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=120) as r:
            raw = r.read()
            return r.status, json.loads(raw) if raw else None
    except urllib.error.HTTPError as e:
        raw = e.read()
        try:
            return e.code, json.loads(raw)
        except ValueError:
            return e.code, raw[:300].decode(errors="replace")


def wait_for(pred, timeout=600, step=0.5):
    t0 = time.monotonic()
    while time.monotonic() - t0 < timeout:
        try:
            if pred():
                return round(time.monotonic() - t0, 1)
        except Exception:
            pass
        time.sleep(step)
    raise TimeoutError(f"not ready after {timeout}s")


def healthy():
    with urllib.request.urlopen(BASE + "/healthz/", timeout=5) as r:
        return r.status == 200


def weblate_shell(code):
    return compose("exec", "-T", "weblate", "weblate", "shell", "-c", code).stdout.strip()


TOKEN_PY = f"""
from django.apps import apps
from weblate.auth.models import User
Token = apps.get_model("authtoken", "Token")
user = User.objects.get(username="admin")
Token.objects.filter(user=user).delete()
Token.objects.create(user=user, key="{TOKEN}")
"""

STORED_URLS_PY = """
from weblate.trans.models import Component
c = Component.objects.get(project__slug="proto", slug="demo")
print(repr(c.repo), repr(c.push))
"""


def cold_start():
    compose("down", "-v", "--remove-orphans")
    t0 = time.monotonic()
    lap = lambda: round(time.monotonic() - t0, 1)
    compose("up", "-d")
    result = {"compose_up": lap()}
    wait_for(healthy)
    result["healthz"] = lap()
    weblate_shell(TOKEN_PY)
    result["token"] = lap()
    status, _ = api("GET", "/api/")
    result["api"] = lap()
    result["api_status"] = status
    return result


def probe(fn):
    try:
        return fn()
    except Exception as e:
        return {"error": repr(e)}


def probe_schema():
    status, schema = api("GET", "/api/schema/?format=json")
    if status != 200:
        return {"status": status, "body": schema}
    paths = schema["paths"]
    return {
        "project_addon_paths": {p: sorted(paths[p]) for p in paths if "project" in p and "addon" in p},
        "component_lists_methods": sorted(paths.get("/api/component-lists/", {})),
    }


def probe_settings():
    _, project = api("GET", "/api/projects/proto/")
    _, component = api("GET", "/api/components/proto/demo/")
    return {"project_keys": sorted(project), "component_keys": sorted(component)}


def probe_component():
    out = {}
    out["create_project"], _ = api(
        "POST", "/api/projects/", {"name": "Proto", "slug": "proto", "web": "https://example.com"}
    )
    t0 = time.monotonic()
    status, body = api(
        "POST",
        "/api/projects/proto/components/",
        {
            "name": "Demo",
            "slug": "demo",
            "vcs": "git",
            "repo": REPO.replace("http://", "http://alice:repo-secret@"),
            "push": REPO.replace("http://", "http://bob:push-secret@"),
            "branch": "main",
            "file_format": "po",
            "filemask": "po/*.po",
            "new_base": "po/messages.pot",
            "new_lang": "add",
        },
    )
    out["create_component"] = {"status": status, "seconds": round(time.monotonic() - t0, 1)}
    if status != 201:
        out["create_component"]["body"] = body
        return out
    out["create_component"]["until_translation_exists"] = wait_for(
        lambda: api("GET", "/api/translations/proto/demo/de/")[0] == 200, timeout=120
    )
    _, read = api("GET", "/api/components/proto/demo/")
    out["api_returns"] = {"repo": read.get("repo"), "push": read.get("push")}
    out["stored_after_create"] = weblate_shell(STORED_URLS_PY)
    out["patch_name"], _ = api("PATCH", "/api/components/proto/demo/", {"name": "Demo renamed"})
    out["stored_after_patch_name"] = weblate_shell(STORED_URLS_PY)
    out["patch_urls_without_creds"], _ = api(
        "PATCH", "/api/components/proto/demo/", {"repo": REPO, "push": REPO}
    )
    out["stored_after_patch_urls"] = weblate_shell(STORED_URLS_PY)
    return out


def probe_nulls():
    out = {}
    api("POST", "/api/projects/", {"name": "Proto 2", "slug": "proto2", "web": "https://example.com"})
    _, page1 = api("GET", "/api/projects/?page_size=1")
    out["page1"] = {k: page1.get(k) for k in ("count", "next", "previous")}
    if page1.get("next"):
        _, page2 = api("GET", page1["next"].replace(page1["next"].split("/api/")[0], ""))
        out["page2"] = {k: page2.get(k) for k in ("count", "next", "previous")}

    status, addon = api("POST", "/api/components/proto/demo/addons/", {"name": "weblate.cleanup.generic"})
    out["component_addon"] = {"status": status, **{k: addon.get(k) for k in ("project", "component")}} if status == 201 else {"status": status, "body": addon}
    for name in ("weblate.consistency.languages", "weblate.cleanup.generic"):
        status, addon = api("POST", "/api/projects/proto/addons/", {"name": name})
        out[f"project_addon {name}"] = {"status": status, **{k: addon.get(k) for k in ("project", "component")}} if status == 201 else {"status": status, "body": addon}

    out["add_translation_fr"], _ = api("POST", "/api/components/proto/demo/translations/", {"language_code": "fr"})
    for lang in ("de", "fr"):
        _, tr = api("GET", f"/api/translations/proto/demo/{lang}/")
        out[f"last_change {lang}"] = tr.get("last_change") if isinstance(tr, dict) else tr
    return out


def probe_groups():
    out = {}
    status, clist = api("POST", "/api/component-lists/", {"name": "Proto list", "slug": "proto-list"})
    out["create_component_list"] = status if status == 201 else {"status": status, "body": clist}
    status, group = api("POST", "/api/groups/", {"name": "Proto team"})
    out["create_group"] = status if status == 201 else {"status": status, "body": group}
    if status != 201:
        return out
    gid = group["id"]
    _, roles = api("GET", "/api/roles/")
    _, project = api("GET", "/api/projects/proto/")
    _, component = api("GET", "/api/components/proto/demo/")
    for path, body in (
        ("roles", {"role_id": roles["results"][0]["id"]}),
        ("projects", {"project_id": project["id"]}),
        ("languages", {"language_code": "de"}),
        ("components", {"component_id": component["id"]}),
        ("componentlists", {"component_list_id": clist.get("id") if isinstance(clist, dict) else None}),
    ):
        status, resp = api("POST", f"/api/groups/{gid}/{path}/", body)
        out[f"POST {path} {list(body)[0]}"] = status if status < 300 else {"status": status, "body": resp}
    _, group = api("GET", f"/api/groups/{gid}/")
    out["group_after"] = {k: group.get(k) for k in ("roles", "projects", "languages", "components", "componentlists")}
    return out


def probe_cleanup():
    out = {}
    for slug in ("proto2", "proto"):
        t0 = time.monotonic()
        status, _ = api("DELETE", f"/api/projects/{slug}/")
        gone = wait_for(lambda: api("GET", f"/api/projects/{slug}/")[0] == 404, timeout=120)
        out[f"delete {slug}"] = {"status": status, "request_s": round(time.monotonic() - t0 - gone, 1), "until_404_s": gone}
    t0 = time.monotonic()
    compose("down", "-v", "--remove-orphans")
    out["compose_down_s"] = round(time.monotonic() - t0, 1)
    return out


def run_image(image):
    os.environ["WEBLATE_IMAGE"] = image
    t0 = time.monotonic()
    compose("pull", "--quiet")
    report = {"image": image, "pull_s": round(time.monotonic() - t0, 1), "cold_starts": []}
    for _ in range(RUNS):
        try:
            report["cold_starts"].append(cold_start())
        except Exception as e:
            logs = compose("logs", "--tail", "40", "weblate", check=False).stdout
            report["cold_starts"].append({"error": repr(e), "weblate_logs": logs})
    if "error" not in report["cold_starts"][-1]:
        report["component"] = probe(probe_component)
        report["schema"] = probe(probe_schema)
        report["settings"] = probe(probe_settings)
        report["nulls"] = probe(probe_nulls)
        report["groups"] = probe(probe_groups)
        report["cleanup"] = probe(probe_cleanup)
    compose("down", "-v", "--remove-orphans", check=False)
    tag = image.rsplit(":", 1)[-1]
    with open(os.path.join(HERE, f"results-{tag}.json"), "w") as f:
        json.dump(report, f, indent=2)
    return report


if __name__ == "__main__":
    for image in IMAGES:
        print(json.dumps(run_image(image), indent=1), flush=True)
