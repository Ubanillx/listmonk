<template>
  <section class="organizations">
    <header class="columns page-header">
      <div class="column">
        <h1 class="title is-4">{{ $t('organizations.manageTitle') }}</h1>
      </div>
    </header>

    <b-field v-if="manageableOrganizations.length" :label="$t('organizations.manageSelectLabel')" label-position="on-border" class="section-mini mb-5">
      <b-select v-model.number="selectedOrganizationID" expanded>
        <option v-for="organization in manageableOrganizations" :key="organization.id" :value="organization.id">
          {{ organization.name }}
        </option>
      </b-select>
    </b-field>

    <b-notification v-if="!selectedOrganizationID && !canManageAllOrganizations" type="is-light" :closable="false">
      {{ $t('organizations.manageNotAdmin') }}
    </b-notification>

    <b-tabs v-if="selectedOrganizationID || canManageAllOrganizations" type="is-boxed" :animated="false" v-model="activeTab">
      <b-tab-item v-if="selectedOrganizationID" :label="$t('organizations.tabMembers')" icon="account-group-outline">
        <section class="wrap">
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
            <b-table-column v-slot="props" field="email" :label="$t('customers.email')">{{ props.row.email }}</b-table-column>
            <b-table-column v-slot="props" field="role" :label="$t('organizations.role')">
              <b-select :value="props.row.role" size="is-small" @input="changeMemberRole(props.row, $event)">
                <option value="member">{{ $t('organizations.roleMember') }}</option>
                <option value="manager">{{ $t('organizations.roleManager') }}</option>
              </b-select>
            </b-table-column>
            <b-table-column v-slot="props" :label="$t('organizations.columnActions')" numeric>
              <b-button size="is-small" type="is-text" icon-left="account-remove-outline" @click="removeMember(props.row)">
                {{ $t('organizations.remove') }}
              </b-button>
            </b-table-column>
            <template #empty><span class="has-text-grey">{{ $t('organizations.noMembers') }}</span></template>
          </b-table>
        </section>
      </b-tab-item>

      <b-tab-item v-if="selectedOrganizationID" :label="$t('organizations.tabInvites')" icon="key-outline">
        <section class="wrap">
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
              {{ props.row.expiresAt ? $utils.niceDate(props.row.expiresAt, true) : $t('organizations.noExpiry') }}
            </b-table-column>
            <b-table-column v-slot="props" :label="$t('organizations.columnActions')" numeric>
              <b-button v-if="!props.row.revokedAt" size="is-small" type="is-text" icon-left="cancel" @click="revokeInvite(props.row)">
                {{ $t('organizations.revoke') }}
              </b-button>
              <span v-else class="has-text-grey">{{ $t('organizations.revoked') }}</span>
            </b-table-column>
            <template #empty><span class="has-text-grey">{{ $t('organizations.noInvites') }}</span></template>
          </b-table>
        </section>
      </b-tab-item>

      <b-tab-item v-if="selectedOrganizationID" :label="$t('organizations.tabPending')" icon="swap-horizontal">
        <section class="wrap">
          <p class="has-text-grey mb-4">{{ $t('organizations.pendingHelp') }}</p>
          <div class="columns is-vcentered">
            <div class="column is-7">
              <b-field :label="$t('organizations.receiveMember')" label-position="on-border">
                <b-select v-model.number="transferTargetUserID" :placeholder="$t('organizations.selectRecipient')" expanded>
                  <option :value="null">{{ $t('organizations.selectMember') }}</option>
                  <option v-for="member in activeMembers" :key="member.userId" :value="member.userId">
                    {{ member.username }}
                  </option>
                </b-select>
              </b-field>
            </div>
            <div class="column is-3 is-flex is-align-items-flex-end">
              <b-button :disabled="!transferTargetUserID" icon-left="swap-horizontal" @click="transferPendingResources">
                {{ $t('organizations.transferResources') }}
              </b-button>
            </div>
          </div>
        </section>
      </b-tab-item>

      <b-tab-item v-if="selectedOrganizationID" :label="$t('organizations.tabReplyMailboxes')" icon="email-multiple-outline">
        <section class="wrap">
          <reply-mailbox-settings :organization-id="Number(selectedOrganizationID)" />
        </section>
      </b-tab-item>

      <b-tab-item v-if="selectedOrganizationID" :label="$t('organizations.tabReplyForward')" icon="email-arrow-left-outline">
        <section class="wrap">
          <p class="has-text-grey mb-4">{{ $t('organizations.replyForwardHelp') }}</p>
          <b-table :data="replyForwardRules" :mobile-cards="false">
            <b-table-column v-slot="props" field="sourceEmail" :label="$t('organizations.replyForwardSource')">
              <strong>{{ props.row.sourceEmail || props.row.sourceName || '-' }}</strong>
            </b-table-column>
            <b-table-column v-slot="props" field="mailboxEmail" :label="$t('organizations.replyForwardMailbox')">{{ props.row.mailboxEmail }}</b-table-column>
            <b-table-column v-slot="props" field="targetEmail" :label="$t('organizations.replyForwardTarget')">{{ props.row.targetEmail }}</b-table-column>
            <b-table-column v-slot="props" field="status" :label="$t('organizations.status')">
              <b-tag :type="props.row.status === 'active' ? 'is-success' : 'is-light'">
                {{ props.row.status === 'active' ? $t('organizations.replyForwardActive') : $t('organizations.replyForwardDisabled') }}
              </b-tag>
            </b-table-column>
            <b-table-column v-slot="props" :label="$t('organizations.columnActions')" numeric>
              <b-button size="is-small" type="is-text" :icon-left="props.row.status === 'active' ? 'pause-circle-outline' : 'play-circle-outline'"
                @click="toggleReplyForwardRule(props.row)">
                {{ props.row.status === 'active' ? $t('organizations.replyForwardToggle') : $t('organizations.replyForwardResume') }}
              </b-button>
            </b-table-column>
            <template #empty><span class="has-text-grey">{{ $t('organizations.noReplyForwardRules') }}</span></template>
          </b-table>
        </section>
      </b-tab-item>

      <b-tab-item v-if="canManageAllOrganizations" :label="$t('organizations.tabPlatform')" icon="shield-crown-outline">
        <section class="mb-6">
          <h2 class="title is-5">{{ $t('organizations.creationRequests') }}</h2>
          <b-table :data="requests" :mobile-cards="false">
            <b-table-column v-slot="props" field="requestedName" :label="$t('organizations.columnOrg')">{{ props.row.requestedName }}</b-table-column>
            <b-table-column v-slot="props" field="requestedByName" :label="$t('organizations.requester')">{{ props.row.requestedByName }}</b-table-column>
            <b-table-column v-slot="props" field="description" :label="$t('organizations.description')">{{ props.row.description }}</b-table-column>
            <b-table-column v-slot="props" field="createdAt" :label="$t('organizations.requestTime')">{{ $utils.niceDate(props.row.createdAt, true) }}</b-table-column>
            <b-table-column v-slot="props" :label="$t('organizations.columnActions')" numeric>
              <b-button size="is-small" type="is-primary" icon-left="check" @click="reviewRequest(props.row, true)">{{ $t('organizations.approve') }}</b-button>
              <b-button size="is-small" type="is-text" icon-left="close" @click="reviewRequest(props.row, false)">{{ $t('organizations.reject') }}</b-button>
            </b-table-column>
            <template #empty><span class="has-text-grey">{{ $t('organizations.noRequests') }}</span></template>
          </b-table>
        </section>

        <section>
          <h2 class="title is-5">{{ $t('organizations.archiveSection') }}</h2>
          <b-table :data="platformOrganizations" :mobile-cards="false">
            <b-table-column v-slot="props" field="name" :label="$t('organizations.columnOrg')">
              <strong>{{ props.row.name }}</strong>
              <p v-if="props.row.description" class="has-text-grey is-size-7">{{ props.row.description }}</p>
            </b-table-column>
            <b-table-column v-slot="props" field="memberCount" :label="$t('organizations.memberCount')" numeric>{{ props.row.memberCount }}</b-table-column>
            <b-table-column v-slot="props" field="status" :label="$t('organizations.status')">
              <b-tag :type="props.row.status === 'archived' ? 'is-warning' : 'is-success'">
                {{ props.row.status === 'archived' ? $t('organizations.statusArchived') : $t('organizations.statusNormal') }}
              </b-tag>
            </b-table-column>
            <b-table-column v-slot="props" field="archivedAt" :label="$t('organizations.archivedAt')">
              {{ props.row.archivedAt ? $utils.niceDate(props.row.archivedAt, true) : '-' }}
            </b-table-column>
            <b-table-column v-slot="props" :label="$t('organizations.columnActions')" numeric>
              <b-button v-if="props.row.status !== 'archived'" size="is-small" type="is-text" icon-left="account-cog-outline"
                @click="selectOrganization(props.row)">
                {{ $t('globals.buttons.manage') }}
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
      </b-tab-item>
    </b-tabs>

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
import CopyText from '../../components/CopyText.vue';
import ReplyMailboxSettings from '../../components/ReplyMailboxSettings.vue';

export default Vue.extend({
  components: { CopyText, ReplyMailboxSettings },

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

    canManageAllOrganizations() {
      return this.profile.userRole && (Number(this.profile.userRole.id) === 1
        || (this.profile.userRole.permissions || []).includes('organizations:platform_manage'));
    },

    manageableOrganizations() {
      if (this.canManageAllOrganizations) {
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
      if (this.canManageAllOrganizations) {
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
      this.$utils.confirm(this.$t('organizations.confirmRemoveMember', { name: member.username }), async () => {
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
      this.$utils.confirm(this.$t('organizations.confirmTransferPending'), async () => {
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
