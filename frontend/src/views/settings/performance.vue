<template>
  <div class="items">
    <section>
      <p class="help mb-5" data-cy="platform-delivery-help">{{ $t('settings.performance.platformHelp') }}</p>
      <div class="columns">
        <div class="column is-6">
          <b-field :label="$t('settings.performance.concurrency')" label-position="on-border"
            :message="$t('settings.performance.concurrencyHelp')">
            <b-numberinput v-model="data['app.concurrency']" name="app.concurrency" type="is-light" placeholder="5" min="1"
              max="10000" />
          </b-field>
        </div>
        <div class="column is-6">
          <b-field :label="$t('settings.performance.messageRate')" label-position="on-border"
            :message="$t('settings.performance.messageRateHelp')">
            <b-numberinput v-model="data['app.message_rate']" name="app.message_rate" type="is-light" placeholder="5" min="1"
              max="100000" />
          </b-field>
        </div>
      </div>

      <b-field :label="$t('settings.performance.batchSize')" label-position="on-border"
        :message="$t('settings.performance.batchSizeHelp')">
        <b-numberinput v-model="data['app.batch_size']" name="app.batch_size" type="is-light" placeholder="1000" min="1"
          max="100000" />
      </b-field>

      <b-field :label="$t('settings.performance.maxErrThreshold')" label-position="on-border"
        :message="$t('settings.performance.maxErrThresholdHelp')">
        <b-numberinput v-model="data['app.max_send_errors']" name="app.max_send_errors" type="is-light" placeholder="1999"
          min="0" max="100000" />
      </b-field>

      <hr />

      <b-field :label="$t('settings.performance.sendErrorAlert')"
        :message="$t('settings.performance.sendErrorAlertHelp')">
        <b-switch v-model="data['app.send_error_alert_enabled']" name="app.send_error_alert_enabled"
          data-cy="send-error-alert-switch" />
      </b-field>

      <div class="columns" :class="{ disabled: !data['app.send_error_alert_enabled'] }">
        <div class="column is-4">
          <b-field :label="$t('settings.performance.sendErrorAlertThreshold')" label-position="on-border"
            :message="$t('settings.performance.sendErrorAlertThresholdHelp')">
            <b-numberinput v-model="data['app.send_error_alert_threshold']"
              name="app.send_error_alert_threshold" type="is-light" placeholder="50" :disabled="!data['app.send_error_alert_enabled']"
              data-cy="send-error-alert-threshold" min="1" max="1000000" />
          </b-field>
        </div>
        <div class="column is-4">
          <b-field :label="$t('settings.performance.sendErrorAlertWindow')" label-position="on-border"
            :message="$t('settings.performance.sendErrorAlertWindowHelp')">
            <b-input v-model="data['app.send_error_alert_window']" name="app.send_error_alert_window"
              :disabled="!data['app.send_error_alert_enabled']" data-cy="send-error-alert-window"
              placeholder="5m" :pattern="regDuration" :maxlength="10" />
          </b-field>
        </div>
        <div class="column is-4">
          <b-field :label="$t('settings.performance.sendErrorAlertCooldown')" label-position="on-border"
            :message="$t('settings.performance.sendErrorAlertCooldownHelp')">
            <b-input v-model="data['app.send_error_alert_cooldown']" name="app.send_error_alert_cooldown"
              :disabled="!data['app.send_error_alert_enabled']" data-cy="send-error-alert-cooldown"
              placeholder="30m" :pattern="regDuration" :maxlength="10" />
          </b-field>
        </div>
      </div>
    </section>

    <hr />
    <div>
      <div class="columns">
        <div class="column is-6">
          <b-field :label="$t('settings.performance.slidingWindow')"
            :message="$t('settings.performance.slidingWindowHelp')">
            <b-switch v-model="data['app.message_sliding_window']" name="app.message_sliding_window" />
          </b-field>
        </div>

        <div class="column is-3" :class="{ disabled: !data['app.message_sliding_window'] }">
          <b-field :label="$t('settings.performance.slidingWindowRate')" label-position="on-border"
            :message="$t('settings.performance.slidingWindowRateHelp')">
            <b-numberinput v-model="data['app.message_sliding_window_rate']" name="sliding_window_rate" type="is-light"
              controls-position="compact" :disabled="!data['app.message_sliding_window']" placeholder="25" min="1"
              max="10000000" />
          </b-field>
        </div>

        <div class="column is-3" :class="{ disabled: !data['app.message_sliding_window'] }">
          <b-field :label="$t('settings.performance.slidingWindowDuration')" label-position="on-border"
            :message="$t('settings.performance.slidingWindowDurationHelp')">
            <b-input v-model="data['app.message_sliding_window_duration']" name="sliding_window_duration"
              :disabled="!data['app.message_sliding_window']" placeholder="1h" :pattern="regDuration" :maxlength="10" />
          </b-field>
        </div>
      </div>
    </div><!-- sliding window -->

    <div>
      <hr />
      <div class="columns">
        <div class="column is-4">
          <b-field :label="$t('settings.performance.cacheSlowQueries')"
            :message="$t('settings.performance.cacheSlowQueriesHelp')">
            <b-switch v-model="data['app.cache_slow_queries']" name="app.cache_slow_queries" />
          </b-field>
        </div>
        <div class="column is-4" :class="{ disabled: !data['app.cache_slow_queries'] }">
          <b-field :label="$t('settings.maintenance.cron')">
            <b-input v-model="data['app.cache_slow_queries_interval']" :disabled="!data['app.cache_slow_queries']"
              placeholder="0 3 * * *" />
          </b-field>
        </div>
      </div>
    </div>
  </div>
</template>

<script>
import Vue from 'vue';
import { regDuration } from '../../constants';

export default Vue.extend({
  props: {
    form: {
      type: Object, default: () => { },
    },
  },

  data() {
    return {
      data: this.form,
      regDuration,
    };
  },
});
</script>
