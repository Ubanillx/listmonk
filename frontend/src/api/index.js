import { ToastProgrammatic as Toast } from 'buefy';
import axios from 'axios';
import qs from 'qs';
import store from '../store';
import { models } from '../constants';
import Utils from '../utils';

const http = axios.create({
  baseURL: import.meta.env.VUE_APP_ROOT_URL || '/',
  withCredentials: false,
  responseType: 'json',

  // Override the default serializer to switch params from becoming []id=a&[]id=b ...
  // in GET and DELETE requests to id=a&id=b.
  paramsSerializer: (params) => qs.stringify(params, { arrayFormat: 'repeat' }),
});

const utils = new Utils();
const workspaceHeader = 'X-Listmonk-Organization-ID';

// Intercept requests to set the 'loading' state of a model.
http.interceptors.request.use((config) => {
  const requestConfig = { ...config };

  // All authenticated resource requests are scoped by the active workspace.
  // Public endpoints deliberately do not receive the header.
  if (requestConfig.url && requestConfig.url.startsWith('/api/')) {
    const hasExplicitWorkspace = Object.prototype.hasOwnProperty.call(requestConfig, 'workspaceOrganizationId');
    const organizationId = hasExplicitWorkspace
      ? Number(requestConfig.workspaceOrganizationId)
      : Number(store.state.workspace && store.state.workspace.organizationId) || 0;
    if ((hasExplicitWorkspace && Number.isInteger(organizationId) && organizationId >= 0) || organizationId > 0) {
      requestConfig.headers = {
        ...requestConfig.headers,
        [workspaceHeader]: String(organizationId),
      };
    }
  }
  if ('loading' in requestConfig) {
    store.commit('setLoading', { model: requestConfig.loading, status: true });
  }
  return requestConfig;
}, (error) => Promise.reject(error));

// Intercept responses to set them to store.
http.interceptors.response.use((resp) => {
  // Clear the loading state for a model.
  if ('loading' in resp.config) {
    store.commit('setLoading', { model: resp.config.loading, status: false });
  }

  let data = {};
  if (typeof resp.data.data === 'object') {
    if (resp.data.data.constructor === Object) {
      data = { ...resp.data.data };
    } else {
      data = [...resp.data.data];
    }

    // Transform keys to camelCase.
    switch (typeof resp.config.camelCase) {
      case 'function':
        data = utils.camelKeys(data, resp.config.camelCase);
        break;
      case 'boolean':
        if (resp.config.camelCase) {
          data = utils.camelKeys(data);
        }
        break;
      default:
        data = utils.camelKeys(data);
        break;
    }
  } else {
    data = resp.data.data;
  }

  // Store the API response for a model.
  if ('store' in resp.config) {
    store.commit('setModelResponse', { model: resp.config.store, data });
  }

  return data;
}, (err) => {
  // Clear the loading state for a model.
  if ('loading' in err.config) {
    store.commit('setLoading', { model: err.config.loading, status: false });
  }

  let msg = '';
  if (err.response && err.response.data && err.response.data.message) {
    msg = err.response.data.message;
  } else {
    msg = err.toString();
  }

  if (!err.config.disableToast) {
    Toast.open({
      message: msg,
      type: 'is-danger',
      queue: false,
      position: 'is-top',
      pauseOnHover: true,
    });
  }

  return Promise.reject(err);
});

// API calls accept the following config keys.
// loading: modelName (set's the loading status in the global store: eg: store.loading.lists = true)
// store: modelName (set's the API response in the global store. eg: store.lists: { ... } )

// Health check endpoint that does not throw a toast.
export const getHealth = () => http.get(
  '/api/health',
  { disableToast: true },
);

export const reloadApp = () => http.post('/api/admin/reload');

// Dashboard
export const getDashboardCounts = () => http.get(
  '/api/dashboard/counts',
  { loading: models.dashboard },
);

export const getDashboardCharts = () => http.get(
  '/api/dashboard/charts',
  { loading: models.dashboard },
);

// Lists.
export const getLists = (params) => http.get(
  '/api/lists',
  {
    params: (!params ? { per_page: 'all' } : params),
    loading: models.lists,
    store: models.lists,
  },
);

export const queryLists = (params) => http.get(
  '/api/lists',
  {
    params: (!params ? { per_page: 'all' } : params),
    loading: models.listsFull,
  },
);

// Resource migration needs to show the caller's personal resources while an
// organization workspace is active. An explicit zero header takes precedence
// over the browser's workspace cookie without changing the UI selection.
export const getPersonalLists = () => http.get(
  '/api/lists',
  {
    params: { per_page: 'all', status: 'active' },
    workspaceOrganizationId: 0,
  },
);

export const getPersonalTemplates = () => http.get(
  '/api/templates',
  { workspaceOrganizationId: 0 },
);

export const getPersonalCampaigns = () => http.get(
  '/api/campaigns',
  {
    params: { per_page: 'all', no_body: true },
    workspaceOrganizationId: 0,
  },
);

export const getPersonalMedia = () => http.get(
  '/api/media',
  {
    params: { per_page: 'all' },
    workspaceOrganizationId: 0,
  },
);

export const getList = async (id) => http.get(
  `/api/lists/${id}`,
  { loading: models.list },
);

export const createList = (data) => http.post(
  '/api/lists',
  data,
  { loading: models.lists },
);

export const updateList = (data) => http.put(
  `/api/lists/${data.id}`,
  data,
  { loading: models.lists },
);

export const deleteList = (id) => http.delete(
  `/api/lists/${id}`,
  { loading: models.lists },
);

export const deleteLists = (params) => http.delete(
  '/api/lists',
  { params, loading: models.lists },
);

// Organizations and workspaces.
export const getCurrentWorkspace = (config = {}) => http.get('/api/workspace', config);

export const getMyOrganizations = () => http.get('/api/organizations/me');

export const createOrganizationRequest = (data) => http.post('/api/organizations/requests', data);

export const getMyOrganizationRequests = () => http.get('/api/organizations/requests/mine');

export const withdrawOrganizationRequest = (id) => http.delete(`/api/organizations/requests/${id}`);

export const joinOrganization = (data) => http.post('/api/organizations/join', data);

// Management screens select an organization independently of the active
// workspace. Keep calls without an ID backward-compatible with the current
// workspace used elsewhere in the app.
const organizationWorkspaceConfig = (organizationID) => {
  const id = Number(organizationID);
  return Number.isInteger(id) && id >= 0 ? { workspaceOrganizationId: id } : {};
};

export const leaveOrganization = (organizationID) => http.post(
  '/api/organizations/leave',
  {},
  organizationWorkspaceConfig(organizationID),
);

export const getOrganizationMembers = (organizationID) => http.get(
  '/api/organizations/members',
  organizationWorkspaceConfig(organizationID),
);

export const addOrganizationMember = (data, organizationID) => http.post(
  '/api/organizations/members',
  data,
  organizationWorkspaceConfig(organizationID),
);

export const updateOrganizationMember = (userID, data, organizationID) => http.put(
  `/api/organizations/members/${userID}`,
  data,
  organizationWorkspaceConfig(organizationID),
);

export const removeOrganizationMember = (userID, organizationID) => http.delete(
  `/api/organizations/members/${userID}`,
  organizationWorkspaceConfig(organizationID),
);

export const getOrganizationInvites = (organizationID) => http.get(
  '/api/organizations/invites',
  organizationWorkspaceConfig(organizationID),
);

export const createOrganizationInvite = (data, organizationID) => http.post(
  '/api/organizations/invites',
  data,
  organizationWorkspaceConfig(organizationID),
);

export const revokeOrganizationInvite = (id, organizationID) => http.delete(
  `/api/organizations/invites/${id}`,
  organizationWorkspaceConfig(organizationID),
);

export const getReplyForwardRules = (organizationID) => http.get(
  '/api/organizations/reply-forwarding',
  organizationWorkspaceConfig(organizationID),
);

export const updateReplyForwardRule = (id, data, organizationID) => http.put(
  `/api/organizations/reply-forwarding/${id}`,
  data,
  organizationWorkspaceConfig(organizationID),
);

export const deleteReplyForwardRule = (id, organizationID) => http.delete(
  `/api/organizations/reply-forwarding/${id}`,
  organizationWorkspaceConfig(organizationID),
);

export const transferPendingOrganizationResources = (data, organizationID) => http.post(
  '/api/organizations/resources/transfer',
  data,
  organizationWorkspaceConfig(organizationID),
);

export const getOrganizationMembersByID = (id) => http.get(`/api/organizations/${id}/members`);

export const transferArchivedOrganizationResources = (id, data) => http.post(`/api/organizations/${id}/resources/transfer`, data);

export const transferOrganizationTemplate = (id, data) => http.post(`/api/organizations/templates/${id}/transfer`, data);

export const unpublishOrganizationTemplate = (id) => http.post(`/api/organizations/templates/${id}/unpublish`);

export const migratePersonalLists = (data) => http.post('/api/organizations/resources/lists/migrate', data);

export const migratePersonalResources = (data) => http.post('/api/organizations/resources/migrate', data);

export const getOrganizationRequests = () => http.get('/api/organizations/requests');

export const reviewOrganizationRequest = (id, data) => http.put(`/api/organizations/requests/${id}`, data);

export const getOrganizations = (includeArchived = false) => http.get('/api/organizations', {
  params: { include_archived: includeArchived },
});

export const archiveOrganization = (id) => http.post(`/api/organizations/${id}/archive`);

export const purgeArchivedOrganization = (id) => http.delete(`/api/organizations/${id}`);

// Subscribers.
export const getSubscribers = async (params) => http.get(
  '/api/subscribers',
  {
    params,
    loading: models.subscribers,
    store: models.subscribers,
    camelCase: (keyPath) => !keyPath.startsWith('.results.*.attribs'),
  },
);

export const getSubscriber = async (id) => http.get(
  `/api/subscribers/${id}`,
  { loading: models.subscribers },
);

export const getSubscriberActivity = async (id) => http.get(
  `/api/subscribers/${id}/activity`,
  { loading: models.subscribers },
);

export const getSubscriberBounces = async (id) => http.get(
  `/api/subscribers/${id}/bounces`,
  { loading: models.bounces },
);

export const deleteSubscriberBounces = async (id) => http.delete(
  `/api/subscribers/${id}/bounces`,
  { loading: models.bounces },
);

export const deleteBounce = async (id) => http.delete(
  `/api/bounces/${id}`,
  { loading: models.bounces },
);

export const deleteBounces = async (params) => http.delete(
  '/api/bounces',
  { params, loading: models.bounces },
);

export const blocklistBouncedSubscribers = async () => http.put(
  '/api/bounces/blocklist',
  { loading: models.bounces },
);

export const createSubscriber = (data) => http.post(
  '/api/subscribers',
  data,
  { loading: models.subscribers },
);

export const updateSubscriber = (data) => http.put(
  `/api/subscribers/${data.id}`,
  data,
  { loading: models.subscribers },
);

// Subscriber custom field definitions.
export const getCustomFields = async () => http.get('/api/custom-fields', { loading: models.customFields });
export const createCustomField = async (data) => http.post('/api/custom-fields', data, { loading: models.customFields });
export const updateCustomField = async (key, data) => http.put(`/api/custom-fields/${encodeURIComponent(key)}`, data, { loading: models.customFields });
export const deleteCustomField = async (key) => http.delete(`/api/custom-fields/${encodeURIComponent(key)}`, { loading: models.customFields });

export const sendSubscriberOptin = (id) => http.post(
  `/api/subscribers/${id}/optin`,
  {},
  { loading: models.subscribers },
);

export const deleteSubscriber = (id) => http.delete(
  `/api/subscribers/${id}`,
  { loading: models.subscribers },
);

export const addSubscribersToLists = (data) => http.put(
  '/api/subscribers/lists',
  data,
  { loading: models.subscribers },
);

export const addSubscribersToListsByQuery = (data) => http.put(
  '/api/subscribers/query/lists',
  data,

  { loading: models.subscribers },
);

export const blocklistSubscribers = (data) => http.put(
  '/api/subscribers/blocklist',
  data,
  { loading: models.subscribers },
);

export const blocklistSubscribersByQuery = (data) => http.put(
  '/api/subscribers/query/blocklist',
  data,
  { loading: models.subscribers },
);

export const deleteSubscribers = (params) => http.delete(
  '/api/subscribers',
  { params, loading: models.subscribers },
);

export const deleteSubscribersByQuery = (data) => http.post(
  '/api/subscribers/query/delete',
  data,
  { loading: models.subscribers },
);

// Subscriber import.
export const importSubscribers = (data) => http.post('/api/import/subscribers', data);

export const getImportStatus = () => http.get('/api/import/subscribers');

export const getImportLogs = async () => http.get(
  '/api/import/subscribers/logs',
  { camelCase: false },
);

export const stopImport = () => http.delete('/api/import/subscribers');

// Bounces.
export const getBounces = async (params) => http.get(
  '/api/bounces',
  { params, loading: models.bounces },
);

// Campaigns.
export const getCampaigns = async (params) => http.get('/api/campaigns', {
  params,
  loading: models.campaigns,
  store: models.campaigns,
  camelCase: (keyPath) => !keyPath.startsWith('.results.*.headers'),
});

export const getCampaign = async (id) => http.get(`/api/campaigns/${id}`, {
  loading: models.campaigns,
  camelCase: (keyPath) => !keyPath.startsWith('.headers'),
});

export const getCampaignStats = async () => http.get('/api/campaigns/running/stats', {});

export const createCampaign = async (data) => http.post(
  '/api/campaigns',
  data,
  { loading: models.campaigns },
);

export const cloneCampaign = async (id, data) => http.post(
  `/api/campaigns/${id}/clone`,
  data,
  { loading: models.campaigns },
);

export const getCampaignViewCounts = async (params) => http.get(
  '/api/campaigns/analytics/views',
  { params, loading: models.campaigns },
);

export const getCampaignClickCounts = async (params) => http.get(
  '/api/campaigns/analytics/clicks',
  { params, loading: models.campaigns },
);

export const getCampaignBounceCounts = async (params) => http.get(
  '/api/campaigns/analytics/bounces',
  { params, loading: models.campaigns },
);

export const getCampaignLinkCounts = async (params) => http.get(
  '/api/campaigns/analytics/links',
  { params, loading: models.campaigns },
);

export const getCampaignReportSummary = async (id, params) => http.get(
  `/api/campaigns/${id}/report/summary`,
  { params, loading: models.campaigns },
);

export const getCampaignsReportSummary = async (params) => http.get(
  '/api/campaigns/report/summary',
  { params, loading: models.campaigns },
);

export const getCampaignReportSeries = async (id, params) => http.get(
  `/api/campaigns/${id}/report/timeseries`,
  { params, loading: models.campaigns },
);

export const getCampaignsReportSeries = async (params) => http.get(
  '/api/campaigns/report/timeseries',
  { params, loading: models.campaigns },
);

export const getCampaignReportLinks = async (id, params) => http.get(
  `/api/campaigns/${id}/report/links`,
  { params, loading: models.campaigns },
);

export const getCampaignsReportLinks = async (params) => http.get(
  '/api/campaigns/report/links',
  { params, loading: models.campaigns },
);

export const getCampaignReportRecipients = async (id, params) => http.get(
  `/api/campaigns/${id}/report/recipients`,
  { params, loading: models.campaigns },
);

export const getCampaignsReportRecipients = async (params) => http.get(
  '/api/campaigns/report/recipients',
  { params, loading: models.campaigns },
);

export const convertCampaignContent = async (data) => http.post(
  `/api/campaigns/${data.id}/content`,
  data,
  { loading: models.campaigns },
);

export const testCampaign = async (data) => http.post(
  `/api/campaigns/${data.id}/test`,
  data,
  { loading: models.campaigns },
);

export const updateCampaign = async (id, data) => http.put(
  `/api/campaigns/${id}`,
  data,
  { loading: models.campaigns },
);

export const changeCampaignStatus = async (id, status) => http.put(
  `/api/campaigns/${id}/status`,
  { status },

  { loading: models.campaigns },
);

export const updateCampaignArchive = async (id, data) => http.put(
  `/api/campaigns/${id}/archive`,
  data,
  { loading: models.campaigns },
);

export const deleteCampaign = async (id) => http.delete(
  `/api/campaigns/${id}`,
  { loading: models.campaigns },
);

export const deleteCampaigns = (params) => http.delete(
  '/api/campaigns',
  { params, loading: models.campaigns },
);

// Media.
export const getMedia = async (params) => http.get(
  '/api/media',
  { params, loading: models.media, store: models.media },
);

export const uploadMedia = (data) => http.post(
  '/api/media',
  data,
  { loading: models.media },
);

export const deleteMedia = (id) => http.delete(
  `/api/media/${id}`,
  { loading: models.media },
);

// Templates.
export const createTemplate = async (data) => http.post(
  '/api/templates',
  data,
  { loading: models.templates },
);

export const getTemplates = async () => http.get(
  '/api/templates',
  { loading: models.templates, store: models.templates },
);

export const getTemplate = async (id) => http.get(
  `/api/templates/${id}`,
  { loading: models.templates },
);

export const updateTemplate = async (data) => http.put(
  `/api/templates/${data.id}`,
  data,
  { loading: models.templates },
);

export const cloneTemplate = async (id, data) => http.post(
  `/api/templates/${id}/clone`,
  data,
  { loading: models.templates },
);

export const makeTemplateDefault = async (id) => http.put(
  `/api/templates/${id}/default`,
  {},
  { loading: models.templates },
);

export const deleteTemplate = async (id) => http.delete(
  `/api/templates/${id}`,
  { loading: models.templates },
);

// Settings.
export const getServerConfig = async () => http.get(
  '/api/config',
  { loading: models.serverConfig, store: models.serverConfig, camelCase: false },
);

export const getSettings = async () => http.get(
  '/api/settings',
  { loading: models.settings, store: models.settings, camelCase: false },
);

export const updateSettings = async (data) => http.put(
  '/api/settings',
  data,
  { loading: models.settings },
);

export const updateSettingsByKey = async (key, data) => http.put(
  `/api/settings/${key}`,
  data,
  { loading: models.settings },
);

export const testSMTP = async (data) => http.post(
  '/api/settings/smtp/test',
  data,
  { loading: models.settings, disableToast: true },
);

export const getLogs = async () => http.get(
  '/api/logs',
  { loading: models.logs, camelCase: false },
);

export const getLang = async (lang) => http.get(
  `/api/lang/${lang}`,
  { loading: models.lang, camelCase: false },
);

export const logout = async () => http.post('/api/logout');

export const deleteGCCampaignAnalytics = async (typ, beforeDate) => http.delete(
  `/api/maintenance/analytics/${typ}`,
  { loading: models.maintenance, params: { before_date: beforeDate } },
);

export const deleteGCSubscribers = async (typ) => http.delete(
  `/api/maintenance/subscribers/${typ}`,
  { loading: models.maintenance },
);

export const deleteGCSubscriptions = async (beforeDate) => http.delete(
  '/api/maintenance/subscriptions/unconfirmed',
  { loading: models.maintenance, params: { before_date: beforeDate } },
);

// Users.
export const getUsers = () => http.get(
  '/api/users',
  {
    loading: models.users,
    store: models.users,
  },
);

export const queryUsers = () => http.get(
  '/api/users',
  {
    loading: models.users,
    store: models.users,
  },
);

export const getUser = async (id) => http.get(
  `/api/users/${id}`,
  { loading: models.users },
);

export const createUser = (data) => http.post(
  '/api/users',
  data,
  { loading: models.users },
);

export const createUsers = (data) => http.post(
  '/api/users/bulk',
  data,
  { loading: models.users },
);

export const updateUser = (data) => http.put(
  `/api/users/${data.id}`,
  data,
  { loading: models.users },
);

export const deleteUser = (id) => http.delete(
  `/api/users/${id}`,
  { loading: models.users },
);

export const getUserIntegrationTokens = (id) => http.get(
  `/api/users/${id}/integration-tokens`,
  { loading: models.users },
);

export const createUserIntegrationToken = (id, data) => http.post(
  `/api/users/${id}/integration-tokens`,
  data,
  { loading: models.users },
);

export const deleteUserIntegrationToken = (id, tokenID) => http.delete(
  `/api/users/${id}/integration-tokens/${tokenID}`,
  { loading: models.users },
);

export const getUserProfile = () => http.get(
  '/api/profile',
  { loading: models.users, store: models.profile },
);

export const updateUserProfile = (data) => http.put(
  '/api/profile',
  data,
  { loading: models.users, store: models.profile },
);

export const getPersonalSMTP = () => http.get(
  '/api/profile/smtp',
  { loading: models.users },
);

export const updatePersonalSMTP = (data) => http.put(
  '/api/profile/smtp',
  data,
  { loading: models.users },
);

export const deletePersonalSMTP = (id) => http.delete(
  `/api/profile/smtp/${id}`,
  { loading: models.users },
);

export const testPersonalSMTP = (data) => http.post(
  '/api/profile/smtp/test',
  data,
  { loading: models.users, disableToast: true },
);

// Dedicated 263 customer-reply mailboxes. Credentials are accepted only on
// create/update/test and are never returned by the API.
export const getReplyMailboxes = () => http.get(
  '/api/profile/reply-mailboxes',
  { loading: models.users },
);

export const createReplyMailbox = (data) => http.post(
  '/api/profile/reply-mailboxes',
  data,
  { loading: models.users },
);

export const updateReplyMailbox = (id, data) => http.put(
  `/api/profile/reply-mailboxes/${id}`,
  data,
  { loading: models.users },
);

export const deleteReplyMailbox = (id) => http.delete(
  `/api/profile/reply-mailboxes/${id}`,
  { loading: models.users },
);

export const testReplyMailbox = (data) => http.post(
  '/api/profile/reply-mailboxes/test',
  data,
  { loading: models.users, disableToast: true },
);

export const getUserPersonalSMTP = (id) => http.get(
  `/api/users/${id}/smtp`,
  { loading: models.users },
);

export const getUserRoles = async () => http.get(
  '/api/roles/users',
  { loading: models.userRoles, store: models.userRoles },
);

export const getListRoles = async () => http.get(
  '/api/roles/lists',
  { loading: models.listRoles, store: models.listRoles },
);

export const createUserRole = (data) => http.post(
  '/api/roles/users',
  data,
  { loading: models.userRoles },
);

export const createListRole = (data) => http.post(
  '/api/roles/lists',
  data,
  { loading: models.listRoles },
);

export const updateUserRole = (data) => http.put(
  `/api/roles/users/${data.id}`,
  data,
  { loading: models.userRoles },
);

export const updateListRole = (data) => http.put(
  `/api/roles/lists/${data.id}`,
  data,
  { loading: models.userRoles },
);

export const deleteRole = (id) => http.delete(
  `/api/roles/${id}`,
  { loading: models.userRoles },
);

// TOTP 2FA APIs
export const getTOTPQR = (id) => http.get(
  `/api/users/${id}/twofa/totp`,
  { camelCase: true },
);

export const enableTOTP = (id, data) => http.put(
  `/api/users/${id}/twofa`,
  data,
);

export const disableTOTP = (id, data) => http.delete(
  `/api/users/${id}/twofa`,
  { data },
);
