<template>
  <b-modal class="campaign-unsubscribe-modal" active scroll="keep" :aria-modal="true" aria-labelledby="unsubscribe-title"
    width="min(1040px, calc(100vw - 32px))" @close="$emit('close')">
    <div class="modal-card campaign-unsubscribe-report" style="width: auto" data-cy="unsubscribe-report">
      <header class="modal-card-head">
        <h2 id="unsubscribe-title" class="modal-card-title">{{ $t('campaigns.unsubscribeDetails') }} · {{ campaign.name }}</h2>
        <b-button icon-left="close" :aria-label="$t('globals.buttons.close')" @click="$emit('close')" />
      </header>
      <section class="modal-card-body">
        <p class="help mb-4">{{ $t('campaigns.unsubscribesHelp') }}</p>
        <form class="unsubscribe-toolbar" @submit.prevent="refresh">
          <b-field :label="$t('globals.buttons.search')" label-for="unsubscribe-search">
            <b-input id="unsubscribe-search" v-model.trim="search" icon="magnify" :placeholder="$t('campaigns.unsubscribeSearch')"
              maxlength="500" :has-counter="false" />
          </b-field>
          <b-field :label="$t('globals.fields.status')" label-for="unsubscribe-status">
            <b-select id="unsubscribe-status" v-model="status" @input="changePage(1)">
              <option value="all">{{ $t('campaigns.unsubscribeAll') }}</option>
              <option value="clicked">{{ $t('campaigns.unsubscribeClicked') }}</option>
              <option value="confirmed">{{ $t('campaigns.unsubscribeConfirmed') }}</option>
            </b-select>
          </b-field>
          <b-button native-type="submit" icon-left="refresh" :loading="loading">{{ $t('analytics.refreshReport') }}</b-button>
        </form>
        <div v-if="failed" class="notification is-danger" role="alert">
          {{ $t('campaigns.unsubscribeLoadFailed') }}
          <b-button size="is-small" @click="load">{{ $t('globals.buttons.retry') }}</b-button>
        </div>
        <paginated-table :data="failed ? [] : report.results" :loading="loading" :paginated="!failed && report.total > 0" backend-pagination
          :total="report.total" :per-page="perPage" :current-page="page" @page-change="changePage" @per-page-change="changeSize" hoverable>
          <b-table-column v-slot="props" field="customerCode" :label="$t('customers.customerCode')">
            {{ props.row.customerCode || '—' }}
          </b-table-column>
          <b-table-column v-slot="props" field="name" :label="$t('globals.fields.name')">{{ props.row.name || '—' }}</b-table-column>
          <b-table-column v-slot="props" field="email" :label="$t('customers.email')">{{ props.row.email || '—' }}</b-table-column>
          <b-table-column v-slot="props" field="recipientType" :label="$t('exports.recipientType')">
            {{ $t(`customer_lists.types.${props.row.recipientType}`) }}
          </b-table-column>
          <b-table-column v-slot="props" field="status" :label="$t('globals.fields.status')">
            <b-tag :type="props.row.status === 'confirmed' ? 'is-danger is-light' : 'is-warning is-light'">
              {{ $t(props.row.status === 'confirmed' ? 'campaigns.unsubscribeConfirmed' : 'campaigns.unsubscribeClicked') }}
            </b-tag>
          </b-table-column>
          <b-table-column v-slot="props" field="clickedAt" :label="$t('campaigns.unsubscribeClickedAt')">
            {{ props.row.clickedAt ? $utils.niceDate(props.row.clickedAt, true) : '—' }}
          </b-table-column>
          <b-table-column v-slot="props" field="confirmedAt" :label="$t('campaigns.unsubscribedAt')">
            {{ props.row.confirmedAt ? $utils.niceDate(props.row.confirmedAt, true) : '—' }}
          </b-table-column>
          <template #empty><p v-if="!loading && !failed" class="py-5 has-text-centered">{{ $t('campaigns.unsubscribeEmpty') }}</p></template>
        </paginated-table>
      </section>
      <footer class="modal-card-foot"><b-button @click="$emit('close')">{{ $t('globals.buttons.close') }}</b-button></footer>
    </div>
  </b-modal>
</template>

<script>
import { mapState } from 'vuex';

export default {
  props: { campaign: { type: Object, required: true } },
  data() {
    return {
      search: '',
      status: 'all',
      appliedSearch: '',
      page: 1,
      perPage: 20,
      loading: false,
      failed: false,
      requestID: 0,
      report: { results: [], total: 0 },
    };
  },
  computed: {
    ...mapState(['workspace']),
    organizationID() { return Number(this.workspace.organizationId) || 0; },
  },
  watch: { organizationID() { this.requestID += 1; this.$emit('close'); } },
  mounted() { this.load(); },
  beforeDestroy() { this.requestID += 1; },
  methods: {
    refresh() { this.appliedSearch = this.search; this.page = 1; this.load(); },
    changePage(page) { this.page = page; this.load(); },
    changeSize(size) { this.perPage = size; this.changePage(1); },
    async load() {
      this.requestID += 1;
      const { requestID } = this;
      this.loading = true;
      this.failed = false;
      try {
        const report = await this.$api.getCampaignUnsubscribes(this.campaign.id, {
          search: this.appliedSearch, status: this.status, page: this.page, per_page: this.perPage,
        });
        if (requestID === this.requestID) this.report = report;
      } catch (error) {
        if (requestID === this.requestID) this.failed = true;
      } finally {
        if (requestID === this.requestID) this.loading = false;
      }
    },
  },
};
</script>

<style>
.campaign-unsubscribe-modal .modal-content { max-height: calc(100dvh - 32px); overflow: hidden; }
.campaign-unsubscribe-report.modal-card { margin: 0; max-height: calc(100dvh - 32px); }
.campaign-unsubscribe-report .modal-card-head { display: flex; align-items: flex-start; gap: 1rem; }
.campaign-unsubscribe-report .modal-card-title { flex: 1; min-width: 0; white-space: normal; overflow-wrap: anywhere; line-height: 1.4; }
.campaign-unsubscribe-report .modal-card-head .button { flex-shrink: 0; }
.campaign-unsubscribe-report .unsubscribe-toolbar { display: flex; align-items: flex-end; gap: 1rem; margin-bottom: 1.25rem; }
.campaign-unsubscribe-report .unsubscribe-toolbar .field { flex: 1; margin-bottom: 0; }
@media (max-width: 768px) {
  .campaign-unsubscribe-report .unsubscribe-toolbar { flex-direction: column; align-items: stretch; }
}
</style>
