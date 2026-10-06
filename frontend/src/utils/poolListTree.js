// Build the hierarchy only from lists already returned by the permission-scoped API.
// A missing parent must never cause another metadata request or a guessed relationship.
export default function buildPoolListGroups(lists, filters = {}) {
  const query = (filters.query || '').trim().toLowerCase();
  const parents = new Map(lists.filter((list) => list.type === 'pool').map((list) => [Number(list.id), list]));
  const children = new Map();
  const orphans = [];

  lists.filter((list) => list.type === 'org_pool_allocation').forEach((list) => {
    const parentID = Number(list.poolParentId);
    if (!parents.has(parentID)) {
      orphans.push(list);
      return;
    }
    if (!children.has(parentID)) children.set(parentID, []);
    children.get(parentID).push(list);
  });

  const matches = (list, parent) => {
    const organizationID = Number(list.organizationId) || 0;
    return (!filters.type || list.type === filters.type)
      && (filters.organizationId === '' || filters.organizationId === undefined || organizationID === Number(filters.organizationId))
      && (!query || list.name.toLowerCase().includes(query) || (parent && parent.name.toLowerCase().includes(query)));
  };

  const groups = [];
  parents.forEach((parent, id) => {
    const allocations = (children.get(id) || []).filter((child) => matches(child, parent));
    const parentMatches = matches(parent);
    if (!parentMatches && !allocations.length) return;
    groups.push({
      parent: {
        ...parent, treeDepth: 0, treeContext: !parentMatches, treeChildrenCount: allocations.length,
      },
      children: allocations.map((child) => ({ ...child, treeDepth: 1 })),
    });
  });
  orphans.filter((list) => matches(list)).forEach((list) => {
    groups.push({
      parent: {
        ...list, treeDepth: 0, treeOrphan: true, treeChildrenCount: 0,
      },
      children: [],
    });
  });
  return groups;
}
