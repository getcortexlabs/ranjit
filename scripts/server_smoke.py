#!/usr/bin/env python3
"""Exercise the installed service and Go backend against a disposable PostgreSQL."""

import json
import os
import secrets
import subprocess
import sys
import time
import urllib.error
import urllib.request
from contextlib import contextmanager

database = os.environ["RANJIT_TEST_POSTGRES_URL"]
service_key, app_token = secrets.token_hex(32), secrets.token_hex(32)
base = "http://127.0.0.1:8088"
backend = "http://127.0.0.1:3000"
env = {k: v for k, v in os.environ.items() if not k.startswith("RANJIT_")}
env.update(
    RANJIT_API_KEY=service_key,
    RANJIT_DATABASE_URL=database,
    RANJIT_PROFILES_JSON="[]",
    RANJIT_ADDR="127.0.0.1:8088",
    RANJIT_BASE_URL=base,
    RANJIT_URL=base,
    APP_TOKEN=app_token,
)


def request(url, token=None, body=None):
    headers = {"Authorization": "Bearer " + token} if token else {}
    if body is not None:
        headers["Content-Type"] = "text/csv"
    req = urllib.request.Request(url, data=body, headers=headers)
    with urllib.request.urlopen(req, timeout=5) as response:
        return json.load(response)


@contextmanager
def process(command, ready_url, token=None):
    child = subprocess.Popen(command, env=env, stdout=subprocess.DEVNULL)
    try:
        for _ in range(100):
            if child.poll() is not None:
                raise RuntimeError("test process exited before readiness")
            try:
                request(ready_url, token)
                break
            except (urllib.error.URLError, TimeoutError):
                time.sleep(0.1)
        else:
            raise RuntimeError("test process did not become ready")
        yield
    finally:
        child.terminate()
        try:
            child.wait(timeout=10)
        except subprocess.TimeoutExpired:
            child.kill()
            child.wait()


sample = b"id,date,description,amount\nserver-1,2026-11-01,Example,10.00\nserver-2,2026-11-02,Example debit,-2.00\n"
for restarted in (False, True):
    with process([sys.argv[1], "serve"], base + "/readyz"):
        assert request(base + "/v1/institutions", service_key)["data"] == []
        with process([sys.argv[2]], backend + "/transactions", app_token):
            try:
                request(backend + "/transactions")
                raise AssertionError("backend allowed an unauthenticated request")
            except urllib.error.HTTPError as error:
                assert error.code == 401
            before = request(backend + "/transactions", app_token)
            assert len(before["data"]) == (2 if restarted else 0)
            imported = request(backend + "/statements", app_token, sample)
            assert bool(imported.get("duplicate")) == restarted
            rows = request(backend + "/transactions", app_token)["data"]
            assert len(rows) == 2
            assert sorted(row["amount"]["amount_minor"] for row in rows) == [-200, 1000]
print("PostgreSQL service + Go SDK backend: import, authentication and restart passed")
