<template>
  <section class="organizations org-page">
    <header class="org-page-header">
      <div>
        <p class="org-eyebrow">组织空间</p>
        <h1 class="title is-4">我参与的组织</h1>
        <p class="org-subtitle">管理你加入的工作空间，并将个人资源安全地迁移到组织。</p>
      </div>
      <div class="org-header-meta">
        <span class="org-meta-label">当前空间</span>
        <strong>{{ workspace.organizationId ? workspace.organizationName : '个人空间' }}</strong>
      </div>
    </header>

    <section class="org-overview" aria-label="组织概览">
      <div class="org-stat">
        <span class="org-stat-icon is-blue"><b-icon icon="office-building-outline" /></span>
        <div><span class="org-stat-label">已加入组织</span><strong>{{ organizations.length }}</strong></div>
      </div>
      <div class="org-stat">
        <span class="org-stat-icon is-green"><b-icon icon="account-multiple-outline" /></span>
        <div><span class="org-stat-label">可协作成员</span><strong>{{ organizationMemberTotal }}</strong></div>
      </div>
      <div class="org-stat">
        <span class="org-stat-icon is-orange"><b-icon icon="folder-move-outline" /></span>
        <div><span class="org-stat-label">待迁移资源</span><strong>{{ personalResourceTotal }}</strong></div>
      </div>
    </section>

    <section class="org-panel">
      <div class="org-panel-heading">
        <div>
          <h2>组织列表</h2>
          <p>进入组织后即可使用该空间的列表、活动与模板。</p>
        </div>
        <b-tag type="is-light" rounded>{{ organizations.length }} 个组织</b-tag>
      </div>
      <b-table :data="organizations" :mobile-cards="false" narrowed class="org-table">
        <b-table-column v-slot="props" field="name" label="组织">
          <div class="org-name-cell">
            <span class="org-avatar"><b-icon icon="office-building-outline" size="is-small" /></span>
            <div><strong>{{ props.row.name }}</strong><p v-if="props.row.description">{{ props.row.description }}</p></div>
          </div>
        </b-table-column>
        <b-table-column v-slot="props" field="myRole" label="我的角色">
          <b-tag :type="props.row.myRole === 'manager' ? 'is-primary is-light' : 'is-light'" rounded>
            {{ roleLabel(props.row.myRole) }}
          </b-tag>
        </b-table-column>
        <b-table-column v-slot="props" field="memberCount" label="成员数">
          <span class="member-count"><b-icon icon="account-multiple-outline" size="is-small" />{{ props.row.memberCount }}</span>
        </b-table-column>
        <b-table-column v-slot="props" label="状态">
          <span v-if="isActiveOrganization(props.row)" class="status-current"><i />当前空间</span>
          <span v-else class="has-text-grey-light">未使用</span>
        </b-table-column>
        <b-table-column v-slot="props" label="操作" numeric>
          <div class="org-actions">
            <b-button v-if="!isActiveOrganization(props.row)" size="is-small" type="is-primary" outlined icon-left="login-variant" @click="switchWorkspace(props.row)">
              进入
            </b-button>
            <b-button size="is-small" type="is-text" class="leave-action" icon-left="logout-variant" @click="leaveOrganization(props.row)">
              离开
            </b-button>
          </div>
        </b-table-column>
        <template #empty>
          <div class="org-empty"><b-icon icon="office-building-outline" size="is-medium" /><strong>尚未加入组织</strong><span>加入组织后，可在这里切换工作空间。</span></div>
        </template>
      </b-table>
    </section>

    <section v-if="organizations.length" class="migration-panel">
      <div class="org-panel-heading migration-heading">
        <div>
          <span class="step-kicker">资源整理</span>
          <h2>迁移个人资源</h2>
          <p>先选择目标组织，再选择要复制或移动的资源。</p>
        </div>
        <div class="migration-summary"><span>已选择</span><strong>{{ selectedResourceTotal }}</strong><span>项资源</span></div>
      </div>

      <div class="migration-target">
        <span class="step-number">1</span>
        <div class="target-copy"><strong>选择目标组织</strong><span>资源将迁移到所选组织中</span></div>
        <b-field class="target-field" label="目标组织" label-position="on-border">
          <b-select v-model.number="migrationOrganizationID" expanded>
            <option :value="null">请选择组织</option>
            <option v-for="organization in organizations" :key="organization.id" :value="organization.id">{{ organization.name }}</option>
          </b-select>
        </b-field>
      </div>

      <div class="resource-step-label"><span class="step-number">2</span><div><strong>选择资源并执行操作</strong><span>复制会保留个人空间中的原始资源，移动后原资源将不再保留</span></div></div>
      <div class="resource-grid">
        <article class="resource-card resource-list-card">
          <div class="resource-card-top">
            <span class="resource-icon"><b-icon icon="format-list-bulleted-square" /></span>
            <div><h3>个人列表</h3><span>{{ personalLists.length }} 项可迁移</span></div>
            <b-button class="resource-preview-button" size="is-small" type="is-text"
              icon-left="file-find-outline" :disabled="!personalListIDs.length"
              @click="previewResource('lists', personalListIDs, personalLists)">
              预览内容
            </b-button>
          </div>
          <b-field class="resource-field">
            <b-select v-model="personalListIDs" multiple expanded :disabled="!personalLists.length" placeholder="选择列表">
              <option v-for="list in personalLists" :key="list.id" :value="list.id">{{ list.name }}</option>
            </b-select>
          </b-field>
          <div class="resource-card-footer">
            <span>{{ personalListIDs.length }} 项已选</span>
            <div>
              <b-button size="is-small" type="is-light" icon-left="content-copy" :disabled="!canMigrate(personalListIDs)" @click="migrateLists('copy')">复制</b-button>
              <b-button size="is-small" type="is-primary" icon-left="folder-move" :disabled="!canMigrate(personalListIDs)" @click="migrateLists('move')">移动</b-button>
            </div>
          </div>
        </article>
        <article class="resource-card resource-template-card">
          <div class="resource-card-top">
            <span class="resource-icon"><b-icon icon="email-outline" /></span>
            <div><h3>邮件模板</h3><span>{{ personalTemplates.length }} 项可迁移</span></div>
            <b-button class="resource-preview-button" size="is-small" type="is-text"
              icon-left="file-find-outline" :disabled="!personalTemplateIDs.length"
              @click="previewResource('templates', personalTemplateIDs, personalTemplates)">
              预览内容
            </b-button>
          </div>
          <b-field class="resource-field">
            <b-select v-model="personalTemplateIDs" multiple expanded :disabled="!personalTemplates.length" placeholder="选择模板">
              <option v-for="template in personalTemplates" :key="template.id" :value="template.id">{{ template.name }}</option>
            </b-select>
          </b-field>
          <div class="resource-card-footer">
            <span>{{ personalTemplateIDs.length }} 项已选</span>
            <div>
              <b-button size="is-small" type="is-light" icon-left="content-copy"
                :disabled="!canMigrate(personalTemplateIDs)"
                @click="migrateResource('templates', personalTemplateIDs, 'copy')">
                复制
              </b-button>
              <b-button size="is-small" type="is-primary" icon-left="folder-move"
                :disabled="!canMigrate(personalTemplateIDs)"
                @click="migrateResource('templates', personalTemplateIDs, 'move')">
                移动
              </b-button>
            </div>
          </div>
        </article>
        <article class="resource-card resource-campaign-card">
          <div class="resource-card-top">
            <span class="resource-icon"><b-icon icon="rocket-launch-outline" /></span>
            <div><h3>营销活动</h3><span>{{ personalCampaigns.length }} 项可迁移</span></div>
            <b-button class="resource-preview-button" size="is-small" type="is-text"
              icon-left="file-find-outline" :disabled="!personalCampaignIDs.length"
              @click="previewResource('campaigns', personalCampaignIDs, personalCampaigns)">
              预览内容
            </b-button>
          </div>
          <b-field class="resource-field">
            <b-select v-model="personalCampaignIDs" multiple expanded :disabled="!personalCampaigns.length" placeholder="选择活动">
              <option v-for="campaign in personalCampaigns" :key="campaign.id" :value="campaign.id">{{ campaign.name }} ({{ campaign.status }})</option>
            </b-select>
          </b-field>
          <div class="resource-card-footer">
            <span>{{ personalCampaignIDs.length }} 项已选</span>
            <div>
              <b-button size="is-small" type="is-light" icon-left="content-copy"
                :disabled="!canMigrate(personalCampaignIDs)"
                @click="migrateResource('campaigns', personalCampaignIDs, 'copy')">
                复制
              </b-button>
              <b-button size="is-small" type="is-primary" icon-left="folder-move"
                :disabled="!canMigrate(personalCampaignIDs)"
                @click="migrateResource('campaigns', personalCampaignIDs, 'move')">
                移动
              </b-button>
            </div>
          </div>
        </article>
        <article class="resource-card resource-media-card">
          <div class="resource-card-top">
            <span class="resource-icon"><b-icon icon="image-multiple-outline" /></span>
            <div><h3>媒体文件</h3><span>{{ personalMedia.length }} 项可迁移</span></div>
            <b-button class="resource-preview-button" size="is-small" type="is-text"
              icon-left="file-find-outline" :disabled="!personalMediaIDs.length"
              @click="previewResource('media', personalMediaIDs, personalMedia)">
              预览内容
            </b-button>
          </div>
          <b-field class="resource-field">
            <b-select v-model="personalMediaIDs" multiple expanded :disabled="!personalMedia.length" placeholder="选择文件">
              <option v-for="media in personalMedia" :key="media.id" :value="media.id">{{ media.filename }}</option>
            </b-select>
          </b-field>
          <div class="resource-card-footer">
            <span>{{ personalMediaIDs.length }} 项已选</span>
            <div>
              <b-button size="is-small" type="is-light" icon-left="content-copy" :disabled="!canMigrate(personalMediaIDs)" @click="migrateResource('media', personalMediaIDs, 'copy')">复制</b-button>
              <b-button size="is-small" type="is-primary" icon-left="folder-move" :disabled="!canMigrate(personalMediaIDs)" @click="migrateResource('media', personalMediaIDs, 'move')">移动</b-button>
            </div>
          </div>
        </article>
      </div>
      <div class="migration-tip"><b-icon icon="information-outline" size="is-small" /><span>迁移只会处理“个人”可见资源，组织共享资源不会出现在列表中。</span></div>
    </section>

    <b-modal :active.sync="isPreviewVisible" :width="760" scroll="keep" :aria-modal="true">
      <div v-if="previewItem" class="resource-preview-modal">
        <header class="resource-preview-header">
          <div class="resource-preview-title">
            <span class="resource-icon"><b-icon :icon="previewIcon" /></span>
            <div><span class="preview-kicker">内容预览</span><h2>{{ previewItem.name || previewItem.filename }}</h2></div>
          </div>
          <b-button type="is-text" icon-left="close" aria-label="关闭预览" @click="closePreview" />
        </header>
        <section v-if="previewResourceType === 'lists'" class="resource-preview-body">
          <div class="preview-facts">
            <div><span>订阅者</span><strong>{{ previewItem.subscriberCount || 0 }}</strong></div>
            <div><span>列表类型</span><strong>{{ previewItem.type === 'public' ? '公开' : '私有' }}</strong></div>
            <div><span>状态</span><strong>{{ previewItem.status === 'active' ? '正常' : '已归档' }}</strong></div>
          </div>
          <div class="preview-copy"><span class="preview-label">列表说明</span><p>{{ previewItem.description || '暂无列表说明。' }}</p></div>
        </section>
        <section v-else-if="previewResourceType === 'media'" class="resource-preview-body">
          <div v-if="previewItem.thumbUrl || previewItem.url" class="media-preview-image"><img :src="previewItem.thumbUrl || previewItem.url" :alt="previewItem.filename" /></div>
          <div class="preview-copy">
            <span class="preview-label">文件名</span><p>{{ previewItem.filename }}</p>
            <span class="preview-label">文件类型</span><p>{{ previewItem.contentType || previewItem.mimeType || '媒体文件' }}</p>
          </div>
        </section>
        <section v-else class="resource-preview-body">
          <div class="preview-copy"><span class="preview-label">主题</span><p>{{ previewItem.subject || '无主题' }}</p></div>
          <div class="preview-frame-wrap"><iframe :srcdoc="previewDocument" sandbox="" :title="previewItem.name" /></div>
        </section>
        <footer class="resource-preview-footer"><b-button @click="closePreview">关闭</b-button></footer>
      </div>
    </b-modal>
  </section>
</template>

<script>
import Vue from 'vue';
import { mapState } from 'vuex';

export default Vue.extend({
  data() {
    return {
      migrationOrganizationID: null,
      personalLists: [],
      personalListIDs: [],
      personalTemplates: [],
      personalTemplateIDs: [],
      personalCampaigns: [],
      personalCampaignIDs: [],
      personalMedia: [],
      personalMediaIDs: [],
      isPreviewVisible: false,
      previewItem: null,
      previewResourceType: '',
    };
  },

  computed: {
    ...mapState(['workspace', 'organizations']),

    previewIcon() {
      return {
        lists: 'format-list-bulleted-square',
        templates: 'email-outline',
        campaigns: 'rocket-launch-outline',
        media: 'image-multiple-outline',
      }[this.previewResourceType] || 'file-find-outline';
    },

    previewDocument() {
      if (!this.previewItem) {
        return '';
      }
      let body = this.previewItem.body || '<p class="empty">暂无正文内容。</p>';
      if (this.previewItem.contentType === 'plain' || this.previewItem.contentType === 'markdown') {
        body = `<pre>${this.escapePreviewHtml(body)}</pre>`;
      } else {
        body = body.replace(/{{[\s\S]*?}}/g, '');
      }
      const previewStyle = 'body{font-family:Inter,Arial,sans-serif;color:#363636;'
        + 'font-size:15px;line-height:1.7;padding:20px;margin:0}'
        + 'pre{white-space:pre-wrap;font:inherit}.empty{color:#999}';
      return `<!doctype html><html><head><meta charset="utf-8"><style>${previewStyle}`
        + `</style></head><body>${body}</body></html>`;
    },

    organizationMemberTotal() {
      return this.organizations.reduce((total, organization) => total + (Number(organization.memberCount) || 0), 0);
    },

    personalResourceTotal() {
      return this.personalLists.length + this.personalTemplates.length
        + this.personalCampaigns.length + this.personalMedia.length;
    },

    selectedResourceTotal() {
      return this.personalListIDs.length + this.personalTemplateIDs.length
        + this.personalCampaignIDs.length + this.personalMediaIDs.length;
    },
  },

  methods: {
    async refresh() {
      const organizations = await this.$api.getMyOrganizations();
      this.$store.commit('setOrganizations', organizations);
      this.setMigrationTarget(organizations);
      if (organizations.length) {
        await this.refreshPersonalResources();
      } else {
        this.clearPersonalResources();
      }
    },

    setMigrationTarget(organizations) {
      const selectedID = Number(this.migrationOrganizationID) || 0;
      if (organizations.some((organization) => organization.id === selectedID)) {
        return;
      }
      const activeID = Number(this.workspace.organizationId) || 0;
      const activeOrganization = organizations.find((organization) => organization.id === activeID);
      this.migrationOrganizationID = activeOrganization ? activeOrganization.id : (organizations[0] || {}).id || null;
    },

    async refreshPersonalResources() {
      const [lists, templates, campaigns, media] = await Promise.all([
        this.$api.getPersonalLists(),
        this.$api.getPersonalTemplates(),
        this.$api.getPersonalCampaigns(),
        this.$api.getPersonalMedia(),
      ]);
      this.personalLists = this.personalPrivateResources(lists.results);
      this.personalTemplates = this.personalPrivateResources(templates);
      this.personalCampaigns = this.personalPrivateResources(campaigns.results);
      this.personalMedia = this.personalPrivateResources(media.results);
    },

    clearPersonalResources() {
      this.personalLists = [];
      this.personalListIDs = [];
      this.personalTemplates = [];
      this.personalTemplateIDs = [];
      this.personalCampaigns = [];
      this.personalCampaignIDs = [];
      this.personalMedia = [];
      this.personalMediaIDs = [];
    },

    personalPrivateResources(resources) {
      return (Array.isArray(resources) ? resources : []).filter((resource) => resource.visibility === 'private');
    },

    roleLabel(role) {
      return role === 'manager' ? '管理员' : '普通成员';
    },

    isActiveOrganization(organization) {
      return Number(this.workspace.organizationId) === Number(organization.id);
    },

    switchWorkspace(organization) {
      this.$store.commit('setWorkspace', organization);
      this.$store.commit('resetWorkspaceModels');
      this.$router.go(0);
    },

    leaveOrganization(organization) {
      this.$utils.confirm(`离开“${organization.name}”后，该组织中属于你的资源会进入待转移状态。`, async () => {
        await this.$api.leaveOrganization(organization.id);
        if (this.isActiveOrganization(organization)) {
          this.$store.commit('setWorkspace', { organizationId: 0, personal: true });
          this.$store.commit('resetWorkspaceModels');
          this.$router.go(0);
          return;
        }
        await this.refresh();
      });
    },

    canMigrate(ids) {
      return Boolean(this.migrationOrganizationID) && ids.length > 0;
    },

    previewResource(resource, ids, resources) {
      const selectedID = ids[0];
      const item = resources.find((candidate) => Number(candidate.id) === Number(selectedID));
      if (!item) {
        return;
      }
      this.previewResourceType = resource;
      this.previewItem = item;
      this.isPreviewVisible = true;
    },

    closePreview() {
      this.isPreviewVisible = false;
      this.previewItem = null;
      this.previewResourceType = '';
    },

    escapePreviewHtml(value) {
      return String(value)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#039;');
    },

    migrateLists(mode) {
      const listIDs = [...this.personalListIDs];
      const action = mode === 'move' ? '移动' : '复制';
      this.$utils.confirm(`确认${action}所选个人列表？`, async () => {
        await this.$api.migratePersonalLists({
          list_ids: listIDs,
          mode,
          target_organization_id: this.migrationOrganizationID,
        });
        this.personalListIDs = [];
        await this.refresh();
        this.$root.$emit('page.refresh');
      });
    },

    migrateResource(resource, selectedIDs, mode) {
      const ids = [...selectedIDs];
      const labels = {
        templates: '邮件模板',
        campaigns: '营销活动',
        media: '媒体文件',
      };
      const selectedKey = {
        templates: 'personalTemplateIDs',
        campaigns: 'personalCampaignIDs',
        media: 'personalMediaIDs',
      }[resource];
      const action = mode === 'move' ? '移动' : '复制';
      this.$utils.confirm(`确认${action}所选${labels[resource]}？`, async () => {
        await this.$api.migratePersonalResources({
          resource,
          ids,
          mode,
          target_organization_id: this.migrationOrganizationID,
        });
        this[selectedKey] = [];
        await this.refresh();
        this.$root.$emit('page.refresh');
      });
    },
  },

  created() {
    this.$root.$on('page.refresh', this.refresh);
  },

  destroyed() {
    this.$root.$off('page.refresh', this.refresh);
  },

  mounted() {
    this.refresh();
  },
});
</script>

<style lang="scss">
.org-page {
  --org-blue: #0055d4;
  --org-ink: #363636;
  --org-muted: #7a7a7a;
  --org-border: #e6e6e6;
  --org-surface: #ffffff;
  background: #fff;
  max-width: none;
  min-height: auto;
  margin: 0;
  padding: 0 0 48px;
  color: var(--org-ink);

  .title {
    color: var(--org-ink);
    margin-bottom: 6px;
  }

  .org-page-header {
    align-items: flex-end;
    display: flex;
    justify-content: space-between;
    margin: 4px 0 24px;
  }

  .org-eyebrow,
  .step-kicker {
    color: var(--org-blue);
    font-size: 11px;
    font-weight: 600;
    letter-spacing: .04em;
    margin-bottom: 7px;
  }

  .org-subtitle {
    color: var(--org-muted);
    font-size: 14px;
    margin: 0;
  }

  .org-header-meta {
    border-left: 1px solid var(--org-border);
    display: flex;
    flex-direction: column;
    gap: 3px;
    padding-left: 18px;
    min-width: 160px;
    text-align: left;
  }

  .org-meta-label,
  .org-stat-label {
    color: var(--org-muted);
    font-size: 12px;
  }

  .org-overview {
    display: grid;
    gap: 12px;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    margin-bottom: 22px;
  }

  .org-stat {
    align-items: center;
    background: var(--org-surface);
    border: 1px solid var(--org-border);
    border-radius: 3px;
    display: flex;
    gap: 13px;
    min-height: 82px;
    padding: 16px 18px;

    strong {
      display: block;
      font-size: 22px;
      line-height: 1.15;
      margin-top: 4px;
    }
  }

  .org-stat-icon {
    align-items: center;
    background: #f4f6f8;
    border-radius: 3px;
    display: inline-flex;
    height: 40px;
    justify-content: center;
    width: 40px;

    &.is-blue,
    &.is-green,
    &.is-orange { color: var(--org-blue); }
  }

  .org-panel,
  .migration-panel {
    background: var(--org-surface);
    border: 1px solid var(--org-border);
    border-radius: 3px;
    box-shadow: 2px 2px 0 #f3f3f3;
    margin-bottom: 22px;
    padding: 22px 24px;
  }

  .org-panel-heading {
    align-items: flex-start;
    display: flex;
    justify-content: space-between;
    margin-bottom: 18px;

    h2 {
      color: var(--org-ink);
      font-size: 17px;
      font-weight: 600;
      margin: 0 0 5px;
    }

    p {
      color: var(--org-muted);
      font-size: 13px;
      margin: 0;
    }
  }

  .org-table {
    margin: 0 -8px;

    table {
      min-width: 700px;
    }

    thead th {
      background: #fafafa;
      color: #4a4a4a;
      font-size: 12px;
      font-weight: 600;
      padding: 11px 12px;
    }

    tbody td {
      color: #39485a;
      padding: 14px 12px;
      vertical-align: middle;
    }

    tbody tr:hover { background: #fafafa; }
  }

  .org-name-cell {
    align-items: center;
    display: flex;
    gap: 10px;

    strong { color: var(--org-ink); font-weight: 600; }
    p { color: var(--org-muted); font-size: 12px; margin: 3px 0 0; }
  }

  .org-avatar {
    align-items: center;
    background: #f4f6f8;
    border-radius: 3px;
    color: var(--org-blue);
    display: inline-flex;
    flex: 0 0 34px;
    height: 34px;
    justify-content: center;
    width: 34px;
  }

  .member-count { align-items: center; display: inline-flex; gap: 5px; }
  .member-count .icon { color: #98a2b3; }

  .status-current { align-items: center; color: #159957; display: inline-flex; font-size: 13px; gap: 6px; }
  .status-current i { background: #20b26b; border-radius: 50%; display: inline-block; height: 6px; width: 6px; }

  .org-actions { align-items: center; display: inline-flex; gap: 5px; }
  .org-actions .button { font-weight: 500; }
  .org-actions .leave-action { color: #8490a0; }

  .org-empty {
    align-items: center;
    color: var(--org-muted);
    display: flex;
    flex-direction: column;
    gap: 7px;
    padding: 28px 0;

    .icon { color: #b6c1cf; }
    strong { color: #4b5868; font-size: 14px; }
    span { font-size: 12px; }
  }

  .migration-panel { background: #fff; }

  .migration-heading {
    align-items: center;
    margin-bottom: 20px;
  }

  .migration-summary {
    align-items: baseline;
    background: #f6f9ff;
    border: 1px solid #dce8f8;
    border-radius: 3px;
    color: #7a7a7a;
    display: inline-flex;
    font-size: 12px;
    gap: 5px;
    padding: 8px 11px;

    strong { color: var(--org-blue); font-size: 18px; }
  }

  .migration-target {
    align-items: center;
    background: #fff;
    border: 1px solid #e6e6e6;
    border-radius: 3px;
    display: flex;
    gap: 13px;
    margin-bottom: 22px;
    padding: 13px 16px;
  }

  .step-number {
    align-items: center;
    background: var(--org-blue);
    border-radius: 50%;
    color: #fff;
    display: inline-flex;
    flex: 0 0 24px;
    font-size: 12px;
    font-weight: 600;
    height: 24px;
    justify-content: center;
    width: 24px;
  }

  .target-copy {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 190px;
    strong { font-size: 14px; }
    span { color: var(--org-muted); font-size: 12px; }
  }

  .target-field {
    margin: 0 0 0 auto;
    max-width: 360px;
    width: 44%;
  }

  .resource-step-label {
    align-items: flex-start;
    display: flex;
    gap: 10px;
    margin: 0 0 12px;

    > div { display: flex; flex-direction: column; gap: 3px; }
    strong { font-size: 14px; }
    span:not(.step-number) { color: var(--org-muted); font-size: 12px; }
  }

  .resource-grid {
    display: grid;
    gap: 12px;
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .resource-card {
    background: #fff;
    border: 1px solid var(--org-border);
    border-radius: 3px;
    min-width: 0;
    padding: 16px;
  }

  .resource-card-top {
    align-items: center;
    display: flex;
    gap: 10px;
    margin-bottom: 13px;

    h3 { color: var(--org-ink); font-size: 15px; font-weight: 600; margin: 0 0 3px; }
    span:not(.resource-icon) { color: var(--org-muted); font-size: 12px; }
  }

  .resource-preview-button {
    color: var(--org-blue);
    margin-left: auto;
    padding-left: 7px;
    padding-right: 7px;
  }

  .resource-icon {
    align-items: center;
    background: #f4f6f8;
    border-radius: 3px;
    color: var(--org-blue);
    display: inline-flex;
    flex: 0 0 34px;
    height: 34px;
    justify-content: center;
    width: 34px;
  }

  .resource-list-card .resource-icon,
  .resource-template-card .resource-icon,
  .resource-campaign-card .resource-icon,
  .resource-media-card .resource-icon { background: #f4f6f8; color: var(--org-blue); }

  .resource-field { margin: 0; }
  .resource-field .select,
  .resource-field .select select { width: 100%; }
  .resource-field .select select {
    font-size: 16px;
    line-height: 1.55;
    min-height: 132px;
    padding: 10px 28px 10px 12px;
  }
  .resource-field .select select[multiple] { height: 132px; }
  .resource-field .select select option { font-size: 16px; padding: 5px 6px; }
  .resource-field .select select:disabled { background: #fafafa; color: #aaa; }

  .resource-card-footer {
    align-items: center;
    border-top: 1px solid #f0f2f5;
    display: flex;
    justify-content: space-between;
    margin-top: 12px;
    padding-top: 12px;

    > span { color: var(--org-muted); font-size: 12px; }
    .buttons { margin: 0; }
    .button { margin-bottom: 0; }
  }

  .migration-tip {
    align-items: center;
    color: #78879b;
    display: flex;
    font-size: 12px;
    gap: 6px;
    margin-top: 16px;
    padding-left: 34px;
  }

  .resource-preview-modal {
    background: #fff;
    border: 1px solid var(--org-border);
    border-radius: 3px;
    box-shadow: 2px 2px 0 #f3f3f3;
    margin: 0 auto;
    max-width: 760px;
    overflow: hidden;
  }

  .resource-preview-header {
    align-items: center;
    border-bottom: 1px solid var(--org-border);
    display: flex;
    justify-content: space-between;
    padding: 18px 22px;
  }

  .resource-preview-title { align-items: center; display: flex; gap: 11px; }
  .resource-preview-title h2 { color: var(--org-ink); font-size: 17px; font-weight: 600; margin: 0; max-width: 560px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .preview-kicker { color: var(--org-muted); display: block; font-size: 11px; margin-bottom: 3px; }

  .resource-preview-body { padding: 22px; }
  .preview-facts { display: grid; gap: 1px; grid-template-columns: repeat(3, 1fr); margin-bottom: 22px; }
  .preview-facts > div { background: #fafafa; border: 1px solid #efefef; padding: 13px 14px; }
  .preview-facts span { color: var(--org-muted); display: block; font-size: 12px; margin-bottom: 4px; }
  .preview-facts strong { color: var(--org-ink); font-size: 16px; font-weight: 600; }

  .preview-copy { color: #4a4a4a; font-size: 14px; line-height: 1.7; }
  .preview-copy p { margin: 5px 0 16px; white-space: pre-wrap; word-break: break-word; }
  .preview-label { color: var(--org-muted); display: block; font-size: 12px; }
  .preview-frame-wrap { border: 1px solid var(--org-border); height: 330px; margin-top: 18px; overflow: hidden; }
  .preview-frame-wrap iframe { border: 0; height: 100%; width: 100%; }
  .media-preview-image { background: #fafafa; border: 1px solid var(--org-border); margin-bottom: 18px; max-height: 260px; padding: 10px; text-align: center; }
  .media-preview-image img { max-height: 238px; max-width: 100%; object-fit: contain; }
  .resource-preview-footer { border-top: 1px solid var(--org-border); display: flex; justify-content: flex-end; padding: 12px 22px; }

  @media screen and (max-width: 800px) {
    margin: 0;
    padding: 0 0 40px;
    .org-page-header { align-items: flex-start; flex-direction: column; gap: 15px; }
    .org-header-meta { border-left: 0; border-top: 1px solid var(--org-border); padding: 10px 0 0; width: 100%; }
    .org-overview { grid-template-columns: 1fr; }
    .org-panel, .migration-panel { padding: 18px 14px; }
    .migration-target { align-items: flex-start; flex-wrap: wrap; }
    .target-copy { flex: 1; }
    .target-field { margin-left: 37px; max-width: none; width: calc(100% - 37px); }
    .resource-grid { grid-template-columns: 1fr; }
    .migration-summary { margin-top: 8px; }
    .migration-heading { flex-direction: column; }
    .resource-preview-button { margin-left: auto; }
    .resource-preview-title h2 { max-width: 240px; }
    .resource-preview-body { padding: 16px; }
    .preview-facts { grid-template-columns: 1fr; }
  }
}
</style>
