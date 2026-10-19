import { BTable } from 'buefy/dist/esm/table';
import TablePagination from './TablePagination.vue';

// Extend rather than wrap so Buefy columns, refs, selection and toolbar slots
// retain their original table parent and behavior.
export default {
  name: 'PaginatedTable',
  extends: BTable,
  components: { BTablePagination: TablePagination },
  provide() {
    return { changePaginationSize: (size) => this.$emit('per-page-change', size) };
  },
};
