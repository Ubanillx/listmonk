<template>
  <b-modal active scroll="keep" :aria-modal="true" aria-labelledby="send-error-title" width="min(1280px, calc(100vw - 32px))" @close="$emit('close')">
    <div class="modal-card campaign-send-error-report" style="width: auto">
      <header class="modal-card-head">
        <div class="send-error-heading">
          <h2 id="send-error-title" class="modal-card-title">{{ $t('campaigns.sendErrorDetails') }} <span>· {{ campaign.name }}</span></h2>
          <p class="send-error-policy"><b-icon icon="information" size="is-small" />{{ $t('campaigns.sendErrorHistoryHelp') }}</p>
        </div>
        <b-button class="send-error-close" icon-left="close" :aria-label="$t('globals.buttons.close')" @click="$emit('close')" />
      </header>
      <section class="modal-card-body">
        <div class="send-error-overview" :aria-busy="loading" aria-live="polite">
          <div class="send-error-stat">
            <span>{{ $t('campaigns.sendErrorAttempts') }}</span>
            <strong data-cy="send-error-attempts">{{ loading || failed ? '—' : $utils.formatNumber(report.recordedErrors) }}</strong>
          </div>
          <div class="send-error-stat">
            <span>{{ $t('campaigns.sendErrorRecords') }}</span>
            <strong data-cy="send-error-records">{{ loading || failed ? '—' : $utils.formatNumber(report.total) }}</strong>
            <p>{{ $t('campaigns.sendErrorGroupingHelp') }}</p>
          </div>
        </div>
        <div v-if="failed" class="notification is-danger">
          {{ $t('campaigns.sendErrorLoadFailed') }}
          <b-button size="is-small" @click="load">{{ $t('globals.buttons.retry') }}</b-button>
        </div>
        <div v-if="report.historicalErrors" class="notification is-warning">
          {{ $t('campaigns.sendErrorHistoryMissing', { count: report.historicalErrors }) }}
        </div>
        <form class="send-error-toolbar" @submit.prevent="refresh">
          <div class="send-error-search">
            <b-field :label="$t('globals.buttons.search')" label-for="send-error-search" class="lm-search-field">
              <b-input id="send-error-search" v-model.trim="search" icon="magnify" :placeholder="$t('campaigns.sendErrorSearch')"
                maxlength="500" :has-counter="false" />
            </b-field>
          </div>
          <div class="send-error-filter">
            <b-field :label="$t('campaigns.sendErrorReason')" label-for="send-error-category">
              <b-select id="send-error-category" v-model="category" expanded @input="refresh">
                <option value="">{{ $t('globals.terms.all') }}</option>
                <option v-for="reason in categories" :key="reason" :value="reason">{{ reasonLabel(reason) }}</option>
              </b-select>
            </b-field>
          </div>
          <div class="send-error-refresh">
            <b-button native-type="submit" icon-left="refresh" :loading="loading">{{ $t('analytics.refreshReport') }}</b-button>
          </div>
          <div class="send-error-export">
            <b-button type="is-primary" icon-left="download" :disabled="loading || failed || !report.canExport || !report.total"
              :loading="exporting" @click="exportExcel" data-cy="export-send-errors">
              {{ $t('campaigns.sendErrorExport') }}
            </b-button>
            <p>{{ $t('campaigns.sendErrorExportHelp') }}</p>
          </div>
        </form>
        <p v-if="!failed" class="send-error-total">{{ $t('campaigns.sendErrorReportTotal', { count: report.recordedErrors, rows: report.total }) }}</p>
        <b-taglist v-if="!failed && report.reasons.length" class="send-error-reasons">
          <b-tag v-for="reason in report.reasons" :key="reason.category" type="is-light">
            {{ reasonLabel(reason.category) }}: {{ $utils.formatNumber(reason.count) }}
          </b-tag>
        </b-taglist>
        <paginated-table class="send-error-table" :data="failed ? [] : report.results" :loading="loading" :paginated="report.total > 0 && !failed" backend-pagination
          :total="report.total" :per-page="perPage" :current-page="page" @page-change="changePage" @per-page-change="changeSize" hoverable>
          <b-table-column v-slot="props" field="customerCode" :label="$t('campaigns.sendErrorCustomer')">
            <div class="send-error-customer">
              <strong>{{ props.row.customerCode || '—' }}</strong>
              <span>{{ props.row.name || '—' }}</span>
              <small>{{ $t(`customer_lists.types.${props.row.recipientType}`) }} #{{ props.row.recipientId }}</small>
            </div>
          </b-table-column>
          <b-table-column v-slot="props" field="email" :label="$t('customers.email')">
            <span class="send-error-text">{{ props.row.email }}</span>
          </b-table-column>
          <b-table-column v-slot="props" field="category" :label="$t('campaigns.sendErrorReason')">
            <b-tag :class="['send-error-reason', reasonClass(props.row.category)]">{{ reasonLabel(props.row.category) }}</b-tag>
            <p v-if="props.row.smtpCode" class="is-size-7">SMTP {{ props.row.smtpCode }}</p>
            <p class="is-size-7 has-text-grey">{{ $t(`campaigns.sendErrorStages.${props.row.stage}`) }}</p>
          </b-table-column>
          <b-table-column v-slot="props" field="error" :label="$t('campaigns.sendErrorMessage')">
            <div class="send-error-text send-error-message">{{ props.row.error || $t('campaigns.sendErrorMasked') }}</div>
          </b-table-column>
          <b-table-column v-slot="props" field="count" :label="$t('campaigns.sendErrorAttempts')" numeric>
            <strong class="send-error-count">{{ $utils.formatNumber(props.row.count) }}</strong>
          </b-table-column>
          <b-table-column v-slot="props" field="lastAt" :label="$t('campaigns.sendErrorTimes')">
            <p>{{ $utils.niceDate(props.row.firstAt, true) }}</p>
            <p>{{ $utils.niceDate(props.row.lastAt, true) }}</p>
          </b-table-column>
          <template #empty>
            <div v-if="!loading && !failed" class="send-error-empty">
              <b-icon icon="email-check-outline" size="is-large" />
              <p>{{ $t('campaigns.sendErrorEmpty') }}</p>
            </div>
          </template>
        </paginated-table>
      </section>
      <footer class="modal-card-foot">
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
    reasonClass(category) {
      if (['smtp_auth', 'smtp_unavailable', 'reply_unavailable', 'render'].includes(category)) return 'send-error-reason-danger';
      if (category === 'smtp_rejected') return 'send-error-reason-warning';
      return 'send-error-reason-info';
    },
    params() { return { search: this.search, category: this.category }; },
    refresh() { this.page = 1; this.load(); },
    changeSize(size) { this.perPage = size; this.changePage(1); },
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

<style scoped lang="scss">
.campaign-send-error-report {
  color: var(--lm-color-text);
  .modal-card-head { display: flex; align-items: flex-start; background: #fff; border: 0; padding: 28px 28px 0; }
  .modal-card-title { font-size: 24px; font-weight: 600; line-height: 1.4; overflow-wrap: anywhere; }
  .modal-card-title span { font-weight: 400; }
  .modal-card-body { padding: 24px 28px 8px; }
  .modal-card-foot { background: #fff; border: 0; justify-content: flex-end; padding: 12px 28px 24px; }
}
.send-error-heading { flex: 1; min-width: 0; }
.send-error-close.button { border: 0; box-shadow: none; color: var(--lm-color-text-muted); margin-left: 16px; }
.send-error-policy { display: flex; align-items: center; gap: 8px; color: var(--lm-color-text-muted); margin-top: 12px; }
.send-error-policy .icon { color: var(--lm-color-primary); }
.send-error-overview { display: flex; background: #f5f8fc; border-radius: 8px; padding: 24px; margin-bottom: 24px; }
.send-error-stat {
  display: flex; flex-direction: column; gap: 8px; min-width: 200px; color: var(--lm-color-text-muted);
  strong { color: var(--lm-color-text); font-size: 30px; font-weight: 600; line-height: 1.15; font-variant-numeric: tabular-nums; }
  p { font-size: 13px; }
  + .send-error-stat { border-left: 1px solid var(--lm-color-border-strong); margin-left: 24px; padding-left: 28px; }
}
.send-error-toolbar { display: grid; grid-template-columns: minmax(240px, 1fr) 220px auto auto; align-items: start; gap: 16px; margin-bottom: 16px; }
.send-error-toolbar ::v-deep .label { font-size: 13px; color: var(--lm-color-text-muted); margin-bottom: 8px; }
.send-error-search ::v-deep .control, .send-error-search ::v-deep .input { width: 100%; max-width: none; }
.send-error-refresh, .send-error-export { padding-top: 27px; }
.send-error-export p { font-size: 12px; color: var(--lm-color-text-muted); margin-top: 8px; }
.send-error-export .button { width: 100%; }
.send-error-total { font-size: 13px; color: var(--lm-color-text-muted); margin-bottom: 12px; }
.send-error-reasons { margin-bottom: 16px; }
.send-error-customer { display: flex; flex-direction: column; gap: 4px; overflow-wrap: anywhere; }
.send-error-customer strong { font-weight: 500; }
.send-error-customer small { color: var(--lm-color-text-muted); font-size: 11px; }
.send-error-reason.tag { height: auto; white-space: normal; text-align: left; line-height: 1.5; padding: 5px 10px; font-weight: 500; margin-bottom: 4px; }
.send-error-reason-danger { background: #fee4e2; color: #b42318; }
.send-error-reason-warning { background: #fef0c7; color: #93370d; }
.send-error-reason-info { background: #eaf1ff; color: #175cd3; }
.send-error-count { font-weight: 600; font-variant-numeric: tabular-nums; }
.send-error-text { overflow-wrap: anywhere; white-space: pre-wrap; }
.send-error-message { max-width: 320px; min-width: 180px; line-height: 1.6; }
.send-error-empty { padding: 40px 16px; color: var(--lm-color-text-muted); text-align: center; }
.send-error-empty .icon { color: #98b2dc; margin-bottom: 16px; }
.send-error-table ::v-deep {
  .table { font-size: 14px; }
  .table thead th { background: #f3f6fc; color: #526581; font-size: 12px; font-weight: 600; padding: 14px 12px; border-width: 0 0 1px; }
  .table td { padding: 16px 12px; vertical-align: middle; border-color: #edf0f5; }
  .table td:last-child { font-size: 12px; color: var(--lm-color-text-muted); white-space: nowrap; line-height: 1.8; }
  .top.level { margin-top: 20px; }
}
@media (max-width: 1100px) {
  .send-error-toolbar { grid-template-columns: minmax(200px, 1fr) 180px auto; }
  .send-error-export { grid-column: 1 / -1; padding-top: 0; display: flex; align-items: center; justify-content: flex-end; gap: 12px; }
  .send-error-export .button { width: auto; }
  .send-error-export p { margin-top: 0; }
}
@media (max-width: 768px) {
  .campaign-send-error-report {
    .modal-card-head { padding: 20px 16px 0; }
    .modal-card-title { font-size: 20px; }
    .modal-card-title span { display: block; font-size: 14px; margin-top: 6px; }
    .modal-card-body { padding: 20px 16px 8px; }
    .modal-card-foot { padding: 12px 16px 20px; }
  }
  .send-error-overview { padding: 20px 16px; }
  .send-error-stat { min-width: 0; flex: 1; }
  .send-error-stat + .send-error-stat { margin-left: 16px; padding-left: 16px; }
  .send-error-stat strong { font-size: 26px; }
  .send-error-toolbar { grid-template-columns: minmax(0, 1fr) auto; gap: 12px; }
  .send-error-search { grid-column: 1 / -1; }
  .send-error-export { flex-direction: column; align-items: stretch; gap: 0; }
  .send-error-export .button { width: 100%; }
  .send-error-export p { margin-top: 8px; text-align: center; }
  .send-error-message { min-width: 0; max-width: none; }
  .send-error-table ::v-deep .table td { padding: 12px 8px; }
  .send-error-table ::v-deep .table td > :not(.is-empty) { min-width: 0; }
}
</style>
