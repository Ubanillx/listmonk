<template>
  <div class="modal-card allocation-workbench" data-cy="allocation-workbench">
    <header class="modal-card-head">
      <div>
        <h2>{{ $t('allocation.title') }}</h2>
        <p>
{{ $t('allocation.source') }}: <strong>{{ pool.name }}</strong>
          <span class="allocation-workbench__scope">{{ $t('allocation.scope') }}</span>
</p>
      </div>
      <b-button icon-left="close" :aria-label="$t('globals.buttons.close')" :disabled="busy" @click="requestClose" />
    </header>
    <section ref="body" class="modal-card-body">
      <p v-if="initialLoading" aria-live="polite">{{ $t('pool.loadingBindings') }}</p>
      <div v-else-if="initialError" role="alert">
        <p class="has-text-danger">{{ initialError }}</p>
        <b-button @click="initialize">{{ $t('globals.buttons.retry') }}</b-button>
      </div>
      <div v-else class="allocation-workbench__grid">
        <aside class="allocation-workbench__organizations">
          <h3>{{ $t('allocation.target') }}</h3>
          <b-input v-if="across" v-model="organizationSearch" type="search" icon="magnify"
            :placeholder="$t('pool.searchOrganizations')" :aria-label="$t('pool.searchOrganizations')" />
          <b-select class="allocation-workbench__mobile-picker" :value="organizationID" expanded
            :aria-label="$t('allocation.target')" :disabled="locked" @input="chooseOrganization">
            <option v-for="org in filteredOrganizations" :key="org.id" :value="Number(org.id)">{{ org.name }}</option>
          </b-select>
          <div class="allocation-workbench__organization-list" data-cy="pool-target-organization-panel">
            <button v-for="org in filteredOrganizations" :key="org.id" type="button"
              :class="{ 'is-selected': Number(org.id) === organizationID }" :disabled="locked"
              :aria-pressed="Number(org.id) === organizationID" :data-organization-id="org.id"
              data-cy="pool-organization-option" @click="chooseOrganization(Number(org.id))">
              <span><b-icon icon="office-building-outline" size="is-small" /> {{ org.name }}</span>
              <small>{{ bindingLabel(org.id) }}</small>
            </button>
            <p v-if="!filteredOrganizations.length" class="help">{{ $t('pool.noOrganizations') }}</p>
          </div>
        </aside>
        <section class="allocation-workbench__detail" aria-live="polite" data-cy="pool-allocation-panel">
          <div class="allocation-workbench__heading">
            <h3 data-cy="pool-current-organization">{{ organizationName }}</h3>
            <span v-if="detail" class="tag" :class="detail.state === 'active' ? 'is-success' : ''">{{ stateLabel(detail.state) }}</span>
          </div>
          <p v-if="targetLoading">{{ $t('pool.loadingBindings') }}</p>
          <div v-else-if="targetError" role="alert">
            <p class="has-text-danger">{{ targetError }}</p>
            <b-button @click="loadTarget">{{ $t('globals.buttons.retry') }}</b-button>
          </div>
          <template v-else-if="organizationID">
            <p v-if="detail" class="allocation-workbench__name">{{ detail.name }}</p>
            <div v-if="detail" class="tabs" role="tablist" :aria-label="$t('allocation.title')">
              <ul>
                <li v-for="item in tabs" :key="item" :class="{ 'is-active': tab === item }">
                  <button type="button" role="tab" :aria-selected="tab === item" :disabled="locked"
                    :data-cy="`allocation-tab-${item}`" @click="changeTab(item)">
{{ $t(`allocation.${item}`) }}
</button>
                </li>
              </ul>
            </div>
            <p class="allocation-workbench__notice">{{ $t('allocation.boundary') }}</p>
            <p v-if="error" class="has-text-danger" role="alert" data-cy="allocation-error">{{ error }}</p>
            <div v-if="!detail" data-cy="org-pool-allocation-create">
              <b-field :label="$t('pool.createNameLabel')">
                <b-input v-model.trim="createName" maxlength="200" data-cy="org-pool-allocation-name" :disabled="busy" />
              </b-field>
              <p class="help">{{ $t('allocation.createHint') }}</p>
              <b-button v-if="canManage" type="is-primary" :loading="busy"
                :disabled="busy || !createName || (!across && !deliveryAllowed)" data-cy="create-org-pool-allocation" @click="create">
                {{ $t('pool.createAndBind') }}
              </b-button>
            </div>
            <form v-else-if="pending" class="allocation-workbench__confirm" role="alertdialog"
              :aria-label="$t(`allocation.action.${pending.action}`)" @submit.prevent="confirm">
              <h4>{{ $t(`allocation.action.${pending.action}`) }} · {{ organizationName }}</h4>
              <p>{{ $t('allocation.confirmHint') }}</p>
              <p v-if="pending.ids">{{ $t('allocation.selected', { count: pending.ids.length }) }}</p>
              <p v-if="pending.action === 'disable' || pending.action === 'detach'">{{ $t('allocation.inFlight') }}</p>
              <p v-if="pending.impact !== undefined">{{ $t('allocation.impact', { count: pending.impact }) }}</p>
              <b-field v-if="pending.action === 'remove' || pending.action === 'detach'" :label="$t('allocation.reason')">
                <b-input v-model.trim="pending.reason" type="textarea" maxlength="1000" required data-cy="allocation-reason" />
              </b-field>
              <b-field v-if="pending.action === 'detach'" :label="$t('allocation.confirmOrganization')">
                <b-input v-model="pending.confirmOrganization" required data-cy="allocation-confirm-organization" />
              </b-field>
              <div class="allocation-workbench__buttons">
                <b-button ref="cancel" :disabled="busy" @click="cancelPending">{{ $t('globals.buttons.cancel') }}</b-button>
                <b-button native-type="submit" type="is-danger" :loading="busy" :disabled="busy || !canConfirm"
                  data-cy="allocation-confirm">
{{ $t(`allocation.action.${pending.action}`) }}
</b-button>
              </div>
            </form>
            <template v-else-if="tab === 'members'">
              <div v-if="$can('pools:get')">
                <form class="allocation-workbench__toolbar" @submit.prevent="searchRows">
                  <b-input v-model="search" type="search" icon="magnify" :placeholder="$t('allocation.search')"
                    :aria-label="$t('allocation.search')" data-cy="allocation-search" />
                  <b-select v-model="memberStatus" :aria-label="$t('allocation.memberStatus')" data-cy="allocation-member-status" @input="searchRows">
                    <option value="active">{{ $t('allocation.activeMembers') }}</option>
                    <option value="removed">{{ $t('allocation.removedMembers') }}</option>
                    <option value="candidates">{{ $t('allocation.candidates') }}</option>
                  </b-select>
                  <b-button native-type="submit" type="is-primary" icon-left="magnify" :aria-label="$t('globals.buttons.search')" />
                </form>
                <div data-cy="allocation-members">
                <b-table :data="rows" :loading="rowsLoading" :checked-rows.sync="selected" :checkable="canWriteMembers"
                  :mobile-cards="true">
                  <b-table-column v-slot="props" field="name" :label="$t('allocation.contact')">
                    <div class="allocation-workbench__contact">
                    <strong>{{ props.row.customerCode }} · {{ props.row.name }}</strong><br /><small>{{ props.row.email }}</small>
                    <span v-if="props.row.status !== 'active'" class="tag">
                      {{ $t(props.row.status === 'blocklisted' ? 'customers.status.blocklisted' : 'pool.statusArchived') }}
                    </span>
                    </div>
                  </b-table-column>
                  <b-table-column v-slot="props" :label="$t('allocation.reason')">{{ props.row.exclusionReason || '-' }}</b-table-column>
                  <b-table-column v-slot="props" :label="$t('allocation.operation')">
                    <b-button v-if="canWriteMembers" type="is-text" :disabled="busy" @click="prepare(memberAction, [props.row.id])">
                      {{ $t(`allocation.action.${memberAction}`) }}
                    </b-button>
                  </b-table-column>
                  <template #empty><p>{{ $t('allocation.empty') }}</p></template>
                </b-table>
                </div>
                <app-pagination :total="total" :current="page" :per-page="perPage" @change="changePage" @per-page-change="changeSize" />
                <div class="allocation-workbench__buttons">
                  <b-button v-if="selected.length && canWriteMembers" type="is-primary" :disabled="busy" data-cy="allocation-bulk"
                    @click="prepare(memberAction, selected.map(row => row.id))">
                    {{ $t(`allocation.action.${memberAction}`) }} ({{ selected.length }})
                  </b-button>
                </div>
                <p class="help">{{ $t('allocation.restoreHint') }}</p>
              </div>
              <p v-else>{{ $t('allocation.noRead') }}</p>
            </template>
            <form v-else-if="tab === 'info'" @submit.prevent="save">
              <b-field :label="$t('pool.allocationNameLabel')">
                <b-input v-model.trim="draft.name" maxlength="200" required :disabled="busy || detail.state === 'detached'" data-cy="allocation-name" />
              </b-field>
              <b-field :label="$t('allocation.description')">
                <b-input v-model="draft.description" type="textarea" maxlength="1000" :disabled="busy || detail.state === 'detached'" />
              </b-field>
              <div v-if="canManage" class="allocation-workbench__buttons">
                <b-button native-type="submit" type="is-primary" :loading="busy" :disabled="busy || !dirty || detail.state === 'detached'"
                  data-cy="allocation-save">
{{ $t('globals.buttons.save') }}
</b-button>
                <b-button v-if="detail.state !== 'detached'" :disabled="busy" data-cy="allocation-toggle-state"
                  @click="prepare(detail.state === 'active' ? 'disable' : 'enable')">
                  {{ $t(`allocation.action.${detail.state === 'active' ? 'disable' : 'enable'}`) }}
                </b-button>
                <b-button v-if="across" type="is-danger" outlined :disabled="busy" data-cy="allocation-detach"
                  @click="prepare(detail.state === 'detached' ? 'rebind' : 'detach')">
                  {{ $t(`allocation.action.${detail.state === 'detached' ? 'rebind' : 'detach'}`) }}
                </b-button>
              </div>
            </form>
            <div v-else-if="tab === 'history'">
              <b-table :data="rows" :loading="rowsLoading" :mobile-cards="true">
                <b-table-column v-slot="props" :label="$t('allocation.time')">{{ $utils.niceDate(props.row.occurredAt) }}</b-table-column>
                <b-table-column v-slot="props" :label="$t('allocation.actor')">{{ props.row.actor || '-' }}</b-table-column>
                <b-table-column v-slot="props" :label="$t('allocation.operation')">{{ eventLabel(props.row.action) }}</b-table-column>
                <b-table-column v-slot="props" :label="$t('allocation.reason')">{{ props.row.metadata.reason || '-' }}</b-table-column>
                <template #empty><p>{{ $t('allocation.empty') }}</p></template>
              </b-table>
              <app-pagination :total="total" :current="page" :per-page="perPage" @change="changePage" @per-page-change="changeSize" />
            </div>
            <div class="allocation-workbench__delivery" data-cy="pool-delivery-grant">
              <strong>{{ $t('allocation.delivery') }}:</strong>
              <span class="tag" :class="deliveryAllowed ? 'is-success' : ''">{{ $t(deliveryAllowed ? 'allocation.granted' : 'allocation.notGranted') }}</span>
              <b-button v-if="$can('pools:delivery_manage')" :disabled="locked || !detail" type="is-text" @click="prepare('delivery')">
                {{ $t(deliveryAllowed ? 'pool.revokeDelivery' : 'pool.grantDelivery') }}
              </b-button>
              <p class="help">{{ $t($can('pools:delivery_manage') ? 'allocation.deliveryHint' : 'allocation.noDelivery') }}</p>
            </div>
          </template>
        </section>
      </div>
    </section>
    <footer class="modal-card-foot">
      <b-button v-if="canWriteMembers && $can('pools:get') && tab === 'members' && memberStatus !== 'candidates'"
        class="allocation-workbench__add" :disabled="locked" data-cy="allocation-add" @click="memberStatus = 'candidates'; searchRows()">
        {{ $t('allocation.candidates') }}
      </b-button>
      <b-button :disabled="busy" data-cy="allocation-close" @click="requestClose">{{ $t('globals.buttons.close') }}</b-button>
    </footer>
  </div>
</template>

<script>
import { mapState } from 'vuex';
import AppPagination from './AppPagination.vue';

export default {
  name: 'PoolAllocationWorkbench',
  components: { AppPagination },
  props: { pool: { type: Object, required: true }, initialOrganizationId: { type: Number, default: 0 } },
  data() {
    return {
      organizations: [],
      allocations: [],
      organizationID: 0,
      organizationSearch: '',
      detail: null,
      deliveryAllowed: false,
      initialLoading: true,
      initialError: '',
      targetLoading: false,
      targetError: '',
      error: '',
      busy: false,
      tab: 'members',
      memberStatus: 'active',
      search: '',
      rows: [],
      total: 0,
      page: 1,
      perPage: 20,
      selected: [],
      rowsLoading: false,
      drafts: {},
      names: {},
      pending: null,
      targetSerial: 0,
      rowsSerial: 0,
      tabViews: {},
    };
  },
  computed: {
    ...mapState(['workspace']),
    across() { return this.$can('pools:allocation_platform_manage'); },
    canManage() { return this.across || (this.$can('pools:manage') && Number(this.workspace.organizationId) === this.organizationID); },
    canWriteMembers() { return this.canManage && this.detail && this.detail.state === 'active'; },
    locked() { return this.busy || !!this.pending; },
    tabs() { return ['members', 'info', ...(this.$can('audit:get') ? ['history'] : [])]; },
    filteredOrganizations() { return this.organizations.filter((org) => org.name.toLowerCase().includes(this.organizationSearch.toLowerCase().trim())); },
    organizationName() { const org = this.organizations.find((item) => Number(item.id) === this.organizationID); return org ? org.name : ''; },
    draft() { return this.drafts[this.organizationID] || { name: '', description: '' }; },
    dirty() { return this.detail && (this.draft.name !== this.detail.name || this.draft.description !== this.detail.description); },
    createName: {
      get() { return this.names[this.organizationID] === undefined ? this.$t('pool.defaultAllocationName', { organization: this.organizationName }) : this.names[this.organizationID]; },
      set(value) { this.$set(this.names, this.organizationID, value); },
    },
    memberAction() { return ({ active: 'remove', removed: 'restore', candidates: 'assign' })[this.memberStatus]; },
    canConfirm() {
      if (!this.pending) return false;
      if (['disable', 'detach'].includes(this.pending.action) && this.pending.impact === undefined) return false;
      if (['remove', 'detach'].includes(this.pending.action) && !this.pending.reason) return false;
      return this.pending.action !== 'detach' || (this.pending.confirmOrganization === this.organizationName && this.pending.impact === 0);
    },
  },
  methods: {
    stateLabel(state) { return this.$t(`allocation.state.${state}`); },
    eventLabel(action) { const key = `allocation.event.${action.split('.').pop()}`; return this.$te(key) ? this.$t(key) : action; },
    message(err) { return (err && err.response && err.response.data && err.response.data.message) || this.$t('allocation.failed'); },
    bindingLabel(id) {
      const item = this.allocations.find((row) => Number(row.organizationId) === Number(id));
      return item ? this.$t(item.state === 'detached' ? 'allocation.state.detached' : 'pool.bound') : this.$t('pool.unbound');
    },
    async initialize() {
      this.initialLoading = true; this.initialError = '';
      try {
        const [allocations, orgs] = await Promise.all([
          this.$api.getOrgPoolAllocations(this.pool.id),
          this.across ? this.$api.getPoolOrganizations({ purpose: 'allocations' })
            : Promise.resolve(this.workspace.organizationId > 0 ? [{ id: this.workspace.organizationId, name: this.workspace.organizationName }] : []),
        ]);
        this.allocations = allocations || []; this.organizations = orgs || [];
        const wanted = this.initialOrganizationId || Number(this.$route.query.manage_org) || Number(this.workspace.organizationId);
        const initial = this.organizations.find((org) => Number(org.id) === wanted) || this.organizations[0];
        this.organizationID = initial ? Number(initial.id) : 0;
        await this.loadTarget();
      } catch (err) { this.initialError = this.message(err); } finally { this.initialLoading = false; }
    },
    chooseOrganization(id) {
      if (this.locked || Number(id) === this.organizationID) return;
      const change = () => { this.organizationID = Number(id); this.loadTarget(); };
      if (this.dirty) this.discard(change); else change();
    },
    discard(next) {
      this.$buefy.dialog.confirm({
        message: this.$t('allocation.unsaved'),
        focusOn: 'cancel',
        confirmText: this.$t('allocation.discard'),
        cancelText: this.$t('globals.buttons.cancel'),
        onConfirm: () => { this.$delete(this.drafts, this.organizationID); next(); },
      });
    },
    requestClose() {
      if (this.busy) return;
      if (this.dirty || this.pending) this.discard(() => this.$emit('close')); else this.$emit('close');
    },
    syncURL() {
      this.$router.replace({
        query: {
          ...this.$route.query, manage_pool: this.pool.id, manage_org: this.organizationID, manage_tab: this.tab,
        },
      }).catch(() => {});
    },
    async loadTarget() {
      this.targetSerial += 1;
      const serial = this.targetSerial;
      this.rowsSerial += 1; this.detail = null; this.rows = []; this.selected = []; this.total = 0; this.page = 1;
      this.tabViews = {}; this.search = ''; this.memberStatus = 'active';
      this.pending = null; this.targetLoading = true; this.targetError = ''; this.error = '';
      if (!this.organizationID) { this.targetLoading = false; return; }
      try {
        const target = await this.$api.getPoolManagementTarget(this.pool.id, this.organizationID);
        const detail = target.allocation ? await this.$api.getAllocationManagement(target.allocation.id) : null;
        if (serial !== this.targetSerial) return;
        this.detail = detail; this.deliveryAllowed = !!target.deliveryAllowed;
        if (detail && !this.drafts[this.organizationID]) this.$set(this.drafts, this.organizationID, { name: detail.name, description: detail.description });
        const requested = this.$route.query.manage_tab;
        this.tab = this.$can('pools:get') ? 'members' : 'info';
        if (this.tabs.includes(requested)) this.tab = requested;
        this.syncURL(); await this.loadRows();
      } catch (err) { if (serial === this.targetSerial) this.targetError = this.message(err); } finally { if (serial === this.targetSerial) this.targetLoading = false; }
    },
    async loadRows() {
      this.rowsSerial += 1;
      const serial = this.rowsSerial;
      this.selected = []; this.error = '';
      if (!this.detail || this.tab === 'info' || (this.tab === 'members' && !this.$can('pools:get'))) { this.rowsLoading = false; return; }
      this.rowsLoading = true;
      try {
        const params = {
          page: this.page, per_page: this.perPage, search: this.search, status: this.memberStatus,
        };
        const result = this.tab === 'history' ? await this.$api.getAllocationEvents(this.detail.id, params) : await this.$api.getAllocationMembers(this.detail.id, params);
        if (serial !== this.rowsSerial) return;
        this.rows = result.results || []; this.total = result.total;
        const last = Math.max(1, Math.ceil(this.total / this.perPage));
        if (this.page > last) { this.page = last; await this.loadRows(); }
      } catch (err) { if (serial === this.rowsSerial) { this.rows = []; this.error = this.message(err); } } finally { if (serial === this.rowsSerial) this.rowsLoading = false; }
    },
    changeTab(tab) {
      if (this.locked || tab === this.tab || !this.tabs.includes(tab)) return;
      this.$set(this.tabViews, this.tab, {
        rows: this.rows,
        total: this.total,
        page: this.page,
        perPage: this.perPage,
        selected: this.selected,
        search: this.search,
        memberStatus: this.memberStatus,
        loaded: !this.rowsLoading && !this.error,
        error: this.error,
        scroll: this.$refs.body.scrollTop,
      });
      this.rowsSerial += 1;
      this.tab = tab;
      const view = this.tabViews[tab];
      this.rows = view ? view.rows : []; this.total = view ? view.total : 0;
      this.page = view ? view.page : 1; this.perPage = view ? view.perPage : 20;
      this.selected = view ? view.selected : []; this.search = view ? view.search : '';
      this.memberStatus = view ? view.memberStatus : 'active'; this.error = ''; this.rowsLoading = false;
      this.syncURL();
      if (!view || !view.loaded) this.loadRows();
      this.$nextTick(() => { this.$refs.body.scrollTop = view ? view.scroll : 0; });
    },
    searchRows() { this.page = 1; this.loadRows(); },
    changePage(page) { this.page = page; this.loadRows(); },
    changeSize(size) { this.perPage = size; this.searchRows(); },
    async create() {
      if (this.busy || !this.canManage || !this.createName) return;
      this.busy = true; this.error = '';
      try {
        await this.$api.createOrgPoolAllocation({ pool_id: this.pool.id, organization_id: this.organizationID, name: this.createName });
        await this.refresh();
      } catch (err) { this.error = this.message(err); } finally { this.busy = false; }
    },
    async refresh(resetDraft = false) {
      Object.keys(this.tabViews).forEach((key) => {
        this.$set(this.tabViews, key, { ...this.tabViews[key], loaded: false, selected: [] });
      });
      this.$emit('changed');
      this.allocations = await this.$api.getOrgPoolAllocations(this.pool.id);
      const target = await this.$api.getPoolManagementTarget(this.pool.id, this.organizationID);
      this.deliveryAllowed = !!target.deliveryAllowed;
      this.detail = target.allocation ? await this.$api.getAllocationManagement(target.allocation.id) : null;
      if (this.detail && resetDraft) this.$set(this.drafts, this.organizationID, { name: this.detail.name, description: this.detail.description });
      if (this.detail && !this.drafts[this.organizationID]) this.$set(this.drafts, this.organizationID, { name: this.detail.name, description: this.detail.description });
      await this.loadRows();
    },
    async save() {
      if (this.busy || !this.dirty || !this.detail) return;
      this.busy = true; this.error = '';
      try {
        await this.$api.changeAllocationManagement(this.detail.id, {
          action: 'update',
          name: this.draft.name,
          description: this.draft.description,
          expected_version: this.detail.version,
        });
        await this.refresh(true); this.$utils.toast(this.$t('allocation.saved'));
      } catch (err) { this.error = this.message(err); } finally { this.busy = false; }
    },
    async prepare(action, ids) {
      if (this.locked || !this.detail) return;
      this.error = ''; this.pending = {
        action, ids, reason: '', confirmOrganization: '', version: this.detail.version,
      };
      this.$nextTick(() => { if (this.$refs.cancel) this.$refs.cancel.$el.focus(); });
      if (['disable', 'detach'].includes(action)) {
        const { pending } = this;
        try {
          const impact = await this.$api.getAllocationImpact(this.detail.id);
          if (this.pending === pending) this.$set(pending, 'impact', impact.unfinishedCampaigns);
        } catch (err) { if (this.pending === pending) this.error = this.message(err); }
      }
    },
    cancelPending() { if (!this.busy) { this.pending = null; this.error = ''; } },
    async confirm() {
      if (this.busy || !this.canConfirm) return;
      this.busy = true; this.error = '';
      try {
        const item = this.pending;
        if (item.ids) await this.$api.changeAllocationMembers(this.detail.id, { action: item.action, reason: item.reason, contact_ids: item.ids });
        else if (item.action === 'delivery') {
          const method = this.deliveryAllowed ? this.$api.revokePoolOrganization : this.$api.grantPoolOrganization;
          await method({ pool_id: this.pool.id, organization_id: this.organizationID });
        } else {
          await this.$api.changeAllocationManagement(this.detail.id, {
            action: item.action,
            reason: item.reason,
            confirm_organization: item.confirmOrganization,
            expected_version: item.version,
          });
        }
        this.pending = null; await this.refresh(); this.$utils.toast(this.$t('allocation.saved'));
      } catch (err) { this.error = this.message(err); } finally { this.busy = false; }
    },
  },
  mounted() { this.initialize(); },
  beforeDestroy() { this.targetSerial += 1; this.rowsSerial += 1; },
};
</script>

<style lang="scss" scoped>
.allocation-workbench {
  width: 100%; max-width: 1180px; height: 90vh; max-height: 90vh; color: var(--lm-color-text);
  border-radius: var(--lm-radius-md); box-shadow: var(--lm-shadow-md);
  .modal-card-head, .modal-card-foot { background: var(--lm-color-surface); padding: var(--lm-space-6); }
  .modal-card-head { display: flex; align-items: flex-start; justify-content: space-between; }
  .modal-card-foot { justify-content: flex-end; }
  .modal-card-foot .allocation-workbench__add { margin-right: auto; }
  &__contact { min-width: 0; overflow-wrap: anywhere; }
  .modal-card-body { padding: 0 var(--lm-space-6) var(--lm-space-6); min-height: 0; }
  h2 { font-size: 1.25rem; font-weight: 700; margin-bottom: var(--lm-space-3); }
  h3 { font-size: 1rem; font-weight: 600; margin: 0 0 var(--lm-space-4); }
  &__scope { display: inline-block; margin-left: var(--lm-space-4); font-size: 0.8rem; color: var(--lm-color-text-muted); }
  &__grid { display: grid; grid-template-columns: 240px minmax(0, 1fr); border: 1px solid var(--lm-color-border); border-radius: var(--lm-radius-md); }
  &__organizations { padding: var(--lm-space-4); border-right: 1px solid var(--lm-color-border); min-width: 0; }
  &__mobile-picker { display: none; }
  &__organization-list { max-height: 34rem; overflow-y: auto; margin-top: var(--lm-space-4); }
  &__organization-list button { display: flex; align-items: center; justify-content: space-between; gap: var(--lm-space-2);
    width: 100%; min-height: 48px; padding: var(--lm-space-2); background: transparent; border: 0; text-align: left;
    border-radius: var(--lm-radius-md); color: inherit; cursor: pointer;
    span { min-width: 0; overflow-wrap: anywhere; } small { flex-shrink: 0; color: var(--lm-color-text-muted); }
    &.is-selected { color: var(--lm-color-primary); background: var(--lm-color-primary-soft); }
    &:focus-visible { outline: 2px solid var(--lm-color-primary); outline-offset: -2px; }
    &:disabled { cursor: wait; }
  }
  &__detail { min-width: 0; padding: var(--lm-space-6); }
  &__heading { display: flex; justify-content: space-between; gap: var(--lm-space-4); h3 { overflow-wrap: anywhere; } }
  &__name { color: var(--lm-color-text-muted); overflow-wrap: anywhere; margin-bottom: var(--lm-space-4); }
  .tabs button { padding: var(--lm-space-3); background: none; color: var(--lm-color-text-muted); border: 0; border-bottom: 2px solid transparent; cursor: pointer; }
  .tabs .is-active button { color: var(--lm-color-primary); border-bottom-color: var(--lm-color-primary); font-weight: 600; }
  &__notice { border-left: 3px solid var(--lm-color-primary); padding: var(--lm-space-2) var(--lm-space-3); margin-bottom: var(--lm-space-4); }
  &__toolbar { display: flex; flex-wrap: wrap; gap: var(--lm-space-2); margin-bottom: var(--lm-space-4);
    > .control { flex: none; min-width: 0; } > .control:first-child { flex: 1; } }
  &__buttons { display: flex; flex-wrap: wrap; gap: var(--lm-space-3); margin: var(--lm-space-4) 0; }
  &__delivery { margin-top: var(--lm-space-6); padding: var(--lm-space-4); background: var(--lm-color-surface-subtle); border-radius: var(--lm-radius-md);
    > .tag { margin-left: var(--lm-space-2); } }
  &__confirm { h4 { font-weight: 600; margin-bottom: var(--lm-space-4); } p { margin-bottom: var(--lm-space-3); } }
  small { color: var(--lm-color-text-muted); overflow-wrap: anywhere; }
  ::v-deep .table td { overflow-wrap: anywhere; }
  ::v-deep .table .button.is-text { color: var(--lm-color-primary); text-decoration: none; }
  ::v-deep .app-pagination { flex-wrap: wrap; }
}
@media screen and (max-width: 768px) {
  .allocation-workbench { max-height: calc(100vh - 32px);
    .modal-card-head, .modal-card-foot { padding: var(--lm-space-4); }
    .modal-card-body { padding: 0 var(--lm-space-4) var(--lm-space-4); }
    &__grid { grid-template-columns: minmax(0, 1fr); }
    &__scope { display: block; margin: var(--lm-space-2) 0 0; }
    &__organizations { border-right: 0; border-bottom: 1px solid var(--lm-color-border); }
    &__organization-list { display: none; }
    &__mobile-picker { display: block; margin-top: var(--lm-space-3); }
    &__detail { padding: var(--lm-space-4); }
    &__toolbar { > .control:first-child { flex-basis: 100%; } }
  }
}
</style>
