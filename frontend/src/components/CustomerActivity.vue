<template>
  <div class="customer-activity">
    <div v-if="isLoading" class="has-text-centered">
      <b-loading :active="true" :is-full-page="false" />
    </div>

    <div v-else>
      <!-- Summary Stats -->
      <div class="columns">
        <div class="column is-4">
          <div class="box has-text-centered">
            <p class="heading">{{ $t('globals.terms.campaigns') }}</p>
            <p class="title">{{ activity.campaignViews ? activity.campaignViews.length : 0 }}</p>
          </div>
        </div>
        <div class="column is-4">
          <div class="box has-text-centered">
            <p class="heading">{{ $t('campaigns.views') }}</p>
            <p class="title">{{ totalViews }}</p>
          </div>
        </div>
        <div class="column is-4">
          <div class="box has-text-centered">
            <p class="heading">{{ $t('campaigns.clicks') }}</p>
            <p class="title">{{ totalClicks }}</p>
          </div>
        </div>
      </div>

      <!-- Campaign Views Section -->
      <div class="section-header mb-4">
        <h5 class="title is-5">
          {{ $t('campaigns.views') }}
        </h5>
      </div>

      <div v-if="activity.campaignViews && activity.campaignViews.length > 0">
        <b-table :data="activity.campaignViews" hoverable default-sort="lastViewedAt" default-sort-direction="desc"
          paginated :per-page="10" :pagination-simple="false" class="campaign-views-table">
          <b-table-column v-slot="props" field="subject" :label="$tc('globals.terms.campaign', 1)" sortable>
            <div v-if="props.row.uuid">
              <router-link :to="{ name: 'campaign', params: { id: props.row.id } }">
                {{ props.row.name }}
              </router-link>
              <p class="is-size-7 has-text-grey">{{ props.row.subject }}</p>
            </div>
            <div v-else>
              <em class="has-text-grey">{{ $t('customers.activity.campaignDeleted') }}</em>
            </div>
          </b-table-column>

          <b-table-column v-slot="props" field="viewCount" :label="$t('campaigns.views')" sortable numeric>
            <span class="tag is-light">{{ props.row.viewCount }}</span>
          </b-table-column>

          <b-table-column v-slot="props" field="lastViewedAt" :label="$t('globals.fields.createdAt')" sortable>
            <span v-if="props.row.lastViewedAt">
              {{ $utils.niceDate(props.row.lastViewedAt, true) }}
            </span>
          </b-table-column>
        </b-table>
      </div>
      <div v-else class="has-text-centered has-text-grey p-6">
        <p class="mt-2">{{ $t('globals.messages.emptyState') }}</p>
      </div>

      <!-- Link Clicks Section -->
      <div class="section-header mb-4 mt-6">
        <h5 class="title is-5">
          {{ $t('campaigns.clicks') }}
        </h5>
      </div>

      <div v-if="activity.linkClicks && activity.linkClicks.length > 0">
        <b-table :data="activity.linkClicks" hoverable default-sort="lastClickedAt" default-sort-direction="desc"
          paginated :per-page="10" :pagination-simple="false" class="link-clicks-table">
          <b-table-column v-slot="props" field="url" :label="$t('globals.terms.url')" cell-class="link-click-url"
            sortable>
            <a :href="props.row.url" target="_blank" rel="noopener noreferrer">
              {{ props.row.url }}
            </a>
          </b-table-column>

          <b-table-column v-slot="props" field="campaignName" :label="$tc('globals.terms.campaign', 1)" sortable>
            <div v-if="props.row.campaignUuid">
              <router-link :to="{ name: 'campaign', params: { id: props.row.campaignId } }">
                {{ props.row.campaignSubject || props.row.campaignName }}
              </router-link>
            </div>
            <div v-else>
              &mdash;
            </div>
          </b-table-column>

          <b-table-column v-slot="props" field="clickCount" :label="$t('campaigns.clicks')" sortable numeric>
            <span class="tag is-light">{{ props.row.clickCount }}</span>
          </b-table-column>

          <b-table-column v-slot="props" field="lastClickedAt" :label="$t('globals.fields.createdAt')" sortable>
            <span v-if="props.row.lastClickedAt">
              {{ $utils.niceDate(props.row.lastClickedAt, true) }}
            </span>
          </b-table-column>
        </b-table>
      </div>
      <div v-else class="has-text-centered has-text-grey p-6">
        <p class="mt-2">{{ $t('globals.messages.emptyState') }}</p>
      </div>

      <!-- Reply AI Classifications -->
      <div class="section-header mb-4 mt-6">
        <h5 class="title is-5">
          {{ $t('customers.replyAI.title') }}
        </h5>
        <p class="help">{{ $t('customers.replyAI.help') }}</p>
      </div>

      <div v-if="activity.replyAiEvents && activity.replyAiEvents.length > 0">
        <b-table :data="activity.replyAiEvents" hoverable default-sort="receivedAt" default-sort-direction="desc"
          paginated :per-page="10" class="reply-ai-events-table">
          <b-table-column v-slot="props" field="receivedAt" :label="$t('globals.fields.createdAt')" sortable>
            <span v-if="props.row.receivedAt">
              {{ $utils.niceDate(props.row.receivedAt, true) }}
            </span>
          </b-table-column>

          <b-table-column v-slot="props" field="intent" :label="$t('customers.replyAI.intent')" sortable>
            <b-tag :type="intentType(props.row.intent)" rounded size="is-small">
              {{ $t(`customers.replyAI.intent.${props.row.intent}`) }}
            </b-tag>
          </b-table-column>

          <b-table-column v-slot="props" field="action" :label="$t('customers.replyAI.action')" sortable>
            <b-tag :type="actionType(props.row.action)" rounded size="is-small">
              {{ $t(`customers.replyAI.action.${props.row.action}`) }}
            </b-tag>
          </b-table-column>

          <b-table-column v-slot="props" field="status" :label="$t('customers.replyAI.status')" sortable>
            <span class="is-size-7">{{ $t(`customers.replyAI.status.${props.row.status}`) }}</span>
          </b-table-column>

          <b-table-column v-slot="props" field="confidence" :label="$t('customers.replyAI.confidence')" numeric>
            <span v-if="props.row.confidence">{{ (props.row.confidence * 100).toFixed(0) }}%</span>
            <span v-else>&mdash;</span>
          </b-table-column>

          <b-table-column v-slot="props" field="model" :label="$t('customers.replyAI.model')">
            <span v-if="props.row.model" class="is-size-7">{{ props.row.model }}</span>
            <span v-else>&mdash;</span>
          </b-table-column>

          <b-table-column v-slot="props" field="subject" :label="$t('customers.replyAI.subject')">
            <span class="is-size-7 has-text-grey">{{ props.row.subject }}</span>
          </b-table-column>
        </b-table>
      </div>
      <div v-else class="has-text-centered has-text-grey p-6">
        <p class="mt-2">{{ $t('globals.messages.emptyState') }}</p>
      </div>
    </div>
  </div>
</template>

<script>
import Vue from 'vue';

export default Vue.extend({
  props: {
    customerId: {
      type: Number,
      required: true,
    },
  },

  data() {
    return {
      isLoading: false,
      activity: {
        campaignViews: [],
        linkClicks: [],
        replyAiEvents: [],
      },
    };
  },

  computed: {
    totalViews() {
      if (!this.activity.campaignViews) return 0;
      return this.activity.campaignViews.reduce((sum, v) => sum + (v.viewCount || 0), 0);
    },

    totalClicks() {
      if (!this.activity.linkClicks) return 0;
      return this.activity.linkClicks.reduce((sum, c) => sum + (c.clickCount || 0), 0);
    },
  },

  mounted() {
    this.getActivity();
  },

  methods: {
    getActivity() {
      this.isLoading = true;
      this.$api.getCustomerActivity(this.customerId).then((data) => {
        this.activity = data;
        this.isLoading = false;
      }).catch(() => {
        this.isLoading = false;
      });
    },

    intentType(intent) {
      return ({
        unsubscribe: 'is-warning',
        complaint: 'is-danger',
        other: 'is-light',
      })[intent] || 'is-light';
    },

    actionType(action) {
      return ({
        blocklisted: 'is-danger',
        ignored: 'is-light',
        pending: 'is-info',
      })[action] || 'is-light';
    },
  },
});
</script>
