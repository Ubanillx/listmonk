<template>
  <section class="provider-automation" :aria-label="t('title')">
    <div v-if="error" role="alert" class="notification is-danger">
      {{ error }}
      <b-button size="is-small" :loading="loading" :disabled="anyBusy" @click="load">{{ $t('globals.buttons.retry') }}</b-button>
    </div>
    <p v-if="loading && !scopes.length" role="status">{{ t('loading') }}</p>
    <div class="automation-rules">
      <article v-for="state in scopes" :key="state.rule.scope" class="automation-rule" :data-cy="`automation-${state.rule.scope}`">
        <header class="automation-heading">
          <div class="automation-identity">
            <h2>{{ t(`${state.rule.scope}Customers`) }}</h2>
            <p class="automation-help">{{ t(`${state.rule.scope}Summary`) }}</p>
          </div>
          <div class="automation-heading-actions">
            <span v-if="dirty(state) && (state.draft.enabled || state.rule.config.enabled)" class="automation-pending" role="status">
              {{ t(state.draft.enabled === state.rule.config.enabled ? 'unsaved' : (state.draft.enabled ? 'pendingEnable' : 'pendingDisable')) }}
            </span>
            <b-switch v-model="state.draft.enabled" :disabled="!!state.busy" @input="state.manualOpen = false">{{ t('enable') }}</b-switch>
            <b-button v-if="!state.draft.enabled && !state.rule.config.enabled" type="is-text" size="is-small" class="automation-manual"
              :aria-expanded="String(state.manualOpen)" :aria-controls="`automation-config-${state.rule.scope}`" :disabled="!!state.busy"
              @click="state.manualOpen = !state.manualOpen">
              {{ t(state.manualOpen ? 'collapse' : 'manualSetup') }}
            </b-button>
          </div>
        </header>
        <div v-if="state.error" role="alert" class="notification is-danger">{{ state.error }}</div>
        <div v-if="state.historyError" role="alert" class="notification is-warning automation-history-error">
          {{ state.historyError }}
          <b-button size="is-small" :loading="state.historyLoading" :disabled="!!state.busy || state.historyLoading" @click="refreshHistory(state)">
            {{ $t('globals.buttons.retry') }}
          </b-button>
        </div>
        <p v-if="state.notice && (state.noticeKind === 'run' || !dirty(state))" role="status" class="notification is-success is-light">{{ state.notice }}</p>
        <div v-if="!state.draft.enabled && state.rule.config.enabled" class="automation-disable">
          <b-button :disabled="!!state.busy" @click="state.draft.enabled = true">{{ t('cancel') }}</b-button>
          <b-button type="is-primary" :loading="state.busy === 'save'" :disabled="!!state.busy" @click="save(state, true)">{{ t('saveDisable') }}</b-button>
        </div>
        <form v-if="state.draft.enabled || state.manualOpen" :id="`automation-config-${state.rule.scope}`" class="automation-config"
          @submit.prevent="state.draft.enabled ? save(state) : start(state)">
          <div class="automation-config-grid">
            <fieldset :disabled="!!state.busy">
              <legend>{{ t('types') }}</legend>
              <div class="automation-types">
                <b-checkbox v-for="type in types" :key="type" v-model="state.draft.types" :native-value="type">
                  {{ $t(`deliveryProvider.error${type}`) }}
                </b-checkbox>
              </div>
            </fieldset>
            <div class="automation-settings">
              <section v-if="state.draft.enabled" class="automation-schedule-options">
                <h3>{{ t('scheduleTitle') }}</h3>
                <div class="automation-fields">
                  <b-field :label="t('schedule')" :label-for="`automation-schedule-${state.rule.scope}`">
                    <b-select :id="`automation-schedule-${state.rule.scope}`" v-model="state.draft.schedule" expanded :disabled="!!state.busy">
                      <option value="daily">{{ t('daily') }}</option><option value="interval">{{ t('interval') }}</option>
                    </b-select>
                  </b-field>
                  <b-field v-if="state.draft.schedule === 'daily'" :label="t('dailyTime')" :label-for="`automation-time-${state.rule.scope}`">
                    <b-input :id="`automation-time-${state.rule.scope}`" v-model="state.draft.daily_time" type="time" required :disabled="!!state.busy" />
                  </b-field>
                  <b-field v-else :label="t('intervalHours')" :label-for="`automation-interval-${state.rule.scope}`">
                    <b-input :id="`automation-interval-${state.rule.scope}`" v-model.number="state.draft.interval_hours"
                      type="number" min="1" max="168" required :disabled="!!state.busy" />
                  </b-field>
                </div>
                <p v-if="state.rule.config.enabled && state.rule.next_run_at" class="automation-help automation-next">
                  {{ t('next') }}：{{ formatTime(state.rule.next_run_at) }}
                </p>
              </section>
              <b-field class="automation-lookback" :label="t('lookback')" :label-for="`automation-lookback-${state.rule.scope}`">
                <b-input :id="`automation-lookback-${state.rule.scope}`" v-model.number="state.draft.lookback_days"
                  type="number" min="1" max="31" required :disabled="!!state.busy" />
              </b-field>
            </div>
          </div>
          <details class="automation-exclusions" :open="!!state.rule.config.excluded_emails.length">
            <summary>{{ t('excluded') }}<span v-if="state.rule.config.excluded_emails.length">（{{ state.rule.config.excluded_emails.length }}）</span></summary>
            <b-field :label="t('excluded')" :label-for="`automation-exclusions-${state.rule.scope}`">
              <b-input :id="`automation-exclusions-${state.rule.scope}`" v-model="state.exclusionsText" type="textarea" rows="2"
                :aria-describedby="`automation-exclusions-help-${state.rule.scope}`" :disabled="!!state.busy" />
            </b-field>
            <p :id="`automation-exclusions-help-${state.rule.scope}`" class="automation-help automation-excluded-help">{{ t('excludedHelp') }}</p>
          </details>
          <p class="automation-help automation-impact">{{ t(`${state.rule.scope}Impact`) }}</p>
          <div v-if="validPreview(state)" class="automation-preview" role="status">
            <div class="automation-preview-heading">
              <h3>{{ t('checkResult') }}</h3>
              <span>{{ state.preview.run.date_start }} — {{ state.preview.run.date_end }}</span>
              <b-button type="is-text" size="is-small" @click="showDetails(state, state.preview.run)">{{ t('details') }}</b-button>
            </div>
            <p v-if="!state.preview.run.counts.to_block" class="automation-check-empty">{{ t('noTargets') }}</p>
            <dl>
              <div v-for="key in countKeys" :key="key"><dt>{{ t(key) }}</dt><dd>{{ state.preview.run.counts[key] || 0 }}</dd></div>
            </dl>
          </div>
          <footer class="automation-footer">
            <div class="automation-actions">
              <span v-if="state.busy === 'check'" class="automation-help" role="status">{{ t('checking') }}</span>
              <b-button v-if="state.draft.enabled" type="is-primary" native-type="submit" :loading="state.busy === 'save'" :disabled="!!state.busy">
                {{ t('saveSchedule') }}
              </b-button>
              <b-button :type="state.draft.enabled ? '' : 'is-primary'" :native-type="state.draft.enabled ? 'button' : 'submit'"
                :loading="state.busy === 'check' || state.busy === 'run'" :disabled="!!state.busy" @click="state.draft.enabled && start(state, $event)">
                {{ t('startBlock') }}
              </b-button>
            </div>
          </footer>
        </form>
      </article>
    </div>
    <section v-if="scopes.length" class="automation-records" :aria-label="t('records')">
      <header class="automation-records-heading">
        <h2>{{ t('records') }}</h2>
        <b-select v-model="historyScope" size="is-small" :aria-label="t('recordScope')">
          <option value="all">{{ t('allScopes') }}</option>
          <option v-for="state in scopes" :key="state.rule.scope" :value="state.rule.scope">{{ t(`${state.rule.scope}Customers`) }}</option>
        </b-select>
      </header>
      <b-table :data="historyRows" :mobile-cards="true" class="automation-history">
        <b-table-column v-slot="props" field="created_at" :label="t('recordTime')"><time>{{ formatTime(props.row.run.created_at) }}</time></b-table-column>
        <b-table-column v-slot="props" field="scope" :label="t('recordScope')">{{ t(`${props.row.state.rule.scope}Customers`) }}</b-table-column>
        <b-table-column v-slot="props" field="trigger" :label="t('recordTrigger')">{{ t(props.row.run.trigger) }}</b-table-column>
        <b-table-column v-slot="props" field="status" :label="t('recordResult')">
          <span v-if="props.row.run.error" class="has-text-danger">{{ props.row.run.error }}</span>
          <span v-else-if="props.row.run.status === 'preview' || props.row.run.status === 'success'">
            {{ t(props.row.run.status === 'preview' ? 'to_block' : 'blocked') }} {{ props.row.run.counts.to_block || 0 }}
            <small v-if="props.row.run.status === 'preview' && skipSummary(props.row.run)" class="automation-help automation-record-skips">
              {{ skipSummary(props.row.run) }}
            </small>
          </span>
          <span v-else>{{ t(props.row.run.status) }}</span>
        </b-table-column>
        <b-table-column v-slot="props" field="actions" :label="$t('deliveryProvider.actions')" width="110">
          <b-button type="is-text" size="is-small" @click="showDetails(props.row.state, props.row.run)">{{ t('details') }}</b-button>
        </b-table-column>
        <template #empty><p class="automation-help automation-empty">{{ t('noRuns') }}</p></template>
      </b-table>
    </section>
    <details class="automation-guide">
      <summary>{{ t('workflowTitle') }}</summary>
      <p>{{ t('workflow') }}</p>
    </details>
    <b-modal :active.sync="detail.open" has-modal-card trap-focus scroll="keep" :width="1120" class="automation-detail-modal"
      custom-content-class="automation-detail-container" v-bind="{ ariaRole: 'dialog' }" :aria-modal="true" :aria-label="t('detailTitle')"
      :can-cancel="['escape', 'outside']" @after-leave="restoreDetailFocus">
      <section class="modal-card automation-detail">
        <header class="modal-card-head automation-detail-heading">
          <div>
            <h2 class="modal-card-title">{{ t('detailTitle') }} · {{ t(`${detail.scope}Customers`) }}</h2>
            <p v-if="detail.run" class="automation-help automation-detail-context">
              {{ detail.run.date_start }} — {{ detail.run.date_end }} · {{ t(detail.run.status) }}
            </p>
          </div>
          <b-button icon-left="close" :aria-label="t('close')" @click="detail.open = false" />
        </header>
        <div ref="detailBody" class="modal-card-body" :aria-busy="String(detail.loading)">
          <div v-if="detail.error" role="alert" class="notification is-danger">
            {{ detail.error }}
            <b-button size="is-small" @click="fetchDetails">{{ $t('globals.buttons.retry') }}</b-button>
          </div>
          <p v-else-if="detail.loading" role="status" class="automation-detail-message">{{ t('loading') }}</p>
          <b-table v-else :data="detail.items" class="automation-detail-table" :mobile-cards="true" hoverable>
            <b-table-column v-slot="props" field="email" :label="t('emailCustomer')" width="410">
              <div class="automation-detail-customer">
                <span>{{ props.row.email }}</span>
                <small v-if="props.row.target_id" class="automation-help">{{ props.row.name || t('target') }} #{{ props.row.target_id }}</small>
              </div>
            </b-table-column>
            <b-table-column v-slot="props" field="types" :label="t('failureReason')" width="250">
              {{ props.row.evidence.types.map((type) => $t(`deliveryProvider.error${type}`)).join('、') }}
            </b-table-column>
            <b-table-column v-slot="props" field="count" :label="t('errorCount')" numeric>{{ props.row.evidence.count }}</b-table-column>
            <b-table-column v-slot="props" field="outcome" :label="t('outcome')" width="160">{{ t(props.row.outcome) }}</b-table-column>
            <template #empty><p class="automation-detail-message">{{ t('noItems') }}</p></template>
          </b-table>
        </div>
        <footer class="modal-card-foot automation-detail-footer">
          <div class="automation-detail-pagination">
            <span v-if="detailTotal" class="automation-help">{{ $t('deliveryAutomation.detailTotal', { count: detailTotal }) }}</span>
            <fieldset :disabled="detail.loading">
              <app-pagination :total="detailTotal" :current="detail.page" :per-page="detail.perPage"
                @change="detailPage" @per-page-change="detailPageSize" />
            </fieldset>
          </div>
          <b-button @click="detail.open = false">{{ t('close') }}</b-button>
        </footer>
      </section>
    </b-modal>
  </section>
</template>

<script>
import { parse } from '../time';

const message = (error) => error?.response?.data?.message || '请求失败，请稍后重试';
const clone = (value) => JSON.parse(JSON.stringify(value));

export default {
  data: () => ({
    loading: false,
    active: true,
    disposed: false,
    generation: 0,
    error: '',
    scopes: [],
    historyScope: 'all',
    types: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10],
    countKeys: ['addresses', 'to_block', 'already', 'excluded', 'unmatched', 'skipped'],
    detail: {
      open: false, scope: 'private', run: null, items: [], page: 1, perPage: 20, loading: false, error: '', sequence: 0,
    },
  }),
  computed: {
    anyBusy() { return this.loading || this.scopes.some((state) => state.busy); },
    detailTotal() {
      const counts = this.detail.run?.counts || {};
      return ['to_block', 'already', 'excluded', 'unmatched', 'skipped'].reduce((sum, key) => sum + (Number(counts[key]) || 0), 0);
    },
    historyRows() {
      return this.scopes.filter((state) => this.historyScope === 'all' || state.rule.scope === this.historyScope)
        .flatMap((state) => state.runs.map((run) => ({ state, run })))
        .sort((a, b) => Date.parse(b.run.created_at) - Date.parse(a.run.created_at) || b.run.id - a.run.id);
    },
  },
  created() { this.confirmationDialogs = new Map(); this.load(); },
  activated() {
    this.active = true;
    this.scopes.forEach((state) => this.refreshHistory(state));
  },
  deactivated() { this.leave(); },
  beforeDestroy() { this.disposed = true; this.leave(); },
  methods: {
    t(key) { return this.$t(`deliveryAutomation.${key}`); },
    formatTime(value) { return parse(value).format('YYYY-MM-DD HH:mm'); },
    config(state, manual = false) {
      // Manual conditions do not depend on an unfinished scheduling draft.
      const config = clone(manual ? state.rule.config : state.draft);
      return {
        ...config,
        types: [...state.draft.types],
        lookback_days: state.draft.lookback_days,
        excluded_emails: [...new Set(state.exclusionsText.split('\n').map((s) => s.trim().toLowerCase()).filter(Boolean))],
      };
    },
    signature(state, manual = false) { return JSON.stringify(this.config(state, manual)); },
    dirty(state) { return this.signature(state) !== JSON.stringify(state.rule.config); },
    validPreview(state) {
      return state.preview && state.previewSignature === this.signature(state, true) && Date.now() - state.previewAt < 15 * 60 * 1000;
    },
    current(generation) { return this.active && !this.disposed && generation === this.generation; },
    leave() {
      this.active = false; this.generation += 1;
      this.confirmationDialogs.forEach((dialog) => dialog.close());
      this.confirmationDialogs.clear();
      this.detail.open = false; this.detail.sequence += 1;
      this.scopes.forEach((state) => { if (state.busy === 'check' || state.busy === 'confirm') state.busy = ''; });
    },
    async load() {
      this.loading = true; this.error = '';
      try {
        const data = await this.$api.getProviderAutomation();
        if (this.disposed) return;
        this.scopes = data.scopes.map(({ rule, runs }) => ({
          rule,
          runs,
          draft: clone(rule.config),
          exclusionsText: rule.config.excluded_emails.join('\n'),
          busy: '',
          manualOpen: false,
          error: '',
          notice: '',
          noticeKind: '',
          historyError: '',
          historyLoading: false,
          historySequence: 0,
          preview: null,
          previewSignature: '',
          previewAt: 0,
        }));
      } catch (error) { this.error = message(error); } finally { this.loading = false; }
    },
    async start(state, event) {
      if (state.busy) return;
      if (event && !event.target.closest('form').reportValidity()) return;
      if (!state.draft.types.length) { state.error = this.t('selectTypes'); return; }
      state.busy = 'check'; state.error = ''; state.notice = ''; state.preview = null;
      const config = this.config(state, true); const { version } = state.rule; const { generation } = this;
      try {
        const result = await this.$api.previewProviderAutomation(state.rule.scope, { version, config });
        if (!this.current(generation)) return;
        state.preview = result; state.previewSignature = JSON.stringify(config); state.previewAt = Date.now();
        this.rememberRun(state, result.run);
        if (!result.run.counts.to_block) { state.busy = ''; return; }
        state.busy = 'confirm';
        const dialog = this.$utils.confirm(
          this.$t('deliveryAutomation.confirmCurrent', { count: result.run.counts.to_block, scope: this.t(`${state.rule.scope}Customers`) }),
          () => { this.confirmationDialogs.delete(state); this.run(state, { version, config, preview_id: result.run.id }, generation); },
          () => { this.confirmationDialogs.delete(state); if (this.current(generation)) state.busy = ''; },
          {
            title: this.t('confirmBlock'), confirmText: this.t('confirmBlock'), type: 'is-danger', focusOn: 'cancel',
          },
        );
        this.confirmationDialogs.set(state, dialog);
      } catch (error) {
        if (this.current(generation)) { await this.failure(state, error); if (this.current(generation)) state.busy = ''; }
      }
    },
    async save(state, disableOnly = false) {
      const config = disableOnly ? { ...clone(state.rule.config), enabled: false } : this.config(state);
      if (state.busy) return;
      if (!config.types.length) { state.error = this.t('selectTypes'); return; }
      state.busy = 'save'; state.error = ''; state.notice = '';
      try {
        // The server still verifies a recent check; users only save once.
        const check = config.enabled ? await this.$api.previewProviderAutomation(state.rule.scope, { version: state.rule.version, config }) : null;
        if (check) this.rememberRun(state, check.run);
        const rule = await this.$api.saveProviderAutomation(state.rule.scope, {
          version: state.rule.version, config, preview_id: check?.run.id || 0,
        });
        state.rule = rule; state.draft = clone(rule.config); state.exclusionsText = rule.config.excluded_emails.join('\n');
        state.preview = null; state.notice = this.t('saved'); state.noticeKind = 'save';
      } catch (error) { await this.failure(state, error); } finally { state.busy = ''; }
    },
    async run(state, request, generation) {
      if (!this.current(generation) || state.busy !== 'confirm') return;
      state.busy = 'run'; state.error = ''; state.notice = '';
      try {
        const result = await this.$api.runProviderAutomation(state.rule.scope, request);
        state.preview = null; state.notice = this.$t('deliveryAutomation.blockCompleted', { count: result.run.counts.to_block || 0 });
        state.noticeKind = 'run';
        this.rememberRun(state, result.run);
      } catch (error) { await this.failure(state, error); } finally { state.busy = ''; }
    },
    async failure(state, error) {
      if (this.disposed) return;
      state.error = message(error);
      await this.refreshHistory(state);
    },
    async refreshHistory(state) {
      state.historySequence += 1; const sequence = state.historySequence;
      state.historyLoading = true;
      try {
        const data = await this.$api.getProviderAutomation();
        if (this.disposed || sequence !== state.historySequence) return;
        const updated = data.scopes.find((entry) => entry.rule.scope === state.rule.scope);
        state.runs = updated.runs; state.historyError = '';
      } catch (error) {
        if (!this.disposed && sequence === state.historySequence) state.historyError = this.t('historyRefreshFailed');
      } finally {
        if (!this.disposed && sequence === state.historySequence) state.historyLoading = false;
      }
    },
    rememberRun(state, run) {
      state.historySequence += 1; state.historyLoading = false;
      state.runs = [run, ...state.runs.filter((record) => record.id !== run.id)].slice(0, 20);
    },
    skipSummary(run) {
      return ['already', 'excluded', 'unmatched', 'skipped'].filter((key) => run.counts[key]).map((key) => `${this.t(key)} ${run.counts[key]}`).join(' · ');
    },
    showDetails(state, run) {
      this.detailTrigger = document.activeElement;
      this.detail = {
        open: true, scope: state.rule.scope, run, items: [], page: 1, perPage: 20, loading: false, error: '', sequence: this.detail.sequence + 1,
      };
      this.fetchDetails();
    },
    restoreDetailFocus() { if (this.detailTrigger?.isConnected) this.detailTrigger.focus(); },
    detailPage(page) { this.detail.page = page; this.fetchDetails(); },
    detailPageSize(size) { this.detail.perPage = size; this.detail.page = 1; this.fetchDetails(); },
    async fetchDetails() {
      const { detail } = this; detail.sequence += 1; const { sequence } = detail;
      detail.loading = true; detail.error = ''; detail.items = [];
      try {
        // The API keeps fixed 50-row pages. Map the shared paginator onto those
        // pages without dropping rows when a UI page crosses an API boundary.
        const start = (detail.page - 1) * detail.perPage;
        const firstPage = Math.floor(start / 50) + 1;
        const lastPage = Math.max(firstPage, Math.ceil(Math.min(this.detailTotal, start + detail.perPage) / 50));
        const requests = Array.from({ length: lastPage - firstPage + 1 }, (_, index) => (
          this.$api.getProviderAutomationItems(detail.scope, detail.run.id, firstPage + index)));
        const pages = await Promise.all(requests);
        if (detail === this.detail && detail.sequence === sequence) {
          const offset = start % 50;
          detail.items = pages.flat().slice(offset, offset + detail.perPage);
          if (this.$refs.detailBody) this.$refs.detailBody.scrollTop = 0;
        }
      } catch (error) { if (detail === this.detail && detail.sequence === sequence) detail.error = message(error); } finally {
        if (detail === this.detail && detail.sequence === sequence) detail.loading = false;
      }
    },
  },
};
</script>

<style lang="scss" scoped>
.provider-automation { max-width: calc(var(--lm-field-max-width) * 2 + var(--lm-space-8)); }
.automation-heading, .automation-heading-actions, .automation-actions, .automation-footer,
.automation-records-heading, .automation-preview-heading, .automation-disable { display: flex; flex-wrap: wrap; align-items: center; gap: var(--lm-space-3); }
.automation-rules { border: 1px solid var(--lm-color-border); border-radius: var(--lm-radius-md); background: var(--lm-color-surface); }
.automation-rule { min-width: 0; padding: var(--lm-space-6); }
.automation-rule + .automation-rule { border-top: 1px solid var(--lm-color-border); }
.automation-heading { justify-content: space-between; gap: var(--lm-space-4); }
.automation-identity { min-width: 0; flex: 1; }
.automation-heading-actions { justify-content: flex-end; }
.automation-heading-actions .switch { margin: 0; }
.automation-manual, .automation-history .button { color: var(--lm-color-primary); font-size: var(--lm-font-size-toolbar); text-decoration: none; }
.automation-pending { color: var(--lm-color-text-muted); font-size: var(--lm-font-size-toolbar); }
h2 { font-size: 18px; font-weight: 600; }
h3, legend { font-size: 14px; font-weight: 600; margin-bottom: var(--lm-space-3); }
.automation-help { color: var(--lm-color-text-muted); font-size: var(--lm-font-size-toolbar); line-height: 1.7; }
.automation-identity .automation-help { margin: var(--lm-space-2) 0 0; }
.automation-config { margin-top: var(--lm-space-6); padding-top: var(--lm-space-6); border-top: 1px solid var(--lm-color-border); }
.automation-config-grid { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: var(--lm-space-8); }
fieldset { min-width: 0; }
.automation-types { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--lm-space-2) var(--lm-space-4); }
.automation-types .checkbox { margin: 0; min-height: var(--lm-control-height-sm); }
.automation-fields { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--lm-space-3); }
.automation-fields .field { min-width: 0; margin: 0; }
.automation-lookback { margin-top: var(--lm-space-6); max-width: calc((100% - var(--lm-space-3)) / 2); }
.automation-settings > .automation-lookback:first-child { margin-top: 0; }
.automation-next { margin-top: var(--lm-space-3); font-variant-numeric: tabular-nums; }
.automation-exclusions { margin-top: var(--lm-space-4); max-width: var(--lm-field-max-width); }
summary { cursor: pointer; width: fit-content; min-height: var(--lm-control-height-sm); padding: var(--lm-space-1) 0; color: var(--lm-color-primary); }
summary:focus-visible { outline: var(--lm-focus-ring); outline-offset: var(--lm-space-1); }
.automation-exclusions[open] > summary { margin-bottom: var(--lm-space-3); }
.automation-excluded-help { margin-top: var(--lm-space-2); }
.automation-impact { margin-top: var(--lm-space-4); max-width: calc(var(--lm-field-max-width) * 1.5); }
.automation-footer { justify-content: space-between; margin-top: var(--lm-space-6); gap: var(--lm-space-4); }
.automation-actions { margin-left: auto; }
.automation-actions .button[disabled] { color: var(--lm-color-text-muted); background: var(--lm-color-surface-subtle); border-color: var(--lm-color-border); opacity: 1; }
.automation-disable { margin-top: var(--lm-space-4); justify-content: flex-end; }
.automation-rule > .notification { margin: var(--lm-space-4) 0 0; }
.automation-preview { background: var(--lm-color-surface-subtle); border-radius: var(--lm-radius-md); padding: var(--lm-space-4); margin-top: var(--lm-space-6); }
.automation-preview-heading h3 { margin: 0; }
.automation-preview-heading > span { font-size: var(--lm-font-size-toolbar); color: var(--lm-color-text-muted); }
.automation-preview-heading .button { margin-left: auto; }
.automation-check-empty { margin-top: var(--lm-space-3); font-weight: 600; }
dl { display: grid; grid-template-columns: repeat(6, minmax(0, 1fr)); gap: var(--lm-space-4); margin-top: var(--lm-space-4); }
dt { color: var(--lm-color-text-muted); font-size: var(--lm-font-size-toolbar); }
dd { font-size: 20px; font-variant-numeric: tabular-nums; }
.automation-records { margin-top: var(--lm-space-8); }
.automation-records-heading { justify-content: space-between; margin-bottom: var(--lm-space-4); }
.automation-history { font-variant-numeric: tabular-nums; }
.automation-record-skips { display: block; }
.automation-empty { padding: var(--lm-space-6); text-align: center; }
.automation-guide { margin-top: var(--lm-space-4); font-size: var(--lm-font-size-toolbar); }
.automation-guide p { max-width: var(--lm-field-max-width); line-height: 1.7; color: var(--lm-color-text-muted); padding: var(--lm-space-2) 0; }
.modal-card.automation-detail {
  width: min(calc(var(--lm-field-max-width) * 2), calc(100vw - var(--lm-space-8)));
  max-width: 100%;
  max-height: min(800px, calc(100dvh - var(--lm-space-8) * 2));
  margin: 0 auto;
  border: 1px solid var(--lm-color-border);
  border-radius: var(--lm-radius-md);
  box-shadow: var(--lm-shadow-md);
}
.automation-detail .modal-card-head, .automation-detail .modal-card-body, .automation-detail .modal-card-foot { width: 100%; min-width: 0; }
.automation-detail .automation-detail-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--lm-space-4); }
.automation-detail-heading > div { min-width: 0; }
.automation-detail-heading .modal-card-title { white-space: normal; font-size: 18px; line-height: 1.5; }
.automation-detail-heading .button { flex-shrink: 0; }
.automation-detail-context { margin-top: var(--lm-space-2); }
.automation-detail .modal-card-body { min-height: 0; padding: 0; overscroll-behavior: contain; }
.automation-detail-table { overflow-wrap: anywhere; font-variant-numeric: tabular-nums; }
.automation-detail-customer { display: flex; flex-direction: column; gap: var(--lm-space-1); }
.automation-detail-message, .automation-detail .notification { margin: var(--lm-space-6); }
.automation-detail .automation-detail-footer { justify-content: space-between; flex-wrap: wrap; gap: var(--lm-space-4); }
.automation-detail-pagination { display: flex; flex: 1; align-items: center; flex-wrap: wrap; gap: var(--lm-space-3) var(--lm-space-6); min-width: 0; }
.automation-detail-pagination > .automation-help { white-space: nowrap; }
.automation-detail-pagination fieldset { margin-left: auto; }
@media screen and (max-width: 1100px) {
  .automation-heading { align-items: flex-start; }
  .automation-heading-actions { flex-direction: column; align-items: flex-end; }
  .automation-config-grid { grid-template-columns: 1fr; gap: var(--lm-space-6); }
  .automation-lookback { max-width: var(--lm-field-max-width); }
  dl { grid-template-columns: repeat(3, minmax(0, 1fr)); }
}
@media screen and (max-width: 480px) {
  .modal-card.automation-detail { max-height: calc(100dvh - var(--lm-space-8)); }
  .automation-detail-pagination { flex-basis: 100%; }
  .automation-detail .automation-detail-footer > .button { margin-left: auto; }
  .automation-detail-pagination fieldset { margin-left: 0; }
  .automation-rule { padding: var(--lm-space-4); }
  .automation-heading { flex-direction: column; gap: var(--lm-space-3); }
  .automation-heading-actions { width: 100%; flex-direction: row; justify-content: space-between; align-items: center; }
  .automation-pending { flex-basis: 100%; }
  .automation-types { gap: var(--lm-space-2); }
  .automation-types .checkbox { min-height: 44px; }
  .automation-fields { grid-template-columns: 1fr; }
  .automation-actions .button { white-space: normal; height: auto; min-height: 40px; }
  .automation-guide summary, .automation-exclusions summary { min-height: 44px; }
}
</style>
