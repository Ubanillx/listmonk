// Presentation bundles only. The API continues to persist stable permission
// IDs, and opening/saving a partially granted bundle never expands it.
export const businessPermissionGroups = [
  {
    group: 'privateCustomers',
    permissions: [
      { id: 'privateRead', permissions: ['customers:get', 'customers:get_all', 'customer_lists:get_all'] },
      { id: 'privateMaintain', permissions: ['customers:manage', 'customers:import', 'customers:blocklist', 'customers:membership_manage', 'customer_lists:manage_all'] },
      { id: 'privateDelete', permissions: ['customers:delete', 'customer_lists:delete'] },
      { id: 'privateExport', permissions: ['customers:export'] },
      { id: 'sensitiveRead', permissions: ['customers:sensitive_read'] },
    ],
  },
  {
    group: 'pools',
    permissions: [
      { id: 'poolRead', permissions: ['pools:get'] },
      { id: 'poolAllocation', permissions: ['pools:manage'] },
      { id: 'poolMaster', permissions: ['pools:master_manage'] },
      { id: 'poolDelivery', permissions: ['pools:delivery_manage'] },
      { id: 'poolExport', permissions: ['pools:export'] },
    ],
  },
  {
    group: 'campaigns',
    permissions: [
      { id: 'campaignRead', permissions: ['campaigns:get', 'campaigns:get_all', 'campaigns:get_analytics', 'campaigns:recipients'] },
      { id: 'campaignMaintain', permissions: ['campaigns:manage', 'campaigns:manage_all', 'campaigns:test'] },
      { id: 'campaignSend', permissions: ['campaigns:send', 'campaigns:schedule'] },
      { id: 'campaignControl', permissions: ['campaigns:control'] },
      { id: 'campaignPublicPool', permissions: ['campaigns:public_pool_send'] },
    ],
  },
  {
    group: 'assets',
    permissions: [
      { id: 'assetRead', permissions: ['templates:get', 'media:get'] },
      { id: 'assetMaintain', permissions: ['templates:manage', 'media:manage'] },
      { id: 'assetShare', permissions: ['assets:share'] },
    ],
  },
  {
    group: 'mailboxes',
    permissions: [
      { id: 'mailboxUse', permissions: ['mailboxes:use'] },
      { id: 'mailboxManage', permissions: ['mailboxes:manage'] },
    ],
  },
];

export const bundledPermissionIDs = new Set(businessPermissionGroups
  .flatMap((group) => group.permissions.flatMap((bundle) => bundle.permissions)));

export function bundleState(selected, bundle) {
  const count = bundle.permissions.filter((id) => selected.includes(id)).length;
  return { checked: count === bundle.permissions.length, partial: count > 0 && count < bundle.permissions.length };
}

export function toggleBundle(selected, bundle, checked) {
  const rest = selected.filter((id) => !bundle.permissions.includes(id));
  return checked ? [...rest, ...bundle.permissions] : rest;
}
