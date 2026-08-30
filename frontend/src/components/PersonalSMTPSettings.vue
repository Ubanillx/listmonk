<template>
  <section class="personal-smtp mt-6">
    <div class="personal-smtp-header level mb-5">
      <div>
        <h2 class="title is-5 mb-1">{{ $t('settings.personalSMTP.title') }}</h2>
        <p class="help">{{ $t('settings.personalSMTP.help') }}</p>
      </div>
      <b-button type="is-primary" icon-left="plus" class="add-smtp-button" @click="addServer">
        {{ $t('globals.buttons.addNew') }}
      </b-button>
    </div>

    <div v-if="servers.length === 0" class="notification is-light smtp-empty-state">
      <b-icon icon="email-off-outline" size="is-small" />
      <span>{{ $t('settings.personalSMTP.empty') }}</span>
    </div>

    <div v-for="(server, index) in servers" :key="server.id || `new-${index}`" class="box smtp-card">
      <div class="smtp-card-header">
        <div>
          <div class="smtp-card-title">
            {{ server.name || `${$t('settings.smtp.name')} #${index + 1}` }}
            <b-tag v-if="server.id" :type="server.enabled ? 'is-success' : 'is-light'" rounded size="is-small">
              {{ server.enabled ? $t('globals.states.on') : $t('globals.states.off') }}
            </b-tag>
          </div>
          <p class="smtp-card-subtitle">
            {{ server.host || $t('settings.personalSMTP.notConfigured') }}
          </p>
        </div>
        <div class="smtp-card-actions">
          <b-field :label="$t('globals.buttons.enabled')" class="smtp-enabled-field">
            <b-switch v-model="server.enabled" />
          </b-field>
          <b-tag v-if="server.id" type="is-info" rounded>
            {{ $t('settings.personalSMTP.sentToday', { count: server.sentToday || 0 }) }}
          </b-tag>
          <b-button type="is-danger" outlined icon-left="trash-can-outline" @click="removeServer(index)">
            {{ $t('globals.buttons.delete') }}
          </b-button>
        </div>
      </div>

      <div class="smtp-card-body">
        <div class="smtp-section">
          <div class="smtp-section-heading">
            <div>
              <h3 class="smtp-section-title">{{ $t('settings.personalSMTP.connectionSection') }}</h3>
              <p class="smtp-section-help">{{ $t('settings.personalSMTP.connectionHelp') }}</p>
            </div>
          </div>

          <div class="columns is-multiline smtp-grid">
            <div class="column is-8">
              <b-field :label="$t('globals.fields.name')" label-position="on-border">
                <b-input v-model="server.name" maxlength="100" :placeholder="$t('globals.fields.name')" />
              </b-field>
            </div>
            <div class="column is-8">
              <b-field :label="$t('settings.mailserver.host')" label-position="on-border">
                <b-input v-model="server.host" required maxlength="200" placeholder="smtp.example.com" />
              </b-field>
            </div>
            <div class="column is-4">
              <b-field :label="$t('settings.mailserver.port')" label-position="on-border">
                <b-numberinput v-model="server.port" min="1" max="65535" controls-position="compact" placeholder="465" />
              </b-field>
            </div>
            <div class="column is-3">
              <b-field :label="$t('settings.mailserver.authProtocol')" label-position="on-border">
                <b-select v-model="server.authProtocol" expanded>
                  <option value="login">LOGIN</option>
                  <option value="cram">CRAM</option>
                  <option value="plain">PLAIN</option>
                  <option value="none">{{ $t('globals.states.off') }}</option>
                </b-select>
              </b-field>
            </div>
            <div class="column is-5">
              <b-field :label="$t('settings.mailserver.username')" label-position="on-border">
                <b-input v-model="server.username" :disabled="server.authProtocol === 'none'" maxlength="200"
                  placeholder="user@example.com" />
              </b-field>
            </div>
            <div class="column is-4">
              <b-field :label="$t('settings.mailserver.password')" label-position="on-border">
                <b-input v-model="server.password" type="password" :disabled="server.authProtocol === 'none'"
                  maxlength="200" placeholder="••••••••" />
              </b-field>
            </div>
            <div class="column is-8">
              <b-field :label="$t('settings.smtp.fromEmail')" label-position="on-border">
                <b-input v-model="server.fromEmail" maxlength="200" placeholder="sender@example.com" />
              </b-field>
            </div>
            <div class="column is-4">
              <b-field :label="$t('settings.smtp.dailyLimit')" :message="$t('settings.smtp.dailyLimitHelp')"
                label-position="on-border">
                <b-numberinput v-model="server.dailyLimit" min="0" max="100000000" controls-position="compact"
                  placeholder="0" />
              </b-field>
            </div>
          </div>
        </div>

        <div class="smtp-section smtp-advanced-section">
          <div class="smtp-section-heading smtp-advanced-heading">
            <div>
              <h3 class="smtp-section-title">{{ $t('settings.personalSMTP.advancedSection') }}</h3>
              <p class="smtp-section-help">{{ $t('settings.personalSMTP.advancedHelp') }}</p>
            </div>
            <b-button type="is-text" size="is-small" :icon-left="server.showAdvanced ? 'chevron-up' : 'chevron-down'"
              :aria-expanded="server.showAdvanced ? 'true' : 'false'" @click="server.showAdvanced = !server.showAdvanced">
              {{ server.showAdvanced ? $t('settings.personalSMTP.advancedHide') : $t('settings.personalSMTP.advancedShow') }}
            </b-button>
          </div>

          <div v-if="server.showAdvanced" class="columns is-multiline smtp-grid smtp-advanced-content">
            <div class="column is-4">
              <b-field :label="$t('settings.mailserver.tls')" label-position="on-border">
                <b-select v-model="server.tlsType" expanded>
                  <option value="none">{{ $t('globals.states.off') }}</option>
                  <option value="STARTTLS">STARTTLS</option>
                  <option value="TLS">SSL/TLS</option>
                </b-select>
              </b-field>
            </div>
            <div class="column is-4">
              <b-field :label="$t('settings.mailserver.skipTLS')" :message="$t('settings.mailserver.skipTLSHelp')">
                <b-switch v-model="server.tlsSkipVerify" :disabled="server.tlsType === 'none'" />
              </b-field>
            </div>
            <div class="column is-4">
              <b-field :label="$t('settings.mailserver.maxConns')" label-position="on-border">
                <b-numberinput v-model="server.maxConns" min="1" max="65535" controls-position="compact" />
              </b-field>
            </div>
            <div class="column is-4">
              <b-field :label="$t('settings.smtp.retries')" label-position="on-border">
                <b-numberinput v-model="server.maxMsgRetries" min="1" max="1000" controls-position="compact" />
              </b-field>
            </div>
            <div class="column is-4">
              <b-field :label="$t('settings.smtp.heloHost')" :message="$t('settings.smtp.heloHostHelp')"
                label-position="on-border">
                <b-input v-model="server.helloHostname" maxlength="200" placeholder="mail.example.com" />
              </b-field>
            </div>
            <div class="column is-4">
              <b-field :label="$t('settings.mailserver.idleTimeout')" :message="$t('settings.mailserver.idleTimeoutHelp')"
                label-position="on-border">
                <b-input v-model="server.idleTimeout" placeholder="15s" maxlength="10" />
              </b-field>
            </div>
            <div class="column is-4">
              <b-field :label="$t('settings.mailserver.waitTimeout')" :message="$t('settings.mailserver.waitTimeoutHelp')"
                label-position="on-border">
                <b-input v-model="server.waitTimeout" placeholder="5s" maxlength="10" />
              </b-field>
            </div>
            <div class="column is-8">
              <p v-if="server.emailHeaders.length === 0 && !server.showHeaders">
                <a href="#" @click.prevent="server.showHeaders = true">
                  <b-icon icon="plus" />{{ $t('settings.smtp.setCustomHeaders') }}
                </a>
              </p>
              <b-field v-if="server.emailHeaders.length > 0 || server.showHeaders"
                :message="$t('settings.smtp.customHeadersHelp')" label-position="on-border">
                <b-input v-model="server.emailHeadersStr" type="textarea"
                  placeholder="[{&quot;X-Custom&quot;: &quot;value&quot;}, {&quot;X-Custom2&quot;: &quot;value&quot;}]" />
              </b-field>
            </div>
          </div>
        </div>

        <div class="smtp-card-footer">
          <div>
            <h3 class="smtp-section-title">{{ $t('settings.personalSMTP.testSection') }}</h3>
            <p class="smtp-section-help">{{ $t('settings.personalSMTP.testHelp') }}</p>
          </div>
          <div class="smtp-test-controls">
            <b-field :label="$t('settings.personalSMTP.testRecipient')" label-position="on-border">
              <b-input v-model="server.testEmail" type="email" placeholder="email@example.com" />
            </b-field>
            <b-button type="is-light" icon-left="rocket-launch-outline" :loading="testing === index"
              @click="testServer(server, index)">
              {{ $t('settings.smtp.testConnection') }}
            </b-button>
          </div>
        </div>
      </div>
    </div>

    <div v-if="servers.length" class="smtp-save-bar">
      <p class="help">{{ $t('settings.personalSMTP.saveHelp') }}</p>
      <b-button type="is-primary" icon-left="content-save-outline" :loading="saving" @click="save">
        {{ $t('globals.buttons.save') }}
      </b-button>
    </div>
  </section>
</template>

<script>
import Vue from 'vue';

function blankServer() {
  return {
    id: 0,
    uuid: '',
    name: '',
    enabled: true,
    fromEmail: '',
    dailyLimit: 0,
    host: '',
    helloHostname: '',
    port: 465,
    authProtocol: 'plain',
    username: '',
    password: '',
    emailHeaders: [],
    maxConns: 10,
    maxMsgRetries: 2,
    idleTimeout: '15s',
    waitTimeout: '5s',
    tlsType: 'TLS',
    tlsSkipVerify: false,
    emailHeadersStr: '[]',
    showHeaders: false,
    sentToday: 0,
    testEmail: '',
    showAdvanced: false,
  };
}

export default Vue.extend({
  data() {
    return {
      servers: [],
      saving: false,
      testing: null,
    };
  },

  methods: {
    load() {
      this.$api.getPersonalSMTP().then((data) => {
        const rows = Array.isArray(data) ? data : data.smtp;
        this.servers = (rows || []).map((row) => ({
          ...blankServer(),
          ...row,
          emailHeaders: row.emailHeaders || [],
          emailHeadersStr: JSON.stringify(row.emailHeaders || [], null, 4),
        }));
      });
    },

    addServer() {
      this.servers.push(blankServer());
    },

    removeServer(index) {
      this.$utils.confirm(null, () => {
        const server = this.servers[index];
        if (!server.id) {
          this.servers.splice(index, 1);
          return;
        }
        this.$api.deletePersonalSMTP(server.id).then((data) => {
          this.servers.splice(index, 1);
          if (data && data.runningCampaigns) {
            this.$utils.toast(this.$t('settings.personalSMTP.runningWarning'), 'is-warning');
          }
        });
      });
    },

    wireServer(server) {
      return {
        id: server.id || 0,
        uuid: server.uuid || '',
        name: server.name,
        enabled: server.enabled,
        from_email: server.fromEmail,
        daily_limit: Number(server.dailyLimit) || 0,
        host: server.host,
        hello_hostname: server.helloHostname,
        port: Number(server.port) || 465,
        auth_protocol: server.authProtocol,
        username: server.username,
        password: server.password,
        email_headers: server.emailHeaders || [],
        max_conns: Number(server.maxConns) || 10,
        max_msg_retries: Number(server.maxMsgRetries) || 2,
        idle_timeout: server.idleTimeout || '15s',
        wait_timeout: server.waitTimeout || '5s',
        tls_type: server.tlsType || 'TLS',
        tls_skip_verify: !!server.tlsSkipVerify,
      };
    },

    save() {
      const invalidHeaders = this.servers.some((server) => {
        if (server.emailHeadersStr && server.emailHeadersStr !== '[]') {
          try {
            const headers = JSON.parse(server.emailHeadersStr);
            if (!Array.isArray(headers)) {
              throw new Error('custom headers must be an array');
            }
            this.$set(server, 'emailHeaders', headers);
          } catch (e) {
            this.$utils.toast(e.toString(), 'is-danger');
            return true;
          }
        } else {
          this.$set(server, 'emailHeaders', []);
          this.$set(server, 'emailHeadersStr', '[]');
        }
        return false;
      });
      if (invalidHeaders) return;
      this.saving = true;
      this.$api.updatePersonalSMTP({ smtp: this.servers.map(this.wireServer) }).then((data) => {
        const rows = Array.isArray(data) ? data : data.smtp;
        this.servers = (rows || []).map((row) => ({
          ...blankServer(),
          ...row,
          emailHeaders: row.emailHeaders || [],
          emailHeadersStr: JSON.stringify(row.emailHeaders || [], null, 4),
        }));
        this.$utils.toast(this.$t('globals.messages.updated', { name: this.$t('settings.personalSMTP.title') }));
        if (data && data.runningCampaigns) {
          this.$utils.toast(this.$t('settings.personalSMTP.runningWarning'), 'is-warning');
        }
      }).finally(() => {
        this.saving = false;
      });
    },

    testServer(server, index) {
      if (!server.testEmail) {
        this.$utils.toast(this.$t('settings.personalSMTP.testRecipient'), 'is-danger');
        return;
      }
      this.testing = index;
      this.$api.testPersonalSMTP({ ...this.wireServer(server), id: server.id || 0, email: server.testEmail }).then(() => {
        this.$utils.toast(this.$t('campaigns.testSent'));
      }).finally(() => {
        this.testing = null;
      });
    },
  },

  mounted() {
    this.load();
  },

});
</script>

<style lang="scss" scoped>
.personal-smtp {
  width: 100%;
  max-width: 1400px;
}

.personal-smtp-header {
  align-items: flex-end;
  gap: 1rem;

  .help {
    max-width: 780px;
    margin-top: .25rem;
  }
}

.add-smtp-button {
  flex: 0 0 auto;
}

.smtp-empty-state {
  display: flex;
  align-items: center;
  gap: .65rem;
  max-width: 760px;
  border: 1px dashed #d9e0ea;
  background: #f8fafc;
  color: #5b6575;
}

.smtp-card {
  padding: 0;
  overflow: hidden;
  border: 1px solid #e5e9f0;
  border-radius: 10px;
  box-shadow: 0 2px 8px rgba(29, 41, 57, .05);

  & + .smtp-card {
    margin-top: 1.25rem;
  }
}

.smtp-card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  padding: 1.1rem 1.25rem;
  background: #f8fafc;
  border-bottom: 1px solid #edf0f4;
}

.smtp-card-title {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: .5rem;
  font-size: 1.05rem;
  font-weight: 600;
  color: #273142;
}

.smtp-card-subtitle {
  margin-top: .25rem;
  color: #718096;
  font-size: .85rem;
  word-break: break-all;
}

.smtp-card-actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: .75rem;

  .field {
    margin-bottom: 0;
  }
}

.smtp-enabled-field {
  display: flex;
  align-items: center;
  gap: .4rem;

  .label {
    margin-bottom: 0;
    color: #5b6575;
    font-size: .8rem;
    font-weight: 500;
  }
}

.smtp-card-body {
  padding: 1.25rem;
}

.smtp-section {
  padding: 1rem;
  margin-bottom: 1rem;
  border: 1px solid #edf0f4;
  border-radius: 8px;
  background: #fff;

  &:last-child {
    margin-bottom: 0;
  }
}

.smtp-section-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
  margin-bottom: .85rem;
}

.smtp-section-title {
  margin-bottom: .2rem;
  color: #273142;
  font-size: .95rem;
  font-weight: 600;
}

.smtp-section-help {
  margin: 0;
  color: #718096;
  font-size: .8rem;
  line-height: 1.45;
}

.smtp-advanced-section {
  background: #fbfcfe;
}

.smtp-advanced-heading {
  align-items: center;
  margin-bottom: 0;
}

.smtp-advanced-content {
  padding-top: .85rem;
  margin-top: .85rem;
  border-top: 1px solid #edf0f4;
}

.smtp-grid {
  margin: -.35rem;

  > .column {
    padding: .35rem;
  }

  .field {
    margin-bottom: .35rem;
  }
}

.smtp-card-footer {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 1rem;
  padding: 1rem 0 0;
  margin-top: 1rem;
  border-top: 1px solid #edf0f4;
}

.smtp-test-controls {
  display: flex;
  align-items: flex-end;
  gap: .75rem;
  min-width: min(100%, 520px);

  .field {
    flex: 1 1 280px;
    margin-bottom: 0;
  }

  .button {
    flex: 0 0 auto;
  }
}

.smtp-save-bar {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 1rem;
  padding: 1rem 0 0;

  .help {
    margin: 0 auto 0 0;
    color: #718096;
  }
}

@media (max-width: 768px) {
  .personal-smtp-header,
  .smtp-card-header,
  .smtp-card-footer,
  .smtp-test-controls,
  .smtp-save-bar {
    display: block;
  }

  .personal-smtp-header .add-smtp-button {
    width: 100%;
    margin-top: .85rem;
  }

  .smtp-card-actions {
    justify-content: flex-start;
    margin-top: .85rem;
  }

  .smtp-card-footer {
    .smtp-test-controls {
      margin-top: .85rem;
    }

    .smtp-test-controls .button {
      width: 100%;
      margin-top: .5rem;
    }
  }

  .smtp-save-bar {
    .help {
      margin-bottom: .75rem;
    }

    .button {
      width: 100%;
    }
  }
}
</style>
