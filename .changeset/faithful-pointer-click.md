---
"@sightmap/sightkick": patch
---

runtime: dispatch a faithful pointer-click sequence so press-gated widgets actually actuate.

The lean click (bare `pointerdown`/`mousedown`/`pointerup`/`mouseup` + `target.click()`, with a virtual-looking zero-size pointer) could not drive widgets that arm their press on a real cursor arriving — notably react-aria `usePress` controls like JetBlue's fare calendar and the checkout `jb-select` dropdowns. `dispatchPointerClick` now emits the full sequence a real click produces: the cursor arrives (`pointerover`/`enter`/`move`), the element is pressed, **focused**, then released, and every pointer event carries real geometry (`width`/`height`/`pressure`) so react-aria treats it as a genuine (not assistive/virtual) pointer. The `focus()` is also the actual trigger for focus-gated overlays once the page holds focus (paired with the sightmap-side focus emulation). Verified end-to-end on JetBlue: `select_fare`, `set_passenger` (Title/Gender), and `set_dob` (three dropdowns) all commit via the runtime.
