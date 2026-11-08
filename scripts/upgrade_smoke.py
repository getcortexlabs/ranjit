#!/usr/bin/env python3
"""Upgrade actual v0.3.0 data in disposable SQLite/PostgreSQL installations."""
import argparse
from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path
import platform
import secrets
import shutil
import socket
import sqlite3
import subprocess
import tarfile
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parent.parent
SAMPLE = b"id,date,description,amount\nupgrade-1,2026-11-01,Fixture,100.00\nupgrade-2,2026-11-02,Fixture debit,-12.50\n"
BASE_ENV = {k: v for k, v in os.environ.items() if not k.startswith("RANJIT_")}


def run(command, env, **kwargs):
    result = subprocess.run(command, env=env, capture_output=True, **kwargs)
    if result.returncode:
        # Do not echo connection strings, keys, database output or raw server logs.
        raise RuntimeError(f"{Path(str(command[0])).name} failed ({result.returncode})")
    return result.stdout


def released_binary(directory):
    system = "darwin" if platform.system() == "Darwin" else "linux"
    arch = "arm64" if platform.machine() in ("arm64", "aarch64") else "amd64"
    name = f"ranjit_v0.3.0_{system}_{arch}.tar.gz"
    base = "https://github.com/getcortexlabs/ranjit/releases/download/v0.3.0/"
    with urllib.request.urlopen(base + "checksums.txt", timeout=60) as response:
        sums = {line.split()[1]: line.split()[0] for line in response.read().decode().splitlines()}
    with urllib.request.urlopen(base + name, timeout=60) as response:
        content = response.read()
    assert hashlib.sha256(content).hexdigest() == sums[name], "v0.3.0 checksum mismatch"
    archive = directory / name
    archive.write_bytes(content)
    old = directory / "ranjit-v0.3.0"
    with tarfile.open(archive) as bundle:
        old.write_bytes(bundle.extractfile("ranjit").read())
    old.chmod(0o755)
    assert run([old, "version"], BASE_ENV).strip() == b"v0.3.0"
    return old


def request(base, key, path, data=None, method=None, content_type="application/json"):
    if isinstance(data, dict):
        data = json.dumps(data).encode()
    req = urllib.request.Request(base + path, data=data, method=method,
        headers={"Authorization": "Bearer " + key, "Content-Type": content_type})
    with urllib.request.urlopen(req, timeout=5) as response:
        payload = response.read()
        return json.loads(payload) if payload else None


def wait(check):
    for _ in range(150):
        try:
            result = check()
            if result:
                return result
        except (urllib.error.URLError, TimeoutError, ConnectionResetError):
            pass
        time.sleep(0.1)
    raise AssertionError("service did not reach expected state")


@contextmanager
def server(binary, env, base, key):
    child = subprocess.Popen([binary, "serve"], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        wait(lambda: child.poll() is None and request(base, key, "/readyz"))
        yield
    finally:
        child.terminate()
        try:
            child.wait(timeout=10)
        except subprocess.TimeoutExpired:
            child.kill()
            child.wait()


def pg_env(dsn):
    u = urllib.parse.urlsplit(dsn)
    env = dict(BASE_ENV, PGHOST=u.hostname, PGPORT=str(u.port or 5432), PGUSER=urllib.parse.unquote(u.username or ""),
        PGPASSWORD=urllib.parse.unquote(u.password or ""), PGDATABASE=u.path.lstrip("/"))
    env["PGSSLMODE"] = urllib.parse.parse_qs(u.query).get("sslmode", ["prefer"])[0]
    return env


def exercise(old, candidate, directory, postgres=None):
    directory.mkdir()
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        port = s.getsockname()[1]
    base = f"http://127.0.0.1:{port}"
    key = secrets.token_hex(32)
    env = dict(BASE_ENV, RANJIT_API_KEY=key, RANJIT_ADDR=f"127.0.0.1:{port}", RANJIT_BASE_URL=base,
        RANJIT_URL=base, RANJIT_PROVIDER="sandbox", XDG_CONFIG_HOME=str(directory))
    path, before_backup = directory / "ranjit.db", directory / "before.db"
    config = directory / "config.json"
    if postgres:
        env["RANJIT_DATABASE_URL"] = postgres
        sqlenv = pg_env(postgres)
    else:
        # A real v0.3 personal config is read unchanged by the candidate.
        config.write_text(json.dumps(dict(version=1, database=str(path), api_key=key,
            addr=f"127.0.0.1:{port}", base_url=base, sync_interval="1h", profiles=[dict(name="demo", provider="sandbox")])))
        config.chmod(0o600)
        env["RANJIT_CONFIG"] = str(config)
        del env["RANJIT_API_KEY"]
    original_config = config.read_bytes() if config.exists() else None

    def sql(query):
        if postgres:
            return run(["psql", "-X", "-v", "ON_ERROR_STOP=1", "-At", "-c", query], sqlenv).decode().strip()
        with sqlite3.connect(path) as db:
            row = db.execute(query).fetchone()
            return str(row[0]) if row else ""

    def records():
        return request(base, key, "/v1/transactions?customer_id=upgrade&limit=100")["data"]

    with server(old, env, base, key):
        imported = request(base, key, "/v1/imports?customer_id=upgrade&account=fixture&format=csv", SAMPLE, content_type="text/csv")
        connection = imported["connection_id"]
        financial = records()
        assert sorted(r["amount"]["amount_minor"] for r in financial) == [-1250, 10000]
        institutions = request(base, key, "/v1/institutions")["data"]
        pending = request(base, key, "/v1/connections", dict(customer_id="upgrade-sandbox", institution_id=institutions[0]["id"]))
    # A pending job is staged while stopped to deterministically test recovery.
    now = int(time.time() * 1000)
    sid = pending["connection"]["id"]
    sql(f"UPDATE connections SET status='active',token_hash=NULL WHERE id='{sid}'")
    sql(f"INSERT INTO jobs(id,connection_id,status,attempts,error_code,created_at,updated_at,available_at,lease_until,lease_token,requests_completed) VALUES('job-upgrade','{sid}','queued',0,'',{now},{now},{now},0,'',0)")
    # Synthetic direct grant: no bank is called and the credential binding is fake.
    sql(f"INSERT INTO connections(id,customer_id,institution_id,provider,status,created_at,consent_expires_at,last_synced_at,token_hash,authorization_expires_at,authorization_mode,credential_id) VALUES('con-direct-fixture','upgrade-direct','efi','efi','active',{now},{now+86400000},NULL,NULL,0,'api_credentials','synthetic-binding')")
    if postgres:
        before_backup.write_bytes(run(["pg_dump", "--format=custom", "--no-owner", "--no-acl"], sqlenv))
    else:
        run([old, "backup", str(before_backup)], env)
    assert sql("SELECT count(*) FROM schema_migrations") in ("1", "6")

    for _ in range(2):
        with server(candidate, env, base, key):
            assert records() == financial
            assert request(base, key, f"/v1/connections/{connection}")["status"] == "active"
            wait(lambda: request(base, key, "/v1/jobs/job-upgrade")["status"] == "succeeded")
            assert request(base, key, f"/v1/connections/{sid}/status")["transactions"] == 4
            report_env = dict(BASE_ENV, RANJIT_URL=base, RANJIT_API_KEY=key, RANJIT_CONFIG=str(directory / "must-not-exist.json"))
            report = json.loads(run([candidate, "doctor", "--remote", "--json"], report_env))
            assert report["schema_version"] == 1 and report["status"] in ("ok", "warning")
            assert not (directory / "must-not-exist.json").exists()
    assert sql("SELECT count(*) FROM webhook_deliveries") == "0"
    assert sql("SELECT count(*) FROM connection_requests") == "0"
    if config.exists():
        assert config.read_bytes() == original_config

    if not postgres:
        after_backup = directory / "after.db"
        run([candidate, "backup", str(after_backup)], env)
        # Seed one pending notification to prove restore never replays an outbox.
        sql(f"INSERT INTO webhook_deliveries(id,job_id,connection_id,event_type,payload,target_hash,status,attempts,created_at,updated_at,available_at,lease_until,lease_token,error_code) VALUES('evt-fixture',NULL,'con-direct-fixture','connection.activated','{{}}','fixture','queued',0,{now},{now},{now},0,'','')")
        run([candidate, "backup", str(directory / "outbox.db")], env)
        run([candidate, "restore", "--replace", str(directory / "outbox.db")], env)
        assert sql("SELECT status FROM connections WHERE id='con-direct-fixture'") == "paused"
        assert sql("SELECT count(*) FROM webhook_deliveries") == "0"
        assert sql("SELECT count(*) FROM jobs WHERE status IN ('queued','running')") == "0"
        with server(candidate, env, base, key):
            assert records() == financial
        # Roll back using the old executable AND its compatible pre-upgrade copy.
        for suffix in ("", "-wal", "-shm"):
            Path(str(path) + suffix).unlink(missing_ok=True)
        shutil.copy2(before_backup, path)
    else:
        # Roll back only this disposable database, never the caller's database.
        sql("DROP SCHEMA public CASCADE; CREATE SCHEMA public")
        run(["pg_restore", "--no-owner", "--no-acl", "--dbname", sqlenv["PGDATABASE"], str(before_backup)], sqlenv)
    with server(old, env, base, key):
        assert records() == financial
    print(f"{'PostgreSQL' if postgres else 'SQLite'}: actual v0.3.0 upgrade, records, jobs, restart and rollback passed", flush=True)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("candidate", type=Path)
    args = parser.parse_args()
    candidate = args.candidate.resolve()
    with tempfile.TemporaryDirectory(prefix="ranjit-upgrade-") as temporary:
        directory = Path(temporary)
        old = released_binary(directory)
        exercise(old, candidate, directory / "sqlite")
        dsn = os.environ.get("RANJIT_TEST_POSTGRES_URL")
        if dsn:
            admin = pg_env(dsn)
            name = "ranjit_upgrade_" + secrets.token_hex(6)
            run(["createdb", name], admin)
            u = urllib.parse.urlsplit(dsn)
            isolated = urllib.parse.urlunsplit(u._replace(path="/" + name))
            try:
                exercise(old, candidate, directory / "postgres", isolated)
            finally:
                run(["dropdb", "--if-exists", name], admin)


if __name__ == "__main__":
    main()
