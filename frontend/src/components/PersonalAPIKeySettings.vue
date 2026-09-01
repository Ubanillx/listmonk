<template>
  <section class="personal-api-keys mt-6">
    <div class="level api-key-header mb-5">
      <h2 class="title is-5 mb-1">{{ $t('apiKeys.title') }}</h2>
      <b-button type="is-primary" icon-left="plus" @click="openCreate">{{ $t('apiKeys.new') }}</b-button>
    </div>

    <div v-if="keys.length === 0" class="notification is-light api-key-empty-state">
      <b-icon icon="key-outline" size="is-small" />
      <span>{{ $t('apiKeys.empty') }}</span>
    </div>

    <div v-else class="table-container api-key-table-wrap">
      <table class="table is-fullwidth is-hoverable">
        <thead>
          <tr>
            <th>{{ $t('apiKeys.name') }}</th>
            <th>{{ $t('apiKeys.workspace') }}</th>
            <th>{{ $t('apiKeys.scopes') }}</th>
            <th>{{ $t('apiKeys.expires') }}</th>
            <th>{{ $t('apiKeys.lastUsed') }}</th>
            <th>{{ $t('apiKeys.status') }}</th>
            <th class="has-text-right">{{ $t('apiKeys.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="key in keys" :key="key.id">
            <td>{{ key.name }}</td>
            <td>{{ workspaceName(key) }}</td>
            <td>{{ scopeSummary(key.scopes) }}</td>
            <td>{{ formatDate(key.expiresAt) }}</td>
            <td>{{ formatDate(key.lastUsedAt) }}</td>
            <td><b-tag :type="keyStatusType(key)" size="is-small">{{ keyStatus(key) }}</b-tag></td>
            <td class="has-text-right">
              <b-button size="is-small" type="is-text" icon-left="pencil-outline" :title="$t('apiKeys.edit')" :disabled="!isActive(key)" @click="openEdit(key)" />
              <b-button size="is-small" type="is-text" icon-left="refresh" :title="$t('apiKeys.rotate')" :disabled="!isActive(key)" @click="openRotate(key)" />
              <b-button size="is-small" type="is-text" icon-left="trash-can-outline" :title="$t('apiKeys.revoke')" :disabled="!isActive(key)" @click="revoke(key)" />
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <b-modal :active.sync="showEditor" has-modal-card :can-cancel="!saving">
      <form class="modal-card" @submit.prevent="save">
        <header class="modal-card-head"><p class="modal-card-title">{{ editingID ? $t('apiKeys.edit') : $t('apiKeys.new') }}</p></header>
        <section class="modal-card-body">
          <b-field :label="$t('apiKeys.name')" label-position="on-border"><b-input v-model="form.name" maxlength="200" required autofocus /></b-field>
          <b-field v-if="!editingID" :label="$t('apiKeys.workspace')" label-position="on-border">
            <b-select v-model="form.workspaceOrganizationId" expanded>
              <option v-for="workspace in workspaces" :key="workspace.id" :value="workspace.id">{{ workspace.name }}</option>
            </b-select>
          </b-field>
          <b-field :label="$t('apiKeys.expires')" label-position="on-border">
            <b-select v-model="form.expiresAt" expanded required>
              <option v-for="month in expiryMonths" :key="month.value" :value="month.value">{{ month.label }}</option>
            </b-select>
          </b-field>
          <div class="api-key-scopes">
            <div class="level is-mobile mb-2"><p class="label mb-0">{{ $t('apiKeys.permissions') }}</p><b-checkbox v-model="allScopesSelected">{{ $t('apiKeys.all') }}</b-checkbox></div>
            <div class="api-key-scope-grid">
              <b-checkbox v-for="scope in scopeOptions" :key="scope.id" v-model="form.scopes" :native-value="scope.id">{{ scopeLabel(scope.id) }}</b-checkbox>
            </div>
          </div>
        </section>
        <footer class="modal-card-foot">
          <b-button type="button" @click="showEditor = false">{{ $t('globals.buttons.cancel') }}</b-button>
          <b-button type="is-primary" native-type="submit" :loading="saving">{{ $t('globals.buttons.save') }}</b-button>
        </footer>
      </form>
    </b-modal>

    <b-modal :active.sync="showRotation" has-modal-card :can-cancel="!rotating">
      <form class="modal-card" @submit.prevent="rotate">
        <header class="modal-card-head"><p class="modal-card-title">{{ $t('apiKeys.rotateTitle') }}</p></header>
        <section class="modal-card-body">
          <b-field :label="$t('apiKeys.expires')" label-position="on-border">
            <b-select v-model="rotationExpiry" expanded required>
              <option v-for="month in expiryMonths" :key="month.value" :value="month.value">{{ month.label }}</option>
            </b-select>
          </b-field>
        </section>
        <footer class="modal-card-foot">
          <b-button type="button" @click="showRotation = false">{{ $t('globals.buttons.cancel') }}</b-button>
          <b-button type="is-primary" native-type="submit" :loading="rotating">{{ $t('apiKeys.rotate') }}</b-button>
        </footer>
      </form>
    </b-modal>

    <b-modal :active.sync="showToken" has-modal-card :can-cancel="false">
      <section class="modal-card">
        <header class="modal-card-head"><p class="modal-card-title">{{ $t('apiKeys.secretTitle') }}</p></header>
        <section class="modal-card-body">
          <b-message type="is-warning" :closable="false">{{ $t('apiKeys.secretWarning') }}</b-message>
          <b-input :value="createdToken" readonly type="textarea" rows="3" />
        </section>
        <footer class="modal-card-foot api-key-secret-actions"><copy-text :text="createdToken" /><b-button type="is-primary" @click="closeSecret">{{ $t('globals.buttons.done') }}</b-button></footer>
      </section>
    </b-modal>
  </section>
</template>

<script>
import Vue from 'vue';
import CopyText from './CopyText.vue';

function emptyForm(defaultExpiry) {
  return {
    name: '', workspaceOrganizationId: 0, scopes: [], expiresAt: defaultExpiry,
  };
}

export default Vue.extend({
  name: 'PersonalAPIKeySettings',
  components: { CopyText },
  data() {
    return {
      keys: [],
      scopeOptions: [],
      organizations: [],
      form: emptyForm(''),
      editingID: 0,
      showEditor: false,
      showRotation: false,
      rotatingKeyID: 0,
      rotationExpiry: '',
      showToken: false,
      createdToken: '',
      saving: false,
      rotating: false,
    };
  },
  computed: {
    expiryMonths() {
      const months = [];
      const start = new Date();
      for (let offset = 1; offset <= 24; offset += 1) {
        const date = new Date(start.getFullYear(), start.getMonth() + offset, 1);
        const value = `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}`;
        months.push({ value, label: value });
      }
      return months;
    },
    workspaces() {
      return [
        { id: 0, name: this.$t('apiKeys.personalWorkspace') },
        ...this.organizations.filter((org) => org.status !== 'archived').map((org) => ({ id: org.id, name: org.name })),
      ];
    },
    allScopesSelected: {
      get() { return this.scopeOptions.length > 0 && this.form.scopes.length === this.scopeOptions.length; },
      set(enabled) { this.form.scopes = enabled ? this.scopeOptions.map((scope) => scope.id) : []; },
    },
  },
  methods: {
    defaultExpiry() { return this.expiryMonths.length ? this.expiryMonths[0].value : ''; },
    load() {
      return Promise.all([this.$api.getPersonalAPIKeyScopes(), this.$api.getPersonalAPIKeys(), this.$api.getMyOrganizations()])
        .then(([scopeOptions, keys, organizations]) => {
          this.scopeOptions = scopeOptions || [];
          this.keys = keys || [];
          this.organizations = organizations || [];
        });
    },
    openCreate() {
      this.editingID = 0;
      this.form = emptyForm(this.defaultExpiry());
      this.form.scopes = this.scopeOptions.map((scope) => scope.id);
      this.showEditor = true;
    },
    openEdit(key) {
      this.editingID = key.id;
      this.form = {
        name: key.name,
        workspaceOrganizationId: Number(key.workspaceOrganizationId || key.workspace_organization_id) || 0,
        scopes: [...(key.scopes || [])],
        expiresAt: this.expiryMonth(key.expiresAt || key.expires_at),
      };
      this.showEditor = true;
    },
    save() {
      if (!this.form.scopes.length) {
        this.$utils.toast(this.$t('apiKeys.scopeRequired'), 'is-danger');
        return;
      }
      this.saving = true;
      const payload = { name: this.form.name, scopes: this.form.scopes, expires_at: this.form.expiresAt };
      const request = this.editingID
        ? this.$api.updatePersonalAPIKey(this.editingID, payload)
        : this.$api.createPersonalAPIKey({ ...payload, workspace_organization_id: this.form.workspaceOrganizationId });
      request.then((data) => {
        this.showEditor = false;
        if (data.token) { this.createdToken = data.token; this.showToken = true; }
        return this.load();
      }).finally(() => { this.saving = false; });
    },
    openRotate(key) {
      this.rotatingKeyID = key.id;
      this.rotationExpiry = this.expiryMonth(key.expiresAt || key.expires_at) || this.defaultExpiry();
      this.showRotation = true;
    },
    rotate() {
      this.rotating = true;
      this.$api.rotatePersonalAPIKey(this.rotatingKeyID, { expires_at: this.rotationExpiry }).then((data) => {
        this.showRotation = false;
        this.createdToken = data.token;
        this.showToken = true;
        return this.load();
      }).finally(() => { this.rotating = false; });
    },
    revoke(key) { this.$utils.confirm(null, () => this.$api.deletePersonalAPIKey(key.id).then(() => this.load())); },
    closeSecret() { this.showToken = false; this.createdToken = ''; },
    workspaceName(key) {
      const id = Number(key.workspaceOrganizationId || key.workspace_organization_id) || 0;
      if (!id) return this.$t('apiKeys.personalWorkspace');
      const org = this.organizations.find((candidate) => Number(candidate.id) === id);
      return org ? org.name : this.$t('apiKeys.organization', { id });
    },
    scopeLabel(scope) {
      const keys = {
        'lists:read': 'apiKeys.scope.listsRead',
        'lists:write': 'apiKeys.scope.listsWrite',
        'subscribers:read': 'apiKeys.scope.subscribersRead',
        'subscribers:write': 'apiKeys.scope.subscribersWrite',
        'subscribers:import': 'apiKeys.scope.subscribersImport',
        'templates:read': 'apiKeys.scope.templatesRead',
        'templates:write': 'apiKeys.scope.templatesWrite',
        'media:read': 'apiKeys.scope.mediaRead',
        'media:write': 'apiKeys.scope.mediaWrite',
        'campaigns:read': 'apiKeys.scope.campaignsRead',
        'campaigns:write': 'apiKeys.scope.campaignsWrite',
        'campaigns:send': 'apiKeys.scope.campaignsSend',
        'campaigns:analytics': 'apiKeys.scope.campaignsAnalytics',
        'campaigns:recipients': 'apiKeys.scope.campaignsRecipients',
        'bounces:read': 'apiKeys.scope.bouncesRead',
        'bounces:write': 'apiKeys.scope.bouncesWrite',
        'transactional:send': 'apiKeys.scope.transactionalSend',
      };
      return this.$t(keys[scope] || scope);
    },
    scopeSummary(scopes) { return this.$t('apiKeys.scopeCount', { count: (scopes || []).length }); },
    expiryMonth(value) { return value ? String(value).slice(0, 7) : ''; },
    formatDate(value) {
      if (!value) return this.$t('apiKeys.never');
      const date = new Date(value);
      return Number.isNaN(date.getTime()) ? String(value) : date.toLocaleString();
    },
    isActive(key) {
      if (key.revokedAt || key.revoked_at) return false;
      const expiresAt = key.expiresAt || key.expires_at;
      return expiresAt && new Date(expiresAt).getTime() > Date.now();
    },
    keyStatus(key) {
      if (key.revokedAt || key.revoked_at) return this.$t('apiKeys.status.revoked');
      return this.isActive(key) ? this.$t('apiKeys.status.active') : this.$t('apiKeys.status.expired');
    },
    keyStatusType(key) {
      if (key.revokedAt || key.revoked_at) return 'is-light';
      return this.isActive(key) ? 'is-success' : 'is-warning';
    },
  },
  mounted() { this.load(); },
});
</script>

<style lang="scss" scoped>
.personal-api-keys { width: 100%; }
.api-key-header { align-items: center; }
.api-key-empty-state { display: flex; align-items: center; gap: .65rem; max-width: 760px; border: 1px dashed #d9e0ea; background: #f8fafc; color: #5b6575; }
.api-key-table-wrap { border: 1px solid #e5e9f0; border-radius: 8px; }
.api-key-table-wrap .table { margin: 0; }
.api-key-scopes { padding: .85rem; border: 1px solid #e5e9f0; border-radius: 6px; }
.api-key-scope-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: .75rem 1rem; }
.api-key-secret-actions { align-items: center; justify-content: space-between; }
@media (max-width: 768px) {
  .api-key-header { display: block; }
  .api-key-header .button { width: 100%; margin-top: .75rem; }
}
</style>
