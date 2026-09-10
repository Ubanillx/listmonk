<template>
  <section class="organizations section-mini">
    <header class="columns page-header">
      <div class="column">
        <h1 class="title is-4">{{ $t('organizations.joinTitleShort') }}</h1>
      </div>
    </header>

    <form @submit.prevent="joinOrganization">
      <b-field :label="$t('organizations.inviteCode')" label-position="on-border">
        <b-input v-model.trim="joinCode" icon="key-outline" maxlength="500" required autofocus />
      </b-field>
      <b-button native-type="submit" type="is-primary" icon-left="account-plus-outline">{{ $t('organizations.joinTitleShort') }}</b-button>
    </form>

    <b-notification v-if="joinedOrganization" class="mt-5" type="is-success" :closable="false">
      {{ $t('organizations.joined', { name: joinedOrganization.name }) }}
      <b-button class="ml-3" size="is-small" icon-left="login-variant" @click="switchWorkspace(joinedOrganization)">
        {{ $t('organizations.enterOrgShort') }}
      </b-button>
    </b-notification>
  </section>
</template>

<script>
import Vue from 'vue';

export default Vue.extend({
  data() {
    return {
      joinCode: '',
      joinedOrganization: null,
    };
  },

  methods: {
    async refreshOrganizations() {
      const organizations = await this.$api.getMyOrganizations();
      this.$store.commit('setOrganizations', organizations);
    },

    async joinOrganization() {
      const organization = await this.$api.joinOrganization({ code: this.joinCode });
      this.joinCode = '';
      this.joinedOrganization = organization;
      await this.refreshOrganizations();
      this.$utils.toast(this.$t('organizations.toastJoined', { name: organization.name }));
    },

    switchWorkspace(organization) {
      this.$store.commit('setWorkspace', organization);
      this.$store.commit('resetWorkspaceModels');
      this.$router.go(0);
    },
  },

  created() {
    this.$root.$on('page.refresh', this.refreshOrganizations);
  },

  destroyed() {
    this.$root.$off('page.refresh', this.refreshOrganizations);
  },
});
</script>
