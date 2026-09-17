<template>
  <form class="pool-contact-form" @submit.prevent="onSubmit">
    <div class="modal-card" style="width: auto;">
      <header class="modal-card-head">
        <p class="modal-card-title">{{ $t('pool.formTitle') }}</p>
      </header>
      <section class="modal-card-body">
        <b-field :label="$t('customers.customerCode')">
          <b-input v-model.trim="form.customer_code" data-cy="pool-form-customer-code" />
        </b-field>

        <b-field :label="$t('pool.tableName')">
          <b-input v-model.trim="form.name" data-cy="pool-form-name" />
        </b-field>

        <b-field :label="$t('pool.tableEmail')" required>
          <b-input v-model.trim="form.email" type="email" data-cy="pool-form-email" />
        </b-field>

        <!-- A non-platform-admin always adds the contact to its own
             organization; the server pins the department in that case. -->
        <b-field v-if="isPlatformAdmin" :label="$t('pool.tableDepartment')"
          :message="$t('pool.formDepartmentHelp')">
          <b-input v-model.trim="form.allocation_department" data-cy="pool-form-department" />
        </b-field>
      </section>
      <footer class="modal-card-foot has-text-right">
        <b-button @click="$parent.close()">
          {{ $t('globals.buttons.cancel') }}
        </b-button>
        <b-button native-type="submit" type="is-primary" :loading="loading" :disabled="!form.email"
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
    isPlatformAdmin: { type: Boolean, default: false },
  },

  data() {
    return {
      loading: false,
      form: {
        customer_code: '',
        name: '',
        email: '',
        allocation_department: '',
      },
    };
  },

  methods: {
    onSubmit() {
      if (!this.poolListID || !this.form.email) {
        return;
      }
      this.loading = true;
      this.$api.createPoolContact(this.poolListID, this.form).then(() => {
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
