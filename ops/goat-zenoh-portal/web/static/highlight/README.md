# highlight.js bundle

Pinned vendor copy of [highlight.js](https://highlightjs.org/) 11.10.0 —
syntax coloring for the portal's `/schema.html` viewer. Same bundle
pattern as `web/static/swagger/` next door: downloaded once, checked
into the repo, served from the portal binary's embedded FS.

## What is in here

| File | sha256 | Purpose |
|---|---|---|
| `highlight.min.js` | `471ef9ae90c407af440fcdc48edfeeb562106b3267bd12d99071c162fb52ed32` | core |
| `github.min.css` | `3a9a5def8b9c311e5ae43abde85c63133185eed4f0d9f67fea4b00a8308cf066` | theme |
| `languages/rust.min.js` | `586150754442e85a7d26d22194c7aa79570c307814b138851e492d4cd623c9b3` | language plugin |
| `languages/go.min.js` | `546f12bfa8883538c5142f3dda3673b3f2e4055974ea43ed55878d3988797344` | language plugin |
| `languages/python.min.js` | `2c3dcc1fd2d8688bf399c4fee8c220fe9a09b4abd439d89f6d2cb2c767731a55` | language plugin |
| `languages/typescript.min.js` | `3ad23d510617c45f3f924e17cfed5864cdcd46064147c8d289ccca85cc41117b` | language plugin |
| `languages/javascript.min.js` | `5a43b9f27d9a30002da874e83999e60373e9e67b4121809c543168a7aef925bc` | dep of typescript |
| `languages/protobuf.min.js` | `22faafea7ddd968f9f4fd73791400c36a3e1a24005c86903dd7f6479cb144206` | language plugin |

## How to refresh

```bash
HLJS_VER=11.10.0
BASE="https://cdnjs.cloudflare.com/ajax/libs/highlight.js/${HLJS_VER}"
curl -fsSL "$BASE/highlight.min.js"                -o highlight.min.js
curl -fsSL "$BASE/styles/github.min.css"           -o github.min.css
for L in rust go python typescript javascript protobuf; do
  curl -fsSL "$BASE/languages/$L.min.js" -o "languages/$L.min.js"
done
shasum -a 256 *.js *.css languages/*.js   # verify against the table above
```

If you bump the version, update the sha256 column in this README and
mention the bump in the same change-set so reviewers can verify the
new hashes against an independent download.

## License

highlight.js is BSD-3-Clause; see
[github.com/highlightjs/highlight.js/blob/main/LICENSE](https://github.com/highlightjs/highlight.js/blob/main/LICENSE).
Compatible with our preferred-permissive posture per ADR 0111.
