<template>
  <section class="organizations">
    <header class="columns page-header">
      <div class="column">
        <h1 class="title is-4"><b-icon icon="office-building-outline" size="is-small" />{{ $t('organizations.title') }}</h1>
        <p class="has-text-grey">{{ workspaceLabel }}</p>
      </div>
    </header>

    <div class="columns is-variable is-6">
      <div class="column is-5">
        <section class="mb-6">
          <h2 class="title is-6"><b-icon icon="office-building-outline" size="is-small" />{{ $t('organizations.myOrganizations') }}</h2>
          <b-table :data="organizations" :mobile-cards="false">
            <b-table-column v-slot="props" field="name" :label="$t('organizations.columnOrg')">
              <a href="#" @click.prevent="switchWorkspace(props.row)">{{ props.row.name }}</a>
            </b-table-column>
            <b-table-column v-slot="props" field="myRole" :label="$t('organizations.role')">
              {{ props.row.myRole === 'manager' ? $t('organizations.roleManager') : $t('organizations.roleMember') }}
            </b-table-column>
            <template #empty><span class="has-text-grey">{{ $t('organizations.notJoined') }}</span></template>
          </b-table>
        </section>

        <section class="mb-6">
          <h2 class="title is-6"><b-icon icon="key-outline" size="is-small" />{{ $t('organizations.joinTitle') }}</h2>
          <form @submit.prevent="joinOrganization">
            <b-field :label="$t('organizations.inviteCode')" label-position="on-border">
              <b-input v-model.trim="joinCode" icon="key-outline" required />
            </b-field>
            <b-button native-type="submit" type="is-primary" icon-left="account-plus-outline">{{ $t('organizations.join') }}</b-button>
          </form>
        </section>

        <section>
          <h2 class="title is-6"><b-icon icon="file-document-edit-outline" size="is-small" />{{ $t('organizations.createRequestTitle') }}</h2>
          <form @submit.prevent="submitOrganizationRequest">
            <b-field :label="$t('organizations.name')" label-position="on-border">
              <b-input v-model.trim="requestForm.name" maxlength="200" required />
            </b-field>
            <b-field :label="$t('organizations.description')" label-position="on-border">
              <b-input v-model.trim="requestForm.description" type="textarea" maxlength="2000" />
            </b-field>
            <b-button native-type="submit" icon-left="file-send-outline">{{ $t('organizations.submitRequest') }}</b-button>
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
              <b-button type="is-text" @click="leaveCurrentOrganization" icon-left="logout-variant">{{ $t('organizations.leave') }}</b-button>
            </div>
          </div>
          <p v-if="!isManager" class="has-text-grey">{{ $t('organizations.memberNote') }}</p>
        </section>

        <section class="mb-6">
          <h2 class="title is-6">{{ $t('organizations.migrateListsTitle') }}</h2>
          <p class="has-text-grey mb-3">{{ $t('organizations.migrateListsHelp') }}</p>
          <b-field :label="$t('organizations.personalLists')" label-position="on-border">
            <b-select v-model="personalCustomerListIDs" multiple expanded>
              <option v-for="customerList in personalLists" :key="customerList.id" :value="customerList.id">{{ customerList.name }}</option>
            </b-select>
          </b-field>
          <div class="buttons">
            <b-button icon-left="content-copy" :disabled="personalCustomerListIDs.length === 0"
              @click="migratePersonalLists('copy')">
{{ $t('organizations.copyToOrg') }}
</b-button>
            <b-button type="is-primary" icon-left="folder-move" :disabled="personalCustomerListIDs.length === 0"
              @click="migratePersonalLists('move')">
{{ $t('organizations.moveToOrg') }}
</b-button>
          </div>
        </section>

        <section class="mb-6">
          <h2 class="title is-6">{{ $t('organizations.migrateResourcesTitle') }}</h2>
          <p class="has-text-grey mb-3">{{ $t('organizations.migrateResourcesHelp') }}</p>

          <b-field :label="$t('organizations.personalTemplates')" label-position="on-border">
            <b-select v-model="personalTemplateIDs" multiple expanded>
              <option v-for="template in personalTemplates" :key="template.id" :value="template.id">
                {{ template.name }}
              </option>
            </b-select>
          </b-field>
          <div class="buttons mb-5">
            <b-button icon-left="content-copy" :disabled="personalTemplateIDs.length === 0"
              @click="migratePersonalResource('templates', personalTemplateIDs, 'copy')">
              {{ $t('organizations.copyToOrg') }}
            </b-button>
            <b-button type="is-primary" icon-left="folder-move" :disabled="personalTemplateIDs.length === 0"
              @click="migratePersonalResource('templates', personalTemplateIDs, 'move')">
              {{ $t('organizations.moveToOrg') }}
            </b-button>
          </div>

          <b-field :label="$t('organizations.personalCampaigns')" label-position="on-border">
            <b-select v-model="personalCampaignIDs" multiple expanded>
              <option v-for="campaign in personalCampaigns" :key="campaign.id" :value="campaign.id">
                {{ campaign.name }} ({{ campaign.status }})
              </option>
            </b-select>
          </b-field>
          <div class="buttons mb-5">
            <b-button icon-left="content-copy" :disabled="personalCampaignIDs.length === 0"
              @click="migratePersonalResource('campaigns', personalCampaignIDs, 'copy')">
              {{ $t('organizations.copyToOrg') }}
            </b-button>
            <b-button type="is-primary" icon-left="folder-move" :disabled="personalCampaignIDs.length === 0"
              @click="migratePersonalResource('campaigns', personalCampaignIDs, 'move')">
              {{ $t('organizations.moveToOrg') }}
            </b-button>
          </div>

          <b-field :label="$t('organizations.personalMedia')" label-position="on-border">
            <b-select v-model="personalMediaIDs" multiple expanded>
              <option v-for="media in personalMedia" :key="media.id" :value="media.id">{{ media.filename }}</option>
            </b-select>
          </b-field>
          <div class="buttons">
            <b-button icon-left="content-copy" :disabled="personalMediaIDs.length === 0"
              @click="migratePersonalResource('media', personalMediaIDs, 'copy')">
              {{ $t('organizations.copyToOrg') }}
            </b-button>
            <b-button type="is-primary" icon-left="folder-move" :disabled="personalMediaIDs.length === 0"
              @click="migratePersonalResource('media', personalMediaIDs, 'move')">
              {{ $t('organizations.moveToOrg') }}
            </b-button>
          </div>
        </section>

        <template v-if="isManager">
          <section class="mb-6">
            <h2 class="title is-6"><b-icon icon="account-group-outline" size="is-small" />{{ $t('organizations.members') }}</h2>
            <form class="columns is-multiline" @submit.prevent="addMember">
              <div class="column is-6">
                <b-field :label="$t('organizations.memberAccount')" label-position="on-border">
                  <b-input v-model.trim="memberForm.account" required />
                </b-field>
              </div>
              <div class="column is-3">
                <b-field :label="$t('organizations.organizationRole')" label-position="on-border">
                  <b-select v-model="memberForm.role" expanded>
                    <option value="member">{{ $t('organizations.roleMember') }}</option>
                    <option value="manager">{{ $t('organizations.roleManager') }}</option>
                  </b-select>
                </b-field>
              </div>
              <div class="column is-3 is-flex is-align-items-flex-end">
                <b-button native-type="submit" type="is-primary" expanded icon-left="account-plus-outline">{{ $t('organizations.add') }}</b-button>
              </div>
            </form>
            <b-table :data="activeMembers" :mobile-cards="false">
              <b-table-column v-slot="props" field="username" :label="$t('organizations.account')">
                <strong>{{ props.row.username }}</strong>
                <span v-if="props.row.name" class="has-text-grey"> {{ props.row.name }}</span>
              </b-table-column>
              <b-table-column v-slot="props" field="role" :label="$t('organizations.role')">
                <b-select :value="props.row.role" size="is-small" @input="changeMemberRole(props.row, $event)">
                  <option value="member">{{ $t('organizations.roleMember') }}</option>
                  <option value="manager">{{ $t('organizations.roleManager') }}</option>
                </b-select>
              </b-table-column>
              <b-table-column v-slot="props" :label="$t('organizations.columnActions')" numeric>
                <b-button size="is-small" type="is-text" icon-left="account-remove-outline"
                  @click="removeMember(props.row)">
{{ $t('organizations.remove') }}
</b-button>
              </b-table-column>
            </b-table>
          </section>

          <section class="mb-6">
            <h2 class="title is-6"><b-icon icon="key-outline" size="is-small" />{{ $t('organizations.invites') }}</h2>
            <form class="columns is-multiline" @submit.prevent="createInvite">
              <div class="column is-4">
                <b-field :label="$t('organizations.inviteName')" label-position="on-border"><b-input v-model.trim="inviteForm.name" /></b-field>
              </div>
              <div class="column is-4">
                <b-field :label="$t('organizations.expiry')" label-position="on-border">
                  <b-input v-model="inviteForm.expiresAt" type="datetime-local" />
                </b-field>
              </div>
              <div class="column is-2">
                <b-field :label="$t('organizations.maxUses')" label-position="on-border">
                  <b-input v-model.number="inviteForm.maxUses" type="number" min="1" />
                </b-field>
              </div>
              <div class="column is-2 is-flex is-align-items-flex-end">
                <b-button native-type="submit" type="is-primary" expanded icon-left="key-plus">{{ $t('organizations.create') }}</b-button>
              </div>
            </form>
            <b-notification v-if="newInviteCode" type="is-success" :closable="false">
              <copy-text :text="newInviteCode" />
            </b-notification>
            <b-table :data="invites" :mobile-cards="false">
              <b-table-column v-slot="props" field="name" :label="$t('organizations.inviteName')">{{ props.row.name || $t('organizations.inviteCode') }}</b-table-column>
              <b-table-column v-slot="props" field="useCount" :label="$t('organizations.uses')">
                {{ props.row.useCount }}<span v-if="props.row.maxUses"> / {{ props.row.maxUses }}</span>
              </b-table-column>
              <b-table-column v-slot="props" field="expiresAt" :label="$t('organizations.expiry')">
                {{ props.row.expiresAt ? $utils.niceDate(props.row.expiresAt) : $t('organizations.noExpiry') }}
              </b-table-column>
              <b-table-column v-slot="props" :label="$t('organizations.columnActions')" numeric>
                <b-button v-if="!props.row.revokedAt" size="is-small" type="is-text" icon-left="cancel"
                  @click="revokeInvite(props.row)">
{{ $t('organizations.revoke') }}
</b-button>
                <span v-else class="has-text-grey">{{ $t('organizations.revoked') }}</span>
              </b-table-column>
            </b-table>
          </section>

          <section>
            <h2 class="title is-6"><b-icon icon="swap-horizontal" size="is-small" />{{ $t('organizations.pendingResources') }}</h2>
            <div class="columns is-vcentered">
              <div class="column is-7">
                <b-select v-model.number="transferTargetUserID" :placeholder="$t('organizations.selectRecipient')" expanded>
                  <option v-for="member in activeMembers" :key="member.userId" :value="member.userId">
                    {{ member.username }}
                  </option>
                </b-select>
              </div>
              <div class="column">
                <b-button :disabled="!transferTargetUserID" icon-left="swap-horizontal"
                  @click="transferPendingResources">
{{ $t('organizations.transferPending') }}
</b-button>
              </div>
            </div>
          </section>
        </template>
      </div>
    </div>

    <section v-if="isPlatformAdmin" class="mt-6">
      <h2 class="title is-5"><b-icon icon="file-document-edit-outline" size="is-small" />{{ $t('organizations.creationRequests') }}</h2>
      <b-table :data="requests" :mobile-cards="false">
        <b-table-column v-slot="props" field="requestedName" :label="$t('organizations.columnOrg')">{{ props.row.requestedName }}</b-table-column>
        <b-table-column v-slot="props" field="requestedByName" :label="$t('organizations.requester')">{{ props.row.requestedByName }}</b-table-column>
        <b-table-column v-slot="props" field="description" :label="$t('organizations.description')">{{ props.row.description }}</b-table-column>
        <b-table-column v-slot="props" :label="$t('organizations.columnActions')" numeric>
          <b-button size="is-small" type="is-primary" icon-left="check" @click="reviewRequest(props.row, true)">{{ $t('organizations.approve') }}</b-button>
          <b-button size="is-small" type="is-text" icon-left="close" @click="reviewRequest(props.row, false)">{{ $t('organizations.reject') }}</b-button>
        </b-table-column>
      </b-table>

      <h2 class="title is-5 mt-6"><b-icon icon="archive-outline" size="is-small" />{{ $t('organizations.archiveSection') }}</h2>
      <b-table :data="platformOrganizations" :mobile-cards="false">
        <b-table-column v-slot="props" field="name" :label="$t('organizations.columnOrg')">{{ props.row.name }}</b-table-column>
        <b-table-column v-slot="props" field="memberCount" :label="$t('organizations.memberCount')">{{ props.row.memberCount }}</b-table-column>
        <b-table-column v-slot="props" field="status" :label="$t('organizations.status')">
          <b-tag :type="props.row.status === 'archived' ? 'is-warning' : 'is-success'">
            {{ props.row.status === 'archived' ? $t('organizations.statusArchived') : $t('organizations.statusNormal') }}
          </b-tag>
        </b-table-column>
        <b-table-column v-slot="props" field="archivedAt" :label="$t('organizations.archivedAt')">
          {{ props.row.archivedAt ? $utils.niceDate(props.row.archivedAt) : '-' }}
        </b-table-column>
        <b-table-column v-slot="props" :label="$t('organizations.columnActions')" numeric>
          <b-button v-if="props.row.status !== 'archived'" size="is-small" type="is-text" icon-left="login-variant"
            @click="switchWorkspace(props.row)">
            {{ $t('organizations.enterOrg') }}
          </b-button>
          <b-button v-if="props.row.status !== 'archived'" size="is-small" type="is-text" icon-left="archive-outline"
            @click="archivePlatformOrganization(props.row)">
{{ $t('organizations.archive') }}
</b-button>
          <b-button v-if="props.row.status === 'archived'" size="is-small" type="is-text" icon-left="swap-horizontal"
            @click="openArchiveTransfer(props.row)">
{{ $t('organizations.transferResources') }}
</b-button>
          <b-button v-if="props.row.status === 'archived'" size="is-small" type="is-text" icon-left="delete-forever-outline"
            @click="purgePlatformOrganization(props.row)">
{{ $t('organizations.deleteForever') }}
</b-button>
        </b-table-column>
      </b-table>
    </section>

    <b-modal scroll="keep" :aria-modal="true" :active.sync="isArchiveTransferVisible" :width="520">
      <div class="modal-card content" style="width: auto">
        <header class="modal-card-head"><h4><b-icon icon="swap-horizontal" size="is-small" />{{ $t('organizations.transferArchivedTitle') }}</h4></header>
        <section class="modal-card-body">
          <p v-if="archiveTransferOrganization" class="mb-4">{{ archiveTransferOrganization.name }}</p>
          <b-field :label="$t('organizations.receiveMember')" label-position="on-border">
            <b-select v-model.number="archiveTransferTargetUserID" expanded>
              <option :value="null">{{ $t('organizations.selectMember') }}</option>
              <option v-for="member in archiveTransferMembers" :key="member.userId" :value="member.userId">
                {{ member.username }}
              </option>
            </b-select>
          </b-field>
          <p class="has-text-grey is-size-7">{{ $t('organizations.transferToPersonalHelp') }}</p>
        </section>
        <footer class="modal-card-foot has-text-right">
          <b-button @click="isArchiveTransferVisible = false">{{ $t('globals.buttons.close') }}</b-button>
          <b-button type="is-primary" icon-left="swap-horizontal" :disabled="!archiveTransferTargetUserID" @click="transferArchivedResources">
{{ $t('organizations.transfer') }}
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
      personalCustomerListIDs: [],
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
      return this.workspace.organizationId ? this.workspace.organizationName : this.$t('organizations.personalSpace');
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
        this.personalCustomerListIDs = [];
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
      this.$utils.toast(this.$t('organizations.toastRequestSubmitted'));
    },

    leaveCurrentOrganization() {
      this.$utils.confirm(this.$t('organizations.confirmLeave'), async () => {
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
      this.$utils.confirm(this.$t('organizations.confirmRemoveMember', { name: member.username }), async () => {
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
      this.$utils.confirm(this.$t('organizations.confirmTransferPending'), async () => {
        await this.$api.transferPendingOrganizationResources({ target_user_id: this.transferTargetUserID });
        this.transferTargetUserID = null;
        await this.refresh();
      });
    },

    migratePersonalLists(mode) {
      const action = mode === 'move' ? this.$t('organizations.migrationMove') : this.$t('organizations.migrationCopy');
      this.$utils.confirm(this.$t('organizations.confirmMigrateLists', { action }), async () => {
        await this.$api.migratePersonalLists({
          customer_list_ids: this.personalCustomerListIDs,
          mode,
          target_organization_id: this.workspace.organizationId,
        });
        this.personalCustomerListIDs = [];
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
        templates: this.$t('globals.terms.templates'),
        campaigns: this.$t('globals.terms.campaigns'),
        media: this.$t('organizations.personalMedia'),
      };
      const selectedKey = {
        templates: 'personalTemplateIDs',
        campaigns: 'personalCampaignIDs',
        media: 'personalMediaIDs',
      }[resource];
      const action = mode === 'move' ? this.$t('organizations.migrationMove') : this.$t('organizations.migrationCopy');
      this.$utils.confirm(this.$t('organizations.confirmMigrateResources', { action, label: labels[resource] }), async () => {
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
      this.$utils.confirm(this.$t('organizations.confirmArchive', { name: organization.name }), async () => {
        await this.$api.archiveOrganization(organization.id);
        await this.refresh();
      });
    },

    purgePlatformOrganization(organization) {
      this.$utils.confirm(this.$t('organizations.confirmPurge', { name: organization.name }), async () => {
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
