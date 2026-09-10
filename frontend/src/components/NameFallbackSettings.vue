<template>
  <section class="name-fallback" data-cy="name-fallback-settings">
    <b-checkbox v-model="enabled" :disabled="disabled" data-cy="name-fallback-enabled">
      {{ $t('nameFallback.enable') }}
    </b-checkbox>
    <b-field v-if="enabled" :label="$t('nameFallback.label')" label-position="on-border" class="mt-3">
      <b-input v-model="fallback" :disabled="disabled" required maxlength="200"
        data-cy="name-fallback-value" />
    </b-field>
  </section>
</template>

<script>
export default {
  props: {
    value: { type: Object, required: true },
    disabled: { type: Boolean, default: false },
  },
  computed: {
    enabled: {
      get() { return !!this.value.enabled; },
      set(enabled) { this.$emit('input', { ...this.value, enabled }); },
    },
    fallback: {
      get() { return this.value.value || ''; },
      set(value) { this.$emit('input', { ...this.value, value }); },
    },
  },
};
</script>

<style lang="scss" scoped>
.name-fallback {
  margin: 1rem 0;
  padding: .25rem 0;
}
</style>
