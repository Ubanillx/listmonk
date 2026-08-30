<template>
  <section class="organizations section-mini">
    <header class="columns page-header">
      <div class="column">
        <h1 class="title is-4"><b-icon icon="account-plus-outline" size="is-small" />加入组织</h1>
      </div>
    </header>

    <form @submit.prevent="joinOrganization">
      <b-field label="邀请码" label-position="on-border">
        <b-input v-model.trim="joinCode" icon="key-outline" maxlength="500" required autofocus />
      </b-field>
      <b-button native-type="submit" type="is-primary" icon-left="account-plus-outline">加入组织</b-button>
    </form>

    <b-notification v-if="joinedOrganization" class="mt-5" type="is-success" :closable="false">
      已加入 {{ joinedOrganization.name }}
      <b-button class="ml-3" size="is-small" icon-left="login-variant" @click="switchWorkspace(joinedOrganization)">
        进入组织
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
      this.$utils.toast(`已加入 ${organization.name}`);
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
