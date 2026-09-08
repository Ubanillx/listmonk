export const models = Object.freeze({
  serverConfig: 'serverConfig',
  lang: 'lang',
  dashboard: 'dashboard',
  // This loading state is used across all contexts where customer_lists are loaded
  // via the instant "minimal" API.
  customer_lists: 'customer_lists',
  customer_list: 'customer_list',
  // This is used only on the customer_lists page where customer_lists are loaded with full
  // context (customer counts), which can be slow and expensive.
  listsFull: 'listsFull',
  customers: 'customers',
  campaigns: 'campaigns',
  templates: 'templates',
  media: 'media',
  bounces: 'bounces',
  users: 'users',
  profile: 'profile',
  userRoles: 'userRoles',
  customerListRoles: 'customerListRoles',
  settings: 'settings',
  logs: 'logs',
  maintenance: 'maintenance',
  customFields: 'customFields',
});

// Ad-hoc URIs that are used outside of vuex requests.
const rootURL = import.meta.env.VUE_APP_ROOT_URL || '/';
const baseURL = import.meta.env.BASE_URL.replace(/\/$/, '');

export const uris = Object.freeze({
  previewCampaign: '/api/campaigns/:id/preview',
  previewCampaignArchive: '/api/campaigns/:id/preview/archive',
  previewTemplate: '/api/templates/:id/preview',
  previewRawTemplate: '/api/templates/preview',
  exportCustomers: '/api/customers/export',
  errorEvents: '/api/events?type=error',
  base: `${baseURL}/static`,
  root: rootURL,
  static: `${baseURL}/static`,
});

// Keys used in Vuex store.
export const storeKeys = Object.freeze({
  models: 'models',
  isLoading: 'isLoading',
});

export const timestamp = 'ddd D MMM YYYY, hh:mm A';

export const colors = Object.freeze({
  primary: '#0055d4',
});

export const regDuration = '[0-9]+(ms|s|m|h|d)';
