#!/bin/bash
# Build dist/recipe-to-cooklang.zip (for claude.ai / Claude Code) from the embedded SKILL.md.
# Usage: scripts/build-skill.sh [DATA_DIR]   (DATA_DIR is used to list the current tag vocabulary)
set -euo pipefail
cd "$(dirname "$0")/.."
DATA="${1:-./data}"
OUT=dist/recipe-to-cooklang
rm -rf "$OUT" dist/recipe-to-cooklang.zip
mkdir -p "$OUT/references" "$OUT/scripts"
cp internal/importer/skill/SKILL.md "$OUT/SKILL.md"
cp scripts/validate.py "$OUT/scripts/validate.py"
{
  echo "# Tag vocabulary"
  echo
  echo "Tags already used in the recipe collection (count). Reuse these; new tags only when nothing fits."
  echo
  if ls "$DATA"/recipes/*.cook >/dev/null 2>&1; then
    awk 'FNR==1{f=0;t=0} /^---$/{f++; next} f==1 && /^tags:/{t=1; next} f==1 && t && /^[[:space:]]*- /{sub(/^[[:space:]]*- /,""); print; next} f==1 && /^[A-Za-z]/{t=0}' "$DATA"/recipes/*.cook \
      | sort | uniq -c | sort -rn | awk '{print "- " $2 " (" $1 ")"}'
  fi
} > "$OUT/references/tags.md"
(cd dist && zip -qr recipe-to-cooklang.zip recipe-to-cooklang)
echo "built dist/recipe-to-cooklang.zip"
