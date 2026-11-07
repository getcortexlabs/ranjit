#!/usr/bin/env python3
"""Exercise the running sandbox through its public HTTP API. No third-party packages."""

import json
import os
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

BASE = os.environ.get("RANJIT_BASE_URL") or "http://localhost:8080"
KEY = os.environ["RANJIT_API_KEY"]


def call(method, path, payload=None, expected=200):
    data = json.dumps(payload).encode() if payload is not None else None
    request = urllib.request.Request(
        BASE + path, data=data, method=method,
        headers={"Authorization": "Bearer " + KEY, "Content-Type": "application/json"},
    )
    try:
        response = urllib.request.urlopen(request, timeout=5)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        body = response.read()
        assert response.status == expected, (response.status, body)
        return json.loads(body) if body else None


def wait_job(job_id):
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        job = call("GET", "/v1/jobs/" + job_id)
        if job["status"] == "succeeded":
            return
        assert job["status"] not in ("failed", "cancelled"), job
        time.sleep(0.2)
    raise AssertionError("sync did not finish within 15 seconds")


def main():
    call("GET", "/readyz")
    institutions = call("GET", "/v1/institutions")["data"]
    assert all(item["sandbox"] for item in institutions)
    result = call("POST", "/v1/connections", {
        "customer_id": "smoke-" + uuid.uuid4().hex,
        "institution_id": institutions[0]["id"],
    }, expected=201)
    connection_id = result["connection"]["id"]
    try:
        authorization = urllib.parse.urlsplit(result["authorization_url"])
        assert (authorization.scheme, authorization.netloc) == (
            urllib.parse.urlsplit(BASE).scheme, urllib.parse.urlsplit(BASE).netloc
        ), "authorization origin differs from RANJIT_BASE_URL"
        token = urllib.parse.parse_qs(authorization.query)["token"][0]
        form = urllib.parse.urlencode({"token": token, "decision": "approve"}).encode()
        request = urllib.request.Request(BASE + "/sandbox/authorize", data=form)
        with urllib.request.urlopen(request, timeout=5) as response:
            assert json.load(response)["status"] == "active"
        for _ in range(2):
            job = call("POST", "/v1/connections/" + connection_id + "/sync", expected=202)
            wait_job(job["id"])
            accounts = call("GET", "/v1/connections/" + connection_id + "/accounts")["data"]
            assert len(accounts) == 2
            count = sum(len(call("GET", "/v1/accounts/" + a["id"] + "/transactions")["data"]) for a in accounts)
            assert count == 4, count
    finally:
        call("DELETE", "/v1/connections/" + connection_id, expected=204)
    call("GET", "/v1/connections/" + connection_id + "/accounts", expected=409)
    print("OK: consent, sync, 2 accounts, 4 transactions, deduplication and revocation")


if __name__ == "__main__":
    main()
