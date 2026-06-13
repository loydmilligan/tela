<template>
  <div class="slidev-layout tela-section">
    <div v-if="'section' === 'cover'" class="kicker"></div>
    <slot />
  </div>
</template>
