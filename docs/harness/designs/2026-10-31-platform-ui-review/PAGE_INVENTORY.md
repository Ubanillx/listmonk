# 页面与证据清单

2026-10-31 · 内置浏览器 · 当前管理员个人空间。主截图约1982宽；另采1440×900桌面证据及独立走查1265×712证据。截图是页面/页签/弹窗/状态，不等于77条独立路由。

## 77张页面与状态截图

|编号|模块|页面或状态|建议顺序|观察与修改指导|源码入口|
|---|---|---|---|---|---|
|1|概览|[仪表板](shots/01-dashboard.jpg)|P2|日期占据页标题；卡片内标签与数值距离过远，术语生硬。 标题用“仪表盘”，日期作为辅助信息；把指标名和值成组排列。|`frontend/src/views/Dashboard.vue`|
|2|客户|[公海客户](shots/02-pool-customers.jpg)|P2|筛选、搜索、导出分散，上下分页重复；操作依赖小图标。 工具栏统一排列；短表保留底部分页；常用操作可见命名。|`frontend/src/views/Customers.vue`|
|3|客户|[新建公海联系人](shots/03-pool-contact-form.jpg)|P1|“联系人/客户”“部门/组织”混用；分配目标要求手输组织名。 统一“公海客户”“所属组织”；组织改可搜索选择器，必填和禁用原因就近说明。|`frontend/src/views/PoolContactForm.vue`|
|4|客户|[公海客户列表](shots/04-pool-lists.jpg)|P2|层级标题、匹配计数、展开说明三层重复。 保留父子树和组织归属；删除默认展开时的教学句，计数降为辅助文字。|`frontend/src/views/CustomerLists.vue`|
|5|客户|[私域客户](shots/05-private-customers.jpg)|P2|筛选栏碎片化；日期和工具图标占据注意力。 突出姓名、邮箱和状态；时间列与操作列统一宽度。截图只显示示例域名筛选结果。|`frontend/src/views/Customers.vue`|
|6|客户|[私域客户列表](shots/06-private-lists.jpg)|P1|日期的“日”落到下一行；类型、确认方式、可见范围标签密集。 日期禁止拆行；把列表类型、可见范围、订阅确认方式分列或按重要性降级。|`frontend/src/views/CustomerLists.vue`|
|7|客户|[新建私域客户列表](shots/07-private-list-form.jpg)|P2|浮动标签与其他表单风格不同；“打码邮箱”“单重确认”不自然。 统一字段标签；用“隐藏部分邮箱字符”“无需邮件确认/需要邮件确认”。|`frontend/src/views/CustomerListForm.vue`|
|8|客户|[导入私域客户](shots/08-import.jpg)|P1|未上传文件就显示“无”映射；标题和文件说明重复，技术示例常驻。 按选择目标→上传→字段映射→确认导入组织；CSV示例折叠，保留必需列规则。|`frontend/src/views/Import.vue`|
|9|客户|[退信列表](shots/09-bounces.jpg)|P2|无记录时仍有上下分页；通用加号与“暂无内容”缺少含义。 空态用“暂无退信记录”；有记录时再显示分页，不暗示可新建退信。|`frontend/src/views/Bounces.vue`|
|10|客户|[订阅表单生成器初始态](shots/10-subscription-form-builder.jpg)|P2|标题“表单”不说明用途；UUID和协议说明先于任务。 改“订阅表单”；先选择可公开订阅的列表，再生成代码；技术参数放帮助。|`frontend/src/views/Forms.vue`|
|11|客户|[订阅表单代码生成态](shots/11-subscription-form-generated.jpg)|P2|代码编辑区域过高，缺少醒目的复制动作。 增加“复制表单代码”和预览入口；代码区域按内容设合理初始高度。|`frontend/src/views/Forms.vue`|
|12|营销|[营销活动列表](shots/12-campaigns.jpg)|P2|指标、标记和六个小操作挤在同一行，学习成本高。 保留业务分组统计；常用入口用“查看/编辑”，次要操作收入更多菜单。|`frontend/src/views/Campaigns.vue`|
|13|营销|[新建营销活动](shots/13-campaign-new.jpg)|P1|“继续”实际保存；SMTP前置条件提示要求离开，缺少直达和保留输入能力。 按钮改“保存并编辑内容”；内容入口附近解释前置；配置邮箱往返保留草稿。|`frontend/src/views/Campaign.vue`|
|14|营销|[媒体文件](shots/14-media.jpg)|P2|随机文件名与淡色元数据难扫描；可见一张SVG缩略图未正常显示。 增加文件类型/名称层级、失败占位和预览；保留平台只读与归属信息。黑色素材不能直接判为故障。|`frontend/src/views/Media.vue`|
|15|营销|[邮件模板列表](shots/15-templates.jpg)|P2|“可视化”类型使用红色，类似错误；行内操作密集。 内容类型改中性色；加预览缩略图或预览入口，保留自定义模板名称。|`frontend/src/views/Templates.vue`|
|16|营销|[自定义字段](shots/16-custom-fields.jpg)|P1|说明称账号填写值，表格却展示Customer字段，字段归属易误解。 核对实际数据来源后区分“客户字段/用户字段”；先显示业务名称，语法提供复制。|`frontend/src/views/CustomFields.vue`|
|17|营销|[营销统计报告](shots/17-campaign-analytics.jpg)|P1|无数据时仍绘制大灰色饼图、空图表；“次数”列实际显示是/否。 无数据用短空态；布尔列按“是否浏览/是否点击”命名；未启用GeoIP与零记录分别说明。|`frontend/src/views/CampaignAnalyticsReport.vue`|
|18|发件服务|[总览（局部）](shots/18-provider-overview.jpg)|P2|三条离散统计记录使用平滑面积曲线，容易暗示连续趋势。 用真实日期间隔的折线/柱形；显式保留服务商口径及失败类别可重叠说明。截图已裁去账号资料。|`frontend/src/views/DeliveryProvider.vue`|
|19|发件服务|[发送分析（上部）](shots/19-provider-statistics.jpg)|P1|发送量与成功/失败不能简单加总，指标解释离顶部较远。 摘要处提供“统计口径”；区分地址数、错误记录数和SMTP尝试次数。截图裁去真实地址表。|`frontend/src/views/DeliveryProvider.vue`|
|20|发件服务|[投递日志](shots/20-provider-logs.jpg)|P2|查询前空白没有示例或查询范围提示。 保留“输入收件邮箱查看投递详情”，补充日期范围限制与清空条件；不预填真实地址。|`frontend/src/views/DeliveryProvider.vue`|
|21|发件服务|[账号设置（局部）](shots/21-provider-account.jpg)|P2|连接配置与营销SMTP概念容易混淆。 保留“此连接仅查询结果”的一句话；保留密码留空规则，技术详情折叠。截图仅保留空密码区域。|`frontend/src/views/DeliveryProvider.vue`|
|22|组织|[我的组织](shots/22-organizations-mine.jpg)|P2|“未使用”像不可用；“待迁移资源”让可选操作看似待办。 当前状态用“当前未进入”；核对资源含义后改“可迁移资源”；删“安全地”等空泛保证。|`frontend/src/views/organizations/MyOrganizations.vue`|
|23|组织|[加入组织](shots/23-organizations-join.jpg)|P2|简单任务页面基本清楚。 保留单输入与单主按钮；补一句“向组织管理员获取邀请码”，无需填充装饰内容。|`frontend/src/views/organizations/JoinOrganization.vue`|
|24|组织|[申请创建组织](shots/24-organizations-create.jpg)|P1|标题“创建组织”，动作是“提交申请”。 统一“申请创建组织/提交申请”；说明提交后在哪里查看审核结果。|`frontend/src/views/organizations/CreateOrganization.vue`|
|25|组织|[组织成员管理](shots/25-organizations-manage.jpg)|P1|组织选择框箭头脱离输入边界；添加按钮被拉得很宽。 修共享选择器布局；压缩添加按钮，输入、角色和操作形成一行。|`frontend/src/views/organizations/ManageOrganizations.vue`|
|26|组织|[邀请码管理](shots/26-organizations-invites.jpg)|P2|有效期和使用次数缺少单位与默认值语义。 明确“有效期/最多使用次数”的含义；保留限制，不展示生成的真实邀请码。|`frontend/src/views/organizations/ManageOrganizations.vue`|
|27|组织|[待转移资源](shots/27-organizations-transfer.jpg)|P1|只见接收者和禁用操作，缺少待转移资源的清单和数量。 先显示资源类型、数量与归属，再选接收者；空态解释无可转移资源。|`frontend/src/views/organizations/ManageOrganizations.vue`|
|28|组织|[组织回信邮箱](shots/28-organizations-reply-mailboxes.jpg)|P1|“回件/回信”混用；统一与默认邮箱关系不清；多组保存。 统一“回信邮箱”；分别说明默认回信地址与收取/转发地址；保存动作标明保存范围。|`frontend/src/components/ReplyMailboxSettings.vue`|
|29|组织|[客户回信转发](shots/29-organizations-forwarding.jpg)|P2|规则说明长，空表没有下一步提示。 保留离组后的转发范围；空态写“暂无转发规则”，提供有权限的下一步。|`frontend/src/views/organizations/ManageOrganizations.vue`|
|30|组织|[组织营销SMTP](shots/30-organizations-smtp.jpg)|P2|左右重复“尚未配置…池”；普通运营先碰到轮询等技术词。 合并一个空态，按钮用“新建发件邮箱组”；设置详情再解释SMTP和轮询。|`frontend/src/views/organizations/ManageOrganizations.vue`|
|31|组织|[平台管理](shots/31-organizations-platform.jpg)|P1|平台级组织列表嵌在所选组织下，“组织归档”标题覆盖正常组织。 平台管理明确独立范围；将组织列表与已归档组织分开命名，保留管理员权限边界。|`frontend/src/views/organizations/ManageOrganizations.vue`|
|32|用户|[用户列表](shots/32-users.jpg)|P2|姓名、登录名层级弱；SMTP状态仅小图标。 姓名与账号清楚分层；动作提供可见名称/提示；不硬改用户自定义角色名。|`frontend/src/views/Users.vue`|
|33|用户|[用户角色列表](shots/33-user-roles.jpg)|P2|角色仅有名称，权限内容必须打开才能知道。 增加权限范围摘要；编辑、复制放可理解的动作入口。|`frontend/src/views/Roles.vue`|
|34|用户|[客户列表角色列表](shots/34-list-roles.jpg)|P2|通用加号空态无法解释与用户角色的区别。 空态说明“按客户列表分配访问权限”；主按钮用“新建列表角色”。|`frontend/src/views/Roles.vue`|
|35|个人|[个人资料](shots/35-profile.jpg)|P2|页标题为@root，密码区域缺少明确的留空说明。 标题用“个人资料”，账号作为辅助信息；若实现支持则标明密码留空不修改。|`frontend/src/views/UserProfile.vue`|
|36|个人|[个人邮件配置](shots/36-profile-mail.jpg)|P2|两个近似“新建”入口，发件与回信对象需要读段落辨别。 分别命名“添加发件邮箱”“添加回信邮箱”；保留验证状态与组织/个人范围。|`frontend/src/views/UserProfile.vue`|
|37|个人|[API密钥](shots/37-profile-api.jpg)|P2|空表仅显示通用新建动作。 增加“创建API密钥”和一句用途；保留密钥权限与有效期限制，未创建密钥。|`frontend/src/components/PersonalAPIKeySettings.vue`|
|38|设置|[SMTP设置](shots/38-settings-smtp.jpg)|P1|系统通知SMTP与统一投递设置连续堆叠，作用范围容易串读。 按通知邮箱/营销发送/统一规则分组；简化重复帮助，保留并发、重试、限速与生效条件。|`frontend/src/views/Settings.vue`|
|39|设置|[常规设置](shots/39-settings-general.jpg)|P2|“根URL”“Logo URL”等技术名称直出，帮助重复标签。 用“站点地址”“Logo地址”；辅助文案只解释格式、用途或生效条件。|`frontend/src/views/Settings.vue`|
|40|设置|[投递与性能](shots/40-settings-performance.jpg)|P1|同名标题重复，长段解释与字段帮助重复；多个限速层级难区分。 分为“平台总速度/每个SMTP/活动自身限制”；保留0值、排队和限额范围说明。|`frontend/src/views/Settings.vue`|
|41|设置|[隐私](shots/41-settings-privacy.jpg)|P1|正负开关混用；“允许导出/删除”没说是谁的数据。 明确“允许客户导出自己的数据”等主体；解释全局跟踪与客户级设置关系，绑定取反不能漏。|`frontend/src/views/Settings.vue`|
|42|设置|[安全](shots/42-settings-security.jpg)|P2|OIDC关闭后长表单整体淡出，读起来费力；提供商译名不一。 关闭时收起配置，保留短说明；统一“身份提供商”，CORS保留技术名。|`frontend/src/views/Settings.vue`|
|43|设置|[媒体存储](shots/43-settings-media.jpg)|P2|filesystem、上传URI等术语门槛高。 选项显示“本地文件系统/S3”；区分存储目录和访问地址，真实配置值保持原样。|`frontend/src/views/Settings.vue`|
|44|设置|[退信处理](shots/44-settings-bounces.jpg)|P2|“退回/退信”混用，大面积禁用字段仍占高度。 统一“退信”；关闭时只显示作用说明；开启后再展示阈值、处理动作和邮箱。|`frontend/src/views/Settings.vue`|
|45|设置|[AI回信处理](shots/45-settings-ai.jpg)|P1|三步结构清楚，但测试段“未涉及客户数据”等表述过于绝对。 按真实请求核对后写“测试正文将发送至所配置模型”；保留验证邮箱、附件和置信度边界。|`frontend/src/views/Settings.vue`|
|46|设置|[消息通道](shots/46-settings-messengers.jpg)|P2|只有空白容器和新建按钮，不知道功能目的。 空态说明通道用途；按钮用“添加消息通道”；高级用户仍可配置自定义通道。|`frontend/src/views/Settings.vue`|
|47|设置|[管理后台外观](shots/47-settings-appearance.jpg)|P2|“管理员/公开”像用户类别；直接展现大块CSS/JS编辑器。 页签用“管理后台/公开页面”；说明是自定义代码，编辑器按需展开。|`frontend/src/views/Settings.vue`|
|48|设置|[公开页面外观](shots/48-settings-public-appearance.jpg)|P2|同上，CSS/JS面积过大。 明确影响范围；保持代码能力和保存提示，避免写成普通主题选择。|`frontend/src/views/Settings.vue`|
|49|设置|[维护](shots/49-maintenance.jpg)|P1|三个删除动作均使用大蓝色主按钮，并统称“删除”。 使用危险样式和对象明确的动词；确认步骤展示范围、数量、不可恢复后果。确认框未触发。|`frontend/src/views/Maintenance.vue`|
|50|设置|[系统日志](shots/50-logs.jpg)|P2|小号等宽文本固定容器，缺少明显检索入口。 提高日志可读字号，增加筛选/复制；原始日志内容、时间和错误码保持原样。|`frontend/src/views/Logs.vue`|
|51|设置|[业务审计（局部）](shots/51-audit.jpg)|P2|动作和对象中技术字段重复；普通人需要读英文事件码。 主行展示中文动作与对象摘要，事件码放展开详情；补齐事件字典。截图裁去客户明细。|`frontend/src/views/Audit.vue`|
|52|客户|[新建私域客户](shots/52-private-customer-form.jpg)|P2|新对象就出现订阅/退信/行为页签，JSON示例与普通字段并列。 新建态只展示可填写项；历史页签保存后出现；原始JSON收进高级属性。|`frontend/src/views/CustomerForm.vue`|
|53|营销|[已完成活动配置](shots/53-campaign-detail.jpg)|P2|整页禁用控件过淡，当前空SMTP提示容易与历史发件配置混淆。 只读态使用清晰标签和值；区分发送时配置与当前可用邮箱状态。|`frontend/src/views/Campaign.vue`|
|54|营销|[单活动营销概览](shots/54-campaign-overview.jpg)|P2|已发送/失败客户在概览和进度重复，页面整体高度偏大。 保留主要指标及失败历史入口，合并重复小指标；失败人数与失败尝试数仍明确分开。|`frontend/src/components/CampaignDeliveryOverview.vue`|
|55|营销|[活动内容（HTML）](shots/55-campaign-content.jpg)|P2|一行内容撑出巨大代码区域；模板参考被挤到最下方。 合理初始编辑高度；模板字段参考靠近编辑器；保留预览快捷键。|`frontend/src/views/Campaign.vue`|
|56|营销|[活动属性](shots/56-campaign-attributes.jpg)|P2|一个空JSON占据独立大页签。 以“高级属性”折叠展示；保留开发者语法和模板引用能力。|`frontend/src/views/Campaign.vue`|
|57|营销|[活动公开存档设置](shots/57-campaign-archive.jpg)|P1|发布开关与保存行为关系不突出；外链可访问名仍是$t表达式。 明确“保存后发布到公开页面”；修链接可访问名称为“查看公开页面”，不触发发布。|`frontend/src/views/Campaign.vue`|
|58|营销|[可视化模板编辑器](shots/58-template-visual-editor.jpg)|P1|可见范围箭头脱离输入框；编辑器操作面板英文与外层中文混用。 修共享选择器；本地化Styles/Inspect等操作，邮件内容及模板语法保持原样。|`frontend/src/views/TemplateForm.vue`|
|59|用户|[新建用户](shots/59-user-form.jpg)|P2|名称、API类型、角色与密码登录关系不够直观。 名称改“显示名称”；API用“API账号”；密码确认统一“确认密码”，补必填规则。|`frontend/src/views/UserForm.vue`|
|60|用户|[新建用户角色](shots/60-user-role-form.jpg)|P2|长权限矩阵需要滚动，“切换选择”含义不明确。 按业务分组折叠与搜索；批量动作明确“全选/取消全选”，保留部分授权和高风险标记。|`frontend/src/views/RoleForm.vue`|
|61|用户|[新建列表角色](shots/61-list-role-form.jpg)|P2|添加前表格为空，角色作用边界不直观。 短说明“为指定客户列表设置权限”；空态提示先选择列表并添加，保持权限语义。|`frontend/src/views/RoleForm.vue`|
|62|客户|[公海分配工作台](shots/62-pool-workbench.jpg)|P2|空表仍显示完整分页和与当前空态无关的恢复说明。 无记录时收起分页；恢复规则在相应动作附近显示；保留跨组织范围提示。|`frontend/src/components/PoolAllocationWorkbench.vue`|
|63|客户|[公海分配信息](shots/63-pool-allocation-info.jpg)|P2|分配启停、解除绑定、投放授权三个独立状态需长文字解释。 以独立状态行和对象明确的按钮展示；保留三者独立规则，不合并成一个开关。|`frontend/src/components/PoolAllocationWorkbench.vue`|
|64|客户|[公海分配操作记录](shots/64-pool-allocation-history.jpg)|P2|单条记录仍占用大面积空白和完整跳转控件。 短表简化分页，操作记录用紧凑布局；不删除审计内容。|`frontend/src/components/PoolAllocationWorkbench.vue`|
|65|客户|[已归档公海列表](shots/65-pool-archived.jpg)|P1|页标题原样显示archived；零记录仍显示树的教学说明。 标题改“已归档公海列表”；空态用“暂无已归档列表”，收起无效树工具。|`frontend/src/views/CustomerLists.vue`|
|66|通用|[无权限页](shots/66-permission-denied.jpg)|P2|侧栏“仪表板”与按钮“仪表盘”不一致，主文案不是标题。 统一“仪表盘”；主提示使用真实标题，保留联系管理员和返回入口。|`frontend/src/views/403.vue`|
|67|通用|[404页](shots/67-not-found.jpg)|P2|404数字占比大；与403页的标题层级不一致。 以“页面不存在”为主标题，404作辅助码，保留返回入口。|`frontend/src/views/404.vue`|
|68|公开|[站点首页](shots/68-public-home.jpg)|P2|“邮件客户列表存档”暴露内部对象名。 改“历史邮件”；配置的站点名称不作为翻译错误批量替换。|`static/public/templates/home.html`|
|69|公开|[公开订阅页](shots/69-public-subscription.jpg)|P1|面向订阅者却显示“私域客户列表”，列表项前多余圆点。 改“选择订阅内容”；去掉圆点；按实际确认机制说明下一步。|`static/public/templates/subscription-form.html`|
|70|公开|[公开存档列表](shots/70-public-archive.jpg)|P2|“邮件客户列表存档”难懂。 标题“历史邮件”；空态“暂无公开邮件”；保留RSS/订阅入口。|`static/public/templates/archive.html`|
|71|登录|[登录页](shots/71-login.jpg)|P2|整体清楚，可保留简洁表单。 统一输入与按钮规范，后续补测错误/密码可见切换和键盘焦点，不凭截图声称已通过。|`static/public/templates/login.html`|
|72|登录|[找回密码](shots/72-forgot-password.jpg)|P2|“继续”没有说明会发送重置邮件。 若流程确为发送邮件，按钮改“发送重置邮件”；增加“请输入账号绑定的邮箱”。未提交请求。|`static/public/templates/forgot-password.html`|
|73|登录|[选择工作空间](shots/73-select-workspace.jpg)|P1|空间卡片外多余黑圆点；已有多个组织仍写“还没有组织？”。 去除列表标记，左右对齐；底部改“加入其他组织”；保留个人/组织图标区分。|`static/public/templates/select-workspace.html`|
|74|验证|[新建活动1440桌面布局](shots/74-campaign-new-1440.jpg)|P1|1440×900首屏看不到底部保存前置说明。 关键保存条件靠近内容入口，主按钮命名明确；长表保留清晰页尾动作。|`frontend/src/views/Campaign.vue`|
|75|验证|[活动返回后输入为空](shots/75-campaign-return-empty.jpg)|P1|输入示例名称/主题→个人→浏览器返回，两个输入变空，无离开提醒。 配置邮箱往返保留当前草稿，或离开前提供可取消提示；验证浏览器返回和刷新恢复。|`frontend/src/views/Campaign.vue`|
|76|验证|[组织管理1440桌面布局](shots/76-organizations-manage-1440.jpg)|P1|1440宽下组织选择器箭头仍在边框外。 从共享选择器布局修复；验收1440及更宽桌面所有使用处。此截图不用于认定成员为空。|`frontend/src/views/organizations/ManageOrganizations.vue`|
|77|客户|[已有私域客户详情](shots/77-customer-detail.jpg)|P2|技术ID、UUID和JSON默认突出；姓名、订阅与历史混在长弹窗。 信息按“基本资料/订阅与行为/高级属性”分层；保留黑名单和订阅限制说明。仅示例客户。|`frontend/src/views/CustomerForm.vue`|

## 后台路由核对

以下来自当前 frontend/src/router/index.js，共37条路由规则。直接页面、动态详情、筛选变体和兼容跳转分开列出。动态ID不是独立设计页面，不声称对每个客户/组织逐个审查。

|路由（均位于/admin下）|页面实现|覆盖说明|
|---|---|---|
|`/404`|views/404.vue|该页面类型已采集，参见上表|
|`/403`|views/403.vue|该页面类型已采集，参见上表|
|`/`|views/Dashboard.vue|该页面类型已采集，参见上表|
|`/pool-lists`|views/CustomerLists.vue|该页面类型已采集，参见上表|
|`/pool-lists/:id`|views/CustomerLists.vue|共用列表/客户组件；具体ID预选及筛选变体未逐条实测|
|`/customer-lists`|views/CustomerLists.vue|该页面类型已采集，参见上表|
|`/customer-lists/forms`|views/Forms.vue|该页面类型已采集，参见上表|
|`/customer-lists/:id`|views/CustomerLists.vue|共用列表/客户组件；具体ID预选及筛选变体未逐条实测|
|`/customers`|views/Customers.vue|该页面类型已采集，参见上表|
|`/customers/import`|views/Import.vue|该页面类型已采集，参见上表|
|`/customers/bounces`|views/Bounces.vue|该页面类型已采集，参见上表|
|`/customers/customer-lists/:customerListID`|views/Customers.vue|共用列表/客户组件；具体ID预选及筛选变体未逐条实测|
|`/pool-lists/:customerListID/contacts`|views/Customers.vue|共用列表/客户组件；具体ID预选及筛选变体未逐条实测|
|`/customers/pool-lists/:customerListID`|redirect|兼容/入口跳转，不另算页面|
|`/pool/:customerListID`|redirect|兼容/入口跳转，不另算页面|
|`/pool`|views/Customers.vue|该页面类型已采集，参见上表|
|`/customers/:id`|views/Customers.vue|该页面类型已采集，参见上表|
|`/campaigns`|views/Campaigns.vue|该页面类型已采集，参见上表|
|`/campaigns/media`|views/Media.vue|该页面类型已采集，参见上表|
|`/campaigns/templates`|views/Templates.vue|该页面类型已采集，参见上表|
|`/campaigns/custom-fields`|views/CustomFields.vue|该页面类型已采集，参见上表|
|`/campaigns/analytics`|views/CampaignAnalyticsReport.vue|该页面类型已采集，参见上表|
|`/campaigns/:id`|views/Campaign.vue|该页面类型已采集，参见上表|
|`/user/profile`|views/UserProfile.vue|该页面类型已采集，参见上表|
|`/organizations`|redirect|兼容/入口跳转，不另算页面|
|`/organizations/mine`|views/organizations/MyOrganizations.vue|该页面类型已采集，参见上表|
|`/organizations/join`|views/organizations/JoinOrganization.vue|该页面类型已采集，参见上表|
|`/organizations/create`|views/organizations/CreateOrganization.vue|该页面类型已采集，参见上表|
|`/organizations/manage`|views/organizations/ManageOrganizations.vue|该页面类型已采集，参见上表|
|`/settings`|views/Settings.vue|该页面类型已采集，参见上表|
|`/delivery-provider`|views/DeliveryProvider.vue|该页面类型已采集，参见上表|
|`/settings/logs`|views/Logs.vue|该页面类型已采集，参见上表|
|`/settings/audit`|views/Audit.vue|该页面类型已采集，参见上表|
|`/users`|views/Users.vue|该页面类型已采集，参见上表|
|`/users/roles/users`|views/Roles.vue|该页面类型已采集，参见上表|
|`/users/roles/customer-lists`|views/Roles.vue|该页面类型已采集，参见上表|
|`/settings/maintenance`|views/Maintenance.vue|该页面类型已采集，参见上表|

## 公开和登录模板核对

|模板|现场覆盖|后续指导|
|---|---|---|
|`home.html`|68|后台入口与面向订阅者的入口清楚区分。|
|`login.html`|71|保持简洁；另测错误、锁定和SSO分支。|
|`forgot-password.html`|72|按钮说明实际下一步，不提交真实重置请求。|
|`select-workspace.html`|73|去除多余圆点，加入其他组织入口命名。|
|`subscription-form.html`|69|改为面向订阅者的“订阅内容”。|
|`archive.html`|70列表空态；单封公开内容未实测|统一历史邮件命名与空态；单封正文保留邮件内容。|
|`login-setup.html`|仅静态盘点；依赖初始化/验证条件|后续使用隔离账号检查设置密码与验证反馈。|
|`reset-password.html`|仅静态盘点；需要有效重置凭证|后续检查新密码、确认密码、过期与完成状态。|
|`twofa.html`|仅静态盘点；需要两步验证会话|后续检查验证码输入、恢复码入口与错误恢复。|
|`optin.html`|仅静态盘点；需要有效订阅确认链接|用隔离测试订阅核对待确认列表和确认后状态。|
|`subscription.html`|仅静态盘点；需要有效客户链接|覆盖退订、偏好、导出与删除自己的数据；区分范围。|
|`message.html`|仅静态盘点；通用结果模板|每类结果写清完成了什么与下一步，分别验证成功/错误。|
|`index.html`|共享页头/页尾，不单算业务页|统一公开页与登录页的字号、表单和品牌基础。|

## 覆盖边界

已实看后台所有独立页面类型、全部主设置页签、组织管理七个页签、发件服务四页签、活动主要详情页签及常用创建弹窗。单活动营销统计复用报告组件，但其特定数据状态未另采；长页面的下半部分、全部字段悬浮帮助、分页第二页、移动端、所有权限组合、所有编辑器格式、批量导入用户/组织成员弹窗、SMTP/密钥创建弹窗、错误/成功/加载分支没有穷举。未上传、保存业务表单、发送、测试外部连接、删除、发布或修改权限。现有缺少SMTP和GeoIP配置是环境事实，不能当作产品功能故障。

18/19/21/51为现场裁切截图，以避免保留账号联系信息和客户地址；05使用示例域名筛选，77仅示例客户。原始DOM和表单数据没有作为交付保存。
