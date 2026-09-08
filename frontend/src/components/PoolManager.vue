<!-- eslint-disable vue/max-len -->
<template>
  <section class="pool-manager">
    <div class="pool-manager__intro"><div><h3>一级公海拆分</h3><p>管理员可在这里指定目标组织并创建二级列表，不需要切换或加入目标组织。</p></div><b-tag type="is-info" class="is-light">一级公海</b-tag></div>
    <section class="pool-manager__section" data-cy="pool-target-organization-panel">
      <div class="pool-manager__section-heading"><span class="pool-manager__section-number">1</span><div><h4>选择目标组织</h4><p>二级列表和回件邮箱归属于所选组织，当前工作区不会被切换。</p></div></div>
      <div class="pool-manager__section-content">
        <b-field v-if="isPlatformAdmin" label="目标组织" label-position="on-border"><b-select v-model.number="targetOrganizationID" expanded data-cy="pool-target-organization"><option :value="null">请选择组织</option><option v-for="organization in organizations" :key="organization.id" :value="organization.id">{{ organization.name }}</option></b-select></b-field>
        <div v-else class="pool-manager__selected-organization"><span>当前组织（组织管理员）</span><strong>{{ targetOrganizationName }}</strong></div>
      </div>
    </section>
    <section v-if="organizationID" class="pool-manager__section" data-cy="pool-secondary-list-panel">
      <div class="pool-manager__section-heading"><span class="pool-manager__section-number">2</span><div><h4>二级列表</h4><p>每个组织对同一个一级公海只能有一个；未绑定时可直接填写名称创建。</p></div></div>
      <div class="pool-manager__section-content">
        <div v-if="selectedSegment" class="pool-manager__segment-summary" data-cy="pool-segment-summary"><div><span>列表名称</span><strong>{{ selectedSegment.listName || selectedSegment.listId }}</strong></div><div><span>所属组织</span><strong>{{ selectedSegment.organizationName || targetOrganizationName }}</strong></div><div v-if="!isPlatformAdmin"><span>回件邮箱</span><strong>{{ selectedSegment.replyMailboxEmail || '未配置' }}</strong></div></div>
        <div v-if="selectedSegment && isPlatformAdmin" class="pool-manager__organization-note"><b-icon icon="information-outline" size="is-small" /><span>回件邮箱由目标组织在组织工作区内配置，最高管理员无需代为设置。</span></div>
        <div v-if="selectedSegment && !isPlatformAdmin" class="pool-manager__reply-mailbox"><b-field label="统一回件邮箱" label-position="on-border"><b-select v-model="replyMailboxID" expanded :disabled="!canManageSegments" data-cy="pool-reply-mailbox"><option :value="null">未配置（发送前需补充）</option><option v-for="mailbox in replyMailboxes" :key="mailbox.id" :value="mailbox.id">{{ mailbox.name || mailbox.email }}（{{ mailbox.email }}）</option></b-select></b-field><div class="pool-manager__inline-action"><b-button size="is-small" type="is-primary" :disabled="!canManageSegments" @click="saveReplyMailbox">保存回件邮箱</b-button><p class="help">公司内部地址，营销活动最终以该二级列表配置为准。</p></div></div>
        <div v-if="!selectedSegment && canCreateSegment" class="pool-manager__create-segment" data-cy="pool-segment-create"><div class="pool-manager__create-segment-title"><div><span>目标组织</span><strong>{{ targetOrganizationName }}</strong></div><small>创建后立即绑定当前一级公海</small></div><b-field label="二级列表名称" label-position="on-border"><b-input v-model.trim="newSegment.name" maxlength="200" placeholder="例如：华东销售组" data-cy="pool-segment-name" /></b-field><b-field v-if="!isPlatformAdmin" label="统一回件邮箱" label-position="on-border"><b-select v-model="newSegment.replyMailboxID" expanded data-cy="pool-segment-reply-mailbox"><option :value="null">未配置（可稍后设置）</option><option v-for="mailbox in replyMailboxes" :key="mailbox.id" :value="mailbox.id">{{ mailbox.name || mailbox.email }}（{{ mailbox.email }}）</option></b-select></b-field><p v-if="isPlatformAdmin" class="help pool-manager__organization-note-text">创建后由目标组织在组织工作区配置回件邮箱。</p><b-button type="is-primary" :loading="creatingSegment" :disabled="!newSegment.name" data-cy="create-pool-segment" @click="createSegment">创建并绑定</b-button></div>
        <div v-else-if="!selectedSegment" class="pool-manager__empty-state" data-cy="pool-secondary-list-empty"><b-icon icon="playlist-check" size="is-medium" /><div><strong>该组织尚未绑定二级列表</strong><p>请联系最高管理员从一级公海拆分，或由组织管理员在当前组织创建。</p></div></div>
      </div>
    </section>
    <section v-else class="pool-manager__empty-state pool-manager__empty-state--top" data-cy="pool-secondary-list-empty"><b-icon icon="account-group-outline" size="is-medium" /><div><strong>请选择目标组织</strong><p>选择后即可直接查看或创建该组织的二级列表，管理员无需加入该组织。</p></div></section>
    <section v-if="isAllocationReady" class="pool-manager__section pool-manager__allocation" data-cy="pool-contact-allocation">
      <div class="pool-manager__section-heading"><span class="pool-manager__section-number">3</span><div><h4>导入分配文件</h4><p>一次上传几千条联系人，系统按“客户编码 + 邮箱”匹配一级公海并批量分配。</p></div></div>
      <div class="pool-manager__section-content">
        <div class="pool-manager__import-card">
          <div class="pool-manager__import-card-header"><div><strong>CSV / Excel 文件</strong><p>首行必须包含 <code>customer_code</code>、<code>email</code> 两列；客户编码可重复，邮箱按忽略大小写匹配。</p></div><b-button size="is-small" type="is-light" icon-left="download" @click="downloadAllocationTemplate">下载模板</b-button></div>
          <b-upload v-model="allocationFile" drag-drop expanded accept=".csv,.xlsx,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" :disabled="importingAllocation">
            <div class="content has-text-centered"><b-icon icon="file-upload-outline" size="is-large" /><p>{{ allocationFile ? allocationFile.name : '拖拽文件到此处，或点击选择文件' }}</p><small>支持 .csv、.xlsx，单次最多 100,000 行</small></div>
          </b-upload>
          <div class="pool-manager__import-actions"><b-button type="is-primary" icon-left="account-multiple-plus" :loading="importingAllocation" :disabled="!allocationFile" @click="importAllocationFile">开始分配</b-button><span class="help">上传文件仅用于匹配，不会在页面展示或导出真实邮箱。</span></div>
        </div>
        <article v-if="allocationResult" class="message is-info pool-manager__import-result" data-cy="pool-import-result"><div class="message-header"><p>分配完成</p><button type="button" class="delete" aria-label="关闭" @click="allocationResult = null" /></div><div class="message-body"><div class="pool-manager__import-stats"><span><b>{{ allocationResult.created || 0 }}</b> 新分配</span><span><b>{{ allocationResult.reactivated || 0 }}</b> 恢复</span><span><b>{{ allocationResult.alreadyAssigned || 0 }}</b> 已存在</span><span><b>{{ allocationResult.unmatched || 0 }}</b> 未匹配</span><span><b>{{ allocationResult.ambiguous || 0 }}</b> 编码+邮箱重复</span><span><b>{{ allocationResult.invalid || 0 }}</b> 格式无效</span></div><p v-if="allocationResult.duplicates">文件内重复行已自动去重 {{ allocationResult.duplicates }} 条。</p><div v-if="allocationResult.issues && allocationResult.issues.length" class="pool-manager__import-issues"><strong>需要修正的行（最多展示 50 条）</strong><ul><li v-for="issue in allocationResult.issues.slice(0, 50)" :key="`${issue.row}-${issue.reason}`">第 {{ issue.row }} 行 · {{ issue.customerCode || issue.customer_code || '-' }} · {{ importReason(issue.reason) }}</li></ul></div></div></article>
        <b-collapse v-model="showManualMaintenance" class="pool-manager__manual-maintenance" animation="slide">
          <template #trigger="props"><a class="pool-manager__manual-trigger"><b-icon :icon="props.open ? 'chevron-down' : 'chevron-right'" size="is-small" />单条维护（查询、移除或清空无效邮箱）</a></template>
          <div class="pool-manager__manual-body">
            <div class="pool-manager__table-toolbar">
              <form class="pool-manager__search" @submit.prevent="loadContacts">
                <b-input v-model="customerCode" expanded placeholder="输入客户编码查询" />
                <b-button native-type="submit" type="is-primary" icon-left="magnify">查询</b-button>
              </form>
              <div class="pool-manager__table-meta"><span>查询结果</span><strong>{{ contacts.length }}</strong><small>条</small></div>
            </div>
            <p class="help pool-manager__table-help">仅显示客户编码、公司名称和脱敏邮箱。勾选联系人后，可执行本组织移除、恢复分配或清空无效邮箱。</p>
            <b-table :data="contacts" :loading="loadingContacts" checkable :checked-rows.sync="selectedContacts" :mobile-cards="false" narrowed hoverable class="pool-manager__contact-table">
              <b-table-column v-slot="props" field="customer_code" label="客户编码"><span class="pool-manager__cell-code">{{ props.row.customerCode || props.row.customer_code || '-' }}</span></b-table-column>
              <b-table-column v-slot="props" field="company_name" label="公司名称"><span class="pool-manager__cell-company">{{ props.row.companyName || props.row.company_name || '-' }}</span></b-table-column>
              <b-table-column v-slot="props" field="email" label="邮箱"><span class="pool-manager__cell-email" :title="props.row.email || '-'">{{ props.row.email || '-' }}</span></b-table-column>
              <b-table-column v-slot="props" field="status" label="状态"><b-tag rounded size="is-small" :type="contactStatusType(props.row)">{{ contactStatusLabel(props.row) }}</b-tag></b-table-column>
              <b-table-column v-if="isPlatformAdmin" v-slot="props" label="组织剔除标记"><div v-if="props.row.exclusions && props.row.exclusions.length" class="pool-manager__exclusion-tags"><b-tag v-for="item in props.row.exclusions" :key="`${props.row.id}-${item.organizationId || item.organization_id}`" rounded size="is-small" type="is-warning">{{ item.organizationName || item.organizationId }}：{{ item.reason || '已移除' }}</b-tag></div><span v-else class="pool-manager__muted">—</span></b-table-column>
              <template #empty><div class="pool-manager__table-empty"><b-icon icon="account-search-outline" size="is-medium" /><strong>{{ customerCode ? '未找到匹配联系人' : '请输入客户编码开始查询' }}</strong><span>{{ customerCode ? '请确认客户编码后重试' : '公海联系人较多，请使用客户编码定位记录' }}</span></div></template>
            </b-table>
            <div class="pool-manager__contact-actions"><div class="buttons"><b-button size="is-small" type="is-danger" :disabled="!selectedContacts.length" @click="removeSelected">移除（仅本组织）</b-button><b-button size="is-small" type="is-light" :disabled="!selectedContacts.length" @click="restoreSelected">恢复分配</b-button><b-button v-if="canManageSegments" size="is-small" type="is-light" :disabled="!selectedContacts.length" @click="clearSelectedEmails">清空无效邮箱</b-button></div><b-field label="移除原因" label-position="on-border" class="pool-manager__remove-reason"><b-input v-model="removeReason" size="is-small" placeholder="可选，例如客户明确拒收" /></b-field></div>
          </div>
        </b-collapse>
      </div>
    </section>
  </section>
</template>

<script>
/* eslint-disable vue/max-len */
import Vue from 'vue';
import { mapState } from 'vuex';
import * as XLSX from 'xlsx';

export default Vue.extend({
  name: 'PoolManager',
  props: { pool: { type: Object, required: true } },
  data() {
    return {
      contacts: [], segments: [], targetOrganizationID: null, selectedContacts: [], selectedSegmentID: null, customerCode: '', replyMailboxes: [], replyMailboxID: null, loadingContacts: false, creatingSegment: false, importingAllocation: false, allocationFile: null, allocationResult: null, showManualMaintenance: false, newSegment: { name: '', replyMailboxID: null }, removeReason: '手动移除',
    };
  },
  computed: {
    ...mapState(['profile', 'workspace', 'organizations']),
    isPlatformAdmin() { return Number(this.profile && this.profile.userRole && this.profile.userRole.id) === 1; },
    organizationID() { return this.isPlatformAdmin ? Number(this.targetOrganizationID) || 0 : Number(this.workspace && this.workspace.organizationId) || 0; },
    targetOrganizationName() { const org = this.organizations.find((item) => Number(item.id) === this.organizationID); return org ? org.name : `组织 ${this.organizationID}`; },
    selectedSegment() { return this.segments.find((segment) => Number(segment.id) === Number(this.selectedSegmentID)); },
    canManageSegments() { return this.isPlatformAdmin || Boolean(this.workspace && this.workspace.organizationId && this.workspace.role === 'manager'); },
    canCreateSegment() { return this.canManageSegments && !this.selectedSegment; },
    isAllocationReady() { return Boolean(this.organizationID && this.selectedSegment); },
  },
  watch: { selectedSegmentID() { this.replyMailboxID = this.selectedSegment ? this.selectedSegment.replyMailboxId : null; }, targetOrganizationID() { this.selectedSegmentID = null; this.contacts = []; this.selectedContacts = []; this.loadTargetOrganization(); } },
  methods: {
    loadContacts() {
      const customerCode = this.customerCode.trim();
      if (!customerCode) {
        this.contacts = [];
        this.selectedContacts = [];
        return Promise.resolve();
      }
      this.loadingContacts = true;
      return this.$api.getPoolContacts(this.pool.id, { customer_code: customerCode })
        .then((rows) => { this.contacts = Array.isArray(rows) ? rows : []; })
        .finally(() => { this.loadingContacts = false; });
    },
    loadSegments() { return this.$api.getPoolSegments(this.pool.id).then((rows) => { this.segments = Array.isArray(rows) ? rows : []; const current = this.segments.find((segment) => Number(segment.organizationId || segment.organization_id) === this.organizationID); this.selectedSegmentID = current ? current.id : null; }); },
    loadTargetOrganization() {
      if (!this.organizationID) { this.replyMailboxes = []; return Promise.resolve(); }
      const mailboxes = this.isPlatformAdmin
        ? this.$api.getPoolManagementTarget(this.pool.id, this.organizationID).then(() => { this.replyMailboxes = []; })
        : this.$api.getReplyMailboxes().then((rows) => { this.replyMailboxes = Array.isArray(rows) ? rows : []; });
      return mailboxes.then(() => this.loadSegments());
    },
    downloadAllocationTemplate() {
      const rows = [['customer_code', 'email'], ['CUSTOMER-001', 'contact@example.com']];
      const worksheet = XLSX.utils.aoa_to_sheet(rows);
      const workbook = XLSX.utils.book_new();
      XLSX.utils.book_append_sheet(workbook, worksheet, 'Allocation');
      const xlsxData = new Uint8Array(XLSX.write(workbook, { bookType: 'xlsx', type: 'array' }));
      const csvData = new TextEncoder().encode(`\uFEFF${rows.map((row) => row.join(',')).join('\r\n')}\r\n`);
      const zipData = this.createZip([
        { name: 'pool-segment-allocation-template.csv', data: csvData },
        { name: 'pool-segment-allocation-template.xlsx', data: xlsxData },
      ]);
      const blob = new Blob([zipData], { type: 'application/zip' });
      const url = window.URL.createObjectURL(blob);
      const link = document.createElement('a'); link.href = url; link.download = 'pool-segment-allocation-templates.zip'; document.body.appendChild(link); link.click(); link.remove(); window.setTimeout(() => window.URL.revokeObjectURL(url), 1000);
    },
    /* eslint-disable no-bitwise */
    createZip(entries) {
      const encoder = new TextEncoder();
      const files = entries.map((entry) => ({ ...entry, nameData: encoder.encode(entry.name), data: entry.data instanceof Uint8Array ? entry.data : new Uint8Array(entry.data) }));
      const centralSize = files.reduce((size, file) => size + 46 + file.nameData.length, 0);
      const localSize = files.reduce((size, file) => size + 30 + file.nameData.length + file.data.length, 0);
      const output = new Uint8Array(localSize + centralSize + 22);
      const view = new DataView(output.buffer);
      const crc32 = (bytes) => {
        let crc = 0xFFFFFFFF;
        bytes.forEach((byte) => {
          crc ^= byte;
          for (let bit = 0; bit < 8; bit += 1) crc = (crc >>> 1) ^ ((crc & 1) ? 0xEDB88320 : 0);
        });
        return (crc ^ 0xFFFFFFFF) >>> 0;
      };
      const centralEntries = [];
      let offset = 0;
      files.forEach((file) => {
        const checksum = crc32(file.data);
        const localOffset = offset;
        view.setUint32(offset, 0x04034B50, true); view.setUint16(offset + 4, 20, true); view.setUint16(offset + 6, 0, true); view.setUint16(offset + 8, 0, true);
        view.setUint16(offset + 10, 0, true); view.setUint16(offset + 12, 0, true); view.setUint32(offset + 14, checksum, true); view.setUint32(offset + 18, file.data.length, true); view.setUint32(offset + 22, file.data.length, true);
        view.setUint16(offset + 26, file.nameData.length, true); view.setUint16(offset + 28, 0, true); output.set(file.nameData, offset + 30); output.set(file.data, offset + 30 + file.nameData.length);
        offset += 30 + file.nameData.length + file.data.length;
        centralEntries.push({ file, checksum, localOffset });
      });
      const centralOffset = offset;
      centralEntries.forEach(({ file, checksum, localOffset }) => {
        view.setUint32(offset, 0x02014B50, true); view.setUint16(offset + 4, 20, true); view.setUint16(offset + 6, 20, true); view.setUint16(offset + 8, 0, true); view.setUint16(offset + 10, 0, true);
        view.setUint16(offset + 12, 0, true); view.setUint16(offset + 14, 0, true); view.setUint32(offset + 16, checksum, true); view.setUint32(offset + 20, file.data.length, true); view.setUint32(offset + 24, file.data.length, true);
        view.setUint16(offset + 28, file.nameData.length, true); view.setUint16(offset + 30, 0, true); view.setUint16(offset + 32, 0, true); view.setUint16(offset + 34, 0, true); view.setUint16(offset + 36, 0, true); view.setUint32(offset + 38, 0, true); view.setUint32(offset + 42, localOffset, true);
        output.set(file.nameData, offset + 46); offset += 46 + file.nameData.length;
      });
      view.setUint32(offset, 0x06054B50, true); view.setUint16(offset + 4, 0, true); view.setUint16(offset + 6, 0, true); view.setUint16(offset + 8, files.length, true); view.setUint16(offset + 10, files.length, true);
      view.setUint32(offset + 12, centralSize, true); view.setUint32(offset + 16, centralOffset, true); view.setUint16(offset + 20, 0, true);
      return output;
    },
    /* eslint-enable no-bitwise */
    contactStatusLabel(contact) {
      if (contact.excluded) return '已移除（本组织）';
      return { active: '正常', archived: '已归档' }[contact.status] || '正常';
    },
    contactStatusType(contact) {
      if (contact.excluded) return 'is-warning';
      return contact.status === 'archived' ? 'is-dark' : 'is-success';
    },
    importReason(reason) {
      const labels = { invalid: '客户编码或邮箱为空', not_found: '一级公海中未找到匹配记录', ambiguous: '同编码同邮箱对应多条记录，无法自动选择' };
      return labels[reason] || reason;
    },
    importAllocationFile() {
      if (!this.allocationFile || !this.selectedSegmentID) return Promise.resolve();
      this.importingAllocation = true;
      return this.$api.importPoolSegmentMembers(this.selectedSegmentID, this.allocationFile)
        .then((result) => { this.allocationResult = result; this.allocationFile = null; return this.refresh(); })
        .finally(() => { this.importingAllocation = false; });
    },
    selectedIDs() { return this.selectedContacts.map((contact) => Number(contact.id)); },
    assignSelected() { return Promise.all(this.selectedIDs().map((contactID) => this.$api.assignPoolContact({ segment_id: this.selectedSegmentID, contact_id: contactID }))).then(() => this.refresh()); },
    removeSelected() { const reason = this.removeReason || '手动移除'; return Promise.all(this.selectedIDs().map((contactID) => this.$api.removePoolContact({ segment_id: this.selectedSegmentID, contact_id: contactID, reason }))).then(() => this.refresh()); },
    restoreSelected() { return Promise.all(this.selectedIDs().map((contactID) => this.$api.restorePoolContact({ segment_id: this.selectedSegmentID, contact_id: contactID }))).then(() => this.refresh()); },
    clearSelectedEmails() { return Promise.all(this.selectedIDs().map((contactID) => this.$api.clearPoolContactEmail(this.pool.id, contactID))).then(() => this.refresh()); },
    saveReplyMailbox() { return this.$api.updatePoolSegmentReplyMailbox(this.selectedSegmentID, this.replyMailboxID).then(() => this.loadSegments()); },
    createSegment() {
      this.creatingSegment = true; return this.$api.createPoolSegment({
        pool_id: this.pool.id, organization_id: this.organizationID, name: this.newSegment.name, ...(this.isPlatformAdmin ? {} : { reply_mailbox_id: this.newSegment.replyMailboxID || null }),
      }).then(() => { this.newSegment = { name: '', replyMailboxID: null }; this.$utils.toast('二级列表已创建并绑定'); return this.refresh(); }).finally(() => { this.creatingSegment = false; });
    },
    refresh() { this.selectedContacts = []; return Promise.all([this.loadContacts(), this.loadTargetOrganization()]); },
  },
  mounted() { this.refresh(); },
});
</script>
