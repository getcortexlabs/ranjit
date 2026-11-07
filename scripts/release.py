#!/usr/bin/env python3
"""Build portable release archives. Uses only Python's standard library and Go."""
import argparse
import hashlib
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parent.parent
TARGETS = ("linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64", "windows/arm64")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--version", required=True)
    parser.add_argument("--output", default="dist")
    parser.add_argument("--target", action="append", choices=TARGETS)
    args = parser.parse_args()
    if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+(?:-[a-zA-Z0-9.-]+)?", args.version):
        parser.error("version must be a v-prefixed semantic version")
    output = Path(args.output).resolve()
    output.mkdir(parents=True, exist_ok=True)
    checksums = []
    for target in args.target or TARGETS:
        system, arch = target.split("/")
        name = f"ranjit_{args.version}_{system}_{arch}"
        with tempfile.TemporaryDirectory(prefix="ranjit-build-") as temp:
            binary = Path(temp) / ("ranjit.exe" if system == "windows" else "ranjit")
            env = dict(os.environ, CGO_ENABLED="0", GOOS=system, GOARCH=arch)
            subprocess.run(["go", "build", "-trimpath", "-buildvcs=true", "-ldflags", f"-s -w -X main.version={args.version}", "-o", str(binary), "./cmd/ranjit"], cwd=ROOT, env=env, check=True)
            files = [(binary, binary.name), (ROOT / "LICENSE", "LICENSE"), (ROOT / "README.md", "README.md")]
            if system == "windows":
                archive = output / f"{name}.zip"
                with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED) as bundle:
                    for path, entry in files:
                        bundle.write(path, entry)
            else:
                archive = output / f"{name}.tar.gz"
                with tarfile.open(archive, "w:gz") as bundle:
                    for path, entry in files:
                        bundle.add(path, arcname=entry)
            checksums.append(f"{hashlib.sha256(archive.read_bytes()).hexdigest()}  {archive.name}\n")
            print(archive.name, flush=True)
    (output / "checksums.txt").write_text("".join(sorted(checksums)))


if __name__ == "__main__":
    main()
