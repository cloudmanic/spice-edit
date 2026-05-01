# spice-edit.com

The marketing + docs site for [SpiceEdit](https://github.com/cloudmanic/spice-edit). Hugo + Tailwind v4. Static, single-binary deploy.

## Requirements

- **Hugo extended** (≥ 0.135) — `brew install hugo`
- **Node** (≥ 18) — for the Tailwind CLI

## Build

```sh
cd website
npm install
npm run build:css        # produces static/css/site.css
hugo --minify            # produces public/
```

`public/` contains the deployable static site. Drop it on any host (GitHub Pages, Cloudflare Pages, S3 + CloudFront, etc.). The `static/CNAME` ships through to `public/CNAME` automatically.

## Develop

In one terminal:

```sh
cd website
npm run dev:css          # watches Tailwind input, rebuilds site.css on save
```

In a second terminal:

```sh
cd website
hugo server              # serves on http://localhost:1313 with live reload
```

You can also run them in parallel via:

```sh
cd website
npm run dev
```

## Layout

- `hugo.toml` — site config, params (`github_url`, `version`, `releases_url`, …)
- `assets/css/tailwind.css` — Tailwind v4 entry. CSS-first config via `@theme`. Tokyo Night palette mirrors `internal/theme/theme.go`.
- `assets/js/` — Vanilla JS (OS detect, copy buttons, site glue). Hugo asset pipeline minifies + fingerprints.
- `content/` — Markdown content. `_index.md` files set section metadata; `docs/*.md` are the documentation pages.
- `layouts/` — Custom Hugo layouts. No theme dependency.
  - `index.html` — homepage
  - `docs/single.html` — individual doc page (sidebar + prose)
  - `docs/list.html` — docs index
  - `404.html` — custom 404
  - `partials/` — shared building blocks (head, header, footer, install-block, code-block, docs-sidebar, feature-icon)
- `static/` — verbatim files: `CNAME`, `robots.txt`, `favicon.svg`. Tailwind output (`static/css/site.css`) is gitignored and produced by `build:css`.

## Customizing

- Site-wide colors live in `assets/css/tailwind.css` under `@theme { ... }`. They mirror the editor's actual palette.
- The version badge / hero release marker reads `params.version` in `hugo.toml`.
- The install commands embedded in the homepage and `/docs/installation/` are duplicated by design — the homepage hero and install section are content-managed in `layouts/index.html` and `layouts/partials/install-block.html`.
- Screenshots are placeholder boxes labeled `[screenshot: name]`. Drop real PNGs into `static/img/` and replace the `<div class="screenshot-placeholder">` markup with `<img>` tags when ready.

## Performance

- One CSS file (~30 kB minified), three small JS files (defer-loaded).
- Fonts (Inter + JetBrains Mono) load from Google Fonts with `font-display: swap`.
- No analytics, no trackers, no third-party requests at runtime besides fonts.
