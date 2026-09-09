export const exportTypes = {
  campaigns: '营销活动汇总',
  activity: '营销行为明细',
  customers: '客户名单',
  blocklist: '黑名单',
  bounces: '退信投诉明细',
  bounce_customers: '退信客户名单',
  lists: '列表目录',
  pools: '公海分配名单',
  pool_contacts: '一级公海联系人',
};

export const canExport = ({ workspace, profile }) => !workspace.archived && (
  (profile && profile.userRole && Number(profile.userRole.id) === 1)
  || (workspace.organizationId > 0 && workspace.role === 'manager')
);
