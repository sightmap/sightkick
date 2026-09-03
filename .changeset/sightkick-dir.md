---
"@sightmap/sightkick": minor
---

The tool layer moves from a single `webmcp.tools.yaml` file to a `.sightkick/`
directory (a sibling of the corpus's `.sightmap/`). Every `*.yaml` file inside is
merged into one manifest — tools and journeys concatenated, no dependencies
between files — so a large tool layer can be split however helps (e.g. one file
per view). `corpus:` now defaults to the sibling `../.sightmap` and `name:` to the
app dir's name, so both are usually omitted. `sightkick build/browser/call` accept
an app dir (or a `.sightkick` dir) as before. No migration path: rename your
`webmcp.tools.yaml` to `.sightkick/tools.yaml` and drop the `corpus:` line.
