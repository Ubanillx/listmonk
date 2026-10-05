<template>
  <section class="report-section campaign-geo-heatmap">
    <h4 class="title is-5">{{ $t('analytics.geoTitle') }}</h4>
    <p class="has-text-grey mb-3">{{ $t('analytics.geoApproximate') }}</p>

    <div class="campaign-geo-toolbar">
      <b-field :label="$t('analytics.geoCountry')" class="campaign-geo-country">
        <b-select v-model="selectedCountry" expanded data-cy="geo-country">
          <option value="">{{ $t('analytics.geoWorld') }}</option>
          <option v-for="country in countries" :key="country.key" :value="country.key">
            {{ country.label }}{{ country.count ? ` · ${formatCount(country.count)}` : '' }}
          </option>
        </b-select>
      </b-field>
      <b-button native-type="button" icon-left="restore" data-cy="geo-reset" @click="resetView">
        {{ $t('analytics.geoResetView') }}
      </b-button>
      <b-button v-if="selectedCountry" native-type="button" icon-left="earth" @click="selectedCountry = ''">
        {{ $t('analytics.geoWorld') }}
      </b-button>
    </div>

    <div v-if="loading" class="has-text-grey has-text-centered p-5">
      <b-loading :active="loading" :is-full-page="false" />
    </div>
    <template v-else-if="report">
      <p class="mb-3">
        {{ $t('analytics.geoSummary', { total: formatCount(report.totalOpens) }) }}
        <span class="has-text-grey ml-3">
          {{ $t('analytics.geoLocated') }}: {{ formatCount(report.locatedOpens) }}
          · {{ $t('analytics.geoUnknown') }}: {{ formatCount(report.unknownOpens) }}
        </span>
      </p>
      <div v-if="!report.enabled" class="notification is-light">
        {{ $t('analytics.geoNotConfigured') }}
      </div>
      <div v-if="!hasPoints" class="notification is-light">
        {{ $t(selectedCountry ? 'analytics.geoCountryNoData' : 'analytics.geoNoData') }}
      </div>
    </template>
    <div v-else-if="error" class="notification is-light">
      {{ $t('analytics.geoLoadError') }}
    </div>

    <div class="campaign-geo-content">
      <div>
        <p class="campaign-geo-scope" aria-live="polite" data-cy="geo-scope">
          <strong>{{ selectedCountryLabel }}</strong>
          <span>{{ $t('analytics.geoCitySummary', { cities: formatCount(knownCityCount), opens: formatCount(mappedOpens) }) }}</span>
          <b-tag v-if="focusedCity" type="is-light">{{ focusedCity.city }}</b-tag>
        </p>
        <div ref="map" class="campaign-geo-map" />
      </div>
      <aside class="campaign-geo-cities" data-cy="geo-cities">
        <h5 class="title is-6">{{ $t('analytics.geoCities') }}</h5>
        <p class="help mb-3">{{ $t('analytics.geoCityHint') }}</p>
        <b-table :data="cities" :paginated="cities.length > 8" :per-page="8" :current-page.sync="cityPage"
          :loading="loading" narrowed class="campaign-geo-city-table">
          <b-table-column field="city" :label="$t('analytics.geoCity')" v-slot="props">
            <b-button v-if="props.row.city" native-type="button" type="is-text" size="is-small"
              :aria-label="$t('analytics.geoViewCity', { city: props.row.city, region: props.row.region || props.row.countryLabel })"
              class="campaign-geo-city-name" @click="focusCity(props.row)">
              {{ props.row.city }}
            </b-button>
            <span v-else class="has-text-grey">{{ $t('analytics.geoCityUnknown') }}</span>
            <small class="campaign-geo-city-region">{{ [props.row.region, props.row.countryLabel].filter(Boolean).join(' · ') }}</small>
          </b-table-column>
          <b-table-column field="count" :label="$t('analytics.geoOpens')" numeric v-slot="props">
            {{ formatCount(props.row.count) }}
          </b-table-column>
          <template #empty>
            <p class="has-text-grey p-3">{{ $t('analytics.geoNoCities') }}</p>
          </template>
        </b-table>
      </aside>
    </div>
  </section>
</template>

<script>
import { use, init, registerMap } from 'echarts/core';
import { GeoComponent, TooltipComponent, VisualMapComponent } from 'echarts/components';
import { HeatmapChart, ScatterChart } from 'echarts/charts';
import { CanvasRenderer } from 'echarts/renderers';
import { LabelLayout } from 'echarts/features';
import { colors } from '../constants';
import {
  worldMap, mapCountries, locationPoints, cityTotals, countryLabel, countryView, wrapLongitude,
} from '../utils/campaignGeo';

use([GeoComponent, TooltipComponent, VisualMapComponent, HeatmapChart, ScatterChart, CanvasRenderer, LabelLayout]);
registerMap('world', { geoJSON: worldMap });
const registeredCountries = new Set();

const escapeHTML = (value) => String(value).replace(/[&<>"']/g, (character) => ({
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;',
}[character]));

export default {
  name: 'CampaignGeoHeatmap',

  props: {
    country: {
      type: String,
      default: '',
    },
    campaignId: {
      type: Number,
      default: null,
    },
    params: {
      type: Object,
      required: true,
    },
    refreshToken: {
      type: Number,
      required: true,
    },
  },

  data() {
    return {
      loading: false,
      error: false,
      report: null,
      chart: null,
      requestToken: 0,
      selectedCountry: this.country,
      focusedCityKey: null,
      cityPage: 1,
    };
  },

  computed: {
    points() {
      return locationPoints(this.report);
    },

    filteredPoints() {
      return this.selectedCountry ? this.points.filter((point) => point.countryKey === this.selectedCountry) : this.points;
    },

    countries() {
      const countries = new Map(mapCountries.map((country) => [country.key, { key: country.key, name: country.name, count: 0 }]));
      this.points.forEach((point) => {
        const previous = countries.get(point.countryKey) || { key: point.countryKey, name: point.country || '', count: 0 };
        countries.set(point.countryKey, { ...previous, count: previous.count + point.count });
      });
      if (this.selectedCountry && !countries.has(this.selectedCountry)) {
        countries.set(this.selectedCountry, { key: this.selectedCountry, count: 0 });
      }
      return [...countries.values()].map((country) => ({ ...country, label: this.labelForCountry(country.key, country.name) }))
        .sort((first, second) => (second.count - first.count) || first.label.localeCompare(second.label, this.$i18n.locale));
    },

    selectedCountryLabel() {
      return this.selectedCountry ? this.labelForCountry(this.selectedCountry) : this.$t('analytics.geoWorld');
    },

    cities() {
      return cityTotals(this.filteredPoints).map((city) => ({ ...city, countryLabel: this.labelForCountry(city.countryKey, city.country) }));
    },

    focusedCity() {
      return this.cities.find((city) => city.key === this.focusedCityKey) || null;
    },

    knownCityCount() {
      return this.cities.filter((city) => city.city).length;
    },

    mappedOpens() {
      return this.filteredPoints.reduce((count, point) => count + point.count, 0);
    },

    hasPoints() {
      return this.filteredPoints.length > 0;
    },
  },

  watch: {
    country(value) {
      this.selectedCountry = value;
    },
    refreshToken() {
      this.load();
    },
    selectedCountry() {
      this.$emit('update:country', this.selectedCountry);
      this.focusedCityKey = null;
      this.cityPage = 1;
      this.renderChart();
    },
  },

  mounted() {
    window.addEventListener('resize', this.resize);
    // The world map is useful context even before any locations are available.
    this.renderChart();
    this.load();
  },

  beforeDestroy() {
    this.requestToken += 1;
    window.removeEventListener('resize', this.resize);
    if (this.chart) {
      this.chart.dispose();
      this.chart = null;
    }
  },

  methods: {
    formatCount(value) {
      return this.$utils.niceNumber(value || 0);
    },

    labelForCountry(key, fallback) {
      return key === 'unknown' ? this.$t('analytics.geoCountryUnknown') : countryLabel(key, fallback, this.$i18n.locale);
    },

    resetView() {
      this.focusedCityKey = null;
      this.renderChart();
    },

    async focusCity(city) {
      if (this.selectedCountry !== city.countryKey) {
        this.selectedCountry = city.countryKey;
        await this.$nextTick();
      }
      this.focusedCityKey = city.key;
      this.renderChart();
    },

    async load() {
      const token = this.requestToken + 1;
      this.requestToken = token;
      this.loading = true;
      this.error = false;

      try {
        const report = this.campaignId
          ? await this.$api.getCampaignReportGeo(this.campaignId, this.params)
          : await this.$api.getCampaignsReportGeo(this.params);
        if (token !== this.requestToken) {
          return;
        }
        this.report = report;
      } catch (err) {
        if (token === this.requestToken) {
          this.report = null;
          this.error = true;
        }
      } finally {
        if (token === this.requestToken) {
          this.loading = false;
          this.focusedCityKey = null;
          this.cityPage = 1;
          await this.$nextTick();
          if (token === this.requestToken) {
            // Also clear previous heat when a new result is empty or fails.
            this.renderChart();
          }
        }
      }
    },

    tooltip({ data }) {
      if (!data || !data.value) {
        return '';
      }
      const rows = [
        [this.$t('analytics.geoCity'), data.city],
        [this.$t('analytics.geoRegion'), data.region],
        [this.$t('analytics.geoCountry'), data.country],
        [this.$t('analytics.geoOpens'), this.formatCount(data.count)],
      ];
      return rows.filter(([, value]) => value)
        .map(([label, value]) => `${escapeHTML(label)}: ${escapeHTML(value)}`)
        .join('<br>');
    },

    renderChart() {
      if (!this.chart) {
        this.chart = init(this.$refs.map);
      }
      const view = this.selectedCountry ? countryView(this.selectedCountry, this.filteredPoints, this.focusedCity) : null;
      const mapName = view && view.geoJSON ? `campaign-country-${this.selectedCountry}` : 'world';
      if (view && view.geoJSON && !registeredCountries.has(mapName)) {
        registerMap(mapName, { geoJSON: view.geoJSON });
        registeredCountries.add(mapName);
      }
      const mapPoint = (point) => ({
        ...point,
        country: this.labelForCountry(point.countryKey, point.country),
        value: [view ? wrapLongitude(point.longitude, view.anchor) : point.longitude, point.latitude, point.count],
      });
      const heat = this.filteredPoints.map(mapPoint);
      const markers = this.cities.map((city) => ({
        ...mapPoint(city),
        name: city.city,
        label: { show: !!this.selectedCountry && !!city.city },
      }));
      this.chart.setOption({
        tooltip: { trigger: 'item', formatter: this.tooltip },
        visualMap: {
          show: this.hasPoints,
          seriesIndex: 0,
          dimension: 2,
          min: 0,
          max: Math.max(1, ...heat.map((point) => point.count)),
          calculable: true,
          orient: 'horizontal',
          left: 'center',
          bottom: 0,
          inRange: { color: ['#60a5fa', '#facc15', '#ef4444'] },
        },
        geo: {
          map: mapName,
          roam: true,
          boundingCoords: view && view.bounds ? view.bounds : undefined,
          aspectScale: 1,
          layoutCenter: ['50%', '48%'],
          layoutSize: this.mapSize(view && view.bounds),
          itemStyle: { areaColor: '#edf3f8', borderColor: '#adc0d0' },
          emphasis: { itemStyle: { areaColor: '#e2eaf2' } },
        },
        series: [{
          type: 'heatmap',
          coordinateSystem: 'geo',
          data: heat,
          pointSize: 10,
          blurSize: 24,
          minOpacity: 0.35,
          maxOpacity: 0.95,
        }, {
          type: 'scatter',
          coordinateSystem: 'geo',
          data: markers,
          symbolSize: (value) => Math.min(18, 7 + Math.sqrt(value[2]) * 2),
          itemStyle: { color: colors.primary },
          label: { formatter: '{b}', position: 'right', fontSize: 11 },
          labelLayout: { hideOverlap: true },
          z: 3,
        }],
      }, true);
      this.chart.resize();
    },

    mapSize(bounds) {
      const aspect = bounds ? (bounds[1][0] - bounds[0][0]) / (bounds[0][1] - bounds[1][1]) : 2;
      const width = Math.max(120, this.$refs.map.clientWidth - 40);
      const height = Math.max(120, this.$refs.map.clientHeight - 80);
      return aspect > 1 ? Math.min(width, height * aspect) : Math.min(height, width / aspect);
    },

    resize() {
      if (this.chart) {
        this.renderChart();
      }
    },
  },
};
</script>

<style scoped>
.campaign-geo-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: var(--lm-space-3);
  margin-bottom: var(--lm-space-4);
}

.campaign-geo-country {
  width: 280px;
  max-width: 100%;
  margin-bottom: 0;
}

.campaign-geo-content {
  display: grid;
  grid-template-columns: minmax(0, 2fr) minmax(280px, 1fr);
  gap: var(--lm-space-5);
}

.campaign-geo-scope {
  display: flex;
  flex-wrap: wrap;
  gap: var(--lm-space-3);
  align-items: baseline;
  color: var(--lm-color-text-muted);
}

.campaign-geo-city-name {
  height: auto;
  padding: 0;
  max-width: 100%;
  white-space: normal;
  text-align: left;
  color: var(--lm-color-primary);
}

.campaign-geo-city-region {
  display: block;
  color: var(--lm-color-text-muted);
  overflow-wrap: anywhere;
}

.campaign-geo-cities {
  min-width: 0;
}

.campaign-geo-map {
  width: 100%;
  height: 430px;
  min-height: 320px;
}

@media (max-width: 768px) {
  .campaign-geo-content {
    grid-template-columns: minmax(0, 1fr);
  }

  .campaign-geo-map {
    height: 320px;
  }
}
</style>
