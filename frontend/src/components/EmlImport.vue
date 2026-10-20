<template>
  <span class="eml-import">
    <b-button :disabled="disabled || busy" :loading="busy" icon-left="file-import-outline"
              data-cy="btn-import-eml" @click="$refs.file.click()">
      {{ $t('campaigns.editor.importEml') }}
    </b-button>
    <input ref="file" type="file" accept=".eml,.msg,message/rfc822,application/vnd.ms-outlook" hidden
           data-cy="eml-file" :disabled="disabled || busy" :aria-label="$t('campaigns.editor.importEml')" @change="onFile">
    <p v-if="error" class="help is-danger" role="alert" data-cy="eml-error">{{ error }}</p>
  </span>
</template>

<script>
import { importEml } from '../utils/importEml';

export default {
  props: { disabled: { type: Boolean, default: false } },
  data: () => ({ busy: false, error: '', disposed: false }),
  methods: {
    onFile(event) {
      const input = event.target;
      const file = input.files?.[0];
      input.value = '';
      if (!file || this.disabled || this.busy) return;
      this.$utils.confirm(this.$t('campaigns.editor.emlConfirm'), () => this.importFile(file));
    },
    async importFile(file) {
      if (this.disabled || this.busy || this.disposed) return;
      this.busy = true;
      this.error = '';
      this.$emit('busy', true);
      try {
        const result = await importEml(file, (data) => {
          if (this.disposed || this.disabled) throw new Error('cancelled');
          return this.$api.uploadMedia(data);
        });
        if (this.disposed || this.disabled) return;
        this.$emit('imported', result);
        await this.$nextTick();
        this.$utils.toast(this.$t('campaigns.editor.emlSuccess', { images: result.media.length, ignored: result.ignored }));
      } catch (error) {
        if (!this.disposed) {
          const known = ['invalid', 'size', 'empty', 'missing', 'unsupported'];
          this.error = this.$t(`campaigns.editor.emlError.${known.includes(error.message) ? error.message : 'upload'}`);
        }
      } finally {
        this.busy = false;
        this.$emit('busy', false);
      }
    },
  },
  beforeDestroy() { this.disposed = true; },
};
</script>

<style scoped>
.eml-import { max-width: 24rem; }
.help { text-align: left; white-space: normal; }
</style>
