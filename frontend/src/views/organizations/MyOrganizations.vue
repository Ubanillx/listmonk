<template>
  <section class="organizations">
    <header class="columns page-header">
      <div class="column">
        <h1 class="title is-4"><b-icon icon="office-building-outline" size="is-small" />我参与的组织</h1>
      </div>
    </header>

    <section>
      <b-table :data="organizations" :mobile-cards="false" narrowed>
        <b-table-column v-slot="props" field="name" label="组织">
          <strong>{{ props.row.name }}</strong>
          <p v-if="props.row.description" class="has-text-grey is-size-7">{{ props.row.description }}</p>
        </b-table-column>
        <b-table-column v-slot="props" field="myRole" label="角色">
          <b-tag :type="props.row.myRole === 'manager' ? 'is-primary' : 'is-light'">
            {{ roleLabel(props.row.myRole) }}
          </b-tag>
        </b-table-column>
        <b-table-column v-slot="props" field="memberCount" label="成员数" numeric>
          {{ props.row.memberCount }}
        </b-table-column>
        <b-table-column v-slot="props" label="操作" numeric>
          <b-tag v-if="isActiveOrganization(props.row)" type="is-light">当前工作空间</b-tag>
          <b-button v-else size="is-small" type="is-text" icon-left="login-variant" @click="switchWorkspace(props.row)">
            进入
          </b-button>
          <b-button size="is-small" type="is-text" icon-left="logout-variant" @click="leaveOrganization(props.row)">
            离开
          </b-button>
        </b-table-column>
        <template #empty><span class="has-text-grey">尚未加入组织</span></template>
      </b-table>
    </section>

    <section v-if="organizations.length" class="mt-6">
      <h2 class="title is-5"><b-icon icon="folder-move" size="is-small" />迁移个人资源</h2>
      <div class="columns is-variable is-6">
        <div class="column is-5">
          <b-field label="目标组织" label-position="on-border">
            <b-select v-model.number="migrationOrganizationID" expanded>
              <option :value="null">请选择组织</option>
              <option v-for="organization in organizations" :key="organization.id" :value="organization.id">
                {{ organization.name }}
              </option>
            </b-select>
          </b-field>

          <b-field label="个人列表" label-position="on-border">
            <b-select v-model="personalListIDs" multiple expanded>
              <option v-for="list in personalLists" :key="list.id" :value="list.id">{{ list.name }}</option>
            </b-select>
          </b-field>
          <div class="buttons mb-5">
            <b-button icon-left="content-copy" :disabled="!canMigrate(personalListIDs)" @click="migrateLists('copy')">
              复制
            </b-button>
            <b-button type="is-primary" icon-left="folder-move" :disabled="!canMigrate(personalListIDs)" @click="migrateLists('move')">
              移动
            </b-button>
          </div>
        </div>

        <div class="column is-7">
          <b-field label="个人邮件模板" label-position="on-border">
            <b-select v-model="personalTemplateIDs" multiple expanded>
              <option v-for="template in personalTemplates" :key="template.id" :value="template.id">
                {{ template.name }}
              </option>
            </b-select>
          </b-field>
          <div class="buttons mb-5">
            <b-button icon-left="content-copy" :disabled="!canMigrate(personalTemplateIDs)"
              @click="migrateResource('templates', personalTemplateIDs, 'copy')">
              复制
            </b-button>
            <b-button type="is-primary" icon-left="folder-move" :disabled="!canMigrate(personalTemplateIDs)"
              @click="migrateResource('templates', personalTemplateIDs, 'move')">
              移动
            </b-button>
          </div>

          <b-field label="个人营销活动" label-position="on-border">
            <b-select v-model="personalCampaignIDs" multiple expanded>
              <option v-for="campaign in personalCampaigns" :key="campaign.id" :value="campaign.id">
                {{ campaign.name }} ({{ campaign.status }})
              </option>
            </b-select>
          </b-field>
          <div class="buttons mb-5">
            <b-button icon-left="content-copy" :disabled="!canMigrate(personalCampaignIDs)"
              @click="migrateResource('campaigns', personalCampaignIDs, 'copy')">
              复制
            </b-button>
            <b-button type="is-primary" icon-left="folder-move" :disabled="!canMigrate(personalCampaignIDs)"
              @click="migrateResource('campaigns', personalCampaignIDs, 'move')">
              移动
            </b-button>
          </div>

          <b-field label="个人媒体文件" label-position="on-border">
            <b-select v-model="personalMediaIDs" multiple expanded>
              <option v-for="media in personalMedia" :key="media.id" :value="media.id">{{ media.filename }}</option>
            </b-select>
          </b-field>
          <div class="buttons">
            <b-button icon-left="content-copy" :disabled="!canMigrate(personalMediaIDs)"
              @click="migrateResource('media', personalMediaIDs, 'copy')">
              复制
            </b-button>
            <b-button type="is-primary" icon-left="folder-move" :disabled="!canMigrate(personalMediaIDs)"
              @click="migrateResource('media', personalMediaIDs, 'move')">
              移动
            </b-button>
          </div>
        </div>
      </div>
    </section>
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
    };
  },

  computed: {
    ...mapState(['workspace', 'organizations']),
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
