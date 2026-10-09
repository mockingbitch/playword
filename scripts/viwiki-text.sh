#!/bin/sh
# Turns the Vietnamese Wikipedia parquet files (wikimedia/wikipedia, CC BY-SA 4.0)
# into one plain-text file for `playword-cli freq -corpus`. Runs pyarrow in a
# throwaway container so nothing is installed on the host.
#
#   scripts/viwiki-text.sh <dir>     # <dir> receives parquet/ and viwiki.txt
set -eu
dir=$(realpath "${1:?usage: viwiki-text.sh <dir>}")
mkdir -p "$dir/parquet"
base=https://huggingface.co/datasets/wikimedia/wikipedia/resolve/main/20231101.vi
for i in 0 1 2 3; do
  f="train-0000$i-of-00004.parquet"
  [ -s "$dir/parquet/$f" ] || curl -sfL -o "$dir/parquet/$f" "$base/$f"
done
docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -v "$dir":/data python:3.13-slim sh -c '
  pip install -q --target /tmp/py pyarrow && python -I -c "
import sys; sys.path.insert(0, \"/tmp/py\")
import glob, pyarrow.parquet as pq
with open(\"/data/viwiki.txt\", \"w\", encoding=\"utf-8\") as out:
    for f in sorted(glob.glob(\"/data/parquet/*.parquet\")):
        for batch in pq.ParquetFile(f).iter_batches(columns=[\"text\"], batch_size=2000):
            for t in batch.column(0).to_pylist():
                out.write(t)
                out.write(\"\n\")
"'
ls -la "$dir/viwiki.txt"
