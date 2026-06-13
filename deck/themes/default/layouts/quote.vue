<template>
  <div class="slidev-layout tela-quote">
    <div v-if="'quote' === 'cover'" class="kicker"></div>
    <slot />
  </div>
</template>
