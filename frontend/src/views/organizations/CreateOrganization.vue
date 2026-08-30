<template>
  <section class="organizations">
    <header class="columns page-header">
      <div class="column">
        <h1 class="title is-4"><b-icon icon="file-document-edit-outline" size="is-small" />创建组织</h1>
      </div>
    </header>

    <section class="section-mini mb-6">
      <form @submit.prevent="submitOrganizationRequest">
        <b-field label="组织名称" label-position="on-border">
          <b-input ref="nameInput" v-model.trim="requestForm.name" maxlength="200" required />
        </b-field>
        <b-field label="说明" label-position="on-border">
          <b-input v-model.trim="requestForm.description" type="textarea" maxlength="2000" />
        </b-field>
        <b-button native-type="submit" type="is-primary" icon-left="file-send-outline">提交申请</b-button>
      </form>
    </section>

    <section>
      <h2 class="title is-5"><b-icon icon="clipboard-text-outline" size="is-small" />我的申请</h2>
      <b-table :data="requests" :mobile-cards="false" narrowed>
        <b-table-column v-slot="props" field="requestedName" label="组织">
          <strong>{{ props.row.requestedName }}</strong>
          <p v-if="props.row.description" class="has-text-grey is-size-7">{{ props.row.description }}</p>
        </b-table-column>
        <b-table-column v-slot="props" field="status" label="状态">
          <b-tag :type="statusType(props.row.status)">{{ statusLabel(props.row.status) }}</b-tag>
        </b-table-column>
        <b-table-column v-slot="props" field="createdAt" label="申请时间">
          {{ $utils.niceDate(props.row.createdAt, true) }}
        </b-table-column>
        <b-table-column v-slot="props" field="reviewNote" label="处理说明">
          {{ props.row.reviewNote || '-' }}
        </b-table-column>
        <b-table-column v-slot="props" label="操作" numeric>
          <b-button v-if="props.row.status === 'pending'" size="is-small" type="is-text" icon-left="undo-variant"
            @click="withdrawRequest(props.row)">
            撤回
          </b-button>
          <b-button v-if="props.row.status === 'rejected'" size="is-small" type="is-text" icon-left="content-copy"
            @click="copyRejectedRequest(props.row)">
            复制后重新提交
          </b-button>
        </b-table-column>
        <template #empty><span class="has-text-grey">尚未提交组织创建申请</span></template>
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
      this.$utils.toast('组织创建申请已提交');
    },

    withdrawRequest(request) {
      this.$utils.confirm(`确认撤回“${request.requestedName}”创建申请？`, async () => {
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
        pending: '待处理',
        approved: '已确认',
        rejected: '已拒绝',
        withdrawn: '已撤回',
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
