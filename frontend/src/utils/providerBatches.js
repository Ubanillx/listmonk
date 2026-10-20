const dateKey = (value) => (/^\d{4}-\d{2}-\d{2}$/.test(value || '') ? value : '');

// Group before pagination so records from a single task cannot span pages.
export default function providerBatches(records) {
  const groups = new Map();
  records.forEach((row, index) => {
    const ident = row.task_ident || '';
    const key = ident ? `batch:${ident}` : `unknown:${index}`;
    if (!groups.has(key)) {
      groups.set(key, {
        key, task_ident: ident, rows: [], latestDate: '',
      });
    }
    const group = groups.get(key);
    group.rows.push(row);
    const date = dateKey(row.task_date);
    if (date > group.latestDate) group.latestDate = date;
  });
  return [...groups.values()].map((group) => ({
    ...group,
    rows: group.rows.slice().sort((a, b) => dateKey(b.task_date).localeCompare(dateKey(a.task_date))),
  })).sort((a, b) => b.latestDate.localeCompare(a.latestDate) || b.task_ident.localeCompare(a.task_ident));
}
