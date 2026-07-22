<script setup lang="ts">
import { ArrowLeft } from '@lucide/vue'
</script>

<template>
  <section class="panel grid min-h-[60vh] place-items-center p-8 text-center">
    <div>
      <p class="text-7xl font-black text-[var(--brand-soft)]">404</p>
      <h2 class="page-title mt-3">这个页面走丢了</h2>
      <p class="muted mt-2">回到概览继续管理你的媒体库。</p>
      <RouterLink to="/" class="btn btn-primary mt-6"><ArrowLeft :size="17" />返回概览</RouterLink>
    </div>
  </section>
</template>
