<template>
  <section data-cy="pending-resources">
    <div class="is-flex is-justify-content-space-between is-align-items-center mb-4">
      <h2 class="title is-5 mb-0">{{ $t('organizations.pendingPreview') }}</h2>
      <b-button size="is-small" icon-left="refresh" :loading="busy" :disabled="transferring" @click="load(page)">
        {{ $t('maintenance.refreshPreview') }}
      </b-button>
    </div>
    <p v-if="busy" role="status">{{ $t('maintenance.previewLoading') }}</p>
    <b-notification v-else-if="error" type="is-warning" :closable="false" role="alert">
      {{ $t('maintenance.previewError') }}
    </b-notification>
    <template v-else-if="preview">
      <p v-if="preview.total === 0" class="notification is-light">{{ $t('organizations.noPendingResources') }}</p>
      <template v-else>
        <p class="mb-3"><strong>{{ $t('organizations.pendingTotal', { count: preview.total }) }}</strong></p>
        <div class="tags mb-4">
          <span v-for="kind in kinds" :key="kind" class="tag is-light">{{ kindLabel(kind) }} {{ countFor(kind) }}</span>
        </div>
      </template>
    </template>
    <div class="columns is-vcentered mt-4">
      <div class="column is-7">
        <b-field :label="$t('organizations.receiveMember')" label-position="on-border">
          <b-select v-model.number="targetID" expanded :disabled="transferring" data-cy="pending-recipient">
            <option :value="null">{{ $t('organizations.selectMember') }}</option>
            <option v-for="member in members" :key="member.userId" :value="member.userId">{{ member.username }}</option>
          </b-select>
        </b-field>
      </div>
      <div class="column">
        <b-button type="is-primary" :loading="transferring" :disabled="!canTransfer" icon-left="swap-horizontal" @click="transfer" data-cy="pending-transfer">
          {{ $t('organizations.transferResources') }}
        </b-button>
      </div>
    </div>
    <template v-if="preview && preview.total > 0">
      <p class="help mb-4">{{ $t('organizations.pendingMergeHelp') }}</p>
      <b-table :data="preview.results" :mobile-cards="true">
          <b-table-column v-slot="props" :label="$t('organizations.resourceKind')">{{ kindLabel(props.row.kind) }}</b-table-column>
          <b-table-column v-slot="props" :label="$t('globals.fields.name')">
            <span class="resource-name">{{ props.row.name || `#${props.row.id}` }}</span>
            <span v-if="props.row.name" class="has-text-grey is-size-7"> #{{ props.row.id }}</span>
          </b-table-column>
          <b-table-column v-slot="props" :label="$t('organizations.originalOwner')">
            {{ props.row.originalOwnerName || (props.row.originalOwnerUserId ? `#${props.row.originalOwnerUserId}` : $t('organizations.ownerUnavailable')) }}
          </b-table-column>
          <b-table-column v-slot="props" :label="$t('organizations.pendingSince')">{{ $utils.niceDate(props.row.pendingAt, true) }}</b-table-column>
      </b-table>
      <b-pagination v-if="preview.total > preview.perPage" :total="preview.total" :per-page="preview.perPage" :current="page"
        v-bind="paginationLabels" @change="load" class="mt-4" />
    </template>
  </section>
</template>

<script>
export default {
  props: {
    organizationId: { type: Number, required: true },
    organizationName: { type: String, default: '' },
    members: { type: Array, default: () => [] },
  },
  data() {
    return {
      preview: null,
      busy: false,
      error: false,
      revision: 0,
      page: 1,
      targetID: null,
      transferring: false,
      kinds: ['customer_lists', 'customers', 'campaigns', 'templates', 'media'],
    };
  },
  computed: {
    paginationLabels() {
      return { ariaNextLabel: this.$t('globals.buttons.nextPage'), ariaPreviousLabel: this.$t('globals.buttons.previousPage') };
    },
    canTransfer() {
      return !this.busy && !this.error && !this.transferring && this.preview && this.preview.total > 0
        && this.members.some((member) => member.userId === this.targetID);
    },
  },
  watch: {
    organizationId: { immediate: true, handler() { this.targetID = null; this.load(1); } },
  },
  beforeDestroy() { this.revision += 1; },
  methods: {
    kindLabel(kind) { return this.$t(`organizations.resourceKinds.${kind}`); },
    countFor(kind) { return ((this.preview.counts || []).find((row) => row.kind === kind) || {}).count || 0; },
    async load(page = 1) {
      this.revision += 1;
      const { revision } = this;
      this.preview = null;
      this.busy = true;
      this.error = false;
      try {
        const preview = await this.$api.previewPendingOrganizationResources(this.organizationId, page);
        if (revision !== this.revision) return null;
        this.preview = preview;
        this.page = preview.page;
        return preview;
      } catch (error) {
        if (revision === this.revision) this.error = true;
        return null;
      } finally {
        if (revision === this.revision) this.busy = false;
      }
    },
    async transfer() {
      if (!this.canTransfer) return;
      const organizationID = this.organizationId;
      const { targetID } = this;
      const target = this.members.find((member) => member.userId === targetID);
      this.transferring = true;
      const preview = await this.load(this.page);
      if (!preview || !preview.total || organizationID !== this.organizationId) { this.transferring = false; return; }
      const summary = this.kinds.map((kind) => `${this.kindLabel(kind)} ${this.countFor(kind)}`).join(' / ');
      const message = this.$t('organizations.confirmPendingPreview', { organization: this.organizationName || `#${organizationID}`, target: target.username, summary });
      this.$utils.confirm(message, async () => {
        try {
          await this.$api.transferPendingOrganizationResources({ target_user_id: targetID }, organizationID);
          this.$utils.toast(this.$t('globals.messages.done'));
          this.targetID = null;
          this.$emit('changed');
        } finally {
          this.transferring = false;
          if (organizationID === this.organizationId) await this.load(1);
        }
      }, () => { this.transferring = false; }, { title: this.$t('organizations.confirmTransferTitle'), confirmText: this.$t('organizations.confirmTransferTitle'), focusOn: 'cancel' });
    },
  },
};
</script>

<style scoped>
.resource-name { overflow-wrap: anywhere; }
</style>
