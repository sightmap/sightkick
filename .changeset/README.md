# Changesets

This directory drives sightkick's versioning and changelog via
[changesets](https://github.com/changesets/changesets).

- Add a changeset for any user-facing change to the published `@sightmap/sightkick`
  package: `npm run changeset` (pick `patch`/`minor`/`major`) and commit the
  generated `.changeset/*.md`. Pure infra/docs changes can skip it.
- On pushes to `main`, the `release` workflow opens/updates a "Version Packages"
  PR that bumps `generator/npm/package.json` and writes its changelog. Merging
  that PR tags `vX.Y.Z`, which drives goreleaser + the npm publish.

See `.github/workflows/release.yml` for the full flow.
