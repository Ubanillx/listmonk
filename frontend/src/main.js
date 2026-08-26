import Vue from 'vue';
import Buefy from 'buefy';
import VueI18n from 'vue-i18n';

import App from './App.vue';
import router from './router';
import store from './store';
import * as api from './api';
import Utils from './utils';

// Internationalisation.
Vue.use(VueI18n);
const i18n = new VueI18n();

Vue.use(Buefy, {});
Vue.config.productionTip = false;

// Setup the router.
router.beforeEach((to, from, next) => {
  if (to.matched.length === 0) {
    next('/404');
  } else {
    next();
  }
});

router.afterEach((to) => {
  Vue.nextTick(() => {
    const t = to.meta.title && i18n.te(to.meta.title) ? `${i18n.tc(to.meta.title, 0)} /` : '';
    document.title = `${t} listmonk`;
  });
});

async function initConfig(app) {
  // Load logged in user profile, server side config, and the language file before mounting the app.
  const [profile, cfg, organizations] = await Promise.all([
    api.getUserProfile(),
    api.getServerConfig(),
    api.getMyOrganizations(),
  ]);

  store.commit('setOrganizations', organizations);
  const storedOrganizationID = Number(store.state.workspace.organizationId) || 0;
  const savedOrganization = organizations.find((organization) => organization.id === storedOrganizationID);
  store.commit('setWorkspace', savedOrganization || { organizationId: 0, personal: true });
  let workspace;
  try {
    workspace = await api.getCurrentWorkspace({ disableToast: true });
  } catch (err) {
    // A manager can remove a member while that member still has the former
    // organization persisted in localStorage. Fall back to personal space so
    // revoking organization access never prevents access to personal data.
    store.commit('setWorkspace', { organizationId: 0, personal: true });
    workspace = await api.getCurrentWorkspace();
  }
  store.commit('setWorkspace', workspace);

  const lang = await api.getLang(cfg.lang);
  i18n.locale = cfg.lang;
  i18n.setLocaleMessage(i18n.locale, lang);

  Vue.prototype.$utils = new Utils(i18n);
  Vue.prototype.$api = api;
  Vue.prototype.$events = app;

  // $can('permission:name') is used in the UI to check whether the logged in user
  // has a certain permission to toggle visibility of UI objects and UI functionality.
  Vue.prototype.$can = (...perms) => {
    if (profile.userRole.id === 1) {
      return true;
    }

    // If the perm ends with a wildcard, check whether at least one permission
    // in the group is present. Eg: campaigns:* will return true if at least
    // one of campaigns:get, campaigns:manage etc. are present.
    return perms.some((perm) => {
      if (perm.endsWith('*')) {
        const group = `${perm.split(':')[0]}:`;
        return profile.userRole.permissions.some((p) => p.startsWith(group));
      }

      return profile.userRole.permissions.includes(perm);
    });
  };

  Vue.prototype.$canList = (id, perm) => {
    if (profile.userRole.id === 1) {
      return true;
    }

    // If the user role has global list permissions, return true.
    const can = Vue.prototype.$can('lists:get_all', 'lists:manage_all');
    if (can) {
      return true;
    }

    return profile.listRole.lists.some((list) => list.id === id && list.permissions.includes(perm));
  };

  // Resource rows include their owner and organization. This mirrors the
  // server's write rule so organization managers see member resources as
  // read-only rather than discovering the restriction only after a mutation.
  Vue.prototype.$canManageResource = (resource) => {
    if (!resource) {
      return false;
    }
    if (profile.userRole.id === 1) {
      return true;
    }
    const ownerID = Number(resource.ownerUserId || resource.owner_user_id) || 0;
    const resourceOrganizationID = Number(resource.organizationId || resource.organization_id) || 0;
    const activeOrganizationID = Number(store.state.workspace.organizationId) || 0;
    return ownerID === profile.id
      && resourceOrganizationID === activeOrganizationID
      && !resource.transferPendingAt
      && !resource.transfer_pending_at;
  };

  // All active workspace members can create resources for themselves. The
  // API performs the authoritative membership and archive checks; this keeps
  // the UI from hiding normal member workflows behind legacy global roles.
  Vue.prototype.$canCreateWorkspaceResource = () => {
    const activeWorkspace = store.state.workspace || {};
    return !activeWorkspace.archived;
  };

  // A resource can be used in a campaign or template without necessarily
  // being editable. This matters for organization-shared and global media,
  // where a member may select the resource but cannot modify its source row.
  Vue.prototype.$canUseResource = (resource) => {
    if (!resource || resource.transferPendingAt || resource.transfer_pending_at) {
      return false;
    }
    if (profile.userRole.id === 1) {
      return true;
    }
    const ownerID = Number(resource.ownerUserId || resource.owner_user_id) || 0;
    const resourceOrganizationID = Number(resource.organizationId || resource.organization_id) || 0;
    const activeOrganizationID = Number(store.state.workspace.organizationId) || 0;
    if (resource.visibility === 'global') {
      return true;
    }
    if (resourceOrganizationID !== activeOrganizationID) {
      return false;
    }
    if (ownerID === profile.id) {
      return true;
    }
    return resourceOrganizationID > 0 && resource.visibility === 'organization';
  };

  // Organization managers receive a deliberately narrow, read-only view of
  // member resources in the active organization. The API remains the source
  // of truth, but this keeps navigation and analytics affordances aligned
  // with that server-side policy.
  Vue.prototype.$canInspectOrganization = () => {
    const activeWorkspace = store.state.workspace || {};
    return Number(activeWorkspace.organizationId) > 0
      && (profile.userRole.id === 1 || activeWorkspace.role === 'manager');
  };

  // Set the page title after i18n has loaded.
  const to = router.history.current;
  const title = to.meta.title ? `${i18n.tc(to.meta.title, 0)} /` : '';
  document.title = `${title} listmonk`;

  if (app) {
    app.$mount('#app');
  }
}

const v = new Vue({
  router,
  store,
  i18n,
  render: (h) => h(App),

  data: {
    isLoaded: false,
  },

  methods: {
    loadConfig() {
      initConfig();
    },

    // awaitRestart handles app restart polling after settings changes.
    // Shows a toast and polls until the backend is back up.
    // Returns a promise that resolves with { needsRestart: boolean }.
    awaitRestart(response) {
      return new Promise((resolve) => {
        // If there are running campaigns, app won't auto restart.
        if (response && typeof response === 'object' && response.needsRestart) {
          this.loadConfig();
          resolve({ needsRestart: true });
          return;
        }

        Vue.prototype.$utils.toast(i18n.t('settings.messengers.messageSaved'));

        // Poll until backend is back up.
        const pollId = setInterval(() => {
          api.getHealth().then(() => {
            clearInterval(pollId);
            this.loadConfig();
            resolve({ needsRestart: false });
          });
        }, 1000);
      });
    },
  },

  mounted() {
    v.isLoaded = true;
  },
});

initConfig(v);
