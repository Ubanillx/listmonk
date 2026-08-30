<template>
  <section class="reply-mailboxes mt-6">
    <div class="reply-mailboxes-header level mb-5">
      <div>
        <h2 class="title is-5 mb-1"><b-icon icon="email-arrow-left-outline" size="is-small" /> 客户回信邮箱</h2>
        <p class="help">使用企业邮箱接收客户直接回复。营销活动会把 Reply-To 设置为这里选定的邮箱，个人空间与组织空间分别配置。</p>
      </div>
      <b-button type="is-primary" icon-left="plus" @click="addMailbox">新增回信邮箱</b-button>
    </div>

    <div v-if="mailboxes.length === 0" class="notification is-light reply-empty-state">
      <b-icon icon="email-off-outline" size="is-small" />
      <span>尚未配置回信邮箱。未配置并验证邮箱时，活动不能排期或发送。</span>
    </div>

    <div v-for="(mailbox, index) in mailboxes" :key="mailbox.id || `new-${index}`" class="box reply-mailbox-card">
      <div class="reply-card-header">
        <div>
          <div class="reply-card-title">
            {{ mailbox.name || mailbox.email || `回信邮箱 #${index + 1}` }}
            <b-tag v-if="mailbox.id" rounded size="is-small" :type="statusType(mailbox.status)">
              {{ statusLabel(mailbox.status) }}
            </b-tag>
            <b-tag v-if="mailbox.isDefault" type="is-info" rounded size="is-small">默认</b-tag>
          </div>
          <p class="reply-card-subtitle">{{ mailbox.email || '填写客户回信邮箱地址' }}</p>
        </div>
        <b-button v-if="mailbox.id" type="is-danger" outlined size="is-small" icon-left="trash-can-outline"
          @click="disableMailbox(mailbox, index)">
          停用
        </b-button>
      </div>

      <div class="columns is-multiline reply-grid">
        <div class="column is-6">
          <b-field label="邮箱地址" label-position="on-border">
            <b-input v-model.trim="mailbox.email" type="email" required placeholder="employee@company.example" />
          </b-field>
        </div>
        <div class="column is-6">
          <b-field label="显示名称" label-position="on-border">
            <b-input v-model="mailbox.name" maxlength="100" placeholder="客户回信" />
          </b-field>
        </div>
        <div class="column is-6">
          <b-field label="登录账号" label-position="on-border" message="通常与邮箱地址相同">
            <b-input v-model.trim="mailbox.username" placeholder="employee@company.example" />
          </b-field>
        </div>
        <div class="column is-6">
          <b-field label="密码" label-position="on-border"
            message="部分邮箱服务商要求填写客户端授权码；只保存于服务器用于收信验证，不会回显">
            <b-input v-model="mailbox.password" type="password" password-reveal
              :placeholder="mailbox.id ? '已保存，留空表示不修改' : '输入邮箱密码或客户端授权码'" />
          </b-field>
        </div>
        <div class="column is-6">
          <b-field label="IMAP 服务器" label-position="on-border">
            <b-input v-model.trim="mailbox.imapHost" placeholder="imap.example.com" />
          </b-field>
        </div>
        <div class="column is-3">
          <b-field label="端口" label-position="on-border">
            <b-numberinput v-model="mailbox.imapPort" min="1" max="65535" controls-position="compact" />
          </b-field>
        </div>
        <div class="column is-3">
          <b-field label="文件夹" label-position="on-border">
            <b-input v-model.trim="mailbox.folder" placeholder="INBOX" />
          </b-field>
        </div>
      </div>

      <div class="reply-card-footer">
        <b-checkbox v-model="mailbox.isDefault">设为当前空间默认回信邮箱</b-checkbox>
        <div class="buttons mb-0">
          <b-button type="is-light" icon-left="connection" :loading="testing === index" @click="testMailbox(mailbox, index)">
            测试连接
          </b-button>
          <b-button type="is-primary" icon-left="content-save-outline" :loading="saving === index" @click="saveMailbox(mailbox, index)">
            保存
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
    verifiedAt: null,
    lastSyncAt: null,
    lastSyncError: '',
    forwardCount: 0,
  };
}

export default Vue.extend({
  name: 'ReplyMailboxSettings',

  data() {
    return {
      mailboxes: [],
      saving: null,
      testing: null,
      loadedWorkspace: null,
    };
  },

  computed: {
    ...mapState(['workspace']),
    workspaceKey() {
      return Number(this.workspace && this.workspace.organizationId) || 0;
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
      this.$api.getReplyMailboxes().then((data) => {
        this.mailboxes = (Array.isArray(data) ? data : []).map(this.normalize);
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
        active: '已验证', retained: '离组保留', disabled: '已停用', pending: '待验证',
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
      };
      if (includePassword || mailbox.password) data.password = mailbox.password;
      return data;
    },

    saveMailbox(mailbox, index) {
      if (!mailbox.email || (!mailbox.id && !mailbox.password)) {
        this.$utils.toast('请填写邮箱地址和密码或客户端授权码', 'is-danger');
        return;
      }
      this.saving = index;
      const request = mailbox.id
        ? this.$api.updateReplyMailbox(mailbox.id, this.wire(mailbox))
        : this.$api.createReplyMailbox(this.wire(mailbox));
      request.then((data) => {
        const saved = this.normalize(data);
        this.$set(this.mailboxes, index, saved);
        this.$utils.toast('回信邮箱已保存');
      }).finally(() => {
        this.saving = null;
      });
    },

    testMailbox(mailbox, index) {
      if (!mailbox.email || !mailbox.password) {
        this.$utils.toast('测试连接需要邮箱地址和密码或客户端授权码', 'is-danger');
        return;
      }
      this.testing = index;
      this.$api.testReplyMailbox({
        ...this.wire(mailbox),
        id: mailbox.id || 0,
      }).then(() => {
        this.$set(mailbox, 'status', 'active');
        this.$utils.toast('邮箱连接成功');
      }).catch((err) => {
        const message = err.response?.data?.message || '邮箱连接失败';
        this.$utils.toast(message, 'is-danger');
      }).finally(() => {
        this.testing = null;
      });
    },

    disableMailbox(mailbox, index) {
      this.$utils.confirm('停用后，新的营销活动不能使用此回信邮箱。', () => {
        this.$api.deleteReplyMailbox(mailbox.id).then(() => {
          this.$set(this.mailboxes, index, { ...mailbox, status: 'disabled', isDefault: false });
          this.$utils.toast('回信邮箱已停用');
        });
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
@media (max-width: 768px) {
  .reply-mailboxes-header, .reply-card-header, .reply-card-footer { display: block; }
  .reply-mailboxes-header .button { width: 100%; margin-top: .85rem; }
  .reply-card-header .button { margin-top: .75rem; }
  .reply-card-footer .buttons { margin-top: .85rem; }
  .reply-card-footer .button { width: 100%; }
}
</style>
