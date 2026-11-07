#!/usr/bin/env python3
"""Exercise the installer without a network connection or touching the user's PATH."""
import hashlib
import io
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent


class InstallerTest(unittest.TestCase):
    def exercise(self, corrupt=False):
        with tempfile.TemporaryDirectory(prefix="ranjit-install-test-") as temporary:
            directory = Path(temporary)
            shim = directory / "tools"
            shim.mkdir()
            destination = directory / "bin with spaces"
            destination.mkdir()
            (destination / "ranjit").write_text("previous version")
            archive = directory / "fixture.tar.gz"
            content = b"#!/bin/sh\necho fixture\n"
            with tarfile.open(archive, "w:gz") as bundle:
                entry = tarfile.TarInfo("ranjit")
                entry.size = len(content)
                entry.mode = 0o755
                bundle.addfile(entry, io.BytesIO(content))
            checksum = hashlib.sha256(archive.read_bytes()).hexdigest()
            script = '''#!/bin/sh
set -eu
url= output=
while [ "$#" -gt 0 ]; do
  case "$1" in
    https://*) url=$1 ;;
    -o) shift; output=$1 ;;
  esac
  shift
done
case "$url" in
  */checksums.txt) printf '%s  %s\\n' "$FIXTURE_SUM" "$FIXTURE_ASSET" > "$output" ;;
  *) cp "$FIXTURE_ARCHIVE" "$output" ;;
esac
'''
            (shim / "curl").write_text(script)
            (shim / "curl").chmod(0o755)
            system = "darwin" if os.uname().sysname == "Darwin" else "linux"
            arch = "arm64" if os.uname().machine in ("arm64", "aarch64") else "amd64"
            env = dict(os.environ, PATH=str(shim) + os.pathsep + os.environ["PATH"], RANJIT_INSTALL_DIR=str(destination), RANJIT_VERSION="v0.1.0", FIXTURE_SUM="0" * 64 if corrupt else checksum, FIXTURE_ASSET=f"ranjit_v0.1.0_{system}_{arch}.tar.gz", FIXTURE_ARCHIVE=str(archive))
            result = subprocess.run(["sh", str(ROOT / "install.sh")], env=env, text=True, capture_output=True)
            if corrupt:
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual((destination / "ranjit").read_text(), "previous version")
            else:
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual((destination / "ranjit").read_bytes(), content)
                self.assertEqual((destination / "ranjit").stat().st_mode & 0o777, 0o755)

    def test_install_replaces_binary_after_verification(self):
        self.exercise()

    def test_bad_checksum_keeps_existing_binary(self):
        self.exercise(corrupt=True)


if __name__ == "__main__":
    unittest.main()
