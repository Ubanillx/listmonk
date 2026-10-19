<template>
  <div class="app-pagination" data-cy="pagination">
    <div class="pagination-size">
      <span>{{ $t('pagination.perPage') }}</span>
      <b-select :value="pageSize" size="is-small" :aria-label="$t('pagination.perPage')"
        data-cy="page-size" @input="changeSize">
        <option v-for="size in sizes" :key="size" :value="size">{{ size }}</option>
      </b-select>
    </div>
    <b-pagination :total="total || 0" :current="current" :per-page="pageSize" size="is-small"
      :range-before="1" :range-after="1" v-bind="paginationLabels" @change="changePage" />
    <form class="pagination-jump" @submit.prevent="jump">
      <label>
        <span>{{ $t('pagination.goToPage') }}</span>
        <input v-model="targetPage" class="input is-small" type="number" min="1" :max="pageCount" step="1"
          :aria-label="$t('pagination.goToPage')" data-cy="page-jump" />
      </label>
      <span class="pagination-page-count">/ {{ pageCount }}</span>
      <b-button native-type="submit" size="is-small" data-cy="page-jump-submit">{{ $t('pagination.go') }}</b-button>
    </form>
  </div>
</template>

<script>
export default {
  name: 'AppPagination',
  props: {
    total: { type: [Number, String], default: 0 },
    current: { type: [Number, String], default: 1 },
    perPage: { type: [Number, String], default: 20 },
  },
  data() {
    return { targetPage: this.current };
  },
  computed: {
    pageSize() { return Number(this.perPage) > 0 ? Number(this.perPage) : 20; },
    sizes() { return [...new Set([10, 20, 50, 100, this.pageSize])].sort((a, b) => a - b); },
    pageCount() { return Math.max(1, Math.ceil((Number(this.total) || 0) / this.pageSize)); },
    paginationLabels() {
      return {
        ariaNextLabel: this.$t('globals.buttons.nextPage'),
        ariaPreviousLabel: this.$t('globals.buttons.previousPage'),
      };
    },
  },
  watch: {
    current(value) { this.targetPage = value; },
    perPage() { this.targetPage = this.current; },
  },
  methods: {
    changePage(value) {
      const page = Math.min(this.pageCount, Math.max(1, value));
      this.targetPage = page;
      if (page === Number(this.current)) return;
      this.$emit('update:current', page);
      this.$emit('change', page);
    },
    jump() {
      const page = Number(this.targetPage);
      if (this.targetPage === '' || !Number.isInteger(page)) {
        this.targetPage = this.current;
        return;
      }
      this.changePage(page);
    },
    changeSize(value) {
      const size = Number(value);
      if (size !== this.pageSize) this.$emit('per-page-change', size);
    },
  },
};
</script>
