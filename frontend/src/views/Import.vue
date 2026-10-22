<template>
  <section class="import">
    <h1 class="title is-4 mb-4">{{ $t('import.pageTitle') }}</h1>
    <div class="tabs is-boxed" role="tablist" :aria-label="$t('import.pageTitle')" data-cy="import-tabs">
      <ul>
        <li :class="{ 'is-active': activeImportTab === 'private' }">
          <a href="#" role="tab" :aria-selected="activeImportTab === 'private'"
            data-cy="import-tab-private" @click.prevent="setImportTab('private')">{{ $t('import.privateTab') }}</a>
        </li>
        <li v-if="canMaintainPoolMaster" :class="{ 'is-active': activeImportTab === 'pool' }">
          <a href="#" role="tab" :aria-selected="activeImportTab === 'pool'"
            data-cy="import-tab-pool" @click.prevent="setImportTab('pool')">{{ $t('import.poolTab') }}</a>
        </li>
      </ul>
    </div>
    <h2 class="title is-5">
      {{ poolImport ? $t('import.poolTitle') : $t('import.title') }}
    </h2>
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
            <div v-if="showSubscriptionStatus" class="column" data-cy="import-subscription-status">
              <b-field :label="$t('globals.fields.status')" :addons="false">
                <b-radio v-model="form.subStatus" name="subStatus" native-value="unconfirmed"
                  data-cy="check-unconfirmed">
                  {{ $t('customers.status.unconfirmed') }}
                </b-radio>
                <b-radio v-model="form.subStatus" name="subStatus" native-value="confirmed" data-cy="check-confirmed">
                  {{ $t('customers.status.confirmed') }}
                </b-radio>
                <p class="help">{{ $t('import.statusHelp') }}</p>
              </b-field>
            </div>
          </div>
          <p v-if="poolImport && form.mode === 'blocklist'" class="help mb-4" data-cy="pool-blocklist-help">
            {{ $t('import.poolBlacklistHelp') }}
          </p>
          <div v-if="form.mode === 'subscribe' || poolImport" class="import-list-setup">
            <customer-list-selector :key="activeImportTab"
              :label="$t(poolImport ? 'menu.poolLists' : 'globals.terms.customer_lists')"
              :placeholder="poolImport ? $t('import.selectPoolList') : $t('import.listSubHelp')"
              :message="poolImport ? $t('import.poolListHelp') : $t('import.listSubHelp')"
              v-model="form.customer_lists" :selected="form.customer_lists" :all="importListOptions"
              :max-selected="poolImport ? 1 : 0" />
            <p v-if="canCreateList" class="import-list-create">
              {{ $t('import.noListPrompt') }}
              <a href="#" data-cy="import-create-list" @click.prevent="showCreateList">
                {{ $t(poolImport ? 'import.createPoolList' : 'import.createPrivateList') }}
              </a>
            </p>
          </div>
          <p v-if="hasInvalidPoolSelection" class="help has-text-danger">{{ $t('import.poolListSelectionError') }}</p>

          <div v-if="poolImport && selectedPoolLists.length === 1 && $can('pools:manage', 'pools:delivery_manage')"
            class="import-pool-manager" data-cy="import-pool-manager">
            <pool-manager :key="selectedPoolLists[0].id" :pool="selectedPoolLists[0]" />
          </div>

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
              <b-field :label="$t('import.mapNameField')" :message="poolImport ? $t('import.poolNameFieldHelp') : ''">
                <b-select v-model="form.fieldMap.name" expanded>
                  <option value="">{{ $t('globals.terms.none') }}</option>
                  <option v-for="col in preview.columns" :key="`name-${col.value}`" :value="col.value">
                    {{ col.label }}
                  </option>
                </b-select>
              </b-field>
            </div>
          </div>

          <div v-if="form.mode === 'subscribe' || poolImport" class="columns">
            <div class="column is-4">
              <b-field :label="$t('import.mapCustomerCodeField')"
                :message="$t('import.mapCustomerCodeFieldHelp')">
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
                <b-select v-model="form.fieldMap.allocation_department" expanded required data-cy="import-map-allocation-department">
                  <option value="">{{ $t('globals.terms.none') }}</option>
                  <option v-for="col in preview.columns" :key="`allocation_department-${col.value}`" :value="col.value">
                    {{ col.label }}
                  </option>
                </b-select>
              </b-field>
            </div>
            <div class="column is-8">
              <b-field :label="$t('import.mapReplyToField')" :message="$t('import.mapReplyToFieldHelp')">
                <b-select v-model="form.fieldMap.reply_to" expanded data-cy="import-map-reply-to">
                  <option value="">{{ $t('globals.terms.none') }}</option>
                  <option v-for="col in preview.columns" :key="`reply_to-${col.value}`" :value="col.value">
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
              :loading="isProcessing" data-cy="btn-upload-import">
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
            <span>客户编号,</span> <span>姓名,</span> <span>邮箱,</span> <span>分配部门,</span> <span>回信邮箱</span>
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

      <section v-if="poolImport && poolImportResult" class="import-result" data-cy="pool-import-result">
        <header class="import-result-header">
          <div>
            <h3 class="title is-5">{{ $t('import.poolResultTitle') }}</h3>
          </div>
        </header>

        <div class="import-result-metrics">
          <div class="import-result-metric metric-neutral" data-cy="pool-result-total">
            <b-icon icon="account-group-outline" />
            <span>{{ $t('import.poolResultTotalLabel') }}</span>
            <strong>{{ formatCount(poolImportResult.total) }}</strong>
          </div>
          <div class="import-result-metric metric-success">
            <b-icon icon="check-circle-outline" />
            <span>{{ $t('import.poolResultCreatedLabel') }}</span>
            <strong>{{ formatCount(poolImportResult.created) }}</strong>
          </div>
          <div class="import-result-metric metric-info">
            <b-icon icon="account-check-outline" />
            <span>{{ $t('import.poolResultExistingLabel') }}</span>
            <strong>{{ formatCount(poolImportResult.existing) }}</strong>
          </div>
          <button type="button" class="import-result-metric metric-warning"
            :aria-label="$t('import.poolResultFilterConflicts')"
            :aria-pressed="issueFilter === 'conflict'"
            @click="setIssueFilter('conflict')">
            <b-icon icon="alert-outline" />
            <span>{{ $t('import.poolResultConflictsLabel') }}</span>
            <strong>{{ formatCount(poolImportResult.conflicts) }}</strong>
          </button>
          <button type="button" class="import-result-metric metric-danger"
            :aria-label="$t('import.poolResultFilterInvalid')"
            :aria-pressed="issueFilter === 'invalid'"
            @click="setIssueFilter('invalid')">
            <b-icon icon="close-circle-outline" />
            <span>{{ $t('import.poolResultInvalidLabel') }}</span>
            <strong>{{ formatCount(poolImportResult.invalid) }}</strong>
          </button>
          <button type="button" class="import-result-metric metric-neutral"
            :aria-label="$t('import.poolResultFilterDuplicate')"
            :aria-pressed="issueFilter === 'duplicate'"
            @click="setIssueFilter('duplicate')">
            <b-icon icon="content-copy" />
            <span>{{ $t('import.poolResultDuplicatesLabel') }}</span>
            <strong>{{ formatCount(poolImportResult.duplicates) }}</strong>
          </button>
        </div>
        <p v-if="poolImportResult.blocklisted" class="import-blocklist-result" data-cy="pool-result-blocklisted">
          <b-tag type="is-danger">{{ $t('import.poolResultBlocklisted', { count: poolImportResult.blocklisted }) }}</b-tag>
        </p>

        <section class="import-issues" aria-labelledby="import-issues-title">
          <div class="import-issues-heading" :class="{ 'has-no-issues': !poolImportIssues.length }">
            <div class="import-issues-title">
              <b-icon :icon="poolImportIssues.length ? 'alert-circle-outline' : 'check-circle-outline'"
                :type="poolImportIssues.length ? 'is-danger' : 'is-success'" />
              <div>
                <h4 id="import-issues-title" class="title is-6">{{ $t('import.poolResultIssues') }}</h4>
                <p class="help">{{ $t('import.poolResultIssuesHelp') }}</p>
              </div>
            </div>
            <b-button type="is-primary" outlined icon-left="file-download-outline"
              :disabled="!poolImportIssues.length" data-cy="export-pool-import-issues"
              @click="exportPoolImportIssues">
              {{ $t('import.poolResultExport') }}
            </b-button>
          </div>
          <p class="help import-issue-total">{{ $t('import.poolResultIssueCount', { count: poolImportIssues.length }) }}</p>

          <div class="import-issue-toolbar">
            <b-field :label="$t('import.poolResultErrorType')" class="issue-filter">
              <b-select v-model="issueFilter" expanded data-cy="pool-issue-filter">
                <option value="all">{{ $t('import.poolResultAllTypes') }}</option>
                <option value="invalid">{{ $t('import.poolResultInvalidLabel') }}</option>
                <option value="conflict">{{ $t('import.poolResultConflictsLabel') }}</option>
                <option value="duplicate">{{ $t('import.poolResultDuplicatesLabel') }}</option>
              </b-select>
            </b-field>
            <b-field :label="$t('import.poolResultSearch')" class="issue-search lm-search-field">
              <b-input v-model.trim="issueSearch" icon="magnify" :placeholder="$t('import.poolResultSearchHelp')"
                data-cy="pool-issue-search" />
            </b-field>
            <p class="issue-range" data-cy="pool-issue-range">
              {{ $t('import.poolResultRange', { from: issueRange.from, to: issueRange.to, count: issueRange.total }) }}
            </p>
          </div>

          <div v-if="!filteredPoolIssues.length" class="import-issues-empty">
            {{ $t(poolImportIssues.length ? 'import.poolResultNoIssues' : 'import.poolResultNoErrors') }}
          </div>
          <div v-else class="import-issues-table-wrap">
            <b-table :data="pagedPoolIssues" :mobile-cards="false" :paginated="false" striped hoverable>
              <b-table-column field="row" :label="$t('import.poolResultRow')" width="70" v-slot="props">
                {{ props.row.row }}
              </b-table-column>
              <b-table-column field="customer_code" :label="$t('import.poolResultCustomerCode')" width="140" v-slot="props">
                <span class="issue-code">{{ props.row.customerCode || '-' }}</span>
              </b-table-column>
              <b-table-column field="allocation_department" :label="$t('import.poolResultDepartment')" width="160" v-slot="props">
                {{ props.row.allocationDepartment || '-' }}
              </b-table-column>
              <b-table-column field="reason" :label="$t('import.poolResultErrorType')" width="130" v-slot="props">
                <b-tag :type="issueTagType(props.row.reason)" size="is-small">
                  {{ issueTypeLabel(props.row.reason) }}
                </b-tag>
              </b-table-column>
              <b-table-column field="reason_detail" :label="$t('import.poolResultReason')" v-slot="props">
                {{ poolIssueReason(props.row.reason, props.row.allocationDepartment) }}
              </b-table-column>
              <b-table-column field="actions" :label="$t('import.poolResultAction')" width="100" centered v-slot="props">
                <button type="button" class="button is-text is-small" data-cy="view-pool-issue" @click="showPoolIssue(props.row)">
                  {{ $t('import.poolResultViewDetails') }}
                </button>
              </b-table-column>
            </b-table>
          </div>

          <app-pagination v-if="filteredPoolIssues.length" :total="filteredPoolIssues.length"
            :current="issuePage" :per-page="issuePageSize" @change="issuePage = $event"
            @per-page-change="changeIssuePageSize" />
        </section>

        <div class="import-result-actions">
          <p class="help">{{ $t('import.poolResultLocationHelp') }}</p>
          <b-button type="is-primary" icon-left="account-group-outline" data-cy="view-pool-contacts"
            @click="viewImportedPool">
            {{ $t('import.poolResultViewPool') }}
          </b-button>
        </div>
      </section>

      <b-modal :active.sync="issueDetailVisible" :width="560" aria-modal>
        <section v-if="selectedPoolIssue" class="box import-issue-detail" role="dialog"
          aria-labelledby="import-issue-detail-title">
          <h3 id="import-issue-detail-title" class="title is-5">{{ $t('import.poolResultDetailTitle') }}</h3>
          <dl>
            <div><dt>{{ $t('import.poolResultRow') }}</dt><dd>{{ selectedPoolIssue.row }}</dd></div>
            <div><dt>{{ $t('import.poolResultCustomerCode') }}</dt><dd>{{ selectedPoolIssue.customerCode || '-' }}</dd></div>
            <div><dt>{{ $t('import.poolResultDepartment') }}</dt><dd>{{ selectedPoolIssue.allocationDepartment || '-' }}</dd></div>
            <div><dt>{{ $t('import.poolResultErrorType') }}</dt><dd>{{ issueTypeLabel(selectedPoolIssue.reason) }}</dd></div>
            <div><dt>{{ $t('import.poolResultReason') }}</dt><dd>{{ poolIssueReason(selectedPoolIssue.reason, selectedPoolIssue.allocationDepartment) }}</dd></div>
          </dl>
          <div class="buttons is-right">
            <b-button @click="issueDetailVisible = false">{{ $t('globals.buttons.close') }}</b-button>
          </div>
        </section>
      </b-modal>
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

    <b-modal scroll="keep" :aria-modal="true" :active.sync="createListVisible" :width="600">
      <customer-list-form :key="createFormKey" :data="{ type: poolImport ? 'pool' : 'private' }"
        :list-group="activeImportTab" @finished="onListCreated" />
    </b-modal>
  </section>
</template>

<script>
import Vue from 'vue';
import Papa from 'papaparse';
import * as XLSX from 'xlsx';
import { mapState } from 'vuex';
import CustomerListSelector from '../components/CustomerListSelector.vue';
import PoolManager from '../components/PoolManager.vue';
import LogView from '../components/LogView.vue';
import CustomerListForm from './CustomerListForm.vue';
import AppPagination from '../components/AppPagination.vue';
import { isOwnedActiveWorkspaceCustomerList } from '../utils/workspace';

export default Vue.extend({
  components: {
    CustomerListSelector,
    CustomerListForm,
    LogView,
    PoolManager,
    AppPagination,
  },

  props: {
    data: { type: Object, default: () => { } },
    isEditing: { type: Boolean, default: false },
  },

  data() {
    return {
      activeImportTab: 'private',
      selectedListsByTab: { private: [], pool: [] },
      createListVisible: false,
      createFormKey: 0,
      form: {
        mode: 'subscribe',
        subStatus: 'confirmed',
        customer_lists: [],
        file: null,
        fieldMap: {
          email: '',
          name: '',
          customer_code: '',
          allocation_department: '',
          reply_to: '',
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
      issueFilter: 'all',
      issueSearch: '',
      issuePage: 1,
      issuePageSize: 20,
      issueDetailVisible: false,
      selectedPoolIssue: null,
    };
  },

  watch: {
    'form.mode': function formMode() {
      // Select the appropriate status radio whenever mode changes.
      this.$nextTick(() => {
        if (this.form.mode === 'subscribe') {
          this.form.subStatus = 'confirmed';
        } else {
          this.form.subStatus = 'unsubscribed';
        }
      });
    },

    showSubscriptionStatus(value) {
      if (!value && this.form.mode === 'subscribe') {
        this.form.subStatus = 'confirmed';
      }
    },

    'form.file': function onFileChanged(file) {
      if (!file) {
        this.clearPreview();
        return;
      }
      this.previewFromFile();
    },

    issueFilter() { this.issuePage = 1; },
    issueSearch() { this.issuePage = 1; },

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
          this.form.fieldMap.reply_to = '';
        }
      });
    },

  },

  methods: {
    setImportTab(tab) {
      if (tab === this.activeImportTab || (tab === 'pool' && !this.canMaintainPoolMaster)) {
        return;
      }
      this.selectedListsByTab[this.activeImportTab] = [...this.form.customer_lists];
      this.activeImportTab = tab;
      this.form.customer_lists = [...this.selectedListsByTab[tab]];
      // The two imports require different source columns. Do not carry a file or
      // its field mapping from one workflow into the other.
      this.clearFile();
      if (tab === 'pool') {
        this.form.mode = 'subscribe';
        this.preview.firstRowHeader = true;
      }
    },

    showCreateList() {
      if (!this.canCreateList) {
        return;
      }
      this.createFormKey += 1;
      this.createListVisible = true;
    },

    onListCreated(list) {
      this.createListVisible = false;
      this.form.customer_lists = [list];
      this.selectedListsByTab[this.activeImportTab] = [list];
      this.$api.getLists({ minimal: true, per_page: 'all', status: 'active' });
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
      this.form.fieldMap.customer_code = '';
      this.form.fieldMap.allocation_department = '';
      this.form.fieldMap.reply_to = '';
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
        keyMap.reply_to = ['reply_to', 'reply to', 'reply-to', 'reply_email', '回信邮箱', '回件邮箱', '回复邮箱'];
        keyMap.allocation_department = [
          'allocation_department', 'allocation department', '分配部门', '分配部门名称', 'department', '部门',
        ];
      }

      Object.keys(keyMap).forEach((target) => {
        if (this.form.fieldMap[target]) {
          return;
        }
        const col = keyMap[target].map((alias) => this.preview.columns.find((c) => (
          this.normalizeFieldName(c.header || c.value) === alias
        ))).find(Boolean);
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
      this.form.fieldMap.reply_to = '';
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

      this.form.poolExample = '客户编号,姓名,邮箱,分配部门,回信邮箱\n'
        + 'AE-20004,"JAGATHISH PICHAIYAPPA",jagath.p@example.com,分表1,replies@example.com\n'
        + 'AE-20005,"Contact Two",contact2@example.com,分表2,';
    },

    resetForm() {
      this.form.mode = 'subscribe';
      this.form.file = null;
      this.form.customer_lists = [];
      this.selectedListsByTab = { private: [], pool: [] };
      this.form.subStatus = 'confirmed';
      this.form.fieldMap = {
        email: '',
        name: '',
        customer_code: '',
        allocation_department: '',
        reply_to: '',
      };
      this.poolImportResult = null;
      this.clearPreview();
    },

    onUpload() {
      if (this.form.mode === 'subscribe' && !this.poolImport) {
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
        fieldMap.reply_to = this.form.fieldMap.reply_to;
      }
      let subscriptionStatus = 'confirmed';
      if (this.form.mode === 'blocklist') {
        subscriptionStatus = 'unsubscribed';
      } else if (this.showSubscriptionStatus) {
        subscriptionStatus = this.form.subStatus;
      }
      params.set('params', JSON.stringify({
        mode: this.form.mode,
        subscription_status: subscriptionStatus,
        customer_list_ids: this.form.customer_lists.map((l) => l.id),
        overwrite_userinfo: !this.poolImport && this.form.mode === 'subscribe',
        overwrite_subscription_status: !this.poolImport && this.form.mode === 'subscribe',
        field_map: fieldMap,
      }));
      params.set('file', this.form.file);

      // Post.
      this.$api.importCustomers(params).then((result) => {
        if (result && result.target === 'pool') {
          this.poolImportResult = {
            ...result,
            issues: (result.issues || []).map((issue) => ({
              ...issue,
              customerCode: issue.customerCode || issue.customer_code || '',
              allocationDepartment: issue.allocationDepartment || issue.allocation_department || '',
            })),
          };
          this.issueFilter = 'all';
          this.issueSearch = '';
          this.issuePage = 1;
          this.issueDetailVisible = false;
          this.selectedPoolIssue = null;
          this.isProcessing = false;
          this.form.file = null;
          this.clearPreview();
          this.$nextTick(() => {
            const resultSection = this.$el.querySelector('.import-result');
            if (resultSection) resultSection.scrollIntoView({ block: 'start' });
          });
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
      if ((this.form.mode === 'subscribe' || this.poolImport) && this.form.customer_lists.length === 0) {
        return true;
      }
      if ((this.form.mode === 'subscribe' || this.poolImport) && !this.form.fieldMap.customer_code) {
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
        invalid_reply_to: this.$t('import.poolIssueInvalidReplyTo'),
        customer_code_conflict: this.$t('import.poolIssueCustomerCodeConflict'),
        duplicate_row: this.$t('import.poolIssueDuplicateRow'),
      };
      return labels[reason] || reason;
    },

    issueType(reason) {
      if (reason === 'customer_code_conflict') return 'conflict';
      if (reason === 'duplicate_row') return 'duplicate';
      return 'invalid';
    },

    issueTypeLabel(reason) {
      return this.$t({
        invalid: 'import.poolResultInvalidLabel',
        conflict: 'import.poolResultConflictsLabel',
        duplicate: 'import.poolResultDuplicatesLabel',
      }[this.issueType(reason)]);
    },

    issueTagType(reason) {
      return { invalid: 'is-danger', conflict: 'is-warning', duplicate: 'is-info' }[this.issueType(reason)];
    },

    setIssueFilter(filter) {
      this.issueFilter = filter;
      this.issuePage = 1;
      this.$nextTick(() => {
        const section = this.$el.querySelector('.import-issues');
        if (section) section.scrollIntoView({ behavior: 'smooth', block: 'start' });
      });
    },

    changeIssuePageSize(value) {
      this.issuePageSize = value;
      this.issuePage = 1;
    },

    formatCount(value) {
      return new Intl.NumberFormat(this.$i18n.locale).format(Number(value) || 0);
    },

    showPoolIssue(issue) {
      this.selectedPoolIssue = issue;
      this.issueDetailVisible = true;
    },

    exportPoolImportIssues() {
      if (!this.poolImportIssues.length) return;
      const rows = [
        [this.$t('import.poolResultRow'), this.$t('import.poolResultCustomerCode'),
          this.$t('import.poolResultDepartment'), this.$t('import.poolResultErrorType'),
          this.$t('import.poolResultReason')],
        ...this.poolImportIssues.map((issue) => [
          String(issue.row),
          issue.customerCode || '',
          issue.allocationDepartment || '',
          this.issueTypeLabel(issue.reason),
          this.poolIssueReason(issue.reason, issue.allocationDepartment),
        ]),
      ];
      const sheet = XLSX.utils.aoa_to_sheet(rows);
      sheet['!cols'] = [{ wch: 12 }, { wch: 22 }, { wch: 24 }, { wch: 18 }, { wch: 48 }];
      sheet['!autofilter'] = { ref: sheet['!ref'] };
      const workbook = XLSX.utils.book_new();
      XLSX.utils.book_append_sheet(workbook, sheet, 'Import errors');
      XLSX.writeFile(workbook, 'customer-import-errors.xlsx');
    },

    viewImportedPool() {
      const poolID = Number(this.poolImportResult
        && (this.poolImportResult.poolId || this.poolImportResult.pool_id));
      if (poolID > 0) {
        this.$router.push({ name: 'poolListContacts', params: { customerListID: poolID } });
      }
    },
  },

  computed: {
    ...mapState(['customer_lists', 'profile', 'workspace']),

    canMaintainPoolMaster() {
      return this.$can('pools:master_manage');
    },

    selectedPoolLists() {
      return this.form.customer_lists.filter((list) => list.type === 'pool');
    },

    poolImportIssues() {
      return (this.poolImportResult && this.poolImportResult.issues) || [];
    },

    filteredPoolIssues() {
      const query = this.issueSearch.toLocaleLowerCase();
      return this.poolImportIssues.filter((issue) => {
        const matchesType = this.issueFilter === 'all' || this.issueType(issue.reason) === this.issueFilter;
        if (!matchesType || !query) return matchesType;
        const searchable = [issue.row, issue.customerCode, issue.allocationDepartment,
          this.poolIssueReason(issue.reason, issue.allocationDepartment)].join(' ').toLocaleLowerCase();
        return searchable.includes(query);
      });
    },

    pagedPoolIssues() {
      const start = (this.issuePage - 1) * this.issuePageSize;
      return this.filteredPoolIssues.slice(start, start + this.issuePageSize);
    },

    issueRange() {
      const total = this.filteredPoolIssues.length;
      const from = total ? (this.issuePage - 1) * this.issuePageSize + 1 : 0;
      return { from, to: Math.min(total, this.issuePage * this.issuePageSize), total };
    },

    showSubscriptionStatus() {
      return !this.poolImport && this.form.mode === 'subscribe'
        && this.form.customer_lists.some((list) => list.optin === 'double');
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
      return this.canMaintainPoolMaster && this.activeImportTab === 'pool';
    },

    hasInvalidPoolSelection() {
      const selected = this.form.customer_lists;
      return this.poolImport
        ? selected.length > 1 || selected.some((list) => list.type !== 'pool')
        : selected.some((list) => list.type === 'pool' || list.type === 'org_pool_allocation');
    },

    importListOptions() {
      return this.poolImport ? this.poolImportLists : this.regularImportLists;
    },

    canCreateList() {
      return this.poolImport
        ? this.canMaintainPoolMaster
        : this.$canCreateWorkspaceResource('customer_lists:manage_all');
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
    this.$api.getLists({ minimal: true, per_page: 'all', status: 'active' }).then(() => {
      if (ids.length === 0) {
        return;
      }
      const pool = this.canMaintainPoolMaster && this.poolImportLists.find((list) => ids.includes(list.id));
      if (pool) {
        this.setImportTab('pool');
        this.form.customer_lists = [pool];
      } else {
        this.form.customer_lists = this.regularImportLists.filter((list) => ids.includes(list.id));
      }
    });
  },
});
</script>

<style scoped>
.import-list-setup {
  display: flex;
  align-items: flex-start;
  gap: 1rem;
}

.import-list-setup .customer_list-selector {
  flex: 1;
  min-width: 0;
}

.import-list-create {
  padding-top: 0.65rem;
  white-space: nowrap;
}

.import-pool-manager {
  margin: var(--lm-space-6) 0;
}

@media (max-width: 768px) {
  .import-list-setup {
    display: block;
  }

  .import-list-create {
    padding-top: 0;
  }
}

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

.import-result {
  margin-top: var(--lm-space-8);
  scroll-margin-top: 80px;
  overflow: hidden;
  border: 1px solid var(--lm-color-border);
  border-radius: var(--lm-radius-md);
  background: var(--lm-color-surface);
}

.import-result-header,
.import-issues-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--lm-space-4);
}

.import-result-header {
  padding: var(--lm-space-6);
  background: var(--lm-color-surface-subtle);
  border-bottom: 1px solid var(--lm-color-border);
}

.import-result-header .title,
.import-issues-heading .title { margin-bottom: var(--lm-space-1); }
.import-result-header .help { max-width: 820px; margin: 0; }

.import-result-metrics {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  gap: var(--lm-space-3);
  padding: var(--lm-space-4) var(--lm-space-6);
}

.import-result-metric {
  position: relative;
  display: flex;
  min-width: 0;
  min-height: 88px;
  flex-direction: column;
  justify-content: center;
  gap: var(--lm-space-2);
  padding: var(--lm-space-3) var(--lm-space-4);
  border: 1px solid var(--lm-color-border);
  border-radius: var(--lm-radius-md);
  background: var(--lm-color-surface-subtle);
  color: var(--lm-color-text);
  text-align: left;
}

.import-result-metric > .icon { position: absolute; right: var(--lm-space-3); top: var(--lm-space-3); color: inherit; }
.import-result-metric > span:not(.icon) { padding-right: var(--lm-space-6); }

button.import-result-metric {
  width: 100%;
  cursor: pointer;
  font: inherit;
}

.import-result-metric > span:not(.icon) { color: var(--lm-color-text-muted); font-size: var(--lm-font-size-toolbar); }
.import-result-metric strong { color: var(--lm-color-text); font-size: 24px; line-height: 1.15; }
button.import-result-metric:focus-visible,
button.import-result-metric:hover,
button.import-result-metric[aria-pressed="true"] { box-shadow: var(--lm-focus-ring); outline: none; }
.metric-success { border-color: var(--lm-color-success); color: var(--lm-color-success); }
.metric-success strong { color: var(--lm-color-success); }
.metric-info { border-color: var(--lm-color-primary); color: var(--lm-color-primary); background: var(--lm-color-primary-soft); }
.metric-info strong { color: var(--lm-color-primary); }
.metric-warning { border-color: var(--lm-color-warning); color: var(--lm-color-warning); }
.metric-warning strong { color: var(--lm-color-warning); }
.metric-danger { border-color: var(--lm-color-danger); color: var(--lm-color-danger); }
.metric-danger strong { color: var(--lm-color-danger); }
.metric-success { background: color-mix(in srgb, var(--lm-color-success) 5%, var(--lm-color-surface)); }
.metric-warning { background: color-mix(in srgb, var(--lm-color-warning) 5%, var(--lm-color-surface)); }
.metric-danger { background: color-mix(in srgb, var(--lm-color-danger) 5%, var(--lm-color-surface)); }
.import-blocklist-result { padding: 0 var(--lm-space-6) var(--lm-space-4); }

.import-issues { padding: var(--lm-space-6); border-top: 1px solid var(--lm-color-border); scroll-margin-top: 80px; }
.import-issues-heading {
  margin: calc(-1 * var(--lm-space-6)) calc(-1 * var(--lm-space-6)) var(--lm-space-4);
  padding: var(--lm-space-4) var(--lm-space-6);
  background: color-mix(in srgb, var(--lm-color-danger) 4%, var(--lm-color-surface));
}
.import-issues-title { display: flex; align-items: center; gap: var(--lm-space-3); }
.import-issues-heading.has-no-issues { background: color-mix(in srgb, var(--lm-color-success) 4%, var(--lm-color-surface)); }
.import-issues-heading :deep(.button.is-primary.is-outlined) { color: var(--lm-color-primary); border-color: var(--lm-color-primary); }
.import-issues-heading :deep(.button.is-primary.is-outlined:disabled) { opacity: 0.5; }
.import-issues-heading :deep(.button.is-primary.is-outlined:hover) { background: var(--lm-color-primary-soft); }
.import-issue-total { margin-bottom: var(--lm-space-3); color: var(--lm-color-text-muted); }

.import-issue-toolbar {
  display: grid;
  grid-template-columns: minmax(160px, 240px) minmax(240px, 1fr) auto;
  align-items: end;
  gap: var(--lm-space-4);
  margin-bottom: var(--lm-space-3);
}

.issue-range { margin: 0 0 10px; color: var(--lm-color-text-muted); white-space: nowrap; }
.import-issue-toolbar .field { margin-bottom: 0; }
.import-issues-table-wrap { width: 100%; overflow-x: auto; }
.import-issues-table-wrap :deep(table) { table-layout: fixed; width: 100%; }
.import-issues-table-wrap :deep(th) {
  background: var(--lm-color-surface-subtle);
  color: var(--lm-color-text);
  font-weight: 700;
  white-space: nowrap;
}
.issue-code { font-variant-numeric: tabular-nums; }
.import-issues-table-wrap :deep(td) { overflow-wrap: anywhere; }
.import-issues-table-wrap :deep(.button.is-text) { white-space: nowrap; color: var(--lm-color-primary); text-decoration: none; }
.import-issues-table-wrap :deep(.tag) { border-radius: var(--lm-radius-sm); font-weight: 400; }
.import-issues-table-wrap :deep(.tag.is-danger) { color: var(--lm-color-danger); background: color-mix(in srgb, var(--lm-color-danger) 8%, var(--lm-color-surface)); }
.import-issues-table-wrap :deep(.tag.is-warning) { color: var(--lm-color-warning); background: color-mix(in srgb, var(--lm-color-warning) 8%, var(--lm-color-surface)); }
.import-issues-table-wrap :deep(.tag.is-info) { color: var(--lm-color-primary); background: var(--lm-color-primary-soft); }
.import-issues-empty {
  padding: var(--lm-space-8) var(--lm-space-4);
  border: 1px dashed var(--lm-color-border-strong);
  border-radius: var(--lm-radius-md);
  color: var(--lm-color-text-muted);
  text-align: center;
}
.import-result-actions { padding: 0 var(--lm-space-6) var(--lm-space-6); }
.import-result-actions .help { margin-bottom: var(--lm-space-3); }
.import-issue-detail { margin: var(--lm-space-6); background: var(--lm-color-surface); }
.import-issue-detail dl { margin: var(--lm-space-6) 0; }
.import-issue-detail dl > div {
  display: grid;
  grid-template-columns: minmax(120px, 1fr) 2fr;
  gap: var(--lm-space-4);
  padding: var(--lm-space-3) 0;
  border-bottom: 1px solid var(--lm-color-border);
}
.import-issue-detail dt { color: var(--lm-color-text-muted); }
.import-issue-detail dd { margin: 0; overflow-wrap: anywhere; }

@media (max-width: 1024px) {
  .import-result-metrics { grid-template-columns: repeat(3, minmax(0, 1fr)); }
  .import-issue-toolbar { grid-template-columns: minmax(160px, 220px) minmax(200px, 1fr); }
  .issue-range { grid-column: 1 / -1; margin: 0; }
}

@media (max-width: 768px) {
  .import-result-header,
  .import-issues-heading { align-items: stretch; flex-direction: column; }
  .import-result-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); padding: var(--lm-space-4); }
  .import-result-header,
  .import-issues { padding: var(--lm-space-4); }
  .import-issues-heading { margin: calc(-1 * var(--lm-space-4)) calc(-1 * var(--lm-space-4)) var(--lm-space-4); padding: var(--lm-space-4); }
  .import-issue-toolbar { grid-template-columns: minmax(0, 1fr); }
  .issue-range { white-space: normal; }
  .import-issues-table-wrap :deep(table) { min-width: 860px; }
  .import-result-actions { padding: 0 var(--lm-space-4) var(--lm-space-4); }
}
</style>
