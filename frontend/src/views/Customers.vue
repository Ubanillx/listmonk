<template>
  <section class="customers">
    <header class="columns page-header">
      <div class="column is-10">
        <h1 class="title is-4">
          {{ $t('globals.terms.customers') }}
          <span v-if="dataCount !== null">
            (<span data-cy="count">{{ dataCount }}</span>)
          </span>
          <span v-if="activeList">
            &raquo; {{ activeList.name }}
            <b-tag v-if="isPoolList" size="is-small" class="is-light" data-cy="pool-list-type">
              {{ $t(`customer_lists.types.${activeList.type}`) }}
            </b-tag>
            <span v-if="queryParams.subStatus" class="has-text-grey has-text-weight-normal is-capitalized">({{
              queryParams.subStatus }})</span>
          </span>
        </h1>
        <p v-if="isPoolList" class="help" data-cy="pool-list-help">{{ $t('pool.listViewHelp') }}</p>
      </div>
      <div class="column has-text-right">
        <b-field v-if="canManageCustomers && !isPoolList" expanded>
          <b-button expanded type="is-primary" icon-left="plus" @click="showNewForm" data-cy="btn-new" class="btn-new">
            {{ $t('globals.buttons.new') }}
          </b-button>
        </b-field>
        <b-field v-else-if="isPoolList && isFirstLevelPool && canManagePoolContacts" expanded>
          <b-button expanded type="is-primary" icon-left="plus" @click="isPoolFormVisible = true" data-cy="btn-new-pool-contact"
            class="btn-new">
            {{ $t('globals.buttons.new') }}
          </b-button>
        </b-field>
      </div>
    </header>

    <!-- One customer area with two data sources: ordinary customers and the
         contacts of a public-pool list. The tabs keep the two stores visually
         in the same place. -->
    <div v-if="!$route.params.id" class="tabs is-boxed customer-area-tabs" data-cy="customer-area-tabs">
      <ul>
        <li :class="{ 'is-active': !isPoolList }">
          <router-link :to="{ name: 'customers' }" data-cy="tab-all-customers">
            {{ $t('menu.allCustomers') }}
          </router-link>
        </li>
        <li :class="{ 'is-active': isPoolList }">
          <a href="#" data-cy="tab-pool-contacts" @click.prevent="goToPoolTab">{{ $t('pool.tabPoolContacts') }}</a>
        </li>
      </ul>
    </div>
    <section v-if="listState !== 'error'" class="customers-controls">
      <div class="columns">
        <div class="column is-8">
          <form @submit.prevent="onSubmit">
            <div>
              <b-field addons>
                <b-input @input="onSimpleQueryInput" v-model="queryInput" expanded
                  :placeholder="isPoolList ? $t('pool.searchPlaceholder') : $t('customers.queryPlaceholder')"
                  icon="magnify" ref="query"
                  :disabled="isSearchAdvanced" :data-cy="isPoolList ? 'pool-search' : 'search'" />
                <p class="controls">
                  <b-button native-type="submit" type="is-primary" icon-left="magnify" :disabled="isSearchAdvanced"
                    data-cy="btn-search" />
                </p>
              </b-field>

              <div v-if="isSearchAdvanced && !isPoolList">
                <b-input v-model="queryParams.queryExp" @keydown.native.enter="onAdvancedQueryEnter" type="textarea"
                  ref="queryExp" placeholder="customers.name LIKE '%user%' or customers.status='blocklisted'"
                  data-cy="query" />
                <span class="is-size-6 has-text-grey">
                  {{ $t('customers.advancedQueryHelp') }}.
                </span>
                <div class="buttons">
                  <b-button native-type="submit" type="is-primary" icon-left="magnify" data-cy="btn-query">
                    {{
                      $t('customers.query') }}
                  </b-button>
                  <b-button @click.prevent="toggleAdvancedSearch" icon-left="cancel" data-cy="btn-query-reset">
                    {{ $t('customers.reset') }}
                  </b-button>
                </div>
              </div><!-- advanced query -->
            </div>
          </form>
          <div v-if="!isSearchAdvanced && !isPoolList" class="toggle-advanced">
            <a href="#" @click.prevent="toggleAdvancedSearch" data-cy="btn-advanced-search">
              <b-icon icon="cog-outline" size="is-small" />
              {{ $t('customers.advancedQuery') }}
            </a>
          </div>
        </div><!-- search -->
      </div>
    </section><!-- control -->

    <br />
    <!-- Pool lists keep their contacts in a separate store and are rendered
         with the same table shape, toolbar and pagination as ordinary
         customers. Buefy's b-table does not forward `data-cy` to the DOM, so
         the pool table is wrapped for tests. -->
    <div v-if="isPoolList" data-cy="pool-contacts-table">
      <b-table :data="pool.results" :loading="poolLoading" :mobile-cards="false" hoverable
        paginated backend-pagination pagination-position="both" @page-change="onPoolPageChange"
        :current-page="pool.page" :per-page="pool.perPage" :total="pool.total"
        :checked-rows.sync="poolBulk.checked" :checkable="canManagePoolContacts" backend-sorting
        @sort="onPoolSort">
        <template #top-left>
          <div class="actions">
            <a v-if="canExportPoolContacts" class="a" href="#" @click.prevent="exportPoolContacts"
              data-cy="btn-export-pool-contacts">
              <b-icon icon="cloud-download-outline" size="is-small" />
              {{ $t('customers.export') }}
            </a>
            <template v-if="poolBulk.checked.length > 0">
              <a v-if="canManagePoolContacts && isFirstLevelPool" class="a" href="#" @click.prevent="showPoolAssignForm(null)"
                data-cy="btn-assign-pool-contacts">
                <b-icon icon="account-arrow-right-outline" size="is-small" /> {{ $t('pool.assignSelected') }}
              </a>
              <a v-if="canManagePoolContacts && isAllocationList && hasRemovablePoolContacts" class="a" href="#"
                @click.prevent="showPoolRemovalForm(null)" data-cy="btn-remove-pool-contacts">
                <b-icon icon="account-off-outline" size="is-small" /> {{ $t('pool.removeSelected') }}
              </a>
              <a v-if="canManagePoolContacts && isAllocationList && hasRestorablePoolContacts" class="a" href="#"
                @click.prevent="restorePoolContacts(null)" data-cy="btn-restore-pool-contacts">
                <b-icon icon="account-check-outline" size="is-small" /> {{ $t('pool.restoreSelected') }}
              </a>
              <a v-if="canManagePoolContacts && hasEmailablePoolContacts" class="a" href="#"
                @click.prevent="clearPoolContactsEmail" data-cy="btn-clear-pool-emails">
                <b-icon icon="email-off-outline" size="is-small" /> {{ $t('pool.clearSelected') }}
              </a>
              <span class="a">{{ $t('globals.messages.numSelected', { num: poolBulk.checked.length }) }}</span>
            </template>
          </div>
        </template>

        <b-table-column v-slot="props" field="customer_code" :label="$t('customers.customerCode')"
          header-class="cy-pool-customer_code" sortable>
          <copy-text v-if="poolRowValue(props.row, 'customerCode', 'customer_code')"
            :text="`${poolRowValue(props.row, 'customerCode', 'customer_code')}`" />
          <span v-else>-</span>
        </b-table-column>

        <b-table-column v-slot="props" field="name" :label="$t('pool.tableName')" header-class="cy-pool-name" sortable>
          {{ props.row.name || '-' }}
        </b-table-column>

        <b-table-column v-slot="props" field="email" :label="$t('pool.tableEmail')" header-class="cy-pool-email" sortable>
          {{ props.row.email || '-' }}
        </b-table-column>

        <b-table-column v-slot="props" field="allocation_department" :label="$t('pool.tableDepartment')"
          header-class="cy-pool-department" sortable>
          {{ poolRowValue(props.row, 'allocationDepartment', 'allocation_department') || '-' }}
        </b-table-column>

        <b-table-column v-slot="props" field="status" :label="$t('pool.tableStatus')" header-class="cy-pool-status" sortable>
          {{ poolContactStatus(props.row) }}
        </b-table-column>

        <b-table-column v-slot="props" field="created_at" :label="$t('globals.fields.createdAt')" sortable>
          {{ $utils.niceDate(poolRowValue(props.row, 'createdAt', 'created_at')) }}
        </b-table-column>

        <b-table-column v-slot="props" field="updated_at" :label="$t('globals.fields.updatedAt')" sortable>
          {{ $utils.niceDate(poolRowValue(props.row, 'updatedAt', 'updated_at')) }}
        </b-table-column>

        <b-table-column v-slot="props" cell-class="actions" align="right">
          <div>
            <a v-if="canManagePoolContacts && isFirstLevelPool" href="#" @click.prevent="showPoolAssignForm(props.row)"
              data-cy="btn-assign-pool-contact" :aria-label="$t('pool.actionAssign')">
              <b-tooltip :label="$t('pool.actionAssign')" type="is-dark">
                <b-icon icon="account-arrow-right-outline" size="is-small" />
              </b-tooltip>
            </a>
            <a v-if="canManagePoolContacts && isAllocationList && !props.row.excluded" href="#"
              @click.prevent="showPoolRemovalForm(props.row)" data-cy="btn-remove-pool-contact"
              :aria-label="$t('pool.actionRemove')">
              <b-tooltip :label="$t('pool.actionRemove')" type="is-dark">
                <b-icon icon="account-off-outline" size="is-small" />
              </b-tooltip>
            </a>
            <a v-if="canManagePoolContacts && isAllocationList && props.row.excluded" href="#"
              @click.prevent="restorePoolContacts(props.row)" data-cy="btn-restore-pool-contact"
              :aria-label="$t('pool.actionRestore')">
              <b-tooltip :label="$t('pool.actionRestore')" type="is-dark">
                <b-icon icon="account-check-outline" size="is-small" />
              </b-tooltip>
            </a>
            <a v-if="canManagePoolContacts && props.row.email" href="#" @click.prevent="clearPoolContactEmail(props.row)"
              data-cy="btn-clear-pool-email" :aria-label="$t('pool.actionClearEmail')">
              <b-tooltip :label="$t('pool.actionClearEmail')" type="is-dark">
                <b-icon icon="email-off-outline" size="is-small" />
              </b-tooltip>
            </a>
          </div>
        </b-table-column>

        <template #empty v-if="!poolLoading">
          <empty-placeholder :label="$t('globals.messages.emptyState')" />
        </template>
      </b-table>
    </div>

    <!-- The filtered list is still being resolved, or it cannot be read: never
         fall back to the ordinary customer table, whose rows and actions would
         belong to a different scope. -->
    <div v-else-if="listState === 'pending'" class="has-text-centered" data-cy="list-load-pending">
      <span class="spinner"><b-loading :is-full-page="false" active /></span>
    </div>

    <p v-else-if="listState === 'error'" class="has-text-grey" data-cy="list-load-error">
      {{ $t('globals.messages.notFound', { name: $t('globals.terms.customer_lists') }) }}
    </p>

    <b-table v-else :data="customers.results ?? []" :loading="loading.customers" @check-all="onTableCheck"
      @check="onTableCheck" :checked-rows.sync="bulk.checked" paginated backend-pagination pagination-position="both"
      @page-change="onPageChange" :current-page="queryParams.page" :per-page="customers.perPage"
      :total="customers.total" hoverable :checkable="canSelectCustomerRows"
      :is-row-checkable="canSelectCustomerRow" backend-sorting @sort="onSort">
      <template #top-left>
        <div class="actions">
          <a v-if="canExportCustomers" class="a" href="#" @click.prevent="exportCustomers" data-cy="btn-export-customers">
            <b-icon icon="cloud-download-outline" size="is-small" />
            {{ $t('customers.export') }}
          </a>
          <template v-if="bulk.checked.length > 0">
            <a v-if="canManageMemberships" class="a" href="#" @click.prevent="showBulkListForm" data-cy="btn-manage-customer_lists">
              <b-icon icon="format-list-bulleted-square" size="is-small" /> Manage customer_lists
            </a>
            <a v-if="canDeleteCustomers" class="a" href="#" @click.prevent="deleteCustomers" data-cy="btn-delete-customers">
              <b-icon icon="trash-can-outline" size="is-small" /> Delete
            </a>
            <a v-if="canBlocklistCustomers" class="a" href="#" @click.prevent="blocklistCustomers" data-cy="btn-manage-blocklist">
              <b-icon icon="account-off-outline" size="is-small" /> Blocklist
            </a>
            <span v-if="canManageMemberships || canDeleteCustomers || canBlocklistCustomers" class="a">
              {{ $t('globals.messages.numSelected', { num: numSelectedCustomers }) }}
              <span v-if="canSelectAll && !bulk.all && customers.total > customers.perPage">
                &mdash;
                <a href="#" @click.prevent="selectAllCustomers">
                  {{ $t('globals.messages.selectAll', { num: customers.total }) }}
                </a>
              </span>
            </span>
          </template>
        </div>
      </template>

      <b-table-column v-slot="props" field="customer_code" :label="$t('customers.customerCode')"
        header-class="cy-customer_code" sortable>
        <copy-text v-if="props.row.customerCode" :text="`${props.row.customerCode}`" />
      </b-table-column>

      <b-table-column v-slot="props" field="email" :label="$t('customers.email')" header-class="cy-email" sortable
        :td-attrs="$utils.tdID">
        <a :href="`/customers/${props.row.id}`" @click.prevent="showEditForm(props.row)"
          :class="{ 'blocklisted': props.row.status === 'blocklisted' }">
          {{ props.row.email }}
          <copy-text :text="`${props.row.email}`" hide-text />
        </a>
        <b-tag v-if="props.row.status !== 'enabled'" :class="props.row.status" data-cy="blocklisted">
          {{ $t(`customers.status.${props.row.status}`) }}
        </b-tag>
      </b-table-column>

      <b-table-column v-slot="props" field="name" :label="$t('customers.salutation')" header-class="cy-name" sortable>
        <a :href="`/customers/${props.row.id}`" @click.prevent="showEditForm(props.row)"
          :class="{ 'blocklisted': props.row.status === 'blocklisted' }">
          {{ props.row.name }}
          <copy-text :text="`${props.row.name}`" hide-text />
        </a>
      </b-table-column>

      <b-table-column v-slot="props" field="customerLists" :label="$t('globals.terms.customer_lists')" header-class="cy-customer_lists">
        <ul>
          <li v-for="customerList in props.row.customerLists" :key="customerList.id">
            <router-link :to="{ name: 'customersCustomerList', params: { customerListID: customerList.id } }">
              {{ customerList.name }}
            </router-link>
          </li>
        </ul>
      </b-table-column>

      <b-table-column v-slot="props" field="ownerUsername" :label="$t('shared.owner')">
        {{ ownerLabel(props.row) }}
        <b-tag v-if="transferPendingAt(props.row)" size="is-small" type="is-warning" class="is-light">
          {{ $t('shared.transferPending', { date: $utils.niceDate(transferPendingAt(props.row), true) }) }}
        </b-tag>
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
          <a v-if="canExportCustomers && canExportCustomer(props.row)" :href="customerExportURL(props.row.id)" data-cy="btn-download"
            :aria-label="$t('customers.downloadData')">
            <b-tooltip :label="$t('customers.downloadData')" type="is-dark">
              <b-icon icon="cloud-download-outline" size="is-small" />
            </b-tooltip>
          </a>
          <a v-if="canManageCustomer(props.row)" :href="`/customers/${props.row.id}`"
            @click.prevent="showEditForm(props.row)" data-cy="btn-edit" :aria-label="$t('globals.buttons.edit')">
            <b-tooltip :label="$t('globals.buttons.edit')" type="is-dark">
              <b-icon icon="pencil-outline" size="is-small" />
            </b-tooltip>
          </a>
          <a v-if="canDeleteCustomer(props.row)" href="#" @click.prevent="deleteCustomer(props.row)"
            data-cy="btn-delete" :aria-label="$t('globals.buttons.delete')">
            <b-tooltip :label="$t('globals.buttons.delete')" type="is-dark">
              <b-icon icon="trash-can-outline" size="is-small" />
            </b-tooltip>
          </a>
        </div>
      </b-table-column>

      <template #empty v-if="!loading.customers">
        <empty-placeholder />
      </template>
    </b-table>

    <!-- Manage customerList modal -->
    <b-modal scroll="keep" :aria-modal="true" :active.sync="isBulkListFormVisible" :width="500" class="has-overflow">
      <customer-bulk-list :num-customers="this.numSelectedCustomers" @finished="bulkChangeLists" />
    </b-modal>

    <!-- Add / edit form modal -->
    <b-modal scroll="keep" :aria-modal="true" :active.sync="isFormVisible" :width="850" @close="onFormClose">
      <customer-form :data="curItem" :is-editing="isEditing" @finished="queryCustomers" />
    </b-modal>

    <!-- New public-pool contact modal -->
    <b-modal scroll="keep" :aria-modal="true" :active.sync="isPoolFormVisible" :width="600" class="has-overflow">
      <pool-contact-form :pool-list-id="queryParams.customerListID" :is-platform-admin="workspace.platformAdmin"
        @finished="loadPoolContacts" />
    </b-modal>

    <!-- Remove pool contacts from this organization, with a shared reason -->
    <b-modal scroll="keep" :aria-modal="true" :active.sync="isPoolRemovalVisible" :width="480" class="has-overflow">
      <div class="modal-card" style="width: auto;">
        <header class="modal-card-head">
          <p class="modal-card-title">{{ $t('pool.actionRemove') }}</p>
        </header>
        <section class="modal-card-body">
          <p>{{ $t('pool.removeSelectedHelp', { num: poolPendingContacts.length }) }}</p>
          <b-field :label="$t('pool.tableRemoveReason')">
            <b-input v-model.trim="poolRemovalReason" type="textarea" data-cy="pool-removal-reason" />
          </b-field>
        </section>
        <footer class="modal-card-foot has-text-right">
          <b-button @click="isPoolRemovalVisible = false">{{ $t('globals.buttons.cancel') }}</b-button>
          <b-button type="is-primary" @click="removePoolContacts" data-cy="btn-confirm-pool-removal">
            {{ $t('globals.buttons.ok') }}
          </b-button>
        </footer>
      </div>
    </b-modal>

    <!-- Assign pool contacts to one of the pool's allocations -->
    <b-modal scroll="keep" :aria-modal="true" :active.sync="isPoolAssignVisible" :width="480" class="has-overflow">
      <div class="modal-card" style="width: auto;">
        <header class="modal-card-head">
          <p class="modal-card-title">{{ $t('pool.actionAssign') }}</p>
        </header>
        <section class="modal-card-body">
          <p>{{ $t('pool.assignSelectedHelp', { num: poolPendingContacts.length }) }}</p>
          <b-field :label="$t('pool.assignTargetLabel')">
            <b-select v-model.number="poolAssignAllocationID" expanded data-cy="pool-assign-allocation">
              <option v-for="allocation in poolAllocations" :key="allocation.id" :value="Number(allocation.id)">
                {{ allocation.listName || allocation.list_name || allocation.organizationName || allocation.organization_name }}
              </option>
            </b-select>
          </b-field>
        </section>
        <footer class="modal-card-foot has-text-right">
          <b-button @click="isPoolAssignVisible = false">{{ $t('globals.buttons.cancel') }}</b-button>
          <b-button type="is-primary" :disabled="!poolAssignAllocationID" @click="assignPoolContacts"
            data-cy="btn-confirm-pool-assign">
            {{ $t('globals.buttons.ok') }}
          </b-button>
        </footer>
      </div>
    </b-modal>
  </section>
</template>

<script>
import Vue from 'vue';
import { mapState } from 'vuex';
import { uris } from '../constants';
import EmptyPlaceholder from '../components/EmptyPlaceholder.vue';
import CustomerBulkList from './CustomerBulkList.vue';
import CustomerForm from './CustomerForm.vue';
import PoolContactForm from './PoolContactForm.vue';
import CopyText from '../components/CopyText.vue';

export default Vue.extend({
  components: {
    CustomerForm,
    CustomerBulkList,
    PoolContactForm,
    CopyText,
    EmptyPlaceholder,
  },

  data() {
    return {
      // Current customer item being edited.
      curItem: null,
      isSearchAdvanced: false,
      isEditing: false,
      isFormVisible: false,
      isBulkListFormVisible: false,

      // Pool lists (first-level public pools and their organization
      // allocations) keep their contacts in a separate store. They are
      // rendered inside this same customers view so that viewing a pool list
      // does not feel like leaving the customer area.
      listDetail: null,
      pool: {
        results: [],
        total: 0,
        perPage: 20,
        page: 1,
        orderBy: 'id',
        order: 'desc',
        search: '',
      },
      poolLoading: false,
      poolAllocations: [],

      // Pool contact mutations.
      poolBulk: {
        checked: [],
      },
      poolPendingContacts: [],
      poolRemovalReason: '',
      poolAssignAllocationID: null,
      isPoolRemovalVisible: false,
      isPoolAssignVisible: false,
      isPoolFormVisible: false,

      // Whether the filtered list is resolved yet: 'ready' | 'pending' |
      // 'error'. Only a resolved, non-pool list may render the ordinary
      // customer table.
      listState: 'ready',

      // Table bulk row selection states.
      bulk: {
        checked: [],
        all: false,
      },

      queryInput: '',

      // Query params to filter the getCustomers() API call.
      queryParams: {
        // Search query expression.
        queryExp: '',
        search: '',

        // ID of the customerList the current customer view is filtered by.
        customerListID: null,
        page: 1,
        orderBy: 'id',
        order: 'desc',
        subStatus: null,
      },
    };
  },

  methods: {
    canManageCustomer(customer) {
      return this.$canManageResource(customer, 'customers:manage');
    },

    canDeleteCustomer(customer) {
      return this.$canManageResource(customer, 'customers:delete');
    },

    canBlocklistCustomer(customer) {
      return this.$canManageResource(customer, 'customers:blocklist');
    },

    canManageMembership(customer) {
      return this.$canManageResource(customer, 'customers:membership_manage');
    },

    canExportCustomer(customer) {
      return this.$canManageResource(customer) && this.$can('customers:get_all', 'customers:get');
    },

    canSelectCustomerRow(customer) {
      return this.canManageCustomer(customer)
        || this.canDeleteCustomer(customer)
        || this.canBlocklistCustomer(customer)
        || this.canManageMembership(customer);
    },

    ownerLabel(resource) {
      return resource.ownerName || resource.ownerUsername || '-';
    },

    transferPendingAt(resource) {
      return resource.transferPendingAt || resource.transfer_pending_at;
    },

    customerExportURL(id) {
      const organizationID = Number(this.workspace.organizationId) || 0;
      const suffix = organizationID > 0 ? `?organization_id=${organizationID}` : '';
      return `/api/customers/${id}/export${suffix}`;
    },

    // Loads one server-paginated page of the contacts of the selected pool
    // list. The endpoint masks e-mail addresses for everyone but the highest
    // administrator, so the table renders them verbatim.
    loadPoolContacts(params = {}) {
      const id = this.queryParams.customerListID;
      if (!id) {
        return Promise.resolve();
      }
      this.pool = { ...this.pool, ...params };
      this.poolLoading = true;
      this.poolBulk.checked = [];

      const qp = {
        page: this.pool.page,
        order_by: this.pool.orderBy,
        order: this.pool.order,
      };
      if (this.pool.perPage > 0) {
        qp.per_page = this.pool.perPage;
      }
      if (this.pool.search) {
        qp.search = this.pool.search;
      }

      return this.$api.getPoolContacts(id, qp).then((resp) => {
        // Tolerate the legacy bare-array response shape.
        const results = Array.isArray(resp) ? resp : (resp.results || []);
        this.pool.results = results;
        this.pool.total = Array.isArray(resp) ? results.length : (Number(resp.total) || 0);
        const perPage = Number(resp.perPage || resp.per_page);
        if (perPage > 0) {
          this.pool.perPage = perPage;
        }
      }).finally(() => {
        this.poolLoading = false;
      });
    },

    loadPoolAllocations() {
      const id = this.queryParams.customerListID;
      if (!id) {
        return Promise.resolve();
      }
      return this.$api.getOrgPoolAllocations(id).then((rows) => {
        this.poolAllocations = Array.isArray(rows) ? rows : [];
      }).catch(() => {
        this.poolAllocations = [];
      });
    },

    onPoolPageChange(page) {
      this.loadPoolContacts({ page });
    },

    onPoolSort(field, direction) {
      this.loadPoolContacts({ orderBy: field, order: direction, page: 1 });
    },

    // Pool contacts live in a separate store: the pool tab reopens the last
    // pool list the user visited and falls back to the first accessible one.
    goToPoolTab() {
      if (this.isPoolList) {
        return;
      }
      const pools = this.accessiblePoolLists;
      if (pools.length === 0) {
        this.$utils.toast(this.$t('pool.noAccessiblePool'), 'is-danger');
        return;
      }
      const lastID = Number(window.localStorage.getItem('poolLastListID'));
      const target = pools.find((l) => l.id === lastID) || pools[0];
      this.$router.push({ name: 'customersCustomerList', params: { customerListID: target.id } });
    },

    exportPoolContacts() {
      const id = this.queryParams.customerListID;
      if (!id) {
        return;
      }
      this.$utils.confirm(this.$t('customers.confirmExport', { num: this.pool.total }), () => {
        const q = new URLSearchParams();
        if (this.pool.search) {
          q.append('search', this.pool.search);
        }
        if (this.pool.orderBy) {
          q.append('order_by', this.pool.orderBy);
        }
        if (this.pool.order) {
          q.append('order', this.pool.order);
        }
        document.location.href = `/api/customer-lists/${id}/pool-contacts/export?${q.toString()}`;
      });
    },

    // Removal and assignment target either the clicked row or, with a null
    // argument, the current page selection.
    showPoolRemovalForm(contact) {
      this.poolPendingContacts = contact ? [contact] : this.poolBulk.checked;
      this.poolRemovalReason = '';
      this.isPoolRemovalVisible = true;
    },

    removePoolContacts() {
      const allocationID = this.poolAllocationID;
      if (!allocationID || this.poolPendingContacts.length === 0) {
        return;
      }
      const reason = this.poolRemovalReason.trim();
      const targets = this.poolPendingContacts;
      const calls = targets.map((contact) => this.$api.removePoolContact({
        allocation_id: allocationID,
        contact_id: contact.id,
        reason,
      }));
      Promise.all(calls).then(() => {
        this.isPoolRemovalVisible = false;
        this.poolPendingContacts = [];
        this.loadPoolContacts();
        this.$utils.toast(this.$t('pool.toastContactsRemoved', { num: targets.length }));
      });
    },

    restorePoolContacts(contact) {
      const allocationID = this.poolAllocationID;
      const targets = contact ? [contact] : this.poolBulk.checked.filter((c) => c.excluded);
      if (!allocationID || targets.length === 0) {
        return;
      }
      const calls = targets.map((c) => this.$api.restorePoolContact({
        allocation_id: allocationID,
        contact_id: c.id,
      }));
      Promise.all(calls).then(() => {
        this.loadPoolContacts();
        this.$utils.toast(this.$t('pool.toastContactsRestored', { num: targets.length }));
      });
    },

    clearPoolContactEmail(contact) {
      this.$utils.confirm(this.$t('pool.confirmClearEmail'), () => {
        this.$api.clearPoolContactEmail(this.queryParams.customerListID, contact.id).then(() => {
          this.loadPoolContacts();
          this.$utils.toast(this.$t('pool.toastEmailCleared'));
        });
      });
    },

    clearPoolContactsEmail() {
      const targets = this.poolBulk.checked.filter((c) => c.email);
      if (targets.length === 0) {
        return;
      }
      this.$utils.confirm(this.$t('pool.confirmClearEmail'), () => {
        const calls = targets.map((c) => this.$api.clearPoolContactEmail(this.queryParams.customerListID, c.id));
        Promise.all(calls).then(() => {
          this.loadPoolContacts();
          this.$utils.toast(this.$t('pool.toastEmailCleared'));
        });
      });
    },

    showPoolAssignForm(contact) {
      this.poolPendingContacts = contact ? [contact] : this.poolBulk.checked;
      const own = this.poolAllocations[0];
      this.poolAssignAllocationID = own ? Number(own.id) : null;
      this.isPoolAssignVisible = true;
    },

    assignPoolContacts() {
      if (!this.poolAssignAllocationID || this.poolPendingContacts.length === 0) {
        return;
      }
      const targets = this.poolPendingContacts;
      const calls = targets.map((c) => this.$api.assignPoolContact({
        allocation_id: this.poolAssignAllocationID,
        contact_id: c.id,
      }));
      Promise.all(calls).then(() => {
        this.isPoolAssignVisible = false;
        this.poolPendingContacts = [];
        this.loadPoolContacts();
        this.$utils.toast(this.$t('pool.toastContactsAssigned', { num: targets.length }));
      });
    },

    poolContactStatus(contact) {
      if (contact.excluded) {
        return this.$t('pool.statusRemoved');
      }
      return contact.status === 'archived'
        ? this.$t('pool.statusArchived')
        : this.$t('pool.statusNormal');
    },

    // The API client camel-cases response keys (`allocation_department` ->
    // `allocationDepartment`); both shapes are read so the table keeps
    // rendering even if that conversion is disabled for a call.
    poolRowValue(row, camelKey, snakeKey) {
      if (row[camelKey] !== undefined && row[camelKey] !== null) {
        return row[camelKey];
      }
      return row[snakeKey];
    },

    // Resolves the filtered list and then loads whichever contact store it
    // uses: ordinary customers, or the pool contacts of a first-level pool /
    // organization pool allocation. A list that cannot be resolved must not
    // fall back to the ordinary customer table, whose rows and actions would
    // belong to a different scope.
    loadCustomerListView() {
      const known = this.currentList;
      if (known) {
        this.listDetail = known;
        this.listState = 'ready';
        if (this.isPoolList) {
          this.rememberPoolList();
          this.loadPoolAllocations();
          return this.loadPoolContacts();
        }
        this.queryCustomers();
        return Promise.resolve();
      }
      // The list is not in the store yet (direct link, or the app shell is
      // still loading lists): resolve its type before picking the store.
      this.listState = 'pending';
      return this.$api.getList(this.queryParams.customerListID).then((list) => {
        this.listDetail = list;
        this.listState = 'ready';
        if (this.isPoolList) {
          this.rememberPoolList();
          this.loadPoolAllocations();
          return this.loadPoolContacts();
        }
        this.queryCustomers();
        return null;
      }).catch(() => {
        // Deleted, or not shared with this workspace: the interceptor has
        // already surfaced the server error.
        this.listState = 'error';
        return null;
      });
    },

    rememberPoolList() {
      if (this.queryParams.customerListID) {
        window.localStorage.setItem('poolLastListID', String(this.queryParams.customerListID));
      }
    },

    // Refresh handler for both modes (the app shell emits `page.refresh`).
    refreshPage() {
      if (this.isPoolList) {
        this.loadPoolContacts();
        return;
      }
      this.queryCustomers();
    },

    toggleAdvancedSearch() {
      this.isSearchAdvanced = !this.isSearchAdvanced;
      this.queryParams.search = '';

      // Toggling to simple search.
      if (!this.isSearchAdvanced) {
        this.queryInput = '';
        this.queryParams.queryExp = '';
        this.queryParams.page = 1;
        this.queryCustomers();
        this.$refs.query.focus();
        return;
      }

      // Toggling to advanced search.
      const q = this.queryInput.replace(/'/, "''").trim();
      if (q) {
        if (this.$utils.validateEmail(q)) {
          this.queryParams.queryExp = `email = '${q.toLowerCase()}'`;
        } else {
          this.queryParams.queryExp = `(name ~* '${q}' OR email ~* '${q.toLowerCase()}')`;
        }
      }

      // Toggling to advanced search.
      this.$nextTick(() => {
        this.$refs.queryExp.focus();
      });
    },

    // Mark all customers in the query as selected.
    selectAllCustomers() {
      this.bulk.all = true;
    },

    onTableCheck() {
      // Disable bulk.all selection if there are no rows checked in the table.
      if (this.bulk.checked.length !== this.customers.total) {
        this.bulk.all = false;
      }
    },

    // Show the edit customerList form.
    showEditForm(sub) {
      this.curItem = sub;
      this.isFormVisible = true;
      this.isEditing = true;
    },

    // Show the new customerList form.
    showNewForm() {
      this.curItem = {};
      this.isFormVisible = true;
      this.isEditing = false;
    },

    showBulkListForm() {
      this.isBulkListFormVisible = true;
    },

    onFormClose() {
      if (this.$route.params.id) {
        this.$router.push({ name: 'customers' });
      }
    },

    onPageChange(p) {
      this.queryCustomers({ page: p });
    },

    onSort(field, direction) {
      this.queryCustomers({ orderBy: field, order: direction });
    },

    // Prepares an SQL expression for simple name search inputs and saves it
    // in this.queryExp.
    onSimpleQueryInput(v) {
      const q = v.replace(/'/, "''").trim();
      this.queryParams.queryExp = '';
      this.queryParams.page = 1;
      this.queryParams.search = q.toLowerCase();
    },

    // Ctrl + Enter on the advanced query searches.
    onAdvancedQueryEnter(e) {
      if (e.ctrlKey) {
        this.onSubmit();
      }
    },

    onSubmit() {
      if (this.isPoolList) {
        this.loadPoolContacts({ search: (this.queryInput || '').trim(), page: 1 });
        return;
      }
      this.queryCustomers({ page: 1 });
    },

    // Search / query customers.
    queryCustomers(params) {
      this.queryParams = { ...this.queryParams, ...params };

      const qp = {
        customer_list_id: this.queryParams.customerListID,
        search: this.queryParams.search,
        query: this.queryParams.queryExp,
        page: this.queryParams.page,
        subscription_status: this.queryParams.subStatus,
        order_by: this.queryParams.orderBy,
        order: this.queryParams.order,
      };

      if (this.queryParams.queryExp) {
        delete qp.search;
      } else {
        delete qp.queryExp;
      }

      this.$nextTick(() => {
        this.$api.getCustomers(qp).then(() => {
          this.bulk.checked = [];
        });
      });
    },

    deleteCustomer(sub) {
      this.$utils.confirm(
        null,
        () => {
          this.$api.deleteCustomer(sub.id).then(() => {
            this.queryCustomers();

            this.$utils.toast(this.$t('globals.messages.deleted', { name: sub.name }));
          });
        },
      );
    },

    blocklistCustomers() {
      let fn = null;
      if (!this.bulk.all && this.bulk.checked.length > 0) {
        // If 'all' is not selected, blocklist customers by IDs.
        fn = () => {
          const ids = this.bulk.checked.map((s) => s.id);
          this.$api.blocklistCustomers({ ids })
            .then(() => this.queryCustomers());
        };
      } else {
        // 'All' is selected, blocklist by query.
        fn = () => {
          this.$api.blocklistCustomersByQuery({
            search: this.queryParams.search,
            query: this.queryParams.queryExp,
            customer_list_ids: this.queryParams.customerListID ? [this.queryParams.customerListID] : null,
            subscription_status: this.queryParams.subStatus,
          }).then(() => this.queryCustomers());
        };
      }

      this.$utils.confirm(this.$t('customers.confirmBlocklist', { num: this.numSelectedCustomers }), fn);
    },

    exportCustomers() {
      const num = !this.bulk.all && this.bulk.checked.length > 0
        ? this.bulk.checked.length : this.customers.total;

      this.$utils.confirm(this.$t('customers.confirmExport', { num }), () => {
        const q = new URLSearchParams();

        if (this.queryParams.search) {
          q.append('search', this.queryParams.search);
        } else if (this.queryParams.queryExp) {
          q.append('query', this.queryParams.queryExp);
        }

        if (this.queryParams.customerListID) {
          q.append('customer_list_id', this.queryParams.customerListID);
        }

        if (this.queryParams.subStatus) {
          q.append('subscription_status', this.queryParams.subStatus);
        }

        if (this.workspace.organizationId) {
          q.append('organization_id', this.workspace.organizationId);
        }

        if (!this.bulk.all && this.bulk.checked.length > 0) {
          this.bulk.checked.forEach((customer) => q.append('id', customer.id));
        }

        document.location.href = `${uris.exportCustomers}?${q.toString()}`;
      });
    },

    deleteCustomers() {
      let fn = null;
      if (!this.bulk.all && this.bulk.checked.length > 0) {
        // If 'all' is not selected, delete customers by IDs.
        fn = () => {
          const ids = this.bulk.checked.map((s) => s.id);
          this.$api.deleteCustomers({ id: ids })
            .then(() => {
              this.queryCustomers();

              this.$utils.toast(this.$t('customers.customersDeleted', { num: this.numSelectedCustomers }));
            });
        };
      } else {
        // 'All' is selected, delete by query.
        fn = () => {
          this.$api.deleteCustomersByQuery({
            // If the query expression is empty, explicitly pass `all=true`
            // so that the backend deletes all records in the DB with an empty query string.
            all: this.queryParams.queryExp.trim() === '' && this.queryParams.search.trim() === '',
            search: this.queryParams.search,
            query: this.queryParams.queryExp,
            customer_list_ids: this.queryParams.customerListID ? [this.queryParams.customerListID] : null,
            subscription_status: this.queryParams.subStatus,
          }).then(() => {
            this.queryCustomers();

            this.$utils.toast(this.$t(
              'customers.customersDeleted',
              { num: this.numSelectedCustomers },
            ));
          });
        };
      }

      this.$utils.confirm(this.$t('customers.confirmDelete', { num: this.numSelectedCustomers }), fn);
    },

    bulkChangeLists(action, preconfirm, customerLists) {
      const data = {
        action,
        query: this.fullQueryExp,
        search: this.queryParams.search,
        customer_list_ids: this.queryParams.customerListID ? [this.queryParams.customerListID] : null,
        target_customer_list_ids: customerLists.map((l) => l.id),
      };

      if (preconfirm) {
        data.status = 'confirmed';
      }

      let fn = null;
      if (!this.bulk.all && this.bulk.checked.length > 0) {
        // If 'all' is not selected, perform by IDs.
        fn = this.$api.addCustomersToLists;
        data.ids = this.bulk.checked.map((s) => s.id);
      } else {
        // 'All' is selected, perform by query.
        data.query = this.queryParams.queryExp;
        data.subscription_status = this.queryParams.subStatus;
        fn = this.$api.addCustomersToListsByQuery;
      }

      fn(data).then(() => {
        this.queryCustomers();
        this.$utils.toast(this.$t('customers.listChangeApplied'));
      });
    },
  },

  computed: {
    ...mapState(['customers', 'customer_lists', 'loading', 'workspace']),

    canManageCustomers() {
      return this.$canCreateWorkspaceResource('customers:manage');
    },

    canDeleteCustomers() {
      return this.$can('customers:delete')
        && (!this.bulk.checked.length || this.bulk.checked.every((customer) => this.canDeleteCustomer(customer)));
    },

    canBlocklistCustomers() {
      return this.$can('customers:blocklist')
        && (!this.bulk.checked.length || this.bulk.checked.every((customer) => this.canBlocklistCustomer(customer)));
    },

    canManageMemberships() {
      return this.$can('customers:membership_manage')
        && (!this.bulk.checked.length || this.bulk.checked.every((customer) => this.canManageMembership(customer)));
    },

    canSelectCustomerRows() {
      return this.canManageCustomers || this.$can('customers:delete', 'customers:blocklist', 'customers:membership_manage');
    },

    canSelectAll() {
      return !(this.workspace.organizationId && this.workspace.role === 'manager');
    },

    canExportCustomers() {
      return (this.workspace.platformAdmin || (this.workspace.organizationId && this.workspace.role === 'manager')
        || this.$can('customers:export'))
        && this.$canCreateWorkspaceResource('customers:get_all', 'customers:get')
        && (!this.bulk.checked.length || this.bulk.checked.every((customer) => this.$canManageResource(customer)));
    },

    numSelectedCustomers() {
      if (this.bulk.all) {
        return this.customers.total;
      }
      return this.bulk.checked.length;
    },

    // Returns the customer list the current view is filtered by: the freshly
    // fetched detail when available, otherwise the copy already loaded by the
    // app shell.
    activeList() {
      return this.listDetail || this.currentList;
    },

    // True while the view shows a public-pool list. Pool lists have no rows in
    // the ordinary customer membership tables, so their contacts come from the
    // pool store instead.
    isPoolList() {
      const type = (this.listDetail && this.listDetail.type)
        || (this.currentList && this.currentList.type);
      return type === 'pool' || type === 'org_pool_allocation';
    },

    // First-level public pools carry the whole pool; organization allocations
    // are one organization's slice of it. A few row actions only make sense in
    // one of the two contexts.
    isFirstLevelPool() {
      const type = (this.listDetail && this.listDetail.type)
        || (this.currentList && this.currentList.type);
      return type === 'pool';
    },

    isAllocationList() {
      const type = (this.listDetail && this.listDetail.type)
        || (this.currentList && this.currentList.type);
      return type === 'org_pool_allocation';
    },

    // The allocation of the currently viewed allocation list, used by the
    // per-organization remove/restore actions.
    poolAllocationID() {
      if (!this.isAllocationList) {
        return null;
      }
      const listID = Number(this.queryParams.customerListID);
      const row = this.poolAllocations.find(
        (allocation) => Number(allocation.listId || allocation.list_id) === listID,
      );
      return row ? Number(row.id) : null;
    },

    accessiblePoolLists() {
      if (!this.customer_lists.results) {
        return [];
      }
      return this.customer_lists.results.filter(
        (l) => l.type === 'pool' || l.type === 'org_pool_allocation',
      );
    },

    canManagePoolContacts() {
      return this.$can('pools:manage');
    },

    canExportPoolContacts() {
      return this.$can('pools:export');
    },

    hasRemovablePoolContacts() {
      return this.poolBulk.checked.some((contact) => !contact.excluded);
    },

    hasRestorablePoolContacts() {
      return this.poolBulk.checked.some((contact) => contact.excluded);
    },

    hasEmailablePoolContacts() {
      return this.poolBulk.checked.some((contact) => !!contact.email);
    },

    // Row count of whatever this view currently lists: pool contacts or
    // ordinary customers. `null` means "not known yet" (the customers model is
    // an empty array before the first response).
    dataCount() {
      if (this.isPoolList) {
        return this.pool.total;
      }
      if (this.customers.total === undefined || Number.isNaN(Number(this.customers.total))) {
        return null;
      }
      return this.customers.total;
    },

    // Returns the customerList that the customers are being filtered by in.
    currentList() {
      if (!this.queryParams.customerListID || !this.customer_lists.results) {
        return null;
      }

      return this.customer_lists.results.find((l) => l.id === this.queryParams.customerListID);
    },
  },

  created() {
    this.$root.$on('page.refresh', this.refreshPage);
  },

  destroyed() {
    this.$root.$off('page.refresh', this.refreshPage);
  },

  mounted() {
    if (this.$route.params.customerListID) {
      this.queryParams.customerListID = parseInt(this.$route.params.customerListID, 10);
    }
    if (this.$route.query.subscription_status) {
      this.queryParams.subStatus = this.$route.query.subscription_status;
    }

    if (this.$route.params.id) {
      this.$api.getCustomer(parseInt(this.$route.params.id, 10)).then((data) => {
        this.showEditForm(data);
      });
      return;
    }

    // Resolve the filtered list first: a pool list has to render its pool
    // contacts instead of an empty ordinary-customer table.
    if (this.queryParams.customerListID) {
      this.loadCustomerListView();
      return;
    }

    // Get customers on load.
    this.queryCustomers();
  },
});
</script>
