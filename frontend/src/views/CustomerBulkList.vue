<template>
  <form @submit.prevent="onSubmit">
    <div class="modal-card" style="width: auto">
      <header class="modal-card-head">
        <h4 class="title is-size-5">
          {{ $t('customers.manageLists') }}
        </h4>
      </header>

      <section expanded class="modal-card-body">
        <b-field :label="$t('customers.action')">
          <div>
            <b-radio v-model="form.action" name="action" native-value="add" data-cy="check-customer_list-add">
              {{ $t('globals.buttons.add') }}
            </b-radio>
            <b-radio v-model="form.action" name="action" native-value="remove" data-cy="check-customer_list-remove">
              {{ $t('globals.buttons.remove') }}
            </b-radio>
            <b-radio v-model="form.action" name="action" native-value="unsubscribe" data-cy="check-customer_list-unsubscribe">
              {{ $t('customers.markUnsubscribed') }}
            </b-radio>
          </div>
        </b-field>

        <customer-list-selector :label="$t('globals.terms.customer_lists')" :placeholder="$t('customers.listsPlaceholder')" v-model="form.customer_lists" :selected="form.customer_lists"
          :all="customer_lists.results" />

        <b-field :message="$t('customers.preconfirmHelp')">
          <b-checkbox v-model="form.preconfirm" data-cy="preconfirm" :native-value="true" :disabled="!hasOptinList">
            {{ $t('customers.preconfirm') }}
          </b-checkbox>
        </b-field>
      </section>

      <footer class="modal-card-foot has-text-right">
        <b-button @click="$parent.close()">
          {{ $t('globals.buttons.close') }}
        </b-button>
        <b-button native-type="submit" type="is-primary" :disabled="form.customer_lists.length === 0">
          {{ $t('globals.buttons.save') }}
        </b-button>
      </footer>
    </div>
  </form>
</template>

<script>
import Vue from 'vue';
import { mapState } from 'vuex';
import CustomerListSelector from '../components/CustomerListSelector.vue';

export default Vue.extend({
  components: {
    CustomerListSelector,
  },

  props: {
    numCustomers: { type: Number, default: 0 },
  },

  data() {
    return {
      // Binds form input values.
      form: {
        action: 'add',
        customer_lists: [],
        preconfirm: false,
      },
    };
  },

  methods: {
    onSubmit() {
      this.$emit('finished', this.form.action, this.form.preconfirm, this.form.customer_lists);
      this.$parent.close();
    },
  },

  computed: {
    ...mapState(['customer_lists', 'loading']),

    hasOptinList() {
      return this.form.customer_lists.some((l) => l.optin === 'double');
    },
  },
});
</script>
