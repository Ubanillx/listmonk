<template>
  <div ref="chart" class="dashboard-chart" role="img" :aria-label="label" />
</template>

<script>
import { init, use } from 'echarts/core';
import { BarChart, LineChart, PieChart } from 'echarts/charts';
import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import { LabelLayout, LegacyGridContainLabel } from 'echarts/features';

use([BarChart, LineChart, PieChart, GridComponent, LegendComponent, TooltipComponent, CanvasRenderer, LabelLayout, LegacyGridContainLabel]);

export default {
  name: 'DashboardChart',

  props: {
    option: { type: Object, required: true },
    label: { type: String, required: true },
  },

  data() {
    return { chart: null };
  },

  watch: {
    option: {
      deep: true,
      handler() {
        this.render();
      },
    },
  },

  mounted() {
    this.chart = init(this.$refs.chart);
    this.render();
    this.observer = new ResizeObserver(() => this.chart.resize());
    this.observer.observe(this.$refs.chart);
  },

  beforeDestroy() {
    if (this.observer) this.observer.disconnect();
    if (this.chart) this.chart.dispose();
  },

  methods: {
    render() {
      this.chart.setOption(this.option, true);
    },
  },
};
</script>

<style scoped>
.dashboard-chart {
  width: 100%;
  height: 100%;
  min-height: 0;
  overflow: hidden;
  contain: strict;
}
</style>
