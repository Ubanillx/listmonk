<template>
  <section class="section-mini page-403">
    <div class="content has-text-centered">
      <p>
        <b-icon icon="lock-outline" size="is-large" />
      </p>
      <p class="title is-5">
        {{ $t('globals.messages.permissionDeniedPage') }}
      </p>
      <p class="has-text-grey">
        {{ $t('globals.messages.permissionDeniedHelp') }}
      </p>
      <b-button type="is-primary" @click="goToDashboard">
        {{ $t('globals.buttons.backToDashboard') }}
      </b-button>
    </div>
  </section>
</template>

<script>
import Vue from 'vue';

export default Vue.extend({
  name: 'Forbidden',

  methods: {
    goToDashboard() {
      this.$router.push({ name: 'dashboard' });
    },
  },
});
</script>
