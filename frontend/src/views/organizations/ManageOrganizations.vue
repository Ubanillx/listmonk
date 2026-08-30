<template>
  <section class="organizations">
    <header class="columns page-header">
      <div class="column">
        <h1 class="title is-4"><b-icon icon="account-cog-outline" size="is-small" />管理组织</h1>
      </div>
    </header>

    <b-field v-if="manageableOrganizations.length" label="管理的组织" label-position="on-border" class="section-mini mb-5">
      <b-select v-model.number="selectedOrganizationID" expanded>
        <option v-for="organization in manageableOrganizations" :key="organization.id" :value="organization.id">
          {{ organization.name }}
        </option>
      </b-select>
    </b-field>

    <b-notification v-if="!selectedOrganizationID && !isPlatformAdmin" type="is-light" :closable="false">
      你目前不是任何组织的管理员。
    </b-notification>

    <b-tabs v-if="selectedOrganizationID || isPlatformAdmin" type="is-boxed" :animated="false" v-model="activeTab">
      <b-tab-item v-if="selectedOrganizationID" label="成员" icon="account-group-outline">
        <section class="wrap">
          <form class="columns is-multiline" @submit.prevent="addMember">
            <div class="column is-6">
              <b-field label="已注册账号或邮箱" label-position="on-border">
                <b-input v-model.trim="memberForm.account" required />
              </b-field>
            </div>
            <div class="column is-3">
              <b-field label="组织角色" label-position="on-border">
                <b-select v-model="memberForm.role" expanded>
                  <option value="member">普通成员</option>
                  <option value="manager">管理员</option>
                </b-select>
              </b-field>
            </div>
            <div class="column is-3 is-flex is-align-items-flex-end">
              <b-button native-type="submit" type="is-primary" expanded icon-left="account-plus-outline">添加</b-button>
            </div>
          </form>

          <b-table :data="activeMembers" :mobile-cards="false" narrowed>
            <b-table-column v-slot="props" field="username" label="账号">
              <strong>{{ props.row.username }}</strong>
              <span v-if="props.row.name" class="has-text-grey"> {{ props.row.name }}</span>
            </b-table-column>
            <b-table-column v-slot="props" field="email" label="邮箱">{{ props.row.email }}</b-table-column>
            <b-table-column v-slot="props" field="role" label="角色">
              <b-select :value="props.row.role" size="is-small" @input="changeMemberRole(props.row, $event)">
                <option value="member">普通成员</option>
                <option value="manager">管理员</option>
              </b-select>
            </b-table-column>
            <b-table-column v-slot="props" label="操作" numeric>
              <b-button size="is-small" type="is-text" icon-left="account-remove-outline" @click="removeMember(props.row)">
                移除
              </b-button>
            </b-table-column>
            <template #empty><span class="has-text-grey">暂无成员</span></template>
          </b-table>
        </section>
      </b-tab-item>

      <b-tab-item v-if="selectedOrganizationID" label="邀请码" icon="key-outline">
        <section class="wrap">
          <form class="columns is-multiline" @submit.prevent="createInvite">
            <div class="column is-4">
              <b-field label="名称" label-position="on-border"><b-input v-model.trim="inviteForm.name" /></b-field>
            </div>
            <div class="column is-4">
              <b-field label="有效期" label-position="on-border">
                <b-input v-model="inviteForm.expiresAt" type="datetime-local" />
              </b-field>
            </div>
            <div class="column is-2">
              <b-field label="最大使用次数" label-position="on-border">
                <b-input v-model.number="inviteForm.maxUses" type="number" min="1" />
              </b-field>
            </div>
            <div class="column is-2 is-flex is-align-items-flex-end">
              <b-button native-type="submit" type="is-primary" expanded icon-left="key-plus">创建</b-button>
            </div>
          </form>

          <b-notification v-if="newInviteCode" type="is-success" :closable="false">
            <copy-text :text="newInviteCode" />
          </b-notification>

          <b-table :data="invites" :mobile-cards="false" narrowed>
            <b-table-column v-slot="props" field="name" label="名称">{{ props.row.name || '邀请码' }}</b-table-column>
            <b-table-column v-slot="props" field="useCount" label="使用次数">
              {{ props.row.useCount }}<span v-if="props.row.maxUses"> / {{ props.row.maxUses }}</span>
            </b-table-column>
            <b-table-column v-slot="props" field="expiresAt" label="有效期">
              {{ props.row.expiresAt ? $utils.niceDate(props.row.expiresAt, true) : '长期有效' }}
            </b-table-column>
            <b-table-column v-slot="props" label="操作" numeric>
              <b-button v-if="!props.row.revokedAt" size="is-small" type="is-text" icon-left="cancel" @click="revokeInvite(props.row)">
                撤销
              </b-button>
              <span v-else class="has-text-grey">已撤销</span>
            </b-table-column>
            <template #empty><span class="has-text-grey">暂无邀请码</span></template>
          </b-table>
        </section>
      </b-tab-item>

      <b-tab-item v-if="selectedOrganizationID" label="待转移资源" icon="swap-horizontal">
        <section class="wrap">
          <p class="has-text-grey mb-4">成员离开后留下的组织资源会保留在组织中，仅组织管理员可将其转移给当前成员。</p>
          <div class="columns is-vcentered">
            <div class="column is-7">
              <b-field label="接收成员" label-position="on-border">
                <b-select v-model.number="transferTargetUserID" placeholder="选择接收成员" expanded>
                  <option :value="null">请选择成员</option>
                  <option v-for="member in activeMembers" :key="member.userId" :value="member.userId">
                    {{ member.username }}
                  </option>
                </b-select>
              </b-field>
            </div>
            <div class="column is-3 is-flex is-align-items-flex-end">
              <b-button :disabled="!transferTargetUserID" icon-left="swap-horizontal" @click="transferPendingResources">
                转移资源
              </b-button>
            </div>
          </div>
        </section>
      </b-tab-item>

      <b-tab-item v-if="selectedOrganizationID" label="客户回信转发" icon="email-arrow-left-outline">
        <section class="wrap">
          <p class="has-text-grey mb-4">成员离组后，原回信邮箱仍会继续收信。这里可以暂停或恢复转发到组织管理员工作邮箱。</p>
          <b-table :data="replyForwardRules" :mobile-cards="false" narrowed>
            <b-table-column v-slot="props" field="sourceEmail" label="成员邮箱">
              <strong>{{ props.row.sourceEmail || props.row.sourceName || '-' }}</strong>
            </b-table-column>
            <b-table-column v-slot="props" field="mailboxEmail" label="回信邮箱">{{ props.row.mailboxEmail }}</b-table-column>
            <b-table-column v-slot="props" field="targetEmail" label="转发目标">{{ props.row.targetEmail }}</b-table-column>
            <b-table-column v-slot="props" field="status" label="状态">
              <b-tag :type="props.row.status === 'active' ? 'is-success' : 'is-light'">
                {{ props.row.status === 'active' ? '转发中' : '已停用' }}
              </b-tag>
            </b-table-column>
            <b-table-column v-slot="props" label="操作" numeric>
              <b-button size="is-small" type="is-text" :icon-left="props.row.status === 'active' ? 'pause-circle-outline' : 'play-circle-outline'"
                @click="toggleReplyForwardRule(props.row)">
                {{ props.row.status === 'active' ? '停用' : '恢复' }}
              </b-button>
            </b-table-column>
            <template #empty><span class="has-text-grey">暂无离组成员回信转发规则</span></template>
          </b-table>
        </section>
      </b-tab-item>

      <b-tab-item v-if="isPlatformAdmin" label="平台管理" icon="shield-crown-outline">
        <section class="mb-6">
          <h2 class="title is-5"><b-icon icon="file-document-edit-outline" size="is-small" />组织创建申请</h2>
          <b-table :data="requests" :mobile-cards="false" narrowed>
            <b-table-column v-slot="props" field="requestedName" label="组织">{{ props.row.requestedName }}</b-table-column>
            <b-table-column v-slot="props" field="requestedByName" label="申请人">{{ props.row.requestedByName }}</b-table-column>
            <b-table-column v-slot="props" field="description" label="说明">{{ props.row.description }}</b-table-column>
            <b-table-column v-slot="props" field="createdAt" label="申请时间">{{ $utils.niceDate(props.row.createdAt, true) }}</b-table-column>
            <b-table-column v-slot="props" label="操作" numeric>
              <b-button size="is-small" type="is-primary" icon-left="check" @click="reviewRequest(props.row, true)">确认</b-button>
              <b-button size="is-small" type="is-text" icon-left="close" @click="reviewRequest(props.row, false)">拒绝</b-button>
            </b-table-column>
            <template #empty><span class="has-text-grey">没有待处理申请</span></template>
          </b-table>
        </section>

        <section>
          <h2 class="title is-5"><b-icon icon="archive-outline" size="is-small" />组织归档</h2>
          <b-table :data="platformOrganizations" :mobile-cards="false" narrowed>
            <b-table-column v-slot="props" field="name" label="组织">
              <strong>{{ props.row.name }}</strong>
              <p v-if="props.row.description" class="has-text-grey is-size-7">{{ props.row.description }}</p>
            </b-table-column>
            <b-table-column v-slot="props" field="memberCount" label="成员数" numeric>{{ props.row.memberCount }}</b-table-column>
            <b-table-column v-slot="props" field="status" label="状态">
              <b-tag :type="props.row.status === 'archived' ? 'is-warning' : 'is-success'">
                {{ props.row.status === 'archived' ? '已归档' : '正常' }}
              </b-tag>
            </b-table-column>
            <b-table-column v-slot="props" field="archivedAt" label="归档时间">
              {{ props.row.archivedAt ? $utils.niceDate(props.row.archivedAt, true) : '-' }}
            </b-table-column>
            <b-table-column v-slot="props" label="操作" numeric>
              <b-button v-if="props.row.status !== 'archived'" size="is-small" type="is-text" icon-left="account-cog-outline"
                @click="selectOrganization(props.row)">
                管理
              </b-button>
              <b-button v-if="props.row.status !== 'archived'" size="is-small" type="is-text" icon-left="archive-outline"
                @click="archivePlatformOrganization(props.row)">
                归档
              </b-button>
              <b-button v-if="props.row.status === 'archived'" size="is-small" type="is-text" icon-left="swap-horizontal"
                @click="openArchiveTransfer(props.row)">
                转移资源
              </b-button>
              <b-button v-if="props.row.status === 'archived'" size="is-small" type="is-text" icon-left="delete-forever-outline"
                @click="purgePlatformOrganization(props.row)">
                永久删除
              </b-button>
            </b-table-column>
          </b-table>
        </section>
      </b-tab-item>
    </b-tabs>

    <b-modal scroll="keep" :aria-modal="true" :active.sync="isArchiveTransferVisible" :width="520">
      <div class="modal-card content" style="width: auto">
        <header class="modal-card-head"><h4><b-icon icon="swap-horizontal" size="is-small" />转移归档资源</h4></header>
        <section class="modal-card-body">
          <p v-if="archiveTransferOrganization" class="mb-4">{{ archiveTransferOrganization.name }}</p>
          <b-field label="接收成员" label-position="on-border">
            <b-select v-model.number="archiveTransferTargetUserID" expanded>
              <option :value="null">请选择成员</option>
              <option v-for="member in archiveTransferMembers" :key="member.userId" :value="member.userId">
                {{ member.username }}
              </option>
            </b-select>
          </b-field>
          <p class="has-text-grey is-size-7">资源将转移到该成员的个人空间，组织共享资源会改为个人私有。</p>
        </section>
        <footer class="modal-card-foot has-text-right">
          <b-button @click="isArchiveTransferVisible = false">{{ $t('globals.buttons.close') }}</b-button>
          <b-button type="is-primary" icon-left="swap-horizontal" :disabled="!archiveTransferTargetUserID" @click="transferArchivedResources">
            转移
          </b-button>
        </footer>
      </div>
    </b-modal>
  </section>
</template>

<script>
import Vue from 'vue';
import { mapState } from 'vuex';
import CopyText from '../../components/CopyText.vue';

export default Vue.extend({
  components: { CopyText },

  data() {
    return {
      activeTab: 0,
      selectedOrganizationID: null,
      members: [],
      invites: [],
      replyForwardRules: [],
      requests: [],
      platformOrganizations: [],
      newInviteCode: '',
      transferTargetUserID: null,
      memberForm: { account: '', role: 'member' },
      inviteForm: { name: '', expiresAt: '', maxUses: null },
      isArchiveTransferVisible: false,
      archiveTransferOrganization: null,
      archiveTransferMembers: [],
      archiveTransferTargetUserID: null,
    };
  },

  computed: {
    ...mapState(['organizations', 'profile']),

    isPlatformAdmin() {
      return this.profile.userRole && Number(this.profile.userRole.id) === 1;
    },

    manageableOrganizations() {
      if (this.isPlatformAdmin) {
        return this.platformOrganizations.filter((organization) => organization.status === 'active');
      }
      return this.organizations.filter((organization) => organization.myRole === 'manager');
    },

    activeMembers() {
      return this.members.filter((member) => !member.removedAt);
    },
  },

  watch: {
    selectedOrganizationID() {
      this.newInviteCode = '';
      this.refreshSelectedOrganization();
    },
  },

  methods: {
    async refresh() {
      const organizations = await this.$api.getMyOrganizations();
      this.$store.commit('setOrganizations', organizations);
      if (this.isPlatformAdmin) {
        const [requests, platformOrganizations] = await Promise.all([
          this.$api.getOrganizationRequests(),
          this.$api.getOrganizations(true),
        ]);
        this.requests = requests;
        this.platformOrganizations = platformOrganizations;
      }
      this.ensureSelectedOrganization();
      await this.refreshSelectedOrganization();
    },

    ensureSelectedOrganization() {
      const selectedID = Number(this.selectedOrganizationID) || 0;
      if (this.manageableOrganizations.some((organization) => organization.id === selectedID)) {
        return;
      }
      this.selectedOrganizationID = (this.manageableOrganizations[0] || {}).id || null;
    },

    async refreshSelectedOrganization() {
      if (!this.selectedOrganizationID) {
        this.members = [];
        this.invites = [];
        this.replyForwardRules = [];
        this.transferTargetUserID = null;
        return;
      }
      const [members, invites] = await Promise.all([
        this.$api.getOrganizationMembers(this.selectedOrganizationID),
        this.$api.getOrganizationInvites(this.selectedOrganizationID),
      ]);
      this.members = members;
      this.invites = invites;
      this.replyForwardRules = await this.$api.getReplyForwardRules(this.selectedOrganizationID);
      this.transferTargetUserID = null;
    },

    selectOrganization(organization) {
      this.selectedOrganizationID = organization.id;
      this.activeTab = 0;
    },

    async addMember() {
      await this.$api.addOrganizationMember(this.memberForm, this.selectedOrganizationID);
      this.memberForm = { account: '', role: 'member' };
      await this.refreshSelectedOrganization();
    },

    async changeMemberRole(member, role) {
      await this.$api.updateOrganizationMember(member.userId, { role }, this.selectedOrganizationID);
      await this.refresh();
    },

    removeMember(member) {
      this.$utils.confirm(`移除 ${member.username} 后，其组织资源将转为待转移。`, async () => {
        await this.$api.removeOrganizationMember(member.userId, this.selectedOrganizationID);
        await this.refresh();
      });
    },

    async createInvite() {
      const expiresAt = this.inviteForm.expiresAt ? new Date(this.inviteForm.expiresAt).toISOString() : '';
      const maxUses = this.inviteForm.maxUses > 0 ? this.inviteForm.maxUses : null;
      const invite = await this.$api.createOrganizationInvite({
        name: this.inviteForm.name,
        expires_at: expiresAt,
        max_uses: maxUses,
      }, this.selectedOrganizationID);
      this.newInviteCode = invite.code;
      this.inviteForm = { name: '', expiresAt: '', maxUses: null };
      await this.refreshSelectedOrganization();
    },

    async revokeInvite(invite) {
      await this.$api.revokeOrganizationInvite(invite.id, this.selectedOrganizationID);
      await this.refreshSelectedOrganization();
    },

    async toggleReplyForwardRule(rule) {
      const status = rule.status === 'active' ? 'disabled' : 'active';
      await this.$api.updateReplyForwardRule(rule.id, { status }, this.selectedOrganizationID);
      this.replyForwardRules = await this.$api.getReplyForwardRules(this.selectedOrganizationID);
    },

    transferPendingResources() {
      this.$utils.confirm('待转移资源及关联订阅者将转移给所选成员。', async () => {
        await this.$api.transferPendingOrganizationResources({ target_user_id: this.transferTargetUserID }, this.selectedOrganizationID);
        this.transferTargetUserID = null;
        await this.refreshSelectedOrganization();
      });
    },

    async reviewRequest(request, approve) {
      await this.$api.reviewOrganizationRequest(request.id, { approve, note: '' });
      await this.refresh();
    },

    archivePlatformOrganization(organization) {
      this.$utils.confirm(`确认归档组织“${organization.name}”？排期活动会转为草稿，正在发送的活动会立即停止。`, async () => {
        await this.$api.archiveOrganization(organization.id);
        await this.refresh();
      });
    },

    purgePlatformOrganization(organization) {
      this.$utils.confirm(`确认永久删除组织“${organization.name}”？只有资源已全部转移或清理后才能完成。`, async () => {
        await this.$api.purgeArchivedOrganization(organization.id);
        await this.refresh();
      });
    },

    async openArchiveTransfer(organization) {
      this.archiveTransferOrganization = organization;
      this.archiveTransferTargetUserID = null;
      const members = await this.$api.getOrganizationMembersByID(organization.id);
      this.archiveTransferMembers = members.filter((member) => !member.removedAt);
      this.isArchiveTransferVisible = true;
    },

    async transferArchivedResources() {
      if (!this.archiveTransferOrganization || !this.archiveTransferTargetUserID) {
        return;
      }
      await this.$api.transferArchivedOrganizationResources(this.archiveTransferOrganization.id, {
        target_user_id: this.archiveTransferTargetUserID,
      });
      this.isArchiveTransferVisible = false;
      this.archiveTransferOrganization = null;
      this.archiveTransferMembers = [];
      this.archiveTransferTargetUserID = null;
      await this.refresh();
    },
  },

  created() {
    this.$root.$on('page.refresh', this.refresh);
  },

  destroyed() {
    this.$root.$off('page.refresh', this.refresh);
  },

  mounted() {
    this.refresh();
  },
});
</script>
