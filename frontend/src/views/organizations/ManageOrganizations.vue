<template>
  <section class="organizations">
    <header class="columns page-header">
      <div class="column">
        <h1 class="title is-4">{{ $t('organizations.manageTitle') }}</h1>
      </div>
    </header>

    <b-field v-if="manageableOrganizations.length" :label="$t('organizations.manageSelectLabel')" label-position="on-border" class="section-mini mb-5">
      <b-select v-model.number="membershipOrganizationID" expanded>
        <option v-if="!membershipOrganizationID" :value="null" disabled>{{ $t('pool.selectOrganizationPlaceholder') }}</option>
        <option v-for="organization in manageableOrganizations" :key="organization.id" :value="organization.id">
          {{ organization.name }}
        </option>
      </b-select>
    </b-field>

    <b-notification v-if="selectedPlatformOrganization" type="is-light" :closable="false" data-cy="platform-managed-organization">
      {{ $t('organizations.manageSelectLabel') }}: {{ selectedPlatformOrganization.name }}
    </b-notification>

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

          <div class="table-scroll">
            <b-table :data="activeMembers">
            <b-table-column v-slot="props" field="username" :label="$t('organizations.account')">
              <strong>{{ props.row.username }}</strong>
              <span v-if="props.row.name" class="has-text-grey"> {{ props.row.name }}</span>
            </b-table-column>
            <b-table-column v-slot="props" field="email" :label="$t('customers.email')">{{ props.row.email }}</b-table-column>
            <b-table-column v-slot="props" field="role" :label="$t('organizations.role')">
              <b-select :key="`member-role-${props.row.userId}-${roleRevision}`" :value="props.row.role" size="is-small"
                @input="confirmMemberRoleChange(props.row, $event)">
                <option value="member">{{ $t('organizations.roleMember') }}</option>
                <option value="manager">{{ $t('organizations.roleManager') }}</option>
              </b-select>
            </b-table-column>
            <b-table-column v-slot="props" :label="$t('organizations.columnActions')" numeric>
              <b-button size="is-small" type="is-text" icon-left="account-remove-outline" @click="removeMember(props.row)">
                {{ $t('organizations.remove') }}
              </b-button>
            </b-table-column>
            <template #empty v-if="!isLoading"><span class="has-text-grey">{{ $t('organizations.noMembers') }}</span></template>
            </b-table>
          </div>
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

          <div class="table-scroll">
            <b-table :data="invites">
            <b-table-column v-slot="props" field="name" :label="$t('organizations.inviteName')">{{ props.row.name || $t('organizations.inviteCode') }}</b-table-column>
            <b-table-column v-slot="props" field="useCount" :label="$t('organizations.uses')">
              {{ props.row.useCount }}<span v-if="props.row.maxUses"> / {{ props.row.maxUses }}</span>
            </b-table-column>
            <b-table-column v-slot="props" field="expiresAt" :label="$t('organizations.expiry')">
              {{ props.row.expiresAt ? $utils.niceDate(props.row.expiresAt, true) : $t('organizations.noExpiry') }}
            </b-table-column>
            <b-table-column v-slot="props" :label="$t('organizations.columnActions')" numeric>
              <b-button v-if="!props.row.revokedAt" size="is-small" type="is-text" icon-left="cancel"
                @click="$utils.confirm($t('organizations.confirmRevokeInvite'), () => revokeInvite(props.row), null, { type: 'is-danger' })">
                {{ $t('organizations.revoke') }}
              </b-button>
              <span v-else class="has-text-grey">{{ $t('organizations.revoked') }}</span>
            </b-table-column>
            <template #empty v-if="!isLoading"><span class="has-text-grey">{{ $t('organizations.noInvites') }}</span></template>
            </b-table>
          </div>
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

      <b-tab-item v-if="selectedOrganizationID && $can('mailboxes:manage')" :label="$t('organizations.tabReplyMailboxes')" icon="email-multiple-outline">
        <section class="wrap">
          <section class="mb-6" data-cy="org-unified-reply-mailbox">
            <h2 class="title is-5">{{ $t('organizations.unifiedReplyMailbox') }}</h2>
            <b-field addons :label="$t('organizations.unifiedReplyMailbox')" label-position="on-border"
              :message="$t('organizations.unifiedReplyMailboxHelp')">
              <b-field expanded>
                <b-select v-model="unifiedReplyMailboxID" expanded :disabled="!canEditUnifiedReplyMailbox"
                  data-cy="org-unified-reply-mailbox-select">
                  <option :value="null">{{ $t('organizations.unifiedReplyMailboxNone') }}</option>
                  <option v-if="unifiedReplyMailboxID && !unifiedMailboxOptions.some((mailbox) => Number(mailbox.id) === Number(unifiedReplyMailboxID))"
                    :value="unifiedReplyMailboxID" disabled>
                    {{ selectedOrganizationReplyMailboxEmail || $t('organizations.unifiedReplyMailboxNone') }}
                  </option>
                  <option v-for="mailbox in unifiedMailboxOptions" :key="mailbox.id" :value="mailbox.id">
                    {{ mailbox.email || mailbox.name }}
                  </option>
                </b-select>
              </b-field>
              <p class="control">
                <b-button type="is-primary" icon-left="content-save-outline" :loading="savingUnifiedReplyMailbox"
                  :disabled="!canEditUnifiedReplyMailbox" data-cy="org-unified-reply-mailbox-save"
                  @click="saveUnifiedReplyMailbox">
                  {{ $t('organizations.unifiedReplyMailboxSave') }}
                </b-button>
              </p>
            </b-field>
            <p v-if="!canEditUnifiedReplyMailbox" class="help" data-cy="org-unified-reply-mailbox-readonly">
              {{ $t('organizations.unifiedReplyMailboxReadOnly') }}
            </p>
          </section>
          <reply-mailbox-settings :organization-id="Number(selectedOrganizationID)" />
        </section>
      </b-tab-item>

      <b-tab-item v-if="selectedOrganizationID && $can('mailboxes:manage')" :label="$t('organizations.tabReplyForward')" icon="email-arrow-left-outline">
        <section class="wrap">
          <p class="has-text-grey mb-4">{{ $t('organizations.replyForwardHelp') }}</p>
          <div class="table-scroll">
            <b-table :data="replyForwardRules">
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
            <template #empty v-if="!isLoading"><span class="has-text-grey">{{ $t('organizations.noReplyForwardRules') }}</span></template>
            </b-table>
          </div>
        </section>
      </b-tab-item>

      <b-tab-item v-if="selectedOrganizationID && $can('mailboxes:manage')" :label="$t('organizations.smtpTitle')" icon="email-fast-outline">
        <section class="wrap smtp-pool-wrap" data-cy="organization-smtp">
          <div class="columns is-variable is-5 smtp-pool-layout">
            <div class="column is-3">
              <aside class="box smtp-pool-sidebar">
                <div class="smtp-pool-sidebar-heading">
                  <div>
                    <p class="heading mb-1">{{ $t('organizations.smtpPoolSelect') }}</p>
                    <p class="help">{{ $t('organizations.smtpHelp') }}</p>
                  </div>
                  <b-tag v-if="smtpPools.length" type="is-light" rounded>{{ smtpPools.length }}</b-tag>
                </div>

                <div v-if="smtpPools.length" class="smtp-pool-list" role="list">
                  <button v-for="pool in smtpPools" :key="pool.id" type="button" role="listitem"
                    :class="['smtp-pool-option', { 'is-active': Number(pool.id) === Number(selectedSMTPPoolID) }]"
                    @click="selectSMTPPool(pool.id)">
                    <span class="smtp-pool-option-main">
                      <strong>{{ pool.name }}</strong>
                      <span class="help">{{ pool.enabledCount }}/{{ pool.smtpCount }} {{ $t('organizations.smtpTitle') }}</span>
                    </span>
                    <b-icon icon="chevron-right" size="is-small" />
                  </button>
                </div>
                <div v-else class="notification is-light smtp-pool-empty mb-4">
                  {{ $t('organizations.smtpPoolEmpty') }}
                </div>

                <div class="smtp-pool-create">
                  <b-field :label="$t('organizations.smtpPoolName')" label-position="on-border">
                    <b-input v-model.trim="newSMTPPoolName" @keyup.native.enter="createSMTPPool" />
                  </b-field>
                  <b-button type="is-primary" expanded icon-left="plus" :disabled="!newSMTPPoolName" @click="createSMTPPool">
                    {{ $t('organizations.smtpPoolCreate') }}
                  </b-button>
                </div>

                <b-button v-if="selectedSMTPPoolID" type="is-text" size="is-small" class="smtp-pool-delete"
                  icon-left="delete-outline" :disabled="selectedSMTPPool && selectedSMTPPool.smtpCount > 0" @click="deleteSMTPPool">
                  {{ $t('organizations.smtpPoolDelete') }}
                </b-button>
              </aside>
            </div>

            <div class="column is-9">
              <div v-if="selectedSMTPPoolID" class="smtp-pool-editor">
                <personal-s-m-t-p-settings ref="smtpSettings" :key="`${selectedOrganizationID}-${selectedSMTPPoolID}`"
                  :organization-id="Number(selectedOrganizationID)" :smtp-pool-id="Number(selectedSMTPPoolID)"
                  :organization-pool-name="selectedSMTPPool ? selectedSMTPPool.name : ''" />
              </div>
              <div v-else class="notification is-light smtp-pool-first-step">
                <b-icon icon="arrow-left" />
                <span>{{ $t('organizations.smtpPoolEmpty') }}</span>
              </div>
            </div>
          </div>
        </section>
      </b-tab-item>

      <b-tab-item v-if="canManageAllOrganizations" :label="$t('organizations.tabPlatform')" icon="shield-crown-outline">
        <section class="mb-6">
          <h2 class="title is-5">{{ $t('organizations.creationRequests') }}</h2>
          <div class="table-scroll">
            <b-table :data="requests">
            <b-table-column v-slot="props" field="requestedName" :label="$t('organizations.columnOrg')">{{ props.row.requestedName }}</b-table-column>
            <b-table-column v-slot="props" field="requestedByName" :label="$t('organizations.requester')">{{ props.row.requestedByName }}</b-table-column>
            <b-table-column v-slot="props" field="description" :label="$t('organizations.description')">{{ props.row.description }}</b-table-column>
            <b-table-column v-slot="props" field="createdAt" :label="$t('organizations.requestTime')">{{ $utils.niceDate(props.row.createdAt, true) }}</b-table-column>
            <b-table-column v-slot="props" :label="$t('organizations.columnActions')" numeric>
              <b-button size="is-small" type="is-primary" icon-left="check" @click="confirmApproveRequest(props.row)">{{ $t('organizations.approve') }}</b-button>
              <b-button size="is-small" type="is-text" icon-left="close" @click="confirmRejectRequest(props.row)">{{ $t('organizations.reject') }}</b-button>
            </b-table-column>
            <template #empty v-if="!isLoading"><span class="has-text-grey">{{ $t('organizations.noRequests') }}</span></template>
            </b-table>
          </div>
        </section>

        <section>
          <h2 class="title is-5">{{ $t('organizations.archiveSection') }}</h2>
          <div class="table-scroll">
            <b-table :data="platformOrganizations">
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
          </div>
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
import PersonalSMTPSettings from '../../components/PersonalSMTPSettings.vue';

export default Vue.extend({
  components: { CopyText, ReplyMailboxSettings, PersonalSMTPSettings },

  data() {
    return {
      isLoading: false,
      roleRevision: 0,
      activeTab: 0,
      selectedOrganizationID: null,
      platformOrganizationID: null,
      members: [],
      invites: [],
      replyForwardRules: [],
      organizationReplyMailboxes: [],
      unifiedReplyMailboxID: null,
      savingUnifiedReplyMailbox: false,
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
      smtpPools: [],
      selectedSMTPPoolID: null,
      newSMTPPoolName: '',
    };
  },

  computed: {
    ...mapState(['profile']),
    ...mapState({ organizations: (state) => state.organizationMemberships }),

    canManageAllOrganizations() {
      return this.profile.userRole && (Number(this.profile.userRole.id) === 1
        || (this.profile.userRole.permissions || []).includes('organizations:platform_manage'));
    },

    manageableOrganizations() {
      return this.organizations.filter((organization) => this.canManageAllOrganizations || organization.myRole === 'manager');
    },

    membershipOrganizationID: {
      get() {
        return this.manageableOrganizations.some((organization) => Number(organization.id) === Number(this.selectedOrganizationID))
          ? this.selectedOrganizationID : null;
      },
      set(id) {
        this.platformOrganizationID = null;
        this.selectedOrganizationID = id;
      },
    },

    selectedPlatformOrganization() {
      if (!this.canManageAllOrganizations || this.membershipOrganizationID
        || Number(this.platformOrganizationID) !== Number(this.selectedOrganizationID)) return null;
      return this.platformOrganizations.find((organization) => Number(organization.id) === Number(this.selectedOrganizationID)) || null;
    },

    activeMembers() {
      return this.members.filter((member) => !member.removedAt);
    },

    // The organization shown by the tabs. Platform organization operators may
    // edit the selected organization's unified reply mailbox from this screen.
    selectedOrganization() {
      const id = Number(this.selectedOrganizationID) || 0;
      const lists = [...(this.manageableOrganizations || []), ...(this.platformOrganizations || [])];
      return lists.find((organization) => Number(organization.id) === id) || null;
    },

    selectedOrganizationReplyMailboxEmail() {
      const organization = this.selectedOrganization;
      return (organization && (organization.replyMailboxEmail || organization.reply_mailbox_email)) || '';
    },

    selectedSMTPPool() {
      return this.smtpPools.find((pool) => Number(pool.id) === Number(this.selectedSMTPPoolID)) || null;
    },

    canEditUnifiedReplyMailbox() {
      const id = Number(this.selectedOrganizationID) || 0;
      if (this.canManageAllOrganizations) {
        return true;
      }
      return (this.organizations || []).some(
        (organization) => Number(organization.id) === id && organization.myRole === 'manager',
      );
    },
    // The organization's listing can hold the same address more than once: the
    // unique index is per member, so two members may register the same mailbox.
    // The unified setting is per address, so collapse duplicates and prefer a row
    // the caller also owns and that is already verified.
    unifiedMailboxOptions() {
      const rank = (mailbox) => (mailbox.manageable === false ? 2 : 0)
        + (mailbox.status === 'active' ? 0 : 1);
      const byEmail = (this.organizationReplyMailboxes || []).reduce((acc, mailbox) => {
        const email = String(mailbox.email || '').trim().toLowerCase();
        if (email) {
          const current = acc.get(email);
          if (!current || rank(mailbox) < rank(current)) {
            acc.set(email, mailbox);
          }
        }
        return acc;
      }, new Map());
      return Array.from(byEmail.values());
    },
  },

  watch: {
    selectedOrganizationID() {
      this.newInviteCode = '';
      this.smtpPools = [];
      this.selectedSMTPPoolID = null;
      this.refreshSelectedOrganization();
    },
  },

  methods: {
    async refresh() {
      this.isLoading = true;
      try {
        await this.$api.refreshOrganizationDirectory();
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
      } catch (err) {
        if (!this.$store.state.organizationDirectoryError) throw err;
      } finally {
        this.isLoading = false;
      }
    },

    ensureSelectedOrganization() {
      const selectedID = Number(this.selectedOrganizationID) || 0;
      if (this.manageableOrganizations.some((organization) => Number(organization.id) === selectedID)) {
        return;
      }
      if (this.selectedPlatformOrganization && this.selectedPlatformOrganization.status === 'active') return;
      this.platformOrganizationID = null;
      const activeOrganizationID = Number(this.$store.state.workspace.organizationId) || 0;
      const activeOrganization = this.manageableOrganizations.find(
        (organization) => Number(organization.id) === activeOrganizationID,
      );
      this.selectedOrganizationID = (activeOrganization || this.manageableOrganizations[0] || {}).id || null;
    },

    async refreshSelectedOrganization() {
      if (!this.selectedOrganizationID) {
        this.members = [];
        this.invites = [];
        this.replyForwardRules = [];
        this.transferTargetUserID = null;
        this.organizationReplyMailboxes = [];
        this.unifiedReplyMailboxID = null;
        this.smtpPools = [];
        this.selectedSMTPPoolID = null;
        return;
      }
      this.isLoading = true;
      try {
        const [members, invites] = await Promise.all([
          this.$api.getOrganizationMembers(this.selectedOrganizationID),
          this.$api.getOrganizationInvites(this.selectedOrganizationID),
        ]);
        this.members = members;
        this.invites = invites;
        if (this.$can('mailboxes:manage')) {
          this.replyForwardRules = await this.$api.getReplyForwardRules(this.selectedOrganizationID);
          await this.loadSMTPPools();
        }
        this.transferTargetUserID = null;
        if (this.$can('mailboxes:manage')) await this.loadUnifiedReplyMailbox();
      } finally {
        this.isLoading = false;
      }
    },

    async loadSMTPPools() {
      const pools = await this.$api.getOrganizationSMTPPools(this.selectedOrganizationID);
      this.smtpPools = pools || [];
      if (!this.smtpPools.some((pool) => Number(pool.id) === Number(this.selectedSMTPPoolID))) {
        this.selectedSMTPPoolID = this.smtpPools.length ? this.smtpPools[0].id : null;
      }
    },

    async createSMTPPool() {
      if (!this.newSMTPPoolName) return;
      const create = async () => {
        const pool = await this.$api.createOrganizationSMTPPool({ name: this.newSMTPPoolName }, this.selectedOrganizationID);
        this.newSMTPPoolName = '';
        await this.loadSMTPPools();
        this.selectedSMTPPoolID = pool.id;
      };
      if (this.hasUnsavedSMTPChanges()) {
        this.$utils.confirm(this.$t('globals.messages.confirmDiscard'), create);
        return;
      }
      await create();
    },

    hasUnsavedSMTPChanges() {
      const settings = this.$refs.smtpSettings;
      return !!settings && settings.isDirty();
    },

    selectSMTPPool(poolID) {
      if (Number(poolID) === Number(this.selectedSMTPPoolID)) return;
      const select = () => { this.selectedSMTPPoolID = poolID; };
      if (this.hasUnsavedSMTPChanges()) {
        this.$utils.confirm(this.$t('globals.messages.confirmDiscard'), select);
        return;
      }
      select();
    },

    deleteSMTPPool() {
      if (!this.selectedSMTPPoolID) return;
      this.$utils.confirm(this.$t('organizations.smtpPoolDeleteConfirm'), async () => {
        await this.$api.deleteOrganizationSMTPPool(this.selectedSMTPPoolID, this.selectedOrganizationID);
        await this.loadSMTPPools();
      });
    },

    selectOrganization(organization) {
      this.platformOrganizationID = organization.id;
      this.selectedOrganizationID = organization.id;
      this.activeTab = 0;
    },

    async addMember() {
      await this.$api.addOrganizationMember(this.memberForm, this.selectedOrganizationID);
      this.memberForm = { account: '', role: 'member' };
      await this.refreshSelectedOrganization();
    },

    confirmMemberRoleChange(member, role) {
      if (role === member.role) {
        return;
      }
      const roleLabel = role === 'manager' ? this.$t('organizations.roleManager') : this.$t('organizations.roleMember');
      this.$utils.confirm(
        this.$t('organizations.confirmRoleChange', { name: member.username, role: roleLabel }),
        () => this.changeMemberRole(member, role),
        () => this.resetMemberRoleSelects(),
        { type: 'is-danger' },
      );
    },

    // Buefy's select keeps its own internal state and only re-syncs when the
    // `value` prop changes, so a cancelled or failed change needs a rebuild to
    // show the role the server still holds.
    resetMemberRoleSelects() {
      this.roleRevision += 1;
    },

    async changeMemberRole(member, role) {
      try {
        await this.$api.updateOrganizationMember(member.userId, { role }, this.selectedOrganizationID);
        await this.refresh();
      } catch (error) {
        this.resetMemberRoleSelects();
      }
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

    // The pool audience reply route resolves to the organization's single
    // unified reply mailbox, so the value is loaded from the organization
    // payload and refreshed after saving.
    async loadUnifiedReplyMailbox() {
      const organization = this.selectedOrganization;
      const mailboxID = organization && (organization.replyMailboxId || organization.reply_mailbox_id);
      this.unifiedReplyMailboxID = mailboxID ? Number(mailboxID) : null;
      const mailboxes = await this.$api
        .getReplyMailboxes(Number(this.selectedOrganizationID))
        .catch(() => []);
      this.organizationReplyMailboxes = Array.isArray(mailboxes) ? mailboxes : [];
    },

    async saveUnifiedReplyMailbox() {
      if (!this.selectedOrganizationID || !this.canEditUnifiedReplyMailbox) {
        return;
      }
      this.savingUnifiedReplyMailbox = true;
      try {
        await this.$api.updateOrganizationReplyMailbox(this.selectedOrganizationID, this.unifiedReplyMailboxID);
        this.$utils.toast(this.$t('organizations.unifiedReplyMailboxSaved'));
        await this.refresh();
      } finally {
        this.savingUnifiedReplyMailbox = false;
      }
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

    confirmApproveRequest(request) {
      this.$utils.confirm(
        `${this.$t('organizations.approve')} "${request.requestedName}"?`,
        () => this.reviewRequest(request, true),
        null,
        { type: 'is-danger' },
      );
    },

    confirmRejectRequest(request) {
      this.$utils.prompt(
        this.$t('organizations.rejectReason'),
        { type: 'string', maxlength: 200 },
        (note) => this.reviewRequest(request, false, note),
      );
    },

    async reviewRequest(request, approve, note = '') {
      await this.$api.reviewOrganizationRequest(request.id, { approve, note });
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

<style scoped>
.table-scroll {
  overflow-x: auto;
}

.smtp-pool-layout {
  align-items: flex-start;
}

/* The SMTP workspace is a two-pane editor and needs the full tab width.
   Other organization tabs keep the shared readable .wrap max-width. */
.smtp-pool-wrap {
  width: 100%;
  max-width: none !important;
}

.smtp-pool-sidebar,
.smtp-pool-editor {
  height: 100%;
}

.smtp-pool-sidebar-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
}

.smtp-pool-sidebar-heading .help {
  margin-top: .25rem;
}

.smtp-pool-list {
  display: grid;
  gap: .5rem;
  margin: 1rem 0 1.25rem;
}

.smtp-pool-option {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: .75rem;
  padding: .75rem .85rem;
  border: 1px solid #dbdbdb;
  border-radius: 6px;
  background: #fff;
  color: inherit;
  text-align: left;
  cursor: pointer;
  transition: border-color .15s ease, background-color .15s ease;
}

.smtp-pool-option:hover,
.smtp-pool-option:focus-visible {
  border-color: #3273dc;
  outline: none;
}

.smtp-pool-option.is-active {
  border-color: #3273dc;
  background: #eff5ff;
  box-shadow: 0 0 0 1px #3273dc;
}

.smtp-pool-option-main {
  min-width: 0;
  display: grid;
  gap: .15rem;
}

.smtp-pool-option-main strong {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.smtp-pool-create {
  padding-top: 1rem;
  border-top: 1px solid #ededed;
}

.smtp-pool-delete {
  margin-top: .75rem;
  padding-left: 0;
}

.smtp-pool-editor {
  padding: 1.25rem;
  border: 1px solid #ededed;
  border-radius: 6px;
  background: #fff;
}

.smtp-pool-first-step {
  display: flex;
  align-items: center;
  gap: .5rem;
  min-height: 12rem;
}

@media screen and (max-width: 768px) {
  .smtp-pool-editor {
    padding: 1rem;
  }
}
</style>
