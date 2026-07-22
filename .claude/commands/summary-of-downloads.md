---
description: Summarize SpiceEdit GitHub release downloads (totals, by platform, by version)
allowed-tools: Bash(gh api:*), Bash(jq:*)
---

Run the following and present the results as a clean summary with three sections (Grand total, By platform, Top 10 versions). Note in the output that the count includes Homebrew installs, CI, mirrors, and scrapers — it's an upper bound, not unique humans. Exclude `checksums.txt` from totals.

```bash
gh api repos/cloudmanic/spice-edit/releases --paginate > /tmp/spiceedit_releases.json

echo "=== GRAND TOTAL ==="
jq '[.[] | (.assets // [])[] | select(.name != "checksums.txt") | .download_count] | add' /tmp/spiceedit_releases.json

echo ""
echo "=== BY PLATFORM ==="
jq -r '[.[] | (.assets // [])[] | select(.name != "checksums.txt")] | group_by(.name | capture("_(?<p>(darwin|linux|windows)_(amd64|arm64))") | .p) | map({platform: (.[0].name | capture("_(?<p>(darwin|linux|windows)_(amd64|arm64))") | .p), total: (map(.download_count) | add)}) | sort_by(-.total) | .[] | "\(.total)\t\(.platform)"' /tmp/spiceedit_releases.json

echo ""
echo "=== BY VERSION (top 10) ==="
jq -r '[.[] | {tag: .tag_name, published: .published_at, total: ((.assets // []) | map(select(.name != "checksums.txt") | .download_count) | add // 0)}] | sort_by(-.total) | .[0:10] | .[] | "\(.total)\t\(.tag)\t\(.published)"' /tmp/spiceedit_releases.json
```

After presenting the summary, briefly call out anything notable (e.g. a release that's an obvious outlier, dominant platform, recent vs. older traction).
