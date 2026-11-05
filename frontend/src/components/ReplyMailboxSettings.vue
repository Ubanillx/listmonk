<template>
  <section class="reply-mailboxes mt-6">
    <div class="reply-mailboxes-header level mb-5">
      <div>
        <h2 class="title is-5 mb-1"><b-icon icon="email-arrow-left-outline" size="is-small" /> {{ $t('replyMailbox.title') }}</h2>
        <p class="help">{{ organizationId ? $t('replyMailbox.organizationHelp') : $t('replyMailbox.help') }}</p>
      </div>
      <b-button type="is-primary" icon-left="plus" @click="addMailbox">{{ $t('replyMailbox.add') }}</b-button>
    </div>

    <p class="help mb-4" data-cy="reply-mailbox-flow">{{ $t('replyMailbox.setupHelp') }}</p>

    <div v-if="mailboxes.length === 0" class="notification is-light reply-empty-state">
      <b-icon icon="email-off-outline" size="is-small" />
      <span>{{ $t('replyMailbox.empty') }}</span>
    </div>

    <div v-for="(mailbox, index) in mailboxes" :key="mailbox.id || `new-${index}`" class="box reply-mailbox-card">
      <div class="reply-card-header">
        <div>
          <div class="reply-card-title">
            {{ mailbox.name || mailbox.email || `${$t('replyMailbox.fallbackName', { n: index + 1 })}` }}
            <b-tag v-if="mailbox.id" rounded size="is-small" :type="statusType(mailbox.status)">
              {{ statusLabel(mailbox) }}
            </b-tag>
            <b-tag v-if="mailbox.isDefault" type="is-info" rounded size="is-small">{{ $t('replyMailbox.defaultTag') }}</b-tag>
            <b-tag v-if="mailbox.readOnly" type="is-light" rounded size="is-small">{{ $t('replyMailbox.otherMember') }}</b-tag>
          </div>
          <p class="reply-card-subtitle">{{ mailbox.email || $t('replyMailbox.noEmail') }}</p>
        </div>
        <div class="buttons mb-0">
          <b-button v-if="mailbox.id && mailbox.status !== 'disabled' && !mailbox.readOnly" type="is-danger" outlined
            size="is-small" icon-left="pause-circle-outline" @click="disableMailbox(mailbox, index)">
            {{ $t('replyMailbox.disable') }}
          </b-button>
          <b-button v-else-if="mailbox.id && !mailbox.readOnly" type="is-primary" outlined size="is-small"
            icon-left="play-circle-outline" :loading="enabling === index" @click="enableMailbox(mailbox, index)">
            {{ $t('replyMailbox.enable') }}
          </b-button>
          <b-button v-if="mailbox.id && mailbox.deletable === true" type="is-danger" outlined size="is-small" icon-left="trash-can-outline"
            :loading="deleting === index" data-cy="reply-mailbox-delete" @click="removeMailbox(mailbox, index)">
            {{ $t('replyMailbox.delete') }}
          </b-button>
        </div>
      </div>

      <div class="reply-card-body">
      <h3 class="title is-6 mb-3">{{ $t('replyMailbox.addressSection') }}</h3>
      <div class="columns is-multiline reply-grid">
        <div class="column is-6">
          <b-field :label="$t('replyMailbox.emailLabel')" label-position="on-border">
            <b-input v-model.trim="mailbox.email" type="email" required placeholder="name@example.com"
              :disabled="mailbox.readOnly || busy(index)" data-cy="reply-mailbox-email" />
          </b-field>
        </div>
        <div class="column is-6">
          <b-field :label="$t('replyMailbox.nameLabel')" label-position="on-border">
            <b-input v-model="mailbox.name" maxlength="100" :placeholder="$t('replyMailbox.nameLabel')"
              :disabled="mailbox.readOnly || busy(index)" data-cy="reply-mailbox-name" />
          </b-field>
        </div>
      </div>
      <div class="mt-5 mb-4">
        <h3 class="title is-6 mb-3">{{ $t('replyMailbox.processingSection') }}</h3>
        <b-switch v-model="mailbox.aiEnabled" :disabled="mailbox.readOnly || busy(index)" data-cy="reply-mailbox-ai">
          {{ $t('replyMailbox.aiToggle') }}
        </b-switch>
        <p class="help mt-2">{{ $t('replyMailbox.aiSetupHelp') }}</p>

        <b-switch :value="mailbox.bounceEnabled" :disabled="mailbox.readOnly || busy(index)"
          data-cy="reply-mailbox-bounce" @input="onBounceToggle(mailbox, $event)">
          {{ $t('replyMailbox.bounceToggle') }}
        </b-switch>
        <p class="help mt-2">{{ $t('replyMailbox.bounceHelp') }}</p>
        <p v-if="mailbox.bounceEnabled && mailbox.id && !mailbox.bounceProcessingEnabled" class="help has-text-warning"
          data-cy="reply-mailbox-bounce-global-warning">
          {{ $t('replyMailbox.bounceRequiresGlobal') }}
        </p>
        <p v-if="mailbox.bounceEnabled && mailbox.id && mailbox.bouncePolicy && mailbox.bouncePolicy.action !== 'blocklist'"
          class="help has-text-warning" data-cy="reply-mailbox-bounce-policy-warning">
          {{ $t('replyMailbox.bouncePolicyNotBlocklist') }}
        </p>
        <p v-if="mailbox.bounceEnabled && mailbox.id && mailbox.bouncePolicy && mailbox.bouncePolicy.action === 'blocklist'"
          class="help" data-cy="reply-mailbox-bounce-policy">
          {{ $t('replyMailbox.bouncePolicy', { count: mailbox.bouncePolicy.threshold }) }}
        </p>
      </div>
      <div v-if="mailbox.aiEnabled || mailbox.bounceEnabled" data-cy="reply-mailbox-receiving">
      <h3 class="title is-6 mb-3">{{ $t('replyMailbox.receivingSection') }}</h3>
      <div class="columns is-multiline reply-grid">
        <div class="column is-6">
          <b-field :label="$t('replyMailbox.usernameLabel')" label-position="on-border" :message="$t('replyMailbox.usernameHelp')">
            <b-input v-model.trim="mailbox.username" placeholder="user@example.com" :disabled="mailbox.readOnly || busy(index)"
              data-cy="reply-mailbox-username" />
          </b-field>
        </div>
        <div class="column is-6">
          <b-field :label="$t('replyMailbox.passwordLabel')" label-position="on-border"
            :message="mailbox.hasPassword ? $t('replyMailbox.passwordStoredHelp') : $t('replyMailbox.passwordHelp')">
            <b-input v-model="mailbox.password" type="password" password-reveal :disabled="mailbox.readOnly || busy(index)"
              data-cy="reply-mailbox-password"
              :placeholder="mailbox.hasPassword ? $t('replyMailbox.passwordSaved') : $t('replyMailbox.passwordPlaceholder')" />
          </b-field>
        </div>
        <div class="column is-6">
          <b-field :label="$t('replyMailbox.imapHostLabel')" label-position="on-border">
            <b-input v-model.trim="mailbox.imapHost" placeholder="imap.263.net" :disabled="mailbox.readOnly || busy(index)"
              data-cy="reply-mailbox-host" />
          </b-field>
        </div>
        <div class="column is-3">
          <b-field :label="$t('replyMailbox.portLabel')" label-position="on-border">
            <b-numberinput v-model="mailbox.imapPort" min="1" max="65535" controls-position="compact" :disabled="mailbox.readOnly || busy(index)" />
          </b-field>
        </div>
        <div class="column is-3">
          <b-field :label="$t('replyMailbox.folderLabel')" label-position="on-border">
            <b-input v-model.trim="mailbox.folder" placeholder="INBOX" :disabled="mailbox.readOnly || busy(index)" />
          </b-field>
        </div>
      </div>
      <p v-if="!mailbox.readOnly" class="help" data-cy="reply-mailbox-test-hint">
        {{ !mailbox.id || hasUnsavedChanges(mailbox) ? $t('replyMailbox.saveBeforeTest') : $t('replyMailbox.testSavedHelp') }}
      </p>
      </div>

      <div v-if="inspectMailboxId === mailbox.id && inspectResult" class="box mt-4" data-cy="reply-mailbox-inspect-result">
        <p v-if="inspectResult.empty" class="has-text-weight-semibold">{{ $t('replyMailbox.inspectEmpty') }}</p>
        <template v-else>
          <p class="has-text-weight-semibold">
            {{ inspectResult.isBounce ? $t('replyMailbox.inspectBounce') : $t('replyMailbox.inspectNotBounce') }}
          </p>
          <p v-if="inspectResult.subject" class="help">{{ $t('replyMailbox.inspectSubject') }}: {{ inspectResult.subject }}</p>
          <p v-if="inspectResult.from" class="help">{{ $t('replyMailbox.inspectFrom') }}: {{ inspectResult.from }}</p>
          <p v-if="inspectResult.date" class="help">{{ $t('replyMailbox.inspectDate') }}: {{ inspectResult.date }}</p>
          <p v-if="inspectResult.parseError" class="help has-text-warning">{{ $t('replyMailbox.inspectParseError') }}</p>
          <template v-if="inspectResult.isBounce">
            <p v-if="inspectResult.bounceType" class="help">
              {{ $t('replyMailbox.inspectType') }}: {{ bounceTypeLabel(inspectResult.bounceType) }}
            </p>
            <ul v-if="inspectResult.recipients && inspectResult.recipients.length">
              <li v-for="(recipient, i) in inspectResult.recipients" :key="i">
                {{ recipient.address }} <span v-if="recipient.status">— {{ recipient.status }}</span>
              </li>
            </ul>
            <p v-if="inspectResult.reason" class="help reply-bounce-reason">{{ inspectResult.reason }}</p>
          </template>
        </template>
      </div>

      <p v-if="inspectMailboxId === mailbox.id && inspectError" class="help has-text-danger"
        data-cy="reply-mailbox-inspect-error">
        {{ inspectError }}
      </p>

      <div v-if="mailbox.id && mailbox.inspectable" class="mt-5" data-cy="reply-mailbox-bounce-history">
        <a href="#" class="has-text-weight-semibold" @click.prevent="toggleBounceEvents(mailbox)">
          <b-icon :icon="bounceEventsOpen === mailbox.id ? 'chevron-down' : 'chevron-right'" size="is-small" />
          {{ $t('replyMailbox.bounceEventsTitle') }}
        </a>
        <div v-if="bounceEventsOpen === mailbox.id" class="mt-3" data-cy="reply-mailbox-bounce-events">
          <p v-if="bounceEventsLoading && !bounceEvents.length" class="help">{{ $t('replyMailbox.bounceEventsLoading') }}</p>
          <p v-else-if="bounceEventsError && !bounceEvents.length" class="help has-text-danger">{{ bounceEventsError }}</p>
          <p v-else-if="!bounceEvents.length" class="help">{{ $t('replyMailbox.bounceEventsEmpty') }}</p>
          <template v-else>
            <p v-if="bounceEventsError" class="help has-text-danger">{{ bounceEventsError }}</p>
            <table class="table is-fullwidth is-narrow is-size-7">
              <thead>
                <tr>
                  <th>{{ $t('replyMailbox.bounceEventsTime') }}</th>
                  <th>{{ $t('replyMailbox.bounceEventsRecipient') }}</th>
                  <th>{{ $t('replyMailbox.bounceEventsType') }}</th>
                  <th>{{ $t('replyMailbox.bounceEventsOutcome') }}</th>
                  <th>{{ $t('replyMailbox.bounceEventsReason') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="event in bounceEvents" :key="event.id">
                  <td>{{ $utils.niceDate(event.createdAt, true) }}</td>
                  <td class="reply-bounce-recipient">{{ event.recipient || '—' }}</td>
                  <td>{{ bounceTypeLabel(event.bounceType) }}</td>
                  <td>{{ outcomeLabel(event.outcome) }}</td>
                  <td>{{ reasonLabel(event.reasonCode) }}</td>
                </tr>
              </tbody>
            </table>
            <b-button v-if="bounceEvents.length < bounceEventsTotal" size="is-small" :loading="bounceEventsLoading"
              :disabled="bounceEventsLoading"
              data-cy="reply-mailbox-bounce-events-more" @click="loadBounceEvents(mailbox, bounceEventsPage + 1)">
              {{ $t('replyMailbox.bounceEventsMore') }}
            </b-button>
          </template>
        </div>
      </div>
      </div>

      <div class="reply-card-footer">
        <div class="reply-card-default-toggle">
          <b-checkbox v-model="mailbox.isDefault" :disabled="mailbox.readOnly || busy(index)">{{ $t('replyMailbox.setDefault') }}</b-checkbox>
        </div>
        <div class="buttons mb-0">
          <b-button v-if="!mailbox.readOnly" type="is-primary" icon-left="content-save-outline" :loading="saving === index" :disabled="testing === index"
            data-cy="reply-mailbox-save" @click="saveMailbox(mailbox, index)">
            {{ $t('replyMailbox.saveConfiguration') }}
          </b-button>
          <b-button v-if="!mailbox.readOnly && (mailbox.aiEnabled || mailbox.bounceEnabled)" type="is-light" icon-left="connection" :loading="testing === index"
            :disabled="!mailbox.id || hasUnsavedChanges(mailbox) || saving === index" data-cy="reply-mailbox-test"
            @click="testMailbox(mailbox, index)">
            {{ $t('replyMailbox.testConnection') }}
          </b-button>
          <b-button v-if="mailbox.inspectable && (mailbox.aiEnabled || mailbox.bounceEnabled)" type="is-light" icon-left="magnify"
            :loading="inspecting === index" :disabled="!mailbox.id || hasUnsavedChanges(mailbox) || saving === index"
            data-cy="reply-mailbox-inspect" @click="inspectMailbox(mailbox, index)">
            {{ $t('replyMailbox.inspectLatest') }}
          </b-button>
        </div>
        <p v-if="mailbox.readOnly" class="help mb-0" data-cy="reply-mailbox-readonly-note">{{ $t('replyMailbox.readOnlyNote') }}</p>
      </div>
    </div>
  </section>
</template>

<script>
import Vue from 'vue';
import { mapState } from 'vuex';

function blankMailbox() {
  return {
    id: 0,
    email: '',
    name: '',
    username: '',
    password: '',
    imapHost: '',
    imapPort: 993,
    imapTls: true,
    folder: 'INBOX',
    status: 'pending',
    isDefault: false,
    aiEnabled: false,
    bounceEnabled: false,
    bounceProcessingEnabled: false,
    bouncePolicy: { enabled: false, threshold: 1, action: '' },
    inspectable: false,
    hasPassword: false,
    verifiedAt: null,
    lastSyncAt: null,
    lastSyncError: '',
    forwardCount: 0,
  };
}

export default Vue.extend({
  name: 'ReplyMailboxSettings',

  props: {
    // Organization management can inspect a selected organization without
    // changing the browser's active workspace. Personal profile usage omits
    // this prop and follows the active workspace as before.
    organizationId: { type: Number, default: null },
  },

  data() {
    return {
      mailboxes: [],
      saving: null,
      testing: null,
      inspecting: null,
      enabling: null,
      deleting: null,
      loadedWorkspace: null,
      loadRevision: 0,
      inspectMailboxId: null,
      inspectResult: null,
      inspectError: '',
      inspectRevision: 0,
      bounceEventsOpen: null,
      bounceEvents: [],
      bounceEventsPage: 0,
      bounceEventsTotal: 0,
      bounceEventsLoading: false,
      bounceEventsError: '',
      bounceEventsRevision: 0,
    };
  },

  computed: {
    ...mapState(['workspace']),
    workspaceKey() {
      if (Number.isInteger(this.organizationId) && this.organizationId > 0) {
        return this.organizationId;
      }
      return Number(this.workspace && this.workspace.organizationId) || 0;
    },

    apiOrganizationID() {
      return this.organizationId || null;
    },
  },

  watch: {
    workspaceKey() {
      this.mailboxes = [];
      this.load();
    },
  },
  methods: {
    normalize(row) {
      const mailbox = {
        ...blankMailbox(), ...row, password: '', readOnly: row.manageable === false,
      };
      mailbox.bounceEnabled = !!mailbox.bounceEnabled;
      mailbox.bounceProcessingEnabled = !!mailbox.bounceProcessingEnabled;
      mailbox.bouncePolicy = mailbox.bouncePolicy || { enabled: false, threshold: 1, action: '' };
      mailbox.savedConfig = JSON.stringify(this.wire(mailbox, false));
      return mailbox;
    },

    // The listing returns every reply mailbox of the organization so the
    // workspace sees its full inventory. A row that belongs to another member is
    // rendered read-only: the address is usable by the whole workspace, but
    // editing, testing and disabling it requires ownership. Each row also
    // carries `deletable` from the API, which is the deletion right the server
    // enforces for this caller, so the delete button only appears where the
    // request can succeed.
    load() {
      const workspace = this.workspaceKey;
      this.loadRevision += 1;
      const revision = this.loadRevision;
      this.loadedWorkspace = workspace;
      return this.$api.getReplyMailboxes(this.apiOrganizationID).then((data) => {
        if (workspace !== this.workspaceKey || revision !== this.loadRevision || this.isDirty()) return;
        const rows = Array.isArray(data) ? data : [];
        this.mailboxes = rows.map(this.normalize);
      });
    },

    isDirty() {
      return this.mailboxes.some((mailbox) => !mailbox.id || this.hasUnsavedChanges(mailbox));
    },

    addMailbox() {
      this.mailboxes.push(blankMailbox());
    },

    statusType(status) {
      if (status === 'active') return 'is-success';
      if (status === 'retained') return 'is-warning';
      if (status === 'disabled') return 'is-light';
      return 'is-info';
    },

    statusLabel(mailbox) {
      if (mailbox.status === 'active' && !mailbox.aiEnabled && !mailbox.bounceEnabled) return this.$t('replyMailbox.statusAddressReady');
      return ({
        active: this.$t('replyMailbox.statusActive'),
        retained: this.$t('replyMailbox.statusRetained'),
        disabled: this.$t('replyMailbox.statusDisabled'),
        pending: this.$t('replyMailbox.statusPending'),
      })[mailbox.status] || mailbox.status;
    },

    wire(mailbox, includePassword = true) {
      const receiving = mailbox.aiEnabled || mailbox.bounceEnabled;
      const data = {
        email: mailbox.email,
        name: mailbox.name,
        is_default: !!mailbox.isDefault,
        ai_enabled: !!mailbox.aiEnabled,
        bounce_enabled: !!mailbox.bounceEnabled,
      };
      if (receiving) {
        Object.assign(data, {
          username: mailbox.username,
          imap_host: mailbox.imapHost,
          imap_port: Number(mailbox.imapPort) || 993,
          imap_tls: mailbox.imapTls !== false,
          folder: mailbox.folder || 'INBOX',
        });
        if (includePassword && mailbox.password) data.password = mailbox.password;
      }
      return data;
    },

    hasUnsavedChanges(mailbox) {
      const receiving = mailbox.aiEnabled || mailbox.bounceEnabled;
      return (receiving && !!mailbox.password) || JSON.stringify(this.wire(mailbox, false)) !== mailbox.savedConfig;
    },

    busy(index) {
      return this.saving === index || this.testing === index || this.inspecting === index
        || this.enabling === index || this.deleting === index;
    },

    saveMailbox(mailbox, index) {
      if (!mailbox.email) {
        this.$utils.toast(this.$t('replyMailbox.toastMissingEmail'), 'is-danger');
        return;
      }
      if ((mailbox.aiEnabled || mailbox.bounceEnabled) && !mailbox.hasPassword && !mailbox.password) {
        this.$utils.toast(this.$t('replyMailbox.toastMissingFields'), 'is-danger');
        return;
      }
      this.saving = index;
      const organizationID = this.apiOrganizationID;
      const request = mailbox.id
        ? this.$api.updateReplyMailbox(mailbox.id, this.wire(mailbox), this.apiOrganizationID)
        : this.$api.createReplyMailbox(this.wire(mailbox), this.apiOrganizationID);
      request.then((data) => {
        const saved = this.normalize(data);
        this.$set(this.mailboxes, index, saved);
        this.$emit('changed', organizationID);
        this.$utils.toast(this.$t('replyMailbox.toastSaved'));
      }).finally(() => {
        this.saving = null;
      });
    },

    testMailbox(mailbox, index) {
      if ((!mailbox.aiEnabled && !mailbox.bounceEnabled) || !mailbox.id || this.hasUnsavedChanges(mailbox)) {
        this.$utils.toast(this.$t('replyMailbox.saveBeforeTest'), 'is-warning');
        return;
      }
      this.testing = index;
      const organizationID = this.apiOrganizationID;
      this.$api.testReplyMailbox({
        id: mailbox.id,
      }, this.apiOrganizationID).then(() => {
        if (!['disabled', 'retained'].includes(mailbox.status)) this.$set(mailbox, 'status', 'active');
        this.$emit('changed', organizationID);
        this.$utils.toast(this.$t('replyMailbox.toastTestSuccess'));
      }).catch((err) => {
        const message = err.response?.data?.message || this.$t('replyMailbox.toastTestFailed');
        this.$utils.toast(message, 'is-danger');
      }).finally(() => {
        this.testing = null;
      });
    },

    onBounceToggle(mailbox, enabled) {
      if (!enabled) {
        this.$set(mailbox, 'bounceEnabled', false);
        return;
      }
      const policy = mailbox.bouncePolicy || {};
      const count = Number(policy.threshold) || 1;
      if (!mailbox.id || !policy.action) {
        this.$utils.confirm(
          this.$t('replyMailbox.bounceConfirmNew'),
          () => this.$set(mailbox, 'bounceEnabled', true),
          null,
          { type: 'is-warning', focusOn: 'cancel' },
        );
        return;
      }
      if (policy.action !== 'blocklist') {
        this.$utils.confirm(
          this.$t('replyMailbox.bounceConfirmNotBlocklist', { count }),
          () => this.$set(mailbox, 'bounceEnabled', true),
          null,
          { type: 'is-warning' },
        );
        return;
      }
      this.$utils.confirm(
        this.$t('replyMailbox.bounceConfirm', { count }),
        () => this.$set(mailbox, 'bounceEnabled', true),
        null,
        { type: 'is-warning', focusOn: 'cancel' },
      );
    },

    inspectMailbox(mailbox, index) {
      if (!mailbox.id || this.hasUnsavedChanges(mailbox)) {
        this.$utils.toast(this.$t('replyMailbox.saveBeforeTest'), 'is-warning');
        return;
      }
      const mailboxID = mailbox.id;
      this.inspectRevision += 1;
      const revision = this.inspectRevision;
      this.inspecting = index;
      this.inspectMailboxId = mailboxID;
      this.inspectResult = null;
      this.inspectError = '';
      this.$api.inspectReplyMailbox(mailboxID, this.apiOrganizationID).then((data) => {
        if (this.inspectRevision !== revision || this.inspectMailboxId !== mailboxID) return;
        this.inspectResult = data;
      }).catch((err) => {
        if (this.inspectRevision !== revision || this.inspectMailboxId !== mailboxID) return;
        this.inspectError = err.response?.data?.message || this.$t('replyMailbox.inspectFailed');
      }).finally(() => {
        if (this.inspectRevision === revision) this.inspecting = null;
      });
    },

    toggleBounceEvents(mailbox) {
      if (this.bounceEventsOpen === mailbox.id) {
        this.bounceEventsOpen = null;
        this.bounceEventsRevision += 1;
        return;
      }
      this.bounceEventsOpen = mailbox.id;
      this.bounceEvents = [];
      this.bounceEventsPage = 0;
      this.bounceEventsTotal = 0;
      this.bounceEventsError = '';
      this.loadBounceEvents(mailbox, 1);
    },

    loadBounceEvents(mailbox, page) {
      const mailboxID = mailbox.id;
      this.bounceEventsRevision += 1;
      const revision = this.bounceEventsRevision;
      this.bounceEventsLoading = true;
      this.bounceEventsError = '';
      this.$api.getReplyMailboxBounceEvents(mailboxID, page, this.apiOrganizationID).then((data) => {
        if (this.bounceEventsRevision !== revision || this.bounceEventsOpen !== mailboxID) return;
        const results = data.results || [];
        this.bounceEvents = page === 1 ? results : this.bounceEvents.concat(results);
        this.bounceEventsPage = Number(data.page) || page;
        this.bounceEventsTotal = Number(data.total) || 0;
      }).catch((err) => {
        if (this.bounceEventsRevision !== revision || this.bounceEventsOpen !== mailboxID) return;
        this.bounceEventsError = err.response?.data?.message || this.$t('replyMailbox.bounceEventsFailed');
      }).finally(() => {
        if (this.bounceEventsRevision === revision) this.bounceEventsLoading = false;
      });
    },

    bounceTypeLabel(type) {
      const key = `replyMailbox.bounceType.${type || 'unknown'}`;
      return this.$te(key) ? this.$t(key) : (type || this.$t('replyMailbox.bounceType.unknown'));
    },

    outcomeLabel(outcome) {
      const key = `replyMailbox.bounceOutcome.${outcome || 'ignored'}`;
      return this.$te(key) ? this.$t(key) : (outcome || '—');
    },

    reasonLabel(reason) {
      const key = `replyMailbox.bounceReason.${reason}`;
      return this.$te(key) ? this.$t(key) : (reason || '—');
    },

    disableMailbox(mailbox, index) {
      const organizationID = this.apiOrganizationID;
      this.$utils.confirm(this.$t('replyMailbox.confirmDisable'), () => {
        this.$api.deleteReplyMailbox(mailbox.id, organizationID).then(() => {
          this.$set(this.mailboxes, index, { ...mailbox, status: 'disabled', isDefault: false });
          this.$emit('changed', organizationID);
          this.$utils.toast(this.$t('replyMailbox.toastDisabled'));
        });
      });
    },

    // removeMailbox purges the row for good; disabling it is disableMailbox
    // above. The server refuses a mailbox that is the organization's unified
    // reply mailbox or that an active forwarding rule still depends on, and its
    // message is surfaced by the default error toast.
    removeMailbox(mailbox, index) {
      if (!mailbox.id) {
        return;
      }
      const organizationID = this.apiOrganizationID;
      this.$utils.confirm(
        this.$t('replyMailbox.confirmDelete', { email: mailbox.email || mailbox.name }),
        () => {
          this.deleting = index;
          this.$api.purgeReplyMailbox(mailbox.id, organizationID).then(() => {
            this.mailboxes.splice(index, 1);
            this.$emit('changed', organizationID);
            this.$utils.toast(this.$t('replyMailbox.toastDeleted'));
          }).finally(() => {
            this.deleting = null;
          });
        },
      );
    },

    enableMailbox(mailbox, index) {
      this.enabling = index;
      const organizationID = this.apiOrganizationID;
      this.$api.enableReplyMailbox(mailbox.id, this.apiOrganizationID).then((data) => {
        this.$set(this.mailboxes, index, this.normalize(data));
        this.$emit('changed', organizationID);
        this.$utils.toast(this.$t('replyMailbox.toastEnabled'));
      }).finally(() => {
        this.enabling = null;
      });
    },
  },

  mounted() {
    this.load();
  },
});
</script>

<style lang="scss" scoped>
.reply-mailboxes { width: 100%; max-width: 1400px; }
.reply-mailboxes-header { align-items: flex-end; gap: 1rem; }
.reply-mailboxes-header .help { max-width: 800px; margin-top: .25rem; }
.reply-empty-state { display: flex; align-items: center; gap: .65rem; border: 1px dashed #d9e0ea; background: #f8fafc; color: #5b6575; }
.reply-mailbox-card { padding: 0; overflow: hidden; border: 1px solid var(--lm-color-border); border-radius: var(--lm-radius-md); }
.reply-mailbox-card + .reply-mailbox-card { margin-top: var(--lm-space-4); }
.reply-card-header { display: flex; align-items: center; justify-content: space-between; gap: 1rem; padding: 1.1rem 1.25rem; background: var(--lm-color-surface-subtle); border-bottom: 1px solid var(--lm-color-border); }
.reply-card-title { display: flex; align-items: center; flex-wrap: wrap; gap: .5rem; font-size: 1.05rem; font-weight: 600; color: #273142; }
.reply-card-subtitle { margin-top: .25rem; color: #718096; font-size: .85rem; word-break: break-all; }
.reply-card-body { padding: 1.25rem; }
.reply-grid { margin: -.35rem; padding: 0; }
.reply-grid > .column { min-width: 0; padding: .35rem; }
.reply-grid .field { min-width: 0; margin-bottom: .35rem; }
.reply-card-footer { display: flex; align-items: center; justify-content: space-between; gap: 1rem; padding: 1rem 1.25rem; border-top: 1px solid var(--lm-color-border); }
.reply-card-default-toggle { flex-shrink: 0; }
.reply-bounce-recipient { word-break: break-all; }
.reply-bounce-reason { word-break: break-word; }
@media (max-width: 768px) {
  .reply-mailboxes-header, .reply-card-header, .reply-card-footer { display: block; }
  .reply-mailboxes-header .button { width: 100%; margin-top: .85rem; }
  .reply-card-header .button { margin-top: .75rem; }
  .reply-card-footer .buttons { margin-top: .85rem; }
  .reply-card-footer .button { width: 100%; }
}
</style>
