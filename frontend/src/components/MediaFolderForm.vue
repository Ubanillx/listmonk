<template>
  <form class="modal-card" style="width: 420px; max-width: calc(100vw - 40px)" @submit.prevent="onSubmit">
    <header class="modal-card-head">
      <p class="modal-card-title">{{ folder ? $t('media.editFolder') : $t('media.createFolder') }}</p>
    </header>
    <section class="modal-card-body">
      <b-field :label="$t('media.createFolderPrompt')">
        <b-input v-model="name" :placeholder="$t('media.folderNamePlaceholder')" required maxlength="150"
          autofocus data-cy="folder-name" />
      </b-field>
      <b-field :label="$t('media.folderPermission')" :message="$t('media.folderPermissionHelp')">
        <b-select v-model="visibility" expanded :disabled="!$can('assets:share')" data-cy="folder-permission">
          <option value="private">{{ $t('media.folderPrivate') }}</option>
          <option value="organization" :disabled="!isOrganization">{{ $t('media.folderOrganization') }}</option>
          <option value="global">{{ $t('media.folderGlobal') }}</option>
        </b-select>
      </b-field>
    </section>
    <footer class="modal-card-foot is-justify-content-flex-end">
      <b-button @click="$emit('close')">{{ $t('globals.buttons.cancel') }}</b-button>
      <b-button native-type="submit" type="is-primary" :loading="saving" :disabled="!name.trim() || saving">
        {{ $t('globals.buttons.ok') }}
      </b-button>
    </footer>
  </form>
</template>

<script>
export default {
  props: {
    folder: { type: Object, default: null },
    parentId: { type: Number, default: 0 },
    isOrganization: Boolean,
    onSaved: { type: Function, required: true },
  },

  data() {
    const defaultVisibility = this.isOrganization && this.$can('assets:share') ? 'organization' : 'private';
    return {
      name: this.folder ? this.folder.name : '',
      visibility: this.folder ? this.folder.visibility : defaultVisibility,
      saving: false,
    };
  },

  methods: {
    onSubmit() {
      if (this.saving || !this.name.trim()) return;
      this.saving = true;
      const data = { name: this.name.trim(), visibility: this.visibility };
      const request = this.folder
        ? this.$api.renameMediaFolder(this.folder.id, data)
        : this.$api.createMediaFolder({ ...data, parent_id: this.parentId });
      request.then(() => {
        this.onSaved();
        this.$emit('close');
      }).finally(() => { this.saving = false; });
    },
  },
};
</script>
