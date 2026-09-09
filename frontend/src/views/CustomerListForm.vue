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
          {{ $t('customer_lists.newList') }}
        </h4>
      </header>
      <section expanded class="modal-card-body">
        <b-field :label="$t('globals.fields.name')" label-position="on-border">
          <b-input :maxlength="200" :ref="'focus'" v-model="form.name" name="name" :disabled="!canSave"
            :placeholder="$t('globals.fields.name')" required />
        </b-field>

        <b-field :label="$t('customer_lists.type')" label-position="on-border" :message="$t('customer_lists.typeHelp')">
          <b-select v-model="form.type" name="type" :placeholder="$t('customer_lists.typeHelp')" :disabled="!canSave" required expanded>
            <option value="private">
              {{ $t('customer_lists.types.private') }}
            </option>
            <option value="public">
              {{ $t('customer_lists.types.public') }}
            </option>
            <option v-if="isPlatformAdmin" value="pool">
              {{ $t('customer_lists.types.pool') || '公海' }}
            </option>
            <!-- Secondary public-pool lists are created only from a first-level
              pool's split workflow, never as standalone lists. -->
            <option v-if="isEditing && data.type === 'pool_segment'" value="pool_segment">
              {{ $t('customer_lists.types.pool_segment') || '二级公海列表' }}
            </option>
          </b-select>
        </b-field>

        <b-field :label="$t('customer_lists.optin')" label-position="on-border" :message="$t('customer_lists.optinHelp')">
          <b-select v-model="form.optin" name="optin" placeholder="Opt-in type" :disabled="!canSave" required expanded>
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

        <b-field :message="$t('customer_lists.archivedHelp')" :label="$t('customer_lists.archived')">
          <b-switch v-model="isArchived" name="status" :disabled="!canSave" />
        </b-field>
      </section>
      <footer class="modal-card-foot has-text-right">
        <b-button @click="$parent.close()">
          {{ $t('globals.buttons.close') }}
        </b-button>
        <b-button v-if="canSave" native-type="submit"
          type="is-primary" :loading="loading.customer_lists" data-cy="btn-save">
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
  },

  data() {
    return {
      // Binds form input values.
      form: {
        name: dayjs().format('YYYY-MM-DD'),
        type: 'private',
        optin: 'single',
        status: 'active',
        tags: [],
        visibility: 'private',
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
        this.$emit('finished');
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
    ...mapState(['loading', 'profile']),

    isPlatformAdmin() {
      return Number(this.profile && this.profile.userRole && this.profile.userRole.id) === 1;
    },

    canSave() {
      if (!this.isEditing) {
        return this.$canCreateWorkspaceResource('customer_lists:manage_all');
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
    this.form = { ...this.form, ...this.$props.data, visibility: 'private' };

    this.$nextTick(() => {
      this.$refs.focus.focus();
    });
  },
});
</script>
