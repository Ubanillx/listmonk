<template>
  <section class="import">
    <h1 class="title is-4">
      {{ $t('import.title') }}
    </h1>
    <b-loading :active="isLoading" />

    <section v-if="isFree()" class="wrap">
      <form @submit.prevent="onUpload" class="box">
        <div>
          <div class="columns">
            <div class="column">
              <b-field :label="$t('import.mode')" :addons="false">
                <div>
                  <b-radio v-model="form.mode" name="mode" native-value="subscribe" data-cy="check-subscribe">
                    {{ $t('import.subscribe') }}
                  </b-radio>
                  <br />
                  <b-radio v-if="!poolImport" v-model="form.mode" name="mode" native-value="blocklist" data-cy="check-blocklist">
                    {{ $t('import.blocklist') }}
                  </b-radio>
                </div>
              </b-field>
            </div>
            <div class="column">
              <b-field :label="$t('globals.fields.status')" :addons="false">
                <template v-if="form.mode === 'subscribe'">
                  <b-radio v-model="form.subStatus" name="subStatus" native-value="unconfirmed"
                    data-cy="check-unconfirmed">
                    {{ $t('customers.status.unconfirmed') }}
                  </b-radio>
                  <b-radio v-model="form.subStatus" name="subStatus" native-value="confirmed" data-cy="check-confirmed">
                    {{ $t('customers.status.confirmed') }}
                  </b-radio>
                </template>

                <b-radio v-else v-model="form.subStatus" name="subStatus" native-value="unsubscribed"
                  data-cy="check-unsubscribed">
                  {{ $t('customers.status.unsubscribed') }}
                </b-radio>
                <p v-if="form.mode === 'subscribe'" class="help">{{ $t('import.statusHelp') }}</p>
              </b-field>
            </div>
          </div>
          <div class="columns">
            <div class="column is-4">
              <b-field v-if="form.mode === 'subscribe' && !poolImport" :label="$t('import.overwriteUserInfo')"
                :message="$t('import.overwriteUserInfoHelp')">
                <div>
                  <b-switch v-model="form.overwriteUserInfo" name="overwriteUserInfo" data-cy="overwrite-user-info" />
                </div>
              </b-field>
            </div>

            <div class="column">
              <b-field v-if="form.mode === 'subscribe' && !poolImport" :label="$t('import.overwriteSubStatus')"
                :message="$t('import.overwriteSubStatusHelp')">
                <div>
                  <b-switch v-model="form.overwriteSubStatus" name="overwriteSubStatus"
                    data-cy="overwrite-sub-status" />
                </div>
              </b-field>
            </div>
          </div>

          <customer-list-selector v-if="form.mode === 'subscribe'" :label="$t('globals.terms.customer_lists')"
            :placeholder="$t('import.listSubHelp')" :message="poolImport ? $t('import.poolListHelp') : $t('import.listSubHelp')"
            v-model="form.customer_lists" :selected="form.customer_lists" :all="importListOptions" />
          <p v-if="hasInvalidPoolSelection" class="help has-text-danger">{{ $t('import.poolListSelectionError') }}</p>

          <b-field :label="$t('import.firstRowHeader')" :message="$t('import.previewHelp')">
            <b-switch v-model="preview.firstRowHeader" :disabled="poolImport" @input="rebuildPreviewFromRaw" />
          </b-field>
          <p v-if="poolImport" class="help">{{ $t('import.poolHeaderHelp') }}</p>

          <div class="columns">
            <div class="column">
              <b-field :label="$t('import.mapEmailField')" :message="poolImport ? $t('import.poolRequiredFieldHelp') : ''">
                <b-select v-model="form.fieldMap.email" expanded>
                  <option value="">{{ $t('globals.terms.none') }}</option>
                  <option v-for="col in preview.columns" :key="`email-${col.value}`" :value="col.value">
                    {{ col.label }}
                  </option>
                </b-select>
              </b-field>
            </div>
            <div class="column">
              <b-field :label="$t('import.mapNameField')" :message="poolImport ? $t('import.poolRequiredFieldHelp') : ''">
                <b-select v-model="form.fieldMap.name" expanded>
                  <option value="">{{ $t('globals.terms.none') }}</option>
                  <option v-for="col in preview.columns" :key="`name-${col.value}`" :value="col.value">
                    {{ col.label }}
                  </option>
                </b-select>
              </b-field>
            </div>
          </div>

          <div v-if="form.mode === 'subscribe'" class="columns">
            <div class="column is-4">
              <b-field :label="$t('import.mapCustomerCodeField')"
                :message="poolImport ? $t('import.poolRequiredFieldHelp') : $t('import.mapCustomerCodeFieldHelp')">
                <b-select v-model="form.fieldMap.customer_code" expanded required>
                  <option value="">{{ $t('globals.terms.none') }}</option>
                  <option v-for="col in preview.columns" :key="`customer_code-${col.value}`" :value="col.value">
                    {{ col.label }}
                  </option>
                </b-select>
              </b-field>
            </div>
          </div>

          <div v-if="poolImport" class="columns">
            <div class="column is-4">
              <b-field :label="$t('import.mapAllocationDepartmentField')"
                :message="$t('import.mapAllocationDepartmentFieldHelp')">
                <b-select v-model="form.fieldMap.allocation_department" expanded required>
                  <option value="">{{ $t('globals.terms.none') }}</option>
                  <option v-for="col in preview.columns" :key="`allocation_department-${col.value}`" :value="col.value">
                    {{ col.label }}
                  </option>
                </b-select>
              </b-field>
            </div>
          </div>

          <div class="content" v-if="preview.error">
            <p class="has-text-danger">{{ preview.error }}</p>
          </div>

          <div class="box" v-if="preview.columns.length > 0">
            <h5 class="title is-size-6">{{ $t('import.preview') }}</h5>
            <div class="preview-table-wrap">
              <b-table
                :data="preview.rows"
                :mobile-cards="false"
                :paginated="false"
                striped
                hoverable
                class="preview-table"
              >
                <b-table-column
                  v-for="col in preview.columns"
                  :key="`preview-col-${col.index}`"
                  :label="col.label"
                  :width="getPreviewColumnWidth(col)"
                  v-slot="props"
                >
                  <span class="preview-cell" :title="getCellValue(props.row, col.index)">
                    {{ getCellValue(props.row, col.index) }}
                  </span>
                </b-table-column>
              </b-table>
            </div>
          </div>

          <hr />

          <b-field :label="$t('import.csvFile')" label-position="on-border">
            <b-upload v-model="form.file" drag-drop expanded :accept="uploadAccept">
              <div class="has-text-centered section">
                <p>
                  <b-icon icon="file-upload-outline" size="is-large" />
                </p>
                <p>{{ $t('import.csvFileHelp') }}</p>
              </div>
            </b-upload>
          </b-field>
          <div class="tags" v-if="form.file">
            <b-tag size="is-medium" closable @close="clearFile">
              {{ form.file.name }}
            </b-tag>
          </div>
          <div class="buttons">
            <b-button native-type="submit" type="is-primary" :disabled="isSubmitDisabled()"
              :loading="isProcessing">
              {{ $t('import.upload') }}
            </b-button>
          </div>
        </div>
      </form>
      <br /><br />

      <div class="import-help">
        <h5 class="title is-size-6">
          {{ $t('import.instructions') }}
        </h5>
        <p>{{ poolImport ? $t('import.poolInstructionsHelp') : $t('import.instructionsHelp') }}</p>
        <br />
        <blockquote class="csv-example">
          <code v-if="poolImport" class="csv-headers">
            <span>客户编号,</span> <span>姓名,</span> <span>邮箱,</span> <span>分配部门</span>
          </code>
          <code v-else class="csv-headers">
            <span>email,</span> <span>name,</span> <span>customer_code</span>
          </code>
        </blockquote>

        <hr />

        <h5 class="title is-size-6">
          {{ $t('import.csvExample') }}
        </h5>

        <pre class="csv-example" v-text="poolImport ? form.poolExample : form.example" />
      </div>

      <article v-if="poolImportResult"
        :class="['message', poolImportResult.invalid ? 'is-warning' : 'is-success', 'import-pool-result']"
        data-cy="pool-import-result">
        <div class="message-header">
          <p>{{ $t('import.poolResultTitle') }}</p>
          <button type="button" class="delete" :aria-label="$t('globals.buttons.close')"
            @click="poolImportResult = null" />
        </div>
        <div class="message-body">
          <div class="tags">
            <b-tag>{{ $t('import.poolResultTotal', { count: poolImportResult.total || 0 }) }}</b-tag>
            <b-tag type="is-success">{{ $t('import.poolResultCreated', { count: poolImportResult.created || 0 }) }}</b-tag>
            <b-tag type="is-info">{{ $t('import.poolResultExisting', { count: poolImportResult.existing || 0 }) }}</b-tag>
            <b-tag type="is-warning">{{ $t('import.poolResultConflicts', { count: poolImportResult.conflicts || 0 }) }}</b-tag>
            <b-tag type="is-danger">{{ $t('import.poolResultInvalid', { count: poolImportResult.invalid || 0 }) }}</b-tag>
            <b-tag>{{ $t('import.poolResultDuplicates', { count: poolImportResult.duplicates || 0 }) }}</b-tag>
          </div>
          <div v-if="poolImportResult.issues && poolImportResult.issues.length" class="content">
            <strong>{{ $t('import.poolResultIssues') }}</strong>
            <ul>
              <li v-for="issue in poolImportResult.issues.slice(0, 50)" :key="`${issue.row}-${issue.reason}`">
                {{ $t('import.poolResultIssue', { row: issue.row, code: issue.customerCode || '-', reason: poolIssueReason(issue.reason, issue.allocationDepartment) }) }}
              </li>
            </ul>
          </div>
          <p class="help">{{ $t('import.poolResultLocationHelp') }}</p>
          <div class="buttons">
            <b-button type="is-primary" icon-left="account-group-outline" data-cy="view-pool-contacts"
              @click="viewImportedPool">
              {{ $t('import.poolResultViewPool') }}
            </b-button>
          </div>
        </div>
      </article>
    </section><!-- upload //-->

    <section v-if="isRunning() || isDone()" class="wrap status box has-text-centered">
      <b-progress :value="progress" show-value type="is-success" />
      <br />
      <p
        :class="['is-size-5', 'is-capitalized', { 'has-text-success': status.status === 'finished' }, { 'has-text-danger': (status.status === 'failed' || status.status === 'stopped') }]">
        {{ status.status }}
      </p>

      <p>{{ $t('import.recordsCount', { num: status.imported, total: status.total }) }}</p>
      <br />

      <p>
        <b-button @click="stopImport" :loading="isProcessing" icon-left="file-upload-outline" type="is-primary">
          {{ isDone() ? $t('import.importDone') : $t('import.stopImport') }}
        </b-button>
      </p>
      <br />

      <div class="import-logs">
        <log-view :lines="logs" :loading="false" />
      </div>
    </section>
  </section>
</template>

<script>
import Vue from 'vue';
import Papa from 'papaparse';
import * as XLSX from 'xlsx';
import { mapState } from 'vuex';
import CustomerListSelector from '../components/CustomerListSelector.vue';
import LogView from '../components/LogView.vue';
import { isOwnedActiveWorkspaceCustomerList } from '../utils/workspace';

export default Vue.extend({
  components: {
    CustomerListSelector,
    LogView,
  },

  props: {
    data: { type: Object, default: () => { } },
    isEditing: { type: Boolean, default: false },
  },

  data() {
    return {
      form: {
        mode: 'subscribe',
        subStatus: 'unconfirmed',
        customer_lists: [],
        overwriteUserInfo: false,
        overwriteSubStatus: false,
        file: null,
        fieldMap: {
          email: '',
          name: '',
          customer_code: '',
          allocation_department: '',
        },
        example: '',
        poolExample: '',
      },

      preview: {
        columns: [],
        rows: [],
        rawRows: [],
        firstRowHeader: true,
        error: '',
      },

      // Initial page load still has to wait for the status API to return
      // to either show the form or the status box.
      isLoading: true,

      isProcessing: false,
      status: { status: '' },
      logs: [],
      pollID: null,
      poolImportResult: null,
    };
  },

  watch: {
    'form.mode': function formMode() {
      // Select the appropriate status radio whenever mode changes.
      this.$nextTick(() => {
        if (this.form.mode === 'subscribe') {
          this.form.subStatus = 'unconfirmed';
        } else {
          this.form.subStatus = 'unsubscribed';
        }
      });
    },

    'form.file': function onFileChanged(file) {
      if (!file) {
        this.clearPreview();
        return;
      }
      this.previewFromFile();
    },

    'form.customer_lists': {
      deep: true,
      handler() {
        if (this.poolImport) {
          this.form.mode = 'subscribe';
          this.preview.firstRowHeader = true;
        }
      },
    },

    poolImport(value) {
      if (value) {
        this.form.mode = 'subscribe';
        this.preview.firstRowHeader = true;
      }
      this.$nextTick(() => {
        if (value) {
          this.autoMapFields();
        } else {
          this.form.fieldMap.allocation_department = '';
        }
      });
    },

  },

  methods: {
    clearFile() {
      this.form.file = null;
      this.clearPreview();
    },

    clearPreview() {
      this.preview.columns = [];
      this.preview.rows = [];
      this.preview.rawRows = [];
      this.preview.error = '';
      this.form.fieldMap.email = '';
      this.form.fieldMap.name = '';
      this.form.fieldMap.customer_code = '';
      this.form.fieldMap.allocation_department = '';
    },

    getCellValue(row, idx) {
      if (!row || typeof row[idx] === 'undefined') {
        return '';
      }
      return row[idx];
    },

    getPreviewColumnWidth(col) {
      const base = String((col && (col.header || col.label || col.letter)) || '').trim();
      const len = Math.max(base.length, 6);

      // Keep columns readable for long Chinese headers while still allowing horizontal scroll.
      const px = Math.min(Math.max((len * 14) + 36, 120), 520);
      return `${px}px`;
    },

    toColumnLetters(index) {
      let n = index + 1;
      let out = '';
      while (n > 0) {
        const rem = (n - 1) % 26;
        out = String.fromCharCode(65 + rem) + out;
        n = Math.floor((n - 1) / 26);
      }
      return out;
    },

    buildPreviewColumns(headerRow, width) {
      const cols = [];
      const used = new Set();
      for (let i = 0; i < width; i += 1) {
        const letter = this.toColumnLetters(i);
        const header = (headerRow[i] || '').toString().trim();
        let value = letter;
        let label = letter;

        if (this.preview.firstRowHeader && header) {
          value = header;
          label = `${letter} - ${header}`;
        }

        if (used.has(value)) {
          value = letter;
        }
        used.add(value);

        cols.push({
          index: i,
          label,
          value,
          header,
          letter,
        });
      }

      return cols;
    },

    normalizeFieldName(v) {
      return String(v || '').trim().toLowerCase();
    },

    autoMapFields() {
      const keyMap = {
        email: ['email', 'e-mail', 'mail', '邮箱', '邮件地址'],
        name: ['name', 'fullname', 'full name', '联系人', '姓名'],
        customer_code: ['customer_code', 'customer code', 'customercode', '客户编码', '客户编号'],
      };

      if (this.poolImport) {
        keyMap.allocation_department = [
          'allocation_department', 'allocation department', 'department', '部门', '分配部门', '分配部门名称',
        ];
      }

      Object.keys(keyMap).forEach((target) => {
        if (this.form.fieldMap[target]) {
          return;
        }
        const col = this.preview.columns.find((c) => {
          const norm = this.normalizeFieldName(c.header || c.value);
          return keyMap[target].includes(norm);
        });
        if (col) {
          this.form.fieldMap[target] = col.value;
        }
      });
    },

    rebuildPreviewFromRaw() {
      if (!this.preview.rawRows || this.preview.rawRows.length === 0) {
        return;
      }

      const rows = this.preview.rawRows;
      const width = rows.reduce((m, row) => Math.max(m, row.length), 0);
      const headerRow = this.preview.firstRowHeader ? rows[0] : [];

      this.preview.columns = this.buildPreviewColumns(headerRow, width);
      this.preview.rows = this.preview.firstRowHeader
        ? rows.slice(1, 6)
        : rows.slice(0, 5);

      this.form.fieldMap.email = '';
      this.form.fieldMap.name = '';
      this.form.fieldMap.customer_code = '';
      this.form.fieldMap.allocation_department = '';
      this.autoMapFields();
    },

    parseCSVRows(file) {
      return new Promise((resolve, reject) => {
        Papa.parse(file, {
          skipEmptyLines: true,
          complete: (res) => {
            if (res.errors && res.errors.length > 0) {
              reject(new Error(res.errors[0].message));
              return;
            }
            resolve((res.data || []).map((r) => r.map((v) => String(v || ''))));
          },
          error: (err) => reject(err),
        });
      });
    },

    async parseXLSXRows(file) {
      const buf = await file.arrayBuffer();
      const wb = XLSX.read(buf, { type: 'array' });
      const firstSheet = wb.SheetNames[0];
      if (!firstSheet) {
        return [];
      }
      const ws = wb.Sheets[firstSheet];
      const rows = XLSX.utils.sheet_to_json(ws, { header: 1, raw: false, blankrows: false });
      return rows.map((r) => r.map((v) => String(v || '')));
    },

    async previewFromFile() {
      try {
        this.preview.error = '';

        if (!this.form.file) {
          this.clearPreview();
          return;
        }

        const name = String(this.form.file.name || '').toLowerCase();
        let rows = [];

        if (name.endsWith('.xlsx')) {
          rows = await this.parseXLSXRows(this.form.file);
        } else if (name.endsWith('.csv')) {
          rows = await this.parseCSVRows(this.form.file);
        } else {
          this.clearPreview();
          this.preview.error = this.$t('import.previewUnsupported');
          return;
        }

        if (!rows || rows.length === 0) {
          this.clearPreview();
          this.preview.error = this.$t('import.previewNoData');
          return;
        }

        this.preview.rawRows = rows.slice(0, 20);
        this.rebuildPreviewFromRaw();
      } catch (e) {
        this.clearPreview();
        this.preview.error = this.$t('import.previewParseFailed', { error: e.message || e });
      }
    },

    // Returns true if we're free to do an upload.
    isFree() {
      if (this.status.status === 'none') {
        return true;
      }
      return false;
    },

    // Returns true if an import is running.
    isRunning() {
      if (this.status.status === 'importing'
        || this.status.status === 'stopping') {
        return true;
      }
      return false;
    },

    isSuccessful() {
      return this.status.status === 'finished';
    },

    isFailed() {
      return (
        this.status.status === 'stopped'
        || this.status.status === 'failed'
      );
    },

    // Returns true if an import has finished (failed or successful).
    isDone() {
      if (this.status.status === 'finished'
        || this.status.status === 'stopped'
        || this.status.status === 'failed'
      ) {
        return true;
      }
      return false;
    },

    pollStatus() {
      // Clear any running status polls.
      clearInterval(this.pollID);

      // Poll for the status as long as the import is running.
      this.pollID = setInterval(() => {
        this.$api.getImportStatus().then((data) => {
          this.isProcessing = false;
          this.isLoading = false;
          this.status = data;
          this.getLogs();

          if (!this.isRunning()) {
            clearInterval(this.pollID);
          }
        }, () => {
          this.isProcessing = false;
          this.isLoading = false;
          this.status = { status: 'none' };
          clearInterval(this.pollID);
        });
        return true;
      }, 250);
    },

    getLogs() {
      this.$api.getImportLogs().then((data) => {
        this.logs = data.split('\n').map((line) => line.replace(/\s+importer\.go:\d+:\s*/, ' *: '));
        Vue.nextTick(() => {
          // vue.$refs doesn't work as the logs textarea is rendered dynamically.
          const ref = document.getElementById('import-log');
          if (ref) {
            ref.scrollTop = ref.scrollHeight;
          }
        });
      });
    },

    // Cancel a running import or clears a finished import.
    stopImport() {
      this.isProcessing = true;
      this.$api.stopImport().then(() => {
        this.pollStatus();
        this.form.file = null;
        this.clearPreview();
      });
    },

    renderExample() {
      const h = 'email,name,customer_code\n'
        + 'user1@example.com,"User One",CUST-001\n'
        + 'user2@example.com,"User Two",CUST-002';

      this.form.example = h;

      this.form.poolExample = '客户编号,姓名,邮箱,分配部门\n'
        + 'AE-20004,"JAGATHISH PICHAIYAPPA",jagath.p@example.com,分表1\n'
        + 'AE-20005,"Contact Two",contact2@example.com,分表2';
    },

    resetForm() {
      this.form.mode = 'subscribe';
      this.form.overwriteUserInfo = false;
      this.form.overwriteSubStatus = false;
      this.form.file = null;
      this.form.customer_lists = [];
      this.form.subStatus = 'unconfirmed';
      this.form.fieldMap = {
        email: '',
        name: '',
        customer_code: '',
        allocation_department: '',
      };
      this.poolImportResult = null;
      this.clearPreview();
    },

    onUpload() {
      if (this.form.mode === 'subscribe' && this.form.overwriteSubStatus) {
        this.$utils.confirm(this.$t('import.subscribeWarning'), this.onSubmit, this.resetForm);
        return;
      }

      this.onSubmit();
    },

    onSubmit() {
      if (this.isSubmitDisabled()) {
        return;
      }
      this.isProcessing = true;
      this.poolImportResult = null;

      // Prepare the upload payload.
      const params = new FormData();
      const fieldMap = {
        email: this.form.fieldMap.email,
        name: this.form.fieldMap.name,
        customer_code: this.form.fieldMap.customer_code,
      };
      if (this.poolImport) {
        fieldMap.allocation_department = this.form.fieldMap.allocation_department;
      }
      params.set('params', JSON.stringify({
        mode: this.form.mode,
        subscription_status: this.form.subStatus,
        customer_list_ids: this.form.customer_lists.map((l) => l.id),
        overwrite_userinfo: this.form.overwriteUserInfo,
        overwrite_subscription_status: this.form.overwriteSubStatus,
        field_map: fieldMap,
      }));
      params.set('file', this.form.file);

      // Post.
      this.$api.importCustomers(params).then((result) => {
        if (result && result.target === 'pool') {
          this.poolImportResult = result;
          this.isProcessing = false;
          this.form.file = null;
          this.clearPreview();
          const written = Number(result.created || 0) + Number(result.existing || 0);
          let toastKey = 'import.poolImportComplete';
          if (result.invalid) {
            toastKey = written === 0
              ? 'import.poolImportRejected'
              : 'import.poolImportWithErrors';
          }
          this.$utils.toast(this.$t(toastKey));
          return;
        }

        // On file upload, show a confirmation.
        this.$utils.toast(this.$t('import.importStarted'));

        // Start polling status.
        this.pollStatus();
      }, () => {
        this.isProcessing = false;
        this.form.file = null;
      });
    },

    isSubmitDisabled() {
      if (!this.form.file || this.hasInvalidPoolSelection) {
        return true;
      }
      if (this.form.mode === 'subscribe' && this.form.customer_lists.length === 0) {
        return true;
      }
      if (this.form.mode === 'subscribe' && !this.form.fieldMap.customer_code) {
        return true;
      }
      if (this.poolImport && (!this.form.fieldMap.email || !this.form.fieldMap.name
        || !this.form.fieldMap.allocation_department)) {
        return true;
      }
      return false;
    },

    poolIssueReason(reason, department) {
      const labels = {
        customer_code_required: this.$t('import.poolIssueCustomerCodeRequired'),
        name_required: this.$t('import.poolIssueNameRequired'),
        email_required: this.$t('import.poolIssueEmailRequired'),
        allocation_department_required: this.$t('import.poolIssueDepartmentRequired'),
        allocation_department_not_found: this.$t('import.poolIssueDepartmentNotFound', { department: department || '-' }),
        invalid_email: this.$t('import.poolIssueInvalidEmail'),
      };
      return labels[reason] || reason;
    },

    viewImportedPool() {
      const poolID = Number(this.poolImportResult
        && (this.poolImportResult.poolId || this.poolImportResult.pool_id));
      if (poolID > 0) {
        this.$router.push({ name: 'customersCustomerList', params: { customerListID: poolID } });
      }
    },
  },

  computed: {
    ...mapState(['customer_lists', 'profile', 'workspace']),

    isPlatformAdmin() {
      return Number(this.profile && this.profile.userRole && this.profile.userRole.id) === 1;
    },

    selectedPoolLists() {
      return this.form.customer_lists.filter((list) => list.type === 'pool');
    },

    regularImportLists() {
      const all = (this.customer_lists && this.customer_lists.results) || [];
      const userID = this.profile && this.profile.id;
      return all.filter((customerList) => isOwnedActiveWorkspaceCustomerList(
        customerList,
        this.workspace,
        userID,
      ) && this.$canList(customerList.id, 'customer_list:manage'));
    },

    poolImportLists() {
      const all = (this.customer_lists && this.customer_lists.results) || [];
      return all.filter((list) => list.type === 'pool');
    },

    poolImport() {
      return this.isPlatformAdmin
        && this.selectedPoolLists.length === 1
        && this.form.customer_lists.length === 1
        && this.form.customer_lists[0].type === 'pool';
    },

    hasInvalidPoolSelection() {
      const selected = this.form.customer_lists;
      const poolCount = selected.filter((list) => list.type === 'pool').length;
      const allocationCount = selected.filter((list) => list.type === 'org_pool_allocation').length;
      const regularCount = selected.filter((list) => list.type !== 'pool' && list.type !== 'org_pool_allocation').length;
      return allocationCount > 0 || poolCount > 1 || (poolCount > 0 && (!this.isPlatformAdmin || regularCount > 0));
    },

    importListOptions() {
      const selected = this.form.customer_lists;
      const selectedPool = selected.some((list) => list.type === 'pool');
      const selectedRegular = selected.some((list) => list.type !== 'pool' && list.type !== 'org_pool_allocation');
      if (selectedPool && this.isPlatformAdmin) {
        return this.poolImportLists;
      }
      if (selectedPool || selectedRegular) {
        return this.regularImportLists;
      }
      return this.isPlatformAdmin
        ? this.regularImportLists.concat(this.poolImportLists)
        : this.regularImportLists;
    },

    uploadAccept() {
      if (this.poolImport) {
        return '.csv,.xlsx,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet';
      }
      return '.csv,.zip,.xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet';
    },

    // Import progress bar value.
    progress() {
      if (!this.status || !this.status.total > 0) {
        return 0;
      }
      return Math.ceil((this.status.imported / this.status.total) * 100);
    },
  },

  mounted() {
    this.renderExample();
    this.pollStatus();

    const ids = this.$utils.parseQueryIDs(this.$route.query.customer_list_id);
    if (ids.length > 0 && this.customer_lists.results) {
      this.$nextTick(() => {
        const options = this.isPlatformAdmin
          ? this.regularImportLists.concat(this.poolImportLists)
          : this.regularImportLists;
        this.form.customer_lists = options.filter((list) => ids.indexOf(list.id) > -1);
      });
    }
  },
});
</script>

<style scoped>
.preview-table-wrap {
  width: 100%;
  max-width: 100%;
  overflow-x: auto;
  overflow-y: hidden;
}

.preview-table {
  min-width: 100%;
}

.preview-table :deep(table) {
  table-layout: auto;
}

.preview-table :deep(th),
.preview-table :deep(td) {
  padding: 15px 10px;
  line-height: 1.5;
  font-size: 1rem;
  white-space: nowrap;
}

.preview-cell {
  display: block;
  width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
