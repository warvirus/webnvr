#!/bin/bash
# decoder.ts 포맷 전환 로직 회귀 테스트 — Node 하네스 (WebCodecs 모의)
set -e
cd "$(dirname "$0")/.."
npx esbuild src/services/decoder.ts --bundle --format=esm --outfile=/tmp/decoder-test.mjs --log-level=error
sed "s|'./decoder.mjs'|'/tmp/decoder-test.mjs'|" tests/verify-decoder.mjs > /tmp/verify-decoder-run.mjs
node /tmp/verify-decoder-run.mjs
