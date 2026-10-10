<template>
  <div id="app">
    <b-navbar :fixed-top="true" v-if="$root.isLoaded">
      <template #brand>
        <div class="logo">
          <router-link :to="{ name: 'dashboard' }">
            <img class="full" src="@/assets/logo.svg" alt="" />
            <img class="favicon" src="@/assets/favicon.png" alt="" />
          </router-link>
        </div>
      </template>
      <template #end>
        <navigation v-if="isMobile" :is-mobile="isMobile" :active-item="activeItem" :active-group="activeGroup"
          @toggleGroup="toggleGroup" @doLogout="doLogout" />

        <b-navbar-dropdown class="workspace" tag="div" right data-cy="workspace-switcher">
          <template #label>
            <b-icon :icon="workspaceIcon" size="is-small" />
            <span class="workspace-label">{{ workspaceLabel }}</span>
          </template>
          <b-navbar-item v-if="canPersonalWorkspace" tag="a" href="#" @click.prevent="switchWorkspace({ organizationId: 0, personal: true })">
            <b-icon icon="account-circle-outline" />
            <span>{{ $t('organizations.personalSpace') }}</span>
          </b-navbar-item>
          <b-navbar-item v-for="organization in organizations" :key="organization.id" tag="a" href="#"
            :data-cy="`workspace-organization-${organization.id}`"
            @click.prevent="switchWorkspace(organization)">
            <b-icon icon="office-building-outline" />
            <span>{{ organization.name }}</span>
          </b-navbar-item>
          <b-navbar-item v-if="organizationDirectoryError" tag="a" href="#" data-cy="workspace-directory-retry"
            @click.prevent="retryOrganizationDirectory">
            <b-icon icon="refresh" />
            <span>{{ $t('organizations.directoryRetry') }}</span>
          </b-navbar-item>
          <b-navbar-item tag="router-link" to="/organizations/mine">
            <b-icon icon="account-group-outline" />
            <span>{{ $t('organizations.title') }}</span>
          </b-navbar-item>
        </b-navbar-dropdown>

        <b-navbar-item tag="a" href="#" @click.prevent="emitPageRefresh" data-cy="btn-refresh"
          :aria-label="$t('globals.buttons.refresh')">
          <b-tooltip :label="$t('globals.buttons.refresh')" type="is-dark" position="is-bottom">
            <b-icon icon="refresh" /> <span class="is-hidden-tablet">{{ $t('globals.buttons.refresh') }}</span>
          </b-tooltip>
        </b-navbar-item>

        <b-navbar-dropdown class="user" tag="div" right>
          <template v-if="profile.username" #label>
            <span class="user-avatar">
              <img v-if="profile.avatar" :src="profile.avatar" alt="" />
              <span v-else>{{ profile.username[0].toUpperCase() }}</span>
            </span>
          </template>

          <b-navbar-item class="user-name" tag="router-link" to="/user/profile">
            <strong>{{ profile.username }}</strong>
            <div class="is-size-7">{{ profile.name }}</div>
          </b-navbar-item>

          <b-navbar-item tag="router-link" to="/user/profile">
            <b-icon icon="account-outline" /> {{ $t('users.profile') }}
          </b-navbar-item>
          <b-navbar-item tag="a" href="#" @click.prevent="confirmLogout">
            <b-icon icon="logout-variant" /> {{ $t('users.logout') }}
          </b-navbar-item>
        </b-navbar-dropdown>
      </template>
    </b-navbar>

    <div class="wrapper" v-if="$root.isLoaded">
      <section class="sidebar">
        <b-sidebar position="static" mobile="hide" :fullheight="true" :open="true" :can-cancel="false">
          <div>
            <b-menu :accordion="false">
              <navigation v-if="!isMobile" :is-mobile="isMobile" :active-item="activeItem" :active-group="activeGroup"
                @toggleGroup="toggleGroup" />
            </b-menu>
          </div>
        </b-sidebar>
      </section>
      <!-- sidebar-->

      <!-- body //-->
      <div class="main">
        <div class="global-notices" v-if="isGlobalNotices">
          <div v-if="serverConfig.needs_restart" class="notification is-danger">
            {{ $t('settings.needsRestart') }}
            &mdash;
            <b-button class="is-primary" size="is-small"
              @click="$utils.confirm($t('settings.confirmRestart'), reloadApp)">
              {{ $t('settings.restart') }}
            </b-button>
          </div>

          <template v-if="serverConfig.update">
            <div v-if="serverConfig.update.update.is_new" class="notification is-success">
              {{ $t('settings.updateAvailable', {
                version: `${serverConfig.update.update.release_version}
              (${$utils.niceDate(serverConfig.update.update.release_date)})`,
              }) }}
              <a :href="serverConfig.update.update.url" target="_blank" rel="noopener noreferer">View</a>
            </div>

            <template v-if="serverConfig.update.messages && serverConfig.update.messages.length > 0">
              <div v-for="m in serverConfig.update.messages" class="notification"
                :class="{ [m.priority === 'high' ? 'is-danger' : 'is-info']: true }" :key="m.title">
                <h3 class="is-size-5" v-if="m.title"><strong>{{ m.title }}</strong></h3>
                <p v-if="m.description">{{ m.description }}</p>
                <a v-if="m.url" :href="m.url" target="_blank" rel="noopener noreferer">View</a>
              </div>
            </template>
          </template>

          <div v-if="serverConfig.has_legacy_user" class="notification is-danger">
            <b-icon icon="warning-empty" />
            Remove the <code>admin_username</code> and <code>admin_password</code> fields from the TOML
            configuration file or environment variables. If you are using APIs, create and use new API credentials
            before removing them. Visit
            <router-link :to="{ name: 'users' }">
              Admin -> Settings -> Users
            </router-link> dashboard.
          </div>
        </div>

        <router-view :key="routeViewKey" />
      </div>
    </div>

    <section v-if="!$root.isLoaded && $root.initializationError" class="workspace-bootstrap-error box"
      role="alert" data-cy="workspace-initialization-error">
      <h1 class="title is-4">{{ $te('organizations.initializationFailed') ? $t('organizations.initializationFailed') : 'Unable to load workspace' }}</h1>
      <p>{{ $te('organizations.initializationRetryHelp') ? $t('organizations.initializationRetryHelp') : 'Please check your connection and retry.' }}</p>
      <b-button type="is-primary" data-cy="workspace-initialization-retry" @click="$root.initialize()">
        {{ $te('globals.buttons.retry') ? $t('globals.buttons.retry') : 'Retry' }}
      </b-button>
    </section>
    <b-loading v-if="!$root.isLoaded && !$root.initializationError" active />
  </div>
</template>

<script>
import Vue from 'vue';
import { mapState } from 'vuex';
import { uris } from './constants';

import Navigation from './components/Navigation.vue';

export default Vue.extend({
  name: 'App',

  components: {
    Navigation,
  },

  data() {
    return {
      activeItem: {},
      activeGroup: {},
      windowWidth: window.innerWidth,
    };
  },

  watch: {
    '$root.isLoaded': {
      immediate: true,
      handler(ready) {
        if (ready) this.loadAppData();
      },
    },
    $route: {
      immediate: true,
      handler(to) {
        // Keep the matching menu entry selected, including on a direct URL load.
        this.activeItem = { [to.name]: true };
        this.activeGroup = to.meta.group ? { [to.meta.group]: true } : {};
      },
    },
  },

  methods: {
    loadAppData() {
      this.$api.getLists({ minimal: true, per_page: 'all', status: 'active' });
      // The log stream follows the same settings:get boundary as the logs page.
      if (this.$can('settings:get')) this.listenEvents();
    },

    retryOrganizationDirectory() {
      // Failed refreshes remain visible in the switcher, without clearing the
      // last valid list or resetting the active workspace.
      return this.$api.refreshOrganizationDirectory().catch(() => {});
    },

    toggleGroup(group, state) {
      this.activeGroup = state ? { [group]: true } : {};
    },

    emitPageRefresh() {
      // Views opt in through `meta.refreshable`. Without that hint the button
      // used to do nothing at all on pages that have no data to reload, which
      // reads as a broken control.
      if (!this.$route.meta || !this.$route.meta.refreshable) {
        this.$utils.toast(this.$t('globals.messages.nothingToRefresh'), 'is-info');
        return;
      }

      this.$root.$emit('page.refresh');
    },

    reloadApp() {
      this.$api.reloadApp().then(() => {
        this.$utils.toast(this.$t('globals.messages.reloading'));

        // Poll until there's a 200 response, waiting for the app
        // to restart and come back up.
        const pollId = setInterval(() => {
          this.$api.getHealth().then(() => {
            clearInterval(pollId);
            document.location.reload();
          });
        }, 500);
      });
    },

    confirmLogout() {
      this.$utils.confirm(this.$t('users.logout'), () => this.doLogout(), null, { type: 'is-danger' });
    },

    doLogout() {
      this.$api.logout().then(() => {
        document.location.href = uris.root;
      });
    },

    switchWorkspace(workspace) {
      const nextOrganizationID = Number(
        workspace.organizationId || workspace.organization_id || workspace.id,
      ) || 0;
      const currentOrganizationID = Number(this.workspace.organizationId) || 0;
      if (nextOrganizationID === currentOrganizationID) {
        return;
      }
      this.$store.commit('setWorkspace', workspace);
      this.$store.commit('resetWorkspaceModels');
      this.$router.go(0);
    },

    listenEvents() {
      const reMatchLog = /(.+?)\.go:\d+:(.+?)$/im;
      const evtSource = new EventSource(uris.errorEvents, { withCredentials: true });
      let numEv = 0;
      evtSource.onmessage = (e) => {
        if (numEv > 50) {
          return;
        }
        numEv += 1;

        const d = JSON.parse(e.data);
        if (d && d.type === 'error') {
          const msg = reMatchLog.exec(d.message.trim());
          if (msg) {
            this.$utils.toast(msg[2], 'is-danger', null, true);
          }
        }
      };
    },
  },

  computed: {
    routeViewKey() {
      if (this.$route.name !== 'organizationManage') return this.$route.fullPath;
      // Changing management tabs must preserve the selected organization and drafts.
      const query = { ...this.$route.query };
      delete query.tab;
      return this.$router.resolve({ path: this.$route.path, query, hash: this.$route.hash }).route.fullPath;
    },
    ...mapState(['serverConfig', 'profile', 'workspace', 'organizations', 'organizationDirectoryError']),

    // The personal workspace is only available to platform administrators and
    // roles carrying the workspaces:personal capability. The server enforces
    // this on every personal-workspace request; the UI mirrors it here so a
    // denied account cannot see an entry that would fail with 403.
    canPersonalWorkspace() {
      const role = this.profile && this.profile.userRole;
      if (!role) {
        return false;
      }
      if (Number(role.id) === 1) {
        return true;
      }
      return (role.permissions || []).includes('workspaces:personal');
    },

    workspaceLabel() {
      return this.workspace.organizationId ? this.workspace.organizationName : this.$t('organizations.personalSpace');
    },

    workspaceIcon() {
      return this.workspace.organizationId ? 'office-building-outline' : 'account-circle-outline';
    },

    isGlobalNotices() {
      return (this.serverConfig.needs_restart
        || this.serverConfig.has_legacy_user
        || (this.serverConfig.update
          && this.serverConfig.update.messages
          && this.serverConfig.update.messages.length > 0));
    },

    version() {
      return import.meta.env.VUE_APP_VERSION;
    },

    isMobile() {
      return this.windowWidth <= 768;
    },
  },

  mounted() {
    window.addEventListener('resize', () => {
      this.windowWidth = window.innerWidth;
    });
  },
});
</script>

<style lang="scss">
@import "assets/style.scss";
</style>
