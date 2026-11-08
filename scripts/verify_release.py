#!/usr/bin/env python3
"""Verify archives locally, then published downloads, SDK and anonymous images."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import subprocess
import sys
import tarfile
import tempfile
import urllib.request
import zipfile

TARGETS = ("linux_amd64", "linux_arm64", "darwin_amd64", "darwin_arm64", "windows_amd64", "windows_arm64")


def archives(directory, version, online=False):
    sums = {}
    for line in (directory / "checksums.txt").read_text().splitlines():
        digest, name = line.split()
        if name in sums or Path(name).name != name or not re.fullmatch(r"[0-9a-f]{64}", digest):
            raise ValueError("invalid checksum entry")
        sums[name] = digest
    for target in TARGETS:
        windows = target.startswith("windows")
        name = f"ranjit_{version}_{target}.{'zip' if windows else 'tar.gz'}"
        path = directory / name
        assert hashlib.sha256(path.read_bytes()).hexdigest() == sums[name], f"checksum mismatch: {name}"
        if windows:
            with zipfile.ZipFile(path) as bundle:
                assert {"ranjit.exe", "LICENSE", "README.md"} <= set(bundle.namelist())
        else:
            with tarfile.open(path) as bundle:
                assert {"ranjit", "LICENSE", "README.md"} <= set(bundle.getnames())
    for name, digest in sums.items():
        assert hashlib.sha256((directory / name).read_bytes()).hexdigest() == digest, f"checksum mismatch: {name}"
    system = "darwin" if platform.system() == "Darwin" else "linux"
    arch = "arm64" if platform.machine() in ("arm64", "aarch64") else "amd64"
    with tempfile.TemporaryDirectory(prefix="ranjit-release-check-") as temporary:
        env = {k: v for k, v in os.environ.items() if not k.startswith("RANJIT_")}
        destination = Path(temporary) / "bin with spaces"
        env.update(RANJIT_INSTALL_DIR=str(destination), RANJIT_VERSION=version)
        if not online:
            # Before publication, only the HTTPS transport is substituted. The
            # installer verifies and installs the actual candidate archive.
            shim = Path(temporary) / "tools"
            shim.mkdir()
            curl = shim / "curl"
            curl.write_text(f"#!{sys.executable}\n" + '''import os, pathlib, shutil, sys, urllib.parse
args = sys.argv[1:]
url = next(a for a in args if a.startswith("https://"))
name = pathlib.PurePosixPath(urllib.parse.urlsplit(url).path).name
shutil.copyfile(pathlib.Path(os.environ["RELEASE_FIXTURES"]) / name, args[args.index("-o") + 1])
''')
            curl.chmod(0o755)
            env["PATH"] = str(shim) + os.pathsep + env["PATH"]
            env["RELEASE_FIXTURES"] = str(directory.resolve())
        installer = directory / "install.sh" if online else Path(__file__).resolve().parent.parent / "install.sh"
        subprocess.run(["sh", installer], env=env, check=True, stdout=subprocess.DEVNULL)
        binary = destination / "ranjit"
        assert subprocess.check_output([binary, "version"], text=True).strip() == version
        env["RANJIT_CONFIG"] = str(Path(temporary) / "config.json")
        subprocess.run([binary, "demo"], env=env, check=True, stdout=subprocess.DEVNULL)
        rows = json.loads(subprocess.check_output([binary, "transactions", "--json"], env=env))
        assert len(rows) == 8, "first-use fixture missing (two sandbox institutions)"
    print("Six archives: checksums and contents passed; native binary version and first use passed", flush=True)


def images(version):
    # An empty Docker config prevents local/GitHub credentials masking visibility.
    with tempfile.TemporaryDirectory(prefix="ranjit-anonymous-") as temporary:
        for arch in ("amd64", "arm64"):
            subprocess.run(["docker", "--config", temporary, "pull", "--platform", f"linux/{arch}", f"ghcr.io/getcortexlabs/ranjit:{version}"], check=True)
    print("Anonymous pulls passed for linux/amd64 and linux/arm64", flush=True)


def published(version):
    base = f"https://github.com/getcortexlabs/ranjit/releases/download/{version}/"
    with tempfile.TemporaryDirectory(prefix="ranjit-published-") as temporary:
        directory = Path(temporary)
        with urllib.request.urlopen(base + "checksums.txt", timeout=60) as response:
            checksums = response.read()
        (directory / "checksums.txt").write_bytes(checksums)
        for line in checksums.decode().splitlines():
            _, name = line.split()
            if Path(name).name != name:
                raise ValueError("invalid asset path")
            with urllib.request.urlopen(base + name, timeout=120) as response, (directory / name).open("wb") as output:
                while chunk := response.read(1 << 20):
                    output.write(chunk)
        archives(directory, version, online=True)
        env = dict(os.environ, GOPROXY="direct")
        module = json.loads(subprocess.check_output(["go", "mod", "download", "-json", f"github.com/getcortexlabs/ranjit@{version}"], env=env))
        assert module["Version"] == version and module["Sum"], "published SDK unavailable"
    images(version)
    print("Public downloads, Go SDK tag and container distribution passed", flush=True)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--version", required=True)
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument("--artifacts", type=Path)
    group.add_argument("--images", action="store_true")
    group.add_argument("--published", action="store_true")
    args = parser.parse_args()
    if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+", args.version):
        parser.error("expected stable semantic version")
    if args.artifacts:
        archives(args.artifacts, args.version)
    elif args.images:
        images(args.version)
    else:
        published(args.version)


if __name__ == "__main__":
    main()
