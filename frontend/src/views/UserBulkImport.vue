<template>
  <form @submit.prevent="onSubmit">
    <div class="modal-card content" style="width: auto">
      <header class="modal-card-head">
        <h4>{{ $t('users.bulkImportTitle') }}</h4>
        <b-button icon-left="download" type="is-light" @click="downloadTemplate" data-cy="btn-user-import-template">
          {{ $t('users.downloadTemplate') }}
        </b-button>
      </header>

      <section class="modal-card-body">
        <b-message type="is-info" :closable="false">
          {{ $t('users.bulkImportFormat') }}
        </b-message>

        <b-field :label="$t('users.bulkImportFile')" label-position="on-border">
          <b-upload v-model="file" drag-drop expanded
            accept=".csv,.xlsx,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
            data-cy="user-import-file">
            <div class="has-text-centered section">
              <p><b-icon icon="file-upload-outline" size="is-large" /></p>
              <p>{{ $t('users.bulkImportFileHelp') }}</p>
            </div>
          </b-upload>
        </b-field>

        <div v-if="file" class="tags mb-4">
          <b-tag size="is-medium" closable @close="clearFile">
            {{ file.name }}
          </b-tag>
        </div>

        <b-message v-if="parseError" type="is-danger" :closable="false">
          {{ parseError }}
        </b-message>

        <div v-if="rows.length > 0" class="user-import-preview">
          <h5 class="title is-size-6">{{ $t('users.bulkImportPreview') }} ({{ rows.length }})</h5>
          <b-table :data="rows" :mobile-cards="false" narrowed striped hoverable>
            <b-table-column v-slot="props" field="line" :label="$t('users.bulkImportRow')" numeric>
              {{ props.row.line }}
            </b-table-column>
            <b-table-column v-slot="props" field="username" :label="$t('users.username')">
              {{ props.row.username }}
            </b-table-column>
            <b-table-column v-slot="props" field="name" :label="$t('globals.fields.name')">
              {{ props.row.name || props.row.username }}
            </b-table-column>
            <b-table-column v-slot="props" field="password" :label="$t('users.password')">
              {{ maskedPassword(props.row.password) }}
            </b-table-column>
            <b-table-column v-slot="props" field="email" :label="$t('subscribers.email')">
              {{ props.row.email }}
            </b-table-column>
            <b-table-column v-slot="props" field="userRole" :label="$tc('users.userRole', 1)">
              {{ props.row.userRole }}
            </b-table-column>
            <b-table-column v-slot="props" field="listRole" :label="$tc('users.listRole', 1)">
              {{ props.row.listRole || '—' }}
            </b-table-column>
            <b-table-column v-slot="props" field="status" :label="$t('globals.fields.status')">
              {{ props.row.status || 'enabled' }}
            </b-table-column>
            <b-table-column v-slot="props" field="errors" :label="$t('users.bulkImportErrors')">
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
          :loading="isImporting || isParsing" :disabled="rows.length === 0 || !allRowsValid" data-cy="btn-user-import">
          {{ $t('users.bulkImport') }}
        </b-button>
      </footer>
    </div>
  </form>
</template>

<script>
import Vue from 'vue';
import Papa from 'papaparse';
import * as XLSX from 'xlsx';

const columns = ['username', 'name', 'password', 'email', 'user_role', 'list_role', 'status'];
const requiredColumns = ['username', 'password', 'email', 'user_role'];

export default Vue.extend({
  name: 'UserBulkImport',

  data() {
    return {
      file: null,
      rows: [],
      parseError: '',
      isParsing: false,
      isImporting: false,
    };
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
      worksheet['!cols'] = [
        { wch: 24 }, { wch: 24 }, { wch: 24 }, { wch: 32 }, { wch: 24 }, { wch: 24 }, { wch: 12 },
      ];
      const workbook = XLSX.utils.book_new();
      XLSX.utils.book_append_sheet(workbook, worksheet, 'Users');
      XLSX.writeFile(workbook, 'listmonk-users-import-template.xlsx');
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
      if (!firstSheet) {
        return [];
      }
      return XLSX.utils.sheet_to_json(workbook.Sheets[firstSheet], {
        header: 1,
        raw: false,
        blankrows: false,
      });
    },

    normalizeHeader(value) {
      return String(value || '').trim().toLowerCase().replace(/[\s-]+/g, '_');
    },

    stringValue(value) {
      return value === undefined || value === null ? '' : String(value);
    },

    validateRow(row) {
      const errors = [];
      const username = row.username.trim();
      const email = row.email.trim();
      const status = row.status.trim().toLowerCase();

      if (username.length < 3 || username.length > 2000 || !/^[a-zA-Z0-9_\-.@]+$/.test(username)) {
        errors.push('invalid_username');
      }
      if (row.name.trim().length > 2000) {
        errors.push('invalid_name');
      }
      if (row.password.length < 8 || row.password.length > 2000) {
        errors.push('invalid_password');
      }
      if (!this.$utils.validateEmail(email)) {
        errors.push('invalid_email');
      }
      if (!row.userRole.trim()) {
        errors.push('missing_user_role');
      }
      if (status && status !== 'enabled' && status !== 'disabled') {
        errors.push('invalid_status');
      }
      return errors;
    },

    buildRows(source) {
      if (!source || source.length < 2) {
        throw new Error(this.$t('users.bulkImportNoRows'));
      }

      const headers = source[0].map(this.normalizeHeader);
      const headerIndexes = {};
      headers.forEach((header, index) => {
        if (header && columns.includes(header) && headerIndexes[header] === undefined) {
          headerIndexes[header] = index;
        }
      });

      const missing = requiredColumns.filter((column) => headerIndexes[column] === undefined);
      if (missing.length > 0) {
        throw new Error(this.$t('users.bulkImportMissingColumns', { columns: missing.join(', ') }));
      }

      return source.slice(1).reduce((out, sourceRow, index) => {
        const values = {};
        columns.forEach((column) => {
          values[column] = headerIndexes[column] === undefined
            ? ''
            : this.stringValue(sourceRow[headerIndexes[column]]);
        });
        if (columns.every((column) => values[column].trim() === '')) {
          return out;
        }

        const row = {
          line: index + 2,
          username: values.username,
          name: values.name,
          password: values.password,
          email: values.email,
          userRole: values.user_role,
          listRole: values.list_role,
          status: values.status,
          errors: [],
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
        let source;
        if (fileName.endsWith('.xlsx')) {
          source = await this.parseXLSXRows(file);
        } else if (fileName.endsWith('.csv')) {
          source = await this.parseCSVRows(file);
        } else {
          throw new Error(this.$t('users.bulkImportInvalidFile'));
        }
        const rows = this.buildRows(source);
        if (rows.length === 0) {
          throw new Error(this.$t('users.bulkImportNoRows'));
        }
        this.rows = rows;
      } catch (error) {
        this.parseError = error.message || String(error);
      } finally {
        this.isParsing = false;
      }
    },

    maskedPassword(password) {
      return password ? '********' : '';
    },

    issueMessage(code) {
      return this.$t(`users.bulkImportError.${code}`);
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

    onSubmit() {
      this.isImporting = true;
      const users = this.rows.map((row) => ({
        username: row.username,
        name: row.name,
        password: row.password,
        email: row.email,
        user_role: row.userRole,
        list_role: row.listRole,
        status: row.status,
      }));

      this.$api.createUsers({ users }).then((result) => {
        if (result.errors && result.errors.length > 0) {
          this.applyServerErrors(result.errors);
          this.$utils.toast(this.$t('users.bulkImportValidationFailed'), 'is-danger');
          return;
        }
        this.$emit('finished');
        this.$utils.toast(this.$t('users.bulkImportSuccess', { count: result.created }));
        this.$parent.close();
      }).finally(() => {
        this.isImporting = false;
      });
    },
  },

  computed: {
    allRowsValid() {
      return this.rows.every((row) => row.errors.length === 0);
    },
  },
});
</script>

<style scoped>
.user-import-preview {
  overflow-x: auto;
}
</style>
