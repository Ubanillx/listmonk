<template>
  <b-modal active scroll="keep" :aria-modal="true" :width="1200" @close="$emit('close')">
    <div class="modal-card campaign-send-error-report" style="width: auto">
      <header class="modal-card-head">
        <h2 class="modal-card-title">{{ $t('campaigns.sendErrorDetails') }} · {{ campaign.name }}</h2>
      </header>
      <section class="modal-card-body">
        <p class="mb-4">{{ $t('campaigns.sendErrorReportHelp') }}</p>
        <div v-if="failed" class="notification is-danger">
          {{ $t('campaigns.sendErrorLoadFailed') }}
          <b-button size="is-small" @click="load">{{ $t('globals.buttons.retry') }}</b-button>
        </div>
        <div v-if="report.historicalErrors" class="notification is-warning">
          {{ $t('campaigns.sendErrorHistoryMissing', { count: report.historicalErrors }) }}
        </div>
        <form class="columns is-vcentered" @submit.prevent="refresh">
          <div class="column is-6">
            <b-field :label="$t('globals.buttons.search')">
              <b-input v-model.trim="search" :placeholder="$t('campaigns.sendErrorSearch')" maxlength="500" />
            </b-field>
          </div>
          <div class="column is-3">
            <b-field :label="$t('campaigns.sendErrorReason')">
              <b-select v-model="category" expanded @input="refresh">
                <option value="">{{ $t('globals.terms.all') }}</option>
                <option v-for="reason in categories" :key="reason" :value="reason">{{ reasonLabel(reason) }}</option>
              </b-select>
            </b-field>
          </div>
          <div class="column is-narrow">
            <b-button native-type="submit" icon-left="refresh" :loading="loading">{{ $t('analytics.refreshReport') }}</b-button>
          </div>
        </form>
        <p class="mb-3">{{ $t('campaigns.sendErrorReportTotal', { count: report.recordedErrors, rows: report.total }) }}</p>
        <b-taglist>
          <b-tag v-for="reason in report.reasons" :key="reason.category" type="is-light">
            {{ reasonLabel(reason.category) }}: {{ $utils.formatNumber(reason.count) }}
          </b-tag>
        </b-taglist>
        <b-table :data="report.results" :loading="loading" :paginated="report.total > perPage" backend-pagination
          :total="report.total" :per-page="perPage" :current-page="page" @page-change="changePage" hoverable>
          <b-table-column v-slot="props" field="customerCode" :label="$t('customers.customerCode')">
            {{ props.row.customerCode }}
            <p class="is-size-7 has-text-grey">{{ $t(`customer_lists.types.${props.row.recipientType}`) }} #{{ props.row.recipientId }}</p>
          </b-table-column>
          <b-table-column v-slot="props" field="name" :label="$t('globals.fields.name')">{{ props.row.name }}</b-table-column>
          <b-table-column v-slot="props" field="email" :label="$t('customers.email')">
            <span class="send-error-text">{{ props.row.email }}</span>
          </b-table-column>
          <b-table-column v-slot="props" field="category" :label="$t('campaigns.sendErrorReason')">
            {{ reasonLabel(props.row.category) }}
            <p v-if="props.row.smtpCode" class="is-size-7">SMTP {{ props.row.smtpCode }}</p>
            <p class="is-size-7 has-text-grey">{{ $t(`campaigns.sendErrorStages.${props.row.stage}`) }}</p>
          </b-table-column>
          <b-table-column v-slot="props" field="error" :label="$t('campaigns.sendErrorMessage')">
            <div class="send-error-text send-error-message">{{ props.row.error || $t('campaigns.sendErrorMasked') }}</div>
          </b-table-column>
          <b-table-column v-slot="props" field="count" :label="$t('campaigns.sendErrors')" numeric>{{ props.row.count }}</b-table-column>
          <b-table-column v-slot="props" field="lastAt" :label="$t('campaigns.sendErrorTimes')">
            <p>{{ $utils.niceDate(props.row.firstAt, true) }}</p>
            <p>{{ $utils.niceDate(props.row.lastAt, true) }}</p>
          </b-table-column>
          <template #empty><p class="has-text-centered p-4">{{ $t('campaigns.sendErrorEmpty') }}</p></template>
        </b-table>
      </section>
      <footer class="modal-card-foot">
        <b-button type="is-primary" icon-left="download" :disabled="loading || failed || !report.canExport || !report.total"
          :loading="exporting" @click="exportExcel" data-cy="export-send-errors">
          {{ $t('campaigns.sendErrorExport') }}
        </b-button>
        <b-button @click="$emit('close')">
          {{ $t('globals.buttons.close') }}
        </b-button>
      </footer>
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
      category: '',
      page: 1,
      perPage: 20,
      loading: false,
      exporting: false,
      failed: false,
      requestID: 0,
      reportFilters: {},
      report: {
        results: [], reasons: [], total: 0, recordedErrors: 0, historicalErrors: 0, canExport: false,
      },
      categories: ['smtp_auth', 'smtp_rejected', 'smtp_temporary', 'timeout', 'network', 'smtp_unavailable', 'reply_unavailable', 'render', 'other'],
    };
  },
  computed: {
    ...mapState(['workspace']),
    organizationID() { return Number(this.workspace.organizationId) || 0; },
  },
  watch: {
    organizationID() { this.requestID += 1; this.$emit('close'); },
  },
  mounted() { this.load(); },
  beforeDestroy() { this.requestID += 1; },
  methods: {
    reasonLabel(category) { return this.$t(`campaigns.sendErrorReasons.${category}`); },
    params() { return { search: this.search, category: this.category }; },
    refresh() { this.page = 1; this.load(); },
    changePage(page) { this.page = page; this.load(); },
    async load() {
      this.requestID += 1;
      const { requestID } = this;
      const params = this.params();
      this.loading = true;
      this.failed = false;
      try {
        const report = await this.$api.getCampaignSendErrors(this.campaign.id, { ...params, page: this.page, per_page: this.perPage });
        if (requestID === this.requestID) {
          this.report = report;
          this.reportFilters = params;
        }
      } catch (error) {
        if (requestID === this.requestID) this.failed = true;
      } finally {
        if (requestID === this.requestID) this.loading = false;
      }
    },
    async exportExcel() {
      const { requestID } = this;
      this.exporting = true;
      try {
        const blob = await this.$api.exportCampaignSendErrors(this.campaign.id, { ...this.reportFilters, lang: this.$i18n.locale });
        if (requestID !== this.requestID) return;
        const url = window.URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = `campaign-${this.campaign.id}-send-errors.xlsx`;
        document.body.appendChild(link);
        link.click();
        link.remove();
        window.URL.revokeObjectURL(url);
      } finally {
        this.exporting = false;
      }
    },
  },
};
</script>

<style scoped>
.send-error-text { overflow-wrap: anywhere; white-space: pre-wrap; }
.send-error-message { max-width: 320px; }
.campaign-send-error-report .modal-card-title { overflow-wrap: anywhere; }
</style>
