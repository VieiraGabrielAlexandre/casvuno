#!/usr/bin/env bash
# Creates the next sequential golang-migrate pair: NNNNNN_<name>.up.sql / .down.sql
set -euo pipefail

dir="${1:?usage: new-migration.sh <migrations-dir> <snake_case_name>}"
name="${2:-}"
if [[ ! "$name" =~ ^[a-z][a-z0-9_]*$ ]]; then
  echo "usage: make migrate-new name=snake_case_description" >&2
  exit 1
fi

last=0
for f in "$dir"/*.up.sql; do
  [[ -e "$f" ]] || continue
  n=$(basename "$f" | cut -d_ -f1)
  (( 10#$n > last )) && last=$((10#$n))
done

next=$(printf '%06d' $((last + 1)))
for direction in up down; do
  file="$dir/${next}_${name}.${direction}.sql"
  : > "$file"
  echo "created $file"
done
