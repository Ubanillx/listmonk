<template>
  <div class="campaign-delivery" data-cy="campaign-delivery-overview">
    <div v-if="overviewError" class="notification is-danger" role="alert">
      {{ $t('campaigns.overviewLoadFailed') }}
      <b-button size="is-small" @click="loadOverview">{{ $t('globals.buttons.retry') }}</b-button>
    </div>
    <template v-if="overview && !overviewError">
      <section class="delivery-panel" aria-labelledby="delivery-summary-title">
        <div class="delivery-panel-heading">
          <h2 id="delivery-summary-title">{{ $t('campaigns.deliverySummary') }}</h2>
          <b-button size="is-small" icon-left="refresh" :loading="overviewLoading || listLoading" @click="refresh">
            {{ $t('analytics.refreshReport') }}
          </b-button>
        </div>
        <div class="delivery-metrics" aria-live="polite">
          <div v-for="metric in metrics" :key="metric.key" class="delivery-metric">
            <span>{{ $t(metric.label) }}</span>
            <strong :data-cy="`delivery-${metric.key}`">{{ $utils.formatNumber(metric.value) }}</strong>
          </div>
        </div>
        <p class="delivery-hint">{{ $t('campaigns.deliveryAttemptsNote', { count: $utils.formatNumber(overview.sendErrors) }) }}</p>
      </section>

      <section v-if="overview.organizations.length" class="delivery-panel" aria-labelledby="delivery-organizations-title">
        <div class="delivery-panel-heading"><h2 id="delivery-organizations-title">{{ $t('campaigns.deliveryOrganizations') }}</h2></div>
        <div class="delivery-table-scroll">
          <table class="table is-fullwidth delivery-organization-table">
            <thead>
              <tr>
                <th>{{ $t('campaigns.deliveryOrganization') }}</th>
                <th>{{ $t('campaigns.deliveryProgress') }}</th>
                <th>{{ $t('campaigns.failedRecipients') }}</th>
                <th v-if="canReadFailures">{{ $t('campaigns.deliveryActions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="org in overview.organizations" :key="org.organizationId">
                <td>{{ org.name || $t('campaigns.deliveryOrganizationFallback', { id: org.organizationId }) }}</td>
                <td>
                  <progress class="progress is-primary is-small" :value="org.sent" :max="Math.max(org.planned, 1)" />
                  <span>{{ $utils.formatNumber(org.sent) }} / {{ $utils.formatNumber(org.planned) }}</span>
                </td>
                <td>{{ $utils.formatNumber(org.failed) }}</td>
                <td v-if="canReadFailures">
                  <button type="button" class="button is-text is-small" @click="selectOrganization(org.organizationId)">
                    {{ $t('campaigns.deliveryViewOrganization') }}
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
      <div v-else class="delivery-private-grid">
        <section class="delivery-panel" aria-labelledby="delivery-progress-title">
          <h2 id="delivery-progress-title">{{ $t('campaigns.deliveryProgress') }}</h2>
          <div class="delivery-progress-line">
            <progress class="progress is-primary" :value="overview.sent" :max="Math.max(overview.planned, 1)" />
            <span>{{ Math.round(overview.progress) }}%</span>
          </div>
          <dl class="delivery-facts">
            <div><dt>{{ $t('campaigns.deliverySent') }}</dt><dd>{{ $utils.formatNumber(overview.sent) }}</dd></div>
            <div><dt>{{ $t('campaigns.deliveryWaiting') }}</dt><dd>{{ $utils.formatNumber(overview.waiting) }}</dd></div>
            <div><dt>{{ $t('campaigns.failedRecipients') }}</dt><dd>{{ $utils.formatNumber(overview.failed) }}</dd></div>
          </dl>
        </section>
        <section class="delivery-panel" aria-labelledby="delivery-audience-title">
          <h2 id="delivery-audience-title">{{ $t('campaigns.deliveryAudience') }}</h2>
          <dl class="delivery-facts">
            <div><dt>{{ sourceLabel }}</dt><dd>{{ audienceNames }}</dd></div>
            <div><dt>{{ $t('campaigns.deliverySender') }}</dt><dd>{{ $t(`campaigns.deliverySenderSources.${campaign.smtpSource || 'personal'}`) }}</dd></div>
            <div v-if="campaign.dailySendLimit"><dt>{{ $t('campaigns.deliveryDailyLimit') }}</dt><dd>{{ $utils.formatNumber(campaign.dailySendLimit) }}</dd></div>
          </dl>
          <button type="button" class="button is-text is-small" @click="$emit('show-config')">{{ $t('campaigns.deliveryViewConfig') }}</button>
        </section>
      </div>

      <section v-if="canReadFailures" class="delivery-panel" aria-labelledby="delivery-failed-title">
        <div class="delivery-panel-heading">
          <h2 id="delivery-failed-title">{{ $t('campaigns.failedRecipients') }}</h2>
          <button type="button" class="button is-text is-small" @click="showHistory = true">{{ $t('campaigns.deliveryHistory') }}</button>
        </div>
        <form class="delivery-toolbar" @submit.prevent="applyFilters">
          <div v-if="overview.organizations.length" class="delivery-filter">
            <b-select v-model="organizationId" :aria-label="$t('campaigns.deliveryOrganization')" expanded @input="applyFilters" data-cy="failed-org-filter">
              <option value="">{{ $t('campaigns.deliveryAllOrganizations') }}</option>
              <option v-for="org in overview.organizations" :key="org.organizationId" :value="String(org.organizationId)">{{ org.name }}</option>
            </b-select>
          </div>
          <div class="delivery-filter delivery-search">
            <b-input v-model.trim="search" :aria-label="$t('globals.buttons.search')" icon="magnify"
              :placeholder="$t('campaigns.deliverySearch')" maxlength="500" :has-counter="false" data-cy="failed-search" />
          </div>
          <div class="delivery-filter">
            <b-select v-model="category" :aria-label="$t('campaigns.sendErrorReason')" expanded @input="applyFilters" data-cy="failed-category">
              <option value="">{{ $t('campaigns.deliveryAllReasons') }}</option>
              <option v-for="reason in categories" :key="reason" :value="reason">{{ $t(`campaigns.sendErrorReasons.${reason}`) }}</option>
            </b-select>
          </div>
          <b-button native-type="submit" :loading="listLoading" data-cy="failed-apply">{{ $t('globals.buttons.search') }}</b-button>
          <b-button type="is-primary" outlined icon-left="download" :disabled="!report.canExport || !report.total || listError || listLoading" :loading="exporting"
            @click="exportList" data-cy="failed-export">
            {{ $t('campaigns.deliveryExport') }}
          </b-button>
        </form>
        <div v-if="listError" class="notification is-danger" role="alert">
          {{ $t('campaigns.deliveryListFailed') }} <b-button size="is-small" @click="loadList">{{ $t('globals.buttons.retry') }}</b-button>
        </div>
        <div class="delivery-table-scroll">
          <paginated-table :data="listError ? [] : report.results" :loading="listLoading" :paginated="report.total > 0 && !listError"
            backend-pagination :total="report.total" :per-page="perPage" :current-page="page"
            @page-change="changePage" @per-page-change="changeSize" hoverable data-cy="failed-table">
            <b-table-column width="48" :label="$t('campaigns.deliverySelect')">
              <template #header>
                <b-checkbox :value="allSelected" :indeterminate="someSelected" :disabled="!selectableKeys.length || retrying"
                  :aria-label="$t('campaigns.deliverySelectPage')" @input="togglePage" data-cy="failed-select-page" />
              </template>
              <template #default="props">
                <b-checkbox :value="selectedKeys.includes(rowKey(props.row))" :disabled="!canSelect(props.row) || retrying"
                  :aria-label="$t('campaigns.deliverySelectRecipient', { name: props.row.name || props.row.customerCode || props.row.recipientId })"
                  @input="toggleRow(props.row, $event)" />
              </template>
            </b-table-column>
            <b-table-column v-slot="props" field="name" :label="$t('campaigns.sendErrorCustomer')">
              <strong>{{ props.row.name || '—' }}</strong><small class="delivery-subtext">{{ props.row.customerCode || '—' }}</small>
            </b-table-column>
            <b-table-column v-if="overview.organizations.length" v-slot="props" field="organizationId" :label="$t('campaigns.deliveryOrganization')">
              {{ organizationName(props.row.organizationId) }}
            </b-table-column>
            <b-table-column v-else v-slot="props" field="email" :label="$t('customers.email')">{{ props.row.email }}</b-table-column>
            <b-table-column v-slot="props" field="category" :label="$t('campaigns.deliveryRecentReason')">
              {{ $t(`campaigns.sendErrorReasons.${props.row.category}`) }}
              <small v-if="props.row.smtpCode" class="delivery-subtext">SMTP {{ props.row.smtpCode }}</small>
            </b-table-column>
            <b-table-column v-slot="props" field="status" :label="$t('campaigns.deliveryRetryStatus')">
              <b-tag :type="props.row.retryable ? 'is-success is-light' : 'is-warning is-light'">
                {{ $t(props.row.retryable ? 'campaigns.deliveryEligible' : 'campaigns.deliveryUnavailable') }}
              </b-tag>
              <small v-if="!props.row.retryable" class="delivery-subtext">{{ $t(`campaigns.failedRecipientStatuses.${props.row.status}`) }}</small>
            </b-table-column>
            <template #empty><p v-if="!listLoading && !listError" class="delivery-empty">{{ $t('campaigns.deliveryEmpty') }}</p></template>
          </paginated-table>
        </div>
        <div v-if="report.total && !listError" class="delivery-actions">
          <span>{{ $t('campaigns.deliverySelected', { count: selectedKeys.length }) }}</span>
          <b-button type="is-primary" :disabled="!selectedKeys.length || !report.canRetry || retrying" :loading="retrying"
            @click="confirmRetry" data-cy="failed-retry">
            {{ $t('campaigns.deliveryRetry', { count: selectedKeys.length }) }}
          </b-button>
        </div>
        <p v-if="retryError" class="help is-danger" role="alert">{{ $t('campaigns.deliveryRetryFailed') }}</p>
      </section>
    </template>
    <b-loading :active="overviewLoading && !overview" :is-full-page="false" />
    <campaign-send-errors v-if="showHistory" :campaign="campaign" @close="showHistory = false" />
  </div>
</template>

<script>
import { mapState } from 'vuex';
import CampaignSendErrors from './CampaignSendErrors.vue';

const categories = ['smtp_auth', 'smtp_rejected', 'smtp_temporary', 'timeout', 'network', 'smtp_unavailable', 'reply_unavailable', 'render', 'other'];

export default {
  components: { CampaignSendErrors },
  props: { campaign: { type: Object, required: true }, active: { type: Boolean, default: false } },
  data() {
    return {
      overview: null,
      overviewLoading: false,
      overviewError: false,
      listLoading: false,
      listError: false,
      report: {
        results: [], total: 0, canExport: false, canRetry: false,
      },
      search: '',
      category: '',
      organizationId: '',
      applied: {},
      page: 1,
      perPage: 20,
      selectedKeys: [],
      exporting: false,
      retrying: false,
      retryError: false,
      showHistory: false,
      pollTimer: null,
      overviewRequest: 0,
      listRequest: 0,
      categories,
    };
  },
  computed: {
    ...mapState(['workspace', 'profile']),
    workspaceId() { return Number(this.workspace.organizationId) || 0; },
    canReadFailures() { return this.$canReadCampaignSendErrors(this.campaign); },
    selectableKeys() { return this.report.results.filter(this.canSelect).map(this.rowKey); },
    allSelected() { return this.selectableKeys.length > 0 && this.selectableKeys.every((key) => this.selectedKeys.includes(key)); },
    someSelected() { return this.selectedKeys.length > 0 && !this.allSelected; },
    metrics() {
      return [
        { key: 'planned', label: 'campaigns.deliveryPlanned', value: this.overview.planned },
        { key: 'sent', label: 'campaigns.deliverySent', value: this.overview.sent },
        { key: 'unsent', label: 'campaigns.deliveryUnsent', value: this.overview.unsent },
        { key: 'failed', label: 'campaigns.failedRecipients', value: this.overview.failed },
      ];
    },
    sourceLabel() { return this.$t(`campaigns.deliverySources.${this.overview.source}`); },
    audienceNames() {
      const lists = [...(this.campaign.customerLists || []), ...(this.campaign.customerPools || [])];
      return lists.map((item) => item.name).filter(Boolean).join('、') || '—';
    },
  },
  watch: {
    active(value) { if (value) this.refresh(); else this.invalidate(); },
    workspaceId() {
      this.invalidate(); this.overview = null; this.report = {
        results: [], total: 0, canExport: false, canRetry: false,
      }; this.selectedKeys = [];
    },
  },
  mounted() { if (this.active) this.refresh(); },
  beforeDestroy() { this.invalidate(); },
  methods: {
    invalidate() {
      this.overviewRequest += 1;
      this.listRequest += 1;
      clearTimeout(this.pollTimer);
      this.overviewLoading = false;
      this.listLoading = false;
      this.showHistory = false;
    },
    rowKey(row) { return `${row.recipientType}:${row.recipientId}`; },
    canSelect(row) { return !this.listLoading && !this.listError && this.report.canRetry && row.retryable && row.status !== 'queued'; },
    organizationName(id) {
      const org = this.overview.organizations.find((item) => Number(item.organizationId) === Number(id));
      return org ? org.name : this.$t('campaigns.deliveryOrganizationFallback', { id });
    },
    selectOrganization(id) { this.organizationId = String(id); this.applyFilters(); },
    togglePage(checked) { this.selectedKeys = checked ? [...this.selectableKeys] : []; },
    toggleRow(row, checked) {
      const key = this.rowKey(row);
      this.selectedKeys = checked ? [...this.selectedKeys, key] : this.selectedKeys.filter((item) => item !== key);
    },
    refresh() {
      this.loadOverview();
      if (this.canReadFailures) this.loadList();
    },
    async loadOverview() {
      this.overviewRequest += 1;
      const request = this.overviewRequest;
      this.overviewLoading = true;
      this.overviewError = false;
      try {
        const overview = await this.$api.getCampaignDeliveryOverview(this.campaign.id);
        if (request === this.overviewRequest) {
          this.overview = overview;
          this.$emit('status-updated', overview.status);
          clearTimeout(this.pollTimer);
          if (this.active && overview.status === 'running') this.pollTimer = setTimeout(() => this.refresh(), 10000);
        }
      } catch (error) { if (request === this.overviewRequest) this.overviewError = true; } finally { if (request === this.overviewRequest) this.overviewLoading = false; }
    },
    applyFilters() {
      this.applied = { search: this.search, category: this.category, organization_id: this.organizationId };
      this.page = 1;
      this.loadList();
    },
    changePage(page) { this.page = page; this.loadList(); },
    changeSize(size) { this.perPage = size; this.page = 1; this.loadList(); },
    async loadList() {
      this.listRequest += 1;
      const request = this.listRequest;
      this.listLoading = true;
      this.listError = false;
      this.selectedKeys = [];
      try {
        const report = await this.$api.getCampaignFailedRecipients(
          this.campaign.id,
          { ...this.applied, page: this.page, per_page: this.perPage },
        );
        if (request === this.listRequest) this.report = report;
      } catch (error) { if (request === this.listRequest) this.listError = true; } finally { if (request === this.listRequest) this.listLoading = false; }
    },
    async exportList() {
      const request = this.listRequest;
      this.exporting = true;
      try {
        const blob = await this.$api.exportCampaignFailedRecipients(this.campaign.id, { ...this.applied, lang: this.$i18n.locale });
        if (request !== this.listRequest) return;
        const url = window.URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = `campaign-${this.campaign.id}-failed-recipients.xlsx`;
        document.body.appendChild(link);
        link.click();
        link.remove();
        window.URL.revokeObjectURL(url);
      } finally { this.exporting = false; }
    },
    confirmRetry() {
      const keys = [...this.selectedKeys];
      const request = this.listRequest;
      if (!this.report.canRetry || !keys.length) return;
      const ownerID = Number(this.campaign.ownerUserId || this.campaign.owner_user_id);
      const otherOwner = this.$isPlatformAdmin() && ownerID > 0 && ownerID !== Number(this.profile && this.profile.id);
      const message = otherOwner ? this.$t('campaigns.adminOtherOwnerConfirm', {
        action: this.$t('campaigns.deliveryRetry', { count: keys.length }),
        name: this.campaign.name,
        owner: this.campaign.ownerName || this.campaign.ownerUsername || '-',
      }) : this.$t('campaigns.deliveryRetryConfirm', { count: keys.length });
      this.$utils.confirm(message, () => {
        if (this.active && request === this.listRequest) this.retry(keys);
      }, () => {}, otherOwner ? { type: 'is-warning', focusOn: 'cancel' } : undefined);
    },
    async retry(keys) {
      if (this.retrying) return;
      const request = this.listRequest;
      this.retrying = true;
      this.retryError = false;
      try {
        const recipients = keys.map((key) => {
          const [type, id] = key.split(':');
          return { type, id: Number(id) };
        });
        await this.$api.retryCampaignFailedRecipients(this.campaign.id, recipients);
        if (request !== this.listRequest) return;
        this.$emit('retry-started');
        this.$utils.toast(this.$t('campaigns.deliveryRetryStarted', { count: keys.length }));
        this.refresh();
      } catch (error) {
        if (request === this.listRequest) { this.retryError = true; this.refresh(); }
      } finally { this.retrying = false; }
    },
  },
};
</script>

<style scoped lang="scss">
.campaign-delivery { max-width: 1120px; position: relative; color: var(--lm-color-text); }
.delivery-panel { background: var(--lm-color-surface); border: 1px solid var(--lm-color-border); border-radius: var(--lm-radius-md); padding: 24px; margin-bottom: 20px; }
.delivery-panel h2 { font-size: 18px; font-weight: 600; margin-bottom: 16px; }
.delivery-panel-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.delivery-panel .button.is-text { color: var(--lm-color-primary); font-size: 13px; text-decoration: none; }
.delivery-panel .button.is-primary { color: white; background: var(--lm-color-primary); border-color: var(--lm-color-primary); }
.delivery-panel .button.is-primary.is-outlined { color: var(--lm-color-primary); background: transparent; }
.delivery-metrics { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); margin: 8px 0 16px; }
.delivery-metric { display: flex; flex-direction: column; gap: 4px; padding: 0 20px; border-left: 1px solid var(--lm-color-border); }
.delivery-metric:first-child { border-left: 0; padding-left: 0; }
.delivery-metric span, .delivery-hint, .delivery-subtext { color: var(--lm-color-text-muted); font-size: 12px; }
.delivery-metric strong { font-size: 28px; line-height: 1.3; font-variant-numeric: tabular-nums; }
.delivery-private-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 20px; }
.delivery-private-grid .progress { margin: 12px 0; }
.delivery-progress-line { display: flex; align-items: center; gap: 12px; }
.delivery-progress-line .progress { flex: 1; min-width: 0; }
.delivery-progress-line span { color: var(--lm-color-text-muted); font-size: 12px; }
.progress.is-primary::-webkit-progress-value { background-color: var(--lm-color-primary); }
.progress.is-primary::-moz-progress-bar { background-color: var(--lm-color-primary); }
.campaign-delivery ::v-deep .tag.is-success { color: var(--lm-color-success); background: #e8f5ee; }
.campaign-delivery ::v-deep .tag.is-warning { color: var(--lm-color-warning); background: #fff3df; }
.delivery-facts > div { display: flex; justify-content: space-between; gap: 16px; padding: 7px 0; border-bottom: 1px solid var(--lm-color-border); }
.delivery-facts + .button { height: auto; padding: 8px 0; }
.delivery-facts dt { color: var(--lm-color-text-muted); }
.delivery-facts dd { text-align: right; overflow-wrap: anywhere; }
.delivery-table-scroll { overflow-x: auto; }
.delivery-organization-table { min-width: 560px; }
.delivery-organization-table td { vertical-align: middle; }
.delivery-organization-table .progress { width: 160px; margin: 0 0 5px; }
.delivery-toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; margin-bottom: 16px; }
.delivery-filter { width: 180px; }
.delivery-search { flex: 1 1 230px; }
.delivery-filter ::v-deep .control, .delivery-filter ::v-deep .select, .delivery-filter ::v-deep .select select, .delivery-filter ::v-deep .input { width: 100%; }
.delivery-subtext { display: block; margin-top: 3px; }
.delivery-empty { text-align: center; padding: 32px 12px; color: var(--lm-color-text-muted); }
.delivery-actions { display: flex; justify-content: space-between; align-items: center; gap: 12px; margin-top: 16px; }
.campaign-delivery ::v-deep .table thead th { background: var(--lm-color-surface-subtle); font-weight: 700; padding: 8px 12px; }
.campaign-delivery ::v-deep .table td { vertical-align: middle; padding: 6px 12px; font-size: 13px; line-height: 1.4; }
.campaign-delivery ::v-deep .table .delivery-subtext { margin-top: 0; line-height: 1.4; }
.campaign-delivery ::v-deep .table td strong { font-weight: 600; }
@media (max-width: 768px) {
  .delivery-panel { padding: 16px; }
  .delivery-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); row-gap: 16px; }
  .delivery-metric:nth-child(3) { border-left: 0; padding-left: 0; }
  .delivery-private-grid { grid-template-columns: 1fr; gap: 0; }
  .delivery-filter { width: 100%; }
  .delivery-toolbar > .button { flex: 1; }
  .campaign-delivery ::v-deep .table td { padding: 10px 12px; }
}
</style>
