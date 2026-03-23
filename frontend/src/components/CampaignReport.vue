<template>
  <section class="campaign-report">
    <div class="columns is-vcentered">
      <div class="column is-8">
        <div class="notification is-info" v-if="trackingDisabled">
          {{ $t('analytics.trackingDisabled') }}
        </div>
        <div class="notification is-info" v-else-if="!serverConfig.privacy.individual_tracking">
          {{ $t('analytics.nonIndividualTracking') }}
        </div>
      </div>
      <div class="column is-4">
        <form @submit.prevent="onDateSubmit">
          <div class="columns">
            <div class="column is-6">
              <b-field :label="$t('analytics.fromDate')" label-position="on-border">
                <b-datetimepicker
                  v-model="filters.from"
                  icon="calendar-clock"
                  :timepicker="{ hourFormat: '24' }"
                  :datetime-formatter="formatDateTime"
                />
              </b-field>
            </div>
            <div class="column is-6">
              <b-field :label="$t('analytics.toDate')" label-position="on-border">
                <b-datetimepicker
                  v-model="filters.to"
                  icon="calendar-clock"
                  :timepicker="{ hourFormat: '24' }"
                  :datetime-formatter="formatDateTime"
                />
              </b-field>
            </div>
          </div>
          <b-button native-type="submit" type="is-primary" icon-left="magnify" size="is-small" expanded>
            {{ $t('analytics.refreshReport') }}
          </b-button>
        </form>
      </div>
    </div>

    <div class="columns is-multiline report-cards">
      <div class="column is-4-tablet is-3-desktop" v-for="card in summaryCards" :key="card.key">
        <div class="box has-text-centered">
          <p class="heading">{{ card.label }}</p>
          <p class="title is-5">{{ card.value }}</p>
        </div>
      </div>
    </div>

    <section class="report-section">
      <div class="columns is-vcentered">
        <div class="column">
          <h4 class="title is-5">{{ $t('analytics.trends') }}</h4>
        </div>
      </div>

      <div v-if="trackingDisabled" class="notification is-light">
        {{ $t('analytics.trackingChartsUnavailable') }}
      </div>
      <div v-else-if="chartData">
        <chart :key="chartVersion" type="line" :data="chartData" />
      </div>
      <div v-else class="has-text-grey has-text-centered p-5">
        <b-loading :active="loading.series" :is-full-page="false" />
      </div>
    </section>

    <section class="report-section">
      <div class="columns is-vcentered">
        <div class="column">
          <h4 class="title is-5">{{ $t('analytics.links') }}</h4>
        </div>
        <div class="column is-narrow" v-if="activeLinkLabel">
          <b-tag type="is-info" closable @close="clearLinkFilter">
            {{ activeLinkLabel }}
          </b-tag>
        </div>
      </div>

      <div v-if="trackingDisabled" class="notification is-light">
        {{ $t('analytics.trackingChartsUnavailable') }}
      </div>
      <b-table v-else :data="links" :loading="loading.links" hoverable narrowed>
        <b-table-column v-slot="props" field="url" :label="$t('globals.terms.url')">
          <a href="#" @click.prevent="applyLinkFilter(props.row)">
            {{ props.row.url }}
          </a>
        </b-table-column>
        <b-table-column v-slot="props" field="totalClicks" :label="$t('analytics.totalClicks')" numeric>
          {{ props.row.totalClicks }}
        </b-table-column>
        <b-table-column v-slot="props" field="uniqueClickers" :label="$t('analytics.uniqueClicks')" numeric>
          {{ formatMetric(props.row.uniqueClickers) }}
        </b-table-column>
        <b-table-column v-slot="props" field="uniqueClickRate" :label="$t('analytics.uniqueClickRate')" numeric>
          {{ formatRate(props.row.uniqueClickRate) }}
        </b-table-column>
      </b-table>
    </section>

    <section class="report-section">
      <div class="columns is-vcentered">
        <div class="column">
          <h4 class="title is-5">{{ $t('analytics.recipients') }}</h4>
        </div>
      </div>

      <div v-if="!serverConfig.privacy.individual_tracking" class="notification is-light">
        {{ $t('analytics.nonIndividualTracking') }}
      </div>
      <div v-else-if="trackingDisabled" class="notification is-light">
        {{ $t('analytics.recipientDetailsUnavailable') }}
      </div>
      <div v-else-if="!canReadSubscribers" class="notification is-light">
        {{ $t('analytics.recipientPermission') }}
      </div>
      <div v-else>
        <div class="columns is-multiline recipient-filters">
          <div class="column is-4">
            <b-field :label="$t('globals.buttons.search')" label-position="on-border">
              <b-input v-model="filters.search" :placeholder="$t('analytics.recipientSearch')" icon="magnify" />
            </b-field>
          </div>
          <div class="column is-2">
            <b-field :label="$t('campaigns.views')" label-position="on-border">
              <b-select v-model="filters.opened" expanded>
                <option value="all">{{ $t('analytics.filterAll') }}</option>
                <option value="yes">{{ $t('analytics.filterYes') }}</option>
                <option value="no">{{ $t('analytics.filterNo') }}</option>
              </b-select>
            </b-field>
          </div>
          <div class="column is-2">
            <b-field :label="$t('campaigns.clicks')" label-position="on-border">
              <b-select v-model="filters.clicked" expanded>
                <option value="all">{{ $t('analytics.filterAll') }}</option>
                <option value="yes">{{ $t('analytics.filterYes') }}</option>
                <option value="no">{{ $t('analytics.filterNo') }}</option>
              </b-select>
            </b-field>
          </div>
          <div class="column is-2">
            <b-field :label="$t('globals.terms.bounces')" label-position="on-border">
              <b-select v-model="filters.bounced" expanded>
                <option value="all">{{ $t('analytics.filterAll') }}</option>
                <option value="yes">{{ $t('analytics.filterYes') }}</option>
                <option value="no">{{ $t('analytics.filterNo') }}</option>
              </b-select>
            </b-field>
          </div>
          <div class="column is-2">
            <b-field :label="$t('globals.buttons.search')" label-position="on-border">
              <b-button type="is-primary" icon-left="magnify" expanded @click="applyRecipientFilters">
                {{ $t('analytics.applyFilters') }}
              </b-button>
            </b-field>
          </div>
        </div>

        <b-table
          :data="recipients.results || []"
          :loading="loading.recipients"
          hoverable
          paginated
          backend-pagination
          backend-sorting
          pagination-position="both"
          @page-change="onRecipientPageChange"
          @sort="onRecipientSort"
          :current-page="recipientQuery.page"
          :per-page="recipients.perPage || recipientQuery.perPage"
          :total="recipients.total || 0"
        >
          <b-table-column v-slot="props" field="email" :label="$t('subscribers.email')" sortable>
            <router-link :to="{ name: 'subscriber', params: { id: props.row.subscriberId } }">
              {{ props.row.email }}
            </router-link>
            <p class="is-size-7 has-text-grey">{{ props.row.name }}</p>
          </b-table-column>

          <b-table-column v-slot="props" field="view_count" :label="$t('campaigns.views')" sortable numeric>
            <b-tag :type="props.row.viewCount > 0 ? 'is-success' : 'is-light'">
              {{ props.row.viewCount }}
            </b-tag>
          </b-table-column>

          <b-table-column v-slot="props" field="click_count" :label="$t('campaigns.clicks')" sortable numeric>
            <b-tag :type="props.row.clickCount > 0 ? 'is-info' : 'is-light'">
              {{ props.row.clickCount }}
            </b-tag>
          </b-table-column>

          <b-table-column v-slot="props" field="bounce_count" :label="$t('globals.terms.bounces')" numeric>
            <b-tag :type="props.row.bounceCount > 0 ? 'is-danger' : 'is-light'">
              {{ props.row.bounceCount }}
            </b-tag>
          </b-table-column>

          <b-table-column v-slot="props" field="last_engaged_at" :label="$t('analytics.lastEngagedAt')" sortable>
            {{ props.row.lastEngagedAt ? $utils.niceDate(props.row.lastEngagedAt, true) : '—' }}
          </b-table-column>

          <b-table-column v-slot="props" field="lastLinkUrl" :label="$t('analytics.lastClickedLink')">
            <a v-if="props.row.lastLinkUrl" :href="props.row.lastLinkUrl" target="_blank" rel="noopener noreferrer">
              {{ props.row.lastLinkUrl }}
            </a>
            <span v-else>—</span>
          </b-table-column>
        </b-table>
      </div>
    </section>
  </section>
</template>

<script>
import dayjs from 'dayjs';
import Vue from 'vue';
import { mapState } from 'vuex';
import Chart from './Chart.vue';

const chartColors = {
  views: '#0055d4',
  clicks: '#0f9d58',
  bounces: '#d9534f',
};

export default Vue.extend({
  name: 'CampaignReport',

  components: {
    Chart,
  },

  props: {
    campaign: {
      type: Object,
      required: true,
    },
    active: {
      type: Boolean,
      default: false,
    },
  },

  data() {
    return {
      initialized: false,
      chartVersion: 0,
      loading: {
        summary: false,
        series: false,
        links: false,
        recipients: false,
      },
      summary: {
        sent: 0,
        bounced: 0,
        viewsTotal: 0,
        clicksTotal: 0,
        uniqueViewers: null,
        uniqueClickers: null,
        openRate: null,
        clickRate: null,
        ctor: null,
      },
      series: {
        views: [],
        clicks: [],
        bounces: [],
      },
      chartData: null,
      links: [],
      recipients: {
        results: [],
        total: 0,
        perPage: 20,
      },
      filters: {
        from: null,
        to: null,
        search: '',
        opened: 'all',
        clicked: 'all',
        bounced: 'all',
        linkID: 0,
      },
      recipientQuery: {
        page: 1,
        perPage: 20,
        sortBy: 'last_engaged_at',
        order: 'desc',
      },
    };
  },

  computed: {
    ...mapState(['serverConfig']),

    trackingDisabled() {
      return this.serverConfig.privacy.disable_tracking;
    },

    canReadSubscribers() {
      return this.$can('subscribers:get_all', 'subscribers:get');
    },

    canShowRecipients() {
      return !this.trackingDisabled && this.serverConfig.privacy.individual_tracking && this.canReadSubscribers;
    },

    summaryCards() {
      return [
        { key: 'sent', label: this.$t('campaigns.sent'), value: this.summary.sent ?? 0 },
        { key: 'bounced', label: this.$t('globals.terms.bounces'), value: this.summary.bounced ?? 0 },
        { key: 'uniqueViewers', label: this.$t('analytics.uniqueOpens'), value: this.formatMetric(this.summary.uniqueViewers) },
        { key: 'uniqueClickers', label: this.$t('analytics.uniqueClicks'), value: this.formatMetric(this.summary.uniqueClickers) },
        { key: 'openRate', label: this.$t('analytics.openRate'), value: this.formatRate(this.summary.openRate) },
        { key: 'clickRate', label: this.$t('analytics.clickRate'), value: this.formatRate(this.summary.clickRate) },
        { key: 'ctor', label: this.$t('analytics.ctor'), value: this.formatRate(this.summary.ctor) },
      ];
    },

    activeLinkLabel() {
      if (!this.filters.linkID) {
        return '';
      }

      const row = this.links.find((item) => item.linkId === this.filters.linkID);
      return row ? row.url : '';
    },
  },

  watch: {
    active(val) {
      if (val) {
        this.ensureLoaded();
      }
    },

    'campaign.id': function onCampaignChange() {
      this.initialized = false;
      this.chartData = null;
      this.resetFilters();
      if (this.active) {
        this.ensureLoaded();
      }
    },
  },

  mounted() {
    if (this.active) {
      this.ensureLoaded();
    }
  },

  methods: {
    ensureLoaded() {
      if (!this.campaign || !this.campaign.id) {
        return;
      }

      if (!this.initialized) {
        this.resetFilters();
        this.initialized = true;
      }

      this.refreshReport();
    },

    resetFilters() {
      const from = this.campaign.startedAt || this.campaign.createdAt;
      const to = this.campaign.status === 'finished' && this.campaign.updatedAt
        ? this.campaign.updatedAt
        : new Date();

      this.filters.from = dayjs(from).toDate();
      this.filters.to = dayjs(to).toDate();
      this.filters.search = '';
      this.filters.opened = 'all';
      this.filters.clicked = 'all';
      this.filters.bounced = 'all';
      this.filters.linkID = 0;
      this.recipientQuery.page = 1;
      this.recipientQuery.sortBy = 'last_engaged_at';
      this.recipientQuery.order = 'desc';
    },

    reportParams() {
      return {
        from: this.filters.from,
        to: this.filters.to,
      };
    },

    recipientParams() {
      return {
        ...this.reportParams(),
        search: this.filters.search,
        opened: this.filters.opened,
        clicked: this.filters.clicked,
        bounced: this.filters.bounced,
        link_id: this.filters.linkID,
        page: this.recipientQuery.page,
        per_page: this.recipientQuery.perPage,
        sort_by: this.recipientQuery.sortBy,
        order: this.recipientQuery.order,
      };
    },

    formatDateTime(value) {
      return dayjs(value).format('YYYY-MM-DD HH:mm');
    },

    formatMetric(value) {
      if (value === null || typeof value === 'undefined' || this.trackingDisabled) {
        return '—';
      }
      return this.$utils.niceNumber(value);
    },

    formatRate(value) {
      if (value === null || typeof value === 'undefined' || this.trackingDisabled) {
        return '—';
      }
      return `${value.toFixed(2)}%`;
    },

    buildChartData(series) {
      const mkDataset = (label, data, color) => ({
        label,
        data: data.map((item) => ({
          x: this.formatDateTime(item.timestamp),
          y: item.count,
        })),
        borderColor: color,
        backgroundColor: color,
        borderWidth: 2,
        pointBorderWidth: 0.5,
      });

      return {
        datasets: [
          mkDataset(this.$t('campaigns.views'), series.views, chartColors.views),
          mkDataset(this.$t('campaigns.clicks'), series.clicks, chartColors.clicks),
          mkDataset(this.$t('globals.terms.bounces'), series.bounces, chartColors.bounces),
        ],
      };
    },

    refreshReport() {
      this.loadSummary();
      if (this.trackingDisabled) {
        this.chartData = null;
        this.links = [];
        this.recipients = { results: [], total: 0, perPage: this.recipientQuery.perPage };
        return;
      }

      this.loadSeries();
      this.loadLinks();
      if (this.canShowRecipients) {
        this.loadRecipients();
      }
    },

    loadSummary() {
      this.loading.summary = true;
      this.$api.getCampaignReportSummary(this.campaign.id, this.reportParams()).then((data) => {
        this.summary = data;
      }).finally(() => {
        this.loading.summary = false;
      });
    },

    loadSeries() {
      this.loading.series = true;
      this.$api.getCampaignReportSeries(this.campaign.id, this.reportParams()).then((data) => {
        this.series = data;
        this.chartData = this.buildChartData(data);
        this.chartVersion += 1;
      }).finally(() => {
        this.loading.series = false;
      });
    },

    loadLinks() {
      this.loading.links = true;
      this.$api.getCampaignReportLinks(this.campaign.id, this.reportParams()).then((data) => {
        this.links = data;
      }).finally(() => {
        this.loading.links = false;
      });
    },

    loadRecipients() {
      this.loading.recipients = true;
      this.$api.getCampaignReportRecipients(this.campaign.id, this.recipientParams()).then((data) => {
        this.recipients = data;
      }).finally(() => {
        this.loading.recipients = false;
      });
    },

    onDateSubmit() {
      this.recipientQuery.page = 1;
      this.refreshReport();
    },

    applyLinkFilter(row) {
      this.filters.linkID = row.linkId;
      this.recipientQuery.page = 1;
      if (this.canShowRecipients) {
        this.loadRecipients();
      }
    },

    clearLinkFilter() {
      this.filters.linkID = 0;
      this.recipientQuery.page = 1;
      if (this.canShowRecipients) {
        this.loadRecipients();
      }
    },

    applyRecipientFilters() {
      this.recipientQuery.page = 1;
      this.loadRecipients();
    },

    onRecipientPageChange(page) {
      this.recipientQuery.page = page;
      this.loadRecipients();
    },

    onRecipientSort(field, order) {
      const fields = {
        email: 'email',
        view_count: 'view_count',
        click_count: 'click_count',
        last_engaged_at: 'last_engaged_at',
      };

      this.recipientQuery.sortBy = fields[field] || 'last_engaged_at';
      this.recipientQuery.order = order;
      this.loadRecipients();
    },
  },
});
</script>

<style scoped>
.report-section {
  margin-top: 2rem;
}

.report-cards .box {
  min-height: 110px;
}
</style>
