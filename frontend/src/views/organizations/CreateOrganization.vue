<template>
  <section class="organizations">
    <header class="columns page-header">
      <div class="column">
        <h1 class="title is-4">{{ $t('organizations.createTitle') }}</h1>
      </div>
    </header>

    <section class="section-mini mb-6">
      <form @submit.prevent="submitOrganizationRequest">
        <b-field :label="$t('organizations.name')" label-position="on-border">
          <b-input ref="nameInput" v-model.trim="requestForm.name" maxlength="200" required />
        </b-field>
        <b-field :label="$t('organizations.description')" label-position="on-border">
          <b-input v-model.trim="requestForm.description" type="textarea" maxlength="2000" />
        </b-field>
        <b-button native-type="submit" type="is-primary" icon-left="file-send-outline">{{ $t('organizations.submitRequest') }}</b-button>
      </form>
    </section>

    <section>
      <h2 class="title is-5">{{ $t('organizations.myRequests') }}</h2>
      <b-table :data="requests" :mobile-cards="false">
        <b-table-column v-slot="props" field="requestedName" :label="$t('organizations.columnOrg')">
          <strong>{{ props.row.requestedName }}</strong>
          <p v-if="props.row.description" class="has-text-grey is-size-7">{{ props.row.description }}</p>
        </b-table-column>
        <b-table-column v-slot="props" field="status" :label="$t('organizations.status')">
          <b-tag :type="statusType(props.row.status)">{{ statusLabel(props.row.status) }}</b-tag>
        </b-table-column>
        <b-table-column v-slot="props" field="createdAt" :label="$t('organizations.requestTime')">
          {{ $utils.niceDate(props.row.createdAt, true) }}
        </b-table-column>
        <b-table-column v-slot="props" field="reviewNote" :label="$t('organizations.requestNote')">
          {{ props.row.reviewNote || '-' }}
        </b-table-column>
        <b-table-column v-slot="props" :label="$t('organizations.columnActions')" numeric>
          <b-button v-if="props.row.status === 'pending'" size="is-small" type="is-text" icon-left="undo-variant"
            @click="withdrawRequest(props.row)">
            {{ $t('organizations.withdraw') }}
          </b-button>
          <b-button v-if="props.row.status === 'rejected'" size="is-small" type="is-text" icon-left="content-copy"
            @click="copyRejectedRequest(props.row)">
            {{ $t('organizations.resubmit') }}
          </b-button>
        </b-table-column>
        <template #empty><span class="has-text-grey">{{ $t('organizations.noCreateRequests') }}</span></template>
      </b-table>
    </section>
  </section>
</template>

<script>
import Vue from 'vue';

export default Vue.extend({
  data() {
    return {
      requests: [],
      requestForm: { name: '', description: '' },
    };
  },

  methods: {
    async refresh() {
      this.requests = await this.$api.getMyOrganizationRequests();
    },

    async submitOrganizationRequest() {
      await this.$api.createOrganizationRequest(this.requestForm);
      this.requestForm = { name: '', description: '' };
      await this.refresh();
      this.$utils.toast(this.$t('organizations.toastRequestSubmitted'));
    },

    withdrawRequest(request) {
      this.$utils.confirm(this.$t('organizations.confirmWithdraw', { name: request.requestedName }), async () => {
        await this.$api.withdrawOrganizationRequest(request.id);
        await this.refresh();
      });
    },

    copyRejectedRequest(request) {
      this.requestForm = {
        name: request.requestedName,
        description: request.description,
      };
      this.$nextTick(() => {
        const input = this.$refs.nameInput;
        if (input && input.focus) {
          input.focus();
        }
        window.scrollTo({ top: 0, behavior: 'smooth' });
      });
    },

    statusLabel(status) {
      const labels = {
        pending: this.$t('organizations.statusPendingRequest'),
        approved: this.$t('organizations.statusApproved'),
        rejected: this.$t('organizations.statusRejected'),
        withdrawn: this.$t('organizations.statusWithdrawn'),
      };
      return labels[status] || status;
    },

    statusType(status) {
      const types = {
        pending: 'is-warning',
        approved: 'is-success',
        rejected: 'is-danger',
        withdrawn: 'is-light',
      };
      return types[status] || 'is-light';
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
