const poolListTypes = ['pool', 'pool_segment'];

export function isPoolCustomerList(customerList) {
  return !!customerList && poolListTypes.indexOf(customerList.type) > -1;
}

export function isRegularCustomerList(customerList) {
  return !!customerList && !isPoolCustomerList(customerList);
}

function organizationID(resource) {
  return Number(resource && (resource.organizationId || resource.organization_id)) || 0;
}

function ownerID(resource) {
  return Number(resource && (resource.ownerUserId || resource.owner_user_id)) || 0;
}

// Ordinary customer-list selectors must only offer records that can be used
// by a customer mutation in the active workspace. Public pools are an
// explicit cross-workspace exception and are handled by their dedicated UI.
export function isActiveWorkspaceCustomerList(customerList, workspace, userID) {
  if (!isRegularCustomerList(customerList)
    || customerList.transferPendingAt || customerList.transfer_pending_at
    || (customerList.status && customerList.status !== 'active')) {
    return false;
  }

  const customerListOwnerID = ownerID(customerList);
  if (customerListOwnerID < 1) {
    return false;
  }

  const activeOrganizationID = organizationID(workspace);
  const customerListOrganizationID = organizationID(customerList);
  if (activeOrganizationID > 0) {
    return customerListOrganizationID === activeOrganizationID;
  }
  return customerListOrganizationID === 0 && customerListOwnerID === Number(userID);
}

export function isOwnedActiveWorkspaceCustomerList(customerList, workspace, userID) {
  return isActiveWorkspaceCustomerList(customerList, workspace, userID)
    && ownerID(customerList) === Number(userID)
    && ownerID(customerList) > 0;
}
