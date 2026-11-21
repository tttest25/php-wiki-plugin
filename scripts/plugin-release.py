"""Build and verify the fixed Petar runtime archive format."""

import argparse
import gzip
import hashlib
import io
import os
from pathlib import Path
import re
import subprocess
import sys
import tarfile


PLUGIN_FILES = (
    "PetarClient.php",
    "action.php",
    "conf/default.php",
    "conf/metadata.php",
    "lang/en/lang.php",
    "lang/en/settings.php",
    "lang/ru/lang.php",
    "lang/ru/settings.php",
    "plugin.info.txt",
    "script.js",
    "style.css",
    "syntax.php",
)
ARCHIVE_FILES = {"petar/" + path for path in PLUGIN_FILES}
VERSION = r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"
MAX_BYTES = 4 * 1024 * 1024


def git(*args):
    return subprocess.check_output(["git", *args])


def build(commit, version, out):
    if not re.fullmatch(VERSION, version):
        raise ValueError("expected a vMAJOR.MINOR.PATCH version")
    sha = git("rev-parse", "--verify", "--end-of-options", commit + "^{commit}").decode().strip()
    if not out.is_dir() or any(out.iterdir()):
        raise ValueError("OUT_DIR must exist and be empty")

    contents = {}
    for path in PLUGIN_FILES:
        record = git("ls-tree", "-z", sha, "--", path)
        if not re.fullmatch(rb"100644 blob ([a-f0-9]+)\t" + re.escape(path.encode()) + rb"\0", record):
            raise ValueError(f"expected regular 0644 Git blob: {path}")
        contents["petar/" + path] = git("cat-file", "blob", record.split()[2].decode())

    buffer = io.BytesIO()
    with gzip.GzipFile(filename="", mode="wb", fileobj=buffer, mtime=0) as compressed:
        compressed.write(tar_bytes(contents))
    data = buffer.getvalue()
    name = f"petar-{version}.tar.gz"
    validate_members(data)
    with (out / name).open("xb") as asset:
        asset.write(data)
    with (out / "SHA256SUMS").open("x", encoding="ascii") as checksum:
        checksum.write(f"{hashlib.sha256(data).hexdigest()}  {name}\n")
    print(f"Packaged {name} from {sha}")


def tar_bytes(contents):
    buffer = io.BytesIO()
    for name, data in sorted(contents.items()):
        item = tarfile.TarInfo(name)
        item.mode, item.size = 0o644, len(data)
        header = bytearray(item.tobuf(tarfile.USTAR_FORMAT))
        # Device numbers do not apply to regular files. Older Python writes
        # octal zeros there, newer Python leaves the fields NUL-filled.
        header[329:345] = b"\0" * 16
        header[148:156] = b" " * 8
        header[148:156] = f"{sum(header):06o}\0 ".encode("ascii")
        buffer.write(header)
        buffer.write(data)
        buffer.write(b"\0" * (-len(data) % tarfile.BLOCKSIZE))
    # Two end-of-archive blocks, then pad to a complete tar record.
    buffer.write(b"\0" * (2 * tarfile.BLOCKSIZE))
    buffer.write(b"\0" * (-buffer.tell() % tarfile.RECORDSIZE))
    return buffer.getvalue()


def validate_members(data):
    if len(data) > MAX_BYTES:
        raise ValueError("archive exceeds size limit")
    with gzip.GzipFile(fileobj=io.BytesIO(data)) as compressed:
        plain = compressed.read(MAX_BYTES + 1)
    if len(plain) > MAX_BYTES:
        raise ValueError("archive exceeds unpacked size limit")

    contents = {}
    with tarfile.open(fileobj=io.BytesIO(plain), mode="r:") as archive:
        for member in archive:
            if (
                member.name not in ARCHIVE_FILES
                or member.name in contents
                or member.type != tarfile.REGTYPE
                or member.mode != 0o644
                or member.pax_headers
                or member.sparse is not None
            ):
                raise ValueError("archive members must be unique allowlisted regular 0644 files")
            contents[member.name] = archive.extractfile(member).read()
    if contents.keys() != ARCHIVE_FILES:
        raise ValueError("archive members do not match the required files")
    if plain != tar_bytes(contents):
        raise ValueError("archive members do not use the canonical tar representation")
    return contents


def verify(asset, sums, destination):
    if not re.fullmatch(r"petar-" + VERSION + r"\.tar\.gz", asset.name):
        raise ValueError("expected an archive named petar-vMAJOR.MINOR.PATCH.tar.gz")
    if not asset.is_file() or not sums.is_file():
        raise ValueError("archive and checksum must be regular files")
    with sums.open("rb") as checksum:
        line = checksum.read(512)
    match = re.fullmatch(rb"([0-9a-f]{64})  " + re.escape(asset.name.encode()) + rb"\n", line)
    if not match:
        raise ValueError("SHA256SUMS must name exactly this archive")
    with asset.open("rb") as file:
        data = file.read(MAX_BYTES + 1)
    if len(data) > MAX_BYTES:
        raise ValueError("archive exceeds size limit")
    if hashlib.sha256(data).hexdigest().encode() != match[1]:
        raise ValueError("SHA-256 mismatch")
    contents = validate_members(data)
    if destination is not None:
        if not destination.is_dir() or any(destination.iterdir()):
            raise ValueError("extraction directory must exist and be empty")
        # The destination is a private temp directory. Never extract paths or
        # restore metadata from tar; use only the fixed allowlist validated above.
        old_umask = os.umask(0o022)
        try:
            for name, data in contents.items():
                path = destination / name
                path.parent.mkdir(parents=True, exist_ok=True)
                with path.open("xb") as file:
                    file.write(data)
        finally:
            os.umask(old_umask)
    print(f"Verified {asset.name}: SHA-256 and {len(contents)} runtime files")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    actions = parser.add_subparsers(required=True)
    pack = actions.add_parser("build")
    pack.add_argument("commit")
    pack.add_argument("version")
    pack.add_argument("out", type=Path)
    pack.set_defaults(run=build)
    check = actions.add_parser("verify")
    check.add_argument("asset", type=Path)
    check.add_argument("sums", type=Path)
    check.add_argument("destination", nargs="?", type=Path)
    check.set_defaults(run=verify)
    args = vars(parser.parse_args())
    run = args.pop("run")
    try:
        run(**args)
    except (OSError, ValueError, EOFError, tarfile.TarError, subprocess.CalledProcessError) as error:
        sys.exit(f"Plugin release: {error}")
