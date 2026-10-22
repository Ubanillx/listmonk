<template>
  <section class="media-files" :class="{ 'is-platform-catalog': adminView }">
    <header class="media-page-header">
      <h1 class="title is-4 mb-2">{{ $t('media.title') }}</h1>
      <p v-if="adminView" class="has-text-grey mb-0">{{ $t('media.catalogHelp') }}</p>
    </header>

    <b-loading :active="isProcessing || loading.media" :is-full-page="false" />

    <section class="media-surface gallery mt-5">
      <div class="media-layout">
        <aside v-if="adminView" class="media-category-column" :aria-label="$t('media.organizations')" data-cy="media-organizations">
          <h2>{{ $t('media.organizations') }}</h2>
          <button type="button" class="media-category-option" :class="{ 'is-active': categoryOrganizationID === null }"
            @click="selectOrganization(null)">
            <b-icon icon="earth" size="is-small" />
            <span>{{ $t('media.allOrganizations') }}</span><span class="media-category-count">{{ platformMediaCount }}</span>
          </button>
          <button v-for="group in categoryGroups" :key="group.id" type="button" class="media-category-option"
            :class="{ 'is-active': categoryOrganizationID === group.id }" @click="selectOrganization(group.id)">
            <b-icon :icon="group.id ? 'office-building-outline' : 'account-outline'" size="is-small" />
            <span>{{ group.name }}</span><span class="media-category-count">{{ group.count }}</span>
          </button>
        </aside>
        <aside v-if="adminView" class="media-category-column" :aria-label="$t('media.owners')" data-cy="media-owners">
          <h2>{{ $t('media.owners') }}</h2>
          <button type="button" class="media-category-option" :class="{ 'is-active': categoryOwnerID === null }"
            @click="selectOwner(null)">
            <b-icon icon="account-group-outline" size="is-small" />
            <span>{{ $t('media.allOwners') }}</span><span class="media-category-count">{{ visibleOwnerCount }}</span>
          </button>
          <button v-for="owner in visibleOwners" :key="owner.id" type="button" class="media-category-option"
            :class="{ 'is-active': categoryOwnerID === owner.id }" @click="selectOwner(owner.id)">
            <b-icon icon="account-outline" size="is-small" />
            <span>{{ owner.name }}</span><span class="media-category-count">{{ owner.count }}</span>
          </button>
        </aside>
        <div class="media-main">
      <div class="media-toolbar mb-4">
        <div class="media-search">
          <form @submit.prevent="onQueryMedia" class="search">
            <div>
              <b-field addons class="lm-search-field">
                <b-input v-model="queryParams.query" name="query" expanded icon="magnify" ref="query" data-cy="query"
                  :aria-label="$t('globals.buttons.search')" />
                <p class="controls">
                    <b-button native-type="submit" type="is-primary" icon-left="magnify" data-cy="btn-query"
                      :aria-label="$t('globals.buttons.search')" />
                </p>
              </b-field>
            </div>
          </form>
        </div>
        <div v-if="canWriteCurrentFolder" class="media-toolbar-actions">
          <div class="buttons">
            <b-button @click="onCreateFolder" icon-left="folder-plus-outline" data-cy="btn-create-folder">
              {{ $t('media.createFolder') }}
            </b-button>
            <b-button @click="onToggleForm" icon-left="file-upload-outline" data-cy="btn-toggle-upload">
              {{ $t('media.upload') }}
            </b-button>
          </div>
        </div>
      </div>

      <div v-if="adminView" class="media-catalog-context" data-cy="media-catalog-context">
        <nav class="breadcrumb mb-0" :aria-label="$t('media.title')">
          <ul>
            <li><span>{{ $t('media.platform') }}</span></li>
            <li v-if="categoryOrganizationID !== null"><span>{{ selectedOrganizationName }}</span></li>
            <li v-if="categoryOwnerID !== null" class="is-active"><span>{{ selectedOwnerName }}</span></li>
          </ul>
        </nav>
        <span>{{ $t('media.fileCount', { count: shownMedia.total }) }} <b-tag size="is-small" type="is-light">{{ $t('media.readOnly') }}</b-tag></span>
      </div>
      <nav v-else class="breadcrumb media-breadcrumb" :aria-label="$t('media.title')">
        <ul>
          <li :class="{ 'is-active': currentFolderId === 0, 'drop-target': dragOverFolderId === 0 }"
            @dragover.prevent="onDragOverFolder(0)" @dragleave="onDragLeaveFolder(0)"
            @drop.prevent="onDropOnFolder(0, $event)">
            <a href="#" @click.prevent="openFolder(0)">
              <b-icon icon="folder-home-outline" size="is-small" />
              <span>{{ $t('media.root') }}</span>
            </a>
          </li>
          <li v-for="crumb in breadcrumbs" :key="crumb.id"
            :class="{ 'is-active': crumb.id === currentFolderId, 'drop-target': dragOverFolderId === crumb.id }"
            @dragover.prevent="onDragOverFolder(crumb.id)" @dragleave="onDragLeaveFolder(crumb.id)"
            @drop.prevent="onDropOnFolder(crumb.id, $event)">
            <a href="#" @click.prevent="openFolder(crumb.id)">{{ crumb.name }}</a>
          </li>
        </ul>
      </nav>

      <div v-if="loading.media" class="has-text-centered py-6">
        <b-loading :active="loading.media" />
      </div>
      <div v-else-if="(visibleFolders.length > 0) || (shownMedia.results && shownMedia.results.length > 0)" class="media-browser-content">
        <div v-if="visibleFolders.length > 0" class="folder-grid">
          <div v-for="folder in visibleFolders" :key="`folder-${folder.id}`" class="folder-item"
            :class="{ 'is-drop-target': dragOverFolderId === folder.id }"
            :draggable="canManageMediaLibrary && folder.manageable" role="button" tabindex="0"
            @click="openFolder(folder.id)" @keydown.enter.prevent="openFolder(folder.id)"
            @keydown.space.prevent="openFolder(folder.id)"
            @dragstart="onDragStart('folder', folder.id, $event)" @dragend="onDragEnd"
            @dragover.prevent.stop="onDragOverFolder(folder.id)" @dragleave="onDragLeaveFolder(folder.id)"
            @drop.prevent.stop="onDropOnFolder(folder.id, $event)" data-cy="media-folder">
            <div class="folder-icon"><b-icon icon="folder-outline" size="is-large" /></div>
            <div class="folder-info">
              <p class="folder-name" :title="folder.name">{{ folder.name }}</p>
              <b-tag size="is-small">{{ folderPermissionLabel(folder.visibility) }}</b-tag>
              <p class="folder-meta">
                {{ $tc('media.folderItems', folder.mediaCount, { count: folder.mediaCount }) }}
                <span v-if="folder.childCount > 0"> · {{ $tc('media.folderFolders', folder.childCount, { count: folder.childCount }) }}</span>
              </p>
            </div>
            <div v-if="canManageMediaLibrary && folder.manageable" class="folder-actions">
              <a href="#" @click.prevent.stop="onRenameFolder(folder)" data-cy="btn-rename-folder"
                :aria-label="$t('media.editFolder')">
                <b-icon icon="pencil-outline" size="is-small" />
              </a>
              <a href="#" :class="{ disabled: folder.mediaCount > 0 || folder.childCount > 0 }"
                @click.prevent.stop="onDeleteFolder(folder)" data-cy="btn-delete-folder"
                :aria-label="$t('globals.buttons.delete')">
                <b-icon icon="trash-can-outline" size="is-small" />
              </a>
            </div>
          </div>
        </div>

        <div v-if="shownMedia.results && shownMedia.results.length > 0" class="grid">
          <div v-for="item in shownMedia.results" :key="item.id" class="item" :draggable="canManageMedia(item)"
            @dragstart="onDragStart('media', item.id, $event)" @dragend="onDragEnd">
            <div class="thumb">
              <a @click="(e) => onMediaSelect(item, e)" :href="item.url" target="_blank" rel="noopener noreferer"
                class="thumb-link">
                <div class="thumb-container">
                  <img v-if="item.thumbUrl" :src="item.thumbUrl" :title="item.filename" :alt="item.filename" />
                  <div v-else class="thumb-placeholder">
                    <span class="file-ext">
                      {{ item.filename.split(".").pop().toUpperCase() }}
                    </span>
                  </div>
                </div>
              </a>
              <div class="actions">
                <a v-if="canManageMedia(item)" href="#" data-cy="btn-delete" :aria-label="$t('globals.buttons.delete')" class="delete-btn"
                  @click.prevent="$utils.confirm($tc('globals.messages.confirmDelete', 1, { num: 1, name: item.filename }), () => onDeleteMedia(item.id), null, { type: 'is-danger' })">
                  <b-icon icon="trash-can-outline" size="is-small" />
                </a>
              </div>
            </div>
            <div class="info">
              <p class="filename" :title="item.filename">{{ item.filename }}</p>
              <p class="date">{{ $utils.niceDate(item.createdAt, false) }}</p>
              <p v-if="item.ownerName || item.ownerUsername" class="date">{{ ownerLabel(item) }}</p>
              <p v-if="adminView" class="date">
                {{ item.organizationName || $t('organizations.personalSpace') }}
                <span v-if="item.folderName"> / {{ item.folderName }}</span>
              </p>
              <b-tag v-if="transferPendingAt(item)" size="is-small" type="is-warning" class="is-light">
                {{ $t('shared.transferPending', { date: $utils.niceDate(transferPendingAt(item), true) }) }}
              </b-tag>
            </div>
          </div>
        </div>
      </div>

      <!-- Empty State -->
      <div v-else-if="!loading.media">
        <empty-placeholder
          :label="adminView || queryParams.query ? $t('globals.messages.emptyState') : $t('media.folderEmpty')" />
      </div>

      <!-- Pagination -->
      <div class="pagination-wrapper mt-5">
        <app-pagination :total="shownMedia.total" :current="queryParams.page" :per-page="shownMedia.perPage"
          @change="onPageChange" @per-page-change="onPerPageChange" />
      </div>
        </div>
      </div>
    </section>
    <b-modal :active.sync="showUploadForm" :aria-modal="true" :width="560" scroll="keep" trap-focus>
      <form class="media-upload-dialog" @submit.prevent="onSubmit" data-cy="upload">
        <h2 class="title is-5">{{ $t('media.upload') }}</h2>
        <b-field :label="$t('media.upload')">
          <b-upload v-model="form.files" drag-drop multiple expanded>
            <div class="has-text-centered section">
              <b-icon icon="file-upload-outline" size="is-large" />
              <p>{{ $t('media.uploadHelp') }}</p>
            </div>
          </b-upload>
        </b-field>
        <div v-if="form.files.length > 0" class="tags">
          <b-tag v-for="(f, i) in form.files" :key="i" size="is-medium" closable @close="removeUploadFile(i)">{{ f.name }}</b-tag>
        </div>
        <div class="buttons is-right mt-5">
          <b-button @click="showUploadForm = false">{{ $t('globals.buttons.cancel') }}</b-button>
          <b-button native-type="submit" type="is-primary" icon-left="file-upload-outline"
            :disabled="form.files.length === 0" :loading="isProcessing">
            {{ $t('media.upload') }}
          </b-button>
        </div>
      </form>
    </b-modal>
  </section>
</template>

<script>
import Vue from 'vue';
import { mapState } from 'vuex';
import EmptyPlaceholder from '../components/EmptyPlaceholder.vue';
import MediaFolderForm from '../components/MediaFolderForm.vue';

export default Vue.extend({
  components: {
    EmptyPlaceholder,
  },

  name: 'Media',

  props: {
    isModal: Boolean,
    type: { type: String, default: '' },
  },

  data() {
    return {
      form: {
        files: [],
      },
      toUpload: 0,
      uploaded: 0,
      showUploadForm: false,
      folders: [],
      adminMedia: { results: [], total: 0, perPage: 20 },
      categories: [],
      categoryOrganizationID: null,
      categoryOwnerID: null,
      currentFolderId: 0,
      dragOverFolderId: null,

      queryParams: {
        perPage: undefined,
        page: 1,
        query: '',
      },
    };
  },

  methods: {
    removeUploadFile(i) {
      this.form.files.splice(i, 1);
    },

    getMedia() {
      if (this.adminView) {
        return this.$api.queryPlatformMedia({
          page: this.queryParams.page,
          per_page: this.queryParams.perPage,
          query: this.queryParams.query,
          ...(this.categoryOrganizationID !== null ? { organization_id: this.categoryOrganizationID } : {}),
          ...(this.categoryOwnerID !== null ? { owner_user_id: this.categoryOwnerID } : {}),
        }).then((page) => { this.adminMedia = page; });
      }
      return this.$api.getMedia({
        page: this.queryParams.page,
        per_page: this.queryParams.perPage,
        query: this.queryParams.query,
        folder_id: this.currentFolderId,
      });
    },

    loadFolders() {
      return this.$api.getMediaFolders().then((folders) => {
        this.folders = Array.isArray(folders) ? folders : [];
        if (this.currentFolderId > 0 && !this.folderByID[this.currentFolderId]) {
          this.currentFolderId = 0;
          this.queryParams.page = 1;
        }
      });
    },

    refreshLibrary() {
      if (this.adminView) {
        return this.$api.getPlatformMediaCategories().then((categories) => {
          this.categories = Array.isArray(categories) ? categories : [];
          return this.getMedia();
        });
      }
      return this.loadFolders().then(() => this.getMedia());
    },

    selectOrganization(organizationID) {
      if (this.categoryOrganizationID === organizationID) return;
      this.categoryOrganizationID = organizationID;
      this.categoryOwnerID = null;
      this.queryParams.page = 1;
      this.getMedia();
    },

    selectOwner(ownerID) {
      if (this.categoryOwnerID === ownerID) return;
      this.categoryOwnerID = ownerID;
      this.queryParams.page = 1;
      this.getMedia();
    },

    openFolder(id) {
      const folderID = Number(id) || 0;
      if (folderID === this.currentFolderId) {
        return;
      }
      this.currentFolderId = folderID;
      this.queryParams.page = 1;
      this.getMedia();
    },

    onToggleForm() {
      this.showUploadForm = !this.showUploadForm;
    },

    onQueryMedia() {
      this.queryParams.page = 1;
      this.getMedia();
    },

    onMediaSelect(m, e) {
      // If the component is open in the modal mode, close the modal and
      // fire the selection event.
      // Otherwise, do nothing and let the image open like a normal link.
      if (this.isModal) {
        e.preventDefault();
        if (!this.$canUseResource(m, 'media:get')) {
          return;
        }
        this.$emit('selected', m);
        this.$parent.close();
      }
    },

    onSubmit() {
      this.uploadFiles(this.form.files, this.currentFolderId);
    },

    uploadFiles(files, folderID) {
      const filesToUpload = Array.from(files || []);
      this.toUpload = filesToUpload.length;
      if (this.toUpload === 0) {
        return;
      }

      // Upload N files with N requests.
      for (let i = 0; i < this.toUpload; i += 1) {
        const params = new FormData();
        params.set('file', filesToUpload[i]);
        params.set('folder_id', String(folderID || 0));
        this.$api.uploadMedia(params).then(() => {
          this.onUploaded();
        }, () => {
          this.onUploaded();
        });
      }
    },

    onDeleteMedia(id) {
      return this.$api.deleteMedia(id).then(() => this.refreshLibrary());
    },

    onCreateFolder() {
      this.openFolderForm();
    },

    onRenameFolder(folder) {
      this.openFolderForm(folder);
    },

    openFolderForm(folder = null) {
      this.$buefy.modal.open({
        parent: this,
        component: MediaFolderForm,
        hasModalCard: true,
        props: {
          folder,
          parentId: this.currentFolderId,
          isOrganization: Number(this.workspace.organizationId) > 0,
          onSaved: () => this.refreshLibrary(),
        },
      });
    },

    folderPermissionLabel(visibility) {
      const labels = { private: 'media.folderPrivate', organization: 'media.folderOrganization', global: 'media.folderGlobal' };
      return this.$t(labels[visibility] || labels.private);
    },

    onDeleteFolder(folder) {
      if (folder.mediaCount > 0 || folder.childCount > 0) {
        return;
      }
      this.$utils.confirm(this.$t('media.deleteFolderConfirm'), () => {
        const parentID = this.folderParentID(folder);
        this.$api.deleteMediaFolder(folder.id).then(() => {
          if (this.currentFolderId === folder.id) {
            this.currentFolderId = parentID;
            this.queryParams.page = 1;
          }
          return this.refreshLibrary();
        });
      });
    },

    onDragStart(type, id, event) {
      const { dataTransfer } = event;
      if (!dataTransfer) {
        return;
      }
      const payload = { type, id: Number(id) };
      dataTransfer.effectAllowed = 'move';
      dataTransfer.setData('application/x-listmonk-media', JSON.stringify(payload));
      dataTransfer.setData('text/plain', `listmonk-media:${type}:${id}`);
    },

    onDragEnd() {
      this.dragOverFolderId = null;
    },

    onDragOverFolder(folderID) {
      this.dragOverFolderId = Number(folderID) || 0;
    },

    onDragLeaveFolder(folderID) {
      if (this.dragOverFolderId === (Number(folderID) || 0)) {
        this.dragOverFolderId = null;
      }
    },

    dragPayload(event) {
      if (!event.dataTransfer) {
        return null;
      }
      const raw = event.dataTransfer.getData('application/x-listmonk-media');
      if (raw) {
        try {
          const payload = JSON.parse(raw);
          if (payload && (payload.type === 'media' || payload.type === 'folder') && Number(payload.id) > 0) {
            return { type: payload.type, id: Number(payload.id) };
          }
        } catch {
          // The text/plain fallback below is used by browsers that sanitize
          // custom drag data between nested elements.
        }
      }
      const fallback = event.dataTransfer.getData('text/plain') || '';
      const match = fallback.match(/^listmonk-media:(media|folder):(\d+)$/);
      return match ? { type: match[1], id: Number(match[2]) } : null;
    },

    onDropOnFolder(folderID, event) {
      this.dragOverFolderId = null;
      const targetID = Number(folderID) || 0;
      if (!this.canManageMediaLibrary || (targetID > 0 && (!this.folderByID[targetID] || !this.folderByID[targetID].writable))) return;
      const payload = this.dragPayload(event);
      if (payload) {
        if (payload.type === 'media') {
          this.$api.moveMediaToFolder(payload.id, targetID).then(() => this.refreshLibrary());
        } else if (payload.type === 'folder' && payload.id !== targetID) {
          this.$api.moveMediaFolder(payload.id, targetID).then(() => this.refreshLibrary());
        }
        return;
      }

      const files = event.dataTransfer && event.dataTransfer.files
        ? Array.from(event.dataTransfer.files)
        : [];
      if (files.length > 0) {
        this.form.files = files;
        this.uploadFiles(files, targetID);
      }
    },

    canManageMedia(item) {
      return !this.adminView && this.$canManageResource(item, 'media:manage');
    },

    folderParentID(folder) {
      return Number(folder.parentId) || 0;
    },

    ownerLabel(resource) {
      return resource.ownerName || resource.ownerUsername || '-';
    },

    transferPendingAt(resource) {
      return resource.transferPendingAt || resource.transfer_pending_at;
    },

    onUploaded() {
      this.uploaded += 1;
      if (this.uploaded >= this.toUpload) {
        this.toUpload = 0;
        this.uploaded = 0;
        this.form.files = [];
        this.showUploadForm = false;

        return this.refreshLibrary();
      }
      return null;
    },

    onPerPageChange(size) {
      this.queryParams.perPage = size;
      this.onPageChange(1);
    },

    onPageChange(p) {
      this.queryParams.page = p;
      this.getMedia();
    },
  },

  computed: {
    ...mapState(['loading', 'media', 'workspace']),

    adminView() {
      return !this.isModal && this.$isPlatformAdmin();
    },

    canWriteCurrentFolder() {
      return !this.adminView && this.canManageMediaLibrary && (this.currentFolderId === 0
        || (this.folderByID[this.currentFolderId] && this.folderByID[this.currentFolderId].writable));
    },

    canManageMediaLibrary() {
      return this.$canCreateWorkspaceResource('media:manage');
    },

    shownMedia() {
      return this.adminView ? this.adminMedia : this.media;
    },

    categoryGroups() {
      const groups = new Map();
      this.categories.forEach((row) => {
        const id = Number(row.organizationId) || 0;
        if (!groups.has(id)) {
          groups.set(id, {
            id, name: row.organizationName || this.$t('organizations.personalSpace'), count: 0, owners: [],
          });
        }
        const group = groups.get(id);
        group.count += row.mediaCount;
        group.owners.push({ id: Number(row.ownerUserId) || 0, name: row.ownerName || '-', count: row.mediaCount });
      });
      return [...groups.values()];
    },

    platformMediaCount() {
      return this.categoryGroups.reduce((total, group) => total + group.count, 0);
    },

    visibleOwners() {
      const owners = new Map();
      this.categoryGroups.filter((group) => this.categoryOrganizationID === null || group.id === this.categoryOrganizationID)
        .forEach((group) => group.owners.forEach((owner) => {
          const current = owners.get(owner.id);
          if (current) current.count += owner.count;
          else owners.set(owner.id, { ...owner });
        }));
      return [...owners.values()].sort((a, b) => a.name.localeCompare(b.name));
    },

    visibleOwnerCount() {
      return this.visibleOwners.reduce((total, owner) => total + owner.count, 0);
    },

    selectedOrganizationName() {
      const group = this.categoryGroups.find((entry) => entry.id === this.categoryOrganizationID);
      return group ? group.name : '';
    },

    selectedOwnerName() {
      const owner = this.visibleOwners.find((entry) => entry.id === this.categoryOwnerID);
      return owner ? owner.name : '';
    },

    folderByID() {
      const out = {};
      this.folders.forEach((folder) => {
        out[folder.id] = folder;
      });
      return out;
    },

    visibleFolders() {
      if (this.adminView) return [];
      return this.folders.filter((folder) => this.folderParentID(folder) === this.currentFolderId);
    },

    breadcrumbs() {
      const out = [];
      const seen = {};
      let id = this.currentFolderId;
      while (id > 0 && !seen[id]) {
        seen[id] = true;
        const folder = this.folderByID[id];
        if (!folder) {
          break;
        }
        out.unshift(folder);
        id = this.folderParentID(folder);
      }
      return out;
    },

    isProcessing() {
      if (this.toUpload > 0 && this.uploaded < this.toUpload) {
        return true;
      }
      return false;
    },
  },

  created() {
    this.$root.$on('page.refresh', this.refreshLibrary);
  },

  destroyed() {
    this.$root.$off('page.refresh', this.refreshLibrary);
  },

  mounted() {
    this.refreshLibrary();
  },
});
</script>

<style scoped>
.media-page-header { margin-bottom: 1.25rem; }
.media-surface { max-width: none; border: 1px solid var(--lm-color-border); border-radius: var(--lm-radius-md); background: #fff; }
.media-layout { display: grid; grid-template-columns: minmax(0, 1fr); min-height: 540px; }
.is-platform-catalog .media-layout { grid-template-columns: 190px 190px minmax(0, 1fr); }
.media-category-column { padding: 1rem 0.65rem; border-right: 1px solid var(--lm-color-border); }
.media-category-column h2 { margin: 0 0 0.75rem 0.65rem; font-size: 0.95rem; font-weight: 700; }
.media-category-option { display: flex; align-items: center; gap: 0.5rem; width: 100%; min-height: 40px; padding: 0.45rem 0.6rem; border: 0; border-radius: 6px; background: transparent; color: #35445d; text-align: left; cursor: pointer; }
.media-category-option:hover, .media-category-option:focus-visible { background: #f1f5fb; }
.media-category-option.is-active { background: #eaf2ff; color: #1260d7; font-weight: 600; }
.media-category-option > span:nth-child(2) { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.media-category-count { margin-left: auto; color: #73829a; font-size: 0.8rem; font-variant-numeric: tabular-nums; }
.media-main { min-width: 0; padding: 1rem; }
.media-toolbar { display: flex; align-items: center; gap: 1rem; }
.media-search { flex: 1; min-width: 0; }
.media-toolbar-actions .buttons { margin-bottom: 0; flex-wrap: nowrap; }
.media-catalog-context { display: flex; align-items: center; justify-content: space-between; gap: 0.75rem; color: #6a7890; font-size: 0.85rem; }
.media-catalog-context > span { white-space: nowrap; }
.media-catalog-context .tag { margin-left: 0.35rem; }
.media-upload-dialog { padding: 1.5rem; border-radius: var(--lm-radius-md); background: #fff; }
@media (max-width: 1100px) { .is-platform-catalog .media-layout { grid-template-columns: 150px 150px minmax(0, 1fr); } }
@media (max-width: 800px) {
  .is-platform-catalog .media-layout { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .media-category-column { border-bottom: 1px solid var(--lm-color-border); }
  .media-category-column:nth-child(2) { border-right: 0; }
  .media-main { grid-column: 1 / -1; }
  .media-toolbar { flex-wrap: wrap; }
  .media-toolbar-actions { width: 100%; }
  .media-catalog-context { flex-wrap: wrap; }
}
</style>
