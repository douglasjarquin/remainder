import argparse
import base64
import hashlib
import importlib
import importlib.metadata
import json
import os
from pathlib import Path
import sys

parser = argparse.ArgumentParser()
parser.add_argument("--encoding", default="o200k_base")
args = parser.parse_args()

try:
    tiktoken = importlib.import_module("tiktoken")
except ImportError as error:
    print(f"tiktoken is required for offline token measurement: {error}", file=sys.stderr)
    raise SystemExit(1)

def load_local_bpe(blobpath, expected_hash):
    cache_dir = os.environ.get("TIKTOKEN_CACHE_DIR")
    if not cache_dir:
        raise RuntimeError("TIKTOKEN_CACHE_DIR must name a provisioned local encoding cache")
    cache_path = Path(cache_dir) / hashlib.sha1(blobpath.encode()).hexdigest()
    try:
        contents = cache_path.read_bytes()
    except OSError as error:
        raise RuntimeError(f"provisioned encoding data is missing: {cache_path}") from error
    actual_hash = hashlib.sha256(contents).hexdigest()
    if actual_hash != expected_hash:
        raise RuntimeError(f"provisioned encoding hash mismatch: {cache_path}")
    ranks = {}
    for line in contents.splitlines():
        if line:
            token, rank = line.split()
            ranks[base64.b64decode(token)] = int(rank)
    return ranks


openai_public = importlib.import_module("tiktoken_ext.openai_public")

setattr(openai_public, "load_tiktoken_bpe", load_local_bpe)
try:
    encoding = tiktoken.get_encoding(args.encoding)
except Exception as error:
    print(f"offline tokenizer unavailable: {error}", file=sys.stderr)
    raise SystemExit(1)
print(json.dumps({
    "kind": "tokenizer",
    "package": "tiktoken",
    "version": importlib.metadata.version("tiktoken"),
    "encoding": encoding.name,
}, separators=(",", ":")), flush=True)

for line in sys.stdin:
    request = json.loads(line)
    text = request["text"]
    print(json.dumps({
        "id": request["id"],
        "tokens": len(encoding.encode(text)),
    }, separators=(",", ":")), flush=True)
