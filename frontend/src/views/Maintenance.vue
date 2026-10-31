<template>
  <section class="maintenance wrap">
    <h1 class="title is-4">
      {{ $t('maintenance.title') }}
    </h1>
    <hr />
    <p class="has-text-grey">
      {{ $t('maintenance.platformScope') }}
    </p>
    <p class="help">{{ $t('maintenance.previewLive') }} {{ $t('maintenance.dateBoundary', { zone: deploymentZone }) }}</p>
    <br />

    <div class="box">
      <h4 class="is-size-4">
        {{ $t('globals.terms.customers') }}
      </h4><br />
      <div class="columns">
        <div class="column is-4">
          <b-field :label="$t('maintenance.dataLabel')" :message="customerType === 'orphan' ? $t('maintenance.orphanHelp') : ''">
            <b-select v-model="customerType" expanded>
              <option value="orphan">
                {{ $t('dashboard.orphanSubs') }}
              </option>
              <option value="blocklisted">
                {{ $t('customers.status.blocklisted') }}
              </option>
            </b-select>
          </b-field>
        </div>
      </div>
      <div class="cleanup-review">
        <maintenance-preview ref="customersPreview" dataset="customers" :type="customerType"
          :unit="$t('maintenance.unitCustomers')" @ready="previewReady.customers = $event" />
        <b-button type="is-danger" :loading="pendingAction === 'orphans'" :disabled="pendingAction !== '' || !previewReady.customers" @click="deleteCustomers">
          {{ $t('maintenance.deleteCustomers') }}
        </b-button>
      </div>
      <p class="help">{{ $t('maintenance.customerCascade') }}</p>
    </div><!-- customers -->

    <div class="box mt-6">
      <h4 class="is-size-4">
        {{ $tc('globals.terms.subscriptions', 2) }}
      </h4><br />
      <div class="columns">
        <div class="column is-4">
          <b-field :label="$t('maintenance.dataLabel')">
            <b-select v-model="subscriptionType" expanded>
              <option value="optin">
                {{ $t('maintenance.maintenance.unconfirmedOptins') }}
              </option>
            </b-select>
          </b-field>
        </div>
        <div class="column is-4">
          <b-field :label="$t('maintenance.olderThan')">
            <b-datepicker v-model="subscriptionPickerDate" required expanded icon="calendar-clock"
              :date-formatter="formatDateTime" :date-parser="parsePickerDate" />
          </b-field>
        </div>
      </div>
      <div class="cleanup-review">
        <maintenance-preview ref="subscriptionsPreview" dataset="subscriptions" type="unconfirmed" :before-date="subscriptionDate"
          :unit="$t('maintenance.unitSubscriptions')" @ready="previewReady.subscriptions = $event" />
        <b-button type="is-danger" :loading="pendingAction === 'unconfirmed'" :disabled="pendingAction !== '' || !previewReady.subscriptions" @click="deleteSubscriptions">
          {{ $t('maintenance.deleteSubscriptions') }}
        </b-button>
      </div>
    </div><!-- subscriptions -->

    <div class="box mt-6">
      <h4 class="is-size-4">
        {{ $t('globals.terms.analytics') }}
      </h4><br />
      <div class="columns">
        <div class="column is-4">
          <b-field :label="$t('maintenance.dataLabel')">
            <b-select v-model="analyticsType" expanded>
              <option selected value="all">
                {{ $t('globals.terms.all') }}
              </option>
              <option value="views">
                {{ $t('dashboard.campaignViews') }}
              </option>
              <option value="clicks">
                {{ $t('dashboard.linkClicks') }}
              </option>
            </b-select>
          </b-field>
        </div>
        <div class="column is-4">
          <b-field :label="$t('maintenance.olderThan')">
            <b-datepicker v-model="analyticsPickerDate" required expanded icon="calendar-clock"
              :date-formatter="formatDateTime" :date-parser="parsePickerDate" />
          </b-field>
        </div>
      </div>
      <div class="cleanup-review">
        <maintenance-preview ref="analyticsPreview" dataset="analytics" :type="analyticsType" :before-date="analyticsDate"
          :unit="$t('maintenance.unitAnalytics')" @ready="previewReady.analytics = $event" />
        <b-button type="is-danger" :loading="pendingAction === 'analytics'" :disabled="pendingAction !== '' || !previewReady.analytics" @click="deleteAnalytics">
          {{ $t('maintenance.deleteAnalytics') }}
        </b-button>
      </div>
    </div><!-- analytics -->

    <form @submit.prevent="onUpdateDBSettings" class="box mt-6">
      <h4 class="is-size-4">
        {{ $t('maintenance.database.title') }}
      </h4><br />
      <h5 class="is-size-5">Vacuum</h5>
      <p class="has-text-grey is-size-7">
        {{ $t('maintenance.database.vacuumHelp') }}
      </p>
      <br />
      <div class="columns">
        <div class="column is-2">
          <b-field :label="$t('globals.buttons.enabled')">
            <b-switch v-model="dbSettings.vacuum" />
          </b-field>
        </div>
        <div class="column is-4" :class="{ disabled: !dbSettings.vacuum }">
          <b-field :label="$t('settings.maintenance.cron')">
            <b-input v-model="dbSettings.vacuum_cron_interval" placeholder="0 2 * * *" :disabled="!dbSettings.vacuum"
              pattern="((\*|[0-9,\-\/]+)\s+){4}(\*|[0-9,\-\/]+)" />
          </b-field>
        </div>
        <div class="column is-3" />
        <div class="column is-3">
          <br />
          <b-button type="is-primary" native-type="submit" :loading="loading.settings" :disabled="loading.settings"
            expanded>
            {{ $t('globals.buttons.save') }}
          </b-button>
        </div>
      </div>
    </form><!-- database -->

    <b-loading :is-full-page="true" v-if="isLoading" active />
  </section>
</template>

<script>
import Vue from 'vue';
import { mapState } from 'vuex';
import { now, parse, zone } from '../time';
import MaintenancePreview from '../components/MaintenancePreview.vue';

export default Vue.extend({
  components: { MaintenancePreview },

  data() {
    return {
      isLoading: false,
      pendingAction: '',
      previewReady: { customers: false, subscriptions: false, analytics: false },
      customerType: 'orphan',
      analyticsType: 'all',
      subscriptionType: 'optin',
      // A date-only cutoff starts at midnight on the deployment's clock.
      analyticsDate: now().subtract(7, 'day').startOf('day').toDate(),
      subscriptionDate: now().subtract(7, 'day').startOf('day').toDate(),
      dbSettings: {
        vacuum: false,
        vacuum_cron_interval: '0 2 * * *',
      },
    };
  },

  mounted() {
    this.loadDBSettings();
  },

  methods: {
    formatDateTime(s) {
      return this.$utils.niceDate(this.cutoffDate(s));
    },

    // The date picker uses browser-local calendar fields; the API uses the
    // deployment's midnight. Keep those representations separate.
    cutoffDate(date) {
      return date ? this.$utils.wallClockToDate(date.getFullYear(), date.getMonth(), date.getDate()) : null;
    },

    pickerDate(date) {
      const d = parse(date);
      return d.isValid() ? new Date(d.year(), d.month(), d.date()) : null;
    },

    parsePickerDate(value) {
      return this.pickerDate(this.$utils.parseDateTime(value));
    },

    deleteCustomers() {
      const type = this.customerType;
      const target = type === 'orphan' ? this.$t('dashboard.orphanSubs') : this.$t('customers.status.blocklisted');
      this.confirmCleanup('customers', target, () => this.$api.deleteGCCustomers(type));
    },

    deleteSubscriptions() {
      const date = new Date(this.subscriptionDate);
      this.confirmCleanup('subscriptions', this.$t('maintenance.maintenance.unconfirmedOptins'), () => this.$api.deleteGCSubscriptions(date));
    },

    deleteAnalytics() {
      const type = this.analyticsType;
      const date = new Date(this.analyticsDate);
      const labels = { all: 'globals.terms.all', views: 'dashboard.campaignViews', clicks: 'dashboard.linkClicks' };
      this.confirmCleanup('analytics', this.$t(labels[type]), () => this.$api.deleteGCCampaignAnalytics(type, date));
    },

    async confirmCleanup(dataset, target, remove) {
      if (this.pendingAction) return;
      const actionNames = { customers: 'orphans', subscriptions: 'unconfirmed', analytics: 'analytics' };
      this.pendingAction = actionNames[dataset];
      const component = this.$refs[`${dataset}Preview`];
      const { signature } = component;
      const preview = await component.load();
      if (!preview || !preview.count || signature !== component.signature) {
        this.pendingAction = '';
        return;
      }
      const date = component.beforeDate ? ` (${this.$t('maintenance.beforeExact', { date: this.$utils.niceDate(component.beforeDate, true) })})` : '';
      const message = this.$t('maintenance.confirmPreview', {
        target, count: preview.count, unit: component.unit, date,
      });
      this.$utils.confirm(message, async () => {
        try {
          await remove();
          this.$utils.toast(this.$t('globals.messages.done'));
        } finally {
          this.pendingAction = '';
          ['customers', 'subscriptions', 'analytics'].forEach((key) => this.$refs[`${key}Preview`]?.load());
        }
      }, () => { this.pendingAction = ''; }, {
        type: 'is-danger', title: this.$t('maintenance.confirmTitle'), confirmText: this.$t('maintenance.confirmTitle'),
      });
    },

    loadDBSettings() {
      this.$api.getSettings().then((data) => {
        if (data['maintenance.db'] !== undefined) {
          this.dbSettings = { ...data['maintenance.db'] };
        }
      });
    },

    async onUpdateDBSettings() {
      this.isLoading = true;
      const data = await this.$api.updateSettingsByKey('maintenance.db', this.dbSettings);
      await this.$root.awaitRestart(data);
      this.isLoading = false;
    },
  },

  computed: {
    ...mapState(['loading']),
    deploymentZone: () => zone(),
    subscriptionPickerDate: {
      get() { return this.pickerDate(this.subscriptionDate); },
      set(date) { if (date) this.subscriptionDate = this.cutoffDate(date); },
    },
    analyticsPickerDate: {
      get() { return this.pickerDate(this.analyticsDate); },
      set(date) { if (date) this.analyticsDate = this.cutoffDate(date); },
    },
  },

});
</script>

<style scoped>
.cleanup-review {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  margin-bottom: 1rem;
}
.cleanup-review > .button { min-width: 250px; flex-shrink: 0; }
@media screen and (max-width: 768px) {
  .cleanup-review { flex-direction: column; align-items: stretch; }
  .cleanup-review > .button { width: 100%; min-width: 0; }
}
</style>
