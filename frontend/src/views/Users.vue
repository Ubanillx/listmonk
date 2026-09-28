<template>
  <section class="users">
    <header class="columns page-header">
      <div class="column is-10">
        <h1 class="title is-4">
          {{ $t('globals.terms.users') }}
          <span v-if="!isNaN(users.length)">({{ users.length }})</span>
        </h1>
      </div>
      <div class="column has-text-right">
        <div v-if="$can('users:manage')" class="buttons is-justify-content-flex-end">
          <b-button icon-left="file-upload-outline" type="is-light" @click="showBulkImport" data-cy="btn-user-bulk-import">
            {{ $t('users.bulkImport') }}
          </b-button>
          <b-button type="is-primary" icon-left="plus" class="btn-new" @click="showNewForm" data-cy="btn-new">
            {{ $t('globals.buttons.new') }}
          </b-button>
        </div>
      </div>
    </header>

    <b-table :data="filteredUsers" :loading="loading.users" hoverable checkable :checked-rows.sync="checked"
      default-sort="createdAt" @check-all="onTableCheck" @check="onTableCheck">
      <template #top-left>
        <div class="columns">
          <div class="column is-6">
            <form @submit.prevent="getUsers">
              <div>
                <b-field>
                  <b-input v-model="queryParams.query" name="query" expanded icon="magnify" ref="query"
                    data-cy="query" />
                  <p class="controls">
                    <b-button native-type="submit" type="is-primary" icon-left="magnify" data-cy="btn-query"
                      :aria-label="$t('globals.buttons.search')" />
                  </p>
                </b-field>
              </div>
            </form>
          </div>
        </div>
      </template>

      <b-table-column v-slot="props" field="username" :label="$t('users.username')" header-class="cy-username" sortable
        :td-attrs="$utils.tdID">
        <a :href="`/users/${props.row.id}`" @click.prevent="showEditForm(props.row)"
          :class="{ 'has-text-grey': props.row.status === 'disabled' }">
          {{ props.row.username }}
        </a>
        <b-tag v-if="props.row.status === 'disabled'">
          {{ $t(`users.status.${props.row.status}`) }}
        </b-tag>
        <b-tag v-if="props.row.type === 'api'" class="api">
          <b-icon icon="code-tags" />
          {{ $t(`users.type.${props.row.type}`) }}
        </b-tag>
        <div class="has-text-grey is-size-7 mt-2">
          {{ props.row.name }}
        </div>
      </b-table-column>

      <b-table-column v-slot="props" field="userRole.name" :label="$tc('users.role')" header-class="cy-status" sortable
        :td-attrs="$utils.tdID">
        <router-link v-if="$can('roles:get')" :to="{ name: 'userRoles' }">
          <b-tag :class="Number(props.row.userRole.id) === 1 ? 'enabled' : 'primary'">
            <b-icon icon="account-outline" />
            {{ props.row.userRole.name }}
          </b-tag>
        </router-link>
        <b-tag v-else :class="Number(props.row.userRole.id) === 1 ? 'enabled' : 'primary'">
          <b-icon icon="account-outline" />
          {{ props.row.userRole.name }}
        </b-tag>
        <router-link v-if="$can('roles:get')" :to="{ name: 'customerListRoles' }">
          <b-tag v-if="props.row.customerListRole">
            <b-icon icon="newspaper-variant-outline" />
            {{ props.row.customerListRole.name }}
          </b-tag>
        </router-link>
        <b-tag v-else-if="props.row.customerListRole">
          <b-icon icon="newspaper-variant-outline" />
          {{ props.row.customerListRole.name }}
        </b-tag>
      </b-table-column>

      <b-table-column v-slot="props" field="email" :label="$t('customers.email')" header-class="cy-name" sortable
        :td-attrs="$utils.tdID">
        <div>
          <a v-if="props.row.email" :href="`/users/${props.row.id}`" @click.prevent="showEditForm(props.row)"
            :class="{ 'has-text-grey': props.row.status === 'disabled' }">
            {{ props.row.email }}
          </a>
          <template v-else>
            —
          </template>
        </div>
      </b-table-column>

      <b-table-column v-slot="props" field="createdAt" :label="$t('globals.fields.createdAt')"
        header-class="cy-created_at" sortable>
        {{ $utils.niceDate(props.row.createdAt) }}
      </b-table-column>

      <b-table-column v-slot="props" field="updatedAt" :label="$t('globals.fields.updatedAt')"
        header-class="cy-updated_at" sortable>
        {{ $utils.niceDate(props.row.updatedAt) }}
      </b-table-column>

      <b-table-column v-slot="props" field="loggedinAt" :label="$t('users.lastLogin')" header-class="cy-updated_at"
        sortable>
        {{ props.row.loggedinAt ? $utils.niceDate(props.row.loggedinAt, true) : '—' }}
      </b-table-column>

      <b-table-column v-slot="props" cell-class="actions" align="right">
        <div>
          <a v-if="$can('users:manage')" href="#" @click.prevent="showEditForm(props.row)" data-cy="btn-edit"
            :aria-label="$t('globals.buttons.edit')">
            <b-tooltip :label="$t('globals.buttons.edit')" type="is-dark">
              <b-icon icon="pencil-outline" size="is-small" />
            </b-tooltip>
          </a>

          <a v-if="$can('users:manage')" href="#" @click.prevent="deleteUser(props.row)" data-cy="btn-delete"
            :aria-label="$t('globals.buttons.delete')">
            <b-tooltip :label="$t('globals.buttons.delete')" type="is-dark">
              <b-icon icon="trash-can-outline" size="is-small" />
            </b-tooltip>
          </a>
          <a v-if="isPlatformAdmin" href="#" @click.prevent="showSMTPStatus(props.row)" data-cy="btn-smtp-status"
            :aria-label="$t('settings.personalSMTP.viewStatus')">
            <b-tooltip :label="$t('settings.personalSMTP.viewStatus')" type="is-dark">
              <b-icon icon="email-outline" size="is-small" />
            </b-tooltip>
          </a>
        </div>
      </b-table-column>

      <template #empty v-if="!loading.users">
        <empty-placeholder />
      </template>
    </b-table>

    <!-- Add / edit form modal -->
    <b-modal scroll="keep" :aria-modal="true" :active.sync="isFormVisible" :width="600" @close="onFormClose">
      <user-form :data="curItem" :is-editing="isEditing" @finished="formFinished" />
    </b-modal>

    <b-modal scroll="keep" :aria-modal="true" :active.sync="isBulkImportVisible" :width="1200">
      <user-bulk-import @finished="formFinished" />
    </b-modal>

    <b-modal scroll="keep" :aria-modal="true" :active.sync="isSMTPStatusVisible" :width="720">
      <div class="modal-card content" style="width: auto">
        <header class="modal-card-head">
          <h4><b-icon icon="email-outline" size="is-small" /> {{ $t('settings.personalSMTP.statusTitle') }}</h4>
        </header>
        <section class="modal-card-body">
          <p v-if="smtpStatusUser" class="mb-4">
            <strong>{{ smtpStatusUser.username }}</strong>
            <span v-if="smtpStatusUser.email" class="has-text-grey"> {{ smtpStatusUser.email }}</span>
          </p>
          <div class="table-scroll">
            <b-table :data="smtpStatus" :loading="smtpStatusLoading">
            <b-table-column v-slot="props" field="name" :label="$t('globals.fields.name')">
              {{ props.row.name || $t('settings.smtp.name') }}
            </b-table-column>
            <b-table-column v-slot="props" field="enabled" :label="$t('settings.smtp.enabled')">
              <b-tag :type="props.row.enabled ? 'is-success' : 'is-light'">
                {{ props.row.enabled ? $t('globals.states.on') : $t('globals.states.off') }}
              </b-tag>
            </b-table-column>
            <b-table-column v-slot="props" field="dailyLimit" :label="$t('settings.smtp.dailyLimit')" numeric>
              {{ props.row.dailyLimit || 0 }}
            </b-table-column>
            <b-table-column v-slot="props" field="sentToday" :label="$t('settings.personalSMTP.sentTodayShort')" numeric>
              {{ props.row.sentToday || 0 }}
            </b-table-column>
            <b-table-column v-slot="props" field="updatedAt" :label="$t('globals.fields.updatedAt')">
              {{ props.row.updatedAt ? $utils.niceDate(props.row.updatedAt, true) : '-' }}
            </b-table-column>
            <template #empty v-if="!smtpStatusLoading"><span class="has-text-grey">{{ $t('settings.personalSMTP.empty') }}</span></template>
            </b-table>
          </div>
          <p class="help mt-4">{{ $t('settings.personalSMTP.statusReadOnly') }}</p>
        </section>
        <footer class="modal-card-foot has-text-right">
          <b-button @click="isSMTPStatusVisible = false">{{ $t('globals.buttons.close') }}</b-button>
        </footer>
      </div>
    </b-modal>
  </section>
</template>

<script>
import Vue from 'vue';
import { mapState } from 'vuex';
import EmptyPlaceholder from '../components/EmptyPlaceholder.vue';

import UserForm from './UserForm.vue';
import UserBulkImport from './UserBulkImport.vue';

export default Vue.extend({
  components: {
    EmptyPlaceholder,
    UserForm,
    UserBulkImport,
  },

  data() {
    return {
      curItem: null,
      isEditing: false,
      isFormVisible: false,
      isBulkImportVisible: false,
      isSMTPStatusVisible: false,
      smtpStatusLoading: false,
      smtpStatus: [],
      smtpStatusUser: null,
      users: [],
      checked: [],
      queryParams: {
        page: 1,
        query: '',
      },
    };
  },

  methods: {
    showSMTPStatus(user) {
      this.smtpStatusUser = user;
      this.smtpStatus = [];
      this.isSMTPStatusVisible = true;
      this.smtpStatusLoading = true;
      this.$api.getUserPersonalSMTP(user.id).then((data) => {
        this.smtpStatus = (data && data.smtp) || [];
      }).finally(() => {
        this.smtpStatusLoading = false;
      });
    },

    onTableCheck(rows) {
      // Users currently have no bulk action toolbar. Keep the callback safe
      // for Buefy's check/check-all events while the selection is synced by
      // checked-rows.sync.
      if (Array.isArray(rows)) {
        this.checked = rows;
      }
    },

    // Show the edit form.
    showEditForm(item) {
      this.curItem = item;
      this.isFormVisible = true;
      this.isEditing = true;
    },

    // Show the new form.
    showNewForm() {
      this.curItem = {};
      this.isFormVisible = true;
      this.isEditing = false;
    },

    showBulkImport() {
      this.isBulkImportVisible = true;
    },

    formFinished() {
      this.getUsers();
    },

    onFormClose() {
      if (this.$route.params.id) {
        this.$router.push({ name: 'users' });
      }
    },

    getUsers() {
      this.$api.queryUsers().then((resp) => {
        this.users = resp;
      });
    },

    deleteUser(item) {
      this.$utils.confirm(
        this.$t('globals.messages.confirm'),
        () => {
          this.$api.deleteUser(item.id).then(() => {
            this.getUsers();

            this.$utils.toast(this.$t('globals.messages.deleted', { name: item.name }));
          });
        },
      );
    },
  },

  computed: {
    ...mapState(['loading', 'settings', 'profile']),

    filteredUsers() {
      const query = String(this.queryParams.query || '').trim().toLowerCase();
      if (!query) {
        return this.users;
      }
      return this.users.filter((user) => ['username', 'name', 'email']
        .some((key) => String(user[key] || '').toLowerCase().includes(query)));
    },

    isPlatformAdmin() {
      return this.profile && this.profile.userRole && Number(this.profile.userRole.id) === 1;
    },
  },

  created() {
    this.$root.$on('page.refresh', this.getUsers);
  },

  destroyed() {
    this.$root.$off('page.refresh', this.getUsers);
  },

  mounted() {
    if (this.$route.params.id) {
      this.$api.getUser(parseInt(this.$route.params.id, 10)).then((data) => {
        this.showEditForm(data);
      });
    } else {
      this.getUsers();
    }
  },
});
</script>

<style scoped>
.table-scroll {
  overflow-x: auto;
}
</style>
