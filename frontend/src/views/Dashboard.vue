<template>
  <section class="dashboard content">
    <header class="columns">
      <div class="column is-two-thirds">
        <h1 class="title is-5">
          {{ $utils.niceDate(new Date()) }}
        </h1>
      </div>
    </header>

    <section class="counts wrap">
      <div class="tile is-ancestor">
        <div class="tile is-vertical is-12">
          <div class="tile">
            <div class="tile is-parent is-vertical relative">
              <b-loading v-if="isCountsLoading" active :is-full-page="false" />
              <article class="tile is-child notification metric-card" data-cy="customerLists">
                <div class="columns is-mobile">
                  <div class="column is-6">
                    <p class="metric-value">
                      <b-icon icon="format-list-bulleted-square" />
                      {{ $utils.niceNumber(counts.customerLists.total) }}
                    </p>
                    <p class="metric-label">
                      {{ $tc('globals.terms.customer_list', counts.customerLists.total) }}
                    </p>
                  </div>
                  <div class="column is-6 metric-breakdown">
                    <ul class="no">
                      <li>
                        <strong>{{ $utils.niceNumber(counts.customerLists.public) }}</strong>
                        {{ $t('customer_lists.types.public') }}
                      </li>
                      <li>
                        <strong>{{ $utils.niceNumber(counts.customerLists.private) }}</strong>
                        {{ $t('customer_lists.types.private') }}
                      </li>
                      <li>
                        <strong>{{ $utils.niceNumber(counts.customerLists.optinSingle) }}</strong>
                        {{ $t('customer_lists.optins.single') }}
                      </li>
                      <li>
                        <strong>{{ $utils.niceNumber(counts.customerLists.optinDouble) }}</strong>
                        {{ $t('customer_lists.optins.double') }}
                      </li>
                    </ul>
                  </div>
                </div>
              </article><!-- customer_lists -->

              <article class="tile is-child notification metric-card" data-cy="pool-lists">
                <div class="columns is-mobile">
                  <div class="column is-6">
                    <p class="metric-value">
                      <b-icon icon="format-list-bulleted-square" />
                      {{ $utils.niceNumber(counts.poolLists.total) }}
                    </p>
                    <p class="metric-label">{{ $t('menu.poolLists') }}</p>
                  </div>
                  <div class="column is-6 metric-breakdown">
                    <ul class="no">
                      <li>
                        <strong data-cy="pool-lists-bound">{{ $utils.niceNumber(counts.poolLists.bound) }}</strong>
                        {{ $t('dashboard.boundPools') }}
                      </li>
                      <li>
                        <strong data-cy="pool-lists-unbound">{{ $utils.niceNumber(counts.poolLists.unbound) }}</strong>
                        {{ $t('dashboard.unboundPools') }}
                      </li>
                      <li>
                        <strong data-cy="pool-lists-allocations">{{ $utils.niceNumber(counts.poolLists.allocations) }}</strong>
                        {{ $t('dashboard.poolAllocations') }}
                      </li>
                      <li>
                        <strong data-cy="pool-lists-organizations">{{ $utils.niceNumber(counts.poolLists.organizations) }}</strong>
                        {{ $t('dashboard.boundOrganizations') }}
                      </li>
                    </ul>
                  </div>
                </div>
              </article><!-- pool_lists -->

              <article class="tile is-child notification metric-card" data-cy="campaigns">
                <div class="columns is-mobile">
                  <div class="column is-6">
                    <p class="metric-value">
                      <b-icon icon="rocket-launch-outline" />
                      {{ $utils.niceNumber(counts.campaigns.total) }}
                    </p>
                    <p class="metric-label">
                      {{ $tc('globals.terms.campaign', counts.campaigns.total) }}
                    </p>
                  </div>
                  <div class="column is-6 metric-breakdown">
                    <ul class="no">
                      <li v-for="(num, status) in counts.campaigns.byStatus" :key="status">
                        <strong :data-cy="`campaigns-${status}`">{{ num }}</strong>
                        {{ $t(`campaigns.status.${status}`) }}
                        <span v-if="status === 'running'" class="spinner is-tiny">
                          <b-loading :is-full-page="false" active />
                        </span>
                      </li>
                    </ul>
                  </div>
                </div>
              </article><!-- campaigns -->
            </div><!-- block -->

            <div class="tile is-parent is-vertical relative">
              <b-loading v-if="isCountsLoading" active :is-full-page="false" />
              <article class="tile is-child notification metric-card" data-cy="private-customers">
                <div class="columns is-mobile">
                  <div class="column is-6">
                    <p class="metric-value">
                      <b-icon icon="account-multiple" />
                      {{ $utils.niceNumber(counts.privateCustomers.total) }}
                    </p>
                    <p class="metric-label">
                      {{ $tc('globals.terms.customer', counts.privateCustomers.total) }}
                    </p>
                  </div>

                  <div class="column is-6 metric-breakdown">
                    <ul class="no">
                      <li>
                        <strong>{{ $utils.niceNumber(counts.privateCustomers.blocklisted) }}</strong>
                        {{ $t('customers.status.blocklisted') }}
                      </li>
                      <li>
                        <strong>{{ $utils.niceNumber(counts.privateCustomers.orphans) }}</strong>
                        {{ $t('dashboard.orphanSubs') }}
                      </li>
                    </ul>
                  </div><!-- customer breakdown -->
                </div><!-- customer columns -->
              </article><!-- private customers -->

              <article class="tile is-child notification metric-card" data-cy="dashboard-pool-customers">
                <div class="columns is-mobile">
                  <div class="column is-6">
                    <p class="metric-value">
                      <b-icon icon="account-group-outline" />
                      {{ $utils.niceNumber(counts.poolCustomers.total) }}
                    </p>
                    <p class="metric-label">
                      {{ $t('dashboard.poolCustomers') }}
                    </p>
                  </div>
                  <div class="column is-6 metric-breakdown">
                    <ul class="no">
                      <li>
                        <strong>{{ $utils.niceNumber(counts.poolCustomers.active) }}</strong>
                        {{ $t('pool.activeContacts') }}
                      </li>
                      <li>
                        <strong>{{ $utils.niceNumber(counts.poolCustomers.removed) }}</strong>
                        {{ $t('pool.statusRemovedGlobal') }}
                      </li>
                    </ul>
                  </div>
                </div>
              </article><!-- pool customers -->
            </div>
          </div>
          <div class="tile is-parent relative">
            <article class="tile is-child notification metric-card metric-card--compact" data-cy="messages">
              <p class="metric-value">
                <b-icon icon="email-outline" />
                {{ $utils.niceNumber(counts.messages) }}
              </p>
              <p class="metric-label">{{ $t('dashboard.messagesSent') }}</p>
            </article>
          </div>
          <div class="tile is-parent relative">
            <b-loading v-if="isChartsLoading" active :is-full-page="false" />
            <article class="tile is-child notification charts dashboard-chart-card">
              <div class="columns">
                <div class="column is-6">
                  <h3 class="dashboard-chart-title">
                    {{ $t('dashboard.campaignViews') }}
                  </h3>
                  <chart type="line" v-if="campaignViews" :data="campaignViews" />
                  <p v-else-if="!isChartsLoading" class="has-text-grey">
                    {{ $t('dashboard.noChartData') }}
                  </p>
                </div>
                <div class="column is-6">
                  <h3 class="dashboard-chart-title has-text-right">
                    {{ $t('dashboard.linkClicks') }}
                  </h3>
                  <chart type="line" v-if="campaignClicks" :data="campaignClicks" />
                  <p v-else-if="!isChartsLoading" class="has-text-grey has-text-right">
                    {{ $t('dashboard.noChartData') }}
                  </p>
                </div>
              </div>
            </article>
          </div>
        </div>
      </div><!-- tile block -->
      <p v-if="settings['app.cache_slow_queries']" class="has-text-grey">
        *{{ $t('globals.messages.slowQueriesCached') }}
      </p>
    </section>
  </section>
</template>

<script>
import Vue from 'vue';
import { mapState } from 'vuex';
import { colors } from '../constants';
import Chart from '../components/Chart.vue';

export default Vue.extend({
  components: {
    Chart,
  },

  data() {
    return {
      isChartsLoading: true,
      isCountsLoading: true,
      campaignViews: null,
      campaignClicks: null,
      counts: {
        customerLists: {},
        poolLists: {},
        privateCustomers: {},
        poolCustomers: {},
        campaigns: {},
        messages: 0,
      },
    };
  },

  methods: {
    async fetchData() {
      this.isCountsLoading = true;
      this.isChartsLoading = true;

      try {
        this.counts = await this.$api.getDashboardCounts();
      } catch (err) {
        // The response interceptor already raised the error toast; the flag
        // below still has to be cleared so the page stops "loading" forever.
      } finally {
        this.isCountsLoading = false;
      }

      try {
        const data = await this.$api.getDashboardCharts();
        this.campaignViews = this.makeChart(data.campaignViews);
        this.campaignClicks = this.makeChart(data.linkClicks);
      } catch (err) {
        // Same as above: keep the charts empty instead of spinning.
      } finally {
        this.isChartsLoading = false;
      }
    },

    makeChart(data) {
      if (!data || data.length === 0) {
        return null;
      }
      return {
        labels: data.map((d) => this.$utils.niceDate(d.date)),
        datasets: [
          {
            data: [...data.map((d) => d.count)],
            borderColor: colors.primary,
            backgroundColor: colors.primarySoft,
            borderWidth: 2,
            pointBackgroundColor: colors.primary,
            pointBorderColor: '#ffffff',
            pointHoverBackgroundColor: colors.primary,
            pointHoverBorderColor: '#ffffff',
            pointHoverBorderWidth: 3,
            pointBorderWidth: 2,
            fill: true,
          },
        ],
      };
    },
  },

  computed: {
    ...mapState(['settings']),
  },

  created() {
    this.$root.$on('page.refresh', this.fetchData);
  },

  destroyed() {
    this.$root.$off('page.refresh', this.fetchData);
  },

  mounted() {
    this.fetchData();
  },
});
</script>
