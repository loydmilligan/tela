<template>
  <div v-if="$page > 1" class="tela-footer">
    <span>{{ $slidev?.configs?.title || '' }}</span>
    <span>{{ $page }} / {{ $nav.total }}</span>
  </div>
</template>
