<template>
  <form @submit.prevent="onSubmit">
    <div class="modal-card content" style="width: auto">
      <header class="modal-card-head">
        <h4>{{ $t('organizations.bulkImportTitle') }}</h4>
        <b-button icon-left="download" type="is-light" @click.prevent="downloadTemplate">
          {{ $t('organizations.downloadTemplate') }}
        </b-button>
      </header>

      <section class="modal-card-body">
        <b-message type="is-info" :closable="false">
          {{ $t('organizations.bulkImportFormat') }}
        </b-message>

        <b-field :label="$t('organizations.bulkImportFile')" label-position="on-border">
          <b-upload v-model="file" drag-drop expanded
            accept=".csv,.xlsx,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet">
            <div class="has-text-centered section">
              <p><b-icon icon="file-upload-outline" size="is-large" /></p>
              <p>{{ $t('organizations.bulkImportFileHelp') }}</p>
            </div>
          </b-upload>
        </b-field>

        <div v-if="file" class="tags mb-4">
          <b-tag size="is-medium" closable @close="clearFile">{{ file.name }}</b-tag>
        </div>

        <b-message v-if="parseError" type="is-danger" :closable="false">{{ parseError }}</b-message>

        <div v-if="rows.length > 0" class="organization-import-preview">
          <h5 class="title is-size-6">{{ $t('organizations.bulkImportPreview') }} ({{ rows.length }})</h5>
          <b-table :data="rows" :mobile-cards="false" striped hoverable>
            <b-table-column v-slot="props" field="line" :label="$t('organizations.bulkImportRow')" numeric>
              {{ props.row.line }}
            </b-table-column>
            <b-table-column v-slot="props" field="account" :label="$t('organizations.memberAccount')">
              {{ props.row.account }}
            </b-table-column>
            <b-table-column v-slot="props" field="role" :label="$t('organizations.organizationRole')">
              {{ roleLabel(props.row.role) }}
            </b-table-column>
            <b-table-column v-slot="props" field="errors" :label="$t('organizations.bulkImportErrors')">
              <b-tag v-for="issue in props.row.errors" :key="issue" type="is-danger" class="mr-1 mb-1">
                {{ issueMessage(issue) }}
              </b-tag>
              <span v-if="props.row.errors.length === 0">—</span>
            </b-table-column>
          </b-table>
        </div>
      </section>

      <footer class="modal-card-foot has-text-right">
        <b-button @click="$parent.close()">{{ $t('globals.buttons.close') }}</b-button>
        <b-button native-type="submit" type="is-primary" icon-left="account-multiple-plus"
          :loading="isImporting || isParsing" :disabled="rows.length === 0 || !allRowsValid">
          {{ $t('organizations.bulkImport') }}
        </b-button>
      </footer>
    </div>
  </form>
</template>

<script>
import Vue from 'vue';
import Papa from 'papaparse';
import * as XLSX from 'xlsx';

const columns = ['account', 'role'];

export default Vue.extend({
  name: 'OrganizationMemberBulkImport',

  props: {
    organizationId: { type: Number, required: true },
  },

  data() {
    return {
      file: null,
      rows: [],
      parseError: '',
      isParsing: false,
      isImporting: false,
    };
  },

  computed: {
    allRowsValid() {
      return this.rows.length > 0 && this.rows.every((row) => row.errors.length === 0);
    },
  },

  watch: {
    file(file) {
      if (!file) {
        this.rows = [];
        this.parseError = '';
        return;
      }
      this.parseFile(file);
    },
  },

  methods: {
    downloadTemplate() {
      const worksheet = XLSX.utils.aoa_to_sheet([columns]);
      worksheet['!cols'] = [{ wch: 32 }, { wch: 16 }];
      const workbook = XLSX.utils.book_new();
      XLSX.utils.book_append_sheet(workbook, worksheet, 'Members');
      XLSX.writeFile(workbook, 'organization-members-import-template.xlsx');
    },

    clearFile() {
      this.file = null;
    },

    parseCSVRows(file) {
      return new Promise((resolve, reject) => {
        Papa.parse(file, {
          skipEmptyLines: true,
          complete: (result) => {
            if (result.errors && result.errors.length > 0) {
              reject(new Error(result.errors[0].message));
              return;
            }
            resolve(result.data || []);
          },
          error: reject,
        });
      });
    },

    async parseXLSXRows(file) {
      const workbook = XLSX.read(await file.arrayBuffer(), { type: 'array' });
      const firstSheet = workbook.SheetNames[0];
      return firstSheet ? XLSX.utils.sheet_to_json(workbook.Sheets[firstSheet], {
        header: 1,
        raw: false,
        blankrows: false,
      }) : [];
    },

    normalizeHeader(value) {
      return String(value || '').trim().toLowerCase().replace(/[\s-]+/g, '_');
    },

    stringValue(value) {
      return value === undefined || value === null ? '' : String(value);
    },

    normalizeRole(value) {
      const role = String(value || '').trim().toLowerCase();
      if (role === '管理员' || role === '组织管理员') return 'manager';
      if (role === '成员' || role === '普通成员') return 'member';
      return role || 'member';
    },

    validateRow(row) {
      const errors = [];
      if (!row.account.trim()) errors.push('missing_account');
      if (row.role !== 'member' && row.role !== 'manager') errors.push('invalid_role');
      return errors;
    },

    buildRows(source) {
      if (!source || source.length < 2) throw new Error(this.$t('organizations.bulkImportNoRows'));
      const headers = source[0].map(this.normalizeHeader);
      let accountIndex = headers.indexOf('account');
      if (accountIndex < 0) accountIndex = headers.indexOf('username');
      if (accountIndex < 0) accountIndex = headers.indexOf('email');
      const roleIndex = headers.indexOf('role');
      if (accountIndex < 0 || roleIndex < 0) throw new Error(this.$t('organizations.bulkImportMissingColumns'));

      return source.slice(1).reduce((out, sourceRow, index) => {
        const account = this.stringValue(sourceRow[accountIndex]).trim();
        const role = this.normalizeRole(sourceRow[roleIndex]);
        if (!account && !String(sourceRow[roleIndex] || '').trim()) return out;
        const row = {
          line: index + 2, account, role, errors: [],
        };
        row.errors = this.validateRow(row);
        out.push(row);
        return out;
      }, []);
    },

    async parseFile(file) {
      this.isParsing = true;
      this.parseError = '';
      this.rows = [];
      try {
        const fileName = String(file.name || '').toLowerCase();
        let source = null;
        if (fileName.endsWith('.xlsx')) source = await this.parseXLSXRows(file);
        if (fileName.endsWith('.csv')) source = await this.parseCSVRows(file);
        if (!source) throw new Error(this.$t('organizations.bulkImportInvalidFile'));
        const rows = this.buildRows(source);
        if (rows.length === 0) throw new Error(this.$t('organizations.bulkImportNoRows'));
        this.rows = rows;
      } catch (error) {
        this.parseError = error.message || String(error);
      } finally {
        this.isParsing = false;
      }
    },

    roleLabel(role) {
      return role === 'manager' ? this.$t('organizations.roleManager') : this.$t('organizations.roleMember');
    },

    issueMessage(code) {
      return this.$t(`organizations.bulkImportError.${code}`);
    },

    applyServerErrors(issues) {
      const issuesByLine = issues.reduce((out, issue) => ({
        ...out,
        [issue.row]: [...(out[issue.row] || []), issue.code],
      }), {});
      this.rows = this.rows.map((row) => ({
        ...row,
        errors: [...new Set([...(row.errors || []), ...(issuesByLine[row.line] || [])])],
      }));
    },

    async onSubmit() {
      this.isImporting = true;
      try {
        const result = await this.$api.addOrganizationMembersBulk(this.organizationId, {
          members: this.rows.map((row) => ({ account: row.account, role: row.role })),
        });
        if (result.errors && result.errors.length > 0) {
          this.applyServerErrors(result.errors);
          this.$utils.toast(this.$t('organizations.bulkImportValidationFailed'), 'is-danger');
          return;
        }
        this.$emit('finished');
        this.$utils.toast(this.$t('organizations.bulkImportSuccess', { count: result.added }));
        this.$parent.close();
      } finally {
        this.isImporting = false;
      }
    },
  },
});
</script>

<style scoped>
.organization-import-preview {
  overflow-x: auto;
}
</style>
