<template>
  <div>
    <div class="columns mb-6">
      <div class="column is-3">
        <b-field :label="$t('settings.bounces.enable')" data-cy="btn-enable-bounce">
          <b-switch v-model="data['bounce.enabled']" name="bounce.enabled" />
        </b-field>
      </div>
      <div class="column">
        <div v-for="typ in bounceTypes" :key="typ" class="columns">
          <div class="column is-2" :class="{ disabled: !data['bounce.enabled'] }" :label="$t('settings.bounces.count')"
            label-position="on-border">
            {{ $t(`bounces.${typ}`) }}
          </div>
          <div class="column is-4" :class="{ disabled: !data['bounce.enabled'] }">
            <b-field :label="$t('settings.bounces.count')" label-position="on-border"
              :message="$t('settings.bounces.countHelp')" data-cy="btn-bounce-count">
              <b-numberinput v-model="data['bounce.actions'][typ]['count']" name="bounce.count" type="is-light"
                controls-position="compact" placeholder="3" min="1" max="1000" />
            </b-field>
          </div>
          <div class="column is-4" :class="{ disabled: !data['bounce.enabled'] }">
            <b-field :label="$t('settings.bounces.action')" label-position="on-border">
              <b-select name="bounce.action" v-model="data['bounce.actions'][typ]['action']" expanded>
                <option value="none">
                  {{ $t('globals.terms.none') }}
                </option>
                <option value="unsubscribe">
                  {{ $t('email.unsub') }}
                </option>
                <option value="blocklist">
                  {{ $t('settings.bounces.blocklist') }}
                </option>
                <option value="delete">
                  {{ $t('globals.buttons.delete') }}
                </option>
              </b-select>
            </b-field>
          </div>
        </div>
      </div>
    </div><!-- columns -->

    <div class="mb-6">
      <b-field :label="$t('settings.bounces.enableWebhooks')" data-cy="btn-enable-bounce-webhook">
        <b-switch v-model="data['bounce.webhooks_enabled']" :disabled="!data['bounce.enabled']" name="webhooks_enabled"
          :native-value="true" data-cy="btn-enable-bounce-webhook" />
      </b-field>
      <div class="box" v-if="data['bounce.webhooks_enabled']">
        <div class="columns">
          <div class="column">
            <b-field :label="$t('settings.bounces.enableSES')">
              <b-switch v-model="data['bounce.ses_enabled']" name="ses_enabled" :native-value="true"
                data-cy="btn-enable-bounce-ses" />
            </b-field>
          </div>
        </div>
        <div class="columns">
          <div class="column is-3">
            <b-field :label="$t('settings.bounces.enableSendgrid')">
              <b-switch v-model="data['bounce.sendgrid_enabled']" name="sendgrid_enabled" :native-value="true"
                data-cy="btn-enable-bounce-sendgrid" />
            </b-field>
          </div>
          <div class="column">
            <b-field :label="$t('settings.bounces.sendgridKey')" :message="$t('globals.messages.passwordChange')">
              <b-input v-model="data['bounce.sendgrid_key']" type="password"
                :disabled="!data['bounce.sendgrid_enabled']" name="sendgrid_enabled" :native-value="true"
                data-cy="btn-enable-bounce-sendgrid" />
            </b-field>
          </div>
        </div>
        <div class="columns">
          <div class="column is-3">
            <b-field :label="$t('settings.bounces.enablePostmark')">
              <b-switch v-model="data['bounce.postmark'].enabled" name="postmark_enabled" :native-value="true"
                data-cy="btn-enable-bounce-postmark" />
            </b-field>
          </div>
          <div class="column">
            <b-field :label="$t('settings.bounces.postmarkUsername')"
              :message="$t('settings.bounces.postmarkUsernameHelp')">
              <b-input v-model="data['bounce.postmark'].username" type="text"
                :disabled="!data['bounce.postmark'].enabled" name="postmark_username"
                data-cy="btn-enable-bounce-postmark" />
            </b-field>
          </div>
          <div class="column">
            <b-field :label="$t('settings.bounces.postmarkPassword')" :message="$t('globals.messages.passwordChange')">
              <b-input v-model="data['bounce.postmark'].password" type="password"
                :disabled="!data['bounce.postmark'].enabled" name="postmark_password"
                data-cy="btn-enable-bounce-postmark" />
            </b-field>
          </div>
        </div>
        <div class="columns">
          <div class="column is-3">
            <b-field :label="$t('settings.bounces.enableForwardemail')">
              <b-switch v-model="data['bounce.forwardemail'].enabled" name="forwardemail_enabled" :native-value="true"
                data-cy="btn-enable-bounce-forwardemail" />
            </b-field>
          </div>
          <div class="column">
            <b-field :label="$t('settings.bounces.forwardemailKey')" :message="$t('globals.messages.passwordChange')">
              <b-input v-model="data['bounce.forwardemail'].key" type="password"
                :disabled="!data['bounce.forwardemail'].enabled" name="forwardemail_enabled" :native-value="true"
                data-cy="btn-enable-bounce-forwardemail" />
            </b-field>
          </div>
        </div>
      </div>
    </div>

    <!-- bounce mailbox -->
    <b-field :label="$t('settings.bounces.enableMailbox')">
      <b-switch v-if="data['bounce.mailboxes']" v-model="data['bounce.mailboxes'][0].enabled"
        :disabled="!data['bounce.enabled']" name="enabled" :native-value="true" data-cy="btn-enable-bounce-mailbox" />
    </b-field>

    <template v-if="data['bounce.enabled'] && data['bounce.mailboxes'][0].enabled">
      <div class="block box" v-for="(item, n) in data['bounce.mailboxes']" :key="n">
        <div class="columns">
          <div class="column" :class="{ disabled: !item.enabled }">
            <div class="columns">
              <div class="column is-3">
                <b-field :label="$t('settings.bounces.type')" label-position="on-border">
                  <b-select v-model="item.type" name="type">
                    <option value="pop">
                      POP
                    </option>
                  </b-select>
                </b-field>
              </div>
              <div class="column is-6">
                <b-field :label="$t('settings.mailserver.host')" label-position="on-border"
                  :message="$t('settings.mailserver.hostHelp')">
                  <b-input v-model="item.host" name="host" placeholder="bounce.yourmailserver.net" :maxlength="200" />
                </b-field>
              </div>
              <div class="column is-3">
                <b-field :label="$t('settings.mailserver.port')" label-position="on-border"
                  :message="$t('settings.mailserver.portHelp')">
                  <b-numberinput v-model="item.port" name="port" type="is-light" controls-position="compact"
                    placeholder="25" min="1" max="65535" />
                </b-field>
              </div>
            </div><!-- host -->

            <div class="columns">
              <div class="column is-3">
                <b-field :label="$t('settings.mailserver.authProtocol')" label-position="on-border">
                  <b-select v-model="item.auth_protocol" name="auth_protocol">
                    <option value="none">
                      none
                    </option>
                    <option v-if="item.type === 'pop'" value="userpass">
                      userpass
                    </option>
                    <template v-else>
                      <option value="cram">
                        cram
                      </option>
                      <option value="plain">
                        plain
                      </option>
                      <option value="login">
                        login
                      </option>
                    </template>
                  </b-select>
                </b-field>
              </div>
              <div class="column">
                <b-field grouped>
                  <b-field :label="$t('settings.mailserver.username')" label-position="on-border" expanded>
                    <b-input v-model="item.username" :disabled="item.auth_protocol === 'none'" name="username"
                      placeholder="mysmtp" :maxlength="200" />
                  </b-field>
                  <b-field :label="$t('settings.mailserver.password')" label-position="on-border" expanded
                    :message="$t('settings.mailserver.passwordHelp')">
                    <b-input v-model="item.password" :disabled="item.auth_protocol === 'none'" name="password"
                      type="password" :placeholder="$t('settings.mailserver.passwordHelp')" :maxlength="200" />
                  </b-field>
                </b-field>
              </div>
            </div><!-- auth -->

            <div class="columns">
              <div class="column is-6">
                <b-field grouped>
                  <b-field :label="$t('settings.mailserver.tls')" expanded :message="$t('settings.mailserver.tlsHelp')">
                    <b-select :value="item.starttls ? 'starttls' : (item.tls_enabled ? 'tls' : 'none')" @input="setTLS(item, $event)">
                      <option value="none">POP3</option>
                      <option value="tls">SSL/TLS</option>
                      <option value="starttls">STARTTLS</option>
                    </b-select>
                  </b-field>
                  <b-field :label="$t('settings.mailserver.skipTLS')" expanded
                    :message="$t('settings.mailserver.skipTLSHelp')">
                    <b-switch v-model="item.tls_skip_verify" :disabled="!item.tls_enabled && !item.starttls"
                      name="item.tls_skip_verify" />
                  </b-field>
                </b-field>
              </div>
              <div class="column" />
              <div class="column is-4">
                <b-field :label="$t('settings.bounces.scanInterval')" expanded label-position="on-border"
                  :message="$t('settings.bounces.scanIntervalHelp')">
                  <b-input v-model="item.scan_interval" name="scan_interval" placeholder="15m" :pattern="regDuration"
                    :maxlength="10" />
                </b-field>
              </div>
            </div><!-- TLS -->
            <b-button type="is-info" :loading="testing === n" :disabled="testing !== null || !item.host || !item.port"
              data-cy="test-bounce-mailbox" @click="testMailbox(item, n)">
{{ $t('settings.bounces.testMailbox') }}
</b-button>
            <p class="help">{{ $t('settings.bounces.testHelp') }}</p>
            <div v-if="testResults[n]" class="box mt-3" data-cy="bounce-test-result" aria-live="polite">
              <b-notification :type="resultType(testResults[n].status)" :closable="false">
                {{ $t(`settings.bounces.testStatus.${testResults[n].status}`) }}
                <p v-if="testResults[n].error">{{ testResults[n].error }}</p>
              </b-notification>
              <p v-for="step in testResults[n].steps" :key="step.name" class="mb-2">
                <strong>{{ $t(`settings.bounces.testStep.${step.name}`) }}</strong>：
                {{ $t(`settings.bounces.testStatus.${step.status}`) }}
                <span v-if="step.detail"> — {{ step.detail }}</span>
              </p>
              <p>{{ $t('settings.bounces.testCount') }}：{{ testResults[n].count }}</p>
              <template v-if="testResults[n].message">
                <p>{{ $t('settings.bounces.testFrom') }}：{{ testResults[n].message.from || '—' }}</p>
                <p>{{ $t('settings.bounces.testSubject') }}：{{ testResults[n].message.subject || '—' }}</p>
                <p>
{{ $t('settings.bounces.testDate') }}：{{ testResults[n].message.date || '—' }}
                  <small v-if="testResults[n].message.dateSource === 'date'">({{ $t('settings.bounces.testDateFallback') }})</small>
                </p>
                <p v-if="testResults[n].message.bounceType">{{ $t('settings.bounces.testType') }}：{{ testResults[n].message.bounceType }}</p>
                <p v-if="testResults[n].message.reason">{{ $t('settings.bounces.testReason') }}：{{ testResults[n].message.reason }}</p>
                <b-table :data="testResults[n].message.recipients" :mobile-cards="true">
                  <b-table-column v-slot="props" :label="$t('settings.bounces.testRecipient')">{{ props.row.address }}</b-table-column>
                  <b-table-column v-slot="props" :label="$t('settings.bounces.testSource')">{{ props.row.source }}</b-table-column>
                  <b-table-column v-slot="props" label="SMTP">{{ props.row.status || '—' }}</b-table-column>
                  <b-table-column v-slot="props" :label="$t('settings.bounces.testReason')">{{ props.row.reason || '—' }}</b-table-column>
                </b-table>
              </template>
            </div>
          </div>
        </div><!-- second container column -->
      </div><!-- block -->
    </template>
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
      bounceTypes: ['soft', 'hard', 'complaint'],
      data: this.form,
      regDuration,
      testing: null,
      testResults: {},
    };
  },

  methods: {
    removeBounceBox(i) {
      this.data['bounce.mailboxes'].splice(i, 1);
    },
    async testMailbox(item, index) {
      this.testing = index;
      this.$delete(this.testResults, index);
      try {
        const data = await this.$api.testBounceMailbox({ ...item });
        this.$set(this.testResults, index, data);
      } catch (e) {
        this.$set(this.testResults, index, {
          status: 'failed', error: e.response?.data?.message || e.message, steps: [], count: '—',
        });
      } finally {
        this.testing = null;
      }
    },
    setTLS(item, mode) {
      this.$set(item, 'starttls', mode === 'starttls');
      this.$set(item, 'tls_enabled', mode === 'tls');
    },
    resultType(status) {
      if (status === 'success') return 'is-success';
      if (status === 'failed') return 'is-danger';
      return 'is-warning';
    },
  },
});
</script>
