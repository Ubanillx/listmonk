export const exportTypes = {
  campaigns: 'export.types.campaigns',
  activity: 'export.types.activity',
  customers: 'export.types.customers',
  blocklist: 'export.types.blocklist',
  bounces: 'export.types.bounces',
  bounce_customers: 'export.types.bounceCustomers',
  lists: 'export.types.lists',
  pools: 'export.types.pools',
  pool_contacts: 'export.types.poolContacts',
};

export const canExport = ({ workspace, profile }) => !workspace.archived && (
  (profile && profile.userRole && Number(profile.userRole.id) === 1)
  || (workspace.organizationId > 0 && workspace.role === 'manager')
);
