<template>
  <form @submit.prevent="onSubmit">
    <div class="modal-card content" style="width: auto">
      <header class="modal-card-head">
        <p v-if="isEditing" class="has-text-grey-light is-size-7">
          {{ $t('globals.fields.id') }}: <copy-text :text="`${data.id}`" />
        </p>
        <h4 v-if="isEditing">
          {{ data.name }}
        </h4>
        <h4 v-else>
          {{ type === 'user' ? $t('users.newUserRole') : $t('users.newListRole') }}
        </h4>
      </header>

      <section expanded class="modal-card-body">
        <b-field :label="$t('globals.fields.name')" label-position="on-border">
          <b-input autofocus :disabled="disabled" :maxlength="200" v-model="form.name" name="name" ref="focus"
            required />
        </b-field>

        <div v-if="type === 'customer_list'" class="box">
          <h5>{{ $t('users.customerListPerms') }}</h5>
          <div class="mb-5">
            <div class="columns">
              <div class="column is-9">
                <b-select :placeholder="$tc('globals.terms.customer_list')" v-model="form.curList" name="customer_list"
                  :disabled="disabled || filteredLists.length < 1" expanded class="mb-3">
                  <template v-for="l in filteredLists">
                    <option :value="l.id" :key="l.id">
                      {{ l.name }}
                    </option>
                  </template>
                </b-select>
              </div>
              <div class="column">
                <b-button @click="onAddListPerm" :disabled="!form.curList" class="is-primary" expanded>
                  {{ $t('globals.buttons.add') }}
                </b-button>
              </div>
            </div>
            <span
              v-if="form.customer_lists.length > 0 && Array.isArray(form.permissions)
                && (form.permissions.includes('customer_lists:get_all') || form.permissions.includes('customer_lists:manage_all'))"
              class="is-size-6 has-text-danger">
              <b-icon icon="warning-empty" />
              {{ $t('users.customerListPermsWarning') }}
            </span>
          </div>

          <b-table :data="form.customer_lists">
            <b-table-column v-slot="props" field="name" :label="$tc('globals.terms.customer_list')">
              <router-link :to="`/customer-lists/${props.row.id}`" target="_blank">
                {{ props.row.name }}
              </router-link>
            </b-table-column>

            <b-table-column v-slot="props" field="permissions" :label="$t('users.perms')" width="40%">
              <b-checkbox v-model="props.row.permissions" native-value="customer_list:get">
                {{ $t('globals.buttons.view') }}
              </b-checkbox>
              <b-checkbox v-model="props.row.permissions" native-value="customer_list:manage">
                {{ $t('globals.buttons.manage') }}
              </b-checkbox>
            </b-table-column>

            <b-table-column v-slot="props" width="10%">
              <a href="#" @click.prevent="onDeleteListPerm(props.row.id)" data-cy="btn-delete"
                :aria-label="$t('globals.buttons.delete')">
                <b-tooltip :label="$t('globals.buttons.delete')" type="is-dark">
                  <b-icon icon="trash-can-outline" size="is-small" />
                </b-tooltip>
              </a>
            </b-table-column>
          </b-table>
        </div>

        <template v-if="type === 'user'">
          <div class="columns">
            <div class="column is-7">
              <h5 class="mb-0">
                {{ $t('users.perms') }}
              </h5>
            </div>
            <div class="column has-text-right" v-if="!disabled">
              <a href="#" @click.prevent="onToggleSelect">{{ $t('globals.buttons.toggleSelect') }}</a>
            </div>
          </div>

          <b-table :data="serverConfig.permissions">
            <b-table-column v-slot="props" field="group" :label="$t('users.roleGroup')">
              {{ $tc(`globals.terms.${props.row.group}`) }}
            </b-table-column>

            <b-table-column v-slot="props" field="permissions" label="Permissions">
              <div v-for="p in props.row.permissions" :key="p" class="permission-row">
                <b-checkbox v-model="form.permissions" :native-value="p" :disabled="disabled">
                  {{ permissionLabel(p) }}
                  <span v-if="isHighRiskPermission(p)"
                    :title="$t('users.highRiskPermission')"
                    :aria-label="$t('users.highRiskPermission')">
                    <b-icon icon="warning-empty" type="is-danger" size="is-small" />
                  </span>
                </b-checkbox>
                <b-tooltip :label="permissionDescription(p)" type="is-dark" position="is-right" multilined>
                  <span class="permission-help" tabindex="0" role="button"
                    :aria-label="$t('users.permissionHelp.label')"
                    :title="$t('users.permissionHelp.label')">
                    <b-icon icon="help-circle-outline" size="is-small" />
                  </span>
                </b-tooltip>
              </div>
            </b-table-column>
          </b-table>
        </template>
      </section>

      <footer class="modal-card-foot has-text-right">
        <b-button @click="$parent.close()">
          {{ $t('globals.buttons.close') }}
        </b-button>
        <b-button v-if="!disabled" native-type="submit" type="is-primary" :loading="loading.roles" data-cy="btn-save">
          {{ $t('globals.buttons.save') }}
        </b-button>
      </footer>
    </div>
  </form>
</template>

<script>
import Vue from 'vue';
import { mapState } from 'vuex';
import CopyText from '../components/CopyText.vue';

export default Vue.extend({
  name: 'RoleForm',

  components: {
    CopyText,
  },

  props: {
    data: { type: Object, default: () => ({}) },
    isEditing: { type: Boolean, default: false },
    type: { type: String, default: 'user' },
  },

  data() {
    return {
      // Binds form input values.
      form: {
        curList: null,
        customer_lists: [],
        name: null,
        permissions: {},
      },
      hasToggle: false,
      disabled: false,
    };
  },

  methods: {
    permissionLabel(permission) {
      const key = `users.permission.${permission}`;
      return this.$te(key) ? this.$t(key) : permission;
    },

    permissionDescription(permission) {
      const key = `users.permissionHelp.${permission}`;
      return this.$te(key)
        ? this.$t(key)
        : this.$t('users.permissionHelp.default', { permission: this.permissionLabel(permission) });
    },

    isHighRiskPermission(permission) {
      return [
        'customers:sql_query', 'customers:delete', 'customers:blocklist', 'customers:membership_manage',
        'customers:export', 'customers:sensitive_read', 'campaigns:send', 'campaigns:test',
        'campaigns:schedule', 'campaigns:control', 'campaigns:recipients', 'bounces:delete',
        'bounces:blocklist', 'users:tokens', 'pools:manage', 'pools:export',
        'organizations:platform_manage',
      ].includes(permission);
    },

    onAddListPerm() {
      const customerList = this.customer_lists.results.find((l) => l.id === this.form.curList);
      this.form.customer_lists.push({ id: customerList.id, name: customerList.name, permissions: ['customer_list:get', 'customer_list:manage'] });

      this.form.curList = (this.filteredLists.length > 0) ? this.filteredLists[0].id : null;
    },

    onDeleteListPerm(id) {
      this.form.customer_lists = this.form.customer_lists.filter((p) => p.id !== id);
      this.form.curList = (this.filteredLists.length > 0) ? this.filteredLists[0].id : null;
    },

    onSubmit() {
      if (this.isEditing) {
        this.updateRole();
        return;
      }

      this.createRole();
    },

    onToggleSelect() {
      if (this.hasToggle) {
        this.form.permissions = [];
      } else {
        this.form.permissions = this.serverConfig.permissions.reduce((acc, item) => {
          item.permissions.forEach((p) => {
            acc.push(p);
          });
          return acc;
        }, []);
      }

      this.hasToggle = !this.hasToggle;
    },

    createRole() {
      let fn;
      const form = { name: this.form.name };

      if (this.$props.type === 'user') {
        fn = this.$api.createUserRole;
        form.permissions = this.form.permissions;
      } else {
        fn = this.$api.createListRole;
        form.customer_lists = this.form.customer_lists.reduce((acc, item) => {
          acc.push({ id: item.id, permissions: item.permissions });
          return acc;
        }, []);
      }

      fn(form).then((data) => {
        this.$emit('finished');
        this.$utils.toast(this.$t('globals.messages.created', { name: data.name }));
        this.$parent.close();
      });
    },

    updateRole() {
      let fn;
      const form = { id: this.$props.data.id, name: this.form.name };

      if (this.$props.type === 'user') {
        fn = this.$api.updateUserRole;
        form.permissions = this.form.permissions;
      } else {
        fn = this.$api.updateListRole;
        form.customer_lists = this.form.customer_lists.reduce((acc, item) => {
          acc.push({ id: item.id, permissions: item.permissions });
          return acc;
        }, []);
      }

      fn(form).then((data) => {
        this.$emit('finished');
        this.$utils.toast(this.$t('globals.messages.updated', { name: data.name }));
        this.$parent.close();
      });
    },
  },

  computed: {
    ...mapState(['loading', 'serverConfig', 'customer_lists']),

    // Return the customerList of unselected customer_lists.
    filteredLists() {
      if (!this.customer_lists.results || this.type !== 'customer_list') {
        return [];
      }

      const subIDs = this.form.customer_lists.reduce((obj, item) => ({ ...obj, [item.id]: true }), {});
      return this.customer_lists.results.filter((l) => (!(l.id in subIDs)));
    },

  },

  mounted() {
    if (this.isEditing) {
      this.form = { ...this.form, ...this.$props.data };

      // It's the superadmin role. Disable the form.
      if (this.$props.data.id === 1 || !this.$can('roles:manage')) {
        this.disabled = true;
      }
    } else {
      const skip = ['admin', 'users'];
      const defaultDisabled = [
        'customers:sql_query',
        'customers:delete',
        'customers:blocklist',
        'customers:membership_manage',
        'customers:export',
        'customers:sensitive_read',
        'campaigns:send',
        'campaigns:test',
        'campaigns:schedule',
        'campaigns:control',
        'campaigns:recipients',
        'bounces:delete',
        'bounces:blocklist',
        'users:tokens',
        'pools:manage',
        'pools:export',
        'organizations:platform_manage',
      ];
      this.form.permissions = this.serverConfig.permissions.reduce((acc, item) => {
        if (skip.includes(item.group)) {
          return acc;
        }
        item.permissions.forEach((p) => {
          if (!defaultDisabled.includes(p) && !p.startsWith('customer_lists:') && !p.startsWith('settings:')) {
            acc.push(p);
          }
        });
        return acc;
      }, []);
    }

    this.$nextTick(() => {
      if (this.filteredLists.length > 0) {
        this.form.curList = this.filteredLists[0].id;
      }
      this.$refs.focus.focus();
    });
  },
});
</script>

<style scoped>
.permission-row {
  display: flex;
  align-items: center;
  min-height: 2rem;
}

.permission-help {
  display: inline-flex;
  align-items: center;
  margin-left: 0.25rem;
  color: #7a7a7a;
  cursor: help;
}

.permission-help:focus {
  color: #3273dc;
  outline: 1px dotted currentColor;
  outline-offset: 2px;
}
</style>
