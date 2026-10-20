<!--
  Copyright (C) 2023 Nethesis S.r.l.
  SPDX-License-Identifier: GPL-3.0-or-later
-->
<template>
  <cv-side-nav id="app-side-nav" fixed expanded class="app-side-nav">
    <AppSideMenuContent />
  </cv-side-nav>
</template>

<script>
import AppSideMenuContent from "@/components/AppSideMenuContent";

export default {
  name: "AppSideMenu",
  components: { AppSideMenuContent },
};
</script>

<style scoped lang="scss">
@import "../styles/carbon-utils";

// Carbon's expanded selector outranks the parent's collapsed .app-side-nav rule.
// Keep the actual navigation box (not only its label) out of the form on touch.
@media (max-width: $breakpoint-large) {
  #app-side-nav.bx--side-nav {
    width: $side-menu-collapsed-width;
  }

  @media (hover: hover) and (pointer: fine) {
    #app-side-nav.bx--side-nav:hover {
      width: $side-menu-width;
    }
  }
}
</style>
