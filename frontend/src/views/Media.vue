<template>
  <section class="media-files">
    <h1 class="title is-4">
      {{ $t('media.title') }}
      <span v-if="media.total > 0">({{ media.total }})</span>
      <span class="has-text-grey-light"> / {{ serverConfig.media_provider }}</span>
    </h1>

    <b-loading :active="isProcessing || loading.media" />

    <section class="wrap gallery mt-6">
      <div class="columns is-vcentered mb-4">
        <div class="column">
          <form @submit.prevent="onQueryMedia" class="search">
            <div>
              <b-field>
                <b-input v-model="queryParams.query" name="query" expanded icon="magnify" ref="query" data-cy="query" />
                <p class="controls">
                  <b-button native-type="submit" type="is-primary" icon-left="magnify" data-cy="btn-query" />
                </p>
              </b-field>
            </div>
          </form>
        </div>
        <div v-if="$canCreateWorkspaceResource('media:manage')" class="column is-narrow">
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

      <nav class="breadcrumb media-breadcrumb" :aria-label="$t('media.title')">
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
      <p class="media-folder-help has-text-grey is-size-7 mb-4">{{ $t('media.folderHelp') }}</p>

      <b-collapse v-if="$canCreateWorkspaceResource('media:manage')" v-model="showUploadForm" animation="">
        <form @submit.prevent="onSubmit" class="mb-6" data-cy="upload">
          <div>
            <b-field :label="$t('media.upload')">
              <b-upload v-model="form.files" drag-drop multiple expanded>
                <div class="has-text-centered section">
                  <p>
                    <b-icon icon="file-upload-outline" size="is-large" />
                  </p>
                  <p>{{ $t('media.uploadHelp') }}</p>
                </div>
              </b-upload>
            </b-field>
            <div class="tags" v-if="form.files.length > 0">
              <b-tag v-for="(f, i) in form.files" :key="i" size="is-medium" closable @close="removeUploadFile(i)">
                {{ f.name }}
              </b-tag>
            </div>
            <div class="buttons">
              <b-button native-type="submit" type="is-primary" icon-left="file-upload-outline"
                :disabled="form.files.length === 0" :loading="isProcessing">
                {{ $tc('media.upload') }}
              </b-button>
            </div>
          </div>
        </form>
      </b-collapse>

      <!-- Pagination -->
      <div v-if="media.total > media.perPage" class="pagination-wrapper mt-5">
        <b-pagination :total="media.total" :current.sync="media.page" :per-page="media.perPage"
          @change="onPageChange" />
      </div>

      <div v-if="loading.media" class="has-text-centered py-6">
        <b-loading :active="loading.media" />
      </div>
      <div v-else-if="(visibleFolders.length > 0) || (media.results && media.results.length > 0)" class="media-browser-content">
        <div v-if="visibleFolders.length > 0" class="folder-grid">
          <div v-for="folder in visibleFolders" :key="`folder-${folder.id}`" class="folder-item"
            :class="{ 'is-drop-target': dragOverFolderId === folder.id }"
            :draggable="canManageMediaLibrary" role="button" tabindex="0"
            @click="openFolder(folder.id)" @keydown.enter.prevent="openFolder(folder.id)"
            @keydown.space.prevent="openFolder(folder.id)"
            @dragstart="onDragStart('folder', folder.id, $event)" @dragend="onDragEnd"
            @dragover.prevent.stop="onDragOverFolder(folder.id)" @dragleave="onDragLeaveFolder(folder.id)"
            @drop.prevent.stop="onDropOnFolder(folder.id, $event)" data-cy="media-folder">
            <div class="folder-icon"><b-icon icon="folder-outline" size="is-large" /></div>
            <div class="folder-info">
              <p class="folder-name" :title="folder.name">{{ folder.name }}</p>
              <p class="folder-meta">
                {{ $tc('media.folderItems', folder.mediaCount, { count: folder.mediaCount }) }}
                <span v-if="folder.childCount > 0"> · {{ $tc('media.folderFolders', folder.childCount, { count: folder.childCount }) }}</span>
              </p>
            </div>
            <div v-if="canManageMediaLibrary" class="folder-actions">
              <a href="#" @click.prevent.stop="onRenameFolder(folder)" data-cy="btn-rename-folder"
                :aria-label="$t('globals.buttons.edit')">
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

        <div v-if="media.results && media.results.length > 0" class="grid">
          <div v-for="item in media.results" :key="item.id" class="item" :draggable="canManageMedia(item)"
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
                <a v-if="canManageMedia(item)" href="#" @click.prevent="$utils.confirm(null, () => onDeleteMedia(item.id))" data-cy="btn-delete"
                  :aria-label="$t('globals.buttons.delete')" class="delete-btn">
                  <b-icon icon="trash-can-outline" size="is-small" />
                </a>
              </div>
            </div>
            <div class="info">
              <p class="filename" :title="item.filename">{{ item.filename }}</p>
              <p class="date">{{ $utils.niceDate(item.createdAt, false) }}</p>
              <p v-if="item.ownerName || item.ownerUsername" class="date">{{ ownerLabel(item) }}</p>
              <b-tag v-if="transferPendingAt(item)" size="is-small" type="is-warning" class="is-light">
                {{ $t('shared.transferPending', { date: $utils.niceDate(transferPendingAt(item), true) }) }}
              </b-tag>
            </div>
          </div>
        </div>
      </div>

      <!-- Empty State -->
      <div v-else-if="!loading.media">
        <empty-placeholder :label="$t('media.folderEmpty')" />
      </div>

      <!-- Pagination -->
      <div v-if="media.total > media.perPage" class="pagination-wrapper mt-5">
        <b-pagination :total="media.total" :current.sync="media.page" :per-page="media.perPage"
          @change="onPageChange" />
      </div>
    </section>
  </section>
</template>

<script>
import Vue from 'vue';
import { mapState } from 'vuex';
import EmptyPlaceholder from '../components/EmptyPlaceholder.vue';

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
      currentFolderId: 0,
      dragOverFolderId: null,

      queryParams: {
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
      return this.$api.getMedia({
        page: this.queryParams.page,
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
      return this.loadFolders().then(() => this.getMedia());
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
      this.$utils.setPref('media.upload', this.showUploadForm);
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
      this.$api.deleteMedia(id).then(() => {
        this.refreshLibrary();
      });
    },

    onCreateFolder() {
      this.$utils.prompt(this.$t('media.createFolderPrompt'), {
        placeholder: this.$t('media.folderNamePlaceholder'),
      }, (name) => {
        this.$api.createMediaFolder({
          name,
          parent_id: this.currentFolderId || null,
        }).then(() => this.loadFolders());
      });
    },

    onRenameFolder(folder) {
      this.$utils.prompt(this.$t('media.renameFolderPrompt'), {
        placeholder: this.$t('media.folderNamePlaceholder'),
        value: folder.name,
      }, (name) => {
        this.$api.renameMediaFolder(folder.id, { name }).then(() => this.loadFolders());
      });
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
        this.showUploadForm = true;
        this.uploadFiles(files, targetID);
      }
    },

    canManageMedia(item) {
      return this.$canManageResource(item, 'media:manage');
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

        this.refreshLibrary();
      }
    },

    onPageChange(p) {
      this.queryParams.page = p;
      this.getMedia();
    },
  },

  computed: {
    ...mapState(['loading', 'media', 'serverConfig']),

    canManageMediaLibrary() {
      return this.$canCreateWorkspaceResource('media:manage');
    },

    folderByID() {
      const out = {};
      this.folders.forEach((folder) => {
        out[folder.id] = folder;
      });
      return out;
    },

    visibleFolders() {
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

    if (this.$utils.getPref('media.upload')) {
      this.showUploadForm = true;
    }
  },
});
</script>
