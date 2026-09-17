<template>
  <section class="reply-mailboxes mt-6">
    <div class="reply-mailboxes-header level mb-5">
      <div>
        <h2 class="title is-5 mb-1"><b-icon icon="email-arrow-left-outline" size="is-small" /> {{ $t('replyMailbox.title') }}</h2>
        <p class="help">{{ organizationId ? $t('replyMailbox.organizationHelp') : $t('replyMailbox.help') }}</p>
      </div>
      <b-button type="is-primary" icon-left="plus" @click="addMailbox">{{ $t('replyMailbox.add') }}</b-button>
    </div>

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
              {{ statusLabel(mailbox.status) }}
            </b-tag>
            <b-tag v-if="mailbox.isDefault" type="is-info" rounded size="is-small">{{ $t('replyMailbox.defaultTag') }}</b-tag>
          </div>
          <p class="reply-card-subtitle">{{ mailbox.email || $t('replyMailbox.noEmail') }}</p>
        </div>
        <b-button v-if="mailbox.id && mailbox.status !== 'disabled'" type="is-danger" outlined size="is-small" icon-left="trash-can-outline"
          @click="disableMailbox(mailbox, index)">
          {{ $t('replyMailbox.disable') }}
        </b-button>
        <b-button v-else-if="mailbox.id" type="is-primary" outlined size="is-small" icon-left="play-circle-outline"
          :loading="enabling === index" @click="enableMailbox(mailbox, index)">
          {{ $t('replyMailbox.enable') }}
        </b-button>
      </div>

      <div class="columns is-multiline reply-grid">
        <div class="column is-6">
          <b-field :label="$t('replyMailbox.emailLabel')" label-position="on-border">
            <b-input v-model.trim="mailbox.email" type="email" required placeholder="employee@company.example" />
          </b-field>
        </div>
        <div class="column is-6">
          <b-field :label="$t('replyMailbox.nameLabel')" label-position="on-border">
            <b-input v-model="mailbox.name" maxlength="100" :placeholder="$t('replyMailbox.nameLabel')" />
          </b-field>
        </div>
        <div class="column is-6">
          <b-field :label="$t('replyMailbox.usernameLabel')" label-position="on-border" :message="$t('replyMailbox.usernameHelp')">
            <b-input v-model.trim="mailbox.username" placeholder="employee@company.example" />
          </b-field>
        </div>
        <div class="column is-6">
          <b-field :label="$t('replyMailbox.passwordLabel')" label-position="on-border"
            :message="$t('replyMailbox.passwordHelp')">
            <b-input v-model="mailbox.password" type="password" password-reveal
              :placeholder="mailbox.id ? $t('replyMailbox.passwordSaved') : $t('replyMailbox.passwordPlaceholder')" />
          </b-field>
        </div>
        <div class="column is-6">
          <b-field :label="$t('replyMailbox.imapHostLabel')" label-position="on-border">
            <b-input v-model.trim="mailbox.imapHost" placeholder="imap.example.com" />
          </b-field>
        </div>
        <div class="column is-3">
          <b-field :label="$t('replyMailbox.portLabel')" label-position="on-border">
            <b-numberinput v-model="mailbox.imapPort" min="1" max="65535" controls-position="compact" />
          </b-field>
        </div>
        <div class="column is-3">
          <b-field :label="$t('replyMailbox.folderLabel')" label-position="on-border">
            <b-input v-model.trim="mailbox.folder" placeholder="INBOX" />
          </b-field>
        </div>
      </div>

      <div class="reply-card-footer">
        <div class="reply-card-ai-toggle">
          <b-checkbox v-model="mailbox.aiEnabled">
            {{ $t('replyMailbox.aiToggle') }}
          </b-checkbox>
          <p class="help" v-if="mailbox.aiEnabled">
            {{ $t('replyMailbox.aiHelp') }}
          </p>
        </div>
        <div class="reply-card-default-toggle">
          <b-checkbox v-model="mailbox.isDefault">{{ $t('replyMailbox.setDefault') }}</b-checkbox>
        </div>
        <div class="buttons mb-0">
          <b-button type="is-light" icon-left="connection" :loading="testing === index" @click="testMailbox(mailbox, index)">
            {{ $t('replyMailbox.testConnection') }}
          </b-button>
          <b-button type="is-primary" icon-left="content-save-outline" :loading="saving === index" @click="saveMailbox(mailbox, index)">
            {{ $t('globals.buttons.save') }}
          </b-button>
        </div>
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
      return { ...blankMailbox(), ...row, password: '' };
    },

    load() {
      this.loadedWorkspace = this.workspaceKey;
      this.$api.getReplyMailboxes(this.apiOrganizationID).then((data) => {
        const rows = Array.isArray(data) ? data : [];
        // The listing may include organization reply mailboxes the caller can
        // select in a campaign but does not own. The manage surface only shows
        // mailboxes the caller actually owns and may edit, disable or test.
        this.mailboxes = rows.filter((row) => row.manageable !== false).map(this.normalize);
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

    statusLabel(status) {
      return ({
        active: this.$t('replyMailbox.statusActive'),
        retained: this.$t('replyMailbox.statusRetained'),
        disabled: this.$t('replyMailbox.statusDisabled'),
        pending: this.$t('replyMailbox.statusPending'),
      })[status] || status;
    },

    wire(mailbox, includePassword = true) {
      const data = {
        email: mailbox.email,
        name: mailbox.name,
        username: mailbox.username,
        imap_host: mailbox.imapHost,
        imap_port: Number(mailbox.imapPort) || 993,
        imap_tls: mailbox.imapTls !== false,
        folder: mailbox.folder || 'INBOX',
        is_default: !!mailbox.isDefault,
        ai_enabled: !!mailbox.aiEnabled,
      };
      if (includePassword || mailbox.password) data.password = mailbox.password;
      return data;
    },

    saveMailbox(mailbox, index) {
      if (!mailbox.email || (!mailbox.id && !mailbox.password)) {
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
      if (!mailbox.email || !mailbox.password) {
        this.$utils.toast(this.$t('replyMailbox.toastTestMissing'), 'is-danger');
        return;
      }
      this.testing = index;
      this.$api.testReplyMailbox({
        ...this.wire(mailbox),
        id: mailbox.id || 0,
      }, this.apiOrganizationID).then(() => {
        this.$set(mailbox, 'status', 'active');
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
.reply-mailbox-card { padding: 0; overflow: hidden; border: 1px solid #e5e9f0; border-radius: 10px; box-shadow: 0 2px 8px rgba(29, 41, 57, .05); }
.reply-mailbox-card + .reply-mailbox-card { margin-top: 1.25rem; }
.reply-card-header { display: flex; align-items: center; justify-content: space-between; gap: 1rem; padding: 1.1rem 1.25rem; background: #f8fafc; border-bottom: 1px solid #edf0f4; }
.reply-card-title { display: flex; align-items: center; flex-wrap: wrap; gap: .5rem; font-size: 1.05rem; font-weight: 600; color: #273142; }
.reply-card-subtitle { margin-top: .25rem; color: #718096; font-size: .85rem; word-break: break-all; }
.reply-grid { padding: 1.25rem 1.25rem .5rem; margin: 0; }
.reply-grid > .column { padding: .35rem; }
.reply-card-footer { display: flex; align-items: center; justify-content: space-between; gap: 1rem; padding: 1rem 1.25rem; border-top: 1px solid #edf0f4; }
.reply-card-ai-toggle { flex: 1; min-width: 240px; }
.reply-card-ai-toggle .help { margin-top: .3rem; margin-bottom: 0; }
.reply-card-default-toggle { flex-shrink: 0; }
@media (max-width: 768px) {
  .reply-mailboxes-header, .reply-card-header, .reply-card-footer { display: block; }
  .reply-mailboxes-header .button { width: 100%; margin-top: .85rem; }
  .reply-card-header .button { margin-top: .75rem; }
  .reply-card-footer .buttons { margin-top: .85rem; }
  .reply-card-footer .button { width: 100%; }
}
</style>
