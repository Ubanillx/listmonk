<template>
  <section class="campaign-analytics-report">
    <header class="columns page-header is-vcentered">
      <div class="column">
        <h1 class="title is-4">
          {{ $t('analytics.title') }}
          <b-tag class="ml-3" type="is-light">
            {{ activeScopeLabel }}
          </b-tag>
        </h1>
      </div>
    </header>

    <div class="notification is-info" v-if="trackingDisabled">
      {{ $t('analytics.trackingDisabled') }}
    </div>
    <div class="notification is-info" v-else-if="!serverConfig.privacy.individual_tracking">
      {{ $t('analytics.nonIndividualTracking') }}
    </div>

    <section class="box campaign-selector-panel">
      <div class="columns is-variable is-6 is-multiline is-vcentered">
        <div class="column is-6-desktop">
          <p class="is-size-7 has-text-weight-semibold has-text-grey campaign-selector-label">
            {{ translateOr('analytics.selectedCampaigns', '已选营销活动', 'Selected campaigns') }}
          </p>
          <div class="campaign-selector-tags">
            <b-tag
              v-if="form.campaigns.length === 0"
              type="is-light"
              size="is-medium"
              class="campaign-scope-tag"
            >
              {{ translateOr('analytics.allCampaigns', '全部营销活动', 'All campaigns') }}
            </b-tag>
            <b-tag
              v-for="campaign in form.campaigns"
              :key="campaign.id"
              type="is-info"
              size="is-medium"
              closable
              class="campaign-scope-tag"
              @close="removeCampaign(campaign.id)"
            >
              {{ campaign.name }}
            </b-tag>
          </div>
        </div>

        <div class="column is-6-desktop">
          <b-field
            :label="translateOr('analytics.campaignSearch', '搜索并添加营销活动', 'Search and add campaigns')"
            label-position="on-border"
            :message="translateOr('analytics.allCampaignsHint', '未选择营销活动时，默认显示全部营销活动数据。', 'Shows all campaigns by default when no campaign is selected.')"
          >
            <b-autocomplete
              v-model="campaignQuery"
              icon="magnify"
              clearable
              field="name"
              keep-first
              open-on-focus
              :data="availableCampaignOptions"
              :loading="isSearchLoading"
              :placeholder="translateOr('analytics.campaignSearchPlaceholder', '输入名称搜索营销活动', 'Type to search campaigns')"
              @focus="onCampaignSearchFocus"
              @typing="queryCampaigns"
              @select="selectCampaign"
            />
          </b-field>

          <div class="campaign-selector-actions">
            <b-button
              type="is-light"
              :disabled="form.campaigns.length === 0"
              @click="clearCampaignSelection"
            >
              {{ translateOr('analytics.clearCampaignSelection', '查看全部营销活动', 'View all campaigns') }}
            </b-button>
          </div>
        </div>
      </div>
    </section>

    <form @submit.prevent="onSubmit">
      <div class="columns is-vcentered">
        <div class="column is-3">
          <b-field :label="$t('analytics.fromDate')" label-position="on-border">
            <b-datetimepicker
              v-model="filters.from"
              icon="calendar-clock"
              :timepicker="{ hourFormat: '24' }"
              :datetime-formatter="formatDateTime"
              @input="onFromDateChange"
            />
          </b-field>
        </div>
        <div class="column is-3">
          <b-field :label="$t('analytics.toDate')" label-position="on-border">
            <b-datetimepicker
              v-model="filters.to"
              icon="calendar-clock"
              :timepicker="{ hourFormat: '24' }"
              :datetime-formatter="formatDateTime"
              @input="onToDateChange"
            />
          </b-field>
        </div>
        <div class="column is-2">
          <b-button native-type="submit" type="is-primary" icon-left="magnify" expanded>
            {{ $t('analytics.refreshReport') }}
          </b-button>
        </div>
        <div class="column is-4 has-text-right">
          <router-link v-if="singleCampaign" :to="{ name: 'campaign', params: { id: singleCampaign.id }, hash: '#analytics' }"
            class="button is-primary is-light">
            {{ $t('analytics.openCampaignReport') }}
          </router-link>
        </div>
      </div>
    </form>

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
          <h4 class="title is-5">{{ translateOr('analytics.breakdown', '占比概览', 'Breakdown') }}</h4>
        </div>
      </div>

      <div v-if="loading.summary" class="has-text-grey has-text-centered p-5">
        <b-loading :active="loading.summary" :is-full-page="false" />
      </div>
      <div v-else-if="breakdownChartData" class="columns is-variable is-6 breakdown-section">
        <div class="column is-5-tablet is-4-desktop">
          <div class="breakdown-chart">
            <chart type="pie" :data="breakdownChartData" />
          </div>
        </div>
        <div class="column is-7-tablet is-8-desktop">
          <div class="breakdown-legend">
            <div v-for="item in breakdownItems" :key="item.key" class="breakdown-item">
              <div class="breakdown-item-main">
                <span class="breakdown-swatch" :style="{ backgroundColor: item.color }" />
                <span class="breakdown-label">{{ item.label }}</span>
              </div>
              <div class="breakdown-item-meta">
                <strong>{{ $utils.niceNumber(item.value) }}</strong>
                <span>{{ formatPercent(item.percentage) }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>
      <div v-else class="columns is-variable is-6 breakdown-section breakdown-empty">
        <div class="column is-5-tablet is-4-desktop">
          <div class="breakdown-chart breakdown-chart-empty">
            <chart type="pie" :data="breakdownEmptyChartData" />
          </div>
        </div>
        <div class="column is-7-tablet is-8-desktop">
          <div class="breakdown-empty-copy">
            <h5 class="title is-6">{{ translateOr('analytics.breakdown', '占比概览', 'Breakdown') }}</h5>
            <p>{{ translateOr('analytics.breakdownUnavailable', '暂无', 'Breakdown is unavailable until unique open and click data is available.') }}</p>
          </div>
        </div>
      </div>
    </section>

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
        <b-table-column v-slot="props" field="campaignSubject" :label="$tc('globals.terms.campaign', 1)">
          <router-link :to="{ name: 'campaign', params: { id: props.row.campaignId }, hash: '#analytics' }">
            {{ props.row.campaignSubject }}
          </router-link>
          <p class="is-size-7 has-text-grey">{{ props.row.campaignName }}</p>
        </b-table-column>
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
          <div class="column is-3">
            <b-field :label="$t('globals.buttons.search')" label-position="on-border">
              <b-input v-model="filters.search" :placeholder="$t('analytics.recipientSearch')" icon="magnify" />
            </b-field>
          </div>
          <div class="column is-3">
            <b-field :label="$tc('globals.terms.campaign', 1)" label-position="on-border">
              <b-input :value="activeScopeLabel" disabled />
            </b-field>
          </div>
          <div class="column is-1">
            <b-field :label="$t('campaigns.views')" label-position="on-border">
              <b-select v-model="filters.opened" expanded>
                <option value="all">{{ $t('analytics.filterAll') }}</option>
                <option value="yes">{{ $t('analytics.filterYes') }}</option>
                <option value="no">{{ $t('analytics.filterNo') }}</option>
              </b-select>
            </b-field>
          </div>
          <div class="column is-1">
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
          <div class="column is-2 recipient-filter-action-column">
            <b-field class="recipient-filter-action">
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
          <b-table-column v-slot="props" field="campaignSubject" :label="$tc('globals.terms.campaign', 1)" sortable>
            <router-link :to="{ name: 'campaign', params: { id: props.row.campaignId }, hash: '#analytics' }">
              {{ props.row.campaignSubject }}
            </router-link>
            <p class="is-size-7 has-text-grey">{{ props.row.campaignName }}</p>
          </b-table-column>

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

          <b-table-column v-slot="props" field="bounce_count" :label="$t('globals.terms.bounces')" sortable numeric>
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
import Chart from '../components/Chart.vue';

const chartColors = {
  views: '#0055d4',
  clicks: '#0f9d58',
  bounces: '#d9534f',
};

const breakdownColors = {
  clicked: '#0f9d58',
  openedOnly: '#4f8ef7',
  unopened: '#f5a623',
  bounced: '#d9534f',
};

export default Vue.extend({
  name: 'CampaignAnalyticsReport',

  components: {
    Chart,
  },

  data() {
    return {
      syncToken: 0,
      isSearchLoading: false,
      campaignQuery: '',
      queriedCampaigns: [],
      chartVersion: 0,
      loading: {
        summary: false,
        series: false,
        links: false,
        recipients: false,
      },
      form: {
        campaigns: [],
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

    singleCampaign() {
      return this.form.campaigns.length === 1 ? this.form.campaigns[0] : null;
    },

    activeScopeLabel() {
      if (this.form.campaigns.length === 0) {
        return this.translateOr('analytics.allCampaigns', '全部营销活动', 'All campaigns');
      }

      if (this.form.campaigns.length === 1) {
        return this.form.campaigns[0].name;
      }

      return `${this.form.campaigns.length} ${this.$tc('globals.terms.campaign', this.form.campaigns.length)}`;
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

    breakdownItems() {
      if (this.trackingDisabled) {
        return [];
      }

      const sent = Math.max(this.summary.sent ?? 0, 0);
      const bounced = Math.min(Math.max(this.summary.bounced ?? 0, 0), sent);
      const { uniqueViewers, uniqueClickers } = this.summary;
      if (uniqueViewers === null || typeof uniqueViewers === 'undefined'
        || uniqueClickers === null || typeof uniqueClickers === 'undefined'
        || sent === 0) {
        return [];
      }

      const deliveredBase = Math.max(sent - bounced, 0);
      const clicked = Math.min(Math.max(uniqueClickers, 0), deliveredBase);
      const opened = Math.min(Math.max(uniqueViewers, 0), deliveredBase);
      const openedOnly = Math.max(opened - clicked, 0);
      const unopened = Math.max(deliveredBase - opened, 0);
      const total = clicked + openedOnly + unopened + bounced;

      return [
        {
          key: 'clicked',
          label: this.translateOr('analytics.breakdownClicked', '已点击', 'Clicked'),
          value: clicked,
          color: breakdownColors.clicked,
          percentage: total > 0 ? (clicked / total) * 100 : 0,
        },
        {
          key: 'openedOnly',
          label: this.translateOr('analytics.breakdownOpenedOnly', '已打开未点击', 'Opened, no click'),
          value: openedOnly,
          color: breakdownColors.openedOnly,
          percentage: total > 0 ? (openedOnly / total) * 100 : 0,
        },
        {
          key: 'unopened',
          label: this.translateOr('analytics.breakdownUnopened', '已送达未打开', 'Delivered, no open'),
          value: unopened,
          color: breakdownColors.unopened,
          percentage: total > 0 ? (unopened / total) * 100 : 0,
        },
        {
          key: 'bounced',
          label: this.translateOr('analytics.breakdownBounced', '退信', 'Bounced'),
          value: bounced,
          color: breakdownColors.bounced,
          percentage: total > 0 ? (bounced / total) * 100 : 0,
        },
      ].filter((item) => item.value > 0);
    },

    breakdownChartData() {
      if (this.breakdownItems.length === 0) {
        return null;
      }

      return {
        labels: this.breakdownItems.map((item) => item.label),
        datasets: [{
          data: this.breakdownItems.map((item) => item.value),
          backgroundColor: this.breakdownItems.map((item) => item.color),
          borderWidth: 6,
        }],
      };
    },

    breakdownEmptyChartData() {
      return {
        labels: [this.translateOr('analytics.breakdownUnavailable', '暂无占比数据', 'No breakdown data')],
        datasets: [{
          data: [1],
          backgroundColor: ['#e5e7eb'],
          borderWidth: 0,
        }],
      };
    },

    activeLinkLabel() {
      if (!this.filters.linkID) {
        return '';
      }

      const row = this.links.find((item) => item.linkId === this.filters.linkID);
      if (!row) {
        return '';
      }

      return `${row.campaignSubject} / ${row.url}`;
    },

    availableCampaignOptions() {
      const selected = new Set(this.form.campaigns.map((campaign) => campaign.id));
      return this.queriedCampaigns.filter((campaign) => !selected.has(campaign.id));
    },
  },

  watch: {
    '$route.query': {
      deep: true,
      handler() {
        this.syncFromRoute();
      },
    },
  },

  mounted() {
    this.syncFromRoute();
  },

  methods: {
    translateOr(key, zhFallback, enFallback = zhFallback) {
      if (this.$te(key)) {
        return this.$t(key);
      }

      const locale = (this.$i18n?.locale || '').toLowerCase();
      return locale.startsWith('zh') ? zhFallback : enFallback;
    },

    defaultDateRange() {
      const now = dayjs().set('hour', 23).set('minute', 59).set('seconds', 0);
      const weekAgo = now.subtract(7, 'day').set('hour', 0).set('minute', 0);
      return { from: weekAgo.toDate(), to: now.toDate() };
    },

    async syncFromRoute() {
      const token = this.syncToken + 1;
      this.syncToken = token;

      const defaults = this.defaultDateRange();
      this.filters.from = this.$route.query.from ? dayjs.unix(this.$route.query.from).toDate() : defaults.from;
      this.filters.to = this.$route.query.to ? dayjs.unix(this.$route.query.to).toDate() : defaults.to;
      this.resetInteractiveFilters();

      const ids = this.$utils.parseQueryIDs(this.$route.query.id).filter((id) => !Number.isNaN(id));
      if (ids.length === 0) {
        this.form.campaigns = [];
        this.refreshReport();
        return;
      }

      this.isSearchLoading = true;
      const data = await Promise.allSettled(ids.map((id) => this.$api.getCampaign(id)));
      if (token !== this.syncToken) {
        return;
      }

      this.form.campaigns = data
        .filter((item) => item.status === 'fulfilled')
        .map((item) => this.normalizeCampaign(item.value));
      this.isSearchLoading = false;
      this.refreshReport();
    },

    resetInteractiveFilters() {
      this.filters.search = '';
      this.filters.opened = 'all';
      this.filters.clicked = 'all';
      this.filters.bounced = 'all';
      this.filters.linkID = 0;
      this.recipientQuery.page = 1;
      this.recipientQuery.sortBy = 'last_engaged_at';
      this.recipientQuery.order = 'desc';
    },

    onFromDateChange() {
      if (this.filters.from > this.filters.to) {
        this.filters.to = dayjs(this.filters.from).add(7, 'day').toDate();
      }
    },

    onToDateChange() {
      if (this.filters.from > this.filters.to) {
        this.filters.from = dayjs(this.filters.to).add(-7, 'day').toDate();
      }
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

    formatPercent(value) {
      return `${value.toFixed(1)}%`;
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

    normalizeCampaign(campaign) {
      return { ...campaign, name: `#${campaign.id}: ${campaign.name}` };
    },

    async queryCampaigns(q = '') {
      this.isSearchLoading = true;

      try {
        const data = await this.$api.getCampaigns({
          query: q,
          order_by: 'created_at',
          order: 'DESC',
          per_page: 20,
        });
        this.queriedCampaigns = data.results.map(this.normalizeCampaign);
      } finally {
        this.isSearchLoading = false;
      }
    },

    onCampaignSearchFocus() {
      if (this.queriedCampaigns.length === 0) {
        this.queryCampaigns('');
      }
    },

    selectCampaign(campaign) {
      if (!campaign || this.form.campaigns.find(({ id }) => id === campaign.id)) {
        return;
      }

      this.form.campaigns = [...this.form.campaigns, campaign];
      this.campaignQuery = '';
    },

    removeCampaign(id) {
      this.form.campaigns = this.form.campaigns.filter((campaign) => campaign.id !== id);
    },

    clearCampaignSelection() {
      this.form.campaigns = [];
      this.campaignQuery = '';
    },

    baseReportParams() {
      const params = {
        from: this.filters.from,
        to: this.filters.to,
      };

      if (this.form.campaigns.length === 0) {
        params.all = true;
      } else {
        params.id = this.form.campaigns.map((c) => c.id);
      }

      return params;
    },

    recipientParams() {
      return {
        ...this.baseReportParams(),
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

    onSubmit() {
      const query = {
        from: dayjs(this.filters.from).unix(),
        to: dayjs(this.filters.to).unix(),
      };

      if (this.form.campaigns.length > 0) {
        query.id = this.form.campaigns.map((c) => c.id);
      }

      this.$router.push({ query });
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
      this.$api.getCampaignsReportSummary(this.baseReportParams()).then((data) => {
        this.summary = data;
      }).finally(() => {
        this.loading.summary = false;
      });
    },

    loadSeries() {
      this.loading.series = true;
      this.$api.getCampaignsReportSeries(this.baseReportParams()).then((data) => {
        this.series = data;
        this.chartData = this.buildChartData(data);
        this.chartVersion += 1;
      }).finally(() => {
        this.loading.series = false;
      });
    },

    loadLinks() {
      this.loading.links = true;
      this.$api.getCampaignsReportLinks(this.baseReportParams()).then((data) => {
        this.links = data;
      }).finally(() => {
        this.loading.links = false;
      });
    },

    loadRecipients() {
      this.loading.recipients = true;
      this.$api.getCampaignsReportRecipients(this.recipientParams()).then((data) => {
        this.recipients = data;
      }).finally(() => {
        this.loading.recipients = false;
      });
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
        campaignSubject: 'campaign_subject',
        email: 'email',
        view_count: 'view_count',
        click_count: 'click_count',
        bounce_count: 'bounce_count',
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
.campaign-selector-panel {
  margin-bottom: 1.5rem;
}

.campaign-selector-label {
  margin-bottom: 0.6rem;
}

.campaign-selector-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 0.75rem;
  min-height: 2.75rem;
  align-items: flex-start;
}

.campaign-scope-tag {
  max-width: 100%;
}

.campaign-selector-actions {
  display: flex;
  justify-content: flex-end;
}

.report-section {
  margin-top: 2rem;
}

.report-cards .box {
  min-height: 110px;
}

.recipient-filter-action-column {
  display: flex;
  align-items: flex-end;
}

.recipient-filter-action {
  width: 100%;
  margin-bottom: 0.75rem;
}

.breakdown-section {
  align-items: center;
}

.breakdown-chart {
  position: relative;
  min-height: 280px;
}

.breakdown-chart :deep(canvas) {
  height: 280px;
  max-width: 100%;
}

.breakdown-chart-empty :deep(canvas) {
  opacity: 0.75;
}

.breakdown-legend {
  display: grid;
  gap: 0.9rem;
}

.breakdown-empty-copy {
  display: flex;
  flex-direction: column;
  justify-content: center;
  min-height: 280px;
  padding: 1.25rem 1.5rem;
  border: 1px dashed #d7dce4;
  border-radius: 12px;
  background: #fbfcfe;
  color: #6b7280;
}

.breakdown-empty-copy .title {
  margin-bottom: 0.75rem;
  color: #374151;
}

.breakdown-empty-copy p {
  margin: 0;
  line-height: 1.7;
}

.breakdown-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.85rem 1rem;
  border: 1px solid #e8ecf2;
  border-radius: 12px;
  background: #fbfcfe;
}

.breakdown-item-main {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}

.breakdown-swatch {
  width: 0.9rem;
  height: 0.9rem;
  border-radius: 999px;
  flex: 0 0 auto;
}

@media (max-width: 1023px) {
  .campaign-selector-actions {
    justify-content: flex-start;
  }
}

.breakdown-label {
  font-weight: 600;
}

.breakdown-item-meta {
  display: flex;
  align-items: baseline;
  gap: 0.75rem;
  color: #6b7280;
}

@media (max-width: 768px) {
  .recipient-filter-action-column {
    display: block;
  }

  .recipient-filter-action {
    margin-bottom: 0;
  }

  .breakdown-item {
    align-items: flex-start;
    flex-direction: column;
  }

  .breakdown-item-meta {
    width: 100%;
    justify-content: space-between;
  }

  .breakdown-empty-copy {
    min-height: auto;
  }
}
</style>
