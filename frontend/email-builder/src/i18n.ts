import { create } from 'zustand';

// Interface copy only. Never translate document data, block type names, URLs,
// template expressions or user-authored email content.
const zh: Record<string, string> = {
  'Styles': '整体样式', 'Inspect': '内容设置', 'Global': '邮件样式',
  'Edit': '编辑', 'Preview': '预览', 'HTML output': 'HTML 源码', 'JSON output': 'JSON 源码',
  'Desktop view': '桌面预览', 'Mobile view': '手机预览',
  'Download JSON file': '下载 JSON', 'Import JSON': '导入 JSON', 'Cancel': '取消', 'Import': '导入',
  'Paste the template JSON below.': '粘贴模板的 JSON 内容。',
  'This will override your current template.': '导入将替换当前编辑内容。',
  'Invalid json': 'JSON 格式有误，请检查括号和引号。',
  'Invalid JSON schema': '内容不符合可视化模板格式。',
  'Missing "root" node': '模板缺少 root 根节点。',
  'Invalid root node': '模板根节点必须是邮件布局。',
  'Click on a block to inspect.': '选择邮件中的内容块以编辑。',
  'Block not found': '未找到内容块，请重新选择。',
  'Selected block is unavailable. Select another block.': '当前内容块已不存在，请重新选择。',
  'Heading': '标题', 'Text': '正文', 'Button': '按钮', 'Image': '图片', 'Avatar': '头像',
  'My new text block': '请输入正文内容', 'Sample product': '产品图片', 'Hello world': '请输入内容',
  'Divider': '分隔线', 'Spacer': '留白', 'Html': 'HTML', 'Columns': '分栏', 'Container': '容器',
  'Heading block': '标题', 'Text block': '正文', 'Button block': '按钮', 'Image block': '图片',
  'Avatar block': '头像', 'Divider block': '分隔线', 'Spacer block': '留白', 'Html block': 'HTML',
  'Columns block': '分栏', 'Container block': '容器',
  'Content': '内容', 'Url': '链接地址', 'Source URL': '图片地址', 'Image URL': '图片地址',
  'Button text': '按钮文字',
  'Alt text': '替代文字', 'Click through URL': '点击跳转地址', 'Select media': '选择媒体文件',
  'Size': '尺寸', 'Shape': '形状', 'Circle': '圆形', 'Square': '方形', 'Rounded': '圆角',
  'Width': '宽度', 'Height': '高度', 'Full': '铺满', 'Auto': '自动', 'auto': '自动',
  'Xs': '极小', 'Sm': '小', 'Md': '中', 'Lg': '大', 'Style': '样式', 'Rectangle': '直角', 'Pill': '胶囊',
  'Text color': '文字颜色', 'Button color': '按钮颜色', 'Color': '颜色',
  'Background color': '背景颜色', 'Border color': '边框颜色', 'Border radius': '圆角大小',
  'Backdrop color': '外围背景', 'Canvas color': '邮件背景', 'Canvas border color': '邮件边框',
  'Canvas border radius': '邮件圆角', 'Font family': '字体', 'Font size': '字号', 'Font weight': '字重',
  'Regular': '常规', 'Bold': '加粗', 'Alignment': '对齐方式', 'Padding': '内边距', 'Level': '标题级别',
  'Number of columns': '列数', 'Columns gap': '列间距', 'Column 1': '第 1 列', 'Column 2': '第 2 列', 'Column 3': '第 3 列',
  'Match email settings': '跟随邮件设置', 'Modern sans': '现代无衬线', 'Book sans': '书籍无衬线',
  'Organic sans': '人文无衬线', 'Geometric sans': '几何无衬线', 'Heavy sans': '粗体无衬线',
  'Rounded sans': '圆体无衬线', 'Modern serif': '现代衬线', 'Book serif': '书籍衬线', 'Monospace': '等宽字体',
  'Move up': '上移', 'Move down': '下移', 'Delete': '删除内容块', 'Add block': '添加内容块',
  'Show settings': '展开设置', 'Hide settings': '收起设置',
  'Back to canvas': '返回画布',
  'Align left': '左对齐', 'Align center': '居中对齐', 'Align right': '右对齐',
  'Align top': '顶部对齐', 'Align middle': '垂直居中', 'Align bottom': '底部对齐',
  'Top padding': '上内边距', 'Bottom padding': '下内边距', 'Left padding': '左内边距', 'Right padding': '右内边距',
  'Choose color': '选择颜色', 'Reset color': '重置颜色', 'Hex color': '十六进制颜色值',
  'Share current template': '分享当前模板',
  'The URL was updated. Copy it to share your current template.': '链接已更新，复制链接即可分享当前模板。',
};

export const useEditorLocale = create<{ locale: string }>(() => ({ locale: 'en' }));

export function setLocale(locale: string) {
  useEditorLocale.setState({ locale: locale || 'en' });
  document.documentElement.lang = locale || 'en';
}

export function t(key: string): string {
  return /^zh(?:-|$)/i.test(useEditorLocale.getState().locale) ? zh[key] ?? key : key;
}
