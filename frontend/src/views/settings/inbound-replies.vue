<template>
  <div>
    <div class="columns mb-5">
      <div class="column is-3">
        <b-field :label="$t('settings.inboundReplies.enable')">
          <b-switch v-model="data['reply_ai'].enabled" name="reply_ai_enabled" data-cy="reply-ai-enabled" />
        </b-field>
      </div>
      <div class="column">
        <p class="has-text-grey help mb-2">{{ $t('settings.inboundReplies.enableHelp') }}</p>
        <b-tag v-if="verified" type="is-success" rounded size="is-small" data-cy="reply-ai-verified">
          {{ $t('settings.inboundReplies.verified', { model: data['reply_ai'].model }) }}
        </b-tag>
      </div>
    </div>

    <!-- Step 1: gateway address and credentials. -->
    <h4 class="title is-6 mb-3">1. {{ $t('settings.inboundReplies.connection') }}</h4>
    <div class="columns">
      <div class="column is-7">
        <b-field :label="$t('settings.inboundReplies.baseUrl')" label-position="on-border"
          :message="$t('settings.inboundReplies.baseUrlHelp')">
          <b-input v-model.trim="cfg.base_url" type="url" name="reply_ai_base_url" placeholder="https://gateway.example.com/v1"
            @input="resetGateway" />
        </b-field>
      </div>
      <div class="column is-5">
        <b-field :label="$t('settings.inboundReplies.apiKey')" label-position="on-border"
          :message="$t('settings.inboundReplies.apiKeyHelp')">
          <b-input v-model="cfg.api_key" type="password" password-reveal name="reply_ai_api_key" @input="resetGateway" />
        </b-field>
      </div>
    </div>
    <div class="columns">
      <div class="column is-3">
        <b-field :label="$t('settings.inboundReplies.timeout')" label-position="on-border"
          :message="$t('settings.inboundReplies.timeoutHelp')">
          <b-input v-model="cfg.timeout" name="reply_ai_timeout" placeholder="15s" @input="resetGateway" />
        </b-field>
      </div>
      <div class="column is-9">
        <b-field label=" ">
          <b-field grouped>
            <b-button type="is-primary" icon-left="cloud-download-outline" :loading="loadingModels"
              :disabled="!canLoadModels" data-cy="reply-ai-load-models" @click="loadModels">
              {{ $t('settings.inboundReplies.loadModels') }}
            </b-button>
            <p v-if="models" class="help has-text-success ml-3" data-cy="reply-ai-models-summary">
              {{ $t('settings.inboundReplies.modelsLoaded', { count: models.length, chat: chatModels }) }}
            </p>
            <p v-else class="help has-text-grey ml-3">{{ $t('settings.inboundReplies.loadModelsHelp') }}</p>
          </b-field>
        </b-field>
      </div>
    </div>

    <b-notification v-if="loadError" type="is-danger" :closable="false" data-cy="reply-ai-models-error">
      {{ loadError }}
    </b-notification>
    <b-notification v-if="suggestedBaseUrl" type="is-info" :closable="false" data-cy="reply-ai-suggested-base">
      <div class="is-flex" style="align-items: center; justify-content: space-between;">
        <span>{{ $t('settings.inboundReplies.suggestedBaseUrl', { url: suggestedBaseUrl }) }}</span>
        <b-button size="is-small" type="is-info" @click="useSuggestedBaseUrl">
          {{ $t('settings.inboundReplies.useSuggestedBaseUrl') }}
        </b-button>
      </div>
    </b-notification>

    <!-- Step 2: pick a model from the gateway catalogue. -->
    <h4 class="title is-6 mb-3">2. {{ $t('settings.inboundReplies.modelSelection') }}</h4>
    <div class="columns">
      <div class="column is-7">
        <b-field :label="$t('settings.inboundReplies.model')" label-position="on-border"
          :type="modelNotListed ? 'is-warning' : ''" :message="modelMessage">
          <b-autocomplete v-model="cfg.model" :data="modelOptions" field="id" name="reply_ai_model" icon="magnify"
            clearable keep-first :loading="loadingModels" :open-on-focus="true" :maxlength="200"
            :placeholder="$t('settings.inboundReplies.modelPlaceholder')" data-cy="reply-ai-model"
            @input="resetTest" @select="resetTest">
            <template slot-scope="props">
              <div class="is-flex" style="align-items: center; justify-content: space-between;">
                <span>{{ props.option.id }}</span>
                <span>
                  <b-tag v-if="props.option.ownedBy" size="is-small" type="is-light">{{ props.option.ownedBy }}</b-tag>
                  <b-tag v-if="props.option.chatHint === false" size="is-small" type="is-warning">
                    {{ $t('settings.inboundReplies.nonChatModel') }}
                  </b-tag>
                </span>
              </div>
            </template>
          </b-autocomplete>
        </b-field>
      </div>
      <div class="column is-5">
        <b-field :label="$t('settings.inboundReplies.minConfidence')" label-position="on-border"
          :message="$t('settings.inboundReplies.minConfidenceHelp')">
          <b-numberinput v-model="data['reply_ai'].min_confidence" name="reply_ai_min_confidence"
            :min="0.1" :max="1" :step="0.01" controls-position="compact" />
        </b-field>
      </div>
    </div>

    <!-- Step 3: prove that the model answers with a bounded classification. -->
    <h4 class="title is-6 mb-3">3. {{ $t('settings.inboundReplies.test') }}</h4>
    <div class="columns">
      <div class="column is-7">
        <b-field :label="$t('settings.inboundReplies.testSample')" label-position="on-border"
          :message="$t('settings.inboundReplies.testSampleHelp')">
          <b-input v-model="sample" type="textarea" name="reply_ai_test_sample" :rows="3" :maxlength="2000"
            :placeholder="defaultSampleHint" @input="resetTest" />
        </b-field>
      </div>
      <div class="column is-5">
        <b-field :label="$t('settings.inboundReplies.testExpected')" label-position="on-border"
          :message="$t('settings.inboundReplies.testExpectedHelp')">
          <b-select v-model="expectedIntent" name="reply_ai_test_expected" expanded @input="resetTest">
            <option value="">{{ $t('settings.inboundReplies.testExpectedAuto') }}</option>
            <option v-for="intent in intentOptions" :key="intent" :value="intent">
              {{ intentLabel(intent) }}
            </option>
          </b-select>
        </b-field>
        <b-field>
          <b-button type="is-info" icon-left="play-circle-outline" :loading="testing" :disabled="!canTest"
            data-cy="reply-ai-test" @click="runTest">
            {{ $t('settings.inboundReplies.testRun') }}
          </b-button>
        </b-field>
      </div>
    </div>

    <b-notification v-if="testError" type="is-danger" :closable="false" data-cy="reply-ai-test-error">
      {{ testError }}
    </b-notification>

    <div v-if="result" class="box" data-cy="reply-ai-test-result" aria-live="polite">
      <b-notification :type="resultType(result.status)" :closable="false">
        <strong>{{ $t(`settings.inboundReplies.testStatus.${result.status}`) }}</strong>
        <span class="ml-3 has-text-grey">
          {{ $t('settings.inboundReplies.latency') }}: {{ result.latencyMs }} ms
        </span>
      </b-notification>
      <p v-for="step in result.steps" :key="step.name" class="mb-3">
        <b-icon :icon="stepIcon(step.status)" :type="iconType(step.status)" size="is-small" />
        <strong>{{ $t(`settings.inboundReplies.testStep.${step.name}`) }}</strong>：
        {{ $t(`settings.inboundReplies.testStatus.${step.status}`) }}
        <span v-if="step.reason" class="has-text-grey">
          （{{ reasonLabel(step.reason) }}）
        </span>
        <br>
        <small v-if="step.detail" class="has-text-grey">{{ step.detail }}</small>
      </p>
      <template v-if="result.decision">
        <hr>
        <p>
          <strong>{{ $t('settings.inboundReplies.verdict') }}</strong>：
          {{ intentLabel(result.decision.intent) }}
          · {{ $t('settings.inboundReplies.confidence') }} {{ result.decision.confidence }}
          · {{ reasonLabel(result.decision.reasonCode) }}
        </p>
        <p v-if="result.expectedIntent">
          <b-tag :type="result.matched ? 'is-success' : 'is-warning'" rounded>
            {{ result.matched ? $t('settings.inboundReplies.matched') : $t('settings.inboundReplies.mismatched') }}
          </b-tag>
          <span class="has-text-grey ml-2">
            {{ $t('settings.inboundReplies.testExpected') }}: {{ intentLabel(result.expectedIntent) }}
          </span>
        </p>
        <template v-if="result.response">
          <p class="mt-3"><strong>{{ $t('settings.inboundReplies.modelReply') }}</strong></p>
          <pre class="reply-ai-response">{{ result.response }}</pre>
        </template>
      </template>
    </div>

    <div class="notification is-light">
      <p class="help">
        <b-icon icon="alert-outline" size="is-small" />
        {{ $t('settings.inboundReplies.scopeHelp') }}
      </p>
    </div>
  </div>
</template>

<script>
import Vue from 'vue';

const MAX_SUGGESTIONS = 60;

export default Vue.extend({
  props: {
    form: {
      type: Object, default: () => { },
    },
  },

  data() {
    const d = this.form || {};
    d.reply_ai = d.reply_ai || {
      enabled: false, base_url: '', api_key: '', model: '', timeout: '15s', min_confidence: 0.98,
    };
    return {
      data: d,
      // Probe state stays out of `data` so it never reaches the settings API.
      models: null,
      chatModels: 0,
      suggestedBaseUrl: '',
      loadingModels: false,
      loadError: '',
      testing: false,
      testError: '',
      result: null,
      sample: '',
      expectedIntent: '',
      // The four intents the classifier can return, for the expectation select.
      intentOptions: ['unsubscribe', 'complaint', 'product_complaint', 'other'],
    };
  },

  computed: {
    cfg() {
      return this.data.reply_ai;
    },

    verified() {
      return !!this.result && this.result.status === 'success';
    },

    // Both fields must be filled before the gateway can be queried. A masked
    // key means a stored key exists, so it counts as filled.
    canLoadModels() {
      return !!this.cfg.base_url && !!this.cfg.api_key && !this.loadingModels;
    },

    canTest() {
      return this.canLoadModels && !!this.cfg.model && !this.testing;
    },

    modelNotListed() {
      return !!this.models && !!this.cfg.model
        && !this.models.some((m) => m.id.toLowerCase() === this.cfg.model.trim().toLowerCase());
    },

    modelOptions() {
      if (!this.models) {
        return [];
      }
      const term = (this.cfg.model || '').trim().toLowerCase();
      const matches = term ? this.models.filter((m) => m.id.toLowerCase().includes(term)) : this.models;
      return matches.slice(0, MAX_SUGGESTIONS);
    },

    modelMessage() {
      if (!this.models) {
        return this.$t('settings.inboundReplies.modelHelp');
      }
      if (this.modelNotListed) {
        return this.$t('settings.inboundReplies.modelNotListed');
      }
      if (this.models.length > MAX_SUGGESTIONS) {
        return this.$t('settings.inboundReplies.modelCountHint', {
          shown: MAX_SUGGESTIONS, total: this.models.length,
        });
      }
      return this.$t('settings.inboundReplies.modelHelp');
    },

    defaultSampleHint() {
      return this.$t('settings.inboundReplies.testSamplePlaceholder');
    },
  },

  methods: {
    // The API key field shows the stored key as a mask. Sending it back would
    // overwrite the stored key with bullets, so a masked value means "reuse the
    // stored key", exactly like the settings form does on save.
    sendableKey() {
      const key = this.cfg.api_key || '';
      return /^•+$/.test(key) ? '' : key;
    },

    probePayload() {
      return {
        base_url: this.cfg.base_url || '',
        api_key: this.sendableKey(),
        timeout: this.cfg.timeout || '',
      };
    },

    resetGateway() {
      this.models = null;
      this.chatModels = 0;
      this.suggestedBaseUrl = '';
      this.loadError = '';
      this.resetTest();
    },

    resetTest() {
      this.result = null;
      this.testError = '';
    },

    async loadModels() {
      this.loadingModels = true;
      this.loadError = '';
      this.suggestedBaseUrl = '';
      try {
        const data = await this.$api.listReplyAIModels(this.probePayload());
        this.models = data.models || [];
        this.chatModels = data.chatCandidates || 0;
        this.suggestedBaseUrl = data.suggestedBaseUrl || '';
        // A stored model that the gateway does not list stays selected, but the
        // field flags it so the operator can correct it.
        this.resetTest();
      } catch (e) {
        this.models = null;
        this.loadError = this.errorMessage(e, 'settings.inboundReplies.loadModelsFailed');
      } finally {
        this.loadingModels = false;
      }
    },

    async runTest() {
      this.testing = true;
      this.testError = '';
      this.result = null;
      try {
        const data = await this.$api.testReplyAIModel({
          ...this.probePayload(),
          model: this.cfg.model,
          sample_text: this.sample,
          expected_intent: this.expectedIntent,
        });
        this.result = data;
      } catch (e) {
        this.testError = this.errorMessage(e, 'settings.inboundReplies.testFailed');
      } finally {
        this.testing = false;
      }
    },

    useSuggestedBaseUrl() {
      this.cfg.base_url = this.suggestedBaseUrl;
      this.resetGateway();
    },

    errorMessage(e, fallbackKey) {
      const msg = e && e.response && e.response.data && e.response.data.message;
      return msg || this.$t(fallbackKey);
    },

    resultType(status) {
      if (status === 'success') return 'is-success';
      if (status === 'failed') return 'is-danger';
      return 'is-warning';
    },

    stepIcon(status) {
      if (status === 'success') return 'check-circle-outline';
      if (status === 'failed') return 'close-circle-outline';
      return 'alert-circle-outline';
    },

    iconType(status) {
      if (status === 'success') return 'is-success';
      if (status === 'failed') return 'is-danger';
      return 'is-warning';
    },

    reasonLabel(reason) {
      const key = `settings.inboundReplies.testReason.${reason}`;
      return this.$te(key) ? this.$t(key) : reason;
    },

    intentLabel(intent) {
      const key = `settings.inboundReplies.intent.${intent}`;
      return this.$te(key) ? this.$t(key) : intent;
    },
  },

  watch: {
    'data.reply_ai.enabled': function watchEnabled(enabled) {
      if (enabled && !this.verified) {
        this.$utils.toast(this.$t('settings.inboundReplies.enableWithoutTest'), 'is-warning');
      }
    },
  },
});
</script>

<style scoped>
.reply-ai-response {
  max-height: 200px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-word;
  background: #f5f5f5;
  padding: 10px;
  border-radius: 4px;
}
</style>
