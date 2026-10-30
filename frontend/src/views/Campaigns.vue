<template>
  <section class="campaigns">
    <header class="columns page-header">
      <div class="column is-10">
        <h1 class="title is-4">
          {{ $t('globals.terms.campaigns') }}
          <span v-if="!isNaN(campaigns.total)">({{ campaigns.total }})</span>
        </h1>
      </div>
      <div class="column has-text-right">
        <b-field v-if="$canCreateWorkspaceResource('campaigns:manage_all', 'campaigns:manage')" expanded>
          <b-button expanded :to="{ name: 'campaign', params: { id: 'new' } }" tag="router-link" class="btn-new"
            type="is-primary" icon-left="plus" data-cy="btn-new">
            {{ $t('globals.buttons.new') }}
          </b-button>
        </b-field>
      </div>
    </header>
    <paginated-table :data="campaigns.results" :loading="loading.campaigns" :row-class="highlightedRow"
      @check-all="onTableCheck" @check="onTableCheck" :checked-rows.sync="bulk.checked" paginated backend-pagination
      pagination-position="both" @page-change="onPageChange" @per-page-change="onPerPageChange" :current-page="queryParams.page"
      :per-page="campaigns.perPage" :total="campaigns.total" hoverable :checkable="canManageCampaigns"
      :is-row-checkable="canManageCampaign" backend-sorting @sort="onSort">
      <template #top-left>
        <div class="columns">
          <div class="column is-6">
            <form @submit.prevent="onSearch">
              <div>
                <b-field addons class="lm-search-field">
                  <b-input v-model="queryParams.query" name="query" expanded
                    :placeholder="$t('campaigns.queryPlaceholder')" icon="magnify" ref="query" />
                  <p class="controls">
                    <b-button native-type="submit" type="is-primary" icon-left="magnify"
                      :aria-label="$t('globals.buttons.search')" />
                  </p>
                </b-field>
              </div>
            </form>
          </div>
        </div>

        <div class="actions" v-if="canManageCampaigns && bulk.checked.length > 0">
          <a class="a" href="#" @click.prevent="deleteCampaigns" data-cy="btn-delete-campaigns">
            <b-icon icon="trash-can-outline" size="is-small" /> Delete
          </a>
          <span class="a">
            {{ $tc('globals.messages.numSelected', numSelectedCampaigns, { num: numSelectedCampaigns }) }}
            <span v-if="canSelectAllCampaigns && !bulk.all && campaigns.total > campaigns.perPage">
              &mdash;
              <a href="#" @click.prevent="onSelectAll" data-cy="select-all-campaigns">
                {{ $tc('globals.messages.selectAll', campaigns.total, { num: campaigns.total }) }}
              </a>
            </span>
          </span>
        </div>
      </template>

      <b-table-column v-slot="props" field="name" :label="$t('globals.fields.name')" width="26%" sortable
        cell-class="campaign-identity" header-class="cy-name">
        <div class="campaign-name">
          <b-tag v-if="props.row.type === 'optin'" size="is-small">{{ $t('customer_lists.optin') }}</b-tag>
          <router-link :to="{ name: 'campaign', params: { id: props.row.id } }">{{ props.row.name }}</router-link>
          <copy-text :text="props.row.name" hide-text />
        </div>
        <p class="campaign-subject"><copy-text :text="props.row.subject" /></p>
        <div class="campaign-meta">
          <b-tag size="is-small" :class="props.row.status">{{ $t(`campaigns.status.${props.row.status}`) }}</b-tag>
          <span class="spinner is-tiny" v-if="isRunning(props.row.id)"><b-loading :is-full-page="false" active /></span>
          <span v-if="isSheduled(props.row) && props.row.sendAt && !isDone(props.row) && !isRunning(props.row.id)">
            <b-icon icon="alarm" size="is-small" /> {{ $utils.niceDate(props.row.sendAt, true) }}
          </span>
          <span>{{ ownerLabel(props.row) }}</span>
          <b-tag size="is-small" class="is-light">{{ visibilityLabel(props.row.visibility) }}</b-tag>
          <b-tag v-if="transferPendingAt(props.row)" size="is-small" type="is-warning" class="is-light">
            {{ $t('shared.transferPending', { date: $utils.niceDate(transferPendingAt(props.row), true) }) }}
          </b-tag>
        </div>
        <ul class="campaign-audience">
          <li v-for="l in campaignAudienceLists(props.row)" :key="`${l.type}-${l.id}-${l.name}`">
            <router-link v-if="l.id" :to="{ name: l.type === 'pool' ? 'poolListContacts' : 'customersCustomerList', params: { customerListID: l.id } }">
              {{ l.name }}
            </router-link>
            <span v-else>{{ l.name }}</span>
            <b-tag size="is-small" :type="l.type === 'pool' ? 'is-info' : ''" class="is-light">{{ $t(`customer_lists.types.${l.type}`) }}</b-tag>
          </li>
        </ul>
        <b-taglist v-if="props.row.tags && props.row.tags.length"><b-tag size="is-small" v-for="t in props.row.tags" :key="t">{{ t }}</b-tag></b-taglist>
      </b-table-column>

      <b-table-column v-slot="props" field="created_at" :label="$t('campaigns.deliveryStatus')" width="24%" sortable
        cell-class="campaign-delivery" header-class="cy-timestamp">
        <div :set="stats = getCampaignStats(props.row)">
          <div class="campaign-progress-heading">
            <span><strong>{{ $utils.formatNumber(stats.deliverySent) }}</strong><span class="campaign-target"> / {{ $utils.formatNumber(stats.deliveryTotal) }}</span></span>
            <span class="campaign-progress-percent">{{ deliveryProgress(stats).toFixed(1) }}%</span>
          </div>
          <div class="campaign-progress-track" role="meter" :aria-label="$t('campaigns.progress')"
            aria-valuemin="0" aria-valuemax="100" :aria-valuenow="deliveryProgress(stats)">
            <span class="campaign-progress-fill" :style="{ width: `${deliveryProgress(stats)}%` }" />
          </div>
          <div class="campaign-delivery-note">
            <span>{{ $t('campaigns.unsent') }} {{ $utils.formatNumber(stats.deliveryPending) }}</span>
            <span v-if="isDone(props.row) && stats.updatedAt">{{ $utils.niceDate(stats.updatedAt) }} · {{ $t('campaigns.ended') }}</span>
            <span v-else-if="props.row.status === 'deferred' && props.row.nextResumeAt">{{ $utils.niceDate(props.row.nextResumeAt, true) }}</span>
            <span v-else-if="props.row.status === 'scheduled' && props.row.sendAt">{{ $utils.niceDate(props.row.sendAt, true) }}</span>
          </div>
          <div class="campaign-delivery-states" :title="$t('campaigns.deliveryStatusHelp')">
            <span class="delivery-sent">{{ $t('campaigns.sent') }} <strong data-cy="delivery-sent">{{ $utils.formatNumber(stats.deliverySent) }}</strong></span>
            <span :class="{ 'delivery-unavailable': stats.deliveryUnavailable > 0 }">
              {{ $t('campaigns.deliveryUnavailable') }} <strong data-cy="delivery-unavailable">{{ $utils.formatNumber(stats.deliveryUnavailable) }}</strong>
            </span>
            <span>{{ $t('campaigns.deliveryPending') }} <strong data-cy="delivery-pending">{{ $utils.formatNumber(stats.deliveryPending) }}</strong></span>
          </div>
        </div>
      </b-table-column>

      <b-table-column v-slot="props" field="interaction" :label="$t('campaigns.customerInteraction')" width="16%" cell-class="campaign-interaction">
        <div class="campaign-dual-metrics">
          <div><strong>{{ $utils.formatNumber(props.row.views) }}</strong><span>{{ $t('campaigns.views') }}</span></div>
          <div><strong>{{ $utils.formatNumber(props.row.clicks) }}</strong><span>{{ $t('campaigns.clicks') }}</span></div>
        </div>
      </b-table-column>

      <b-table-column v-slot="props" field="sendErrors" :label="$t('campaigns.deliveryErrors')" width="9%" cell-class="campaign-error-column">
        <div class="campaign-send-errors" :set="stats = getCampaignStats(props.row)" :class="{ 'has-text-danger': stats.sendErrors > 0 }"
          :title="$t('campaigns.deliveryErrorsHelp')">
          <a v-if="stats.sendErrors > 0 && $canReadCampaignSendErrors(props.row)" href="#" @click.prevent="sendErrorCampaign = props.row"
            :aria-label="$t('campaigns.sendErrorDetails')">{{ $t('campaigns.deliveryHasErrors') }}</a>
          <span v-else>{{ $t(stats.sendErrors > 0 ? 'campaigns.deliveryHasErrors' : 'campaigns.deliveryNoErrors') }}</span>
        </div>
      </b-table-column>

      <b-table-column v-slot="props" field="unsubscribeActivity" :label="$t('campaigns.unsubscribeActivity')" width="10%" cell-class="campaign-unsubscribe-column">
        <div class="campaign-unsubscribes" :set="stats = getCampaignStats(props.row)" :title="$t('campaigns.unsubscribesHelp')">
          <a v-if="$canReadCampaignUnsubscribes(props.row) && (stats.unsubscribeActivity || 0) > 0" href="#"
            @click.prevent="unsubscribeCampaign = props.row" :aria-label="$t('campaigns.unsubscribeDetails')">{{ $utils.formatNumber(stats.unsubscribeActivity || 0) }}</a>
          <span v-else>{{ $utils.formatNumber(stats.unsubscribeActivity || 0) }}</span>
          <span class="campaign-metric-caption">{{ $t('campaigns.uniqueRecipients') }}</span>
        </div>
      </b-table-column>

      <b-table-column v-slot="props" cell-class="actions" width="72" align="right">
        <div>
          <!-- start / pause / resume / scheduled -->
          <template v-if="canManageCampaign(props.row)">
            <a v-if="canStart(props.row)" href="#"
              @click.prevent="confirmCampaignOperation(props.row, $t('campaigns.start'), $t('campaigns.confirmStart'), () => changeCampaignStatus(props.row, 'running'))"
              data-cy="btn-start" :aria-label="$t('campaigns.start')">
              <b-tooltip :label="$t('campaigns.start')" type="is-dark">
                <b-icon icon="rocket-launch-outline" size="is-small" />
              </b-tooltip>
            </a>

            <a v-if="canPause(props.row)" href="#"
              @click.prevent="confirmCampaignOperation(props.row, $t('campaigns.pause'), $t('campaigns.confirmPause'), () => changeCampaignStatus(props.row, 'paused'))" data-cy="btn-pause"
              :aria-label="$t('campaigns.pause')">
              <b-tooltip :label="$t('campaigns.pause')" type="is-dark">
                <b-icon icon="pause-circle-outline" size="is-small" />
              </b-tooltip>
            </a>

            <a v-if="canResume(props.row)" href="#"
              @click.prevent="confirmCampaignOperation(props.row, $t('campaigns.send'), $t('campaigns.confirmStart'), () => changeCampaignStatus(props.row, 'running'))"
              data-cy="btn-resume" :aria-label="$t('campaigns.send')">
              <b-tooltip :label="$t('campaigns.send')" type="is-dark">
                <b-icon icon="rocket-launch-outline" size="is-small" />
              </b-tooltip>
            </a>

            <a v-if="canSchedule(props.row)" href="#"
              @click.prevent="confirmCampaignOperation(props.row, $t('campaigns.schedule'), $t('campaigns.confirmSchedule'), () => changeCampaignStatus(props.row, 'scheduled'))"
              data-cy="btn-schedule" :aria-label="$t('campaigns.schedule')">
              <b-tooltip :label="$t('campaigns.schedule')" type="is-dark">
                <b-icon icon="clock-start" size="is-small" />
              </b-tooltip>
            </a>

            <!-- placeholder for finished campaigns -->
            <a v-if="!canCancel(props.row) && !canSchedule(props.row) && !canStart(props.row)" href="#" data-disabled
              aria-label=" ">
              <b-icon icon="rocket-launch-outline" size="is-small" />
            </a>

            <a v-if="canCancel(props.row)" href="#"
              @click.prevent="confirmCampaignOperation(props.row, $t('globals.buttons.cancel'), $t('campaigns.confirmCancel'), () => changeCampaignStatus(props.row, 'cancelled'), true)"
              data-cy="btn-cancel" :aria-label="$t('globals.buttons.cancel')">
              <b-tooltip :label="$t('globals.buttons.cancel')" type="is-dark">
                <b-icon icon="cancel" size="is-small" />
              </b-tooltip>
            </a>
            <a v-else href="#" data-disabled aria-label=" ">
              <b-icon icon="cancel" size="is-small" />
            </a>
          </template>

          <a href="#" @click.prevent="previewCampaign(props.row)" data-cy="btn-preview"
            :aria-label="$t('campaigns.preview')">
            <b-tooltip :label="$t('campaigns.preview')" type="is-dark">
              <b-icon icon="file-find-outline" size="is-small" />
            </b-tooltip>
          </a>
          <a v-if="$can('campaigns:manage', 'campaigns:manage_all')" href="#" @click.prevent="openCloneCampaign(props.row)" data-cy="btn-clone"
            :aria-label="$t('globals.buttons.clone')">
            <b-tooltip :label="$t('globals.buttons.clone')" type="is-dark">
              <b-icon icon="file-multiple-outline" size="is-small" />
            </b-tooltip>
          </a>
          <router-link
            v-if="$canViewCampaignAnalytics(props.row)"
            :to="{ name: 'campaignAnalytics', query: { id: props.row.id } }">
            <b-tooltip :label="$t('globals.terms.analytics')" type="is-dark">
              <b-icon icon="chart-bar" size="is-small" />
            </b-tooltip>
          </router-link>
          <a v-if="canManageCampaign(props.row)" href="#"
            @click.prevent="confirmCampaignOperation(props.row, $t('globals.buttons.delete'), $t('campaigns.confirmDelete', { name: props.row.name }), () => deleteCampaign(props.row), true)"
            data-cy="btn-delete" :aria-label="$t('globals.buttons.delete')">
            <b-icon icon="trash-can-outline" size="is-small" />
          </a>
        </div>
      </b-table-column>

      <template #empty v-if="!loading.campaigns">
        <empty-placeholder />
      </template>
    </paginated-table>

    <campaign-preview v-if="previewItem" type="campaign" :id="previewItem.id" :title="previewItem.name" :media="previewItem.media || []"
      :template-id="previewItem.templateId" :content-type="previewItem.contentType"
      @close="closePreview" />

    <campaign-send-errors v-if="sendErrorCampaign" :campaign="sendErrorCampaign" @close="sendErrorCampaign = null" />
    <campaign-unsubscribes v-if="unsubscribeCampaign" :campaign="unsubscribeCampaign" @close="unsubscribeCampaign = null" />

    <b-modal scroll="keep" :aria-modal="true" :active.sync="isCloneFormVisible" :width="520">
      <div class="modal-card content" style="width: auto">
        <header class="modal-card-head"><h4>{{ $t('campaigns.copyTitle') }}</h4></header>
        <section class="modal-card-body">
          <b-field :label="$t('globals.fields.name')" label-position="on-border">
            <b-input v-model.trim="cloneForm.name" maxlength="200" required />
          </b-field>
          <b-field :label="$t('campaigns.targetLocation')" label-position="on-border">
            <b-select v-model.number="cloneForm.targetOrganizationID" expanded>
              <option :value="0">{{ $t('organizations.personalSpace') }}</option>
              <option v-for="organization in organizations" :key="organization.id" :value="organization.id">
                {{ organization.name }}
              </option>
            </b-select>
          </b-field>
        </section>
        <footer class="modal-card-foot has-text-right">
          <b-button @click="isCloneFormVisible = false">{{ $t('globals.buttons.close') }}</b-button>
          <b-button type="is-primary" :disabled="!cloneForm.name" :loading="loading.campaigns" @click="cloneCampaign">
            {{ $t('globals.buttons.clone') }}
          </b-button>
        </footer>
      </div>
    </b-modal>
  </section>
</template>

<script>
import Vue from 'vue';
import { mapState } from 'vuex';
import CampaignPreview from '../components/CampaignPreview.vue';
import CampaignSendErrors from '../components/CampaignSendErrors.vue';
import CampaignUnsubscribes from '../components/CampaignUnsubscribes.vue';
import CopyText from '../components/CopyText.vue';
import EmptyPlaceholder from '../components/EmptyPlaceholder.vue';

export default Vue.extend({
  components: {
    CampaignPreview,
    CampaignSendErrors,
    CampaignUnsubscribes,
    EmptyPlaceholder,
    CopyText,
  },

  data() {
    return {
      previewItem: null,
      sendErrorCampaign: null,
      unsubscribeCampaign: null,
      isCloneFormVisible: false,
      cloneItem: null,
      cloneForm: {
        name: '',
        targetOrganizationID: 0,
      },
      queryParams: {
        perPage: undefined,
        page: 1,
        query: '',
        orderBy: 'created_at',
        order: 'desc',
      },
      pollID: null,
      campaignStatsData: {},

      // Table bulk row selection states.
      bulk: {
        checked: [],
        all: false,
      },
    };
  },

  methods: {
    campaignAudienceLists(campaign) {
      const lists = (campaign.customerLists || []).map((list) => ({ ...list, type: 'private' }));
      const poolIDs = new Set();
      (campaign.customerPools || []).forEach((pool) => {
        const id = Number(pool.poolId);
        if (id > 0 && !poolIDs.has(id)) {
          poolIDs.add(id);
          lists.push({
            id,
            name: pool.name || this.$t('campaigns.poolFallback', { id }),
            type: 'pool',
          });
        }
      });
      return lists;
    },

    // Campaign statuses.
    canStart(c) {
      return this.canManageCampaign(c) && this.$can('campaigns:send')
        && c.status === 'draft' && !c.sendAt;
    },
    canSchedule(c) {
      return this.canManageCampaign(c) && this.$can('campaigns:schedule')
        && (c.status === 'draft' || c.status === 'paused' || c.status === 'deferred') && c.sendAt;
    },
    canPause(c) {
      return this.canManageCampaign(c) && this.$can('campaigns:control') && c.status === 'running';
    },
    canCancel(c) {
      return this.canManageCampaign(c) && this.$can('campaigns:control')
        && (c.status === 'running' || c.status === 'paused' || c.status === 'deferred');
    },
    canResume(c) {
      return this.canManageCampaign(c) && this.$can('campaigns:send')
        && (c.status === 'paused' || c.status === 'deferred');
    },
    isSheduled(c) {
      return c.status === 'scheduled' || c.sendAt !== null;
    },
    isDone(c) {
      return c.status === 'finished' || c.status === 'cancelled';
    },

    isRunning(id) {
      if (id in this.campaignStatsData) {
        return true;
      }
      return false;
    },

    highlightedRow(data) {
      if (data.status === 'running') {
        return ['running'];
      }
      return '';
    },

    onPerPageChange(size) {
      this.queryParams.perPage = size;
      this.onPageChange(1);
    },

    onPageChange(p) {
      this.queryParams.page = p;
      this.getCampaigns();
    },

    // A new search or sort starts a new result set. Keeping the previous page
    // number requested a page that may no longer exist and rendered an empty
    // table that looked like "the search found nothing".
    onSearch() {
      this.queryParams.page = 1;
      this.getCampaigns();
    },

    onSort(field, direction) {
      this.queryParams.orderBy = field;
      this.queryParams.order = direction;
      this.queryParams.page = 1;
      this.getCampaigns();
    },

    // Campaign actions.
    previewCampaign(c) {
      this.previewItem = c;
    },

    closePreview() {
      this.previewItem = null;
    },

    getCampaigns() {
      this.$api.getCampaigns({
        page: this.queryParams.page,
        per_page: this.queryParams.perPage,
        query: this.queryParams.query.replace(/[^\p{L}\p{N}\s]/gu, ' '),
        order_by: this.queryParams.orderBy,
        order: this.queryParams.order,
        no_body: true,
      });
    },

    // Stats returns the campaign object with stats (sent, toSend etc.)
    // if there's live stats available for running campaigns. Otherwise,
    // it returns the incoming campaign object that has the static stats
    // values.
    getCampaignStats(c) {
      if (c.id in this.campaignStatsData) {
        return { ...c, ...this.campaignStatsData[c.id] };
      }
      return c;
    },

    deliveryProgress(stats) {
      return stats.deliveryTotal > 0 ? Math.min(100, Math.max(0, (stats.deliverySent / stats.deliveryTotal) * 100)) : 0;
    },

    pollStats() {
      // Clear any running status polls.
      clearInterval(this.pollID);

      // Poll for the status as long as the import is running.
      this.pollID = setInterval(() => {
        this.$api.getCampaignStats().then((data) => {
          // Refresh durable rows when any running campaign leaves the live set,
          // including while other campaigns are still running.
          const activeIDs = new Set(data.map((campaign) => String(campaign.id)));
          if (Object.keys(this.campaignStatsData).some((id) => !activeIDs.has(id))) this.getCampaigns();
          // Stop polling. No running campaigns.
          if (data.length === 0) {
            clearInterval(this.pollID);

            // There were running campaigns and stats earlier. Clear them
            // and refetch the campaigns customer_list with up-to-date fields.
            this.campaignStatsData = {};
          } else {
            // Turn the customer_list of campaigns [{id: 1, ...}, {id: 2, ...}] into
            // a map indexed by the id: {1: {}, 2: {}}.
            this.campaignStatsData = data.reduce((obj, cur) => ({ ...obj, [cur.id]: cur }), {});
          }
        }, () => {
          clearInterval(this.pollID);
        });
      }, 1000);
    },

    changeCampaignStatus(c, status) {
      this.$api.changeCampaignStatus(c.id, status).then(() => {
        this.$utils.toast(this.$t('campaigns.statusChanged', { name: c.name, status }));
        this.getCampaigns();
        this.pollStats();
      });
    },

    isAdminOtherOwner(campaign) {
      const ownerID = Number(campaign.ownerUserId || campaign.owner_user_id);
      return this.$isPlatformAdmin() && ownerID > 0 && ownerID !== Number(this.profile && this.profile.id);
    },

    confirmCampaignOperation(campaign, action, ordinaryMessage, callback, danger = false) {
      const otherOwner = this.isAdminOtherOwner(campaign);
      const message = otherOwner ? this.$t('campaigns.adminOtherOwnerConfirm', {
        action, name: campaign.name, owner: this.ownerLabel(campaign),
      }) : ordinaryMessage;
      let type;
      if (danger) type = 'is-danger';
      else if (otherOwner) type = 'is-warning';
      this.$utils.confirm(message, callback, () => {}, {
        type,
        focusOn: otherOwner ? 'cancel' : undefined,
      });
    },

    openCloneCampaign(campaign) {
      this.cloneItem = campaign;
      this.cloneForm = {
        name: this.$t('campaigns.copyOf', { name: campaign.name }),
        targetOrganizationID: Number(this.workspace.organizationId) || 0,
      };
      this.isCloneFormVisible = true;
    },

    async cloneCampaign() {
      if (!this.cloneItem || !this.cloneForm.name) {
        return;
      }
      const currentOrganizationID = Number(this.workspace.organizationId) || 0;
      const targetOrganizationID = Number(this.cloneForm.targetOrganizationID) || 0;
      const request = { name: this.cloneForm.name };
      if (targetOrganizationID !== currentOrganizationID) {
        request.target_organization_id = targetOrganizationID;
      }
      const clone = async () => {
        const campaign = await this.$api.cloneCampaign(this.cloneItem.id, request);
        this.isCloneFormVisible = false;
        this.$router.push({ name: 'campaign', params: { id: campaign.id } });
      };
      if (this.isAdminOtherOwner(this.cloneItem)) {
        this.confirmCampaignOperation(this.cloneItem, this.$t('globals.buttons.clone'), null, clone);
      } else {
        await clone();
      }
    },

    canManageCampaign(campaign) {
      return this.$canManageResource(campaign, 'campaigns:manage_all', 'campaigns:manage');
    },

    ownerLabel(resource) {
      return resource.ownerName || resource.ownerUsername || '-';
    },

    visibilityLabel(visibility) {
      return {
        private: this.$t('visibility.private'),
        organization: this.$t('visibility.organization'),
        global: this.$t('visibility.global'),
      }[visibility] || this.$t('visibility.private');
    },

    transferPendingAt(resource) {
      return resource.transferPendingAt || resource.transfer_pending_at;
    },

    deleteCampaign(c) {
      this.$api.deleteCampaign(c.id).then(() => {
        this.getCampaigns();
        this.$utils.toast(this.$t('globals.messages.deleted', { name: c.name }));
      });
    },

    // Mark all campaigns in the query as selected.
    onSelectAll() {
      this.bulk.all = true;
    },

    onTableCheck() {
      // Disable bulk.all selection if there are no rows checked in the table.
      if (this.bulk.checked.length !== this.campaigns.total) {
        this.bulk.all = false;
      }
    },

    deleteCampaigns() {
      const name = this.$tc('globals.terms.campaign', this.numSelectedCampaigns);

      const fn = () => {
        const params = {};
        if (!this.bulk.all && this.bulk.checked.length > 0) {
          // If 'all' is not selected, delete campaigns by IDs.
          params.id = this.bulk.checked.map((c) => c.id);
        } else {
          // 'All' is selected, delete by query.
          params.query = this.queryParams.query.replace(/[^\p{L}\p{N}\s]/gu, ' ');
          params.all = this.bulk.all;
        }

        const numSelected = this.numSelectedCampaigns;
        this.$api.deleteCampaigns(params)
          .then(() => {
            // Buefy only syncs its internal selection when the `checkedRows`
            // prop changes, so reloading the data left the toolbar showing the
            // previous count (and allowed a second delete of stale ids).
            this.bulk = { checked: [], all: false };
            this.getCampaigns();
            this.$utils.toast(this.$tc(
              'globals.messages.deletedCount',
              numSelected,
              { num: numSelected, name },
            ));
          });
      };

      const confirmation = this.$tc(
        'globals.messages.confirmDelete',
        this.numSelectedCampaigns,
        { num: this.numSelectedCampaigns, name: name.toLowerCase() },
      );
      const hasOtherOwners = this.bulk.all ? this.$isPlatformAdmin() : this.bulk.checked.some(this.isAdminOtherOwner);
      const message = hasOtherOwners ? `${confirmation} ${this.$t('campaigns.adminBulkDeleteWarning')}` : confirmation;
      this.$utils.confirm(message, fn, null, { type: 'is-danger', focusOn: 'cancel' });
    },
  },

  computed: {
    ...mapState(['campaigns', 'loading', 'workspace', 'organizations', 'profile']),

    canManageCampaigns() {
      return this.$canCreateWorkspaceResource('campaigns:manage_all', 'campaigns:manage');
    },

    // The page result can include read-only organization and global campaigns.
    // Only a platform administrator can safely select every result across
    // pages; other callers keep the row-level ownership controls.
    canSelectAllCampaigns() {
      return this.profile.userRole && Number(this.profile.userRole.id) === 1;
    },

    numSelectedCampaigns() {
      return this.bulk.all ? this.campaigns.total : this.bulk.checked.length;
    },
  },

  created() {
    this.$root.$on('page.refresh', this.getCampaigns);
  },

  mounted() {
    this.getCampaigns();
    this.pollStats();
  },

  destroyed() {
    this.$root.$off('page.refresh', this.getCampaigns);
    clearInterval(this.pollID);
  },
});
</script>

<style lang="scss">
@use '../assets/styles/views/campaigns';
</style>
