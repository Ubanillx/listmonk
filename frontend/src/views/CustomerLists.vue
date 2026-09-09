<template>
  <section class="customer_lists">
    <header class="columns page-header">
      <div class="column is-10">
        <h1 class="title is-4 mb-2">
          {{ $t('globals.terms.customer_lists') }}
          <span v-if="queryParams.status === 'archived'" class="has-text-grey-light">/ {{ queryParams.status }} </span>
          <span v-if="!isNaN(customer_lists.total)">({{ customer_lists.total }})</span>
        </h1>

        <div class="is-size-7">
          <router-link v-if="queryParams.status !== 'archived'" :to="{ name: 'customerLists', query: { status: 'archived' } }">
            {{ $t('globals.buttons.view') }} {{ $t('customer_lists.archived').toLowerCase() }} &rarr;
          </router-link>
          <router-link v-else :to="{ name: 'customerLists' }">
            {{ $t('globals.buttons.view') }} {{ $t('menu.allLists').toLowerCase() }} &rarr;
          </router-link>
        </div>
      </div>
      <div class="column has-text-right">
        <b-field v-if="$canCreateWorkspaceResource('customer_lists:manage_all')" expanded>
          <b-button expanded type="is-primary" icon-left="plus" class="btn-new" @click="showNewForm" data-cy="btn-new">
            {{ $t('globals.buttons.new') }}
          </b-button>
        </b-field>
      </div>
    </header>
    <div class="mb-4"><export-button kind="lists" :filters="queryParams" :selected="bulk.all ? [] : bulk.checked" /></div>

    <b-table :data="customer_lists.results" :loading="loading.listsFull" @check-all="onTableCheck" @check="onTableCheck"
      :checked-rows.sync="bulk.checked" hoverable default-sort="createdAt" paginated backend-pagination
      pagination-position="both" @page-change="onPageChange" :current-page="queryParams.page" :per-page="customer_lists.perPage"
      :total="customer_lists.total" :checkable="canManageLists" :is-row-checkable="canManageList" backend-sorting @sort="onSort">
      <template #top-left>
        <div class="columns">
          <div class="column is-6">
            <form @submit.prevent="getLists">
              <b-field>
                <b-input v-model="queryParams.query" name="query" expanded icon="magnify" ref="query" data-cy="query" />
                <p class="controls">
                  <b-button native-type="submit" type="is-primary" icon-left="magnify" data-cy="btn-query" />
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
          <a :href="`/customer-lists/${props.row.id}`" @click.prevent="showEditForm(props.row)">
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

      <b-table-column v-slot="props" field="ownerUsername" label="所属用户">
        {{ ownerLabel(props.row) }}
        <b-tag size="is-small" class="is-light">{{ visibilityLabel(props.row.visibility) }}</b-tag>
        <b-tag v-if="transferPendingAt(props.row)" size="is-small" type="is-warning" class="is-light">
          待转移 {{ $utils.niceDate(transferPendingAt(props.row), true) }}
        </b-tag>
      </b-table-column>

      <b-table-column v-slot="props" field="customer_count" :label="$t('globals.terms.customers')"
        header-class="cy-customers" numeric sortable centered>
        <router-link :to="`/customers/customer-lists/${props.row.id}`">
          {{ $utils.formatNumber(props.row.customerCount) }}
          <span class="is-size-7 view">{{ $t('globals.buttons.view') }}</span>
        </router-link>
      </b-table-column>

      <b-table-column v-slot="props" field="customer_counts" header-class="cy-customers" width="10%">
        <div class="fields stats">
          <p v-for="(count, status) in filterStatuses(props.row)" :key="status">
            <label for="#">{{ $tc(`customers.status.${status}`, count) }}</label>
            <router-link :to="`/customers/customer-lists/${props.row.id}?subscription_status=${status}`" :class="status">
              {{ $utils.formatNumber(count) }}
            </router-link>
          </p>
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

          <a v-if="props.row.type === 'pool'" href="#" @click.prevent="showPoolManager(props.row)"
            data-cy="btn-manage-pool" aria-label="管理公海">
            <b-tooltip label="管理公海" type="is-dark">
              <b-icon icon="database-cog-outline" size="is-small" />
            </b-tooltip>
          </a>

          <router-link v-if="canManageList(props.row)"
            :to="{ name: 'import', query: { customer_list_id: props.row.id } }"
            data-cy="btn-import">
            <b-tooltip :label="$t('import.title')" type="is-dark">
              <b-icon icon="file-upload-outline" size="is-small" />
            </b-tooltip>
          </router-link>

          <a v-if="canManageList(props.row)" href="#"
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
      <customer-list-form :data="curItem" :is-editing="isEditing" @finished="formFinished" />
    </b-modal>

    <b-modal scroll="keep" :aria-modal="true" :active.sync="isPoolVisible" :width="960">
      <div class="modal-card pool-manager-modal" data-cy="pool-manager-modal">
        <header class="modal-card-head"><h4>{{ poolItem && poolItem.name }} · 公海运营管理</h4></header>
        <section class="modal-card-body"><pool-manager v-if="poolItem" :pool="poolItem" /></section>
        <footer class="modal-card-foot"><b-button @click="isPoolVisible = false">关闭</b-button></footer>
      </div>
    </b-modal>

    <p v-if="settings['app.cache_slow_queries']" class="has-text-grey">
      *{{ $t('globals.messages.slowQueriesCached') }}
      <a href="https://listmonk.app/docs/maintenance/performance/" target="_blank" rel="noopener noreferer"
        class="has-text-grey">
        <b-icon icon="link-variant" /> {{ $t('globals.buttons.learnMore') }}
      </a>
    </p>
  </section>
</template>

<script>
import Vue from 'vue';
import { mapState } from 'vuex';
import EmptyPlaceholder from '../components/EmptyPlaceholder.vue';
import PoolManager from '../components/PoolManager.vue';
import CustomerListForm from './CustomerListForm.vue';

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
    showEditForm(customerList) {
      this.curItem = customerList;
      this.isFormVisible = true;
      this.isEditing = true;
    },

    // Show the new customerList form.
    showNewForm() {
      this.curItem = {};
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
        this.$router.push({ name: 'customerLists' });
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

    getLists() {
      this.$api.queryLists({
        page: this.queryParams.page,
        query: this.queryParams.query.replace(/[^\p{L}\p{N}\s]/gu, ' '),
        order_by: this.queryParams.orderBy,
        order: this.queryParams.order,
        status: this.queryParams.status,
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
        }

        this.$api.deleteLists(params)
          .then(() => {
            this.getLists();
            this.$utils.toast(this.$tc(
              'globals.messages.deletedCount',
              this.numSelectedLists,
              { num: this.numSelectedLists, name },
            ));
          });
      };

      this.$utils.confirm(this.$tc(
        'globals.messages.confirmDelete',
        this.numSelectedLists,
        { num: this.numSelectedLists, name: name.toLowerCase() },
      ), fn);
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
      return this.$canManageResource(customerList) && this.$canList(customerList.id, 'customer_list:manage');
    },

    ownerLabel(resource) {
      return resource.ownerName || resource.ownerUsername || '-';
    },

    visibilityLabel(visibility) {
      return {
        private: '个人私有',
        organization: '组织共享',
      }[visibility] || '个人私有';
    },

    transferPendingAt(resource) {
      return resource.transferPendingAt || resource.transfer_pending_at;
    },
  },

  computed: {
    ...mapState(['loading', 'settings', 'profile']),

    canManageLists() {
      return Array.isArray(this.customer_lists.results) && this.customer_lists.results.some((customerList) => this.canManageList(customerList));
    },

    canInspectOrganization() {
      return this.$canInspectOrganization();
    },

    // Organization managers can inspect member customer_lists but must never bulk
    // select them. Cross-page selection is therefore platform-admin only.
    canSelectAllLists() {
      return this.profile.userRole && Number(this.profile.userRole.id) === 1;
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
        this.showEditForm(data);
      });
    } else {
      this.getLists();
    }
  },
});
</script>
