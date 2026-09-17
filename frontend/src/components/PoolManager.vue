<template>
  <section class="pool-manager">
    <div class="pool-manager__intro">
      <div>
        <h3>{{ $t('pool.title') }}</h3>
        <p>{{ isPlatformAdmin ? $t('pool.intro') : $t('pool.introManager') }}</p>
      </div>
      <b-tag type="is-info" class="is-light">{{ $t('pool.primaryTag') }}</b-tag>
    </div>

    <section class="pool-manager__section" data-cy="pool-target-organization-panel">
      <div class="pool-manager__section-heading">
        <span class="pool-manager__section-number">1</span>
        <div>
          <h4>{{ isPlatformAdmin ? $t('pool.stepSelectTitle') : $t('pool.currentOrganization') }}</h4>
          <p v-if="isPlatformAdmin">{{ $t('pool.stepSelectHelp') }}</p>
        </div>
      </div>
      <div class="pool-manager__section-content">
        <b-field v-if="isPlatformAdmin" :label="$t('pool.targetOrganizationLabel')" label-position="on-border">
          <b-select v-model.number="targetOrganizationID" expanded data-cy="pool-target-organization">
            <option :value="null">{{ $t('pool.selectOrganizationPlaceholder') }}</option>
            <option v-for="organization in organizations" :key="organization.id" :value="organization.id">
              {{ organization.name }}
            </option>
          </b-select>
        </b-field>

        <!-- Organization admins are bound to the organization of the workspace
             they are currently in, so the target is shown read-only. -->
        <div v-else class="pool-manager__allocation-summary" data-cy="pool-current-organization">
          <div>
            <span>{{ $t('pool.targetOrganizationLabel') }}</span>
            <strong>{{ currentOrganizationName }}</strong>
          </div>
        </div>
      </div>
    </section>

    <section v-if="organizationID" class="pool-manager__section" data-cy="pool-allocation-panel">
      <div class="pool-manager__section-heading">
        <span class="pool-manager__section-number">2</span>
        <div>
          <h4>{{ $t('pool.allocationTitle') }}</h4>
          <p>{{ $t('pool.allocationHelp') }}</p>
        </div>
      </div>
      <div class="pool-manager__section-content">
        <div v-if="selectedAllocation" class="pool-manager__allocation-summary" data-cy="org-pool-allocation-summary">
          <div>
            <span>{{ $t('pool.allocationNameLabel') }}</span>
            <strong>{{ selectedAllocation.listName || selectedAllocation.listId }}</strong>
          </div>
          <div>
            <span>{{ $t('pool.allocationOrganizationLabel') }}</span>
            <strong>{{ selectedAllocation.organizationName || targetOrganizationName }}</strong>
          </div>
        </div>

        <div v-if="!selectedAllocation" class="pool-manager__create-allocation" data-cy="org-pool-allocation-create">
          <div class="pool-manager__create-allocation-title">
            <div>
              <span>{{ $t('pool.targetOrganizationLabel') }}</span>
              <strong>{{ targetOrganizationName }}</strong>
            </div>
            <small>{{ $t('pool.createBindsPool') }}</small>
          </div>
          <b-field :label="$t('pool.createNameLabel')" label-position="on-border">
            <b-input v-model.trim="newAllocation.name" maxlength="200"
              :placeholder="$t('pool.createNamePlaceholder')" data-cy="org-pool-allocation-name" />
          </b-field>
          <p class="help pool-manager__organization-note-text">{{ $t('pool.createPlatformNote') }}</p>
          <b-button type="is-primary" :loading="creatingAllocation" :disabled="!newAllocation.name"
            data-cy="create-org-pool-allocation" @click="createAllocation">
            {{ $t('pool.createAndBind') }}
          </b-button>
        </div>
      </div>
    </section>

    <section v-else class="pool-manager__empty-state pool-manager__empty-state--top"
      data-cy="pool-allocation-empty">
      <b-icon icon="account-group-outline" size="is-medium" />
      <div>
        <strong>{{ $t('pool.noOrgTitle') }}</strong>
        <p>{{ $t('pool.noOrgHelp') }}</p>
      </div>
    </section>
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
      targetOrganizationNameOverride: '',
      selectedAllocationID: null,
      creatingAllocation: false,
      newAllocation: { name: '' },
    };
  },

  computed: {
    ...mapState(['profile', 'organizations', 'workspace']),

    isPlatformAdmin() {
      return Number(this.profile && this.profile.userRole && this.profile.userRole.id) === 1;
    },

    // Organization admins may only split a pool inside the organization of the
    // workspace they are currently in: the backend rejects any other
    // organization. Highest administrators keep the cross-organization
    // selector, so the target stays null for them until one is picked.
    fixedOrganizationID() {
      if (this.isPlatformAdmin) {
        return 0;
      }
      return Number(this.workspace && this.workspace.organizationId) || 0;
    },

    // Read-only name shown to organization admins, whose target organization is
    // fixed by their workspace.
    currentOrganizationName() {
      return this.workspace.organizationName
        || this.$t('pool.organizationFallback', { id: this.fixedOrganizationID });
    },

    organizationID() {
      if (!this.isPlatformAdmin) {
        return this.fixedOrganizationID;
      }
      return Number(this.targetOrganizationID) || 0;
    },

    targetOrganizationName() {
      if (!this.isPlatformAdmin) {
        return this.currentOrganizationName;
      }
      if (this.targetOrganizationNameOverride) {
        return this.targetOrganizationNameOverride;
      }
      const org = (this.organizations || []).find((item) => Number(item.id) === this.organizationID);
      return org ? org.name : this.$t('pool.organizationFallback', { id: this.organizationID });
    },

    selectedAllocation() {
      return this.allocations.find((allocation) => Number(allocation.id) === Number(this.selectedAllocationID));
    },
  },

  watch: {
    targetOrganizationID() {
      this.selectedAllocationID = null;
      this.targetOrganizationNameOverride = '';
      this.loadTargetOrganization();
    },
  },

  methods: {
    loadAllocations() {
      return this.$api.getOrgPoolAllocations(this.pool.id).then((rows) => {
        this.allocations = Array.isArray(rows) ? rows : [];
        const current = this.allocations.find((allocation) => Number(allocation.organizationId || allocation.organization_id) === this.organizationID);
        this.selectedAllocationID = current ? current.id : null;
      });
    },

    loadTargetOrganization() {
      // Resolving an arbitrary organization's target is highest-administrator
      // only (`GET /api/pools/:id/management-target`). Organization admins are
      // fixed to their own workspace, so their own allocations are all that has to
      // be reloaded.
      if (!this.isPlatformAdmin) {
        return this.loadAllocations();
      }
      if (!this.organizationID) {
        this.allocations = [];
        return Promise.resolve();
      }
      return this.$api.getPoolManagementTarget(this.pool.id, this.organizationID)
        .then((target) => {
          this.targetOrganizationNameOverride = target.organizationName || target.organization_name || '';
          return this.loadAllocations();
        });
    },

    createAllocation() {
      if (!this.organizationID || !this.newAllocation.name) {
        return Promise.resolve();
      }
      this.creatingAllocation = true;
      return this.$api.createOrgPoolAllocation({
        pool_id: this.pool.id,
        organization_id: this.organizationID,
        name: this.newAllocation.name,
      }).then(() => {
        this.newAllocation = { name: '' };
        this.$utils.toast(this.$t('pool.toastCreated'));
        return this.loadTargetOrganization();
      }).finally(() => {
        this.creatingAllocation = false;
      });
    },
  },

  mounted() {
    this.loadTargetOrganization();
  },
});
</script>
