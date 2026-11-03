<template>
  <div class="smtp-presets is-size-7" data-cy="smtp-presets">
    <button v-for="preset in presets" :key="preset.label" type="button" class="button is-text is-small"
      @click="selectPreset(preset)">
      {{ preset.label }}
    </button>
  </div>
</template>

<script>
import Vue from 'vue';

export default Vue.extend({
  data() {
    return {
      presets: [
        {
          label: 'Gmail', host: 'smtp.gmail.com', port: 465, authProtocol: 'login',
        },
        {
          label: '263net', host: 'smtp.263.net', port: 465, authProtocol: 'login',
        },
        {
          label: 'topmax', host: 'smtp.topmax.cn', port: 465, authProtocol: 'login',
        },
        {
          label: 'Amazon SES', host: 'email-smtp.YOUR-REGION.amazonaws.com', port: 465, authProtocol: 'login',
        },
        {
          label: 'Mailgun', host: 'smtp.mailgun.org', port: 465, authProtocol: 'login',
        },
        {
          label: 'Mailjet', host: 'in-v3.mailjet.com', port: 465, authProtocol: 'cram',
        },
        {
          label: 'Sendgrid', host: 'smtp.sendgrid.net', port: 465, authProtocol: 'login',
        },
        {
          label: 'Postmark', host: 'smtp.postmarkapp.com', port: 587, authProtocol: 'cram',
        },
        {
          label: 'Forward Email', host: 'smtp.forwardemail.net', port: 465, authProtocol: 'login',
        },
        {
          label: 'Lettermint', host: 'smtp.lettermint.co', port: 465, authProtocol: 'login',
        },
      ],
    };
  },

  methods: {
    selectPreset(preset) {
      this.$emit('select', {
        host: preset.host,
        port: preset.port,
        authProtocol: preset.authProtocol,
        tlsType: preset.port === 587 ? 'STARTTLS' : 'TLS',
        username: '',
        password: '',
        helloHostname: '',
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

  .button {
    height: auto;
    padding: 0;
    color: var(--lm-color-primary);
    text-decoration: none;
    white-space: normal;
  }
}
</style>
