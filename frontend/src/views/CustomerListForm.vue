<template>
  <form @submit.prevent="onSubmit">
    <div class="modal-card content" style="width: auto">
      <header class="modal-card-head">
        <p v-if="isEditing" class="has-text-grey-light is-size-7">
          {{ $t('globals.fields.id') }}: <copy-text :text="`${data.id}`" />
          {{ $t('globals.fields.uuid') }}: <copy-text :text="data.uuid" />
        </p>
        <b-tag v-if="isEditing" :class="[data.type, 'is-pulled-right']">
          {{ $t(`customer_lists.types.${data.type}`) }}
        </b-tag>
        <h4 v-if="isEditing">
          {{ data.name }}
        </h4>
        <h4 v-else>
          {{ $t(isPoolGroup ? 'customer_lists.newPoolList' : 'customer_lists.newList') }}
        </h4>
      </header>
      <section expanded class="modal-card-body">
        <b-field :label="$t('globals.fields.name')" label-position="on-border">
          <b-input :maxlength="200" :ref="'focus'" v-model="form.name" name="name" :disabled="!canSave"
            :placeholder="$t('globals.fields.name')" required />
        </b-field>

        <b-field :label="$t('customer_lists.type')" label-position="on-border" :message="typeHelp">
          <b-select v-model="form.type" name="type" :placeholder="typeHelp" :disabled="!canSave" required expanded>
            <option v-if="!isPoolGroup" value="private">
              {{ $t('customer_lists.types.private') }}
            </option>
            <option v-if="!isPoolGroup" value="public">
              {{ $t('customer_lists.types.public') }}
            </option>
            <option v-if="isPoolGroup && canMaintainPoolMaster" value="pool">
              {{ $t('customer_lists.types.pool') }}
            </option>
            <!-- Pool-allocation public-pool lists are created only from a first-level
              pool's split workflow, never as standalone lists. -->
            <option v-if="isEditing && data.type === 'org_pool_allocation'" value="org_pool_allocation">
              {{ $t('customer_lists.types.org_pool_allocation') }}
            </option>
          </b-select>
        </b-field>

        <b-field v-if="!isPoolGroup" :label="$t('visibility.label')" label-position="on-border">
          <b-select v-model="form.visibility" name="visibility" :disabled="!canSave" expanded data-cy="list-visibility">
            <option value="private">{{ $t('visibility.private') }}</option>
            <option v-if="workspace.organizationId" value="organization">{{ $t('visibility.organization') }}</option>
          </b-select>
        </b-field>

        <b-field :label="$t('customer_lists.optin')" label-position="on-border" :message="$t('customer_lists.optinHelp')">
          <b-select v-model="form.optin" name="optin" :placeholder="$t('customer_lists.optin')" :disabled="!canSave"
            required expanded>
            <option value="single">
              {{ $t('customer_lists.optins.single') }}
            </option>
            <option value="double">
              {{ $t('customer_lists.optins.double') }}
            </option>
          </b-select>
        </b-field>

        <b-field :label="$t('globals.terms.tags')" label-position="on-border">
          <b-taginput v-model="form.tags" name="tags" :disabled="!canSave" ellipsis icon="tag-outline"
            :placeholder="$t('globals.terms.tags')" />
        </b-field>

        <b-field :label="$t('globals.fields.description')" label-position="on-border">
          <b-input :maxlength="2000" v-model="form.description" name="description" type="textarea" :disabled="!canSave"
            :placeholder="$t('globals.fields.description')" />
        </b-field>

        <b-field :message="$t('customer_lists.maskEmailsHelp')" :label="$t('customer_lists.maskEmails')">
          <b-switch v-model="form.maskEmails" name="mask_emails" :disabled="!canSave" />
        </b-field>

        <b-field v-if="isEditing" :message="$t('customer_lists.archivedHelp')" :label="$t('customer_lists.archived')">
          <b-switch v-model="isArchived" name="status" :disabled="!canSave" />
        </b-field>
      </section>
      <footer class="modal-card-foot has-text-right">
        <b-button @click="$parent.close()">
          {{ $t('globals.buttons.cancel') }}
        </b-button>
        <b-button v-if="canSave" native-type="submit" type="is-primary" :loading="loading.customer_lists"
          :disabled="loading.customer_lists" data-cy="btn-save">
          {{ $t('globals.buttons.save') }}
        </b-button>
      </footer>
    </div>
  </form>
</template>

<script>
import Vue from 'vue';
import dayjs from 'dayjs';
import { mapState } from 'vuex';
import CopyText from '../components/CopyText.vue';

export default Vue.extend({
  name: 'CustomerListForm',

  components: {
    CopyText,
  },

  props: {
    data: { type: Object, default: () => ({}) },
    isEditing: { type: Boolean, default: false },
    listGroup: { type: String, default: 'private' },
  },

  data() {
    const workspaceVisibility = this.$store.state.workspace.organizationId > 0 ? 'organization' : 'private';
    return {
      // Binds form input values.
      form: {
        name: dayjs().format('YYYY-MM-DD'),
        type: this.listGroup === 'pool' ? 'pool' : 'private',
        optin: 'single',
        status: 'active',
        tags: [],
        visibility: this.listGroup === 'pool' ? 'global' : workspaceVisibility,
        maskEmails: false,
      },
    };
  },

  methods: {
    onSubmit() {
      if (this.isEditing) {
        this.updateList();
        return;
      }

      this.createList();
    },

    // API responses are camel-cased by the HTTP layer while the backend binds
    // snake_case fields, so rebuild the outgoing payload.
    toPayload() {
      const out = { ...this.form };
      out.mask_emails = out.maskEmails;
      delete out.maskEmails;
      return out;
    },

    createList() {
      this.$api.createList(this.toPayload()).then((data) => {
        this.$emit('finished', data);
        this.$parent.close();
        this.$utils.toast(this.$t('globals.messages.created', { name: data.name }));
      });
    },

    updateList() {
      this.$api.updateList({ id: this.data.id, ...this.toPayload() }).then((data) => {
        this.$emit('finished');
        this.$parent.close();
        this.$utils.toast(this.$t('globals.messages.updated', { name: data.name }));
      });
    },
  },

  computed: {
    typeHelp() {
      const helpKeys = {
        private: 'customer_lists.typeHelpPrivate',
        public: 'customer_lists.typeHelpPublic',
        pool: 'customer_lists.typeHelpPool',
        org_pool_allocation: 'customer_lists.typeHelpOrgPoolAllocation',
      };
      return this.$t(helpKeys[this.form.type] || 'customer_lists.typeHelpPrivate');
    },
    ...mapState(['loading', 'profile', 'workspace']),

    isPoolGroup() {
      return this.listGroup === 'pool';
    },

    canMaintainPoolMaster() {
      return this.$canCreateWorkspaceResource('pools:master_manage');
    },

    canSave() {
      if (!this.isEditing) {
        return this.isPoolGroup
          ? this.canMaintainPoolMaster
          : this.$canCreateWorkspaceResource('customer_lists:manage_all');
      }
      if (this.data.type === 'pool') {
        return this.canMaintainPoolMaster && !this.data.organizationId && !this.data.organization_id
          && !this.data.transferPendingAt && !this.data.transfer_pending_at;
      }
      return this.$canManageResource(this.data) && this.$canList(this.data.id, 'customer_list:manage');
    },

    isArchived: {
      get() {
        return this.form.status === 'archived';
      },
      set(v) {
        this.form.status = v ? 'archived' : 'active';
      },
    },
  },

  mounted() {
    this.form = {
      ...this.form,
      ...this.$props.data,
      visibility: this.isPoolGroup ? 'global' : (this.$props.data.visibility || this.form.visibility),
    };

    this.$nextTick(() => {
      this.$refs.focus.focus();
    });
  },
});
</script>
