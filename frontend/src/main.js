import Vue from 'vue';
import Buefy from 'buefy';
import VueI18n from 'vue-i18n';
import '@mdi/font/css/materialdesignicons.css';

import App from './App.vue';
import router from './router';
import store from './store';
import * as api from './api';
import Utils from './utils';
import { installA11y } from './a11y';

// Internationalisation.
Vue.use(VueI18n);
const i18n = new VueI18n();

Vue.use(Buefy, {});
Vue.config.productionTip = false;

// Return null until the authenticated profile has been loaded. This keeps the
// initial router transition from denying a manager before memberships arrive,
// while still allowing the route to be checked on every later transition.
function organizationManagerAccess() {
  const { profile } = store.state;
  if (!profile || !profile.userRole) {
    return null;
  }

  if (Number(profile.userRole.id) === 1) {
    return true;
  }

  if ((profile.userRole.permissions || []).includes('organizations:platform_manage')) {
    return true;
  }

  if (!store.state.organizationDirectoryReady) return null;

  const organizations = store.state.organizationMemberships;
  return organizations.some((organization) => organization.myRole === 'manager');
}

// Route-level permission gate for the pages whose menu entry Navigation.vue
// already hides. Returns null until the profile has been loaded so the first
// transition is not denied before permissions arrive; initConfig() re-runs the
// same check once the profile is known.
function routePermissionAccess(route) {
  const perm = route && route.meta && route.meta.permission;
  if (!perm) {
    return true;
  }

  const { profile } = store.state;
  if (!profile || !profile.userRole) {
    return null;
  }

  if (Number(profile.userRole.id) === 1) {
    return true;
  }

  return (profile.userRole.permissions || []).includes(perm);
}

// The first transition can run before initConfig() has resolved the profile
// request. Guards that need the profile wait on this promise instead of letting
// the route through: a direct URL to a page without the permission used to
// render a shell whose every request answered 403.
let resolveProfileReady;
const profileReady = new Promise((resolve) => { resolveProfileReady = resolve; });
// Setup the router.
router.beforeEach((to, from, next) => {
  if (to.matched.length === 0) {
    next('/404');
    return;
  }

  const gated = to.matched.filter((route) => route.meta
    && (route.meta.permission || route.meta.organizationManager));
  const decide = () => {
    if (gated.some((route) => route.meta.permission && routePermissionAccess(route) === false)) {
      // Without the permission this page only produces 403s and a spinner that
      // never resolves; send the user to the explanation instead.
      next({ name: 'forbidden' });
      return;
    }
    if (gated.some((route) => route.meta.organizationManager)
      && organizationManagerAccess() === false) {
      // Keep the management screen out of direct URL access for members and
      // users who do not belong to an organization.
      next({ name: 'organizationMine' });
      return;
    }

    next();
  };

  // Wait for the profile when a gated route cannot be decided yet.
  const undecided = gated.some((route) => routePermissionAccess(route) === null)
    || (gated.some((route) => route.meta.organizationManager) && organizationManagerAccess() === null);
  if (undecided) {
    profileReady.then(decide);
    return;
  }

  decide();
});

router.afterEach((to) => {
  Vue.nextTick(() => {
    const t = to.meta.title && i18n.te(to.meta.title) ? `${i18n.tc(to.meta.title, 0)} /` : '';
    document.title = `${t} ${(store.state.serverConfig || {}).site_name || ''}`.trim();
  });
});

async function initConfig(app) {
  // Load logged in user profile, server side config, and the language file before mounting the app.
  const [profile, cfg] = await Promise.all([
    api.getUserProfile(),
    api.getServerConfig(),
  ]);

  // Load language before resolving workspaces so startup failures can show a
  // localized retry instead of leaving a blank page or a permanent spinner.
  const lang = await api.getLang(cfg.lang);
  i18n.locale = cfg.lang;
  i18n.setLocaleMessage(i18n.locale, lang);

  Vue.prototype.$utils = new Utils(i18n);
  Vue.prototype.$api = api;
  Vue.prototype.$events = app;

  const { organizations } = await api.refreshOrganizationDirectory(profile);

  // No accessible workspace — hand off to the server-rendered selection page,
  // which shows the "contact your administrator" blocker state.
  const redirectToWorkspaceSelection = () => {
    const currentPath = (router.currentRoute && router.currentRoute.fullPath) || '/admin';
    window.location.href = `/admin/select-workspace?next=${encodeURIComponent(currentPath)}`;
  };

  const storedOrganizationID = Number(store.state.workspace.organizationId) || 0;
  const savedOrganization = organizations.find((organization) => organization.id === storedOrganizationID) || null;

  // The personal workspace requires either the super-admin role (id === 1) or
  // the workspaces:personal permission.  Accounts without this capability
  // fall back to the first available organization.
  const canPersonal = Number(profile.userRole && profile.userRole.id) === 1
    || ((profile.userRole && profile.userRole.permissions) || []).includes('workspaces:personal');
  const personal = canPersonal ? { organizationId: 0, personal: true } : null;
  const firstOrg = organizations.length > 0 ? organizations[0] : null;

  let workspace = savedOrganization || personal || firstOrg;
  store.commit('setWorkspace', workspace);

  if (workspace) {
    try {
      workspace = await api.getCurrentWorkspace({ disableToast: true });
    } catch (err) {
      // Connectivity failures do not revoke access. Keep the saved selection
      // intact and let initialization be retried in the same workspace.
      const status = err && err.response && err.response.status;
      if (status !== 403 && status !== 404) throw err;
      // A manager can remove a member, or the personal-space capability may
      // have been revoked while the earlier localStorage snapshot was still
      // valid.  Fall back to the first available space; if none exists the
      // account is blocked.
      workspace = personal || firstOrg;
      if (workspace) {
        store.commit('setWorkspace', workspace);
        workspace = await api.getCurrentWorkspace();
      } else {
        workspace = null;
      }
    }
    if (workspace) {
      store.commit('setWorkspace', workspace);
    } else {
      redirectToWorkspaceSelection();
      return;
    }
  } else {
    // No personal workspace capability and no organizations.
    redirectToWorkspaceSelection();
    return;
  }

  // The first router transition happens before the async profile request. If
  // it landed on the management URL, enforce the now-known membership after
  // initialization as well.
  if (router.currentRoute
    && router.currentRoute.matched.some((route) => route.meta && route.meta.permission
      && routePermissionAccess(route) === false)) {
    await router.replace({ name: 'forbidden' });
  }

  // The first router transition happens before the async profile request. If
  // it landed on the management URL, enforce the now-known membership after
  // initialization as well.
  if (router.currentRoute
    && router.currentRoute.matched.some((route) => route.meta && route.meta.organizationManager)
    && organizationManagerAccess() === false) {
    await router.replace({ name: 'organizationMine' });
  }

  // $can('permission:name') is used in the UI to check whether the logged in user
  // has a certain permission to toggle visibility of UI objects and UI functionality.
  Vue.prototype.$can = (...perms) => {
    if (Number(profile.userRole.id) === 1) {
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

  // Single predicate for the platform administrator role. The role id used to
  // be repeated as a magic number in more than a dozen views, which made any
  // change to the role model a silent, scattered edit.
  Vue.prototype.$isPlatformAdmin = () => Number(profile.userRole && profile.userRole.id) === 1;

  Vue.prototype.$canList = (id, perm) => {
    if (Number(profile.userRole.id) === 1) {
      return true;
    }

    const canManage = perm === 'customer_list:manage';
    if (canManage
      ? Vue.prototype.$can('customer_lists:manage_all')
      : Vue.prototype.$can('customer_lists:get_all', 'customer_lists:manage_all')) {
      return true;
    }

    const customerLists = (profile.customerListRole && profile.customerListRole.customerLists) || [];
    return customerLists.some((customerList) => customerList.id === id && (
      customerList.permissions.includes(perm)
      || (!canManage && customerList.permissions.includes('customer_list:manage'))
    ));
  };

  // Resource rows include their owner and organization. This mirrors the
  // server's write rule so organization managers see member resources as
  // read-only rather than discovering the restriction only after a mutation.
  Vue.prototype.$canManageResource = (resource, ...perms) => {
    if (!resource) {
      return false;
    }
    if (Number(profile.userRole.id) === 1) {
      return true;
    }
    const ownerID = Number(resource.ownerUserId || resource.owner_user_id) || 0;
    const resourceOrganizationID = Number(resource.organizationId || resource.organization_id) || 0;
    const activeOrganizationID = Number(store.state.workspace.organizationId) || 0;
    const ownsResource = ownerID === profile.id
      && resourceOrganizationID === activeOrganizationID
      && !resource.transferPendingAt
      && !resource.transfer_pending_at;
    return ownsResource && (!perms.length || Vue.prototype.$can(...perms));
  };

  // Global template publication is intentionally open to every authenticated
  // user. Its creator can maintain that shared template without receiving the
  // broader templates:manage grant needed for private or organization work.
  Vue.prototype.$canManageTemplate = (template) => (
    Vue.prototype.$canManageResource(template)
    && (template.visibility === 'global' || Vue.prototype.$can('templates:manage'))
  );

  // Recipient rows contain personal data. The server requires an owner-bound
  // campaign plus the dedicated recipient and customer-read capabilities.
  Vue.prototype.$canReadCampaignRecipients = (campaign) => (
    Vue.prototype.$canManageResource(campaign)
    && Vue.prototype.$can('campaigns:get_analytics')
    && Vue.prototype.$can('campaigns:recipients')
    && Vue.prototype.$can('customers:get_all', 'customers:get')
  );

  // Creation and mutation controls mirror the legacy role model as well as
  // the active-workspace archive state. The API remains authoritative, but
  // this prevents an organization membership from making a disabled feature
  // appear actionable in the UI.
  Vue.prototype.$canCreateWorkspaceResource = (...perms) => {
    const activeWorkspace = store.state.workspace || {};
    if (activeWorkspace.archived) {
      return false;
    }
    if (!perms.length) {
      return true;
    }
    return Vue.prototype.$can(...perms);
  };

  // A resource can be used in a campaign or template without necessarily
  // being editable. This matters for organization-shared and global media,
  // where a member may select the resource but cannot modify its source row.
  Vue.prototype.$canUseResource = (resource, ...perms) => {
    if (!resource || resource.transferPendingAt || resource.transfer_pending_at) {
      return false;
    }
    if (Number(profile.userRole.id) === 1) {
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
      return !perms.length || Vue.prototype.$can(...perms);
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
      && (Number(profile.userRole.id) === 1 || activeWorkspace.role === 'manager');
  };

  // Aggregate campaign analytics are narrower than campaign visibility. A
  // public campaign may be opened or copied by any signed-in user, but only
  // its owner, an organization manager in that campaign's organization, or a
  // platform administrator may inspect its statistics. Keep this helper in
  // sync with requireCampaignAnalytics on the server so read-only rows do not
  // render a link that is guaranteed to return 403.
  Vue.prototype.$canViewCampaignAnalytics = (campaign) => {
    if (!campaign) {
      return false;
    }
    if (Number(profile.userRole.id) === 1) {
      return true;
    }

    const activeWorkspace = store.state.workspace || {};
    const activeOrganizationID = Number(activeWorkspace.organizationId) || 0;
    const resourceOrganizationID = Number(campaign.organizationId || campaign.organization_id) || 0;
    const ownerID = Number(campaign.ownerUserId || campaign.owner_user_id) || 0;
    const transferPending = !!(campaign.transferPendingAt || campaign.transfer_pending_at);

    // Managers may inspect aggregate statistics for every campaign in the
    // selected organization, including rows pending transfer. A global
    // campaign from another organization remains outside that grant.
    if (activeOrganizationID > 0
      && activeWorkspace.role === 'manager'
      && resourceOrganizationID === activeOrganizationID) {
      return true;
    }

    // Owners must still be in the matching workspace and the legacy analytics
    // permission remains a narrowing guard. Pending-transfer rows are no
    // longer owned by the departing user and therefore cannot be reported.
    return !transferPending
      && ownerID === profile.id
      && resourceOrganizationID === activeOrganizationID
      && Vue.prototype.$can('campaigns:get_analytics');
  };

  // Set the page title after i18n has loaded.
  const to = router.history.current;
  const title = to.meta.title ? `${i18n.tc(to.meta.title, 0)} /` : '';
  document.title = `${title} ${cfg.site_name || ''}`.trim();

  // Associate Buefy field labels and pagination controls with assistive
  // technology before the first render (see a11y.js).
  installA11y(Vue, i18n);

  // Release the route guards above; the profile is known at this point.
  resolveProfileReady();
}

let configRequest = null;

const v = new Vue({
  router,
  store,
  i18n,
  render: (h) => h(App),

  data: {
    isLoaded: false,
    initializationError: false,
  },

  methods: {
    loadConfig() {
      if (!configRequest) {
        configRequest = initConfig(this).finally(() => { configRequest = null; });
      }
      return configRequest;
    },

    initialize() {
      this.initializationError = false;
      return this.loadConfig().then(() => {
        this.isLoaded = true;
      }).catch((err) => {
        const status = err && err.response && err.response.status;
        if (status === 401 || status === 403) {
          window.location.href = '/admin/login';
          return;
        }
        this.initializationError = true;
      });
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

        // The save responds before the backend shuts down. Wait for that
        // transition, then retry the full configuration load: an early health
        // response alone can still come from the old process.
        const poll = () => {
          api.getHealth().then(() => this.loadConfig()).then(() => {
            resolve({ needsRestart: false });
          }).catch(() => {
            setTimeout(poll, 1000);
          });
        };
        setTimeout(poll, 1500);
      });
    },
  },

});

// Mount only the loading/error shell until profile, directory and workspace
// are validated. Route components cannot initialize from partial state.
v.$mount('#app');
v.initialize();
