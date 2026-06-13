<template>
  <div class="slidev-layout tela-center">
    <div v-if="'center' === 'cover'" class="kicker"></div>
    <slot />
  </div>
</template>
