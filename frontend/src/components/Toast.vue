<script setup>
import { toastState } from '../toast'
</script>

<template>
  <div class="toast" :class="{ show: toastState.visible }">{{ toastState.msg }}</div>
</template>
