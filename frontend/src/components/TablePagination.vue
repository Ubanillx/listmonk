<template>
  <div class="top level app-table-pagination">
    <div class="level-left"><slot /></div>
    <div v-if="paginated" class="level-right">
      <app-pagination :total="total" :current="currentPage" :per-page="perPage"
        @change="changePage" @per-page-change="changePaginationSize" />
    </div>
  </div>
</template>

<script>
import AppPagination from './AppPagination.vue';

// Adapter for Buefy 0.9's table paginator: keep its toolbar slots and events.
export default {
  name: 'TablePagination',
  components: { AppPagination },
  inject: ['changePaginationSize'],
  props: {
    paginated: Boolean,
    total: { type: [Number, String], default: 0 },
    currentPage: { type: [Number, String], default: 1 },
    perPage: { type: [Number, String], default: 20 },
  },
  methods: {
    changePage(page) {
      this.$emit('update:currentPage', page);
      this.$emit('page-change', page);
    },
  },
};
</script>
