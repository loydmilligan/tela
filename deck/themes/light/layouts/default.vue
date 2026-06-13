<template>
  <div class="slidev-layout tela-default">
    <div v-if="'default' === 'cover'" class="kicker"></div>
    <slot />
  </div>
</template>
