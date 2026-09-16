<template>
  <section class="pool-contacts">
    <header class="columns page-header">
      <div class="column is-10">
        <h1 class="title is-4">
          {{ $t('globals.terms.customers') }}
          <span v-if="list"> &raquo; {{ list.name }}</span>
        </h1>
        <p class="help">{{ $t('pool.listViewHelp') }}</p>
      </div>
    </header>

    <section class="customers-controls">
      <div class="columns">
        <div class="column is-8">
          <form @submit.prevent="loadContacts">
            <b-field addons>
              <b-input v-model.trim="customerCode" expanded icon="magnify"
                :placeholder="$t('pool.searchPlaceholder')" />
              <p class="controls">
                <b-button native-type="submit" type="is-primary" icon-left="magnify">
                  {{ $t('pool.search') }}
                </b-button>
              </p>
            </b-field>
          </form>
        </div>
      </div>
    </section>

    <b-table :data="contacts" :loading="loading" :mobile-cards="false" hoverable>
      <b-table-column v-slot="props" field="customerCode" :label="$t('pool.tableCustomerCode')">
        {{ props.row.customerCode || props.row.customer_code || '-' }}
      </b-table-column>
      <b-table-column v-slot="props" field="companyName" :label="$t('pool.tableCompanyName')">
        {{ props.row.companyName || props.row.company_name || '-' }}
      </b-table-column>
      <b-table-column v-slot="props" field="email" :label="$t('pool.tableEmail')">
        {{ props.row.email || '-' }}
      </b-table-column>
      <b-table-column v-slot="props" field="allocationDepartment" :label="$t('pool.tableDepartment')">
        {{ props.row.allocationDepartment || props.row.allocation_department || '-' }}
      </b-table-column>
      <b-table-column v-slot="props" field="status" :label="$t('pool.tableStatus')">
        {{ contactStatus(props.row) }}
      </b-table-column>

      <template #empty>
        <empty-placeholder :label="$t('globals.messages.emptyState')" />
      </template>
    </b-table>
  </section>
</template>

<script>
import Vue from 'vue';
import EmptyPlaceholder from '../components/EmptyPlaceholder.vue';

export default Vue.extend({
  components: {
    EmptyPlaceholder,
  },

  data() {
    return {
      list: null,
      contacts: [],
      customerCode: '',
      loading: false,
    };
  },

  methods: {
    loadContacts() {
      const id = Number(this.$route.params.customerListID);
      if (!id) {
        return Promise.resolve();
      }

      this.loading = true;
      const params = this.customerCode ? { customer_code: this.customerCode } : {};
      return this.$api.getPoolContacts(id, params)
        .then((rows) => {
          this.contacts = Array.isArray(rows) ? rows : [];
        })
        .finally(() => {
          this.loading = false;
        });
    },

    contactStatus(contact) {
      if (contact.excluded) {
        return this.$t('pool.statusRemoved');
      }
      return contact.status === 'archived'
        ? this.$t('pool.statusArchived')
        : this.$t('pool.statusNormal');
    },
  },

  mounted() {
    const id = Number(this.$route.params.customerListID);
    Promise.all([
      this.$api.getList(id),
      this.loadContacts(),
    ]).then(([list]) => {
      this.list = list;
    });
  },
});
</script>
