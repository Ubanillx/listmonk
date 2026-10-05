<template>
  <section class="customer_lists">
    <header class="columns page-header">
      <div class="column is-10">
        <h1 class="title is-4 mb-2">
          {{ $t(isPoolGroup ? 'menu.poolLists' : 'menu.allLists') }}
          <span v-if="queryParams.status === 'archived'" class="has-text-grey-light">/ {{ queryParams.status }} </span>
          <span v-if="!isNaN(customer_lists.total)">({{ customer_lists.total }})</span>
        </h1>

        <div class="is-size-7">
          <router-link v-if="queryParams.status !== 'archived'" :to="{ name: listRouteName, query: { status: 'archived' } }">
            {{ $t('globals.buttons.view') }} {{ $t('customer_lists.archived').toLowerCase() }} &rarr;
          </router-link>
          <router-link v-else :to="{ name: listRouteName }">
            {{ $t('globals.buttons.view') }} {{ $t(isPoolGroup ? 'menu.poolLists' : 'menu.allLists').toLowerCase() }} &rarr;
          </router-link>
        </div>
      </div>
      <div class="column has-text-right">
        <b-field v-if="canCreateList" expanded>
          <b-button expanded type="is-primary" icon-left="plus" class="btn-new" @click="showNewForm" data-cy="btn-new">
            {{ $t('globals.buttons.new') }}
          </b-button>
        </b-field>
      </div>
    </header>
    <b-table :data="customer_lists.results" :loading="loading.listsFull" @check-all="onTableCheck" @check="onTableCheck"
      :checked-rows.sync="bulk.checked" hoverable default-sort="createdAt" paginated backend-pagination
      pagination-position="both" @page-change="onPageChange" :current-page="queryParams.page" :per-page="customer_lists.perPage"
      :total="customer_lists.total" :checkable="canManageLists" :is-row-checkable="canDeleteList" backend-sorting @sort="onSort">
      <template #top-left>
        <div class="columns">
          <div class="column is-6">
            <form @submit.prevent="onSearch">
              <b-field>
                <b-input v-model="queryParams.query" name="query" expanded icon="magnify" ref="query" data-cy="query"
                  :placeholder="$t(isPoolGroup ? 'customer_lists.poolSearchPlaceholder' : 'customer_lists.searchPlaceholder')"
                  :aria-label="$t('globals.buttons.search')" />
                <p class="controls">
                  <b-button native-type="submit" type="is-primary" icon-left="magnify" data-cy="btn-query"
                    :aria-label="$t('globals.buttons.search')" />
                </p>
              </b-field>
            </form>
          </div>
        </div>
        <div class="actions" v-if="canManageLists && bulk.checked.length > 0">
          <a class="a" href="#" @click.prevent="deleteLists" data-cy="btn-delete-customer_lists">
            <b-icon icon="trash-can-outline" size="is-small" /> {{ $t('globals.buttons.delete') }}
          </a>
          <span class="a">
            {{ $tc('globals.messages.numSelected', numSelectedLists, { num: numSelectedLists }) }}
            <span v-if="canSelectAllLists && !bulk.all && customer_lists.total > customer_lists.perPage">
              &mdash;
              <a href="#" @click.prevent="onSelectAll" data-cy="select-all-customer_lists">
                {{ $tc('globals.messages.selectAll', customer_lists.total, { num: customer_lists.total }) }}
              </a>
            </span>
          </span>
        </div>
      </template>

      <b-table-column v-slot="props" field="name" :label="$t('globals.fields.name')" header-class="cy-name" sortable
        width="25%" paginated backend-pagination pagination-position="both" :td-attrs="$utils.tdID"
        @page-change="onPageChange">
        <div>
          <a :href="customerListEditHref(props.row)" @click.prevent="showEditForm(props.row)">
            {{ props.row.name }}
          </a>
          <b-taglist>
            <b-tag class="is-small" v-for="t in props.row.tags" :key="t">
              {{ t }}
            </b-tag>
          </b-taglist>
        </div>
      </b-table-column>

      <b-table-column v-slot="props" field="type" :label="$t('globals.fields.type')" header-class="cy-type" sortable
        width="15%">
        <div class="tags">
          <b-tag :class="props.row.type" :data-cy="`type-${props.row.type}`">
            {{ $t(`customer_lists.types.${props.row.type}`) }}
          </b-tag>
          {{ ' ' }}

          <b-tag :class="props.row.optin" :data-cy="`optin-${props.row.optin}`">
            <b-icon :icon="props.row.optin === 'double' ? 'account-check-outline' : 'account-off-outline'"
              size="is-small" />
            {{ ' ' }}
            {{ $t(`customer_lists.optins.${props.row.optin}`) }}
          </b-tag>{{ ' ' }}

          <a v-if="props.row.optin === 'double' && canManageList(props.row)" class="is-size-7 send-optin" href="#"
            @click="$utils.confirm(null, () => createOptinCampaign(props.row))" data-cy="btn-send-optin-campaign">
            <b-tooltip :label="$t('customer_lists.sendOptinCampaign')" type="is-dark">
              <b-icon icon="rocket-launch-outline" size="is-small" />
              {{ $t('customer_lists.sendOptinCampaign') }}
            </b-tooltip>
          </a>
        </div>
      </b-table-column>

      <b-table-column v-slot="props" field="ownerUsername" :label="$t('shared.ownerScope')">
        {{ ownerLabel(props.row) }}
        <b-tag size="is-small" class="is-light">{{ visibilityLabel(props.row.visibility, props.row.type) }}</b-tag>
        <b-tag v-if="transferPendingAt(props.row)" size="is-small" type="is-warning" class="is-light">
          {{ $t('shared.transferPending', { date: $utils.niceDate(transferPendingAt(props.row), true) }) }}
        </b-tag>
      </b-table-column>

      <b-table-column v-slot="props" field="customer_count" :label="$t(isPoolGroup ? 'pool.tabPoolContacts' : 'globals.terms.customers')"
        header-class="cy-customers" sortable>
        <router-link v-if="canViewListCustomers(props.row)" class="customer-count" :to="customerListCustomersRoute(props.row)">
          <strong>{{ $utils.formatNumber(props.row.customerCount) }}</strong>
          <span class="customer-count-action">
            {{ $t('globals.buttons.view') }}
            <b-icon icon="arrow-right" size="is-small" />
          </span>
        </router-link>
        <strong v-else>{{ $utils.formatNumber(props.row.customerCount) }}</strong>
      </b-table-column>

      <b-table-column v-slot="props" field="customer_counts" :label="$t('globals.fields.status')"
        header-class="cy-customer-statuses" width="12%">
        <div class="fields stats">
          <router-link v-for="(count, status) in canViewListCustomers(props.row) ? filterStatuses(props.row) : {}" :key="status"
            class="status-item" :class="status"
            :to="`/customers/customer-lists/${props.row.id}?subscription_status=${status}`">
            <span class="status-label">{{ $tc(`customers.status.${status}`, count) }}</span>
            <strong>{{ $utils.formatNumber(count) }}</strong>
          </router-link>
        </div>
      </b-table-column>

      <b-table-column v-slot="props" field="created_at" :label="$t('globals.fields.createdAt')"
        header-class="cy-created_at" sortable>
        {{ $utils.niceDate(props.row.createdAt) }}
      </b-table-column>
      <b-table-column v-slot="props" field="updated_at" :label="$t('globals.fields.updatedAt')"
        header-class="cy-updated_at" sortable>
        {{ $utils.niceDate(props.row.updatedAt) }}
      </b-table-column>

      <b-table-column v-slot="props" cell-class="actions" align="right">
        <div>
          <router-link v-if="canManageList(props.row)"
            :to="`/campaigns/new?customer_list_id=${props.row.id}`"
            data-cy="btn-campaign">
            <b-tooltip :label="$t('customer_lists.sendCampaign')" type="is-dark">
              <b-icon icon="rocket-launch-outline" size="is-small" />
            </b-tooltip>
          </router-link>

          <a v-if="canManageList(props.row)" href="#"
            @click.prevent="showEditForm(props.row)" data-cy="btn-edit" :aria-label="$t('globals.buttons.edit')">
            <b-tooltip :label="$t('globals.buttons.edit')" type="is-dark">
              <b-icon icon="pencil-outline" size="is-small" />
            </b-tooltip>
          </a>

          <a v-if="props.row.type === 'pool' && canManagePool" href="#" @click.prevent="showPoolManager(props.row)"
            data-cy="btn-manage-pool" :aria-label="$t('customer_lists.poolManage')">
            <b-tooltip :label="$t('customer_lists.poolManage')" type="is-dark">
              <b-icon icon="database-cog-outline" size="is-small" />
            </b-tooltip>
          </a>

          <router-link v-if="canImportList(props.row)"
            :to="{ name: 'import', query: { customer_list_id: props.row.id } }"
            data-cy="btn-import">
            <b-tooltip :label="$t('import.title')" type="is-dark">
              <b-icon icon="file-upload-outline" size="is-small" />
            </b-tooltip>
          </router-link>

          <a v-if="canDeleteList(props.row)" href="#"
            @click.prevent="deleteList(props.row)" data-cy="btn-delete" :aria-label="$t('globals.buttons.delete')">
            <b-tooltip :label="$t('globals.buttons.delete')" type="is-dark">
              <b-icon icon="trash-can-outline" size="is-small" />
            </b-tooltip>
          </a>
        </div>
      </b-table-column>

      <template #empty v-if="!loading.listsFull">
        <empty-placeholder />
      </template>
    </b-table>

    <!-- Add / edit form modal -->
    <b-modal scroll="keep" :aria-modal="true" :active.sync="isFormVisible" :width="600" @close="onFormClose">
      <customer-list-form :data="curItem" :is-editing="isEditing" :list-group="listGroup" @finished="formFinished" />
    </b-modal>

    <b-modal scroll="keep" :aria-modal="true" :active.sync="isPoolVisible" :width="960">
      <div class="modal-card pool-manager-modal" data-cy="pool-manager-modal">
        <header class="modal-card-head"><h4>{{ poolItem && poolItem.name }} · {{ $t('customer_lists.poolManageTitle') }}</h4></header>
        <section class="modal-card-body"><pool-manager v-if="poolItem" :pool="poolItem" /></section>
        <footer class="modal-card-foot"><b-button @click="isPoolVisible = false">{{ $t('globals.buttons.close') }}</b-button></footer>
      </div>
    </b-modal>

    <p v-if="settings['app.cache_slow_queries']" class="has-text-grey">
      *{{ $t('globals.messages.slowQueriesCached') }}
    </p>
  </section>
</template>

<script>
import Vue from 'vue';
import { mapState } from 'vuex';
import EmptyPlaceholder from '../components/EmptyPlaceholder.vue';
import PoolManager from '../components/PoolManager.vue';
import CustomerListForm from './CustomerListForm.vue';
import { isOwnedActiveWorkspaceCustomerList } from '../utils/workspace';

export default Vue.extend({
  components: {
    CustomerListForm,
    EmptyPlaceholder,
    PoolManager,
  },

  data() {
    return {
      // Current customerList item being edited.
      curItem: null,
      isEditing: false,
      isFormVisible: false,
      isPoolVisible: false,
      poolItem: null,
      customer_lists: [],
      queryParams: {
        page: 1,
        query: '',
        orderBy: 'id',
        order: 'asc',
        status: this.$route.query.status || 'active',
      },

      // Table bulk row selection states.
      bulk: {
        checked: [],
        all: false,
      },
    };
  },

  methods: {
    onPageChange(p) {
      this.queryParams.page = p;
      this.getLists();
    },

    onSort(field, direction) {
      this.queryParams.orderBy = field;
      this.queryParams.order = direction;
      this.getLists();
    },

    // Show the edit customerList form.
    customerListEditHref(customerList) {
      return this.$router.resolve({
        name: this.isPoolGroup ? 'poolList' : 'customerList',
        params: { id: customerList.id },
      }).href;
    },

    showEditForm(customerList) {
      this.curItem = customerList;
      this.isFormVisible = true;
      this.isEditing = true;
    },

    // Show the new customerList form.
    showNewForm() {
      this.curItem = { type: this.isPoolGroup ? 'pool' : 'private' };
      this.isFormVisible = true;
      this.isEditing = false;
    },

    showPoolManager(customerList) {
      this.poolItem = customerList;
      this.isPoolVisible = true;
    },

    formFinished() {
      this.getLists();
    },

    onFormClose() {
      if (this.$route.params.id) {
        this.$router.push({ name: this.listRouteName });
      }
    },

    filterStatuses(customerList) {
      const out = { ...customerList.customerStatuses };
      if (customerList.optin === 'single') {
        delete out.unconfirmed;
        delete out.confirmed;
      }
      return out;
    },

    // A new search starts a new result set; keeping the old page number would
    // request a page that may not exist and render an empty table.
    onSearch() {
      this.queryParams.page = 1;
      this.getLists();
    },

    getLists() {
      this.$api.queryLists({
        page: this.queryParams.page,
        query: this.queryParams.query.replace(/[^\p{L}\p{N}\s]/gu, ' '),
        order_by: this.queryParams.orderBy,
        order: this.queryParams.order,
        status: this.queryParams.status,
        type_group: this.listGroup,
      }).then((resp) => {
        this.customer_lists = resp;
      });

      // Also fetch the minimal customer_lists for the global store that appears
      // in dropdown menus on other pages like import and campaigns.
      this.$api.getLists({ minimal: true, per_page: 'all', status: 'active' });
    },

    deleteList(customerList) {
      this.$utils.confirm(
        this.$t('customer_lists.confirmDelete'),
        () => {
          this.$api.deleteList(customerList.id).then(() => {
            this.getLists();

            this.$utils.toast(this.$t('globals.messages.deleted', { name: customerList.name }));
          });
        },
      );
    },

    // Mark all customer_lists in the query as selected.
    onSelectAll() {
      this.bulk.all = true;
    },

    onTableCheck() {
      // Disable bulk.all selection if there are no rows checked in the table.
      if (this.bulk.checked.length !== this.customer_lists.total) {
        this.bulk.all = false;
      }
    },

    deleteLists() {
      const name = this.$tc('globals.terms.customer_list', this.numSelectedCampaigns);

      const fn = () => {
        const params = {};
        if (!this.bulk.all && this.bulk.checked.length > 0) {
          // If 'all' is not selected, delete customer_lists by IDs.
          params.id = this.bulk.checked.map((l) => l.id);
        } else {
          // 'All' is selected, delete by query.
          params.query = this.queryParams.query.replace(/[^\p{L}\p{N}\s]/gu, ' ');
          params.all = this.bulk.all;
          params.type_group = this.listGroup;
          params.status = this.queryParams.status;
        }

        const numSelected = this.numSelectedLists;
        this.$api.deleteLists(params)
          .then(() => {
            // Reset the selection: Buefy only syncs its internal `checkedRows`
            // when the prop changes, so reloading left the stale count in the
            // toolbar and allowed a second delete of ids that no longer exist.
            this.bulk = { checked: [], all: false };
            this.getLists();
            this.$utils.toast(this.$tc(
              'globals.messages.deletedCount',
              numSelected,
              { num: numSelected, name },
            ));
          });
      };

      this.$utils.confirm(this.$tc(
        'globals.messages.confirmDelete',
        this.numSelectedLists,
        { num: this.numSelectedLists, name: name.toLowerCase() },
      ), fn, null, { type: 'is-danger' });
    },

    createOptinCampaign(customerList) {
      const data = {
        name: this.$t('customer_lists.optinTo', { name: customerList.name }),
        subject: this.$t('customer_lists.confirmSub', { name: customerList.name }),
        customer_lists: [customerList.id],
        from_email: this.settings['app.from_email'],
        content_type: 'richtext',
        messenger: 'email',
        type: 'optin',
      };

      this.$api.createCampaign(data).then((d) => {
        this.$router.push({ name: 'campaign', hash: '#content', params: { id: d.id } });
      });
      return false;
    },

    canManageList(customerList) {
      if (customerList.type === 'pool') {
        return this.$canCreateWorkspaceResource('pools:master_manage') && !customerList.organizationId
          && !customerList.organization_id && !customerList.transferPendingAt && !customerList.transfer_pending_at;
      }
      if (customerList.type === 'org_pool_allocation') {
        return false;
      }
      return this.$canManageResource(customerList) && this.$canList(customerList.id, 'customer_list:manage');
    },

    canDeleteList(customerList) {
      return this.canManageList(customerList)
        && (customerList.type === 'pool' || this.$can('customer_lists:delete'));
    },

    canImportList(customerList) {
      if (customerList.type === 'pool') {
        return this.$can('pools:master_manage');
      }
      return this.$can('customers:import') && isOwnedActiveWorkspaceCustomerList(
        customerList,
        this.workspace,
        this.profile && this.profile.id,
      ) && this.$canList(customerList.id, 'customer_list:manage');
    },

    canViewListCustomers(customerList) {
      if (customerList.type === 'pool' || customerList.type === 'org_pool_allocation') {
        return this.$can('pools:get');
      }
      return this.$can('customers:get', 'customers:get_all') && (this.isPlatformAdmin || this.canInspectOrganization
        || isOwnedActiveWorkspaceCustomerList(customerList, this.workspace, this.profile && this.profile.id));
    },

    ownerLabel(resource) {
      if (resource.type === 'org_pool_allocation') {
        return resource.organizationName || resource.organization_name || '-';
      }
      return resource.ownerName || resource.ownerUsername || '-';
    },

    visibilityLabel(visibility, type) {
      if (type === 'pool') {
        return this.$t('customer_lists.visibility.globalPool');
      }
      return {
        private: this.$t('visibility.private'),
        organization: this.$t('visibility.organization'),
        global: this.$t('visibility.global'),
      }[visibility] || this.$t('visibility.private');
    },

    transferPendingAt(resource) {
      return resource.transferPendingAt || resource.transfer_pending_at;
    },

    // Pool lists render their contacts inside the customers view, so every
    // A list's customer count opens its scoped detail; the navigation's
    // public-pool entry opens the aggregate across all accessible pools.
    customerListCustomersRoute(customerList) {
      const isPool = customerList.type === 'pool' || customerList.type === 'org_pool_allocation';
      return {
        name: isPool ? 'poolListContacts' : 'customersCustomerList',
        params: { customerListID: customerList.id },
      };
    },
  },

  computed: {
    ...mapState(['loading', 'settings', 'profile', 'workspace']),

    isPoolGroup() {
      return this.$route.name === 'poolLists' || this.$route.name === 'poolList';
    },

    listGroup() {
      return this.isPoolGroup ? 'pool' : 'private';
    },

    listRouteName() {
      return this.isPoolGroup ? 'poolLists' : 'customerLists';
    },

    canCreateList() {
      return this.isPoolGroup
        ? this.$canCreateWorkspaceResource('pools:master_manage')
        : this.$canCreateWorkspaceResource('customer_lists:manage_all');
    },

    canManageLists() {
      return Array.isArray(this.customer_lists.results) && this.customer_lists.results.some((customerList) => this.canDeleteList(customerList));
    },

    canInspectOrganization() {
      return this.$canInspectOrganization();
    },

    // Organization managers can inspect member customer_lists but must never bulk
    // select them. Cross-page selection is therefore platform-admin only.
    canSelectAllLists() {
      return this.$isPlatformAdmin();
    },

    isPlatformAdmin() {
      return this.$isPlatformAdmin();
    },

    // Organization admins split the primary pool inside their own
    // organization, matching the workspace-scoped manager check used by
    // Templates.vue. The personal workspace has no organization, so the entry
    // stays highest-administrator only there.
    isOrganizationManager() {
      return this.workspace.organizationId > 0 && this.workspace.role === 'manager';
    },

    // Highest administrators manage any organization's pool; organization
    // admins manage the one they are currently in.
    canManagePool() {
      return this.$can('pools:delivery_manage') || (this.$can('pools:manage') && this.workspace.organizationId > 0);
    },

    numSelectedLists() {
      return this.bulk.all ? this.customer_lists.total : this.bulk.checked.length;
    },
  },

  created() {
    this.$root.$on('page.refresh', this.getLists);
  },

  destroyed() {
    this.$root.$off('page.refresh', this.getLists);
  },

  mounted() {
    if (this.$route.params.id) {
      this.$api.getList(parseInt(this.$route.params.id, 10)).then((data) => {
        const isPool = data.type === 'pool' || data.type === 'org_pool_allocation';
        if (isPool !== this.isPoolGroup) {
          this.$router.replace({
            name: isPool ? 'poolList' : 'customerList',
            params: { id: data.id },
          });
          return;
        }
        this.showEditForm(data);
      });
    } else {
      this.getLists();
    }
  },
});
</script>
