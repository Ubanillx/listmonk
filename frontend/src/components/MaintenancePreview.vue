<template>
  <div class="maintenance-preview" role="status" aria-live="polite" :data-cy="`maintenance-preview-${dataset}`">
    <p v-if="busy" class="has-text-grey">{{ $t('maintenance.previewLoading') }}</p>
    <div v-else-if="error" class="has-text-danger">
      {{ $t('maintenance.previewError') }}
      <b-button type="is-text" size="is-small" @click="load">{{ $t('maintenance.refreshPreview') }}</b-button>
    </div>
    <template v-else-if="preview">
      <p><strong>{{ $t('maintenance.previewCount', { count: preview.count, unit }) }}</strong></p>
      <p v-if="dataset === 'analytics'" class="help">
        {{ $t('maintenance.analyticsPreview', { views: preview.views, clicks: preview.clicks }) }}
      </p>
      <p v-if="beforeDate" class="help">{{ $t('maintenance.beforeExact', { date: $utils.niceDate(beforeDate, true) }) }}</p>
      <b-button type="is-text" size="is-small" @click="load">{{ $t('maintenance.refreshPreview') }}</b-button>
    </template>
  </div>
</template>

<script>
export default {
  props: {
    dataset: { type: String, required: true },
    type: { type: String, required: true },
    beforeDate: { type: Date, default: null },
    unit: { type: String, required: true },
  },
  data() {
    return {
      preview: null, busy: false, error: false, revision: 0,
    };
  },
  computed: {
    signature() { return `${this.dataset}/${this.type}/${this.beforeDate ? this.beforeDate.toISOString() : ''}`; },
  },
  watch: {
    signature: { immediate: true, handler() { this.load(); } },
  },
  beforeDestroy() { this.revision += 1; },
  methods: {
    async load() {
      this.revision += 1;
      const { revision } = this;
      this.preview = null;
      this.busy = true;
      this.error = false;
      this.$emit('ready', false);
      try {
        const preview = await this.$api.previewMaintenance(this.dataset, this.type, this.beforeDate);
        if (revision !== this.revision) return null;
        this.preview = preview;
        this.$emit('ready', preview.count > 0);
        return preview;
      } catch (error) {
        if (revision === this.revision) this.error = true;
        return null;
      } finally {
        if (revision === this.revision) this.busy = false;
      }
    },
  },
};
</script>
