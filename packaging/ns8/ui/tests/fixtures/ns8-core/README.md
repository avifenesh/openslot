# NS8 parent stylesheet fixture

The seven SCSS files are unmodified copies from NethServer/ns8-core,
commit `35aa12403ca08bb2052f96fecab7805f41db4e1d`, directory
[`core/ui/src/styles`](https://github.com/NethServer/ns8-core/tree/35aa12403ca08bb2052f96fecab7805f41db4e1d/core/ui/src/styles).
They are NethServer/Nethesis code, licensed GPL-3.0-or-later by that project,
like the NS8 module UI. They are test-only and are not bundled into dist/.

The Playwright harness compiles `_core.scss` with the locked Carbon and Sass
packages, resolving webpack-style `~` imports without modifying these sources,
and serves the result at `/cluster-admin/css/core.css`, the real module's
parent-provided URL. Do not replace this with a minimal CSS imitation: doing so
previously hid a real mobile sidebar overlay and unstyled form controls.

To refresh, fetch all seven SCSS files from the same pinned upstream commit
and update this reference. Rerun all eight browser tests, inspect the generated
desktop/mobile screenshots, and check hostname computed styles and overflow.
