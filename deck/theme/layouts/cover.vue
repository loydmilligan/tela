<template>
  <div class="slidev-layout tela-cover">
    <div v-if="'cover' === 'cover'" class="kicker"></div>
    <slot />
  </div>
</template>
