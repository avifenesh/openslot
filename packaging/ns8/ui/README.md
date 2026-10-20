# NS8 Calnode UI development

See the [NS8 module UI developer manual](https://nethserver.github.io/ns8-core/ui/modules/#module-ui-development).

Requires Node 24+ and Yarn 4.18.0 (the version in `packageManager`).

```sh
yarn install --immutable
RAYON_NUM_THREADS=2 yarn build
yarn exec playwright install chromium
yarn test
```

`yarn serve` runs the development server on port 8080; `yarn watch` rebuilds
`dist/` for the NS8 development container. Production assets use relative URLs,
retain `/cluster-admin/css/core.css`, and include the public metadata,
translations and shortcuts consumed by NS8.

## Vue 3 compatibility migration

The runtime is **Vue 3.5.44's official `@vue/compat` migration build**, not Vue 2
and not a patched third-party fork. Vite replaces the end-of-life Vue CLI /
webpack build graph. The existing NS8 components, event bus, task service,
Vuex store, router and translations are retained: migrating those all at once
would risk the parent NS8 API contract and change the UI unnecessarily.

The explicit `vue: 3.5.44` Yarn resolution prevents Carbon 2's *direct*
`vue: ^2.7.10` dependency from reinstalling Vue 2 and its compiler. Vite aliases
**all** Vue imports to the official, same-version compatibility build, with
compiler compatibility mode 2. This is a tested runtime migration, not an
advisory-ignore rule or a claim that Carbon 2 natively supports Vue 3. Yarn's
Vue-2 peer-range warnings are expected until NS8's library supports Vue 3.
Do not remove the alias, resolution or compiler mode independently.

The only Node crypto API used by ns8-ui-lib is UUID v4's `randomBytes(16)`;
`src/crypto.js` supplies that using Web Crypto's CSPRNG. No elliptic/stream/VM
polyfills are needed. NS8's HTTPS origin provides the secure browser context.

Tests exercise the **built production bundle** in a same-origin iframe, with
an explicitly mocked NS8 parent, authenticated task endpoints and completion /
abort / validation events. They cover status/services/images/volumes, settings
load/save/reload and exact payloads, parent navigation, backup/log links, about,
mobile viewport, language fallback and task-creation errors. The harness serves
an authentic, pinned NS8 parent stylesheet (see `tests/fixtures/ns8-core/README.md`),
checks hostname computed styles and desktop/mobile horizontal overflow, and writes
styled settings screenshots to `test-results/`. CI runs these tests.
They are not a substitute for install/update/backup/restore tests on a real NS8
cluster; no live cluster was available for this change.

Residual maintenance debt: Carbon 2 / carbon-components and vue-i18n 8 remain
legacy compatibility dependencies. `yarn npm audit --all --recursive` currently
reports their **deprecation notices**, but no security advisory. Moving off the
compatibility build should track an NS8 UI library release supporting native
Vue 3 rather than force-upgrading Carbon beneath its private mixins.
