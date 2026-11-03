<template>
  <section class="provider-automation" :aria-label="t('title')">
    <div class="automation-intro">
      <p>{{ t('intro') }} <span v-if="timezone">{{ t('timezone') }}：{{ timezone }}</span></p>
      <b-button :loading="loading" :disabled="anyBusy" icon-left="refresh" @click="load">{{ t('reload') }}</b-button>
    </div>
    <div v-if="error" role="alert" class="notification is-danger">{{ error }}</div>
    <p v-if="loading && !scopes.length" role="status">{{ t('loading') }}</p>
    <div class="automation-rules">
      <article v-for="state in scopes" :key="state.rule.scope" class="automation-rule" :data-cy="`automation-${state.rule.scope}`">
        <header class="automation-heading">
          <h2>{{ t(state.rule.scope) }}</h2>
          <b-tag :type="state.rule.config.enabled ? 'is-success is-light' : 'is-light'">{{ t(state.rule.config.enabled ? 'enabled' : 'disabled') }}</b-tag>
        </header>
        <p class="automation-help">{{ t(`${state.rule.scope}Scope`) }}</p>
        <form @submit.prevent="save(state)">
          <b-field><b-switch v-model="state.draft.enabled" :disabled="!!state.busy">{{ t('enable') }}</b-switch></b-field>
          <fieldset :disabled="!!state.busy">
            <legend>{{ t('types') }}</legend>
            <div class="automation-types">
              <b-checkbox v-for="type in types" :key="type" v-model="state.draft.types" :native-value="type">
                {{ $t(`deliveryProvider.error${type}`) }}
              </b-checkbox>
            </div>
          </fieldset>
          <div class="automation-fields">
            <b-field :label="t('schedule')">
              <b-select v-model="state.draft.schedule" expanded :disabled="!!state.busy">
                <option value="daily">{{ t('daily') }}</option><option value="interval">{{ t('interval') }}</option>
              </b-select>
            </b-field>
            <b-field v-if="state.draft.schedule === 'daily'" :label="t('dailyTime')">
              <b-input v-model="state.draft.daily_time" type="time" required :disabled="!!state.busy" />
            </b-field>
            <b-field v-else :label="t('intervalHours')">
              <b-input v-model.number="state.draft.interval_hours" type="number" min="1" max="168" required :disabled="!!state.busy" />
            </b-field>
            <b-field :label="t('lookback')">
              <b-input v-model.number="state.draft.lookback_days" type="number" min="1" max="31" required :disabled="!!state.busy" />
            </b-field>
          </div>
          <b-field :label="t('excluded')" :message="t('excludedHelp')">
            <b-input v-model="state.exclusionsText" type="textarea" rows="2" :disabled="!!state.busy" />
          </b-field>
          <div v-if="state.error" role="alert" class="notification is-danger">{{ state.error }}</div>
          <p v-if="state.notice && !dirty(state)" role="status" class="notification is-success is-light">{{ state.notice }}</p>
          <div v-if="validPreview(state)" class="automation-preview" role="status">
            <h3>{{ t('previewResult') }}</h3>
            <p>{{ state.preview.run.date_start }} — {{ state.preview.run.date_end }}</p>
            <dl>
              <div v-for="key in countKeys" :key="key"><dt>{{ t(key) }}</dt><dd>{{ state.preview.run.counts[key] || 0 }}</dd></div>
            </dl>
            <b-button size="is-small" @click="showDetails(state, state.preview.run)">{{ t('details') }}</b-button>
          </div>
          <p v-else-if="state.preview" class="automation-help">{{ t('previewStale') }}</p>
          <div class="automation-actions">
            <b-button icon-left="eye-outline" :loading="state.busy === 'preview'" :disabled="!!state.busy" @click="preview(state)">{{ t('preview') }}</b-button>
            <b-button native-type="submit" type="is-primary" :loading="state.busy === 'save'"
              :disabled="!!state.busy || (state.draft.enabled && !validPreview(state))">
              {{ t('save') }}
            </b-button>
            <b-button type="is-danger is-light" :loading="state.busy === 'run'"
              :disabled="!!state.busy || dirty(state) || !validPreview(state) || !state.preview.run.counts.to_block" @click="confirmRun(state)">
              {{ t('run') }}
            </b-button>
          </div>
          <p class="automation-help">{{ t('workflow') }}</p>
        </form>
        <div class="automation-next">
          <span>{{ t('next') }}：{{ state.rule.next_run_at ? formatTime(state.rule.next_run_at) : t('disabled') }}</span>
          <span v-if="dirty(state)">{{ t('unsaved') }}</span>
        </div>
        <h3>{{ t('history') }}</h3>
        <p v-if="!state.runs.length" class="automation-help">{{ t('noRuns') }}</p>
        <ol v-else class="automation-history">
          <li v-for="entry in state.runs" :key="entry.id">
            <div><time>{{ formatTime(entry.created_at) }}</time><span>{{ t(entry.trigger) }} · {{ t(entry.status) }}</span></div>
            <p v-if="entry.error" class="has-text-danger">{{ entry.error }}</p>
            <p v-else-if="entry.status === 'success' || entry.status === 'preview'">{{ t(entry.status === 'preview' ? 'to_block' : 'blocked') }} {{ entry.counts.to_block || 0 }}</p>
            <b-button size="is-small" @click="showDetails(state, entry)">{{ t('details') }}</b-button>
          </li>
        </ol>
      </article>
    </div>
    <b-modal :active.sync="detail.open" has-modal-card trap-focus :aria-label="t('details')" :can-cancel="['escape', 'outside']">
      <section class="modal-card automation-detail">
        <header class="modal-card-head"><h2 class="modal-card-title">{{ t('details') }} · {{ t(detail.scope) }}</h2></header>
        <div class="modal-card-body">
          <p v-if="detail.run">{{ detail.run.date_start }} — {{ detail.run.date_end }} · {{ t(detail.run.status) }}</p>
          <p v-if="detail.error" role="alert" class="notification is-danger">{{ detail.error }}</p>
          <p v-if="detail.loading" role="status">{{ t('loading') }}</p>
          <div class="automation-table">
            <table class="table is-fullwidth">
              <thead><tr><th>{{ t('email') }}</th><th>{{ t('target') }}</th><th>{{ t('types') }}</th><th>{{ t('outcome') }}</th></tr></thead>
              <tbody>
                <tr v-for="(item, index) in detail.items" :key="index">
                <td>{{ item.email }}</td><td>{{ item.name }}<small v-if="item.target_id"> #{{ item.target_id }}</small></td>
                <td>{{ item.evidence.types.map((type) => $t(`deliveryProvider.error${type}`)).join('、') }} ({{ item.evidence.count }})</td>
                <td>{{ t(item.outcome) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <p v-if="!detail.loading && !detail.items.length && !detail.error">{{ t('noItems') }}</p>
        </div>
        <footer class="modal-card-foot automation-actions">
          <b-button :disabled="detail.loading || detail.page <= 1" @click="detailPage(-1)">{{ t('previous') }}</b-button>
          <span>{{ detail.page }}</span>
          <b-button :disabled="detail.loading || detail.items.length < 50" @click="detailPage(1)">{{ t('nextPage') }}</b-button>
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
    error: '',
    timezone: '',
    scopes: [],
    types: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10],
    countKeys: ['addresses', 'to_block', 'already', 'excluded', 'unmatched', 'skipped'],
    detail: {
      open: false, scope: 'private', run: null, items: [], page: 1, loading: false, error: '', sequence: 0,
    },
  }),
  computed: { anyBusy() { return this.loading || this.scopes.some((state) => state.busy); } },
  created() { this.load(); },
  methods: {
    t(key) { return this.$t(`deliveryAutomation.${key}`); },
    formatTime(value) { return parse(value).format('YYYY-MM-DD HH:mm'); },
    config(state) {
      return { ...clone(state.draft), excluded_emails: [...new Set(state.exclusionsText.split('\n').map((s) => s.trim().toLowerCase()).filter(Boolean))] };
    },
    signature(state) { return JSON.stringify(this.config(state)); },
    dirty(state) { return this.signature(state) !== JSON.stringify(state.rule.config); },
    validPreview(state) {
      return state.preview && state.previewSignature === this.signature(state) && Date.now() - state.previewAt < 15 * 60 * 1000;
    },
    async load() {
      this.loading = true; this.error = '';
      try {
        const data = await this.$api.getProviderAutomation(); this.timezone = data.timezone;
        this.scopes = data.scopes.map(({ rule, runs }) => ({
          rule,
          runs,
          draft: clone(rule.config),
          exclusionsText: rule.config.excluded_emails.join('\n'),
          busy: '',
          error: '',
          notice: '',
          preview: null,
          previewSignature: '',
          previewAt: 0,
        }));
      } catch (error) { this.error = message(error); } finally { this.loading = false; }
    },
    async history(state) {
      const data = await this.$api.getProviderAutomation();
      const updated = data.scopes.find((entry) => entry.rule.scope === state.rule.scope);
      state.runs = updated.runs; state.rule.next_run_at = updated.rule.next_run_at;
    },
    async preview(state) {
      if (state.busy) return;
      state.busy = 'preview'; state.error = ''; state.notice = ''; state.preview = null;
      const config = this.config(state); const signature = JSON.stringify(config);
      try {
        const result = await this.$api.previewProviderAutomation(state.rule.scope, { version: state.rule.version, config });
        state.preview = result; state.previewSignature = signature; state.previewAt = Date.now();
        await this.history(state);
      } catch (error) { state.error = message(error); } finally { state.busy = ''; }
    },
    async save(state) {
      if (state.busy || (state.draft.enabled && !this.validPreview(state))) return;
      state.busy = 'save'; state.error = ''; state.notice = '';
      try {
        const rule = await this.$api.saveProviderAutomation(state.rule.scope, {
          version: state.rule.version, config: this.config(state), preview_id: state.preview?.run.id || 0,
        });
        state.rule = rule; state.draft = clone(rule.config); state.exclusionsText = rule.config.excluded_emails.join('\n');
        state.preview = null; state.notice = this.t('saved');
      } catch (error) { state.error = message(error); } finally { state.busy = ''; }
    },
    confirmRun(state) {
      this.$utils.confirm(this.$t('deliveryAutomation.confirmRun', { count: state.preview.run.counts.to_block, scope: this.t(state.rule.scope) }), () => this.run(state), null, {
        title: this.t('run'), confirmText: this.t('run'), type: 'is-danger', focusOn: 'cancel',
      });
    },
    async run(state) {
      if (state.busy || this.dirty(state) || !this.validPreview(state)) return;
      state.busy = 'run'; state.error = ''; state.notice = '';
      try {
        await this.$api.runProviderAutomation(state.rule.scope, { version: state.rule.version, preview_id: state.preview.run.id });
        state.preview = null; state.notice = this.t('completed'); await this.history(state);
      } catch (error) { state.error = message(error); } finally { state.busy = ''; }
    },
    showDetails(state, run) {
      this.detail = {
        open: true, scope: state.rule.scope, run, items: [], page: 1, loading: false, error: '', sequence: this.detail.sequence + 1,
      };
      this.fetchDetails();
    },
    detailPage(delta) { this.detail.page += delta; this.fetchDetails(); },
    async fetchDetails() {
      const { detail } = this; detail.sequence += 1; const { sequence } = detail;
      detail.loading = true; detail.error = ''; detail.items = [];
      try {
        const items = await this.$api.getProviderAutomationItems(detail.scope, detail.run.id, detail.page);
        if (detail === this.detail && detail.sequence === sequence) detail.items = items;
      } catch (error) { if (detail === this.detail && detail.sequence === sequence) detail.error = message(error); } finally {
        if (detail === this.detail && detail.sequence === sequence) detail.loading = false;
      }
    },
  },
};
</script>

<style lang="scss" scoped>
.automation-intro, .automation-heading, .automation-actions, .automation-next { display: flex; flex-wrap: wrap; align-items: center; gap: var(--lm-space-3); }
.automation-intro { justify-content: space-between; margin-bottom: var(--lm-space-4); }
.automation-rules { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--lm-space-6); }
.automation-rule { min-width: 0; padding: var(--lm-space-6); border: 1px solid var(--lm-color-border); border-radius: var(--lm-radius-md); background: var(--lm-color-surface); }
.automation-heading { justify-content: space-between; margin-bottom: var(--lm-space-3); }
h2 { font-size: 18px; font-weight: 600; }
h3, legend { font-size: 14px; font-weight: 600; margin-bottom: var(--lm-space-3); }
.automation-help { color: var(--lm-color-text-muted); line-height: 1.6; margin: var(--lm-space-3) 0 var(--lm-space-4); }
fieldset { margin-bottom: var(--lm-space-6); }
.automation-types { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--lm-space-3); }
.automation-fields { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--lm-space-3); margin: var(--lm-space-4) 0; }
.automation-preview { border: 1px solid var(--lm-color-border); padding: var(--lm-space-4); margin-bottom: var(--lm-space-4); }
dl { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: var(--lm-space-3); margin: var(--lm-space-4) 0; }
dt { color: var(--lm-color-text-muted); font-size: 12px; }
dd { font-size: 20px; font-variant-numeric: tabular-nums; }
.automation-next { border-top: 1px solid var(--lm-color-border); padding-top: var(--lm-space-4); margin-top: var(--lm-space-6); margin-bottom: var(--lm-space-4); color: var(--lm-color-text-muted); }
.automation-history { list-style: none; margin: 0; padding: 0; max-height: 360px; overflow: auto; }
.automation-history li { border-bottom: 1px solid var(--lm-color-border); padding: var(--lm-space-3) 0; }
.automation-history li > div { display: flex; flex-wrap: wrap; justify-content: space-between; gap: var(--lm-space-2); }
.automation-history p { margin: var(--lm-space-2) 0; }
.automation-detail { width: min(900px, calc(100vw - 32px)); max-width: 100%; }
.automation-table { overflow: auto; }
.automation-table td { overflow-wrap: anywhere; min-width: 110px; }
@media screen and (max-width: 1100px) { .automation-rules { grid-template-columns: 1fr; } }
@media screen and (max-width: 480px) {
  .automation-rule { padding: var(--lm-space-4); }
  .automation-types, .automation-fields { grid-template-columns: 1fr; }
  .automation-actions .button { white-space: normal; height: auto; min-height: 40px; }
}
</style>
