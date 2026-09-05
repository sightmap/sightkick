---
"@sightmap/sightkick": patch
---

Runtime `clickElement` now dispatches at the hit-tested element rather than the
resolved node. A real click lands on the topmost element at the target's
coordinates — often an inner child (e.g. `jb-select-option > div.body`) where the
handler lives — so a synthetic sequence dispatched on the resolved ancestor
never reached it and silently no-opped. `clickElement` now scrolls the target
into view and is `async`: it retries across animation frames until
`document.elementFromPoint` at the target's center resolves into the target's own
subtree, then dispatches there (falling back to the node on budget expiry). This
fixes custom dropdown options that failed to commit, including below-the-fold
options where an immediate hit-test raced the post-scroll paint. Unblocks driving
custom `<select>`-style components with the same two clicks a user makes.
