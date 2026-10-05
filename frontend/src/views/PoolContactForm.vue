<template>
  <form class="pool-contact-form" @submit.prevent="onSubmit">
    <div class="modal-card" style="width: auto;">
      <header class="modal-card-head">
        <p class="modal-card-title">{{ $t('pool.formTitle') }}</p>
      </header>
      <section class="modal-card-body">
        <b-field v-if="poolLists.length" :label="$t('pool.tablePoolList')" required>
          <b-select v-model="selectedPoolID" expanded data-cy="pool-form-list">
            <option v-for="list in poolLists" :key="list.id" :value="list.id">{{ list.name }}</option>
          </b-select>
        </b-field>

        <b-field :label="$t('pool.tableCustomerCode')">
          <b-input v-model.trim="form.customer_code" data-cy="pool-form-customer-code" />
        </b-field>

        <b-field :label="$t('pool.tableName')">
          <b-input v-model.trim="form.name" data-cy="pool-form-name" />
        </b-field>

        <b-field :label="$t('pool.tableEmail')" required>
          <b-input v-model.trim="form.email" type="email" data-cy="pool-form-email" />
        </b-field>

        <b-field :label="$t('pool.tableReplyTo')" :message="$t('import.mapReplyToFieldHelp')">
          <b-input v-model.trim="form.reply_to" type="email" data-cy="pool-form-reply-to" />
        </b-field>

        <b-field :label="$t('pool.tableDepartment')"
          :message="$t('pool.formDepartmentHelp')">
          <b-input v-model.trim="form.allocation_department" data-cy="pool-form-department" />
        </b-field>
      </section>
      <footer class="modal-card-foot has-text-right">
        <b-button @click="$parent.close()">
          {{ $t('globals.buttons.cancel') }}
        </b-button>
        <b-button native-type="submit" type="is-primary" :loading="loading" :disabled="!form.email || !targetPoolID"
          data-cy="btn-save-pool-contact">
          {{ $t('globals.buttons.save') }}
        </b-button>
      </footer>
    </div>
  </form>
</template>

<script>
import Vue from 'vue';

export default Vue.extend({
  name: 'PoolContactForm',

  props: {
    poolListID: { type: Number, default: 0 },
    poolLists: { type: Array, default: () => [] },
  },

  data() {
    return {
      loading: false,
      selectedPoolID: this.poolLists.length ? this.poolLists[0].id : 0,
      form: {
        customer_code: '',
        name: '',
        email: '',
        reply_to: '',
        allocation_department: '',
      },
    };
  },

  computed: {
    targetPoolID() {
      return this.poolListID || Number(this.selectedPoolID);
    },
  },

  watch: {
    poolLists(lists) {
      if (!this.selectedPoolID && lists.length) {
        this.selectedPoolID = lists[0].id;
      }
    },
  },

  methods: {
    onSubmit() {
      if (!this.targetPoolID || !this.form.email) {
        return;
      }
      this.loading = true;
      this.$api.createPoolContact(this.targetPoolID, this.form).then(() => {
        this.$utils.toast(this.$t('pool.toastContactCreated'));
        this.$emit('finished');
        this.$parent.close();
      }).finally(() => {
        this.loading = false;
      });
    },
  },
});
</script>
