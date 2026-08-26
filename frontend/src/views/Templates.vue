<template>
  <section class="templates">
    <header class="columns page-header">
      <div class="column is-10">
        <h1 class="title is-4">
          {{ $t('globals.terms.templates') }}
          <span v-if="templates.length > 0">({{ templates.length }})</span>
        </h1>
      </div>
      <div class="column has-text-right">
        <b-field expanded>
          <b-button expanded type="is-primary" icon-left="plus" class="btn-new" @click="showNewForm">
            {{ $t('globals.buttons.new') }}
          </b-button>
        </b-field>
      </div>
    </header>

    <b-table :data="templates" :hoverable="true" :loading="loading.templates" default-sort="createdAt">
      <b-table-column v-slot="props" field="name" :label="$t('globals.fields.name')" :td-attrs="$utils.tdID" sortable>
        <a v-if="canManageTemplate(props.row)" href="#" @click.prevent="showEditForm(props.row)">
          {{ props.row.name }}
        </a>
        <span v-else>{{ props.row.name }}</span>
        <b-tag v-if="props.row.isDefault">
          {{ $t('templates.default') }}
        </b-tag>

        <p class="is-size-7 has-text-grey" v-if="props.row.type === 'tx'">
          {{ props.row.subject }}
        </p>
      </b-table-column>

      <b-table-column v-slot="props" field="ownerUsername" label="所属用户" sortable>
        {{ ownerLabel(props.row) }}
        <b-tag size="is-small" class="is-light">{{ visibilityLabel(props.row.visibility) }}</b-tag>
      </b-table-column>

      <b-table-column v-slot="props" field="type" :label="$t('globals.fields.type')" sortable>
        <b-tag v-if="props.row.type === 'campaign'" :class="props.row.type" :data-cy="`type-${props.row.type}`">
          {{ $tc('templates.typeCampaignHTML') }}
        </b-tag>
        <b-tag v-else-if="props.row.type === 'campaign_visual'" :class="props.row.type"
          :data-cy="`type-${props.row.type}`">
          {{ $tc('templates.typeCampaignVisual') }}
        </b-tag>
        <b-tag v-else :class="props.row.type" :data-cy="`type-${props.row.type}`">
          {{ $tc('templates.typeTransactional') }}
        </b-tag>
      </b-table-column>

      <b-table-column v-slot="props" field="id" :label="$t('globals.fields.id')" sortable>
        {{ props.row.id }}
      </b-table-column>

      <b-table-column v-slot="props" field="createdAt" :label="$t('globals.fields.createdAt')" sortable>
        {{ $utils.niceDate(props.row.createdAt) }}
      </b-table-column>

      <b-table-column v-slot="props" field="updatedAt" :label="$t('globals.fields.updatedAt')" sortable>
        {{ $utils.niceDate(props.row.updatedAt) }}
      </b-table-column>

      <b-table-column v-slot="props" cell-class="actions" align="right">
        <div>
          <a href="#" @click.prevent="previewTemplate(props.row)" data-cy="btn-preview"
            :aria-label="$t('templates.preview')">
            <b-tooltip :label="$t('templates.preview')" type="is-dark">
              <b-icon icon="file-find-outline" size="is-small" />
            </b-tooltip>
          </a>
          <a v-if="canManageTemplate(props.row)" href="#" @click.prevent="showEditForm(props.row)" data-cy="btn-edit"
            :aria-label="$t('globals.buttons.edit')">
            <b-tooltip :label="$t('globals.buttons.edit')" type="is-dark">
              <b-icon icon="pencil-outline" size="is-small" />
            </b-tooltip>
          </a>
          <a href="#" @click.prevent="$utils.prompt($t('globals.buttons.clone'),
            { placeholder: $t('globals.fields.name'), value: $t('campaigns.copyOf', { name: props.row.name }) },
            (name) => cloneTemplate(name, props.row))" data-cy="btn-clone" :aria-label="$t('globals.buttons.clone')">
            <b-tooltip :label="$t('globals.buttons.clone')" type="is-dark">
              <b-icon icon="file-multiple-outline" size="is-small" />
            </b-tooltip>
          </a>
          <a v-if="canManageOrganizationTemplate(props.row)" href="#"
            @click.prevent="showOrganizationTemplateActions(props.row)" aria-label="管理组织共享模板">
            <b-tooltip label="管理组织共享模板" type="is-dark">
              <b-icon icon="account-cog-outline" size="is-small" />
            </b-tooltip>
          </a>
          <a v-if="canManageTemplate(props.row) && !props.row.isDefault && props.row.type === 'campaign'" href="#"
            @click.prevent="$utils.confirm(null, () => makeTemplateDefault(props.row))" data-cy="btn-set-default"
            :aria-label="$t('templates.makeDefault')">
            <b-tooltip :label="$t('templates.makeDefault')" type="is-dark">
              <b-icon icon="check-circle-outline" size="is-small" />
            </b-tooltip>
          </a>
          <span v-else class="a has-text-grey-light">
            <b-icon icon="check-circle-outline" size="is-small" />
          </span>

          <a v-if="canManageTemplate(props.row) && !props.row.isDefault" href="#" @click.prevent="$utils.confirm(null, () => deleteTemplate(props.row))"
            data-cy="btn-delete" :aria-label="$t('globals.buttons.delete')">
            <b-tooltip :label="$t('globals.buttons.delete')" type="is-dark">
              <b-icon icon="trash-can-outline" size="is-small" />
            </b-tooltip>
          </a>
          <span v-else class="a has-text-grey-light">
            <b-icon icon="trash-can-outline" size="is-small" />
          </span>
        </div>
      </b-table-column>

      <template #empty v-if="!loading.templates">
        <empty-placeholder />
      </template>
    </b-table>

    <!-- Add / edit form modal -->
    <b-modal scroll="keep" :aria-modal="true" :active.sync="isFormVisible" :width="1200" :can-cancel="false"
      class="template-modal">
      <template-form :data="curItem" :is-editing="isEditing" @finished="formFinished" />
    </b-modal>

    <campaign-preview v-if="previewItem" type="template" :id="previewItem.id" :template-type="previewItem.type"
      :title="previewItem.name" @close="closePreview" />

    <b-modal scroll="keep" :aria-modal="true" :active.sync="isOrganizationTemplateActionsVisible" :width="520">
      <div class="modal-card content" style="width: auto">
        <header class="modal-card-head"><h4>管理组织共享模板</h4></header>
        <section class="modal-card-body">
          <p v-if="organizationTemplate" class="mb-4">{{ organizationTemplate.name }}</p>
          <b-field label="转移给成员" label-position="on-border">
            <b-select v-model.number="organizationTemplateTargetUserID" expanded>
              <option :value="null">请选择成员</option>
              <option v-for="member in activeOrganizationMembers" :key="member.userId" :value="member.userId">
                {{ member.username }}
              </option>
            </b-select>
          </b-field>
        </section>
        <footer class="modal-card-foot is-justify-content-space-between">
          <b-button type="is-text" @click="unpublishOrganizationTemplate">下架为个人私有</b-button>
          <div>
            <b-button @click="isOrganizationTemplateActionsVisible = false">{{ $t('globals.buttons.close') }}</b-button>
            <b-button type="is-primary" :disabled="!organizationTemplateTargetUserID"
              @click="transferOrganizationTemplate">
转移
</b-button>
          </div>
        </footer>
      </div>
    </b-modal>
  </section>
</template>

<script>
import Vue from 'vue';
import { mapState } from 'vuex';
import CampaignPreview from '../components/CampaignPreview.vue';
import EmptyPlaceholder from '../components/EmptyPlaceholder.vue';

import TemplateForm from './TemplateForm.vue';

export default Vue.extend({
  components: {
    CampaignPreview,
    TemplateForm,
    EmptyPlaceholder,
  },

  data() {
    return {
      curItem: null,
      isEditing: false,
      isFormVisible: false,
      previewItem: null,
      isOrganizationTemplateActionsVisible: false,
      organizationTemplate: null,
      organizationTemplateTargetUserID: null,
      organizationMembers: [],
    };
  },

  methods: {
    fetchTemplates() {
      this.$api.getTemplates();
    },

    // Show the edit form.
    showEditForm(data) {
      this.curItem = data;
      this.isFormVisible = true;
      this.isEditing = true;
    },

    // Show the new form.
    showNewForm() {
      this.curItem = { type: 'campaign' };
      this.isFormVisible = true;
      this.isEditing = false;
    },

    formFinished() {
      this.$api.getTemplates();
    },

    previewTemplate(c) {
      this.previewItem = c;
    },

    closePreview() {
      this.previewItem = null;
    },

    cloneTemplate(name, t) {
      this.$api.cloneTemplate(t.id, { name }).then((d) => {
        this.$api.getTemplates();
        this.$emit('finished');
        this.$utils.toast(`'${d.name}' created`);
      });
    },

    canManageTemplate(template) {
      return this.$canManageResource(template);
    },

    canManageOrganizationTemplate(template) {
      return this.isOrganizationManager
        && template.visibility === 'organization'
        && !template.transferPendingAt;
    },

    async showOrganizationTemplateActions(template) {
      this.organizationTemplate = template;
      this.organizationTemplateTargetUserID = null;
      this.organizationMembers = await this.$api.getOrganizationMembers();
      this.isOrganizationTemplateActionsVisible = true;
    },

    async transferOrganizationTemplate() {
      if (!this.organizationTemplate || !this.organizationTemplateTargetUserID) {
        return;
      }
      await this.$api.transferOrganizationTemplate(this.organizationTemplate.id, {
        target_user_id: this.organizationTemplateTargetUserID,
      });
      this.isOrganizationTemplateActionsVisible = false;
      await this.$api.getTemplates();
    },

    async unpublishOrganizationTemplate() {
      if (!this.organizationTemplate) {
        return;
      }
      await this.$api.unpublishOrganizationTemplate(this.organizationTemplate.id);
      this.isOrganizationTemplateActionsVisible = false;
      await this.$api.getTemplates();
    },

    ownerLabel(resource) {
      return resource.ownerName || resource.ownerUsername || '-';
    },

    visibilityLabel(visibility) {
      return {
        private: '个人私有',
        organization: '组织共享',
        global: '全体共享',
      }[visibility] || '个人私有';
    },

    makeTemplateDefault(tpl) {
      this.$api.makeTemplateDefault(tpl.id).then(() => {
        this.$api.getTemplates();
        this.$utils.toast(this.$t('globals.messages.created', { name: tpl.name }));
      });
    },

    deleteTemplate(tpl) {
      this.$api.deleteTemplate(tpl.id).then(() => {
        this.$api.getTemplates();
        this.$utils.toast(this.$t('globals.messages.deleted', { name: tpl.name }));
      });
    },
  },

  computed: {
    ...mapState(['templates', 'loading', 'workspace', 'profile']),

    isOrganizationManager() {
      return this.workspace.organizationId > 0
        && (this.workspace.role === 'manager' || (this.profile.userRole && this.profile.userRole.id === 1));
    },

    activeOrganizationMembers() {
      return this.organizationMembers.filter((member) => !member.removedAt);
    },
  },

  created() {
    this.$root.$on('page.refresh', this.fetchTemplates);
  },

  destroyed() {
    this.$root.$off('page.refresh', this.fetchTemplates);
  },

  mounted() {
    this.$api.getTemplates();
  },
});
</script>
