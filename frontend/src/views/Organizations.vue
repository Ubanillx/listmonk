<template>
  <section class="organizations">
    <header class="columns page-header">
      <div class="column">
        <h1 class="title is-4"><b-icon icon="office-building-outline" size="is-small" />组织</h1>
        <p class="has-text-grey">{{ workspaceLabel }}</p>
      </div>
    </header>

    <div class="columns is-variable is-6">
      <div class="column is-5">
        <section class="mb-6">
          <h2 class="title is-6"><b-icon icon="office-building-outline" size="is-small" />我的组织</h2>
          <b-table :data="organizations" :mobile-cards="false" narrowed>
            <b-table-column v-slot="props" field="name" label="组织">
              <a href="#" @click.prevent="switchWorkspace(props.row)">{{ props.row.name }}</a>
            </b-table-column>
            <b-table-column v-slot="props" field="myRole" label="角色">
              {{ props.row.myRole === 'manager' ? '管理员' : '普通成员' }}
            </b-table-column>
            <template #empty><span class="has-text-grey">尚未加入组织</span></template>
          </b-table>
        </section>

        <section class="mb-6">
          <h2 class="title is-6"><b-icon icon="key-outline" size="is-small" />加入组织</h2>
          <form @submit.prevent="joinOrganization">
            <b-field label="邀请码" label-position="on-border">
              <b-input v-model.trim="joinCode" icon="key-outline" required />
            </b-field>
            <b-button native-type="submit" type="is-primary" icon-left="account-plus-outline">加入</b-button>
          </form>
        </section>

        <section>
          <h2 class="title is-6"><b-icon icon="file-document-edit-outline" size="is-small" />申请创建组织</h2>
          <form @submit.prevent="submitOrganizationRequest">
            <b-field label="组织名称" label-position="on-border">
              <b-input v-model.trim="requestForm.name" maxlength="200" required />
            </b-field>
            <b-field label="说明" label-position="on-border">
              <b-input v-model.trim="requestForm.description" type="textarea" maxlength="2000" />
            </b-field>
            <b-button native-type="submit" icon-left="file-send-outline">提交申请</b-button>
          </form>
        </section>
      </div>

      <div class="column" v-if="workspace.organizationId">
        <section class="mb-6">
          <div class="level mb-3">
            <div class="level-left">
              <h2 class="title is-6 mb-0"><b-icon icon="office-building-outline" size="is-small" />{{ workspace.organizationName }}</h2>
            </div>
            <div class="level-right">
              <b-button type="is-text" @click="leaveCurrentOrganization" icon-left="logout-variant">离开组织</b-button>
            </div>
          </div>
          <p v-if="!isManager" class="has-text-grey">当前为普通成员。组织资源保持所属用户隔离。</p>
        </section>

        <section class="mb-6">
          <h2 class="title is-6">迁移个人列表</h2>
          <p class="has-text-grey mb-3">可将本人个人空间中的列表复制或移动到当前组织；关联订阅者会按当前组织和所属用户合并。</p>
          <b-field label="个人列表" label-position="on-border">
            <b-select v-model="personalListIDs" multiple expanded>
              <option v-for="list in personalLists" :key="list.id" :value="list.id">{{ list.name }}</option>
            </b-select>
          </b-field>
          <div class="buttons">
            <b-button icon-left="content-copy" :disabled="personalListIDs.length === 0"
              @click="migratePersonalLists('copy')">
复制到当前组织
</b-button>
            <b-button type="is-primary" icon-left="folder-move" :disabled="personalListIDs.length === 0"
              @click="migratePersonalLists('move')">
移动到当前组织
</b-button>
          </div>
        </section>

        <section class="mb-6">
          <h2 class="title is-6">迁移个人资源</h2>
          <p class="has-text-grey mb-3">仅显示可迁移的个人私有资源。复制会保留个人资源；移动后资源归入当前组织。</p>

          <b-field label="个人邮件模板" label-position="on-border">
            <b-select v-model="personalTemplateIDs" multiple expanded>
              <option v-for="template in personalTemplates" :key="template.id" :value="template.id">
                {{ template.name }}
              </option>
            </b-select>
          </b-field>
          <div class="buttons mb-5">
            <b-button icon-left="content-copy" :disabled="personalTemplateIDs.length === 0"
              @click="migratePersonalResource('templates', personalTemplateIDs, 'copy')">
              复制到当前组织
            </b-button>
            <b-button type="is-primary" icon-left="folder-move" :disabled="personalTemplateIDs.length === 0"
              @click="migratePersonalResource('templates', personalTemplateIDs, 'move')">
              移动到当前组织
            </b-button>
          </div>

          <b-field label="个人营销活动" label-position="on-border">
            <b-select v-model="personalCampaignIDs" multiple expanded>
              <option v-for="campaign in personalCampaigns" :key="campaign.id" :value="campaign.id">
                {{ campaign.name }} ({{ campaign.status }})
              </option>
            </b-select>
          </b-field>
          <div class="buttons mb-5">
            <b-button icon-left="content-copy" :disabled="personalCampaignIDs.length === 0"
              @click="migratePersonalResource('campaigns', personalCampaignIDs, 'copy')">
              复制到当前组织
            </b-button>
            <b-button type="is-primary" icon-left="folder-move" :disabled="personalCampaignIDs.length === 0"
              @click="migratePersonalResource('campaigns', personalCampaignIDs, 'move')">
              移动到当前组织
            </b-button>
          </div>

          <b-field label="个人媒体文件" label-position="on-border">
            <b-select v-model="personalMediaIDs" multiple expanded>
              <option v-for="media in personalMedia" :key="media.id" :value="media.id">{{ media.filename }}</option>
            </b-select>
          </b-field>
          <div class="buttons">
            <b-button icon-left="content-copy" :disabled="personalMediaIDs.length === 0"
              @click="migratePersonalResource('media', personalMediaIDs, 'copy')">
              复制到当前组织
            </b-button>
            <b-button type="is-primary" icon-left="folder-move" :disabled="personalMediaIDs.length === 0"
              @click="migratePersonalResource('media', personalMediaIDs, 'move')">
              移动到当前组织
            </b-button>
          </div>
        </section>

        <template v-if="isManager">
          <section class="mb-6">
            <h2 class="title is-6"><b-icon icon="account-group-outline" size="is-small" />成员</h2>
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
              <b-table-column v-slot="props" field="role" label="角色">
                <b-select :value="props.row.role" size="is-small" @input="changeMemberRole(props.row, $event)">
                  <option value="member">普通成员</option>
                  <option value="manager">管理员</option>
                </b-select>
              </b-table-column>
              <b-table-column v-slot="props" label="操作" numeric>
                <b-button size="is-small" type="is-text" icon-left="account-remove-outline"
                  @click="removeMember(props.row)">
移除
</b-button>
              </b-table-column>
            </b-table>
          </section>

          <section class="mb-6">
            <h2 class="title is-6"><b-icon icon="key-outline" size="is-small" />邀请码</h2>
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
                {{ props.row.expiresAt ? $utils.niceDate(props.row.expiresAt) : '长期有效' }}
              </b-table-column>
              <b-table-column v-slot="props" label="操作" numeric>
                <b-button v-if="!props.row.revokedAt" size="is-small" type="is-text" icon-left="cancel"
                  @click="revokeInvite(props.row)">
撤销
</b-button>
                <span v-else class="has-text-grey">已撤销</span>
              </b-table-column>
            </b-table>
          </section>

          <section>
            <h2 class="title is-6"><b-icon icon="swap-horizontal" size="is-small" />待转移资源</h2>
            <div class="columns is-vcentered">
              <div class="column is-7">
                <b-select v-model.number="transferTargetUserID" placeholder="选择接收成员" expanded>
                  <option v-for="member in activeMembers" :key="member.userId" :value="member.userId">
                    {{ member.username }}
                  </option>
                </b-select>
              </div>
              <div class="column">
                <b-button :disabled="!transferTargetUserID" icon-left="swap-horizontal"
                  @click="transferPendingResources">
转移待处理资源
</b-button>
              </div>
            </div>
          </section>
        </template>
      </div>
    </div>

    <section v-if="isPlatformAdmin" class="mt-6">
      <h2 class="title is-5"><b-icon icon="file-document-edit-outline" size="is-small" />组织创建申请</h2>
      <b-table :data="requests" :mobile-cards="false" narrowed>
        <b-table-column v-slot="props" field="requestedName" label="组织">{{ props.row.requestedName }}</b-table-column>
        <b-table-column v-slot="props" field="requestedByName" label="申请人">{{ props.row.requestedByName }}</b-table-column>
        <b-table-column v-slot="props" field="description" label="说明">{{ props.row.description }}</b-table-column>
        <b-table-column v-slot="props" label="操作" numeric>
          <b-button size="is-small" type="is-primary" icon-left="check" @click="reviewRequest(props.row, true)">确认</b-button>
          <b-button size="is-small" type="is-text" icon-left="close" @click="reviewRequest(props.row, false)">拒绝</b-button>
        </b-table-column>
      </b-table>

      <h2 class="title is-5 mt-6"><b-icon icon="archive-outline" size="is-small" />组织归档</h2>
      <b-table :data="platformOrganizations" :mobile-cards="false" narrowed>
        <b-table-column v-slot="props" field="name" label="组织">{{ props.row.name }}</b-table-column>
        <b-table-column v-slot="props" field="memberCount" label="成员数">{{ props.row.memberCount }}</b-table-column>
        <b-table-column v-slot="props" field="status" label="状态">
          <b-tag :type="props.row.status === 'archived' ? 'is-warning' : 'is-success'">
            {{ props.row.status === 'archived' ? '已归档' : '正常' }}
          </b-tag>
        </b-table-column>
        <b-table-column v-slot="props" field="archivedAt" label="归档时间">
          {{ props.row.archivedAt ? $utils.niceDate(props.row.archivedAt) : '-' }}
        </b-table-column>
        <b-table-column v-slot="props" label="操作" numeric>
          <b-button v-if="props.row.status !== 'archived'" size="is-small" type="is-text" icon-left="login-variant"
            @click="switchWorkspace(props.row)">
            进入组织
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
import CopyText from '../components/CopyText.vue';

export default Vue.extend({
  components: { CopyText },

  data() {
    return {
      members: [],
      invites: [],
      requests: [],
      platformOrganizations: [],
      isArchiveTransferVisible: false,
      archiveTransferOrganization: null,
      archiveTransferMembers: [],
      archiveTransferTargetUserID: null,
      personalLists: [],
      personalListIDs: [],
      personalTemplates: [],
      personalTemplateIDs: [],
      personalCampaigns: [],
      personalCampaignIDs: [],
      personalMedia: [],
      personalMediaIDs: [],
      joinCode: '',
      newInviteCode: '',
      transferTargetUserID: null,
      requestForm: { name: '', description: '' },
      memberForm: { account: '', role: 'member' },
      inviteForm: { name: '', expiresAt: '', maxUses: null },
    };
  },

  computed: {
    ...mapState(['workspace', 'organizations', 'profile']),

    workspaceLabel() {
      return this.workspace.organizationId ? this.workspace.organizationName : '个人空间';
    },

    isPlatformAdmin() {
      return this.profile.userRole && Number(this.profile.userRole.id) === 1;
    },

    isManager() {
      return this.workspace.organizationId > 0
        && (this.isPlatformAdmin || this.workspace.role === 'manager');
    },

    activeMembers() {
      return this.members.filter((member) => !member.removedAt);
    },
  },

  methods: {
    async refresh() {
      const [organizations, workspace] = await Promise.all([
        this.$api.getMyOrganizations(),
        this.$api.getCurrentWorkspace(),
      ]);
      this.$store.commit('setOrganizations', organizations);
      this.$store.commit('setWorkspace', workspace);
      if (this.isManager) {
        const [members, invites] = await Promise.all([
          this.$api.getOrganizationMembers(),
          this.$api.getOrganizationInvites(),
        ]);
        this.members = members;
        this.invites = invites;
      } else {
        this.members = [];
        this.invites = [];
      }
      if (this.workspace.organizationId) {
        const [personalLists, personalTemplates, personalCampaigns, personalMedia] = await Promise.all([
          this.$api.getPersonalLists(),
          this.$api.getPersonalTemplates(),
          this.$api.getPersonalCampaigns(),
          this.$api.getPersonalMedia(),
        ]);
        this.personalLists = this.personalPrivateResources(personalLists.results);
        this.personalTemplates = this.personalPrivateResources(personalTemplates);
        this.personalCampaigns = this.personalPrivateResources(personalCampaigns.results);
        this.personalMedia = this.personalPrivateResources(personalMedia.results);
      } else {
        this.personalLists = [];
        this.personalListIDs = [];
        this.personalTemplates = [];
        this.personalTemplateIDs = [];
        this.personalCampaigns = [];
        this.personalCampaignIDs = [];
        this.personalMedia = [];
        this.personalMediaIDs = [];
      }
      if (this.isPlatformAdmin) {
        const [requests, platformOrganizations] = await Promise.all([
          this.$api.getOrganizationRequests(),
          this.$api.getOrganizations(true),
        ]);
        this.requests = requests;
        this.platformOrganizations = platformOrganizations;
      } else {
        this.requests = [];
        this.platformOrganizations = [];
      }
    },

    switchWorkspace(organization) {
      this.$store.commit('setWorkspace', organization);
      this.$store.commit('resetWorkspaceModels');
      this.$router.go(0);
    },

    async joinOrganization() {
      const organization = await this.$api.joinOrganization({ code: this.joinCode });
      this.joinCode = '';
      this.$store.commit('setWorkspace', organization);
      this.$store.commit('resetWorkspaceModels');
      this.$router.go(0);
    },

    async submitOrganizationRequest() {
      await this.$api.createOrganizationRequest(this.requestForm);
      this.requestForm = { name: '', description: '' };
      this.$utils.toast('组织创建申请已提交');
    },

    leaveCurrentOrganization() {
      this.$utils.confirm('离开后，当前组织资源会进入待转移状态。', async () => {
        await this.$api.leaveOrganization();
        this.$store.commit('setWorkspace', { organizationId: 0, personal: true });
        this.$store.commit('resetWorkspaceModels');
        this.$router.go(0);
      });
    },

    async addMember() {
      await this.$api.addOrganizationMember(this.memberForm);
      this.memberForm = { account: '', role: 'member' };
      await this.refresh();
    },

    async changeMemberRole(member, role) {
      await this.$api.updateOrganizationMember(member.userId, { role });
      await this.refresh();
    },

    removeMember(member) {
      this.$utils.confirm(`移除 ${member.username} 后，其组织资源将转为待转移。`, async () => {
        await this.$api.removeOrganizationMember(member.userId);
        await this.refresh();
      });
    },

    async createInvite() {
      const expiresAt = this.inviteForm.expiresAt
        ? new Date(this.inviteForm.expiresAt).toISOString() : '';
      const maxUses = this.inviteForm.maxUses > 0 ? this.inviteForm.maxUses : null;
      const invite = await this.$api.createOrganizationInvite({
        name: this.inviteForm.name,
        expires_at: expiresAt,
        max_uses: maxUses,
      });
      this.newInviteCode = invite.code;
      this.inviteForm = { name: '', expiresAt: '', maxUses: null };
      await this.refresh();
    },

    async revokeInvite(invite) {
      await this.$api.revokeOrganizationInvite(invite.id);
      await this.refresh();
    },

    transferPendingResources() {
      this.$utils.confirm('待转移资源及关联订阅者将转移给所选成员。', async () => {
        await this.$api.transferPendingOrganizationResources({ target_user_id: this.transferTargetUserID });
        this.transferTargetUserID = null;
        await this.refresh();
      });
    },

    migratePersonalLists(mode) {
      const action = mode === 'move' ? '移动' : '复制';
      this.$utils.confirm(`确认${action}所选个人列表到当前组织？`, async () => {
        await this.$api.migratePersonalLists({
          list_ids: this.personalListIDs,
          mode,
          target_organization_id: this.workspace.organizationId,
        });
        this.personalListIDs = [];
        await this.refresh();
        this.$root.$emit('page.refresh');
      });
    },

    personalPrivateResources(resources) {
      return (Array.isArray(resources) ? resources : [])
        .filter((resource) => resource.visibility === 'private');
    },

    migratePersonalResource(resource, ids, mode) {
      const labels = {
        templates: '邮件模板',
        campaigns: '营销活动',
        media: '媒体文件',
      };
      const selectedKey = {
        templates: 'personalTemplateIDs',
        campaigns: 'personalCampaignIDs',
        media: 'personalMediaIDs',
      }[resource];
      const action = mode === 'move' ? '移动' : '复制';
      this.$utils.confirm(`确认${action}所选${labels[resource]}到当前组织？`, async () => {
        await this.$api.migratePersonalResources({
          resource,
          ids,
          mode,
          target_organization_id: this.workspace.organizationId,
        });
        this[selectedKey] = [];
        await this.refresh();
        this.$root.$emit('page.refresh');
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

  mounted() {
    this.refresh();
  },
});
</script>
