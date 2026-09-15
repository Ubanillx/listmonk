import Vue from 'vue';
import VueRouter from 'vue-router';

Vue.use(VueRouter);

// The meta.group param is used in App.vue to expand menu group by name.
const routes = [
  {
    path: '/404',
    name: '404_page',
    meta: { title: '404' },
    component: () => import('../views/404.vue'),
  },
  {
    path: '/',
    name: 'dashboard',
    meta: { title: '' },
    component: () => import('../views/Dashboard.vue'),
  },
  {
    path: '/customer-lists',
    name: 'customerLists',
    meta: { title: 'globals.terms.customer_lists', group: 'customerLists' },
    component: () => import('../views/CustomerLists.vue'),
  },
  {
    path: '/customer-lists/forms',
    name: 'forms',
    meta: { title: 'forms.title', group: 'customerLists' },
    component: () => import('../views/Forms.vue'),
  },
  {
    path: '/customer-lists/:id',
    name: 'customerList',
    meta: { title: 'globals.terms.customer_lists', group: 'customerLists' },
    component: () => import('../views/CustomerLists.vue'),
  },
  {
    path: '/customers',
    name: 'customers',
    meta: { title: 'globals.terms.customers', group: 'customers' },
    component: () => import('../views/Customers.vue'),
  },
  {
    path: '/customers/import',
    name: 'import',
    meta: { title: 'import.title', group: 'customers' },
    component: () => import('../views/Import.vue'),
  },
  {
    path: '/customers/bounces',
    name: 'bounces',
    meta: { title: 'globals.terms.bounces', group: 'customers' },
    component: () => import('../views/Bounces.vue'),
  },
  {
    path: '/customers/customer-lists/:customerListID',
    name: 'customersCustomerList',
    meta: { title: 'globals.terms.customers', group: 'customers' },
    component: () => import('../views/Customers.vue'),
  },
  {
    path: '/customers/:id',
    name: 'customer',
    meta: { title: 'globals.terms.customers', group: 'customers' },
    component: () => import('../views/Customers.vue'),
  },
  {
    path: '/campaigns',
    name: 'campaigns',
    meta: { title: 'globals.terms.campaigns', group: 'campaigns' },
    component: () => import('../views/Campaigns.vue'),
  },
  {
    path: '/campaigns/media',
    name: 'media',
    meta: { title: 'globals.terms.media', group: 'campaigns' },
    component: () => import('../views/Media.vue'),
  },
  {
    path: '/campaigns/templates',
    name: 'templates',
    meta: { title: 'globals.terms.templates', group: 'campaigns' },
    component: () => import('../views/Templates.vue'),
  },
  {
    path: '/campaigns/custom-fields',
    name: 'customFields',
    meta: { title: 'customFields.title', group: 'campaigns' },
    component: () => import('../views/CustomFields.vue'),
  },
  {
    path: '/campaigns/analytics',
    name: 'campaignAnalytics',
    meta: { title: 'analytics.title', group: 'campaigns' },
    component: () => import('../views/CampaignAnalyticsReport.vue'),
  },
  {
    path: '/campaigns/:id',
    name: 'campaign',
    meta: { title: 'globals.terms.campaign', group: 'campaigns' },
    component: () => import('../views/Campaign.vue'),
  },
  {
    path: '/user/profile',
    name: 'userProfile',
    meta: { title: 'users.profile' },
    component: () => import('../views/UserProfile.vue'),
  },
  {
    path: '/organizations',
    name: 'organizations',
    meta: { title: 'organizations.title', group: 'organizations' },
    redirect: { name: 'organizationMine' },
  },
  {
    path: '/organizations/mine',
    name: 'organizationMine',
    meta: { title: 'organizations.joinedTitle', group: 'organizations' },
    component: () => import('../views/organizations/MyOrganizations.vue'),
  },
  {
    path: '/organizations/join',
    name: 'organizationJoin',
    meta: { title: 'organizations.joinTitle', group: 'organizations' },
    component: () => import('../views/organizations/JoinOrganization.vue'),
  },
  {
    path: '/organizations/create',
    name: 'organizationCreate',
    meta: { title: 'organizations.createTitle', group: 'organizations' },
    component: () => import('../views/organizations/CreateOrganization.vue'),
  },
  {
    path: '/organizations/manage',
    name: 'organizationManage',
    meta: { title: 'organizations.manageTitle', group: 'organizations', organizationManager: true },
    component: () => import('../views/organizations/ManageOrganizations.vue'),
  },
  {
    path: '/settings',
    name: 'settings',
    meta: { title: 'globals.terms.settings', group: 'settings' },
    component: () => import('../views/Settings.vue'),
  },
  {
    path: '/settings/logs',
    name: 'logs',
    meta: { title: 'logs.title', group: 'settings' },
    component: () => import('../views/Logs.vue'),
  },
  {
    path: '/settings/audit',
    name: 'audit',
    meta: { title: 'audit.title', group: 'settings' },
    component: () => import('../views/Audit.vue'),
  },
  {
    path: '/users',
    name: 'users',
    meta: { title: 'globals.terms.users', group: 'users' },
    component: () => import('../views/Users.vue'),
  },
  {
    path: '/users/roles/users',
    name: 'userRoles',
    meta: { title: 'users.userRoles', group: 'users' },
    component: () => import('../views/Roles.vue'),
  },
  {
    path: '/users/roles/customer-lists',
    name: 'customerListRoles',
    meta: { title: 'users.customerListRoles', group: 'users' },
    component: () => import('../views/Roles.vue'),
  },
  {
    path: '/settings/maintenance',
    name: 'maintenance',
    meta: { title: 'maintenance.title', group: 'settings' },
    component: () => import('../views/Maintenance.vue'),
  },
];

const router = new VueRouter({
  mode: 'history',
  base: import.meta.env.BASE_URL,
  routes,

  scrollBehavior(to) {
    if (to.hash) {
      return { selector: to.hash };
    }
    return { x: 0, y: 0 };
  },
});

export default router;
