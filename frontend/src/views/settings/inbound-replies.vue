<template>
  <div>
    <div class="columns mb-6">
      <div class="column is-3">
        <b-field :label="$t('settings.inboundReplies.enable')">
          <b-switch v-model="data['reply_ai'].enabled" name="reply_ai_enabled" />
        </b-field>
      </div>
      <div class="column">
        <p class="has-text-grey help">{{ $t('settings.inboundReplies.enableHelp') }}</p>
      </div>
    </div>

    <div :class="{ disabled: !data['reply_ai'].enabled }">
      <div class="columns">
        <div class="column is-6">
          <b-field :label="$t('settings.inboundReplies.baseUrl')" label-position="on-border"
            :message="$t('settings.inboundReplies.baseUrlHelp')">
            <b-input v-model.trim="data['reply_ai'].base_url" type="url" name="reply_ai_base_url"
              placeholder="https://api.openai.com/v1" />
          </b-field>
        </div>
        <div class="column is-6">
          <b-field :label="$t('settings.inboundReplies.model')" label-position="on-border">
            <b-input v-model.trim="data['reply_ai'].model" name="reply_ai_model" placeholder="gpt-4o-mini" />
          </b-field>
        </div>
      </div>
      <div class="columns">
        <div class="column is-6">
          <b-field :label="$t('settings.inboundReplies.apiKey')" label-position="on-border"
            :message="$t('globals.messages.passwordChange')">
            <b-input v-model="data['reply_ai'].api_key" type="password" password-reveal name="reply_ai_api_key" />
          </b-field>
        </div>
        <div class="column is-3">
          <b-field :label="$t('settings.inboundReplies.timeout')" label-position="on-border">
            <b-input v-model="data['reply_ai'].timeout" name="reply_ai_timeout" placeholder="15s" />
          </b-field>
        </div>
        <div class="column is-3">
          <b-field :label="$t('settings.inboundReplies.minConfidence')" label-position="on-border"
            :message="$t('settings.inboundReplies.minConfidenceHelp')">
            <b-numberinput v-model="data['reply_ai'].min_confidence" name="reply_ai_min_confidence"
              :min="0.1" :max="1" :step="0.01" controls-position="compact" />
          </b-field>
        </div>
      </div>
      <div class="notification is-light">
        <p class="help mb-2">
          <b-icon icon="alert-outline" size="is-small" />
          {{ $t('settings.inboundReplies.scopeHelp') }}
        </p>
      </div>
    </div>
  </div>
</template>

<script>
import Vue from 'vue';

export default Vue.extend({
  props: {
    form: {
      type: Object, default: () => { },
    },
  },

  data() {
    return {
      data: this.form,
    };
  },
});
</script>
