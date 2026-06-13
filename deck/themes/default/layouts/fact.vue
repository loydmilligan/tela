<template>
  <div class="slidev-layout tela-fact">
    <div v-if="'fact' === 'cover'" class="kicker"></div>
    <slot />
  </div>
</template>
