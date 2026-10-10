"""Download pinned official integration-test binaries with release SHA256 checks.

Usage: python3 scripts/download-test-cores.py [--musl]
Linux test/CI helper only; never used by the production service.
"""
import hashlib
import pathlib
import sys
import tarfile
import urllib.request
import zipfile

musl = "--musl" in sys.argv
targets = [
    ("apernet/hysteria", "app/v2.13.0", "hysteria-linux-amd64", "907ba8c9693edb104b20582681fb7dc15639d5b64a9cbb616a7b539190a86691", "hysteria2"),
    ("v2fly/v2ray-core", "v5.53.0", "v2ray-linux-64.zip", "6bbb8aee65a57d0b12599b4b7c842b3ad0daca4436e661d94015c447cb31b4fa", "v2fly"),
    ("SagerNet/sing-box", "v1.14.3", "sing-box-1.14.3-linux-amd64-musl.tar.gz" if musl else "sing-box-1.14.3-linux-amd64.tar.gz", "41fc81fd041134d4d7d850a928d7d2cde17a7d6563953f406b22d32a68bfab1f" if musl else "e2bdf179c15a3652955dc44867e33ca0965c9c9b91b34267dcc5a5d639a5feee", "singbox"),
]
root = pathlib.Path(__file__).resolve().parents[1] / ".tools" / "official-cores"
for repo, tag, name, digest, folder in targets:
    destination = root / folder
    destination.mkdir(parents=True, exist_ok=True)
    archive = destination / name
    with urllib.request.urlopen(f"https://github.com/{repo}/releases/download/{tag}/{name}", timeout=120) as response:
        data = response.read()
    if hashlib.sha256(data).hexdigest() != digest:
        raise SystemExit(f"SHA256 mismatch: {name}")
    archive.write_bytes(data)
    if name.endswith(".zip"):
        with zipfile.ZipFile(archive) as source:
            source.extractall(destination)
    elif name.endswith(".tar.gz"):
        with tarfile.open(archive) as source:
            source.extractall(destination, filter="data")
    else:
        archive.chmod(0o755)
    for binary in destination.rglob("sing-box"):
        binary.chmod(0o755)
    for binary in destination.rglob("v2ray"):
        binary.chmod(0o755)
    print(f"Verified official {repo} {tag}: {destination}")
