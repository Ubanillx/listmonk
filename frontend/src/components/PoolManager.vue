<!-- eslint-disable vue/max-len -->
<template>
  <section class="pool-manager">
    <export-button v-if="selectedSegment && organizationID === Number(workspace.organizationId)" kind="pools"
      :filters="{ search: customerCode, list_ids: [selectedSegment.listId || selectedSegment.list_id] }" />
    <p v-else-if="selectedSegment" class="help">{{ $t('pool.exportHint') }}</p>
    <div class="pool-manager__intro"><div><h3>{{ $t('pool.title') }}</h3><p>{{ $t('pool.intro') }}</p></div><b-tag type="is-info" class="is-light">{{ $t('pool.primaryTag') }}</b-tag></div>
    <section class="pool-manager__section" data-cy="pool-target-organization-panel">
      <div class="pool-manager__section-heading"><span class="pool-manager__section-number">1</span><div><h4>{{ $t('pool.stepSelectTitle') }}</h4><p>{{ $t('pool.stepSelectHelp') }}</p></div></div>
      <div class="pool-manager__section-content">
        <b-field v-if="isPlatformAdmin" :label="$t('pool.targetOrganizationLabel')" label-position="on-border"><b-select v-model.number="targetOrganizationID" expanded data-cy="pool-target-organization"><option :value="null">{{ $t('pool.selectOrganizationPlaceholder') }}</option><option v-for="organization in organizations" :key="organization.id" :value="organization.id">{{ organization.name }}</option></b-select></b-field>
        <div v-else class="pool-manager__selected-organization"><span>{{ $t('pool.currentOrganization') }}</span><strong>{{ targetOrganizationName }}</strong></div>
      </div>
    </section>
    <section v-if="organizationID" class="pool-manager__section" data-cy="pool-secondary-list-panel">
      <div class="pool-manager__section-heading"><span class="pool-manager__section-number">2</span><div><h4>{{ $t('pool.secondaryTitle') }}</h4><p>{{ $t('pool.secondaryHelp') }}</p></div></div>
      <div class="pool-manager__section-content">
        <div v-if="selectedSegment" class="pool-manager__segment-summary" data-cy="pool-segment-summary"><div><span>{{ $t('pool.segmentNameLabel') }}</span><strong>{{ selectedSegment.listName || selectedSegment.listId }}</strong></div><div><span>{{ $t('pool.segmentOrganizationLabel') }}</span><strong>{{ selectedSegment.organizationName || targetOrganizationName }}</strong></div><div v-if="!isPlatformAdmin"><span>{{ $t('pool.segmentMailboxLabel') }}</span><strong>{{ selectedSegment.replyMailboxEmail || $t('pool.notConfigured') }}</strong></div></div>
        <div v-if="selectedSegment && isPlatformAdmin" class="pool-manager__organization-note"><b-icon icon="information-outline" size="is-small" /><span>{{ $t('pool.mailboxPlatformNote') }}</span></div>
        <div v-if="selectedSegment && !isPlatformAdmin" class="pool-manager__reply-mailbox"><b-field :label="$t('pool.unifiedMailboxLabel')" label-position="on-border"><b-select v-model="replyMailboxID" expanded :disabled="!canManageSegments" data-cy="pool-reply-mailbox"><option :value="null">{{ $t('pool.mailboxRequiredOption') }}</option><option v-for="mailbox in replyMailboxes" :key="mailbox.id" :value="mailbox.id">{{ mailbox.name || mailbox.email }}（{{ mailbox.email }}）</option></b-select></b-field><div class="pool-manager__inline-action"><b-button size="is-small" type="is-primary" :disabled="!canManageSegments" @click="saveReplyMailbox">{{ $t('pool.saveMailbox') }}</b-button><p class="help">{{ $t('pool.mailboxHelp') }}</p></div></div>
        <div v-if="!selectedSegment && canCreateSegment" class="pool-manager__create-segment" data-cy="pool-segment-create"><div class="pool-manager__create-segment-title"><div><span>{{ $t('pool.targetOrganizationLabel') }}</span><strong>{{ targetOrganizationName }}</strong></div><small>{{ $t('pool.createBindsPool') }}</small></div><b-field :label="$t('pool.createNameLabel')" label-position="on-border"><b-input v-model.trim="newSegment.name" maxlength="200" :placeholder="$t('pool.createNamePlaceholder')" data-cy="pool-segment-name" /></b-field><b-field v-if="!isPlatformAdmin" :label="$t('pool.unifiedMailboxLabel')" label-position="on-border"><b-select v-model="newSegment.replyMailboxID" expanded data-cy="pool-segment-reply-mailbox"><option :value="null">{{ $t('pool.mailboxOptionalOption') }}</option><option v-for="mailbox in replyMailboxes" :key="mailbox.id" :value="mailbox.id">{{ mailbox.name || mailbox.email }}（{{ mailbox.email }}）</option></b-select></b-field><p v-if="isPlatformAdmin" class="help pool-manager__organization-note-text">{{ $t('pool.createPlatformNote') }}</p><b-button type="is-primary" :loading="creatingSegment" :disabled="!newSegment.name" data-cy="create-pool-segment" @click="createSegment">{{ $t('pool.createAndBind') }}</b-button></div>
        <div v-else-if="!selectedSegment" class="pool-manager__empty-state" data-cy="pool-secondary-list-empty"><b-icon icon="playlist-check" size="is-medium" /><div><strong>{{ $t('pool.emptyTitle') }}</strong><p>{{ $t('pool.emptyHelp') }}</p></div></div>
      </div>
    </section>
    <section v-else class="pool-manager__empty-state pool-manager__empty-state--top" data-cy="pool-secondary-list-empty"><b-icon icon="account-group-outline" size="is-medium" /><div><strong>{{ $t('pool.noOrgTitle') }}</strong><p>{{ $t('pool.noOrgHelp') }}</p></div></section>
    <section v-if="isAllocationReady" class="pool-manager__section pool-manager__allocation" data-cy="pool-contact-allocation">
      <div class="pool-manager__section-heading"><span class="pool-manager__section-number">3</span><div><h4>{{ $t('pool.importTitle') }}</h4><p>{{ $t('pool.importHelp') }}</p></div></div>
      <div class="pool-manager__section-content">
        <div class="pool-manager__import-card">
          <div class="pool-manager__import-card-header"><div><strong>{{ $t('pool.importFileLabel') }}</strong><p>{{ $t('pool.importFileHelp') }}</p></div><b-button size="is-small" type="is-light" icon-left="download" @click="downloadAllocationTemplate">{{ $t('pool.downloadTemplate') }}</b-button></div>
          <b-upload v-model="allocationFile" drag-drop expanded accept=".csv,.xlsx,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" :disabled="importingAllocation">
            <div class="content has-text-centered"><b-icon icon="file-upload-outline" size="is-large" /><p>{{ allocationFile ? allocationFile.name : $t('pool.uploadHint') }}</p><small>{{ $t('pool.uploadHintSmall') }}</small></div>
          </b-upload>
          <div class="pool-manager__import-actions"><b-button type="is-primary" icon-left="account-multiple-plus" :loading="importingAllocation" :disabled="!allocationFile" @click="importAllocationFile">{{ $t('pool.startAllocation') }}</b-button><span class="help">{{ $t('pool.uploadPrivacy') }}</span></div>
        </div>
        <article v-if="allocationResult" class="message is-info pool-manager__import-result" data-cy="pool-import-result"><div class="message-header"><p>{{ $t('pool.allocComplete') }}</p><button type="button" class="delete" :aria-label="$t('globals.buttons.close')" @click="allocationResult = null" /></div><div class="message-body"><div class="pool-manager__import-stats"><span><b>{{ allocationResult.created || 0 }}</b> {{ $t('pool.allocNew') }}</span><span><b>{{ allocationResult.reactivated || 0 }}</b> {{ $t('pool.allocReactivated') }}</span><span><b>{{ allocationResult.alreadyAssigned || 0 }}</b> {{ $t('pool.allocExisting') }}</span><span><b>{{ allocationResult.unmatched || 0 }}</b> {{ $t('pool.allocUnmatched') }}</span><span><b>{{ allocationResult.ambiguous || 0 }}</b> {{ $t('pool.allocAmbiguous') }}</span><span><b>{{ allocationResult.invalid || 0 }}</b> {{ $t('pool.allocInvalid') }}</span></div><p v-if="allocationResult.duplicates">{{ $t('pool.allocDedup', { count: allocationResult.duplicates }) }}</p><div v-if="allocationResult.issues && allocationResult.issues.length" class="pool-manager__import-issues"><strong>{{ $t('pool.allocIssuesTitle') }}</strong><ul><li v-for="issue in allocationResult.issues.slice(0, 50)" :key="`${issue.row}-${issue.reason}`">{{ $t('pool.allocIssueRow', { row: issue.row, code: issue.customerCode || issue.customer_code || '-', reason: importReason(issue.reason) }) }}</li></ul></div></div></article>
        <b-collapse v-model="showManualMaintenance" class="pool-manager__manual-maintenance" animation="slide">
          <template #trigger="props"><a class="pool-manager__manual-trigger"><b-icon :icon="props.open ? 'chevron-down' : 'chevron-right'" size="is-small" />{{ $t('pool.manualToggle') }}</a></template>
          <div class="pool-manager__manual-body">
            <div class="pool-manager__table-toolbar">
              <form class="pool-manager__search" @submit.prevent="loadContacts">
                <b-input v-model="customerCode" expanded :placeholder="$t('pool.searchPlaceholder')" />
                <b-button native-type="submit" type="is-primary" icon-left="magnify">{{ $t('pool.search') }}</b-button>
              </form>
              <div class="pool-manager__table-meta"><span>{{ $t('pool.searchResults') }}</span><strong>{{ contacts.length }}</strong><small>{{ $t('pool.countUnit') }}</small></div>
            </div>
            <p class="help pool-manager__table-help">{{ $t('pool.tableHelp') }}</p>
            <b-table :data="contacts" :loading="loadingContacts" checkable :checked-rows.sync="selectedContacts" :mobile-cards="false" hoverable class="pool-manager__contact-table">
              <b-table-column v-slot="props" field="customer_code" :label="$t('pool.tableCustomerCode')"><span class="pool-manager__cell-code">{{ props.row.customerCode || props.row.customer_code || '-' }}</span></b-table-column>
              <b-table-column v-slot="props" field="company_name" :label="$t('pool.tableCompanyName')"><span class="pool-manager__cell-company">{{ props.row.companyName || props.row.company_name || '-' }}</span></b-table-column>
              <b-table-column v-slot="props" field="email" :label="$t('pool.tableEmail')"><span class="pool-manager__cell-email" :title="props.row.email || '-'">{{ props.row.email || '-' }}</span></b-table-column>
              <b-table-column v-slot="props" field="status" :label="$t('pool.tableStatus')"><b-tag rounded size="is-small" :type="contactStatusType(props.row)">{{ contactStatusLabel(props.row) }}</b-tag></b-table-column>
              <b-table-column v-if="!isPlatformAdmin" v-slot="props" :label="$t('pool.tableRemoveReason')">{{ props.row.excluded ? (props.row.exclusionReason || props.row.exclusion_reason || '—') : '—' }}</b-table-column>
              <b-table-column v-if="isPlatformAdmin" v-slot="props" :label="$t('pool.tableExclusions')"><div v-if="props.row.exclusions && props.row.exclusions.length" class="pool-manager__exclusion-tags"><b-tag v-for="item in props.row.exclusions" :key="`${props.row.id}-${item.organizationId || item.organization_id}`" rounded size="is-small" type="is-warning">{{ item.organizationName || item.organizationId }}：{{ item.reason || $t('pool.statusRemoved') }}</b-tag></div><span v-else class="pool-manager__muted">—</span></b-table-column>
              <template #empty><div class="pool-manager__table-empty"><b-icon icon="account-search-outline" size="is-medium" /><strong>{{ customerCode ? $t('pool.emptyNoMatchTitle') : $t('pool.emptyNoQueryTitle') }}</strong><span>{{ customerCode ? $t('pool.emptyNoMatchHelp') : $t('pool.emptyNoQueryHelp') }}</span></div></template>
            </b-table>
            <p class="help">{{ $t('pool.removeToPrivateHelp') }}</p>
            <div class="pool-manager__contact-actions"><div class="buttons"><b-button size="is-small" type="is-danger" :disabled="!selectedContacts.length" @click="removeSelected">{{ $t('pool.removeSelected') }}</b-button><b-button size="is-small" type="is-light" :disabled="!selectedContacts.length" @click="restoreSelected">{{ $t('pool.restoreSelected') }}</b-button><b-button v-if="canManageSegments" size="is-small" type="is-light" :disabled="!selectedContacts.length" @click="clearSelectedEmails">{{ $t('pool.clearSelected') }}</b-button></div><b-field :label="$t('pool.removeReasonLabel')" label-position="on-border" class="pool-manager__remove-reason"><b-input v-model.trim="removeReason" size="is-small" :placeholder="$t('pool.removeReasonPlaceholder')" /></b-field></div>
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
      contacts: [], segments: [], targetOrganizationID: null, selectedContacts: [], selectedSegmentID: null, customerCode: '', replyMailboxes: [], replyMailboxID: null, loadingContacts: false, creatingSegment: false, importingAllocation: false, allocationFile: null, allocationResult: null, showManualMaintenance: false, newSegment: { name: '', replyMailboxID: null }, removeReason: '',
    };
  },
  computed: {
    ...mapState(['profile', 'workspace', 'organizations']),
    isPlatformAdmin() { return Number(this.profile && this.profile.userRole && this.profile.userRole.id) === 1; },
    organizationID() { return this.isPlatformAdmin ? Number(this.targetOrganizationID) || 0 : Number(this.workspace && this.workspace.organizationId) || 0; },
    targetOrganizationName() { const org = this.organizations.find((item) => Number(item.id) === this.organizationID); return org ? org.name : this.$t('pool.organizationFallback', { id: this.organizationID }); },
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
      if (contact.excluded) return this.$t('pool.statusRemoved');
      return { active: this.$t('pool.statusNormal'), archived: this.$t('pool.statusArchived') }[contact.status] || this.$t('pool.statusNormal');
    },
    contactStatusType(contact) {
      if (contact.excluded) return 'is-warning';
      return contact.status === 'archived' ? 'is-dark' : 'is-success';
    },
    importReason(reason) {
      const labels = { invalid: this.$t('pool.reasonInvalid'), not_found: this.$t('pool.reasonNotFound'), ambiguous: this.$t('pool.reasonAmbiguous') };
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
    removeSelected() { const reason = this.removeReason || this.$t('pool.defaultRemoveReason'); return Promise.all(this.selectedIDs().map((contactID) => this.$api.removePoolContact({ segment_id: this.selectedSegmentID, contact_id: contactID, reason }))).then(() => this.refresh()); },
    restoreSelected() { return Promise.all(this.selectedIDs().map((contactID) => this.$api.restorePoolContact({ segment_id: this.selectedSegmentID, contact_id: contactID }))).then(() => this.refresh()); },
    clearSelectedEmails() { return Promise.all(this.selectedIDs().map((contactID) => this.$api.clearPoolContactEmail(this.pool.id, contactID))).then(() => this.refresh()); },
    saveReplyMailbox() { return this.$api.updatePoolSegmentReplyMailbox(this.selectedSegmentID, this.replyMailboxID).then(() => this.loadSegments()); },
    createSegment() {
      this.creatingSegment = true; return this.$api.createPoolSegment({
        pool_id: this.pool.id, organization_id: this.organizationID, name: this.newSegment.name, ...(this.isPlatformAdmin ? {} : { reply_mailbox_id: this.newSegment.replyMailboxID || null }),
      }).then(() => { this.newSegment = { name: '', replyMailboxID: null }; this.$utils.toast(this.$t('pool.toastCreated')); return this.refresh(); }).finally(() => { this.creatingSegment = false; });
    },
    refresh() { this.selectedContacts = []; return Promise.all([this.loadContacts(), this.loadTargetOrganization()]); },
  },
  mounted() { this.removeReason = this.$t('pool.defaultRemoveReason'); this.refresh(); },
});
</script>
