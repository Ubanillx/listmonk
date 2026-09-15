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
        <code>{{ props.row.action }}</code>
      </b-table-column>
      <b-table-column v-slot="props" field="object" :label="$t('audit.object')">
        {{ props.row.objectType }}<span v-if="props.row.objectId"> #{{ props.row.objectId }}</span>
      </b-table-column>
      <b-table-column v-slot="props" field="actor" :label="$t('audit.actor')">
        {{ props.row.actorType }}<span v-if="props.row.actorUserId"> #{{ props.row.actorUserId }}</span>
      </b-table-column>
      <b-table-column v-slot="props" field="result" :label="$t('audit.result')">
        <b-tag :type="resultType(props.row.result)">{{ resultLabel(props.row.result) }}</b-tag>
      </b-table-column>
      <b-table-column v-slot="props" field="reasonCode" :label="$t('audit.reason')">
        {{ props.row.reasonCode || '-' }}
      </b-table-column>

      <template #detail="props">
        <div class="audit-detail">
          <div><strong>{{ $t('audit.metadata') }}</strong></div>
          <pre>{{ prettyMetadata(props.row.metadata) }}</pre>
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

.audit-detail pre {
  white-space: pre-wrap;
  word-break: break-word;
  margin: 0.35rem 0 0.75rem;
}
</style>
