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
        <b-switch v-model="mailbox.aiEnabled" :disabled="mailbox.readOnly || busy(index)" data-cy="reply-mailbox-ai">
          {{ $t('replyMailbox.aiToggle') }}
        </b-switch>
        <p class="help mt-2">{{ $t('replyMailbox.aiSetupHelp') }}</p>
      </div>
      <div v-if="mailbox.aiEnabled" data-cy="reply-mailbox-receiving">
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
      </div>

      <div class="reply-card-footer">
        <div class="reply-card-default-toggle">
          <b-checkbox v-model="mailbox.isDefault" :disabled="mailbox.readOnly || busy(index)">{{ $t('replyMailbox.setDefault') }}</b-checkbox>
        </div>
        <div v-if="!mailbox.readOnly" class="buttons mb-0">
          <b-button type="is-primary" icon-left="content-save-outline" :loading="saving === index" :disabled="testing === index"
            data-cy="reply-mailbox-save" @click="saveMailbox(mailbox, index)">
            {{ $t('replyMailbox.saveConfiguration') }}
          </b-button>
          <b-button v-if="mailbox.aiEnabled" type="is-light" icon-left="connection" :loading="testing === index"
            :disabled="!mailbox.id || hasUnsavedChanges(mailbox) || saving === index" data-cy="reply-mailbox-test"
            @click="testMailbox(mailbox, index)">
            {{ $t('replyMailbox.testConnection') }}
          </b-button>
        </div>
        <p v-else class="help mb-0" data-cy="reply-mailbox-readonly-note">{{ $t('replyMailbox.readOnlyNote') }}</p>
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
      enabling: null,
      deleting: null,
      loadedWorkspace: null,
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
      this.load();
    },
  },
  methods: {
    normalize(row) {
      const mailbox = {
        ...blankMailbox(), ...row, password: '', readOnly: row.manageable === false,
      };
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
      this.loadedWorkspace = this.workspaceKey;
      this.$api.getReplyMailboxes(this.apiOrganizationID).then((data) => {
        const rows = Array.isArray(data) ? data : [];
        this.mailboxes = rows.map(this.normalize);
      });
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
      if (mailbox.status === 'active' && !mailbox.aiEnabled) return this.$t('replyMailbox.statusAddressReady');
      return ({
        active: this.$t('replyMailbox.statusActive'),
        retained: this.$t('replyMailbox.statusRetained'),
        disabled: this.$t('replyMailbox.statusDisabled'),
        pending: this.$t('replyMailbox.statusPending'),
      })[mailbox.status] || mailbox.status;
    },

    wire(mailbox, includePassword = true) {
      const data = {
        email: mailbox.email,
        name: mailbox.name,
        is_default: !!mailbox.isDefault,
        ai_enabled: !!mailbox.aiEnabled,
      };
      if (mailbox.aiEnabled) {
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
      return (mailbox.aiEnabled && !!mailbox.password) || JSON.stringify(this.wire(mailbox, false)) !== mailbox.savedConfig;
    },

    busy(index) {
      return this.saving === index || this.testing === index || this.enabling === index || this.deleting === index;
    },

    saveMailbox(mailbox, index) {
      if (!mailbox.email) {
        this.$utils.toast(this.$t('replyMailbox.toastMissingEmail'), 'is-danger');
        return;
      }
      if (mailbox.aiEnabled && !mailbox.hasPassword && !mailbox.password) {
        this.$utils.toast(this.$t('replyMailbox.toastMissingFields'), 'is-danger');
        return;
      }
      this.saving = index;
      const request = mailbox.id
        ? this.$api.updateReplyMailbox(mailbox.id, this.wire(mailbox), this.apiOrganizationID)
        : this.$api.createReplyMailbox(this.wire(mailbox), this.apiOrganizationID);
      request.then((data) => {
        const saved = this.normalize(data);
        this.$set(this.mailboxes, index, saved);
        this.$utils.toast(this.$t('replyMailbox.toastSaved'));
      }).finally(() => {
        this.saving = null;
      });
    },

    testMailbox(mailbox, index) {
      if (!mailbox.aiEnabled || !mailbox.id || this.hasUnsavedChanges(mailbox)) {
        this.$utils.toast(this.$t('replyMailbox.saveBeforeTest'), 'is-warning');
        return;
      }
      this.testing = index;
      this.$api.testReplyMailbox({
        id: mailbox.id,
      }, this.apiOrganizationID).then(() => {
        if (!['disabled', 'retained'].includes(mailbox.status)) this.$set(mailbox, 'status', 'active');
        this.$utils.toast(this.$t('replyMailbox.toastTestSuccess'));
      }).catch((err) => {
        const message = err.response?.data?.message || this.$t('replyMailbox.toastTestFailed');
        this.$utils.toast(message, 'is-danger');
      }).finally(() => {
        this.testing = null;
      });
    },

    disableMailbox(mailbox, index) {
      this.$utils.confirm(this.$t('replyMailbox.confirmDisable'), () => {
        this.$api.deleteReplyMailbox(mailbox.id, this.apiOrganizationID).then(() => {
          this.$set(this.mailboxes, index, { ...mailbox, status: 'disabled', isDefault: false });
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
      this.$utils.confirm(
        this.$t('replyMailbox.confirmDelete', { email: mailbox.email || mailbox.name }),
        () => {
          this.deleting = index;
          this.$api.purgeReplyMailbox(mailbox.id, this.apiOrganizationID).then(() => {
            this.mailboxes.splice(index, 1);
            this.$utils.toast(this.$t('replyMailbox.toastDeleted'));
          }).finally(() => {
            this.deleting = null;
          });
        },
      );
    },

    enableMailbox(mailbox, index) {
      this.enabling = index;
      this.$api.enableReplyMailbox(mailbox.id, this.apiOrganizationID).then((data) => {
        this.$set(this.mailboxes, index, this.normalize(data));
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
@media (max-width: 768px) {
  .reply-mailboxes-header, .reply-card-header, .reply-card-footer { display: block; }
  .reply-mailboxes-header .button { width: 100%; margin-top: .85rem; }
  .reply-card-header .button { margin-top: .75rem; }
  .reply-card-footer .buttons { margin-top: .85rem; }
  .reply-card-footer .button { width: 100%; }
}
</style>
