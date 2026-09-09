import argparse
import importlib
import importlib.metadata
import json
import sys

parser = argparse.ArgumentParser()
parser.add_argument("--encoding", default="o200k_base")
args = parser.parse_args()

try:
    tiktoken = importlib.import_module("tiktoken")
except ImportError as error:
    print(f"tiktoken is required for offline token measurement: {error}", file=sys.stderr)
    raise SystemExit(1)

encoding = tiktoken.get_encoding(args.encoding)
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
