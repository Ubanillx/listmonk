<template>
  <section class="audit content relative">
    <header class="page-header columns is-vcentered">
      <div class="column">
        <h1 class="title is-4 mb-1">
          {{ $t('audit.title') }}
        </h1>
        <p class="has-text-grey is-size-7">
          {{ $t('audit.description') }}
        </p>
      </div>
      <div class="column is-narrow">
        <div class="buttons">
          <b-button type="is-light" icon-left="download" :loading="exporting === 'all'" :disabled="!total || exporting !== ''"
            data-cy="audit-export-all" @click="exportEvents('all')">
            {{ $t('audit.exportAll') }}
          </b-button>
          <b-button type="is-primary" icon-left="download" :loading="exporting === 'selected'"
            :disabled="!selectedEvents.length || exporting !== ''" data-cy="audit-export-selected"
            @click="exportEvents('selected')">
            {{ $t('audit.exportSelected', { count: selectedEvents.length }) }}
          </b-button>
        </div>
      </div>
    </header>

    <div class="columns is-multiline mb-2">
      <div class="column is-4">
        <b-input v-model="filters.action" :placeholder="$t('audit.filterAction')" @keyup.enter.native="reload" />
      </div>
      <div class="column is-3">
        <b-select v-model="filters.result" expanded @input="reload">
          <option value="">{{ $t('audit.all') }}</option>
          <option value="success">{{ $t('audit.success') }}</option>
          <option value="failed">{{ $t('audit.failed') }}</option>
          <option value="denied">{{ $t('audit.denied') }}</option>
        </b-select>
      </div>
      <div class="column is-narrow">
        <b-button type="is-light" icon-left="refresh" @click="reload">
          {{ $t('audit.refresh') }}
        </b-button>
      </div>
    </div>

    <b-table :data="events" :loading="loading.auditEvents" detailed detail-key="id" hoverable checkable
      :checked-rows.sync="selectedEvents" @check="onTableCheck" data-cy="audit-table">
      <b-table-column v-slot="props" field="occurredAt" :label="$t('audit.occurredAt')" width="180">
        {{ $utils.niceDate(props.row.occurredAt, true) }}
      </b-table-column>
      <b-table-column v-slot="props" field="action" :label="$t('audit.action')">
        <div class="audit-cell-main">{{ actionTitle(props.row.action) }}</div>
        <code>{{ props.row.action }}</code>
      </b-table-column>
      <b-table-column v-slot="props" field="object" :label="$t('audit.object')" width="220">
        <div class="audit-cell-main">{{ objectTitle(props.row) }}</div>
        <div class="audit-cell-secondary">
          {{ objectTypeLabel(props.row.objectType) }}
          <span v-if="props.row.objectId"> · ID {{ props.row.objectId }}</span>
        </div>
      </b-table-column>
      <b-table-column v-slot="props" field="content" :label="$t('audit.content')" width="280">
        <div v-if="objectSummary(props.row)" class="audit-content-summary" :title="objectSummary(props.row)">
          {{ objectSummary(props.row) }}
        </div>
        <span v-else class="has-text-grey-light">{{ $t('audit.noContent') }}</span>
      </b-table-column>
      <b-table-column v-slot="props" field="actor" :label="$t('audit.actor')">
        <div class="audit-cell-main">{{ actorTitle(props.row) }}</div>
        <div class="audit-cell-secondary">
          {{ actorTypeLabel(props.row.actorType) }}
          <span v-if="actorName(props.row) && actorName(props.row) !== actorTitle(props.row)">
            · {{ detailLabel('name') }}: {{ actorName(props.row) }}
          </span>
          <span v-if="actorTokenName(props.row)">
            · {{ detailLabel('tokenName') }}: {{ actorTokenName(props.row) }}
          </span>
          <span v-if="props.row.actorUserId"> · ID {{ props.row.actorUserId }}</span>
        </div>
      </b-table-column>
      <b-table-column v-slot="props" field="result" :label="$t('audit.result')">
        <b-tag :type="resultType(props.row.result)">{{ resultLabel(props.row.result) }}</b-tag>
      </b-table-column>
      <b-table-column v-slot="props" field="reasonCode" :label="$t('audit.reason')">
        <div class="audit-cell-main">{{ reasonTitle(props.row.reasonCode) }}</div>
        <code v-if="props.row.reasonCode && reasonTitle(props.row.reasonCode) !== props.row.reasonCode">
          {{ props.row.reasonCode }}
        </code>
      </b-table-column>

      <template #detail="props">
        <div class="audit-detail">
          <div class="audit-detail-grid">
            <div>
              <span class="audit-detail-label">{{ $t('audit.object') }}</span>
              <strong>{{ objectTitle(props.row) }}</strong>
              <code v-if="props.row.objectId">ID {{ props.row.objectId }}</code>
            </div>
            <div>
              <span class="audit-detail-label">{{ $t('audit.actor') }}</span>
              <strong>{{ actorTitle(props.row) }}</strong>
              <span v-if="actorUsername(props.row)" class="audit-detail-inline">
                {{ detailLabel('username') }}: {{ actorUsername(props.row) }}
              </span>
              <code v-if="props.row.actorUserId">ID {{ props.row.actorUserId }}</code>
            </div>
          </div>

          <div class="audit-detail-section">
            <div class="audit-detail-heading">{{ $t('audit.content') }}</div>
            <dl v-if="objectDetailEntries(props.row).length" class="audit-detail-list">
              <template v-for="entry in objectDetailEntries(props.row)">
                <dt :key="`object-label-${entry.key}`">{{ detailLabel(entry.key) }}</dt>
                <dd :key="`object-value-${entry.key}`">{{ formatDetailValue(entry.value) }}</dd>
              </template>
            </dl>
            <p v-else class="has-text-grey">{{ $t('audit.noContent') }}</p>
          </div>

          <div v-if="businessMetadataEntries(props.row).length" class="audit-detail-section">
            <div class="audit-detail-heading">{{ $t('audit.metadata') }}</div>
            <dl class="audit-detail-list">
              <template v-for="entry in businessMetadataEntries(props.row)">
                <dt :key="`metadata-label-${entry.key}`">{{ detailLabel(entry.key) }}</dt>
                <dd :key="`metadata-value-${entry.key}`">{{ formatDetailValue(entry.value) }}</dd>
              </template>
            </dl>
          </div>

          <details v-if="technicalMetadata(props.row.metadata)" class="audit-technical-details">
            <summary>{{ $t('audit.technicalMetadata') }}</summary>
            <pre>{{ prettyMetadata(technicalMetadata(props.row.metadata)) }}</pre>
          </details>
          <div v-if="props.row.ip"><strong>{{ $t('audit.ip') }}:</strong> {{ props.row.ip }}</div>
          <div v-if="props.row.userAgent"><strong>{{ $t('audit.userAgent') }}:</strong> {{ props.row.userAgent }}</div>
          <div v-if="props.row.requestId"><strong>{{ $t('audit.requestId') }}:</strong> {{ props.row.requestId }}</div>
        </div>
      </template>

      <template #empty v-if="!loading.auditEvents">
        <empty-placeholder :label="$t('audit.noEvents')" />
      </template>
    </b-table>

    <b-pagination v-if="total > perPage" :total="total" :current.sync="page" :per-page="perPage"
      order="is-centered" @change="getEvents" />
  </section>
</template>

<script>
import Vue from 'vue';
import { mapState } from 'vuex';
import EmptyPlaceholder from '../components/EmptyPlaceholder.vue';

export default Vue.extend({
  components: { EmptyPlaceholder },

  data() {
    return {
      events: [],
      selectedEvents: [],
      total: 0,
      page: 1,
      perPage: 50,
      filters: { action: '', result: '' },
      exporting: '',
    };
  },

  computed: {
    ...mapState(['loading']),
  },

  methods: {
    getEvents() {
      this.selectedEvents = [];
      this.$api.getAuditEvents({
        page: this.page,
        per_page: this.perPage,
        action: this.filters.action,
        result: this.filters.result,
      }).then((data) => {
        this.events = data.results || [];
        this.total = data.total || 0;
      });
    },

    onTableCheck() {
      // Buefy owns the checked row array. Keeping this hook explicit documents
      // that selection is scoped to the currently loaded server page.
    },

    async exportEvents(scope) {
      const params = {
        scope,
        action: this.filters.action,
        result: this.filters.result,
      };
      if (scope === 'selected') {
        params.ids = this.selectedEvents.map((event) => event.id);
      }
      this.exporting = scope;
      try {
        const blob = await this.$api.exportAuditEvents(params);
        const url = window.URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = `audit-events-${scope}-${new Date().toISOString().slice(0, 10)}.csv`;
        document.body.appendChild(link);
        link.click();
        link.remove();
        window.URL.revokeObjectURL(url);
      } finally {
        this.exporting = '';
      }
    },

    reload() {
      this.page = 1;
      this.getEvents();
    },

    resultType(result) {
      return { success: 'is-success', failed: 'is-danger', denied: 'is-warning' }[result] || 'is-light';
    },

    resultLabel(result) {
      return this.$t(`audit.${result}`);
    },

    reasonTitle(reason) {
      if (!reason) {
        return '-';
      }
      const key = `audit.reasons.${reason}`;
      const translated = this.$t(key);
      return translated === key ? reason : translated;
    },

    actionTitle(action) {
      const key = `audit.actions.${action}`;
      const translated = this.$t(key);
      return translated === key ? action : translated;
    },

    objectDetails(row) {
      const metadata = row.metadata || {};
      return metadata.objectDetails || metadata.object_details || {};
    },

    actorDetails(row) {
      const metadata = row.metadata || {};
      return metadata.actorDetails || metadata.actor_details || {};
    },

    objectTypeLabel(type) {
      const terms = {
        campaign: 'globals.terms.campaign',
        customer: 'globals.terms.customer',
        customer_list: 'globals.terms.customer_list',
        media: 'globals.terms.media',
        media_folder: 'audit.mediaFolder',
        template: 'globals.terms.template',
        user: 'globals.terms.user',
        organization: 'globals.terms.organizations',
      };
      return terms[type] ? this.$tc(terms[type], 1) : type;
    },

    actorTypeLabel(type) {
      const labels = {
        user: this.$tc('globals.terms.user', 1),
        customer: this.$tc('globals.terms.customer', 1),
        api_key: this.$t('audit.apiKey'),
        system: this.$t('audit.system'),
        webhook: this.$t('audit.webhook'),
        anonymous: this.$t('audit.anonymous'),
      };
      return labels[type] || type;
    },

    objectTitle(row) {
      const details = this.objectDetails(row);
      return details.name || details.filename || details.label || details.key
        || this.objectTypeLabel(row.objectType) || '-';
    },

    actorTitle(row) {
      return this.actorUsername(row) || this.actorName(row) || this.actorTokenName(row)
        || (this.actorAttemptedUsername(row)
          ? `${this.$t('audit.loginAccount')}: ${this.actorAttemptedUsername(row)}`
          : '')
        || this.actorTypeLabel(row.actorType) || '-';
    },

    actorUsername(row) {
      const details = this.actorDetails(row);
      return details.username || details.userName || row.actorUsername || '';
    },

    actorName(row) {
      const details = this.actorDetails(row);
      return details.name || row.actorName || '';
    },

    actorTokenName(row) {
      const details = this.actorDetails(row);
      return details.tokenName || details.token_name || '';
    },

    actorAttemptedUsername(row) {
      const details = this.actorDetails(row);
      return details.attemptedUsername || details.attempted_username || '';
    },

    objectSummary(row) {
      const details = this.objectDetails(row);
      const primaryKeys = ['name', 'filename', 'label', 'key', 'username'];
      let entries = Object.keys(details)
        .filter((key) => !primaryKeys.includes(key))
        .slice(0, 3)
        .map((key) => `${this.detailLabel(key)}: ${this.formatDetailValue(details[key])}`);
      if (!entries.length) {
        entries = this.businessMetadataEntries(row)
          .slice(0, 3)
          .map(({ key, value }) => `${this.detailLabel(key)}: ${this.formatDetailValue(value)}`);
      }
      return entries.join(' · ');
    },

    objectDetailEntries(row) {
      return this.detailEntries(this.objectDetails(row));
    },

    businessMetadataEntries(row) {
      const metadata = row.metadata || {};
      const technicalKeys = [
        'httpMethod', 'http_method', 'httpStatus', 'http_status', 'route',
        'objectDetails', 'object_details', 'actorDetails', 'actor_details',
      ];
      return this.detailEntries(metadata).filter(({ key }) => !technicalKeys.includes(key));
    },

    technicalMetadata(metadata) {
      const source = metadata || {};
      const technical = {};
      ['httpMethod', 'http_method', 'httpStatus', 'http_status', 'route'].forEach((key) => {
        if (source[key] !== undefined) {
          technical[key] = source[key];
        }
      });
      return Object.keys(technical).length ? technical : null;
    },

    detailEntries(value) {
      if (!value || typeof value !== 'object' || Array.isArray(value)) {
        return [];
      }
      return Object.keys(value).map((key) => ({ key, value: value[key] }));
    },

    detailLabel(key) {
      const labels = {
        name: this.$t('audit.detailName'),
        customerCode: this.$t('audit.detailCustomerCode'),
        customer_code: this.$t('audit.detailCustomerCode'),
        attemptedUsername: this.$t('audit.detailAttemptedUsername'),
        attempted_username: this.$t('audit.detailAttemptedUsername'),
        filename: this.$t('audit.detailFilename'),
        type: this.$t('audit.detailType'),
        subject: this.$t('audit.detailSubject'),
        status: this.$t('audit.detailStatus'),
        contentType: this.$t('audit.detailContentType'),
        content_type: this.$t('audit.detailContentType'),
        extension: this.$t('audit.detailExtension'),
        hasThumbnail: this.$t('audit.detailHasThumbnail'),
        has_thumbnail: this.$t('audit.detailHasThumbnail'),
        username: this.$t('audit.detailUsername'),
        tokenName: this.$t('audit.detailTokenName'),
        token_name: this.$t('audit.detailTokenName'),
        parentFolderId: this.$t('audit.detailParentFolder'),
        parent_folder_id: this.$t('audit.detailParentFolder'),
        folderId: this.$t('audit.detailFolder'),
        folder_id: this.$t('audit.detailFolder'),
        sourceFolderId: this.$t('audit.detailSourceFolderId'),
        source_folder_id: this.$t('audit.detailSourceFolderId'),
        sourceFolderName: this.$t('audit.detailSourceFolderName'),
        source_folder_name: this.$t('audit.detailSourceFolderName'),
        sourceFolderRoot: this.$t('audit.detailSourceFolderRoot'),
        source_folder_root: this.$t('audit.detailSourceFolderRoot'),
        targetFolderId: this.$t('audit.detailTargetFolderId'),
        target_folder_id: this.$t('audit.detailTargetFolderId'),
        targetFolderName: this.$t('audit.detailTargetFolderName'),
        target_folder_name: this.$t('audit.detailTargetFolderName'),
        targetFolderRoot: this.$t('audit.detailTargetFolderRoot'),
        target_folder_root: this.$t('audit.detailTargetFolderRoot'),
      };
      return labels[key] || key;
    },

    formatDetailValue(value) {
      if (typeof value === 'boolean') {
        return value ? this.$t('audit.yes') : this.$t('audit.no');
      }
      if (value && typeof value === 'object') {
        return JSON.stringify(value);
      }
      return String(value);
    },

    prettyMetadata(metadata) {
      return JSON.stringify(metadata || {}, null, 2);
    },
  },

  mounted() {
    this.getEvents();
  },
});
</script>

<style scoped>
.audit-detail {
  padding: 0.5rem 1rem 0.75rem;
}

.audit-cell-main {
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.audit-cell-secondary {
  color: #7a7a7a;
  font-size: 0.75rem;
  margin-top: 0.15rem;
}

.audit-content-summary {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.audit-detail-grid {
  display: grid;
  gap: 0.75rem 2rem;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  margin-bottom: 1rem;
}

.audit-detail-grid > div {
  display: flex;
  flex-wrap: wrap;
  gap: 0.35rem 0.65rem;
  align-items: baseline;
}

.audit-detail-label,
.audit-detail-heading {
  color: #7a7a7a;
  font-size: 0.75rem;
  text-transform: uppercase;
}

.audit-detail-label {
  flex-basis: 100%;
}

.audit-detail-section {
  margin-bottom: 1rem;
}

.audit-detail-heading {
  font-weight: 600;
  margin-bottom: 0.4rem;
}

.audit-detail-list {
  display: grid;
  grid-template-columns: minmax(130px, 0.35fr) minmax(0, 1fr);
  margin: 0;
}

.audit-detail-list dt,
.audit-detail-list dd {
  border-bottom: 1px solid #f0f0f0;
  margin: 0;
  padding: 0.35rem 0;
  word-break: break-word;
}

.audit-detail-list dt {
  color: #7a7a7a;
}

.audit-detail-list dd {
  padding-left: 1rem;
}

.audit-technical-details {
  margin-bottom: 0.75rem;
}

.audit-technical-details summary {
  color: #7a7a7a;
  cursor: pointer;
  font-size: 0.75rem;
  margin-bottom: 0.35rem;
}

.audit-detail pre {
  white-space: pre-wrap;
  word-break: break-word;
  margin: 0.35rem 0 0.75rem;
}
</style>
