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
                  <b-radio v-model="form.mode" name="mode" native-value="blocklist" data-cy="check-blocklist">
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
                    {{ $t('subscribers.status.unconfirmed') }}
                  </b-radio>
                  <b-radio v-model="form.subStatus" name="subStatus" native-value="confirmed" data-cy="check-confirmed">
                    {{ $t('subscribers.status.confirmed') }}
                  </b-radio>
                </template>

                <b-radio v-else v-model="form.subStatus" name="subStatus" native-value="unsubscribed"
                  data-cy="check-unsubscribed">
                  {{ $t('subscribers.status.unsubscribed') }}
                </b-radio>
              </b-field>
            </div>

            <div class="column">
              <b-field :label="$t('import.csvDelim')" :message="$t('import.csvDelimHelp')" class="delimiter"
                v-if="requiresDelimiter()">
                <b-input v-model="form.delim" name="delim" placeholder="," maxlength="1" required />
              </b-field>
            </div>
          </div>

          <div class="columns">
            <div class="column is-4">
              <b-field v-if="form.mode === 'subscribe'" :label="$t('import.overwriteUserInfo')"
                :message="$t('import.overwriteUserInfoHelp')">
                <div>
                  <b-switch v-model="form.overwriteUserInfo" name="overwriteUserInfo" data-cy="overwrite-user-info" />
                </div>
              </b-field>
            </div>

            <div class="column">
              <b-field v-if="form.mode === 'subscribe'" :label="$t('import.overwriteSubStatus')"
                :message="$t('import.overwriteSubStatusHelp')">
                <div>
                  <b-switch v-model="form.overwriteSubStatus" name="overwriteSubStatus"
                    data-cy="overwrite-sub-status" />
                </div>
              </b-field>
            </div>
          </div>

          <list-selector v-if="form.mode === 'subscribe'" :label="$t('globals.terms.lists')"
            :placeholder="$t('import.listSubHelp')" :message="$t('import.listSubHelp')" v-model="form.lists"
            :selected="form.lists" :all="lists.results" />

          <b-field :label="$t('import.firstRowHeader')" :message="$t('import.previewHelp')">
            <b-switch v-model="preview.firstRowHeader" @input="rebuildPreviewFromRaw" />
          </b-field>

          <div class="columns">
            <div class="column">
              <b-field :label="$t('import.mapEmailField')">
                <b-select v-model="form.fieldMap.email" expanded>
                  <option value="">{{ $t('globals.terms.none') }}</option>
                  <option v-for="col in preview.columns" :key="`email-${col.value}`" :value="col.value">
                    {{ col.label }}
                  </option>
                </b-select>
              </b-field>
            </div>
            <div class="column">
              <b-field :label="$t('import.mapNameField')">
                <b-select v-model="form.fieldMap.name" expanded>
                  <option value="">{{ $t('globals.terms.none') }}</option>
                  <option v-for="col in preview.columns" :key="`name-${col.value}`" :value="col.value">
                    {{ col.label }}
                  </option>
                </b-select>
              </b-field>
            </div>
            <div class="column">
              <b-field :label="$t('import.mapAttributesField')">
                <b-select v-model="form.fieldMap.attributes" expanded>
                  <option value="">{{ $t('globals.terms.none') }}</option>
                  <option v-for="col in preview.columns" :key="`attributes-${col.value}`" :value="col.value">
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
            <b-upload v-model="form.file" drag-drop expanded accept=".csv,.zip,.xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet">
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
            <b-button native-type="submit" type="is-primary"
              :disabled="!form.file || (form.mode === 'subscribe' && form.lists.length === 0)" :loading="isProcessing">
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
        <p>{{ $t('import.instructionsHelp') }}</p>
        <br />
        <blockquote class="csv-example">
          <code class="csv-headers"> <span>email,</span> <span>name,</span> <span>attributes</span></code>
        </blockquote>

        <hr />

        <h5 class="title is-size-6">
          {{ $t('import.csvExample') }}
        </h5>

        <pre class="csv-example" v-text="example" />
      </div>
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
import ListSelector from '../components/ListSelector.vue';
import LogView from '../components/LogView.vue';

export default Vue.extend({
  components: {
    ListSelector,
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
        delim: ',',
        lists: [],
        overwriteUserInfo: false,
        overwriteSubStatus: false,
        file: null,
        fieldMap: {
          email: '',
          name: '',
          attributes: '',
        },
        example: '',
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

    'form.delim': function onDelimiterChanged() {
      if (this.form.file && this.requiresDelimiter()) {
        this.previewFromFile();
      }
    },
  },

  methods: {
    requiresDelimiter() {
      if (!this.form.file || !this.form.file.name) {
        return true;
      }
      return !String(this.form.file.name).toLowerCase().endsWith('.xlsx');
    },

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
      this.form.fieldMap.attributes = '';
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
        email: ['email', 'e-mail', 'mail'],
        name: ['name', 'fullname', 'full name'],
        attributes: ['attributes', 'attribs', 'meta', 'metadata'],
      };

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
      this.form.fieldMap.attributes = '';
      this.autoMapFields();
    },

    parseCSVRows(file) {
      return new Promise((resolve, reject) => {
        Papa.parse(file, {
          delimiter: this.form.delim || ',',
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
      const h = 'email,name,attributes\n'
        + 'user1@mail.com,"User One","{""age"": 42, ""planet"": ""Mars""}"\n'
        + 'user2@mail.com,"User Two","{""age"": 24, ""job"": ""Time Traveller""}"';

      this.example = h;
    },

    resetForm() {
      this.form.mode = 'subscribe';
      this.form.overwriteUserInfo = false;
      this.form.overwriteSubStatus = false;
      this.form.file = null;
      this.form.lists = [];
      this.form.subStatus = 'unconfirmed';
      this.form.delim = ',';
      this.form.fieldMap = {
        email: '',
        name: '',
        attributes: '',
      };
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
      this.isProcessing = true;

      // Prepare the upload payload.
      const params = new FormData();
      params.set('params', JSON.stringify({
        mode: this.form.mode,
        subscription_status: this.form.subStatus,
        delim: this.form.delim,
        lists: this.form.lists.map((l) => l.id),
        overwrite_userinfo: this.form.overwriteUserInfo,
        overwrite_subscription_status: this.form.overwriteSubStatus,
        field_map: {
          email: this.form.fieldMap.email,
          name: this.form.fieldMap.name,
          attributes: this.form.fieldMap.attributes,
        },
      }));
      params.set('file', this.form.file);

      // Post.
      this.$api.importSubscribers(params).then(() => {
        // On file upload, show a confirmation.
        this.$utils.toast(this.$t('import.importStarted'));

        // Start polling status.
        this.pollStatus();
      }, () => {
        this.isProcessing = false;
        this.form.file = null;
      });
    },
  },

  computed: {
    ...mapState(['lists']),

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

    const ids = this.$utils.parseQueryIDs(this.$route.query.list_id);
    if (ids.length > 0 && this.lists.results) {
      this.$nextTick(() => {
        this.form.lists = this.lists.results.filter((l) => ids.indexOf(l.id) > -1);
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
  padding: 0.3rem 0.5rem;
  line-height: 1.2;
  font-size: 0.85rem;
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
