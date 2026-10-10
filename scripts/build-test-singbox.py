"""Build an isolated test image from an already SHA256-verified official musl binary."""
import argparse
import pathlib
import shutil
import subprocess
import tempfile

parser = argparse.ArgumentParser()
parser.add_argument("--binary", required=True)
parser.add_argument("--image", default="tml-singbox-v14-test")
args = parser.parse_args()
binary = pathlib.Path(args.binary).resolve(strict=True)
with tempfile.TemporaryDirectory(prefix="tml-singbox-image-") as directory:
    context = pathlib.Path(directory)
    shutil.copy2(binary, context / "sing-box")
    (context / "Dockerfile").write_text(
        'FROM alpine:3.23\nCOPY sing-box /usr/bin/sing-box\n'
        'RUN chmod 755 /usr/bin/sing-box\nENTRYPOINT ["/usr/bin/sing-box"]\n', encoding="utf8")
    subprocess.run(["docker", "build", "-t", args.image, str(context)], check=True)
