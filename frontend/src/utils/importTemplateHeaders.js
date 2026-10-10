// Match translated template headings while preserving legacy API-key headings.
// Chinese aliases also allow templates to be imported after switching language.
const labels = {
  username: ['users.username', 'Username', '用户名', '使用者名稱'],
  name: ['globals.fields.name', 'Name', '姓名', '名称', '名稱'],
  password: ['users.password', 'Password', '密码', '密碼'],
  email: ['customers.email', 'E-mail', 'Email', '邮箱', '电子邮件', '電子郵件', '電子郵箱', '邮件', '郵件'],
  user_role: ['users.userRole', 'User role', '用户角色', '使用者角色', '使用者身分'],
  customer_list_role: ['users.customerListRole', 'CustomerList roles', 'CustomerList role', '客户列表角色', '客戶列表角色', '清單角色'],
  status: ['globals.fields.status', 'Status', '状态', '狀態'],
  account: ['organizations.account', 'Account', '账号', '帳號', '账户', '帳戶'],
  role: ['organizations.role', 'Role', '角色'],
};

const normalize = (value) => String(value || '').trim().toLowerCase().replace(/[\s-]+/g, '_');

export default function importTemplateHeader(value, columns, translate) {
  const header = normalize(value);
  return columns.find((column) => header === column || (labels[column] || []).some((label, index) => (
    header === normalize(index === 0 ? translate(label) : label)
  ))) || header;
}
