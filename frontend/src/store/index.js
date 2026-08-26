import Vue from 'vue';
import Vuex from 'vuex';
import { models } from '../constants';

Vue.use(Vuex);

const workspaceStorageKey = 'listmonk.workspace.organizationId';
const workspaceCookieKey = 'listmonk_workspace_organization_id';

function initialWorkspace() {
  const raw = window.localStorage.getItem(workspaceStorageKey);
  const organizationId = Number.parseInt(raw, 10);
  if (Number.isInteger(organizationId) && organizationId > 0) {
    return { organizationId, personal: false };
  }
  return { organizationId: 0, personal: true };
}

export default new Vuex.Store({
  state: {
    // Data from API responses for different models, eg: lists, campaigns.
    // The API responses are stored in this map as-is. This is invoked by
    // API requests in `http`. This initialises lists: {}, campaigns: {}
    // etc. on state.
    ...Object.keys(models).reduce((obj, cur) => ({ ...obj, [cur]: [] }), {}),

    // Map of loading status (true, false) indicators for different model keys
    // like lists, campaigns etc. loading: {lists: true, campaigns: true ...}.
    // The Axios API global request interceptor marks a model as loading=true
    // and the response interceptor marks it as false. The model keys are being
    // pre-initialised here to fix "reactivity" issues on first loads.
    loading: Object.keys(models).reduce((obj, cur) => ({ ...obj, [cur]: false }), {}),

    // The active organization is persisted locally. The server remains the
    // authority: main.js validates the stored ID against active memberships on
    // startup before resource requests are made.
    workspace: initialWorkspace(),
    organizations: [],
  },

  mutations: {
    // Set data from API responses. `model` is 'lists', 'campaigns' etc.
    setModelResponse(state, { model, data }) {
      state[model] = data;
    },

    // Set the loading status for a model globally. When a request starts,
    // status is set to true which is used by the UI to show loaders and block
    // forms. When a response is received, the status is set to false. This is
    // invoked by API requests in `http`.
    setLoading(state, { model, status }) {
      state.loading[model] = status;
    },

    setWorkspace(state, workspace) {
      // Workspace API responses use organization_id, while organization list
      // rows use the regular id/name fields. Normalize both shapes here so
      // selecting an organization from the switcher cannot silently fall
      // back to the personal workspace.
      const organizationId = Number(workspace && (
        workspace.organizationId || workspace.organization_id || workspace.id
      )) || 0;
      state.workspace = {
        ...workspace,
        organizationId,
        organizationName: workspace && (
          workspace.organizationName || workspace.organization_name || workspace.name
        ),
        role: workspace && (workspace.role || workspace.myRole || workspace.my_role),
        personal: organizationId === 0,
      };
      if (organizationId > 0) {
        window.localStorage.setItem(workspaceStorageKey, String(organizationId));
      } else {
        window.localStorage.removeItem(workspaceStorageKey);
      }
      // <img> requests cannot attach the workspace header used by Axios.
      // This is not an authorization token: the backend validates active
      // membership before serving each protected media file.
      document.cookie = `${workspaceCookieKey}=${organizationId}; Path=/; SameSite=Lax`;
    },

    setOrganizations(state, organizations) {
      state.organizations = Array.isArray(organizations) ? organizations : [];
    },

    resetWorkspaceModels(state) {
      Object.keys(models).forEach((model) => {
        state[model] = [];
        state.loading[model] = false;
      });
    },
  },

  getters: {
    [models.lists]: (state) => state[models.lists],
    [models.subscribers]: (state) => state[models.subscribers],
    [models.campaigns]: (state) => state[models.campaigns],
    [models.media]: (state) => state[models.media],
    [models.templates]: (state) => state[models.templates],
    [models.users]: (state) => state[models.users],
    [models.profile]: (state) => state[models.profile],
    [models.userRoles]: (state) => state[models.userRoles],
    [models.listRoles]: (state) => state[models.listRoles],
    [models.settings]: (state) => state[models.settings],
    [models.serverConfig]: (state) => state[models.serverConfig],
    [models.logs]: (state) => state[models.logs],
    workspace: (state) => state.workspace,
    organizations: (state) => state.organizations,
  },

  modules: {
  },
});
