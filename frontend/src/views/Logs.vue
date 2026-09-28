<template>
  <section class="logs content relative">
    <h1 class="title is-4">
      {{ $t('logs.title') }}
    </h1>
    <hr />
    <b-notification v-if="error" type="is-danger" :closable="false">
      {{ $t('globals.messages.loadFailed') }}
      <b-button class="is-small ml-3" type="is-primary" @click="startPolling">
        {{ $t('globals.buttons.retry') }}
      </b-button>
    </b-notification>
    <log-view :loading="loading.logs" :lines="lines" />
  </section>
</template>

<script>
import Vue from 'vue';
import { mapState } from 'vuex';
import LogView from '../components/LogView.vue';

export default Vue.extend({
  components: {
    LogView,
  },

  data() {
    return {
      lines: [],
      pollId: null,
      error: false,
    };
  },

  methods: {
    getLogs() {
      return this.$api.getLogs().then((data) => {
        this.lines = data;
        this.error = false;
      }).catch(() => {
        // Without `settings:get` every poll answers 403, which used to raise a
        // fresh error toast every 10 seconds for as long as the page stayed
        // open. Stop polling and let the user retry explicitly instead.
        this.error = true;
        this.stopPolling();
      });
    },

    stopPolling() {
      if (this.pollId !== null) {
        clearInterval(this.pollId);
        this.pollId = null;
      }
    },

    startPolling() {
      this.stopPolling();
      this.getLogs();

      // Update the logs every 10 seconds.
      this.pollId = setInterval(() => this.getLogs(), 10000);
    },
  },

  computed: {
    ...mapState(['logs', 'loading']),
  },

  mounted() {
    this.startPolling();
  },

  destroyed() {
    this.stopPolling();
  },
});
</script>
