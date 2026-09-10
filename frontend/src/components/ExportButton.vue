<template>
  <span v-if="allowed" class="export-action">
    <b-button icon-left="download" data-cy="export-open" @click="open">{{ $t('export.open') }}</b-button>
    <b-modal :active.sync="active" has-modal-card :can-cancel="!busy">
      <form class="modal-card has-text-left" style="width: 560px; max-width: 95vw" @submit.prevent="submit">
        <header class="modal-card-head"><p class="modal-card-title">{{ $t('export.title') }}</p></header>
        <section class="modal-card-body">
          <div v-if="jobs.length || jobError" ref="exportResults" class="mb-4" data-cy="export-results" aria-live="polite">
            <p class="has-text-weight-semibold mb-2">{{ $t('export.recent') }}</p>
            <div v-for="job in jobs" :key="job.id" class="box p-3">
              <p style="overflow-wrap: anywhere">{{ job.filename }}</p>
              <p class="help">{{ jobStatus(job) }} · {{ job.rowCount }} {{ $t('export.rows') }}</p>
              <b-button
                v-if="jobStatus(job) === $t('export.statusComplete')"
                tag="a" :href="downloadURL(job)" size="is-small" icon-left="download"
                data-cy="export-download">{{ $t('export.download') }}</b-button>
              <p v-if="job.error" class="help is-danger">{{ job.error }}</p>
            </div>
            <p v-if="jobError" class="help is-danger">{{ jobError }} <a href="#" @click.prevent="refreshJobs">{{ $t('export.retry') }}</a></p>
          </div>
          <b-field :label="$t('export.typeLabel')"><b-select v-model="form.type" expanded data-cy="export-type">
            <option v-for="(label, key) in types" :key="key" :value="key">{{ $t(label) }}</option>
          </b-select></b-field>
          <b-field :label="$t('export.formatLabel')"><b-select v-model="form.format" expanded data-cy="export-format">
            <option value="xlsx">Excel (.xlsx)</option><option value="csv">CSV (.csv)</option>
          </b-select></b-field>
          <b-field v-if="selected.length" :label="$t('export.scopeLabel')"><b-select v-model="selection" expanded>
            <option value="filtered">{{ $t('export.scopeFiltered') }}</option><option value="selected">{{ $t('export.scopeSelected', { count: selected.length }) }}</option>
          </b-select></b-field>
          <p class="mb-3">{{ $t('export.workspaceNote', { name: workspace.organizationName || $t('organizations.personalSpace') }) }}</p>
          <p v-if="form.query" class="notification is-info">{{ $t('export.queryNote', { query: form.query }) }}</p>
          <b-field :label="$t('export.searchLabel')"><b-input v-model="form.search" maxlength="500" /></b-field>
          <b-field v-if="['customers', 'blocklist'].includes(form.type)" :label="$t('export.statusLabel')">
            <b-select v-model="form.status" expanded><option value="">{{ $t('globals.terms.all') }}</option><option value="enabled">{{ $t('export.statusEnabled') }}</option>
              <option value="disabled">{{ $t('export.statusDisabled') }}</option><option value="blocklisted">{{ $t('export.statusBlocklisted') }}</option></b-select>
          </b-field>
          <customer-list-selector :label="$t('export.filterLists')" :placeholder="$t('export.filterListsPlaceholder')" :all="options.lists" :selected="selectedLists" @input="selectedLists = $event" />
          <customer-list-selector v-if="['campaigns', 'activity', 'bounces', 'bounce_customers'].includes(form.type)"
            :label="$t('export.filterCampaigns')" :placeholder="$t('export.filterCampaignsPlaceholder')" :all="options.campaigns" :selected="selectedCampaigns" @input="selectedCampaigns = $event" />
          <template v-if="dateSupported">
            <b-field :label="$t('export.fromLabel')"><b-input v-model="fromText" type="datetime-local" /></b-field>
            <b-field :label="$t('export.toLabel')"><b-input v-model="toText" type="datetime-local" /></b-field>
            <p class="help">{{ $t('export.dateHelp') }}</p>
          </template>
          <p class="help mt-3">{{ $t('export.retentionHelp') }}</p>
          <p v-if="form.type === 'campaigns'" class="help">{{ $t('export.poolHelp') }}</p>
        </section>
        <footer class="modal-card-foot">
          <b-button native-type="submit" type="is-primary" :loading="busy" data-cy="export-submit">{{ $t('export.submit') }}</b-button>
          <b-button :disabled="busy" @click="active = false">{{ $t('globals.buttons.close') }}</b-button>
        </footer>
      </form>
    </b-modal>
  </span>
</template>

<script>
import { mapState } from 'vuex';
import { exportTypes, canExport } from '../exports';
import CustomerListSelector from './CustomerListSelector.vue';

const localText = (value) => (value ? new Date(new Date(value).getTime() + 8 * 3600000).toISOString().slice(0, 16) : '');

export default {
  components: { CustomerListSelector },
  props: {
    kind: { type: String, default: '' }, filters: { type: Object, default: () => ({}) }, selected: { type: Array, default: () => [] },
  },
  data: () => ({
    active: false,
    busy: false,
    form: {},
    selection: 'filtered',
    fromText: '',
    toText: '',
    options: { lists: [], campaigns: [] },
    selectedLists: [],
    selectedCampaigns: [],
    jobs: [],
    jobError: '',
    pollTimer: null,
    generation: 0,
  }),
  computed: {
    ...mapState(['workspace', 'profile']),
    allowed() { return canExport(this.$store.state); },
    types() {
      const groups = {
        customers: ['customers', 'blocklist'],
        bounces: ['bounces', 'bounce_customers'],
        campaigns: ['campaigns', 'activity'],
        lists: ['lists', 'customers', 'blocklist', 'pools', 'pool_contacts'],
        pools: ['pools'],
      };
      return Object.fromEntries(Object.entries(exportTypes).filter(([key]) => (!this.kind || (groups[this.kind] || [this.kind]).includes(key))
        && (key !== 'pool_contacts' || Number(this.profile.userRole.id) === 1)));
    },
    dateSupported() { return ['customers', 'blocklist', 'campaigns', 'activity', 'bounces', 'bounce_customers'].includes(this.form.type); },
    organizationID() { return this.workspace.organizationId || 0; },
  },
  watch: {
    active(value) { if (!value) this.stopPolling(); },
    organizationID() { this.active = false; this.stopPolling(); this.jobs = []; },
    allowed(value) { if (!value) { this.active = false; this.stopPolling(); this.jobs = []; } },
  },
  beforeDestroy() { this.stopPolling(); },
  methods: {
    stopPolling() { window.clearTimeout(this.pollTimer); this.generation += 1; },
    jobStatus(job) {
      if (new Date(job.expiresAt) <= new Date()) return this.$t('export.statusExpired');
      return {
        pending: this.$t('export.statusPending'),
        running: this.$t('export.statusRunning'),
        complete: this.$t('export.statusComplete'),
        failed: this.$t('export.statusFailed'),
        expired: this.$t('export.statusExpired'),
      }[job.status];
    },
    downloadURL(job) { return `/api/exports/${job.id}/download?organization_id=${this.organizationID}`; },
    async refreshJobs(scrollToResults = false) {
      if (!this.active || !this.allowed) return;
      this.stopPolling();
      const { generation } = this;
      try {
        const jobs = await this.$api.getExports();
        if (generation !== this.generation || !this.active) return;
        this.jobs = jobs.filter((job) => this.types[job.request.type]).slice(0, 5);
        this.jobError = '';
        if (scrollToResults === true) {
          await this.$nextTick();
          if (generation !== this.generation || !this.active) return;
          if (this.$refs.exportResults) this.$refs.exportResults.scrollIntoView({ block: 'start' });
        }
        if (this.jobs.some((job) => ['pending', 'running'].includes(job.status))) {
          this.pollTimer = window.setTimeout(() => this.refreshJobs(), 3000);
        }
      } catch (error) {
        if (generation === this.generation) this.jobError = this.$t('export.jobError');
      }
    },
    async open() {
      this.stopPolling();
      const { generation } = this;
      const f = this.filters;
      const statusMatch = (f.queryExp || '').match(/^\s*(?:customers\.)?status\s*=\s*'([^']+)'\s*$/i);
      this.form = {
        type: this.kind || 'customers',
        format: 'xlsx',
        search: f.search || f.query || '',
        status: statusMatch ? statusMatch[1] : (f.status || ''),
        subscription_status: f.subStatus || '',
        source: f.source || '',
        bounce_type: f.type || '',
        query: statusMatch ? '' : (f.queryExp || ''),
      };
      const lists = f.list_ids || (f.customerListID ? [f.customerListID] : []);
      const campaigns = f.campaign_ids || ((f.campaign_id || f.campaignID) ? [f.campaign_id || f.campaignID] : []);
      const options = await this.$api.getExportOptions();
      if (generation !== this.generation || !this.allowed) return;
      this.options = options;
      this.selectedLists = lists.map((id) => options.lists.find((l) => l.id === Number(id)) || { id: Number(id), name: this.$t('export.listFallback', { id }) });
      this.selectedCampaigns = campaigns.map((id) => options.campaigns.find((l) => l.id === Number(id)) || { id: Number(id), name: this.$t('export.campaignFallback', { id }) });
      this.fromText = localText(f.from); this.toText = localText(f.to);
      this.selection = this.selected.length ? 'selected' : 'filtered'; this.active = true;
      this.refreshJobs(true);
    },
    async submit() {
      this.busy = true;
      const { organizationID } = this;
      try {
        const payload = {
          ...this.form,
          list_ids: this.selectedLists.map((l) => l.id),
          campaign_ids: this.selectedCampaigns.map((l) => l.id),
          ids: this.selection === 'selected' ? this.selected.map((row) => row.id) : [],
          from: this.dateSupported && this.fromText ? new Date(`${this.fromText}:00+08:00`).toISOString() : null,
          to: this.dateSupported && this.toText ? new Date(`${this.toText}:00+08:00`).toISOString() : null,
        };
        if (this.kind === 'lists' && this.form.type !== 'lists' && payload.ids.length) { payload.list_ids = payload.ids; payload.ids = []; }
        if (this.kind === 'campaigns' && this.form.type === 'activity' && payload.ids.length) { payload.campaign_ids = payload.ids; payload.ids = []; }
        if (this.kind === 'lists' && this.form.type !== 'lists') {
          if (!payload.list_ids.length) {
            payload.list_ids = this.options.lists.filter((l) => (!this.filters.status || l.status === this.filters.status)
              && (!this.filters.query || l.name.toLowerCase().includes(this.filters.query.toLowerCase()))).map((l) => l.id);
            if (!payload.list_ids.length) throw new Error(this.$t('export.noFilterResults'));
          }
          payload.status = ''; payload.search = '';
        }
        await this.$api.createExport(payload);
        if (organizationID !== this.organizationID || !this.active || !this.allowed) return;
        this.$utils.toast(this.$t('export.toastQueued'));
        await this.refreshJobs(true);
        this.$emit('created');
      } catch (error) { if (!error.response) this.$utils.toast(error.message, 'is-danger'); } finally { this.busy = false; }
    },
  },
};
</script>
