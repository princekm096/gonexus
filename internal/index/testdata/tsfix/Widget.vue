<script setup lang="ts">
import { greet } from "./util";

function hello() {
  return greet("world");
}
</script>

<template>
  <div>{{ hello() }}</div>
</template>
