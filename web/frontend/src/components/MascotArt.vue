<script setup lang="ts">
import { computed } from 'vue'
import { mascotFor, type MascotScene } from '../utils/mascot'

const props = withDefaults(defineProps<{
  scene: MascotScene
  decorative?: boolean
}>(), {
  decorative: false,
})

const asset = computed(() => mascotFor(props.scene))
</script>

<template>
  <img
    :src="asset.src"
    :alt="decorative ? '' : asset.alt"
    :aria-hidden="decorative ? 'true' : undefined"
    :data-mascot-scene="scene"
    :data-mascot-kind="asset.kind"
  />
</template>
