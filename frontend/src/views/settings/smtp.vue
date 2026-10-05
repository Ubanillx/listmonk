<template>
  <div>
    <div class="items mail-servers">
      <div class="block box" v-for="(item, n) in form.smtp" :key="n">
        <h2 class="is-size-5 mb-2">{{ $t('settings.smtp.systemTitle') }}</h2>
        <p class="help mb-5">{{ $t('settings.smtp.systemHelp') }}</p>
        <div class="columns">
          <div class="column">
            <div class="columns">
              <div class="column is-9">
                <b-field :label="$t('settings.mailserver.host')" label-position="on-border"
                  :message="$t('settings.mailserver.hostHelp')">
                  <b-input v-model="item.host" name="host" placeholder="smtp.yourmailserver.net" :maxlength="200" />
                </b-field>
              </div>
              <div class="column">
                <b-field :label="$t('settings.mailserver.port')" label-position="on-border"
                  :message="$t('settings.mailserver.portHelp')">
                  <b-numberinput v-model="item.port" name="port" type="is-light" controls-position="compact"
                    placeholder="25" min="1" max="65535" />
                </b-field>
              </div>
            </div><!-- host -->

            <div class="columns" data-cy="system-smtp-tls">
              <div class="column is-6">
                <b-field :label="$t('settings.mailserver.tls')" :message="$t('settings.mailserver.tlsHelp')">
                  <b-select v-model="item.tls_type" expanded data-cy="system-smtp-tls-type">
                    <option value="none">{{ $t('globals.states.off') }}</option>
                    <option value="STARTTLS">STARTTLS</option>
                    <option value="TLS">SSL/TLS</option>
                  </b-select>
                </b-field>
              </div>
              <div class="column is-6">
                <b-field :label="$t('settings.mailserver.skipTLS')" :message="$t('settings.mailserver.skipTLSHelp')">
                  <b-switch v-model="item.tls_skip_verify" :disabled="item.tls_type === 'none'" />
                </b-field>
              </div>
            </div>

            <div class="columns">
              <div class="column is-2">
                <b-field :label="$t('settings.mailserver.authProtocol')" label-position="on-border">
                  <b-select v-model="item.auth_protocol" name="auth_protocol">
                    <option value="login">
                      LOGIN
                    </option>
                    <option value="cram">
                      CRAM
                    </option>
                    <option value="plain">
                      PLAIN
                    </option>
                    <option value="none">
                      None
                    </option>
                  </b-select>
                </b-field>
              </div>
              <div class="column">
                <b-field grouped>
                  <b-field :label="$t('settings.mailserver.username')" label-position="on-border" expanded>
                    <b-input v-model="item.username" :custom-class="`smtp-username-${n}`"
                      :disabled="item.auth_protocol === 'none'" name="username" placeholder="mysmtp" :maxlength="200" />
                  </b-field>
                  <b-field :label="$t('settings.mailserver.password')" label-position="on-border" expanded
                    :message="$t('settings.mailserver.passwordHelp')">
                    <b-input v-model="item.password" :disabled="item.auth_protocol === 'none'" name="password"
                      type="password" :custom-class="`password-${n}`"
                      :placeholder="$t('settings.mailserver.passwordHelp')" :maxlength="200" />
                  </b-field>
                </b-field>
              </div>
            </div><!-- auth -->
            <div class="smtp-presets is-size-7">
              <a href="#" @click.prevent="() => fillSettings(n, 'gmail')">Gmail</a>
              <a href="#" @click.prevent="() => fillSettings(n, '263net')">263net</a>
              <a href="#" @click.prevent="() => fillSettings(n, 'topmax')">topmax</a>
              <a href="#" @click.prevent="() => fillSettings(n, 'ses')">Amazon SES</a>
              <a href="#" @click.prevent="() => fillSettings(n, 'mailgun')">Mailgun</a>
              <a href="#" @click.prevent="() => fillSettings(n, 'mailjet')">Mailjet</a>
              <a href="#" @click.prevent="() => fillSettings(n, 'sendgrid')">Sendgrid</a>
              <a href="#" @click.prevent="() => fillSettings(n, 'postmark')">Postmark</a>
              <a href="#" @click.prevent="() => fillSettings(n, 'forwardemail')">Forward Email</a>
              <a href="#" @click.prevent="() => fillSettings(n, 'lettermint')">Lettermint</a>
            </div>
            <div class="columns">
              <div class="column is-6">
                <b-field :label="$t('settings.smtp.heloHost')" label-position="on-border"
                  :message="$t('settings.smtp.heloHostHelp')">
                  <b-input v-model="item.hello_hostname" name="hello_hostname" placeholder="" :maxlength="200" />
                </b-field>
              </div>
            </div>

            <div class="columns">
              <div class="column is-6">
                <b-field :label="$t('globals.fields.name')" label-position="on-border"
                  :message="$t('settings.mailserver.nameHelp')">
                  <b-input v-model="item.name" name="name" placeholder="email-primary" :maxlength="100" />
                </b-field>
              </div>
              <div class="column is-6">
                <b-field :label="$t('settings.smtp.fromEmail')" label-position="on-border"
                  :message="$t('settings.smtp.fromEmailHelp')">
                  <b-input v-model="item.from_email" name="from_email"
                    placeholder="Your Name <noreply@yoursite.com>" :maxlength="200" />
                </b-field>
              </div>
            </div>

            <div class="columns">
              <div class="column is-4">
                <b-field :label="$t('settings.smtp.dailyLimit')" label-position="on-border"
                  :message="$t('settings.smtp.systemDailyLimitHelp')">
                  <b-numberinput v-model="item.daily_limit" name="daily_limit" type="is-light"
                    controls-position="compact" min="0" max="100000000" />
                </b-field>
              </div>
            </div>

            <hr />

            <form @submit.prevent="() => doSMTPTest(item, n)">
              <div class="columns">
                <template v-if="smtpTestItem === n">
                  <div class="column is-5">
                    <strong>{{ $t('settings.smtp.fromEmail') }}</strong>
                    <br />
                    {{ item.from_email }}
                  </div>
                  <div class="column is-4">
                    <b-field :label="$t('settings.smtp.toEmail')" label-position="on-border">
                      <b-input type="email" required v-model="testEmail" :ref="'testEmailTo'"
                        placeholder="email@site.com" :custom-class="`test-email-${n}`" />
                    </b-field>
                  </div>
                </template>
                <div class="column has-text-right">
                  <b-button v-if="smtpTestItem === n" class="is-primary" @click.prevent="() => doSMTPTest(item, n)">
                    {{ $t('settings.smtp.sendTest') }}
                  </b-button>
                  <a href="#" v-else class="is-primary" @click.prevent="showTestForm(n)">
                    <b-icon icon="rocket-launch-outline" /> {{ $t('settings.smtp.testConnection') }}
                  </a>
                </div>
                <div class="columns">
                  <div class="column" />
                </div>
              </div>
              <div v-if="errMsg && smtpTestItem === n">
                <b-field class="mt-4" type="is-danger">
                  <b-input v-model="errMsg" type="textarea" custom-class="has-text-danger is-size-6" readonly />
                </b-field>
              </div>
            </form><!-- smtp test -->
          </div>
        </div><!-- second container column -->
      </div><!-- block -->
    </div><!-- mail-servers -->

    <div class="block box" v-if="data.smtp_delivery" data-cy="smtp-delivery-settings">
      <h2 class="is-size-5 mb-2">{{ $t('settings.smtp.deliveryTitle') }}</h2>
      <p class="help mb-5">{{ $t('settings.smtp.deliveryHelp') }}</p>
      <div class="columns is-multiline">
        <div class="column is-3">
          <b-field :label="$t('settings.mailserver.maxConns')" :message="$t('settings.mailserver.maxConnsHelp')">
            <b-numberinput v-model="data.smtp_delivery.max_conns" min="1" max="65535" controls-position="compact"
              data-cy="smtp-delivery-max-conns" />
          </b-field>
        </div>
        <div class="column is-3">
          <b-field :label="$t('settings.smtp.retries')" :message="$t('settings.smtp.retriesHelp')">
            <b-numberinput v-model="data.smtp_delivery.max_msg_retries" min="1" max="1000" controls-position="compact" />
          </b-field>
        </div>
        <div class="column is-3">
          <b-field :label="$t('settings.mailserver.idleTimeout')" :message="$t('settings.mailserver.idleTimeoutHelp')">
            <b-input v-model="data.smtp_delivery.idle_timeout" placeholder="15s" :pattern="regDuration" :maxlength="10" />
          </b-field>
        </div>
        <div class="column is-3">
          <b-field :label="$t('settings.mailserver.waitTimeout')" :message="$t('settings.mailserver.waitTimeoutHelp')">
            <b-input v-model="data.smtp_delivery.wait_timeout" placeholder="5s" :pattern="regDuration" :maxlength="10" />
          </b-field>
        </div>
        <div class="column is-6">
          <b-field :label="$t('settings.smtp.sendDelayMin')" :message="$t('settings.smtp.sendDelayMinHelp')">
            <b-numberinput v-model="data.smtp_delivery.send_delay_min" min="0" max="3600000" step="1"
              controls-position="compact" data-cy="smtp-send-delay-min" required />
          </b-field>
        </div>
        <div class="column is-6">
          <b-field :label="$t('settings.smtp.sendDelayMax')" :message="$t('settings.smtp.sendDelayMaxHelp')">
            <b-numberinput v-model="data.smtp_delivery.send_delay_max" min="0" max="3600000" step="1"
              controls-position="compact" data-cy="smtp-send-delay-max" required />
          </b-field>
        </div>
        <div class="column is-12">
          <p class="help mb-3">{{ $t('settings.smtp.sendDelayHelp') }}</p>
        </div>
        <div class="column is-12">
          <b-field :label="$t('settings.smtp.setCustomHeaders')" :message="$t('settings.smtp.customHeadersHelp')">
            <b-input v-model="data.smtp_delivery.strEmailHeaders" type="textarea"
              placeholder="[{&quot;X-Custom&quot;: &quot;value&quot;}]" />
          </b-field>
        </div>
      </div>
    </div>
  </div>
</template>

<script>
import Vue from 'vue';
import { regDuration } from '../../constants';

const smtpTemplates = {
  '263net': {
    host: 'smtp.263.net', port: 465, auth_protocol: 'login', tls_type: 'TLS',
  },
  topmax: {
    host: 'smtp.topmax.cn', port: 465, auth_protocol: 'login', tls_type: 'TLS',
  },
  gmail: {
    host: 'smtp.gmail.com', port: 465, auth_protocol: 'login',
  },
  ses: {
    host: 'email-smtp.YOUR-REGION.amazonaws.com', port: 465, auth_protocol: 'login',
  },
  mailjet: {
    host: 'in-v3.mailjet.com', port: 465, auth_protocol: 'cram',
  },
  mailgun: {
    host: 'smtp.mailgun.org', port: 465, auth_protocol: 'login',
  },
  sendgrid: {
    host: 'smtp.sendgrid.net', port: 465, auth_protocol: 'login',
  },
  forwardemail: {
    host: 'smtp.forwardemail.net', port: 465, auth_protocol: 'login',
  },
  postmark: {
    host: 'smtp.postmarkapp.com', port: 587, auth_protocol: 'cram',
  },
  lettermint: {
    host: 'smtp.lettermint.co', port: 465, auth_protocol: 'login',
  },
};

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
      // Index of the SMTP block item in the array to show the
      // test form in.
      smtpTestItem: null,
      testEmail: '',
      errMsg: '',
    };
  },

  methods: {
    doSMTPTest(item, n) {
      if (!this.isTestEnabled(item)) {
        this.$utils.toast(this.$t('settings.smtp.testEnterEmail'), 'is-danger');
        this.$nextTick(() => {
          const i = document.querySelector(`.password-${n}`);
          this.data.smtp[n].password = '';
          i.focus();
          i.select();
        });
        return;
      }

      this.errMsg = '';
      this.$api.testSMTP({
        ...item,
        ...this.data.smtp_delivery,
        email_headers: JSON.parse(this.data.smtp_delivery.strEmailHeaders || '[]'),
        email: this.testEmail,
      }).then(() => {
        this.$utils.toast(this.$t('campaigns.testSent'));
      }).catch((err) => {
        if (err.response?.data?.message) {
          this.errMsg = err.response.data.message;
        }
      });
    },

    showTestForm(n) {
      this.smtpTestItem = n;
      this.testItem = this.form.smtp[n];
      this.errMsg = '';

      this.$nextTick(() => {
        document.querySelector(`.test-email-${n}`).focus();
      });
    },

    isTestEnabled(item) {
      if (!item.host || !item.port) {
        return false;
      }
      if (item.auth_protocol !== 'none' && item.password.includes('•')) {
        return false;
      }

      return true;
    },

    fillSettings(n, key) {
      this.data.smtp.splice(n, 1, {
        ...this.data.smtp[n],
        ...smtpTemplates[key],
        tls_type: smtpTemplates[key].port === 587 ? 'STARTTLS' : 'TLS',
        username: '',
        password: '',
        hello_hostname: '',
      });

      this.$nextTick(() => {
        document.querySelector(`.smtp-username-${n}`).focus();
      });
    },

  },
});
</script>

<style lang="scss" scoped>
.smtp-presets {
  display: flex;
  flex-wrap: wrap;
  gap: .5rem 1rem;
  margin-bottom: 1.25rem;

  & + .columns {
    margin-top: 0;
  }
}
</style>
