#!/usr/bin/env python3
"""Exercise the installed service and Go backend against a disposable PostgreSQL."""

import hashlib
import hmac
import json
import os
import queue
import secrets
import subprocess
import sys
import time
import threading
import urllib.error
import urllib.parse
import urllib.request
from contextlib import contextmanager
from datetime import datetime, timedelta, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

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


def request(url, token=None, body=None, content_type="text/csv", extra_headers=None):
    headers = {"Authorization": "Bearer " + token} if token else {}
    if body is not None:
        headers["Content-Type"] = content_type
    headers.update(extra_headers or {})
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


# Verify the running binary wires its outbox worker, idempotency and key overlap.
webhook_secret = secrets.token_hex(32)
received = queue.Queue()


class Receiver(BaseHTTPRequestHandler):
    def do_POST(self):
        body = self.rfile.read(int(self.headers["Content-Length"]))
        signature = self.headers["Ranjit-Signature"]
        timestamp, digest = signature.split(",")
        timestamp = timestamp.removeprefix("t=")
        want = hmac.new(webhook_secret.encode(), timestamp.encode() + b"." + body, hashlib.sha256).hexdigest()
        valid = hmac.compare_digest(digest, "v1=" + want) and abs(int(timestamp) - time.time()) < 300
        if json.loads(body)["type"] == "sync.succeeded":
            received.put((body, valid, signature))
            self.send_response(503 if received.qsize() == 1 else 204)
        else:
            self.send_response(204)
        self.end_headers()

    def log_message(self, *_):
        pass


receiver = ThreadingHTTPServer(("127.0.0.1", 0), Receiver)
thread = threading.Thread(target=receiver.serve_forever, daemon=True)
thread.start()
old_key = service_key
service_key = secrets.token_hex(32)
env.update(
    RANJIT_API_KEY=service_key,
    RANJIT_API_KEY_PREVIOUS=old_key,
    RANJIT_API_KEY_PREVIOUS_EXPIRES_AT=(datetime.now(timezone.utc) + timedelta(hours=1)).isoformat(),
    RANJIT_PROFILES_JSON='[{"name":"demo","provider":"sandbox"}]',
    RANJIT_WEBHOOK_URL=f"http://127.0.0.1:{receiver.server_port}/webhooks/ranjit",
    RANJIT_WEBHOOK_SECRET=webhook_secret,
)
operation_key = secrets.token_hex(16)
body = json.dumps({"customer_id":"webhook-customer","institution_id":"demo:sandbox-bank"}).encode()


def create_connection():
    return request(base + "/v1/connections", service_key, body, "application/json", {"Idempotency-Key":operation_key})


def wait_delivery(status):
    for _ in range(60):
        deliveries = [d for d in request(base + "/v1/webhooks/deliveries", service_key)["data"] if d["event"]["type"] == "sync.succeeded"]
        if deliveries and deliveries[0]["status"] == status and deliveries[0]["attempts"] > 0:
            return deliveries[0]
        time.sleep(0.5)
    raise AssertionError("webhook did not reach expected status")


try:
    with process([sys.argv[1], "serve"], base + "/readyz"):
        assert request(base + "/v1/institutions", old_key)["data"]
        first = create_connection()
        assert create_connection()["authorization_url"] == first["authorization_url"]
        link = urllib.parse.urlparse(first["authorization_url"])
        token = urllib.parse.parse_qs(link.query)["token"][0]
        request(base + "/sandbox/authorize", body=urllib.parse.urlencode({"token":token,"decision":"approve"}).encode(), content_type="application/x-www-form-urlencoded")
        assert wait_delivery("queued")["attempts"] == 1
    with process([sys.argv[1], "serve"], base + "/readyz"):
        replay = create_connection()
        assert replay["connection"]["id"] == first["connection"]["id"]
        assert replay["connection"]["status"] == "active" and "authorization_url" not in replay
        delivered = wait_delivery("delivered")
        assert delivered["attempts"] == 2
        a, b = received.get(timeout=2), received.get(timeout=2)
        assert a[0] == b[0] and a[1] and b[1] and a[2] != b[2]
        assert json.loads(a[0])["data"]["connection_id"] == first["connection"]["id"]
finally:
    receiver.shutdown()
    receiver.server_close()
    thread.join(timeout=5)
print("Idempotent creation + signed webhook retry across restart + API key overlap passed")
