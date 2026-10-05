<template>
  <section class="pool-manager pool-bindings">
    <h3 class="pool-bindings__title">{{ $t('pool.bindingTitle') }}</h3>
    <div class="pool-bindings__grid">
      <aside class="pool-bindings__organizations" data-cy="pool-target-organization-panel">
        <h4>{{ $t('pool.organizationsTitle', { count: availableOrganizations.length }) }}</h4>
        <b-input v-if="isPlatformAdmin" v-model="organizationSearch" type="search" icon="magnify"
          :placeholder="$t('pool.searchOrganizations')" :aria-label="$t('pool.searchOrganizations')"
          data-cy="pool-organization-search" />
        <div class="pool-bindings__organization-list">
          <button v-for="organization in filteredOrganizations" :key="organization.id" type="button"
            class="pool-bindings__organization" :class="{ 'is-selected': Number(organization.id) === organizationID }"
            :aria-pressed="Number(organization.id) === organizationID" :disabled="creatingAllocation"
            :data-organization-id="organization.id" data-cy="pool-organization-option"
            @click="targetOrganizationID = Number(organization.id)">
            <span class="pool-bindings__organization-name">{{ organization.name }}</span>
            <span v-if="!loadingAllocations && !loadError" :class="['pool-bindings__status', { 'is-bound': allocationFor(organization.id) }]">
              {{ $t(allocationFor(organization.id) ? 'pool.bound' : 'pool.unbound') }}
            </span>
          </button>
          <p v-if="!filteredOrganizations.length" class="help">{{ $t('pool.noOrganizations') }}</p>
        </div>
      </aside>

      <section class="pool-bindings__detail" data-cy="pool-allocation-panel" aria-live="polite">
        <p v-if="loadingAllocations" class="help">{{ $t('pool.loadingBindings') }}</p>
        <div v-else-if="loadError">
          <p class="help has-text-danger">{{ $t('pool.loadBindingsError') }}</p>
          <b-button native-type="button" @click="loadAllocations" data-cy="pool-bindings-retry">{{ $t('globals.buttons.retry') }}</b-button>
        </div>
        <template v-else-if="organizationID">
          <div class="pool-bindings__detail-heading">
            <h4 data-cy="pool-current-organization">{{ targetOrganizationName }}</h4>
            <span :class="['pool-bindings__status', { 'is-bound': selectedAllocation }]">
              {{ $t(selectedAllocation ? 'pool.bound' : 'pool.unbound') }}
            </span>
          </div>
          <div v-if="selectedAllocation" class="pool-bindings__summary" data-cy="org-pool-allocation-summary">
            <span>{{ $t('pool.allocationNameLabel') }}</span>
            <strong>{{ selectedAllocation.listName || selectedAllocation.listId }}</strong>
            <router-link v-if="$can('pools:get')" :to="'/pool-lists/' + selectedAllocation.listId + '/contacts'" data-cy="pool-bound-contacts">
              {{ $t('pool.viewAllocationContacts') }}
            </router-link>
          </div>
          <div v-else data-cy="org-pool-allocation-create">
            <p class="help pool-bindings__hint">{{ $t('pool.bindingHelp') }}</p>
            <b-field :label="$t('pool.createNameLabel')" label-position="on-border">
              <b-input v-model.trim="allocationName" maxlength="200" :disabled="creatingAllocation"
                data-cy="org-pool-allocation-name" />
            </b-field>
            <b-button type="is-primary" :loading="creatingAllocation" :disabled="creatingAllocation || !allocationName"
              native-type="button" data-cy="create-org-pool-allocation" @click="createAllocation">
              {{ $t('pool.createAndBind') }}
            </b-button>
          </div>
        </template>
        <p v-else class="help" data-cy="pool-allocation-empty">{{ $t('pool.noOrganizations') }}</p>
      </section>
    </div>
  </section>
</template>

<script>
import Vue from 'vue';
import { mapState } from 'vuex';

export default Vue.extend({
  name: 'PoolManager',
  props: {
    pool: { type: Object, required: true },
  },
  data() {
    return {
      allocations: [],
      targetOrganizationID: null,
      organizationSearch: '',
      allocationNames: {},
      loadingAllocations: true,
      loadError: false,
      creatingAllocation: false,
    };
  },
  computed: {
    ...mapState(['profile', 'organizations', 'workspace']),

    isPlatformAdmin() {
      return Number(this.profile && this.profile.userRole && this.profile.userRole.id) === 1;
    },

    availableOrganizations() {
      if (this.isPlatformAdmin) {
        return (this.organizations || []).filter((organization) => !organization.status || organization.status === 'active');
      }
      const id = Number(this.workspace.organizationId);
      return id > 0 ? [{ id, name: this.workspace.organizationName || this.$t('pool.organizationFallback', { id }) }] : [];
    },

    filteredOrganizations() {
      const search = this.organizationSearch.trim().toLowerCase();
      return this.availableOrganizations.filter((organization) => organization.name.toLowerCase().includes(search));
    },

    organizationID() {
      return this.isPlatformAdmin ? Number(this.targetOrganizationID) || 0 : Number(this.workspace.organizationId) || 0;
    },

    targetOrganizationName() {
      const organization = this.availableOrganizations.find((item) => Number(item.id) === this.organizationID);
      return organization ? organization.name : '';
    },

    selectedAllocation() {
      return this.allocationFor(this.organizationID);
    },

    allocationName: {
      get() {
        const draft = this.allocationNames[this.organizationID];
        return draft === undefined ? this.$t('pool.defaultAllocationName', { organization: this.targetOrganizationName }) : draft;
      },
      set(value) {
        this.$set(this.allocationNames, this.organizationID, value);
      },
    },
  },
  methods: {
    allocationFor(organizationID) {
      return this.allocations.find((allocation) => Number(allocation.organizationId || allocation.organization_id) === Number(organizationID));
    },

    loadAllocations() {
      this.loadingAllocations = true;
      this.loadError = false;
      return this.$api.getOrgPoolAllocations(this.pool.id).then((rows) => {
        this.allocations = Array.isArray(rows) ? rows : [];
        if (!this.availableOrganizations.some((organization) => Number(organization.id) === this.organizationID)) {
          const current = this.availableOrganizations.find((organization) => Number(organization.id) === Number(this.workspace.organizationId));
          const initial = current || this.availableOrganizations[0];
          this.targetOrganizationID = initial ? Number(initial.id) : null;
        }
      }).catch(() => {
        this.loadError = true;
      }).finally(() => {
        this.loadingAllocations = false;
      });
    },

    createAllocation() {
      if (!this.organizationID || !this.allocationName || this.selectedAllocation || this.creatingAllocation || this.loadingAllocations || this.loadError) {
        return Promise.resolve();
      }
      this.creatingAllocation = true;
      return this.$api.createOrgPoolAllocation({
        pool_id: this.pool.id,
        organization_id: this.organizationID,
        name: this.allocationName,
      }).then(() => {
        this.$utils.toast(this.$t('pool.toastCreated'));
        return this.loadAllocations();
      }).catch(() => this.loadAllocations()).finally(() => {
        this.creatingAllocation = false;
      });
    },
  },
  mounted() {
    this.loadAllocations();
  },
});
</script>
