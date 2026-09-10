<template>
  <form @submit.prevent="onSubmit">
    <div class="modal-card content" style="width: auto">
      <header class="modal-card-head">
        <b-tag v-if="isEditing" :class="[data.status, 'is-pulled-right']">
          {{ $t(`customers.status.${data.status}`) }}
        </b-tag>
        <h4 v-if="isEditing">
          {{ data.name }}
        </h4>
        <h4 v-else>
          {{ $t('customers.newCustomer') }}
        </h4>

        <p v-if="isEditing" class="has-text-grey is-size-7">
          {{ $t('globals.fields.id') }}: <span data-cy="id"><copy-text :text="`${data.id}`" /></span>
          {{ $t('globals.fields.uuid') }}: <copy-text :text="data.uuid" />
        </p>
      </header>

      <section expanded class="modal-card-body">
        <b-field :label="$t('customers.customerCode')" label-position="on-border">
          <b-input :maxlength="200" v-model="form.customerCode" name="customer_code" :ref="'focus'" :disabled="!canEdit"
            :placeholder="$t('customers.customerCode')" required />
        </b-field>

        <b-field :label="$t('customers.email')" label-position="on-border">
          <b-input :maxlength="200" v-model="form.email" name="email" :disabled="!canEdit"
            :placeholder="$t('customers.email')" required />
        </b-field>

        <div class="columns">
          <div class="column is-8">
            <b-field :label="$t('customers.salutation')" label-position="on-border">
              <b-input :maxlength="200" v-model="form.name" name="name" :disabled="!canEdit"
                :placeholder="$t('customers.salutation')" />
            </b-field>
          </div>
          <div class="column is-4">
            <b-field :label="$t('globals.fields.status')" label-position="on-border"
              :message="$t('customers.blocklistedHelp')">
              <b-select v-model="form.status" name="status" :placeholder="$t('globals.fields.status')" :disabled="!canEdit" required
                expanded>
                <option value="enabled">
                  {{ $t('customers.status.enabled') }}
                </option>
                <option value="blocklisted">
                  {{ $t('customers.status.blocklisted') }}
                </option>
              </b-select>
            </b-field>
          </div>
        </div>

        <b-tabs type="is-boxed" :animated="false">
          <b-tab-item :label="$t('globals.terms.customer_lists')" label-position="on-border">
            <customer-list-selector :label="$t('customers.customer_lists')" :placeholder="$t('customers.listsPlaceholder')"
              :message="$t('customers.listsHelp')" v-model="form.customer_lists" :selected="form.customer_lists" :all="customer_lists.results"
              :disabled="!canEdit" />
            <div class="columns">
              <div class="column is-7">
                <b-field :message="$t('customers.preconfirmHelp')">
                  <b-checkbox v-model="form.preconfirm" :native-value="true" :disabled="!canEdit || !hasOptinList">
                    {{ $t('customers.preconfirm') }}
                  </b-checkbox>
                </b-field>
              </div>
              <div v-if="canEdit && isEditing" class="column is-5 has-text-right">
                <a href="#" @click.prevent="sendOptinConfirmation" :class="{ 'is-disabled': !hasOptinList }">
                  <b-icon icon="email-outline" size="is-small" />
                  {{ $t('customers.sendOptinConfirm') }}</a>
              </div>
            </div>
          </b-tab-item><!-- customer_lists -->

          <b-tab-item :label="`${$tc('globals.terms.subscriptions', 2)} (${data.customerLists ? data.customerLists.length : 0})`"
            label-position="on-border" :disabled="!data.customerLists || data.customerLists.length === 0">
            <template v-if="data.customerLists">
              <b-table :data="data.customerLists" hoverable default-sort="createdAt" class="subscriptions">
                <b-table-column v-slot="props" field="name" :label="$tc('globals.terms.customer_list', 1)">
                  <div>
                    <router-link :to="`/customer-lists/${props.row.id}`">
                      {{ props.row.name }}
                    </router-link>
                    <br />
                    <b-tag :class="props.row.optin" :data-cy="`optin-${props.row.optin}`">
                      <b-icon :icon="props.row.optin === 'double' ? 'account-check-outline' : 'account-off-outline'"
                        size="is-small" />
                      {{ ' ' }}
                      {{ $t(`customer_lists.optins.${props.row.optin}`) }}
                    </b-tag>{{ ' ' }}
                  </div>
                </b-table-column>

                <b-table-column v-slot="props" field="status" cell-class="status" :label="$t('globals.fields.status')">
                  <b-tag :class="`status-${props.row.subscriptionStatus}`">
                    {{ $t(`customers.status.${props.row.subscriptionStatus}`) }}
                  </b-tag>
                  <template v-if="props.row.optin === 'double' && props.row.subscriptionMeta.optinIp">
                    <br /><span class="is-size-7">{{ props.row.subscriptionMeta.optinIp }}</span>
                  </template>
                </b-table-column>

                <b-table-column v-slot="props" field="createdAt" :label="$t('globals.fields.createdAt')">
                  {{ $utils.niceDate(props.row.subscriptionCreatedAt, true) }}
                </b-table-column>

                <b-table-column v-slot="props" field="updatedAt" :label="$t('globals.fields.updatedAt')">
                  {{ $utils.niceDate(props.row.subscriptionCreatedAt, true) }}
                </b-table-column>
              </b-table>
            </template>
          </b-tab-item><!-- subscriptions -->

          <b-tab-item :label="`${$t('globals.terms.bounces')} (${bounces.length})`" class="bounces"
            :disabled="bounces.length === 0">
            <a href="#" class="is-size-6 is-pulled-right" disabed="true" @click.prevent="deleteBounces"
               v-if="isBounceVisible && canEdit">
              <b-icon icon="trash-can-outline" />
              {{ $t('globals.buttons.delete') }}
            </a>

            <b-table :data="bounces" hoverable default-sort="createdAt" class="bounces">
              <b-table-column field="campaign" :label="$tc('globals.terms.campaign', 1)" v-slot="props">
                <div v-if="props.row.campaign">
                  <router-link :to="{ name: 'bounces', query: { campaign_id: props.row.campaign.id } }">
                    {{ props.row.campaign.name }}
                  </router-link>
                </div>
              </b-table-column>

              <b-table-column field="createdAt" :label="$t('globals.fields.createdAt')" v-slot="props">
                {{ $utils.niceDate(props.row.createdAt, true) }}
              </b-table-column>

              <b-table-column field="action" :label="$t('globals.fields.type')" v-slot="props">
                <span class="is-pulled-right">
                  <a href="#" @click.prevent="toggleMeta(props.row.id)">
                    {{ props.row.source }}
                    <b-icon :icon="visibleMeta[props.row.id] ? 'arrow-up' : 'arrow-down'" />
                  </a>
                </span>
                <span class="is-clearfix" />
                <pre v-if="visibleMeta[props.row.id]">{{ props.row.meta }}</pre>
              </b-table-column>
            </b-table>
          </b-tab-item><!-- bounces -->

          <b-tab-item :label="$t('customers.activity')" class="activity" :disabled="!isEditing">
            <customer-activity v-if="isEditing && data.id" :customer-id="data.id" />
          </b-tab-item><!-- activity -->
        </b-tabs>

        <b-field :message="$t('customers.attribsHelp') + ' ' + egAttribs" class="mt-6">
          <div>
            <h5>{{ $t('globals.terms.attribs') }}</h5>
            <b-input v-model="form.strAttribs" name="attribs" type="textarea" :disabled="!canEdit" />
          </div>
        </b-field>
      </section>
      <footer class="modal-card-foot has-text-right">
        <b-button @click="$parent.close()">
          {{ $t('globals.buttons.close') }}
        </b-button>
        <b-button v-if="canEdit" native-type="submit" type="is-primary"
          :loading="loading.customers">
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
import CopyText from '../components/CopyText.vue';
import CustomerActivity from '../components/CustomerActivity.vue';

export default Vue.extend({
  components: {
    CustomerListSelector,
    CopyText,
    CustomerActivity,
  },

  props: {
    data: {
      type: Object,
      default: () => ({ customerLists: [] }),
    },
    isEditing: Boolean,
  },

  data() {
    return {
      // Binds form input values. This is populated by customer props passed
      // from the parent component in mounted().
      form: {
        customer_lists: [],
        strAttribs: '{}',
        status: 'enabled',
        preconfirm: false,
      },
      isBounceVisible: false,
      bounces: [],
      visibleMeta: {},

      egAttribs: '{"job": "developer", "location": "Mars", "has_rocket": true}',
    };
  },

  methods: {
    toggleBounces() {
      this.isBounceVisible = !this.isBounceVisible;
    },

    toggleMeta(id) {
      let v = false;
      if (!this.visibleMeta[id]) {
        v = true;
      }
      Vue.set(this.visibleMeta, id, v);
    },

    deleteBounces(sub) {
      this.$utils.confirm(
        null,
        () => {
          this.$api.deleteCustomerBounces(this.form.id).then(() => {
            this.getBounces();
            this.$utils.toast(this.$t('globals.messages.deleted', { name: sub.name }));
          });
        },
      );
    },

    getBounces() {
      this.$api.getCustomerBounces(this.form.id).then((data) => {
        this.bounces = data;
      });
    },

    onSubmit() {
      if (this.isEditing) {
        this.updateCustomer();
        return;
      }

      this.createCustomer();
    },

    createCustomer() {
      let attribs = {};
      if (this.form.strAttribs) {
        attribs = this.validateAttribs(this.form.strAttribs);
        if (!attribs) {
          return;
        }
      }
      const data = {
        email: this.form.email,
        name: this.form.name,
        status: this.form.status,
        customer_code: this.form.customerCode,
        attribs,
        preconfirm_subscriptions: this.form.preconfirm,

        // CustomerList IDs.
        customer_list_ids: this.form.customer_lists.map((l) => l.id),
      };

      this.$api.createCustomer(data).then((d) => {
        this.$emit('finished');
        this.$parent.close();
        this.$utils.toast(this.$t('globals.messages.created', { name: d.name }));
      });
    },

    updateCustomer() {
      let attribs = {};
      if (this.form.strAttribs) {
        attribs = this.validateAttribs(this.form.strAttribs);
        if (!attribs) {
          return;
        }
      }
      const data = {
        id: this.form.id,
        email: this.form.email,
        name: this.form.name,
        status: this.form.status,
        customer_code: this.form.customerCode,
        preconfirm_subscriptions: this.form.preconfirm,
        attribs,

        // CustomerList IDs.
        customer_list_ids: this.form.customer_lists.map((l) => l.id),
      };

      this.$api.updateCustomer(data).then((d) => {
        this.$emit('finished');
        this.$parent.close();
        this.$utils.toast(this.$t('globals.messages.updated', { name: d.name }));
      });
    },

    sendOptinConfirmation() {
      this.$api.sendCustomerOptin(this.form.id).then(() => {
        this.$utils.toast(this.$t('customers.sentOptinConfirm'));
      });
    },

    validateAttribs(str) {
      // Parse and validate attributes JSON.
      let attribs = {};
      try {
        attribs = JSON.parse(str);
      } catch (e) {
        this.$utils.toast(
          `${this.$t('customers.invalidJSON')}: ${e.toString()}`,
          'is-danger',

          3000,
        );
        return null;
      }
      if (attribs instanceof Array) {
        this.$utils.toast('Attributes should be a map {} and not an array []', 'is-danger', 3000);
        return null;
      }

      return attribs;
    },
  },

  computed: {
    ...mapState(['customer_lists', 'loading']),

    canEdit() {
      return !this.isEditing
        ? this.$canCreateWorkspaceResource('customers:manage')
        : this.$canManageResource(this.data, 'customers:manage');
    },

    hasOptinList() {
      return this.form.customer_lists.some((l) => l.optin === 'double');
    },
  },

  mounted() {
    if (this.$props.isEditing) {
      this.form = {
        ...this.$props.data,

        // Keep form state separate from the camel-cased API response.
        customer_lists: this.$props.data.customerLists || [],

        // Deep-copy the customer_lists array on to the form.
        strAttribs: JSON.stringify(this.$props.data.attribs, null, 4),
      };
    }

    if (this.form.id) {
      this.getBounces();
    }

    this.$nextTick(() => {
      this.$refs.focus.focus();
    });
  },
});
</script>
