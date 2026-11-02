<template>
  <section class="dashboard content">
    <header class="page-header dashboard-header">
      <div>
        <h1 class="title is-4">{{ $t('menu.dashboard') }}</h1>
        <p class="has-text-grey">{{ $utils.niceDate($utils.currentDate()) }}</p>
      </div>
      <span v-if="lastUpdated" class="dashboard-updated">
        <i class="dashboard-live-dot" />{{ $t('dashboard.updatedAt', { time: lastUpdated }) }}
      </span>
    </header>

    <section class="dashboard-metrics" :aria-label="$t('dashboard.overview')">
      <component :is="metric.to ? 'router-link' : 'div'" v-for="metric in metrics" :key="metric.key" :to="metric.to"
        class="dashboard-metric" :data-cy="metric.key">
        <div class="dashboard-metric-heading">
          <span class="dashboard-metric-icon"><b-icon :icon="metric.icon" size="is-small" /></span>
          <span class="dashboard-metric-label">{{ metric.label }}</span>
          <b-icon v-if="metric.to" icon="arrow-top-right" size="is-small" class="dashboard-metric-link" />
        </div>
        <strong class="metric-value">{{ $utils.niceNumber(metric.value) }}</strong>
        <p class="metric-caption">{{ metric.caption }}</p>
      </component>
    </section>

    <div class="dashboard-primary-grid">
      <article class="dashboard-panel dashboard-trend-panel">
        <header class="dashboard-panel-header">
          <div>
            <h2>{{ $t('dashboard.engagementTrend') }}</h2>
            <p>{{ $t('dashboard.engagementTrendHelp') }}</p>
          </div>
          <span class="dashboard-period"><b-icon icon="calendar-range" size="is-small" />{{ trendRangeLabel }}</span>
        </header>
        <div class="dashboard-trend-summary">
          <div><span>{{ $t('dashboard.campaignViews') }}</span><strong>{{ $utils.niceNumber(trendTotals.views) }}</strong></div>
          <div><span>{{ $t('dashboard.linkClicks') }}</span><strong>{{ $utils.niceNumber(trendTotals.clicks) }}</strong></div>
          <div><span>{{ $t('dashboard.interactionsPerView') }}</span><strong>{{ interactionRate }}<small>%</small></strong></div>
        </div>
        <div v-if="hasTrendData" class="dashboard-trend-visuals">
          <section class="dashboard-trend-visual">
            <h3>{{ $t('dashboard.viewTrendTitle') }}</h3>
            <div v-if="campaignViews.length" class="dashboard-trend-chart">
              <dashboard-chart :option="viewTrendOption" :label="$t('dashboard.viewTrendTitle')" />
            </div>
            <p v-else class="dashboard-trend-none">{{ $t('dashboard.noChartData') }}</p>
          </section>
          <section class="dashboard-trend-visual">
            <h3>{{ $t('dashboard.clickTrendTitle') }}</h3>
            <div v-if="campaignClicks.length" class="dashboard-trend-chart">
              <dashboard-chart :option="clickTrendOption" :label="$t('dashboard.clickTrendTitle')" />
            </div>
            <p v-else class="dashboard-trend-none">{{ $t('dashboard.noChartData') }}</p>
          </section>
        </div>
        <div v-else-if="loadErrors.charts" class="dashboard-empty dashboard-trend-chart" role="alert">
          <b-icon icon="alert-circle-outline" size="is-large" />
          <p>{{ $t('dashboard.loadError') }}</p>
          <b-button size="is-small" icon-left="refresh" @click="fetchData">{{ $t('dashboard.retry') }}</b-button>
        </div>
        <div v-else class="dashboard-empty dashboard-trend-chart">
          <b-icon icon="chart-timeline-variant" size="is-large" />
          <strong>{{ $t('dashboard.noChartData') }}</strong>
          <p>{{ $t('dashboard.noChartDataHelp') }}</p>
        </div>
        <p v-if="hasTrendData" class="dashboard-panel-note">{{ $t('dashboard.trendNote') }}</p>
      </article>

      <article class="dashboard-panel dashboard-status-panel" data-cy="campaigns">
        <header class="dashboard-panel-header">
          <div><h2>{{ $t('dashboard.campaignStatus') }}</h2><p>{{ $t('dashboard.campaignStatusHelp') }}</p></div>
          <router-link :to="{ name: 'campaigns' }" class="dashboard-panel-link">{{ $t('dashboard.viewAll') }}<b-icon icon="arrow-right" size="is-small" /></router-link>
        </header>
        <div v-if="campaignStatuses.length" class="dashboard-status-content">
          <div class="dashboard-donut">
            <dashboard-chart :option="campaignStatusOption" :label="$t('dashboard.campaignStatus')" />
            <div class="dashboard-donut-center"><strong>{{ $utils.niceNumber(counts.campaigns.total || 0) }}</strong><span>{{ $tc('globals.terms.campaign', counts.campaigns.total || 0) }}</span></div>
          </div>
          <ul class="dashboard-status-list no">
            <li v-for="status in campaignStatuses" :key="status.key">
              <span><i :style="{ backgroundColor: status.color }" />{{ status.label }}</span>
              <strong :data-cy="`campaigns-${status.key}`">{{ status.value }}</strong>
            </li>
          </ul>
        </div>
        <div v-else class="dashboard-empty"><b-icon icon="rocket-launch-outline" size="is-large" /><p>{{ $t('dashboard.noCampaigns') }}</p></div>
        <div class="dashboard-status-footer">
          <div><span>{{ $t('dashboard.activeCampaigns') }}</span><strong>{{ activeCampaignCount }}</strong></div>
          <div><span>{{ $t('dashboard.completionRate') }}</span><strong>{{ campaignCompletionRate }}%</strong></div>
        </div>
      </article>
    </div>

    <div class="dashboard-secondary-grid">
      <article class="dashboard-panel dashboard-performance-panel">
        <header class="dashboard-panel-header">
          <div><h2>{{ $t('dashboard.campaignPerformance') }}</h2><p>{{ $t('dashboard.campaignPerformanceHelp') }}</p></div>
          <b-field class="dashboard-sort">
            <b-select v-model="performanceMetric" :aria-label="$t('dashboard.sortMetric')" size="is-small">
              <option value="views">{{ $t('dashboard.campaignViews') }}</option>
              <option value="clicks">{{ $t('dashboard.linkClicks') }}</option>
              <option value="bounces">{{ $t('globals.terms.bounces') }}</option>
              <option value="unsubscribes">{{ $t('campaigns.unsubscribes') }}</option>
            </b-select>
          </b-field>
        </header>
        <p v-if="sourceCampaign" class="dashboard-source-note">
          <span>
            <b-icon icon="history" size="is-small" />
            <span class="dashboard-source-label">{{ $t('dashboard.sourceContext', { id: sourceCampaign.id, name: sourceCampaign.name }) }}</span>
          </span>
          <b-button type="is-text" size="is-small" icon-right="close" :aria-label="$t('dashboard.clearSource')" @click="clearSource">
            {{ $t('dashboard.clearSource') }}
          </b-button>
        </p>
        <div v-if="performanceCampaigns.length" class="dashboard-performance-content">
          <div class="dashboard-performance-chart"><dashboard-chart :option="performanceOption" :label="$t('dashboard.campaignPerformance')" /></div>
          <ol class="dashboard-ranking no">
            <li v-for="(campaign, index) in performanceCampaigns" :key="campaign.id" :class="{ 'is-source': sourceCampaignId === campaign.id }">
              <span class="dashboard-rank">{{ String(index + 1).padStart(2, '0') }}</span>
              <router-link :to="campaignLink(campaign.id)" :title="campaign.name" @click.native.prevent="openCampaign(campaign.id)">{{ campaign.name }}</router-link>
              <strong>{{ $utils.niceNumber(campaign[performanceMetric]) }}</strong>
            </li>
          </ol>
        </div>
        <div v-else class="dashboard-empty"><b-icon icon="chart-bar" size="is-large" /><p>{{ $t('dashboard.noCampaigns') }}</p></div>
        <p class="dashboard-panel-note">{{ $t('dashboard.performanceNote') }}</p>
      </article>

      <article class="dashboard-panel dashboard-audience-panel">
        <header class="dashboard-panel-header">
          <div><h2>{{ $t('dashboard.audienceComposition') }}</h2><p>{{ $t('dashboard.audienceCompositionHelp') }}</p></div>
        </header>
        <div class="dashboard-audience-content">
          <div class="dashboard-donut dashboard-audience-donut">
            <dashboard-chart :option="audienceOption" :label="$t('dashboard.audienceComposition')" />
            <div class="dashboard-donut-center"><strong>{{ $utils.niceNumber(audienceTotal) }}</strong><span>{{ $t('dashboard.audienceTotal') }}</span></div>
          </div>
          <ul class="dashboard-audience-list no">
            <li v-for="segment in audienceSegments" :key="segment.key">
              <span><i :style="{ backgroundColor: segment.color }" />{{ segment.label }}</span>
              <strong>{{ $utils.niceNumber(segment.value) }}<small>{{ audiencePercent(segment.value) }}%</small></strong>
            </li>
          </ul>
        </div>
        <div class="dashboard-audience-attention" data-cy="private-customers">
          <router-link :to="{ name: 'customers' }">
            <span><b-icon icon="account-off-outline" size="is-small" />{{ $t('customers.status.blocklisted') }}</span>
            <strong>{{ $utils.niceNumber(counts.privateCustomers.blocklisted || 0) }}</strong>
          </router-link>
          <router-link :to="{ name: 'customers' }">
            <span><b-icon icon="account-question-outline" size="is-small" />{{ $t('dashboard.orphanSubs') }}</span>
            <strong>{{ $utils.niceNumber(counts.privateCustomers.orphans || 0) }}</strong>
          </router-link>
        </div>
      </article>
    </div>

    <article class="dashboard-panel dashboard-delivery-panel">
      <header class="dashboard-panel-header">
        <div><h2>{{ $t('dashboard.deliveryHealth') }}</h2><p>{{ $t('dashboard.deliveryHealthHelp') }}</p></div>
        <span class="dashboard-period">{{ $t('dashboard.latestCampaigns') }}</span>
      </header>
      <div class="dashboard-delivery-layout">
        <div class="dashboard-delivery-chart">
          <dashboard-chart :option="deliveryOption" :label="$t('dashboard.deliveryHealth')" />
        </div>
        <div class="dashboard-unsubscribe-summary">
          <h3>{{ $t('dashboard.unsubscribeSummary') }}</h3>
          <p>{{ $t('dashboard.unsubscribeHelp') }}</p>
          <div class="dashboard-unsubscribe-metrics">
            <div><span>{{ $t('campaigns.unsubscribeClicks') }}</span><strong>{{ $utils.niceNumber(deliveryTotals.unsubscribeClicks) }}</strong></div>
            <div><span>{{ $t('campaigns.unsubscribes') }}</span><strong>{{ $utils.niceNumber(deliveryTotals.unsubscribes) }}</strong></div>
            <div><span>{{ $t('dashboard.unsubscribeConversion') }}</span><strong>{{ unsubscribeRate }}<small>%</small></strong></div>
          </div>
        </div>
      </div>
      <div v-if="campaigns.length" class="dashboard-delivery-table-wrap">
        <table class="dashboard-delivery-table">
          <thead>
<tr>
            <th>{{ $tc('globals.terms.campaign', 1) }}</th>
            <th>{{ $t('globals.terms.bounces') }}</th>
            <th>{{ $t('campaigns.deliveryCannotSend') }}</th>
            <th>{{ $t('campaigns.unsubscribeClicks') }}</th>
            <th>{{ $t('campaigns.unsubscribes') }}</th>
          </tr>
</thead>
          <tbody>
            <tr v-for="campaign in deliveryCampaigns" :key="campaign.id">
              <td data-label=""><router-link :to="{ name: 'campaign', params: { id: campaign.id } }" :title="campaign.name">{{ campaign.name }}</router-link></td>
              <td :data-label="$t('globals.terms.bounces')" :class="{ 'has-error': campaign.bounces > 0 }">{{ $utils.niceNumber(campaign.bounces) }}</td>
              <td :data-label="$t('campaigns.deliveryCannotSend')" :class="{ 'has-error': campaign.deliveryUnavailable > 0 }">{{ $utils.niceNumber(campaign.deliveryUnavailable) }}</td>
              <td :data-label="$t('campaigns.unsubscribeClicks')">{{ $utils.niceNumber(campaign.unsubscribeClicks) }}</td>
              <td :data-label="$t('campaigns.unsubscribes')">{{ $utils.niceNumber(campaign.unsubscribes) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-else class="dashboard-empty"><p>{{ $t('dashboard.noCampaigns') }}</p></div>
      <p class="dashboard-panel-note">{{ $t('dashboard.deliveryNote') }}</p>
    </article>

    <div class="dashboard-tertiary-grid">
      <article class="dashboard-panel dashboard-list-panel" data-cy="customerLists">
        <header class="dashboard-panel-header">
          <h2>{{ $t('dashboard.privateLists') }}</h2>
          <strong class="dashboard-panel-total">{{ $utils.niceNumber(counts.customerLists.total || 0) }}</strong>
        </header>
        <div class="dashboard-stacked-bar" aria-hidden="true"><i v-for="item in listSegments.slice(0, 2)" :key="item.key" :style="{ width: `${item.percent}%`, backgroundColor: item.color }" /></div>
        <ul class="dashboard-detail-list no">
          <li v-for="item in listSegments" :key="item.key"><span><i :style="{ backgroundColor: item.color }" />{{ item.label }}</span><strong>{{ item.value }}</strong></li>
        </ul>
      </article>
      <article class="dashboard-panel dashboard-pool-panel" data-cy="dashboard-pool-customers">
        <header class="dashboard-panel-header">
          <h2>{{ $t('dashboard.poolCustomers') }}</h2>
          <strong class="dashboard-panel-total">{{ $utils.niceNumber(counts.poolCustomers.total || 0) }}</strong>
        </header>
        <div class="dashboard-pool-content">
          <div class="dashboard-pool-bar"><dashboard-chart :option="poolOption" :label="$t('dashboard.poolCustomers')" /></div>
          <ul class="dashboard-detail-list no">
            <li><span><i class="is-success" />{{ $t('pool.activeContacts') }}</span><strong>{{ $utils.niceNumber(counts.poolCustomers.active || 0) }}</strong></li>
            <li><span><i class="is-muted" />{{ $t('pool.statusRemovedGlobal') }}</span><strong>{{ $utils.niceNumber(counts.poolCustomers.removed || 0) }}</strong></li>
          </ul>
        </div>
      </article>
      <article class="dashboard-panel dashboard-pool-panel" data-cy="pool-lists">
        <header class="dashboard-panel-header">
          <h2>{{ $t('menu.poolLists') }}</h2>
          <strong class="dashboard-panel-total">{{ $utils.niceNumber(counts.poolLists.total || 0) }}</strong>
        </header>
        <div class="dashboard-pool-binding metric-breakdown">
          <div class="dashboard-binding-number">
            <strong data-cy="pool-lists-allocations">{{ $utils.niceNumber(counts.poolLists.allocations || 0) }}</strong>
            <span>{{ $t('dashboard.poolAllocations') }}</span>
          </div>
          <ul class="dashboard-detail-list no">
            <li><span>{{ $t('dashboard.boundPools') }}</span><strong data-cy="pool-lists-bound">{{ $utils.niceNumber(counts.poolLists.bound || 0) }}</strong></li>
            <li><span>{{ $t('dashboard.unboundPools') }}</span><strong data-cy="pool-lists-unbound">{{ $utils.niceNumber(counts.poolLists.unbound || 0) }}</strong></li>
            <li><span>{{ $t('dashboard.boundOrganizations') }}</span><strong data-cy="pool-lists-organizations">{{ $utils.niceNumber(counts.poolLists.organizations || 0) }}</strong></li>
          </ul>
        </div>
      </article>
    </div>
    <div v-if="loadErrors.counts || loadErrors.campaigns" class="dashboard-error" role="alert">
      <span>{{ $t('dashboard.partialLoadError') }}</span>
      <b-button size="is-small" icon-left="refresh" @click="fetchData">{{ $t('dashboard.retry') }}</b-button>
    </div>
    <p v-if="settings['app.cache_slow_queries']" class="has-text-grey dashboard-cache-note">
      *{{ $t('globals.messages.slowQueriesCached') }}
    </p>
  </section>
</template>

<script>
import Vue from 'vue';
import { mapState } from 'vuex';
import dayjs from 'dayjs';
import { colors } from '../constants';
import DashboardChart from '../components/DashboardChart.vue';

const CHART_COLORS = ['#155eef', '#0e9384', '#f79009', '#7a5af8', '#98a2b3', '#d92d20'];
const CAMPAIGN_STATUSES = ['running', 'scheduled', 'draft', 'paused', 'finished', 'cancelled', 'deferred'];

export default Vue.extend({
  components: { DashboardChart },

  data() {
    return {
      performanceMetric: 'views',
      lastUpdated: '',
      loadErrors: { counts: false, charts: false, campaigns: false },
      campaignViews: [],
      campaignClicks: [],
      campaigns: [],
      counts: {
        customerLists: {}, poolLists: {}, privateCustomers: {}, poolCustomers: {}, campaigns: {}, messages: 0,
      },
    };
  },

  computed: {
    ...mapState(['settings']),
    metrics() {
      const n = (value) => value || 0;
      return [
        {
          key: 'private-customers',
          icon: 'account-multiple-outline',
          label: this.$tc('globals.terms.customer', n(this.counts.privateCustomers.total)),
          value: n(this.counts.privateCustomers.total),
          caption: this.$t('dashboard.privateCustomerCaption'),
          to: { name: 'customers' },
        },
        {
          key: 'messages',
          icon: 'email-outline',
          label: this.$t('dashboard.messagesSent'),
          value: n(this.counts.messages),
          caption: this.$t('dashboard.messagesCaption'),
          to: { name: 'campaigns' },
        },
        {
          key: 'campaigns-total',
          icon: 'rocket-launch-outline',
          label: this.$tc('globals.terms.campaign', n(this.counts.campaigns.total)),
          value: n(this.counts.campaigns.total),
          caption: this.$t('dashboard.campaignsCaption', { count: this.activeCampaignCount }),
          to: { name: 'campaigns' },
        },
        {
          key: 'customer-lists',
          icon: 'format-list-bulleted',
          label: this.$t('dashboard.privateLists'),
          value: n(this.counts.customerLists.total),
          caption: this.$t('dashboard.listsCaption', {
            public: n(this.counts.customerLists.public), private: n(this.counts.customerLists.private),
          }),
          to: { name: 'customerLists' },
        },
        {
          key: 'pool-customers',
          icon: 'account-group-outline',
          label: this.$t('dashboard.poolCustomers'),
          value: n(this.counts.poolCustomers.total),
          caption: this.$t('dashboard.poolCustomerCaption', { count: n(this.counts.poolCustomers.active) }),
          to: { name: 'poolLists' },
        },
      ];
    },
    trendTotals() {
      const total = (data) => data.reduce((sum, point) => sum + point.count, 0);
      return { views: total(this.campaignViews), clicks: total(this.campaignClicks) };
    },
    interactionRate() {
      return this.trendTotals.views ? ((this.trendTotals.clicks / this.trendTotals.views) * 100).toFixed(1) : '0.0';
    },
    hasTrendData() {
      return this.campaignViews.length > 0 || this.campaignClicks.length > 0;
    },
    trendRangeLabel() {
      if (!this.timeline.length) return this.$t('dashboard.last30Days');
      const format = (value) => this.$utils.niceDate(value);
      return this.$t('dashboard.trendRange', { from: format(this.timeline[0]), to: format(this.timeline[this.timeline.length - 1]) });
    },
    timeline() {
      const dates = [...this.campaignViews, ...this.campaignClicks].map((point) => dayjs(point.date));
      if (!dates.length) return [];
      const end = dayjs(Math.max(...dates.map((date) => date.valueOf())));
      const start = end.subtract(29, 'day');
      return Array.from({ length: 30 }, (_, index) => start.add(index, 'day').format('YYYY-MM-DD'));
    },
    viewTrendOption() {
      return this.makeTrendOption(this.campaignViews, this.$t('dashboard.campaignViews'), colors.primary, 'rgba(21, 94, 239, 0.09)');
    },
    clickTrendOption() {
      return this.makeTrendOption(this.campaignClicks, this.$t('dashboard.linkClicks'), '#0e9384', 'rgba(14, 147, 132, 0.07)');
    },
    campaignStatuses() {
      const statuses = this.counts.campaigns.byStatus || {};
      return Object.entries(statuses).filter(([, value]) => value > 0).map(([key, value]) => ({
        key, value, label: this.$t(`campaigns.status.${key}`), color: CHART_COLORS[CAMPAIGN_STATUSES.indexOf(key) % CHART_COLORS.length],
      }))
        .sort((a, b) => CAMPAIGN_STATUSES.indexOf(a.key) - CAMPAIGN_STATUSES.indexOf(b.key));
    },
    activeCampaignCount() {
      const statuses = this.counts.campaigns.byStatus || {};
      return (statuses.running || 0) + (statuses.scheduled || 0);
    },
    campaignCompletionRate() {
      const total = this.counts.campaigns.total || 0;
      return total ? Math.round(((this.counts.campaigns.byStatus.finished || 0) / total) * 100) : 0;
    },
    campaignStatusOption() {
      return {
        animationDuration: 450,
        tooltip: {
          trigger: 'item',
          backgroundColor: '#fff',
          borderColor: colors.grid,
          textStyle: { color: colors.text },
          formatter: ({ name, value, percent }) => `${name}<br/><strong>${value}</strong> · ${percent}%`,
        },
        series: [{
          type: 'pie',
          radius: ['69%', '90%'],
          center: ['50%', '50%'],
          avoidLabelOverlap: true,
          itemStyle: { borderColor: '#fff', borderWidth: 3, borderRadius: 3 },
          label: { show: false },
          emphasis: { scaleSize: 4 },
          data: this.campaignStatuses.map((item) => ({ name: item.label, value: item.value, itemStyle: { color: item.color } })),
        }],
      };
    },
    sourceCampaignId() {
      return Number(this.$route.query.source) || null;
    },
    sourceCampaign() {
      return this.campaigns.find((campaign) => campaign.id === this.sourceCampaignId) || null;
    },
    performanceCampaigns() {
      return [...this.campaigns].sort((a, b) => (b[this.performanceMetric] || 0) - (a[this.performanceMetric] || 0)).slice(0, 5);
    },
    performanceOption() {
      const data = [...this.performanceCampaigns].reverse();
      const metricLabels = {
        views: 'dashboard.campaignViews', clicks: 'dashboard.linkClicks', bounces: 'globals.terms.bounces', unsubscribes: 'campaigns.unsubscribes',
      };
      const metricColors = {
        views: colors.primary, clicks: '#0e9384', bounces: '#f79009', unsubscribes: '#7a5af8',
      };
      const formatTooltip = ([point]) => {
        const campaign = data[point.dataIndex];
        return `${campaign.name}<br/>${this.$t(metricLabels[this.performanceMetric])}: <strong>${this.$utils.niceNumber(point.value)}</strong>`;
      };
      return {
        animationDuration: 450,
        grid: {
          left: 10, right: 20, top: 12, bottom: 8, containLabel: true,
        },
        tooltip: {
          trigger: 'axis',
          axisPointer: { type: 'shadow' },
          backgroundColor: '#fff',
          borderColor: colors.grid,
          textStyle: { color: colors.text },
          formatter: formatTooltip,
        },
        xAxis: {
          type: 'value', minInterval: 1, splitLine: { lineStyle: { color: colors.grid, type: 'dashed' } }, axisLabel: { color: colors.muted, fontSize: 12 },
        },
        yAxis: {
          type: 'category',
          data: data.map((campaign) => `#${campaign.id} · ${campaign.name}`),
          axisLine: { show: false },
          axisTick: { show: false },
          axisLabel: {
            color: colors.text, fontSize: 12, width: 112, overflow: 'truncate',
          },
        },
        series: [{
          type: 'bar',
          barMaxWidth: 22,
          data: data.map((campaign) => ({
            value: campaign[this.performanceMetric] || 0,
            itemStyle: { color: metricColors[this.performanceMetric] },
          })),
          itemStyle: { borderRadius: [0, 3, 3, 0] },
          label: {
            show: true, position: 'right', color: colors.muted, fontSize: 12,
          },
        }],
      };
    },
    deliveryCampaigns() {
      return this.campaigns.slice(0, 5);
    },
    deliveryTotals() {
      return this.deliveryCampaigns.reduce((total, campaign) => ({
        sent: total.sent + (campaign.deliverySent || 0),
        pending: total.pending + (campaign.deliveryPending || 0),
        unavailable: total.unavailable + (campaign.deliveryUnavailable || 0),
        unsubscribeClicks: total.unsubscribeClicks + (campaign.unsubscribeClicks || 0),
        unsubscribes: total.unsubscribes + (campaign.unsubscribes || 0),
      }), {
        sent: 0, pending: 0, unavailable: 0, unsubscribeClicks: 0, unsubscribes: 0,
      });
    },
    unsubscribeRate() {
      return this.deliveryTotals.unsubscribeClicks
        ? ((this.deliveryTotals.unsubscribes / this.deliveryTotals.unsubscribeClicks) * 100).toFixed(1)
        : '0.0';
    },
    deliveryOption() {
      const data = [...this.deliveryCampaigns].reverse();
      const series = [
        { key: 'deliverySent', label: this.$t('campaigns.deliverySent'), color: colors.primary },
        { key: 'deliveryPending', label: this.$t('campaigns.deliveryPending'), color: '#f79009' },
        { key: 'deliveryUnavailable', label: this.$t('campaigns.deliveryCannotSend'), color: '#d92d20' },
      ];
      return {
        animationDuration: 450,
        color: series.map((item) => item.color),
        legend: {
          bottom: 0, icon: 'circle', itemWidth: 8, itemHeight: 8, textStyle: { color: colors.muted, fontSize: 12 },
        },
        grid: {
          left: 10, right: 24, top: 12, bottom: 36, containLabel: true,
        },
        tooltip: {
          trigger: 'axis',
          axisPointer: { type: 'shadow' },
          backgroundColor: '#fff',
          borderColor: colors.grid,
          textStyle: { color: colors.text },
          valueFormatter: (value) => this.$utils.niceNumber(value),
        },
        xAxis: {
          type: 'value', minInterval: 1, splitLine: { lineStyle: { color: colors.grid, type: 'dashed' } }, axisLabel: { color: colors.muted, fontSize: 12 },
        },
        yAxis: {
          type: 'category',
          data: data.map((campaign) => `#${campaign.id} · ${campaign.name}`),
          axisLine: { show: false },
          axisTick: { show: false },
          axisLabel: {
            color: colors.text, fontSize: 12, width: 120, overflow: 'truncate',
          },
        },
        series: series.map((item) => ({
          name: item.label,
          type: 'bar',
          stack: 'delivery',
          barMaxWidth: 20,
          data: data.map((campaign) => campaign[item.key] || 0),
          itemStyle: { color: item.color },
        })),
      };
    },
    audienceTotal() {
      return (this.counts.privateCustomers.total || 0) + (this.counts.poolCustomers.total || 0);
    },
    audienceSegments() {
      const privateCustomers = this.counts.privateCustomers.total || 0;
      const privateActive = Math.max(0, privateCustomers - (this.counts.privateCustomers.blocklisted || 0));
      return [
        {
          key: 'private', label: this.$t('dashboard.privateCustomersActive'), value: privateActive, color: colors.primary,
        },
        {
          key: 'pool', label: this.$t('dashboard.poolCustomers'), value: this.counts.poolCustomers.total || 0, color: '#0e9384',
        },
        {
          key: 'blocklisted', label: this.$t('customers.status.blocklisted'), value: this.counts.privateCustomers.blocklisted || 0, color: '#98a2b3',
        },
      ];
    },
    audienceOption() {
      const data = this.audienceSegments.filter((segment) => segment.value > 0);
      return {
        animationDuration: 450,
        tooltip: {
          trigger: 'item',
          backgroundColor: '#fff',
          borderColor: colors.grid,
          textStyle: { color: colors.text },
          formatter: ({ name, value, percent }) => `${name}<br/><strong>${value}</strong> · ${percent}%`,
        },
        series: [{
          type: 'pie',
          radius: ['69%', '90%'],
          center: ['50%', '50%'],
          itemStyle: { borderColor: '#fff', borderWidth: 3, borderRadius: 3 },
          label: { show: false },
          emphasis: { scaleSize: 4 },
          data: data.length ? data.map((item) => ({ name: item.label, value: item.value, itemStyle: { color: item.color } })) : [{
            name: this.$t('dashboard.noAudience'), value: 1, itemStyle: { color: colors.grid }, tooltip: { show: false }, emphasis: { disabled: true },
          }],
        }],
      };
    },
    listSegments() {
      const lists = this.counts.customerLists;
      return [
        {
          key: 'public', label: this.$t('customer_lists.types.public'), value: lists.public || 0, color: colors.primary,
        },
        {
          key: 'private', label: this.$t('customer_lists.types.private'), value: lists.private || 0, color: '#7a5af8',
        },
        {
          key: 'single', label: this.$t('customer_lists.optins.single'), value: lists.optinSingle || 0, color: '#0e9384',
        },
        {
          key: 'double', label: this.$t('customer_lists.optins.double'), value: lists.optinDouble || 0, color: '#f79009',
        },
      ].map((item) => ({ ...item, percent: lists.total ? (item.value / lists.total) * 100 : 0 }));
    },
    poolOption() {
      const total = this.counts.poolCustomers.total || 0;
      return {
        animationDuration: 450,
        grid: {
          left: 0, right: 0, top: 12, bottom: 0,
        },
        xAxis: { type: 'value', max: Math.max(total, 1), show: false },
        yAxis: { type: 'category', data: [''], show: false },
        series: [
          {
            name: this.$t('pool.activeContacts'),
            type: 'bar',
            stack: 'pool',
            barWidth: 12,
            data: [this.counts.poolCustomers.active || 0],
            itemStyle: { color: '#0e9384', borderRadius: [6, 0, 0, 6] },
          },
          {
            name: this.$t('pool.statusRemovedGlobal'),
            type: 'bar',
            stack: 'pool',
            barWidth: 12,
            data: [this.counts.poolCustomers.removed || 0],
            itemStyle: { color: '#d0d5dd', borderRadius: [0, 6, 6, 0] },
          },
        ],
      };
    },
  },

  watch: {
    performanceMetric(value) {
      if (this.$route.name !== 'dashboard' || this.$route.query.metric === value) return;
      this.$router.replace({ query: { ...this.$route.query, metric: value } }).catch(() => {});
    },
  },

  methods: {
    makeTrendOption(points, name, color, areaColor) {
      const counts = Object.fromEntries(points.map((point) => [dayjs(point.date).format('YYYY-MM-DD'), point.count]));
      const data = this.timeline.map((date) => (counts[date] === undefined ? null : counts[date]));
      const sparse = points.length <= 3;
      const type = sparse ? 'bar' : 'line';
      return {
        animationDuration: 450,
        grid: {
          left: 8, right: 32, top: 18, bottom: 8, containLabel: true,
        },
        tooltip: {
          trigger: 'axis',
          backgroundColor: '#fff',
          borderColor: colors.grid,
          textStyle: { color: colors.text },
          axisPointer: { type: 'line', lineStyle: { color: colors.grid } },
          valueFormatter: (value) => (value === null ? '—' : this.$utils.niceNumber(value)),
        },
        xAxis: {
          type: 'category',
          boundaryGap: sparse,
          data: this.timeline,
          axisLine: { lineStyle: { color: colors.grid } },
          axisTick: { show: false },
          axisLabel: {
            color: colors.muted,
            fontSize: 12,
            margin: 12,
            interval: 0,
            hideOverlap: true,
            showMinLabel: true,
            showMaxLabel: true,
            formatter: (value) => ([0, 10, 20, this.timeline.length - 1].includes(this.timeline.indexOf(value))
              ? dayjs(value).format(this.$i18n.locale === 'zh-CN' ? 'MM月DD日' : 'MMM D') : ''),
          },
        },
        yAxis: {
          type: 'value', minInterval: 1, splitNumber: 4, axisLabel: { color: colors.muted, fontSize: 12 }, splitLine: { lineStyle: { color: colors.grid, type: 'dashed' } },
        },
        series: [{
          name,
          type,
          data,
          connectNulls: false,
          barMaxWidth: 12,
          itemStyle: { color, borderRadius: sparse ? [3, 3, 0, 0] : 0 },
          label: {
            show: sparse, position: 'top', color: colors.muted, fontSize: 12,
          },
          symbol: 'circle',
          symbolSize: 7,
          showSymbol: sparse,
          smooth: 0.18,
          lineStyle: { width: 2.5, color },
          emphasis: { focus: 'series' },
          areaStyle: sparse ? undefined : { color: areaColor },
        }],
      };
    },
    campaignLink(id) {
      const query = {};
      if (this.timeline.length) {
        query.from = dayjs(this.timeline[0]).startOf('day').unix();
        query.to = dayjs(this.timeline[this.timeline.length - 1]).endOf('day').unix();
      }
      return {
        name: 'campaign', params: { id }, hash: '#analytics', query,
      };
    },
    openCampaign(id) {
      const query = new URLSearchParams(window.location.search);
      query.set('metric', this.performanceMetric);
      query.set('source', String(id));
      window.history.replaceState({
        ...window.history.state, dashboardScrollY: window.scrollY,
      }, '', `${window.location.pathname}?${query.toString()}`);
      this.$router.push(this.campaignLink(id));
    },
    clearSource() {
      const query = { ...this.$route.query };
      delete query.source;
      this.$router.replace({ query });
    },
    audiencePercent(value) {
      return this.audienceTotal ? ((value / this.audienceTotal) * 100).toFixed(0) : '0';
    },
    async fetchData() {
      const [counts, charts, campaigns] = await Promise.allSettled([
        this.$api.getDashboardCounts(),
        this.$api.getDashboardCharts(),
        this.$api.getCampaigns({
          per_page: 100, order_by: 'created_at', order: 'DESC', no_body: true,
        }),
      ]);
      this.loadErrors = {
        counts: counts.status === 'rejected',
        charts: charts.status === 'rejected',
        campaigns: campaigns.status === 'rejected',
      };
      if (counts.status === 'fulfilled') this.counts = counts.value;
      if (charts.status === 'fulfilled') {
        this.campaignViews = charts.value.campaignViews || [];
        this.campaignClicks = charts.value.linkClicks || [];
      }
      if (campaigns.status === 'fulfilled') this.campaigns = campaigns.value.results || [];
      if (!Object.values(this.loadErrors).some(Boolean)) this.lastUpdated = dayjs().format('HH:mm');
    },
  },

  created() {
    this.performanceMetric = this.$route.query.metric || 'views';
    this.$root.$on('page.refresh', this.fetchData);
  },
  beforeDestroy() {
    this.$root.$off('page.refresh', this.fetchData);
  },
  mounted() {
    this.fetchData();
    if (this.$route.query.metric) {
      this.$nextTick(() => window.scrollTo(0, Number(window.history.state.dashboardScrollY) || 0));
    }
  },
});
</script>
