<template>
  <section class="customers">
    <header class="columns page-header">
      <div class="column is-10">
        <h1 class="title is-4">
          {{ isPoolRoute || isPoolList ? $t('pool.tabPoolContacts') : $t('globals.terms.customers') }}
          <span v-if="dataCount !== null">
            (<span data-cy="count">{{ dataCount }}</span>)
          </span>
          <span v-if="activeList && !isPoolRoute">
            &raquo; {{ activeList.name }}
            <b-tag v-if="isPoolList" size="is-small" class="is-light" data-cy="pool-list-type">
              {{ $t(`customer_lists.types.${activeList.type}`) }}
            </b-tag>
            <span v-if="queryParams.subStatus" class="has-text-grey has-text-weight-normal is-capitalized">({{
              queryParams.subStatus }})</span>
          </span>
        </h1>
      </div>
      <div class="column has-text-right">
        <b-field v-if="canManageCustomers && !isPoolList" expanded>
          <b-button expanded type="is-primary" icon-left="plus" @click="showNewForm" data-cy="btn-new" class="btn-new">
            {{ $t('globals.buttons.new') }}
          </b-button>
        </b-field>
        <b-field v-else-if="isPoolList && (isFirstLevelPool || isPoolRoute) && canMaintainPoolMaster
          && (!isPoolRoute || firstLevelPoolLists.length > 0)" expanded>
          <b-button expanded type="is-primary" icon-left="plus" @click="isPoolFormVisible = true" data-cy="btn-new-pool-contact"
            class="btn-new">
            {{ $t('globals.buttons.new') }}
          </b-button>
        </b-field>
      </div>
    </header>

    <section v-if="listState !== 'error'" class="customers-controls">
      <div class="columns">
        <div class="column is-8">
          <form @submit.prevent="onSubmit">
            <div>
              <b-field addons>
                <b-input @input="onSimpleQueryInput" v-model="queryInput" expanded
                  :placeholder="isPoolList ? $t('pool.searchPlaceholder') : $t('customers.queryPlaceholder')"
                  icon="magnify" ref="query"
                  :data-cy="isPoolList ? 'pool-search' : 'search'" />
                <p class="controls">
                <b-button native-type="submit" type="is-primary" icon-left="magnify"
                  :aria-label="$t('globals.buttons.search')" data-cy="btn-search" />
                </p>
              </b-field>
            </div>
          </form>
        </div><!-- search -->
      </div>
    </section><!-- control -->

    <br />
    <!-- Pool lists keep their contacts in a separate store and are rendered
         with the same table shape, toolbar and pagination as ordinary
         customers. Buefy's b-table does not forward `data-cy` to the DOM, so
         the pool table is wrapped for tests. -->
    <div v-if="isPoolList" data-cy="pool-contacts-table">
      <b-table :data="pool.results" :loading="poolLoading" hoverable
        paginated backend-pagination pagination-position="both" @page-change="onPoolPageChange"
        :current-page="pool.page" :per-page="pool.perPage" :total="pool.total"
        :checked-rows.sync="poolBulk.checked" :checkable="canSelectPoolContacts" backend-sorting
        @sort="onPoolSort">
        <template #top-left>
          <div class="actions pool-toolbar">
            <a v-if="canExportPoolContacts" class="a" href="#" @click.prevent="exportPoolContacts"
              data-cy="btn-export-pool-contacts">
              <b-icon icon="cloud-download-outline" size="is-small" />
              {{ $t('customers.export') }}
            </a>
            <b-field v-if="isPoolList" class="pool-status-filter">
              <b-select :value="queryParams.poolStatus" :aria-label="$t('pool.contactStatusFilter')"
                data-cy="pool-status-filter" @input="setPoolStatus">
                <option value="active">{{ $t('pool.activeContacts') }}</option>
                <option value="removed">{{ $t('pool.tabPoolExceptions') }}</option>
              </b-select>
            </b-field>
            <b-field v-if="isPoolRoute" class="pool-status-filter pool-facet-filter">
              <b-select :value="queryParams.poolID ? String(queryParams.poolID) : 'all'"
                :aria-label="$t('pool.poolListFilter')" data-cy="pool-list-filter"
                @input="setPoolFacet('pool_id', $event)">
                <option value="all">{{ $t('pool.allPoolLists') }}</option>
                <option v-for="option in aggregatePoolOptions" :key="option.id" :value="String(option.id)">
                  {{ option.name }}
                </option>
              </b-select>
            </b-field>
            <b-field v-if="isPoolRoute" class="pool-status-filter pool-facet-filter">
              <b-select :value="queryParams.poolDepartment === null ? 'all' : `department:${queryParams.poolDepartment}`"
                :aria-label="$t('pool.departmentFilter')" data-cy="pool-department-filter"
                @input="setPoolFacet('allocation_department', $event)">
                <option value="all">{{ $t('pool.allDepartments') }}</option>
                <option v-for="department in aggregateDepartmentOptions" :key="`department:${department}`"
                  :value="`department:${department}`">
                  {{ department || $t('pool.unassignedDepartment') }}
                </option>
              </b-select>
            </b-field>
            <template v-if="poolBulk.checked.length > 0">
              <a v-if="canManagePoolContacts && (isFirstLevelPool || isPoolRoute)" class="a" href="#" @click.prevent="showPoolAssignForm(null)"
                data-cy="btn-assign-pool-contacts">
                <b-icon icon="account-arrow-right-outline" size="is-small" /> {{ $t('pool.assignSelected') }}
              </a>
              <a v-if="canManagePoolContacts && (isAllocationList || (isPoolRoute && !isPlatformAdmin)) && hasRemovablePoolContacts" class="a" href="#"
                @click.prevent="showPoolRemovalForm(null)" data-cy="btn-remove-pool-contacts">
                <b-icon icon="account-off-outline" size="is-small" /> {{ $t('pool.removeSelected') }}
              </a>
              <a v-if="canManagePoolContacts && hasRestorablePoolContacts" class="a" href="#"
                @click.prevent="restorePoolContacts(null)" data-cy="btn-restore-pool-contacts">
                <b-icon icon="account-check-outline" size="is-small" /> {{ $t('pool.restoreSelected') }}
              </a>
              <a v-if="canMaintainPoolMaster && hasEmailablePoolContacts" class="a" href="#"
                @click.prevent="clearPoolContactsEmail" data-cy="btn-clear-pool-emails">
                <b-icon icon="email-off-outline" size="is-small" /> {{ $t('pool.archiveInvalidSelected') }}
              </a>
              <a v-if="canDeletePoolContacts" class="a" href="#" @click.prevent="deletePoolContacts()"
                data-cy="btn-delete-pool-contacts">
                <b-icon icon="trash-can-outline" size="is-small" /> {{ $t('pool.deleteSelected') }}
              </a>
              <span class="a">{{ $t('globals.messages.numSelected', { num: poolBulk.checked.length }) }}</span>
            </template>
          </div>
        </template>

        <b-table-column v-slot="props" field="customer_code" :label="$t('pool.tableCustomerCode')"
          header-class="cy-pool-customer_code" sortable>
          <copy-text v-if="poolRowValue(props.row, 'customerCode', 'customer_code')"
            :text="`${poolRowValue(props.row, 'customerCode', 'customer_code')}`" />
          <span v-else>-</span>
        </b-table-column>

        <b-table-column v-slot="props" field="name" :label="$t('pool.tableName')" header-class="cy-pool-name" sortable>
          {{ props.row.name || '-' }}
        </b-table-column>

        <b-table-column v-if="isPoolRoute" v-slot="props" field="pool_name" :label="$t('pool.tablePoolList')"
          header-class="cy-pool-list" sortable>
          <router-link :to="{
            name: 'poolListContacts',
            params: { customerListID: poolRowValue(props.row, 'poolId', 'pool_id') },
          }">
            {{ poolRowValue(props.row, 'poolName', 'pool_name') || '-' }}
          </router-link>
        </b-table-column>

        <b-table-column v-slot="props" field="email" :label="$t('pool.tableEmail')" header-class="cy-pool-email" sortable>
          {{ props.row.email || '-' }}
        </b-table-column>
        <b-table-column v-slot="props" field="reply_to" :label="$t('pool.tableReplyTo')" sortable>
          {{ poolRowValue(props.row, 'replyTo', 'reply_to') || '-' }}
        </b-table-column>

        <b-table-column v-slot="props" field="allocation_department" :label="$t('pool.tableDepartment')"
          header-class="cy-pool-department" sortable>
          {{ poolRowValue(props.row, 'allocationDepartment', 'allocation_department') || '-' }}
        </b-table-column>

        <b-table-column v-if="(isFirstLevelPool || isPoolRoute) && isPlatformAdmin && queryParams.poolStatus === 'removed'"
          v-slot="props" :label="$t('pool.exceptionOrganization')">
          {{ poolRowValue(props.row, 'exceptionOrganizationName', 'exception_organization_name') || '-' }}
        </b-table-column>

        <b-table-column v-if="queryParams.poolStatus === 'removed'" v-slot="props" :label="$t('pool.exceptionReason')">
          {{ poolRowValue(props.row, 'exclusionReason', 'exclusion_reason') || '-' }}
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
            <a v-if="canManagePoolContacts && isFirstLevelPool && !isPoolRoute" href="#" @click.prevent="showPoolAssignForm(props.row)"
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
            <a v-if="canManagePoolContacts && !isPoolRoute && props.row.excluded && poolContactRestoreAllocationID(props.row)" href="#"
              @click.prevent="restorePoolContacts(props.row)" data-cy="btn-restore-pool-contact"
              :aria-label="$t('pool.actionRestore')">
              <b-tooltip :label="$t('pool.actionRestore')" type="is-dark">
                <b-icon icon="account-check-outline" size="is-small" />
              </b-tooltip>
            </a>
            <a v-if="canMaintainPoolMaster && props.row.email" href="#" @click.prevent="clearPoolContactEmail(props.row)"
              data-cy="btn-archive-invalid-pool-contact" :aria-label="$t('pool.actionArchiveInvalid')">
              <b-tooltip :label="$t('pool.actionArchiveInvalid')" type="is-dark">
                <b-icon icon="email-off-outline" size="is-small" />
              </b-tooltip>
            </a>
            <a v-if="canDeletePoolContacts" href="#" @click.prevent="deletePoolContact(props.row)"
              data-cy="btn-delete-pool-contact" :aria-label="$t('pool.actionDelete')">
              <b-tooltip :label="$t('pool.actionDelete')" type="is-dark">
                <b-icon icon="trash-can-outline" size="is-small" />
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
              <b-icon icon="format-list-bulleted-square" size="is-small" /> {{ $t('customers.manageLists') }}
            </a>
            <a v-if="canDeleteCustomers" class="a" href="#" @click.prevent="deleteCustomers" data-cy="btn-delete-customers">
              <b-icon icon="trash-can-outline" size="is-small" /> {{ $t('globals.buttons.delete') }}
            </a>
            <a v-if="canBlocklistCustomers" class="a" href="#" @click.prevent="blocklistCustomers" data-cy="btn-manage-blocklist">
              <b-icon icon="account-off-outline" size="is-small" /> {{ $t('import.blocklist') }}
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
      <pool-contact-form :pool-list-id="queryParams.customerListID || 0" :pool-lists="isPoolRoute ? firstLevelPoolLists : []"
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
          <b-field v-for="target in poolAssignTargets" :key="target.poolID" :label="target.name || $t('pool.assignTargetLabel')">
            <b-select v-model.number="target.allocationID" expanded data-cy="pool-assign-allocation">
              <option v-for="allocation in target.allocations" :key="allocation.id" :value="Number(allocation.id)">
                {{ allocation.listName || allocation.list_name || allocation.organizationName || allocation.organization_name }}
              </option>
            </b-select>
            <p v-if="!target.allocations.length" class="help is-danger">{{ $t('pool.noAllocations') }}</p>
          </b-field>
        </section>
        <footer class="modal-card-foot has-text-right">
          <b-button @click="isPoolAssignVisible = false">{{ $t('globals.buttons.cancel') }}</b-button>
          <b-button type="is-primary" :disabled="!canAssignSelectedPoolContacts" @click="assignPoolContacts"
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
      isEditing: false,
      isFormVisible: false,
      isBulkListFormVisible: false,

      // Pool lists (first-level public pools and their organization
      // allocations) keep their contacts in a separate store and route.
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
      poolAllocationsByPool: {},
      poolFilterOptions: [],

      // Pool contact mutations.
      poolBulk: {
        checked: [],
      },
      poolPendingContacts: [],
      poolRemovalReason: '',
      poolAssignTargets: [],
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
        search: '',

        // ID of the customerList the current customer view is filtered by.
        customerListID: null,
        page: 1,
        orderBy: 'id',
        order: 'desc',
        poolStatus: 'active',
        poolID: 0,
        poolDepartment: null,
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

    // Loads one server-paginated page from either the all-pool landing view
    // or one selected pool. The API masks e-mail addresses by permission.
    loadPoolContacts(params = {}) {
      const id = this.queryParams.customerListID;
      if (!this.isPoolRoute && !id) {
        return Promise.resolve();
      }
      this.pool = { ...this.pool, ...params };
      this.poolLoading = true;
      this.poolBulk.checked = [];
      this.poolAllocationsByPool = {};

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
      if (this.isPoolList) {
        qp.status = this.queryParams.poolStatus;
      }
      if (this.isPoolRoute) {
        if (this.queryParams.poolID > 0) {
          qp.pool_id = this.queryParams.poolID;
        }
        if (this.queryParams.poolDepartment !== null) {
          qp.allocation_department = this.queryParams.poolDepartment;
        }
      }

      const request = this.isPoolRoute
        ? this.$api.getAllPoolContacts(qp)
        : this.$api.getPoolContacts(id, qp);
      return request.then((resp) => {
        // Tolerate the legacy bare-array response shape.
        const results = Array.isArray(resp) ? resp : (resp.results || []);
        this.pool.results = results;
        this.pool.total = Array.isArray(resp) ? results.length : (Number(resp.total) || 0);
        const perPage = Number(resp.perPage || resp.per_page);
        if (perPage > 0) {
          this.pool.perPage = perPage;
        }
        if (this.isPoolRoute && this.canManagePoolContacts) {
          const ids = [...new Set(results.map((contact) => this.poolContactListID(contact)))];
          return Promise.all(ids.map((poolID) => this.$api.getOrgPoolAllocations(poolID).then((allocations) => {
            this.$set(this.poolAllocationsByPool, poolID, allocations || []);
          })));
        }
        return null;
      }).finally(() => {
        this.poolLoading = false;
      });
    },

    loadPoolFilters() {
      return this.$api.getAllPoolContactFilters().then((options) => {
        this.poolFilterOptions = Array.isArray(options) ? options : [];
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

    setPoolStatus(status) {
      const nextStatus = status === 'removed' ? 'removed' : 'active';
      const routeStatus = this.$route.query.pool_status === 'removed' ? 'removed' : 'active';
      if (this.queryParams.poolStatus === nextStatus && routeStatus === nextStatus) {
        return;
      }
      // App.vue keys the router view by fullPath. Replacing the query remounts
      // this view and loads page one with the selected status exactly once.
      this.$router.replace({
        query: { ...this.$route.query, pool_status: nextStatus },
      });
    },

    setPoolFacet(key, value) {
      const query = { ...this.$route.query };
      if (value === 'all') {
        delete query[key];
      } else {
        query[key] = key === 'allocation_department'
          ? String(value).slice('department:'.length) : String(value);
      }
      if (query[key] === this.$route.query[key]) {
        return;
      }
      this.$router.replace({ query });
    },

    exportPoolContacts() {
      const id = this.queryParams.customerListID;
      if (!this.isPoolRoute && !id) {
        return;
      }
      const selected = [...this.poolBulk.checked];
      this.$utils.confirm(this.$t('pool.confirmExport', { num: selected.length || this.pool.total }), () => {
        const q = new URLSearchParams();
        selected.forEach((contact) => {
          q.append('contact', `${this.isPoolRoute ? this.poolContactListID(contact) : 0}:${contact.id}`);
        });
        if (this.pool.search) {
          q.append('search', this.pool.search);
        }
        if (this.pool.orderBy) {
          q.append('order_by', this.pool.orderBy);
        }
        if (this.pool.order) {
          q.append('order', this.pool.order);
        }
        if (this.isPoolList) {
          q.append('status', this.queryParams.poolStatus);
        }
        if (this.isPoolRoute) {
          if (this.queryParams.poolID > 0) {
            q.append('pool_id', this.queryParams.poolID);
          }
          if (this.queryParams.poolDepartment !== null) {
            q.append('allocation_department', this.queryParams.poolDepartment);
          }
        }
        const path = this.isPoolRoute
          ? '/api/pools/contacts/export'
          : `/api/customer-lists/${id}/pool-contacts/export`;
        q.set('lang', this.$i18n.locale);
        q.set('organization_id', this.workspace.organizationId || 0);
        document.location.href = `${path}?${q.toString()}`;
      });
    },

    // Removal and assignment target either the clicked row or, with a null
    // argument, the current page selection.
    showPoolRemovalForm(contact) {
      this.poolPendingContacts = contact ? [contact] : this.poolBulk.checked.filter((c) => !c.excluded && this.poolContactAllocationID(c));
      this.poolRemovalReason = '';
      this.isPoolRemovalVisible = true;
    },

    removePoolContacts() {
      if (this.poolPendingContacts.length === 0) {
        return;
      }
      const reason = this.poolRemovalReason.trim();
      const targets = this.poolPendingContacts;
      const calls = targets.map((contact) => this.$api.removePoolContact({
        allocation_id: this.poolContactAllocationID(contact),
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
      const targets = contact ? [contact] : this.poolBulk.checked.filter((c) => c.excluded && this.poolContactRestoreAllocationID(c));
      if (targets.length === 0) {
        return;
      }
      const calls = targets.map((c) => this.$api.restorePoolContact({
        allocation_id: this.poolContactRestoreAllocationID(c),
        contact_id: c.id,
      }));
      Promise.all(calls).then(() => {
        this.loadPoolContacts();
        this.$utils.toast(this.$t('pool.toastContactsRestored', { num: targets.length }));
      });
    },

    clearPoolContactEmail(contact) {
      this.$utils.confirm(this.$t('pool.confirmArchiveInvalid'), () => {
        const listID = this.isPoolRoute
          ? this.poolRowValue(contact, 'poolId', 'pool_id') : this.queryParams.customerListID;
        this.$api.clearPoolContactEmail(listID, contact.id).then(() => {
          this.loadPoolContacts();
          this.$utils.toast(this.$t('pool.toastInvalidArchived'));
        });
      });
    },

    clearPoolContactsEmail() {
      const targets = this.poolBulk.checked.filter((c) => c.email);
      if (targets.length === 0) {
        return;
      }
      this.$utils.confirm(this.$t('pool.confirmArchiveInvalid'), () => {
        const calls = targets.map((c) => this.$api.clearPoolContactEmail(this.poolContactListID(c), c.id));
        Promise.all(calls).then(() => {
          this.loadPoolContacts();
          this.$utils.toast(this.$t('pool.toastInvalidArchived'));
        });
      });
    },

    deletePoolContact(contact) {
      this.deletePoolContacts([contact]);
    },

    deletePoolContacts(contacts = this.poolBulk.checked) {
      // Deletion removes the shared contact itself, including all memberships.
      const targets = [...new Map(contacts.map((contact) => [contact.id, contact])).values()];
      if (!this.canDeletePoolContacts || !targets.length) {
        return;
      }
      this.$utils.confirm(this.$t('pool.confirmDelete', { num: targets.length }), () => {
        Promise.all(targets.map((contact) => this.$api.deletePoolContact(this.poolContactListID(contact), contact.id))).then(() => {
          this.loadPoolContacts();
          this.$utils.toast(this.$t('pool.toastContactsDeleted', { num: targets.length }));
        });
      });
    },

    showPoolAssignForm(contact) {
      this.poolPendingContacts = contact ? [contact] : [...this.poolBulk.checked];
      const groups = new Map();
      this.poolPendingContacts.forEach((c) => {
        const poolID = this.poolContactListID(c);
        if (groups.has(poolID)) return;
        const allocations = this.isPoolRoute ? (this.poolAllocationsByPool[poolID] || []) : this.poolAllocations;
        groups.set(poolID, {
          poolID,
          name: this.isPoolRoute ? this.poolRowValue(c, 'poolName', 'pool_name') : '',
          allocations,
          allocationID: allocations.length ? Number(allocations[0].id) : null,
        });
      });
      this.poolAssignTargets = [...groups.values()];
      this.isPoolAssignVisible = true;
    },

    assignPoolContacts() {
      if (!this.canAssignSelectedPoolContacts || this.poolPendingContacts.length === 0) {
        return;
      }
      const targets = this.poolPendingContacts;
      const calls = targets.map((c) => this.$api.assignPoolContact({
        allocation_id: this.poolAssignTargets.find((target) => target.poolID === this.poolContactListID(c)).allocationID,
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
      if (contact.status === 'blocklisted') {
        return this.$t('customers.status.blocklisted');
      }
      if (contact.excluded) {
        return this.$t((this.isFirstLevelPool || this.isPoolRoute) && this.isPlatformAdmin
          ? 'pool.statusRemovedGlobal' : 'pool.statusRemoved');
      }
      return contact.status === 'archived'
        ? this.$t('pool.statusArchived')
        : this.$t('pool.statusNormal');
    },

    poolContactListID(contact) {
      return this.isPoolRoute ? Number(this.poolRowValue(contact, 'poolId', 'pool_id')) : this.queryParams.customerListID;
    },

    poolContactAllocationID(contact) {
      if (this.isAllocationList) {
        return this.poolAllocationID;
      }
      const allocations = this.isPoolRoute ? (this.poolAllocationsByPool[this.poolContactListID(contact)] || []) : this.poolAllocations;
      const own = allocations.find(
        (allocation) => Number(allocation.organizationId || allocation.organization_id) === Number(this.workspace.organizationId),
      );
      return own ? Number(own.id) : null;
    },

    poolContactRestoreAllocationID(contact) {
      if (this.isAllocationList) {
        return this.poolAllocationID;
      }
      if (this.isPlatformAdmin) {
        return Number(this.poolRowValue(contact, 'exceptionAllocationId', 'exception_allocation_id')) || null;
      }
      return this.poolContactAllocationID(contact);
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
        if (this.shouldSwitchListRoute(known)) {
          return this.switchListRoute(known);
        }
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
        if (this.shouldSwitchListRoute(list)) {
          return this.switchListRoute(list);
        }
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

    shouldSwitchListRoute(list) {
      const isPool = list.type === 'pool' || list.type === 'org_pool_allocation';
      return (isPool && this.$route.name === 'customersCustomerList')
        || (!isPool && this.$route.name === 'poolListContacts');
    },

    switchListRoute(list) {
      const isPool = list.type === 'pool' || list.type === 'org_pool_allocation';
      return this.$router.replace({
        name: isPool ? 'poolListContacts' : 'customersCustomerList',
        params: { customerListID: this.queryParams.customerListID },
        query: this.$route.query,
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

    onSimpleQueryInput(v) {
      const q = v.trim();
      this.queryParams.page = 1;
      this.queryParams.search = q.toLowerCase();
    },

    onSubmit() {
      if (this.isPoolList) {
        if (this.isPoolRoute) {
          const search = (this.queryInput || '').trim();
          if (search !== (this.$route.query.search || '')) {
            const query = { ...this.$route.query };
            if (search) {
              query.search = search;
            } else {
              delete query.search;
            }
            this.$router.replace({ query });
            return;
          }
        }
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
        page: this.queryParams.page,
        subscription_status: this.queryParams.subStatus,
        order_by: this.queryParams.orderBy,
        order: this.queryParams.order,
      };

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
        // 'All' is selected, blocklist the current search results.
        fn = () => {
          this.$api.blocklistCustomersByFilter({
            all: this.queryParams.search.trim() === '',
            search: this.queryParams.search,
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

        q.set('lang', this.$i18n.locale);
        q.set('organization_id', this.workspace.organizationId || 0);
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
        // 'All' is selected, delete the current search results.
        fn = () => {
          this.$api.deleteCustomersByFilter({
            all: this.queryParams.search.trim() === '',
            search: this.queryParams.search,
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
        // 'All' is selected, perform on the current search results.
        data.subscription_status = this.queryParams.subStatus;
        fn = this.$api.addCustomersToListsByFilter;
      }

      fn(data).then(() => {
        this.queryCustomers();
        this.$utils.toast(this.$t('customers.listChangeApplied'));
      });
    },
  },

  computed: {
    ...mapState(['customers', 'customer_lists', 'loading', 'profile', 'workspace']),

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
      return this.$can('customers:export')
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
      return this.isPoolRoute || type === 'pool' || type === 'org_pool_allocation';
    },

    isPoolRoute() {
      return this.$route.name === 'poolContacts';
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

    firstLevelPoolLists() {
      return this.accessiblePoolLists.filter((list) => list.type === 'pool');
    },

    aggregatePoolOptions() {
      const pools = new Map();
      this.poolFilterOptions.forEach((option) => {
        const id = Number(this.poolRowValue(option, 'poolId', 'pool_id'));
        if (id > 0) {
          pools.set(id, { id, name: this.poolRowValue(option, 'poolName', 'pool_name') });
        }
      });
      return Array.from(pools.values()).sort((a, b) => a.name.localeCompare(b.name));
    },

    aggregateDepartmentOptions() {
      const departments = new Set(this.poolFilterOptions.map((option) => this.poolRowValue(option, 'allocationDepartment', 'allocation_department') || ''));
      return Array.from(departments).sort((a, b) => a.localeCompare(b));
    },

    canManagePoolContacts() {
      return this.$can('pools:manage');
    },

    canMaintainPoolMaster() {
      return this.$can('pools:master_manage');
    },

    canSelectPoolContacts() {
      return this.canManagePoolContacts || this.canMaintainPoolMaster || this.canDeletePoolContacts || this.canExportPoolContacts;
    },

    canAssignSelectedPoolContacts() {
      return this.poolAssignTargets.length > 0 && this.poolAssignTargets.every((target) => target.allocationID);
    },

    canDeletePoolContacts() {
      return this.canMaintainPoolMaster && (this.isFirstLevelPool || this.isPoolRoute);
    },

    isPlatformAdmin() {
      return Number(this.profile && this.profile.userRole && this.profile.userRole.id) === 1;
    },

    canExportPoolContacts() {
      return this.$can('pools:export');
    },

    hasRemovablePoolContacts() {
      return this.poolBulk.checked.some((contact) => !contact.excluded && this.poolContactAllocationID(contact));
    },

    hasRestorablePoolContacts() {
      return this.poolBulk.checked.some((contact) => contact.excluded && this.poolContactRestoreAllocationID(contact));
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
    if (this.$route.query.pool_status === 'removed') {
      this.queryParams.poolStatus = 'removed';
    }

    if (this.isPoolRoute) {
      const poolID = Number(this.$route.query.pool_id);
      this.queryParams.poolID = Number.isSafeInteger(poolID) && poolID > 0 ? poolID : 0;
      if (Object.prototype.hasOwnProperty.call(this.$route.query, 'allocation_department')) {
        this.queryParams.poolDepartment = String(this.$route.query.allocation_department || '');
      }
      this.queryInput = typeof this.$route.query.search === 'string' ? this.$route.query.search : '';
      this.pool.search = this.queryInput;
      this.loadPoolFilters();
      this.loadPoolContacts();
      return;
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
