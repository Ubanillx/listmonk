<template>
  <section class="pool-manager">
    <div class="pool-manager__intro">
      <div>
        <h3>{{ $t('pool.title') }}</h3>
        <p>{{ $t('pool.intro') }}</p>
      </div>
      <b-tag type="is-info" class="is-light">{{ $t('pool.primaryTag') }}</b-tag>
    </div>

    <section class="pool-manager__section" data-cy="pool-target-organization-panel">
      <div class="pool-manager__section-heading">
        <span class="pool-manager__section-number">1</span>
        <div>
          <h4>{{ $t('pool.stepSelectTitle') }}</h4>
          <p>{{ $t('pool.stepSelectHelp') }}</p>
        </div>
      </div>
      <div class="pool-manager__section-content">
        <b-field :label="$t('pool.targetOrganizationLabel')" label-position="on-border">
          <b-select v-model.number="targetOrganizationID" expanded data-cy="pool-target-organization">
            <option :value="null">{{ $t('pool.selectOrganizationPlaceholder') }}</option>
            <option v-for="organization in organizations" :key="organization.id" :value="organization.id">
              {{ organization.name }}
            </option>
          </b-select>
        </b-field>
      </div>
    </section>

    <section v-if="organizationID" class="pool-manager__section" data-cy="pool-secondary-list-panel">
      <div class="pool-manager__section-heading">
        <span class="pool-manager__section-number">2</span>
        <div>
          <h4>{{ $t('pool.secondaryTitle') }}</h4>
          <p>{{ $t('pool.secondaryHelp') }}</p>
        </div>
      </div>
      <div class="pool-manager__section-content">
        <div v-if="selectedSegment" class="pool-manager__segment-summary" data-cy="pool-segment-summary">
          <div>
            <span>{{ $t('pool.segmentNameLabel') }}</span>
            <strong>{{ selectedSegment.listName || selectedSegment.listId }}</strong>
          </div>
          <div>
            <span>{{ $t('pool.segmentOrganizationLabel') }}</span>
            <strong>{{ selectedSegment.organizationName || targetOrganizationName }}</strong>
          </div>
          <div>
            <span>{{ $t('pool.segmentMailboxLabel') }}</span>
            <strong>{{ selectedSegment.replyMailboxEmail || $t('pool.notConfigured') }}</strong>
          </div>
          <p class="help pool-manager__organization-note-text">
            <b-icon icon="information-outline" size="is-small" />
            {{ $t('pool.mailboxPlatformNote') }}
          </p>
        </div>

        <div v-else class="pool-manager__create-segment" data-cy="pool-segment-create">
          <div class="pool-manager__create-segment-title">
            <div>
              <span>{{ $t('pool.targetOrganizationLabel') }}</span>
              <strong>{{ targetOrganizationName }}</strong>
            </div>
            <small>{{ $t('pool.createBindsPool') }}</small>
          </div>
          <b-field :label="$t('pool.createNameLabel')" label-position="on-border">
            <b-input v-model.trim="newSegment.name" maxlength="200"
              :placeholder="$t('pool.createNamePlaceholder')" data-cy="pool-segment-name" />
          </b-field>
          <p class="help pool-manager__organization-note-text">{{ $t('pool.createPlatformNote') }}</p>
          <b-button type="is-primary" :loading="creatingSegment" :disabled="!newSegment.name"
            data-cy="create-pool-segment" @click="createSegment">
            {{ $t('pool.createAndBind') }}
          </b-button>
        </div>
      </div>
    </section>

    <section v-else class="pool-manager__empty-state pool-manager__empty-state--top"
      data-cy="pool-secondary-list-empty">
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
      segments: [],
      targetOrganizationID: null,
      targetOrganizationNameOverride: '',
      selectedSegmentID: null,
      creatingSegment: false,
      newSegment: { name: '' },
    };
  },

  computed: {
    ...mapState(['profile', 'organizations']),

    isPlatformAdmin() {
      return Number(this.profile && this.profile.userRole && this.profile.userRole.id) === 1;
    },

    organizationID() {
      return Number(this.targetOrganizationID) || 0;
    },

    targetOrganizationName() {
      if (this.targetOrganizationNameOverride) {
        return this.targetOrganizationNameOverride;
      }
      const org = (this.organizations || []).find((item) => Number(item.id) === this.organizationID);
      return org ? org.name : this.$t('pool.organizationFallback', { id: this.organizationID });
    },

    selectedSegment() {
      return this.segments.find((segment) => Number(segment.id) === Number(this.selectedSegmentID));
    },
  },

  watch: {
    targetOrganizationID() {
      this.selectedSegmentID = null;
      this.targetOrganizationNameOverride = '';
      this.loadTargetOrganization();
    },
  },

  methods: {
    loadSegments() {
      return this.$api.getPoolSegments(this.pool.id).then((rows) => {
        this.segments = Array.isArray(rows) ? rows : [];
        const current = this.segments.find((segment) => Number(segment.organizationId || segment.organization_id) === this.organizationID);
        this.selectedSegmentID = current ? current.id : null;
      });
    },

    loadTargetOrganization() {
      if (!this.organizationID) {
        this.segments = [];
        return Promise.resolve();
      }
      return this.$api.getPoolManagementTarget(this.pool.id, this.organizationID)
        .then((target) => {
          this.targetOrganizationNameOverride = target.organizationName || target.organization_name || '';
          return this.loadSegments();
        });
    },

    createSegment() {
      if (!this.organizationID || !this.newSegment.name) {
        return Promise.resolve();
      }
      this.creatingSegment = true;
      return this.$api.createPoolSegment({
        pool_id: this.pool.id,
        organization_id: this.organizationID,
        name: this.newSegment.name,
      }).then(() => {
        this.newSegment = { name: '' };
        this.$utils.toast(this.$t('pool.toastCreated'));
        return this.loadTargetOrganization();
      }).finally(() => {
        this.creatingSegment = false;
      });
    },
  },

  mounted() {
    this.loadSegments();
  },
});
</script>
