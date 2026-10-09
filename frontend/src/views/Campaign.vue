<template>
  <section class="campaign">
    <header class="columns page-header">
      <div class="column is-6">
        <p v-if="isEditing && data.status" class="tags">
          <b-tag v-if="isEditing" :class="data.status">
            {{ $t(`campaigns.status.${data.status}`) }}
          </b-tag>
          <b-tag v-if="data.type === 'optin'" :class="data.type">
            {{ $t('customer_lists.optin') }}
          </b-tag>
          <span v-if="isEditing" class="has-text-grey-light is-size-7" :data-campaign-id="data.id">
            {{ $t('globals.fields.id') }}: <copy-text :text="`${data.id}`" />
            {{ $t('globals.fields.uuid') }}: <copy-text :text="data.uuid" />
          </span>
        </p>
        <h4 v-if="isEditing" class="title is-4">
          {{ data.name }}
        </h4>
        <h4 v-else class="title is-4">
          {{ $t('campaigns.newCampaign') }}
        </h4>
      </div>

      <div class="column is-6">
        <div v-if="canManage" class="buttons">
          <b-field grouped v-if="isEditing && canEdit">
            <b-field expanded>
              <b-button expanded @click="() => onSubmit('update')" :loading="loading.campaigns" type="is-primary"
                icon-left="content-save-outline" data-cy="btn-save" aria-keyshortcuts="ctrl+s">
                <span class="has-kbd">{{ $t('globals.buttons.saveChanges') }} <span class="kbd">Ctrl+S</span></span>
              </b-button>
            </b-field>
            <b-field expanded v-if="canStart">
              <b-button expanded @click="startCampaign" :loading="loading.campaigns" type="is-primary"
                icon-left="rocket-launch-outline" data-cy="btn-start">
                {{ $t('campaigns.start') }}
              </b-button>
            </b-field>
            <b-field expanded v-if="canSchedule">
              <b-button expanded @click="startCampaign" :loading="loading.campaigns" type="is-primary"
                icon-left="clock-start" data-cy="btn-schedule">
                {{ $t('campaigns.schedule') }}
              </b-button>
            </b-field>
            <b-field expanded v-if="canUnSchedule">
              <b-button expanded @click="$utils.confirm(null, unscheduleCampaign)" :loading="loading.campaigns"
                type="is-primary" icon-left="clock-start" data-cy="btn-unschedule">
                {{ $t('campaigns.unSchedule') }}
              </b-button>
            </b-field>
          </b-field>
        </div>
      </div>
    </header>

    <b-loading :active="loading.campaigns" />

    <b-tabs type="is-boxed" :animated="false" v-model="activeTab" @input="onTab">
      <b-tab-item :label="$tc('globals.terms.campaign')" label-position="on-border" value="campaign"
        icon="rocket-launch-outline">
        <section class="wrap campaign-setup">
          <form @submit.prevent="() => onSubmit(isNew ? 'create' : 'update')">
            <div class="campaign-setup-grid">
              <div class="campaign-setup-column">
                <section class="campaign-config-section" aria-labelledby="campaign-basics-title" data-cy="campaign-basics">
                  <h3 id="campaign-basics-title">{{ $t('campaigns.setupBasics') }}</h3>
                  <div class="campaign-field-row">
                    <b-field :label="$t('globals.fields.name')">
                      <b-input :maxlength="200" :ref="'focus'" v-model="form.name" name="name" :disabled="!canEdit"
                        :placeholder="$t('globals.fields.name')" required autofocus />
                    </b-field>
                    <b-field :label="$t('visibility.label')">
                      <b-select v-model="form.visibility" :disabled="!canEdit" expanded data-cy="campaign-visibility">
                        <option value="private">{{ $t('visibility.private') }}</option>
                        <option v-if="workspace.organizationId" value="organization">{{ $t('visibility.organization') }}</option>
                        <option value="global">{{ $t('visibility.global') }}</option>
                      </b-select>
                    </b-field>
                  </div>
                  <b-field :label="$t('campaigns.subject')">
                    <b-input :maxlength="5000" v-model="form.subject" name="subject" :disabled="!canEdit"
                      :placeholder="$t('campaigns.subject')" required />
                  </b-field>
                  <div class="campaign-field-row">
                    <b-field :label="$t('campaigns.format')">
                      <b-select v-model="form.content.contentType" :disabled="!canEdit || isEditing" value="richtext" expanded>
                        <option v-for="(name, f) in contentTypes" :key="f" name="format" :value="f" :data-cy="`check-${f}`">
                          {{ name }}
                        </option>
                      </b-select>
                    </b-field>
                    <div class="campaign-inline-toggle">
                      <b-switch v-model="form.autoTrackLinks" :disabled="!canEdit" data-cy="campaign-auto-track-links">
                        {{ $t('campaigns.autoTrackLinks') }}
                      </b-switch>
                    </div>
                  </div>
                </section>

                <section class="campaign-config-section" aria-labelledby="campaign-audience-title" data-cy="campaign-audience-section">
                  <h3 id="campaign-audience-title">{{ $t('campaigns.setupAudience') }}</h3>
                  <campaign-audience-tree-select v-model="form.customer_lists" :all="availableLists"
                    :disabled="!canEdit || listsLocked" :exclusive-groups="exclusiveAudienceGroups"
                    :pool-only="isEditing && isPlatformPoolCampaign"
                    :label="$t('campaigns.audienceLists')" :placeholder="$t('campaigns.sendToLists')" />
                  <p v-if="listsLocked" class="help is-info">{{ $t('campaigns.listsLockedHelp') }}</p>
                  <b-field v-if="isNew && workspace.organizationId && hasPoolAudience && $can('campaigns:public_pool_send')"
                    :label="$t('campaigns.poolScopeLabel')">
                    <b-select v-model="form.poolScope" expanded :disabled="!canEdit" data-cy="campaign-pool-scope">
                      <option value="organization">{{ $t('campaigns.poolScopeOrganization') }}</option>
                      <option value="all_organizations" :disabled="form.customer_lists.some((list) => list.type !== 'pool')">
                        {{ $t('campaigns.poolScopeAllOrganizations') }}
                      </option>
                    </b-select>
                  </b-field>
                  <div v-if="isPlatformPoolCampaign" class="campaign-context-note pool-scope-all-notice" data-cy="pool-scope-all-notice">
                    <strong>{{ $t('campaigns.poolScopeAllTitle') }}</strong>
                    <p class="help">{{ $t('campaigns.poolScopeAllHelp') }}</p>
                    <template v-if="poolSendStatusCurrent">
                      <p :class="poolSendStatus.ready ? 'has-text-success' : 'has-text-warning'" data-cy="pool-send-status-ready">
                        {{ poolSendStatus.ready ? $t('campaigns.poolSendReadyShort') : $t('campaigns.poolSendUnavailable') }}
                      </p>
                      <ul v-if="poolSendStatus.issues && poolSendStatus.issues.length" class="campaign-issue-list" data-cy="pool-send-status-issues">
                        <li v-for="(issue, index) in poolSendStatus.issues" :key="`pool-send-issue-${index}`">{{ issue }}</li>
                      </ul>
                    </template>
                    <p v-else-if="isEditing" class="help" data-cy="pool-send-status-save">{{ $t('campaigns.poolSendSaveToCheck') }}</p>
                  </div>
                </section>
              </div>

              <div class="campaign-setup-column">
                <section class="campaign-config-section" aria-labelledby="campaign-sender-title" data-cy="campaign-sender-section">
                  <h3 id="campaign-sender-title">{{ $t('campaigns.setupSender') }}</h3>
                  <div v-if="isSMTPMessenger" class="campaign-field-row">
                    <b-field :label="$t('campaigns.smtpSource')">
                      <b-select v-model="form.smtpSource" expanded :disabled="!canEdit || !$can('mailboxes:use')" data-cy="campaign-smtp-source" @input="onSMTPSourceSelection">
                        <option value="personal">{{ $t('campaigns.smtpPersonalRotation') }}</option>
                        <option value="organization" :disabled="!workspace.organizationId && !isPlatformPoolCampaign">
                          {{ $t('campaigns.smtpOrganizationRotation') }}
                        </option>
                      </b-select>
                    </b-field>
                    <b-field v-if="form.smtpSource === 'organization' && workspace.organizationId && !isPlatformPoolCampaign"
                      :label="$t('organizations.smtpPoolSelect')">
                      <b-select v-model.number="form.smtpPoolId" expanded
                        :disabled="!canEdit || !$can('mailboxes:use') || !smtpPoolsLoaded || !smtpPools.length" data-cy="campaign-smtp-pool">
                        <option v-for="pool in smtpPools" :key="pool.id" :value="pool.id">{{ pool.name }} ({{ pool.enabledCount }}/{{ pool.smtpCount }})</option>
                      </b-select>
                    </b-field>
                  </div>
                  <p v-if="isSMTPMessenger && isPlatformPoolCampaign" class="help" data-cy="campaign-pool-smtp-help">
                    {{ $t(form.smtpSource === 'organization' ? 'campaigns.poolOrganizationSMTPHelp' : 'campaigns.poolMemberSMTPHelp') }}
                  </p>
                  <section v-if="isSMTPMessenger" class="campaign-sender-overview" data-cy="campaign-smtp-overview">
                    <div class="campaign-section-caption">
                      <strong>{{ $t('campaigns.smtpOverview') }}</strong>
                      <span class="campaign-count">{{ smtpSenders.length }}</span>
                    </div>
                    <p v-if="!personalSMTPLoaded" class="help">{{ $t('campaigns.smtpOverviewLoading') }}</p>
                    <p v-else-if="!smtpSenders.length" class="help is-warning" data-cy="campaign-smtp-unavailable">
                      {{ smtpUnavailableMessage }}
                    </p>
                    <ul v-else class="campaign-sender-list">
                      <li v-for="sender in smtpSenders" :key="sender.id">
                        <div>
                          <strong>{{ sender.fromEmail }}</strong>
                          <span class="help">{{ sender.organizationName || sender.name }}</span>
                        </div>
                        <span class="help">
                          {{ $t('settings.personalSMTP.sentToday', { count: sender.sentToday }) }} ·
                          {{ $t('campaigns.smtpQuotaOverview', { limit: sender.dailyLimit || $t('campaigns.smtpUnlimited') }) }}
                        </span>
                      </li>
                    </ul>
                  </section>
                  <b-field v-else key="campaign-custom-from" :label="$t('campaigns.fromAddress')">
                    <b-input :maxlength="200" v-model="form.fromEmail" name="from_email" :disabled="!canEdit"
                      :placeholder="$t('campaigns.fromAddressPlaceholder')" required />
                  </b-field>

                  <b-field v-if="isSMTPMessenger && hasPrivateAudience" key="campaign-reply-mailbox" :label="$t('campaigns.replyMailbox')">
                    <b-select v-model="form.replyMailboxId" :disabled="!canEdit || !$can('mailboxes:use') || activeReplyMailboxes.length === 0" expanded data-cy="campaign-reply-mailbox">
                      <option :value="null">{{ $t('campaigns.replyMailboxNone') }}</option>
                      <option v-if="form.replyMailboxId && !activeReplyMailboxes.some((mailbox) => mailbox.id === Number(form.replyMailboxId))"
                        :value="form.replyMailboxId" disabled>
                        {{ form.replyMailboxEmail || data.replyMailboxEmail || $t('campaigns.replyMailboxLegacy') }}
                      </option>
                      <option v-for="mailbox in activeReplyMailboxes" :key="mailbox.id" :value="mailbox.id">
                        {{ mailbox.name || mailbox.email }}{{ mailbox.isDefault ? $t('campaigns.replyMailboxDefaultTag') : '' }}
                      </option>
                    </b-select>
                  </b-field>
                  <p v-if="isSMTPMessenger && hasPrivateAudience && replyMailboxesLoaded && activeReplyMailboxes.length === 0" class="help is-warning">
                    {{ $t('campaigns.replyMailboxMissing') }}
                  </p>
                  <b-field v-if="isSMTPMessenger && hasPoolAudience" key="campaign-pool-reply-priority" :label="$t('campaigns.poolReplyPriority')"
                    :message="$t('campaigns.poolReplyPriorityHelp')">
                    <b-select v-model="form.poolReplyPriority" :disabled="!canEdit" expanded data-cy="campaign-pool-reply-priority">
                      <option value="contact_first">{{ $t('campaigns.poolReplyContactFirst') }}</option>
                      <option value="organization_first">{{ $t('campaigns.poolReplyOrganizationFirst') }}</option>
                    </b-select>
                  </b-field>

                  <details v-if="poolNoticeRows.length" class="campaign-config-details pool-routing-notice" data-cy="pool-routing-notice">
                    <summary>{{ $t('campaigns.poolRouteTitle') }}</summary>
                    <p v-if="hasUnresolvedPoolRoute" class="help is-warning">{{ $t('campaigns.poolRouteBlocked') }}</p>
                    <ul class="campaign-route-list">
                      <li v-for="(pool, index) in poolNoticeRows" :key="`pool-route-${pool.poolId || pool.pool_id}-${index}`">
                        <span>{{ pool.name || $t('campaigns.poolFallback', { id: pool.poolId || pool.pool_id }) }}</span>
                        <strong v-if="pool.replyMailboxEmail || pool.reply_mailbox_email">{{ pool.replyMailboxEmail || pool.reply_mailbox_email }}</strong>
                        <span v-else class="has-text-warning">{{ pool.pending ? $t('campaigns.poolRoutePending') : $t('campaigns.poolRouteUnresolved') }}</span>
                      </li>
                    </ul>
                  </details>
                  <details v-if="isSMTPMessenger" class="campaign-config-details" data-cy="campaign-sender-rules">
                    <summary>{{ $t('campaigns.setupSenderRules') }}</summary>
                    <p class="help">{{ $t('campaigns.smtpOverviewHelp') }}</p>
                    <p v-if="!hasPoolAudience" class="help">{{ $t('campaigns.replyMailboxHelp') }}</p>
                    <p v-else class="help">{{ $t('campaigns.replyMailboxPoolHelp') }}</p>
                  </details>
                </section>

                <section class="campaign-config-section" aria-labelledby="campaign-delivery-title" data-cy="campaign-delivery-section">
                  <h3 id="campaign-delivery-title">{{ $t('campaigns.setupDelivery') }}</h3>
                  <div v-if="isSMTPMessenger" data-cy="campaign-delivery-limits">
                    <div class="campaign-field-row">
                      <b-field :label="$t('campaigns.smtpRateLimit')">
                        <b-numberinput v-model="form.smtpRateLimit" :disabled="!canEdit" name="smtp_rate_limit"
                          min="1" max="1000000" type="is-light" controls-position="compact" required data-cy="campaign-smtp-rate-limit" />
                      </b-field>
                      <b-field v-if="isLimitedSMTPCampaign" :label="$t('campaigns.dailySendLimit')">
                        <b-numberinput v-model="form.dailySendLimit" :disabled="!canEdit" name="daily_send_limit"
                          min="1" max="100000000" type="is-light" controls-position="compact" required />
                      </b-field>
                    </div>
                    <b-field v-if="isLimitedSMTPCampaign" :label="$t('campaigns.dailyResumeTime')">
                      <b-timepicker v-model="dailyResumeTimeDate" :disabled="!canEdit" placeholder="09:00"
                        hour-format="24" icon="clock-outline" :time-formatter="formatResumeTime"
                        :aria-label="$t('campaigns.dailyResumeTime')" data-cy="campaign-daily-resume-time" mobile-native expanded required />
                    </b-field>
                    <p v-if="data.status === 'deferred' && data.nextResumeAt" class="help is-warning">
                      {{ $t('campaigns.nextResumeAt') }}: {{ $utils.niceDate(data.nextResumeAt, true) }}
                    </p>
                  </div>
                  <div class="campaign-schedule-control" data-cy="btn-send-later">
                    <b-switch v-model="form.sendLater" :disabled="!canEdit">{{ $t('campaigns.sendLater') }}</b-switch>
                    <b-field v-if="form.sendLater" class="campaign-send-at-field" data-cy="send_at"
                      :message="form.sendAtDate ? $utils.duration(Date(), form.sendAtDate) : ''">
                      <b-datetimepicker v-model="form.sendAtDate" :disabled="!canEdit" required editable mobile-native
                        position="is-top-right" :placeholder="$t('campaigns.dateAndTime')" icon="calendar-clock"
                        :timepicker="{ hourFormat: '24' }" :datetime-formatter="formatDateTime"
                        :datetime-parser="$utils.parseDateTime" horizontal-time-picker />
                    </b-field>
                  </div>
                  <details v-if="isSMTPMessenger" class="campaign-config-details" data-cy="campaign-delivery-rules">
                    <summary>{{ $t('campaigns.setupDeliveryRules') }}</summary>
                    <p class="help">{{ $t('campaigns.smtpRateLimitHelp') }}</p>
                    <p v-if="isLimitedSMTPCampaign" class="help">{{ $t('campaigns.dailyResumeTimeHelp') }}</p>
                  </details>
                </section>
              </div>
            </div>

            <details class="campaign-config-section campaign-advanced" data-cy="campaign-advanced"
              :open="!isNew && (form.headersStr !== '[]' || form.tags.length > 0 || !isSMTPMessenger)">
              <summary>{{ $t('campaigns.setupAdvanced') }}</summary>
              <div class="campaign-field-row">
                <b-field :label="$t('globals.terms.tags')">
                  <b-taginput v-model="form.tags" name="tags" :disabled="!canEdit" ellipsis icon="tag-outline" :placeholder="$t('globals.terms.tags')" />
                </b-field>
                <b-field :label="$tc('globals.terms.messenger')">
                  <b-select :placeholder="$tc('globals.terms.messenger')" v-model="form.messenger" name="messenger" :disabled="!canEdit" required expanded>
                    <template v-if="emailMessengers.length > 1">
                      <optgroup label="email">
                        <option v-for="m in emailMessengers" :value="m" :key="m">{{ m }}</option>
                      </optgroup>
                    </template>
                    <template v-else><option value="email">email</option></template>
                    <option v-for="m in otherMessengers" :value="m" :key="m">{{ m }}</option>
                  </b-select>
                </b-field>
              </div>
              <button type="button" class="campaign-text-button" @click="onShowHeaders" data-cy="btn-headers">
                {{ $t('settings.smtp.setCustomHeaders') }}
              </button>
              <b-field v-if="form.headersStr !== '[]' || isHeadersVisible" :message="$t('campaigns.customHeadersHelp')">
                <b-input v-model="form.headersStr" name="headers" type="textarea"
                  placeholder="[{&quot;X-Custom&quot;: &quot;value&quot;}, {&quot;X-Custom2&quot;: &quot;value&quot;}]" :disabled="!canEdit" />
              </b-field>
            </details>

            <div v-if="isNew" class="campaign-setup-actions">
              <span class="help">{{ $t('campaigns.setupNextStep') }}</span>
              <b-button native-type="submit" type="is-primary" :loading="loading.campaigns" data-cy="btn-continue">
                {{ $t('campaigns.continue') }}
              </b-button>
            </div>
          </form>

          <section v-if="canManage && isEditing" class="campaign-config-section campaign-test" data-cy="campaign-test-section">
            <h3>{{ $t('campaigns.sendTest') }}</h3>
            <div class="campaign-test-controls">
              <b-field :message="$t('campaigns.sendTestHelp')">
                <b-taginput v-model="form.testEmails" :before-adding="$utils.validateEmail" ellipsis
                  icon="email-outline" :placeholder="$t('campaigns.testEmails')" />
              </b-field>
              <b-button @click="() => onSubmit('test')" :loading="loading.campaigns"
                :disabled="!canTestCampaign || (isSMTPMessenger && !personalSMTPAvailable)" type="is-primary" icon-left="email-outline">
                {{ $t('campaigns.send') }}
              </b-button>
            </div>
          </section>
        </section>
      </b-tab-item><!-- campaign -->

      <b-tab-item :label="$t('campaigns.content')" icon="text" :disabled="isNew" value="content">
        <editor v-if="data.id" v-model="form.content" :id="data.id" :title="data.name" :disabled="!canEdit"
          :templates="templates" :content-types="contentTypes" :auto-track-links="form.autoTrackLinks" :media="form.media" ref="contentEditor"
          @template-media="onTemplateMedia" @template-attachments="onTemplateAttachments"
          @media-selected="onMediaSelect" />

        <div class="columns">
          <div class="column is-6">
            <div v-if="templateMedia.length > 0" class="template-media" data-cy="template-media">
              <p class="label template-media-label">
                {{ $t('campaigns.templateAttachments') }}
              </p>
              <b-taglist class="template-media-tags">
                <b-tag v-for="(item, index) in templateMedia" :key="`template-media-${item.id || item.filename}-${index}`"
                  type="is-info">
                  {{ item.filename }}
                </b-tag>
              </b-taglist>
            </div>

            <p v-if="!isAttachFieldVisible && canEdit" class="is-size-6 has-text-grey">
              <a href="#" @click.prevent="onShowAttachField()" data-cy="btn-attach">
                <b-icon icon="file-upload-outline" size="is-small" />
                {{ $t('campaigns.addAttachments') }}
              </a>
            </p>

            <b-field v-if="isAttachFieldVisible" :label="$t('campaigns.campaignAttachments')" label-position="on-border"
              expanded data-cy="media">
              <b-taginput v-model="form.media" name="media" ellipsis icon="tag-outline" ref="media" field="filename"
                @focus="onOpenAttach" :disabled="!canEdit" />
            </b-field>
          </div>
          <div class="column has-text-right">
            <span><b-icon icon="code" /> {{ $t('campaigns.templatingRef') }}</span>
            <div v-if="customFields.length" class="is-size-7 has-text-grey mt-2">
              {{ $t('customFields.placeholder') }}:
              <code v-for="field in customFields" :key="field.key" class="ml-2">{{ field.placeholder }}</code>
            </div>
            <span v-if="canEdit && form.content.contentType !== 'plain'" class="is-size-6 has-text-grey ml-6">
              <a v-if="form.altbody === null" href="#" @click.prevent="onAddAltBody">
                <b-icon icon="text" size="is-small" /> {{ $t('campaigns.addAltText') }}
              </a>
              <a v-else href="#" @click.prevent="$utils.confirm(null, onRemoveAltBody)">
                <b-icon icon="trash-can-outline" size="is-small" />
                {{ $t('campaigns.removeAltText') }}
              </a>
            </span>
          </div>
        </div>

        <div v-if="canEdit && form.content.contentType !== 'plain'" class="alt-body">
          <b-input v-if="form.altbody !== null" v-model="form.altbody" type="textarea" :disabled="!canEdit" />
        </div>
      </b-tab-item><!-- content -->

      <b-tab-item :label="$t('globals.terms.attribs')" icon="code-json" value="attribs" :disabled="isNew">
        <section class="wrap">
          <b-field :label="$t('globals.terms.attribs')" :message="$t('campaigns.attribsHelp')"
            label-position="on-border">
            <b-input v-model="form.attribsStr" type="textarea" :disabled="!canEdit" rows="15" />
          </b-field>
        </section>
      </b-tab-item><!-- attribs -->

      <b-tab-item
        v-if="isEditing && canViewAnalytics"
        :label="$t('globals.terms.analytics')"
        icon="chart-box-outline"
        value="analytics"
      >
        <section class="wrap">
          <campaign-report :campaign="data" :active="activeTab === 'analytics'" />
        </section>
      </b-tab-item><!-- analytics -->

      <b-tab-item :label="$t('campaigns.archive')" icon="newspaper-variant-outline" value="archive" :disabled="isNew">
        <section class="wrap">
          <div class="columns">
            <div class="column is-4">
              <b-field :label="$t('campaigns.archiveEnable')" data-cy="btn-archive"
                :message="$t('campaigns.archiveHelp')">
                <div class="columns">
                <div class="column">
                    <b-switch data-cy="btn-archive" v-model="form.archive" :disabled="!canArchive" />
                  </div>
                  <div class="column is-12">
                    <a :href="`${serverConfig.root_url}/archive/${data.uuid}`" target="_blank" rel="noopener noreferer"
                      :class="{ 'has-text-grey-light': !form.archive }" aria-label="$t('campaigns.archive')">
                      <b-icon icon="link-variant" />
                    </a>
                  </div>
                </div>
              </b-field>
            </div>
            <div class="column is-8">
              <b-field grouped position="is-right">
                <b-field v-if="canArchive">
                  <b-button @click="onUpdateCampaignArchive" :loading="loading.campaigns" type="is-primary"
                    icon-left="content-save-outline" data-cy="btn-save">
                    {{ $t('globals.buttons.saveChanges') }}
                  </b-button>
                </b-field>
              </b-field>
            </div>
          </div>

          <div class="columns">
            <div class="column is-6">
              <b-field :label="$tc('globals.terms.template')" label-position="on-border">
                <b-select :placeholder="$tc('globals.terms.template')" v-model="form.archiveTemplateId" name="template"
                  :disabled="!canArchive || !form.archive || form.content.contentType === 'visual'" required>
                  <template v-for="t in templates">
                    <option v-if="t.type === 'campaign'" :value="t.id" :key="t.id">
                      {{ t.name }}
                    </option>
                  </template>
                </b-select>
              </b-field>
            </div>

            <div class="column is-6">
              <b-field grouped position="is-right">
                <b-field v-if="form.archive && (!this.form.archiveMetaStr || this.form.archiveMetaStr === '{}')">
                  <a class="button is-primary" href="#" @click.prevent="onFillArchiveMeta" aria-label="{}"><b-icon
                      icon="code" /></a>
                </b-field>
                <b-field v-if="form.archive">
                  <b-button @click="onToggleArchivePreview" type="is-primary" icon-left="file-find-outline"
                    data-cy="btn-preview">
                    {{ $t('campaigns.preview') }}
                  </b-button>
                </b-field>
              </b-field>
            </div>
          </div>
          <b-field>
            <b-field :label="$t('campaigns.archiveSlug')" label-position="on-border"
              :message="$t('campaigns.archiveSlugHelp')">
              <b-input :maxlength="200" :ref="'focus'" v-model="form.archiveSlug" name="archive_slug"
                data-cy="archive-slug" :disabled="!canArchive || !form.archive" />
            </b-field>
          </b-field>
          <b-field :label="$t('campaigns.archiveMeta')" :message="$t('campaigns.archiveMetaHelp')"
            label-position="on-border">
            <b-input v-model="form.archiveMetaStr" name="archive_meta" type="textarea" data-cy="archive-meta"
              :disabled="!canArchive || !form.archive" rows="20" />
          </b-field>
        </section>
      </b-tab-item><!-- archive -->
    </b-tabs>

    <b-modal scroll="keep" :aria-modal="true" :active.sync="isAttachModalOpen" :width="900">
      <div class="modal-card content" style="width: auto">
        <section expanded class="modal-card-body">
          <media is-modal @selected="onAttachSelect" />
        </section>
      </div>
    </b-modal>

    <campaign-preview v-if="isPreviewingArchive" @close="onToggleArchivePreview" type="campaign" :id="data.id"
      :archive-meta="form.archiveMetaStr" :title="data.title" :content-type="data.contentType"
      :template-id="form.archiveTemplateId" :media="archivePreviewMedia" is-post is-archive />
  </section>
</template>

<script>
import dayjs from 'dayjs';
import htmlToPlainText from 'textversionjs';
import Vue from 'vue';
import { mapState } from 'vuex';

import CampaignPreview from '../components/CampaignPreview.vue';
import CampaignReport from '../components/CampaignReport.vue';
import CopyText from '../components/CopyText.vue';
import Editor from '../components/Editor.vue';
import CampaignAudienceTreeSelect from '../components/CampaignAudienceTreeSelect.vue';
import { isActiveWorkspaceCustomerList } from '../utils/workspace';
import Media from './Media.vue';

export default Vue.extend({
  components: {
    CampaignAudienceTreeSelect,
    Editor,
    Media,
    CopyText,
    CampaignPreview,
    CampaignReport,
  },

  data() {
    const organizationWorkspace = this.$store.state.workspace.organizationId > 0;
    return {
      contentTypes: Object.freeze({
        richtext: this.$t('campaigns.richText'),
        html: this.$t('campaigns.rawHTML'),
        markdown: this.$t('campaigns.markdown'),
        plain: this.$t('campaigns.plainText'),
        visual: this.$t('campaigns.visual'),
      }),

      isNew: false,
      isEditing: false,
      isHeadersVisible: false,
      isAttachFieldVisible: false,
      isAttachModalOpen: false,
      isPreviewingArchive: false,
      activeTab: 'campaign',
      templateMedia: [],
      smtpSenders: [],
      smtpPools: [],
      smtpPoolsLoaded: false,
      smtpOverviewRequest: 0,
      personalSMTPLoaded: false,
      // Per-organization readiness of a platform-level public-pool campaign.
      poolSendStatus: null,
      poolSendStatusKey: '',
      poolSendStatusRequest: 0,
      replyMailboxes: [],
      replyMailboxesLoaded: false,
      customFields: [],

      data: {},

      // IDs from ?customer_list_id query param.
      selCustomerListIDs: [],

      // Binds form input values.
      form: {
        archiveSlug: null,
        name: '',
        subject: '',
        fromEmail: '',
        smtpSource: organizationWorkspace ? 'organization' : 'personal',
        smtpPoolId: null,
        poolScope: organizationWorkspace ? 'organization' : 'all_organizations',
        smtpRateLimit: organizationWorkspace ? 100 : 20,
        replyMailboxId: null,
        poolReplyPriority: 'contact_first',
        headersStr: '[]',
        headers: [],
        attribsStr: '{}',
        messenger: 'email',
        autoTrackLinks: true,
        visibility: organizationWorkspace ? 'organization' : 'private',
        dailySendLimit: 300,
        dailyResumeTime: '09:00',
        customer_lists: [],
        tags: [],
        sendAt: null,
        content: {
          contentType: 'richtext',
          body: '',
          bodySource: null,
          templateId: null,
        },
        altbody: null,
        media: [],

        // Parsed Date() version of send_at from the API.
        sendAtDate: null,
        sendLater: false,
        archive: false,
        archiveMetaStr: '{}',
        archiveMeta: {},
        testEmails: [],
      },
    };
  },

  methods: {
    onSMTPSourceSelection(source) {
      this.form.smtpRateLimit = source === 'organization' ? 100 : 20;
    },

    formatDateTime(s) {
      return this.$utils.niceDate(s, true);
    },

    onToggleArchivePreview() {
      this.isPreviewingArchive = !this.isPreviewingArchive;
    },

    onAddAltBody() {
      this.form.altbody = htmlToPlainText(this.form.content.body);
    },

    onRemoveAltBody() {
      this.form.altbody = null;
    },

    onShowHeaders() {
      this.isHeadersVisible = !this.isHeadersVisible;
    },

    onShowAttachField() {
      this.isAttachFieldVisible = true;
      this.$nextTick(() => {
        this.$refs.media.focus();
      });
    },

    onOpenAttach() {
      this.isAttachModalOpen = true;
    },

    onAttachSelect(o) {
      if (this.form.media.some((m) => m.id === o.id)) {
        return;
      }

      this.form.media.push(o);
    },

    onMediaSelect(media) {
      this.onAttachSelect(media);
      this.isAttachFieldVisible = this.form.media.length > 0;
    },

    onTemplateMedia(media) {
      media.forEach((item) => {
        if (item.id && !this.form.media.some((m) => m.id === item.id)) {
          this.form.media.push(item);
        }
      });
      this.isAttachFieldVisible = this.form.media.length > 0;
    },

    onTemplateAttachments(media) {
      this.templateMedia = Array.isArray(media) ? media : [];
    },

    isUnsaved() {
      // A campaign that has never been saved has no server state to lose, so
      // there is nothing to warn about. Its default template body differed from
      // the empty server baseline, which made the page prompt on every leave
      // (verified in the browser: the unload handler returned true on an empty
      // new-campaign form).
      if (!this.data.id) {
        return false;
      }

      return this.data.body !== this.form.content.body
        || this.data.contentType !== this.form.content.contentType;
    },

    onTab(tab) {
      if (tab === 'content' && window.tinymce && window.tinymce.editors.length > 0) {
        this.$nextTick(() => {
          window.tinymce.editors[0].focus();
        });
      }

      // this.$router.replace({ hash: `#${tab}` });
      window.history.replaceState({}, '', `#${tab}`);
    },

    onFillArchiveMeta() {
      const archiveStr = `{"email": "email@domain.com", "name": "${this.$t('globals.fields.name')}", "attribs": {}}`;
      this.form.archiveMetaStr = this.$utils.getPref('campaign.archiveMetaStr') || JSON.stringify(JSON.parse(archiveStr), null, 4);
    },

    normalizeDailyResumeTime(value) {
      const match = /^(\d{1,2}):(\d{2})$/.exec((value || '').trim());
      if (!match) {
        return value;
      }

      return `${match[1].padStart(2, '0')}:${match[2]}`;
    },

    formatResumeTime(value) {
      return `${String(value.getHours()).padStart(2, '0')}:${String(value.getMinutes()).padStart(2, '0')}`;
    },

    normalizeSMTPDailyFields(limit, resumeTime) {
      return {
        dailySendLimit: limit > 0 ? limit : 300,
        dailyResumeTime: this.normalizeDailyResumeTime(resumeTime || '09:00'),
      };
    },

    onSubmit(typ) {
      if (typ === 'test') {
        if (!this.canTestCampaign) {
          this.$utils.toast(this.$t('campaigns.onlyOwnerCanSend'), 'is-danger');
          return;
        }
        if (this.isSMTPMessenger && !this.personalSMTPAvailable) {
          this.$utils.toast(this.smtpUnavailableMessage, 'is-danger');
          return;
        }
      }

      if (this.isLimitedSMTPCampaign) {
        const normalized = this.normalizeSMTPDailyFields(this.form.dailySendLimit, this.form.dailyResumeTime);
        this.form.dailySendLimit = normalized.dailySendLimit;
        this.form.dailyResumeTime = normalized.dailyResumeTime;
      }
      if (this.isSMTPMessenger && (!Number.isInteger(this.form.smtpRateLimit)
        || this.form.smtpRateLimit < 1 || this.form.smtpRateLimit > 1000000)) {
        this.$utils.toast(this.$t('campaigns.fieldInvalidSMTPRateLimit'), 'is-danger');
        return;
      }

      // Validate custom JSON headers.
      if (this.form.headersStr && this.form.headersStr !== '[]') {
        try {
          this.form.headers = JSON.parse(this.form.headersStr);
        } catch (e) {
          this.$utils.toast(e.toString(), 'is-danger');
          return;
        }
      } else {
        this.form.headers = [];
      }

      // Validate archive JSON body.
      if (this.form.archive && this.form.archiveMetaStr) {
        try {
          this.form.archiveMeta = JSON.parse(this.form.archiveMetaStr);
        } catch (e) {
          this.$utils.toast(e.toString(), 'is-danger');
          return;
        }
      }

      // Validate custom JSON attribs.
      let attribs = null;
      if (this.form.attribsStr && this.form.attribsStr.trim()) {
        try {
          attribs = JSON.parse(this.form.attribsStr);
        } catch (e) {
          this.$utils.toast(
            `${this.$t('customers.invalidJSON')}: ${e.toString()}`,
            'is-danger',

            3000,
          );
          return;
        }
      }
      this.form.attribs = attribs;

      switch (typ) {
        case 'create':
          this.createCampaign();
          break;
        case 'test':
          this.sendTest();
          break;
        default:
          this.updateCampaign();
          break;
      }
    },

    getCampaign(id) {
      return this.$api.getCampaign(id).then((data) => {
        const normalizedSMTP = (data.type === 'regular' && (data.messenger === 'email' || data.messenger?.startsWith('email-')))
          ? this.normalizeSMTPDailyFields(data.dailySendLimit, data.dailyResumeTime)
          : { dailySendLimit: data.dailySendLimit, dailyResumeTime: data.dailyResumeTime };
        const customerLists = Array.isArray(data.customerLists) ? data.customerLists : [];
        const customerPools = Array.isArray(data.customerPools) ? data.customerPools : [];
        const poolAudienceLists = customerPools.reduce((lists, pool) => {
          const poolID = Number(pool.poolId || pool.pool_id);
          if (poolID > 0) {
            lists.push({
              id: poolID,
              name: pool.name || this.$t('campaigns.poolFallback', { id: poolID }),
              type: 'pool',
              poolDeliveryAllowed: true,
            });
          }
          return lists;
        }, []);

        this.data = data;
        const normalizedMessenger = data.messenger?.startsWith('email-') ? 'email' : (data.messenger || 'email');
        this.form = {
          ...this.form,
          ...data,
          // Legacy installations stored one selectable messenger per SMTP
          // server (email-<name>). Strict account SMTP now exposes a single
          // logical `email` messenger backed by the user's enabled pool.
          messenger: normalizedMessenger,
          ...normalizedSMTP,
          // Campaign stats expose public-pool audiences separately from regular
          // lists. Re-add them as selector-compatible items so an existing
          // draft shows its audience and saving it does not drop the pool rows.
          customer_lists: [...customerLists, ...poolAudienceLists],
          headersStr: JSON.stringify(data.headers, null, 4),
          archiveMetaStr: data.archiveMeta ? JSON.stringify(data.archiveMeta, null, 4) : '{}',
          smtpSource: data.smtpSource || 'personal',
          smtpPoolId: data.smtpPoolId || null,
          smtpRateLimit: data.smtpRateLimit || (data.smtpSource === 'organization' ? 100 : 20),
          poolReplyPriority: data.poolReplyPriority || 'contact_first',
          attribsStr: data.attribs ? JSON.stringify(data.attribs, null, 4) : '{}',

          // The structure that is populated by editor input event.
          content: {
            contentType: data.contentType,
            body: data.body,
            bodySource: data.bodySource,
            templateId: data.templateId,
          },
        };
        this.isAttachFieldVisible = this.form.media.length > 0;

        this.form.media = this.form.media.map((f) => {
          if (!f.id && !f.filename?.startsWith('❌ ')) {
            return { ...f, filename: `❌ ${f.filename}` };
          }
          return f;
        });
        if (this.isPlatformPoolCampaign) return this.loadPoolSendStatus();
        return null;
      });
    },

    async sendTest() {
      if (this.$refs.contentEditor && !(await this.$refs.contentEditor.prepareContent())) return false;
      const data = {
        id: this.data.id,
        name: this.form.name,
        subject: this.form.subject,
        customer_list_ids: this.form.customer_lists.map((l) => l.id),
        from_email: this.form.fromEmail,
        daily_send_limit: this.isLimitedSMTPCampaign ? this.form.dailySendLimit : 0,
        daily_resume_time: this.isLimitedSMTPCampaign ? this.form.dailyResumeTime : '09:00',
        messenger: this.form.messenger,
        smtp_source: this.form.smtpSource,
        smtp_rate_limit: this.isSMTPMessenger ? this.form.smtpRateLimit : 0,
        smtp_pool_id: this.campaignSMTPPoolID,
        auto_track_links: this.form.autoTrackLinks,
        type: 'regular',
        headers: this.form.headers,
        tags: this.form.tags,
        template_id: this.form.content.templateId,
        content_type: this.form.content.contentType,
        body: this.form.content.body,
        altbody: this.form.content.contentType !== 'plain' ? this.form.altbody : null,
        customers: this.form.testEmails,
        media: this.form.media.map((m) => m.id),
        visibility: this.form.visibility,
        reply_mailbox_id: this.campaignReplyMailboxID,
        pool_reply_priority: this.form.poolReplyPriority,
      };

      this.$api.testCampaign(data).then(() => {
        this.$utils.toast(this.$t('campaigns.testSent'));
      });
      return false;
    },

    createCampaign() {
      const data = {
        archiveSlug: this.form.subject,
        name: this.form.name,
        subject: this.form.subject,
        customer_list_ids: this.form.customer_lists.map((l) => l.id),
        from_email: this.form.fromEmail,
        daily_send_limit: this.isLimitedSMTPCampaign ? this.form.dailySendLimit : 0,
        daily_resume_time: this.isLimitedSMTPCampaign ? this.form.dailyResumeTime : '09:00',
        content_type: this.form.content.contentType,
        messenger: this.form.messenger,
        smtp_source: this.form.smtpSource,
        smtp_rate_limit: this.isSMTPMessenger ? this.form.smtpRateLimit : 0,
        smtp_pool_id: this.campaignSMTPPoolID,
        auto_track_links: this.form.autoTrackLinks,
        type: 'regular',
        tags: this.form.tags,
        send_at: this.form.sendLater ? this.form.sendAtDate : null,
        headers: this.form.headers,
        attribs: this.form.attribs,
        media: this.form.media.map((m) => m.id),
        visibility: this.form.visibility,
        reply_mailbox_id: this.campaignReplyMailboxID,
        pool_reply_priority: this.form.poolReplyPriority,
        // Platform-level public pool: the audience is every active
        // organization's allocation of the selected first-level pool.
        pool_scope: this.isPlatformPoolCampaign ? 'all_organizations' : 'organization',
      };

      if (!this.$can('mailboxes:use')) {
        delete data.smtp_source;
        delete data.smtp_pool_id;
        delete data.reply_mailbox_id;
      }
      this.$api.createCampaign(data).then((d) => {
        this.$router.push({ name: 'campaign', hash: '#content', params: { id: d.id } });
      });
      return false;
    },

    async updateCampaign(typ) {
      if (this.$refs.contentEditor && !(await this.$refs.contentEditor.prepareContent())) return false;
      const data = {
        archive_slug: this.form.archiveSlug,
        name: this.form.name,
        subject: this.form.subject,
        customer_list_ids: this.form.customer_lists.map((l) => l.id),
        from_email: this.form.fromEmail,
        daily_send_limit: this.isLimitedSMTPCampaign ? this.form.dailySendLimit : 0,
        daily_resume_time: this.isLimitedSMTPCampaign ? this.form.dailyResumeTime : '09:00',
        messenger: this.form.messenger,
        smtp_source: this.form.smtpSource,
        smtp_rate_limit: this.isSMTPMessenger ? this.form.smtpRateLimit : 0,
        smtp_pool_id: this.campaignSMTPPoolID,
        auto_track_links: this.form.autoTrackLinks,
        type: 'regular',
        tags: this.form.tags,
        send_at: this.form.sendLater ? this.form.sendAtDate : null,
        headers: this.form.headers,
        attribs: this.form.attribs,
        template_id: this.form.content.templateId,
        content_type: this.form.content.contentType,
        body: this.form.content.body,
        body_source: this.form.content.bodySource,
        altbody: this.form.content.contentType !== 'plain' ? this.form.altbody : null,
        archive: this.form.archive,
        archive_template_id: this.form.archiveTemplateId,
        archive_meta: this.form.archiveMeta,
        media: this.form.media.map((m) => m.id),
        visibility: this.form.visibility,
        reply_mailbox_id: this.campaignReplyMailboxID,
        pool_reply_priority: this.form.poolReplyPriority,
      };

      let typMsg = 'globals.messages.updated';
      if (typ === 'start') {
        typMsg = 'campaigns.started';
      }

      if (!this.form.sendAtDate) {
        this.form.sendLater = false;
      }

      // This promise is used by startCampaign to first save before starting.
      if (!this.$can('mailboxes:use')) {
        delete data.smtp_source;
        delete data.smtp_pool_id;
        delete data.reply_mailbox_id;
      }
      return this.$api.updateCampaign(this.data.id, data).then(async (d) => {
        this.data = d;
        if (d.messenger) {
          this.form.messenger = d.messenger.startsWith('email-') ? 'email' : d.messenger;
        }
        this.form.archiveSlug = d.archiveSlug;
        this.form.attribsStr = d.attribs ? JSON.stringify(d.attribs, null, 4) : '{}';

        // The server may snapshot media when a visual template is imported
        // (and clears the transient template_id). Sync the returned
        // campaign state back into the form so a second save does not keep
        // submitting the source template/media IDs or create orphan copies.
        if (Array.isArray(d.media)) {
          this.form.media = d.media.map((item) => ({
            ...item,
            ...(item.id || item.filename?.startsWith('❌ ') ? {} : { filename: `❌ ${item.filename}` }),
          }));
          this.isAttachFieldVisible = this.form.media.length > 0;
        }
        this.form.content.templateId = d.templateId || null;
        this.form.content.body = d.body;
        this.form.content.bodySource = d.bodySource;

        this.$utils.toast(this.$t(typMsg, { name: d.name }));
        if (this.isPlatformPoolCampaign) await this.loadPoolSendStatus();
        return true;
      }).catch(() => false);
    },

    onUpdateCampaignArchive() {
      if (!this.canArchive) {
        return;
      }

      let archiveMeta = {};
      try {
        archiveMeta = JSON.parse(this.form.archiveMetaStr);
      } catch (err) {
        // The generic save path validates this field too. Without the guard a
        // broken JSON blob only failed in the browser console: the request was
        // never sent and the page said nothing at all.
        this.$utils.toast(
          this.$t('globals.messages.invalidFields', { name: this.$t('campaigns.archiveMeta') }),
          'is-danger',
        );
        return;
      }

      const data = {
        archive: this.form.archive,
        archive_template_id: this.form.archiveTemplateId,
        archive_meta: archiveMeta,
        archive_slug: this.form.archiveSlug,
      };

      this.$api.updateCampaignArchive(this.data.id, data).then((d) => {
        this.form.archiveSlug = d.archiveSlug;
      });
    },

    // Starts or schedule a campaign.
    startCampaign() {
      if (!this.canStart && !this.canSchedule) {
        return;
      }

      this.$utils.confirm(
        null,
        () => {
          // First save the campaign.
          this.updateCampaign().then((saved) => {
            if (!saved) return;
            // Then start/schedule it.
            let status = '';
            if (this.canStart) {
              status = 'running';
            } else if (this.canSchedule) {
              status = 'scheduled';
            } else {
              return;
            }

            this.$api.changeCampaignStatus(this.data.id, status).then(() => {
              this.$router.push({ name: 'campaigns' });
            });
          });
        },
      );
    },

    unscheduleCampaign() {
      this.$api.changeCampaignStatus(this.data.id, 'draft').then((d) => {
        this.data = d;
      });
    },

    loadPersonalSMTPStatus() {
      if (this.form.smtpSource !== 'organization' || this.isPlatformPoolCampaign) this.form.smtpPoolId = null;
      if (this.isNew && !this.workspace.organizationId && !this.isPlatformPoolCampaign && this.form.smtpSource === 'organization') {
        this.form.smtpSource = 'personal';
      }
      if (!this.$can('mailboxes:use')) return Promise.resolve();
      this.smtpOverviewRequest += 1;
      const request = this.smtpOverviewRequest;
      this.personalSMTPLoaded = false;
      this.smtpSenders = [];
      this.smtpPools = [];
      this.smtpPoolsLoaded = false;
      const poolsPromise = (this.form.smtpSource === 'organization' && this.workspace.organizationId && !this.isPlatformPoolCampaign)
        ? this.$api.getCampaignSMTPPools(this.data.id)
        : Promise.resolve([]);
      return poolsPromise.then((pools) => {
        if (request !== this.smtpOverviewRequest) return null;
        this.smtpPools = pools || [];
        if (this.form.smtpSource === 'organization' && !this.isPlatformPoolCampaign
          && !this.smtpPools.some((pool) => Number(pool.id) === Number(this.form.smtpPoolId))) {
          this.form.smtpPoolId = this.smtpPools.length ? this.smtpPools[0].id : null;
        }
        this.smtpPoolsLoaded = true;
        return this.$api.getCampaignSMTPOverview(this.form.smtpSource, this.data.id, {
          pool_scope: this.isPlatformPoolCampaign ? 'all_organizations' : 'organization',
          customer_list_ids: this.form.customer_lists.map((list) => list.id).join(','),
          smtp_pool_id: this.campaignSMTPPoolID || undefined,
        });
      }).then((rows) => {
        if (request === this.smtpOverviewRequest) this.smtpSenders = rows || [];
      }).catch(() => {
        if (request === this.smtpOverviewRequest) this.smtpSenders = [];
      }).finally(() => {
        if (request === this.smtpOverviewRequest) this.personalSMTPLoaded = true;
      });
    },

    loadPoolSendStatus() {
      this.poolSendStatusRequest += 1;
      const request = this.poolSendStatusRequest;
      const key = this.savedPoolSendSettingsKey;
      this.poolSendStatus = null;
      return this.$api.getCampaignPoolSendStatus(this.data.id).then((data) => {
        if (request !== this.poolSendStatusRequest || key !== this.savedPoolSendSettingsKey) return;
        this.poolSendStatusKey = key;
        this.poolSendStatus = {
          ready: !!(data && data.ready),
          organizations: (data && Array.isArray(data.organizations)) ? data.organizations : [],
          issues: (data && Array.isArray(data.issues)) ? data.issues : [],
        };
      }).catch(() => {
        if (request !== this.poolSendStatusRequest || key !== this.savedPoolSendSettingsKey) return;
        this.poolSendStatusKey = key;
        // Fail closed: a status read failure must not enable delivery.
        this.poolSendStatus = { ready: false, organizations: [], issues: [] };
      });
    },

    loadReplyMailboxes() {
      if (!this.$can('mailboxes:use')) return Promise.resolve();
      this.replyMailboxesLoaded = false;
      return this.$api.getReplyMailboxes().then((data) => {
        this.replyMailboxes = (Array.isArray(data) ? data : []).map((mailbox) => ({
          ...mailbox,
          id: Number(mailbox.id),
        }));
        if (this.isNew && !this.form.replyMailboxId) {
          const defaultMailbox = this.activeReplyMailboxes.find((mailbox) => mailbox.isDefault);
          if (defaultMailbox) this.form.replyMailboxId = defaultMailbox.id;
        }
      }).catch(() => {
        this.replyMailboxes = [];
      }).finally(() => {
        this.replyMailboxesLoaded = true;
      });
    },

    canManageList(customerList) {
      // Campaign audiences are always resolved from a first-level public pool.
      // Organization allocation lists are operational partitions of a pool,
      // not selectable campaign audiences; selecting the parent pool lets the
      // server resolve the correct allocation for the active organization.
      if (customerList.type === 'org_pool_allocation') {
        return false;
      }
      // Public pools expose a delivery capability independently from ordinary
      // customer-list read/manage grants. The backend still enforces the
      // organization grant; this flag only keeps an authorized pool visible
      // in the campaign selector when contact details are unavailable.
      if (customerList.type === 'pool'
        && (customerList.poolDeliveryAllowed || this.$can('campaigns:public_pool_send'))) {
        return true;
      }
      if (!isActiveWorkspaceCustomerList(customerList, this.workspace, this.profile && this.profile.id)) {
        return false;
      }
      return this.$canManageResource(customerList) && this.$canList(customerList.id, 'customer_list:manage');
    },
  },

  computed: {
    archivePreviewMedia() {
      const template = this.templates.find((item) => item.id === this.form.archiveTemplateId);
      return [...this.form.media, ...(template?.media || [])];
    },
    ...mapState(['serverConfig', 'loading', 'customer_lists', 'templates', 'workspace', 'profile']),

    canManage() {
      return this.isNew
        ? this.$canCreateWorkspaceResource('campaigns:manage_all', 'campaigns:manage')
        : this.$canManageResource(this.data, 'campaigns:manage_all', 'campaigns:manage');
    },

    canEdit() {
      return this.canManage && (this.isNew
        || this.data.status === 'draft'
        || this.data.status === 'scheduled'
        || this.data.status === 'paused'
        || this.data.status === 'deferred');
    },

    canSendCampaign() {
      if (!this.canManage || (this.isSMTPMessenger && !this.$can('mailboxes:use'))) {
        return false;
      }
      if (!this.$can('campaigns:send')) {
        return false;
      }
      if (this.isNew) {
        return true;
      }
      // A platform-level public-pool campaign never uses the owner's personal
      // SMTP; the dedicated permission authorizes starting it.
      if (this.isPlatformPoolCampaign) {
        return this.$can('campaigns:public_pool_send');
      }
      const ownerID = Number(this.data.ownerUserId || this.data.owner_user_id) || 0;
      return ownerID > 0 && ownerID === Number(this.profile && this.profile.id);
    },

    canTestCampaign() {
      if (!this.canManage || !this.$can('campaigns:test') || !this.$can('mailboxes:use')) {
        return false;
      }
      if (this.isNew) {
        return true;
      }
      const ownerID = Number(this.data.ownerUserId || this.data.owner_user_id) || 0;
      return ownerID > 0 && ownerID === Number(this.profile && this.profile.id);
    },

    canSchedule() {
      const ownerID = Number(this.data.ownerUserId || this.data.owner_user_id) || 0;
      const sendAuthorized = this.isPlatformPoolCampaign
        ? this.$can('campaigns:public_pool_send')
        : (this.isNew || (ownerID > 0 && ownerID === Number(this.profile && this.profile.id)));
      return this.canManage
        && this.$can('campaigns:schedule')
        && (!this.isSMTPMessenger || this.$can('mailboxes:use'))
        && sendAuthorized
        && (!this.isSMTPMessenger || this.smtpReadyForSend)
        && (this.data.status === 'draft' || this.data.status === 'paused' || this.data.status === 'deferred')
        && (this.form.sendLater && this.form.sendAtDate);
    },

    canUnSchedule() {
      return this.canManage && this.$can('campaigns:control') && this.data.status === 'scheduled';
    },

    canStart() {
      return this.canSendCampaign
        && (!this.isSMTPMessenger || this.smtpReadyForSend)
        && (this.data.status === 'draft' || this.data.status === 'paused' || this.data.status === 'deferred')
        && !this.form.sendLater;
    },

    canArchive() {
      return this.canManage && this.$can('campaigns:control')
        && this.data.status !== 'cancelled' && this.data.type !== 'optin';
    },

    canViewAnalytics() {
      return this.$canViewCampaignAnalytics(this.data);
    },

    availableLists() {
      if (!this.customer_lists.results) {
        return [];
      }
      return this.customer_lists.results.filter((customerList) => this.canManageList(customerList));
    },

    selectedLists() {
      if (this.selCustomerListIDs.length === 0) {
        return [];
      }

      return this.availableLists.filter((customerList) => this.selCustomerListIDs.indexOf(customerList.id) > -1);
    },

    emailMessengers() {
      return ['email'];
    },

    otherMessengers() {
      return this.serverConfig.messengers.filter((m) => m !== 'email' && !m.startsWith('email-'));
    },

    isSMTPMessenger() {
      return this.form.messenger === 'email' || !!this.form.messenger?.startsWith('email-');
    },

    isLimitedSMTPCampaign() {
      return this.isSMTPMessenger && this.data.type !== 'optin';
    },

    // A platform-level public-pool campaign sends through every active
    // organization's member SMTP pool, so its readiness is the per-organization
    // status instead of the caller's personal SMTP.
    isPlatformPoolCampaign() {
      if (this.isNew) {
        return this.$can('campaigns:public_pool_send')
          && this.form.poolScope === 'all_organizations'
          && this.selectedPoolLists.some((list) => list.type === 'pool')
          && this.form.customer_lists.every((list) => list.type === 'pool');
      }
      return (this.data.poolScope || this.data.pool_scope) === 'all_organizations';
    },

    exclusiveAudienceGroups() {
      return this.isNew && this.$can('campaigns:public_pool_send')
        && (!this.workspace.organizationId || this.form.poolScope === 'all_organizations');
    },

    campaignSMTPPoolID() {
      return this.form.smtpSource === 'organization' && !this.isPlatformPoolCampaign ? this.form.smtpPoolId : null;
    },

    poolSendSettingsKey() {
      return JSON.stringify([this.data.id, this.form.smtpSource, this.form.poolReplyPriority,
        [...new Set(this.form.customer_lists.map((list) => Number(list.id)))].sort((a, b) => a - b)]);
    },

    savedPoolSendSettingsKey() {
      const ids = [...(this.data.customerLists || []).map((list) => Number(list.id)),
        ...(this.data.customerPools || []).map((pool) => Number(pool.poolId || pool.pool_id))];
      return JSON.stringify([this.data.id, this.data.smtpSource || 'personal', this.data.poolReplyPriority || 'contact_first',
        [...new Set(ids)].sort((a, b) => a - b)]);
    },

    poolSendStatusCurrent() {
      return this.poolSendStatusKey === this.poolSendSettingsKey ? this.poolSendStatus : null;
    },

    personalSMTPAvailable() {
      return this.personalSMTPLoaded && this.smtpSenders.length > 0;
    },

    smtpReadyForSend() {
      if (!this.isPlatformPoolCampaign) {
        return this.personalSMTPAvailable;
      }
      return !!this.poolSendStatusCurrent && this.poolSendStatusCurrent.ready === true;
    },

    listsLocked() {
      return this.isEditing && this.data.toSend > 0;
    },

    dailyResumeTimeDate: {
      get() {
        const match = /^(\d{1,2}):(\d{2})$/.exec(this.form.dailyResumeTime || '');
        if (!match || Number(match[1]) > 23 || Number(match[2]) > 59) return null;
        // Only local clock fields are used; the API stores a server-time HH:mm value.
        return new Date(2000, 0, 1, Number(match[1]), Number(match[2]));
      },
      set(value) {
        this.form.dailyResumeTime = value ? this.formatResumeTime(value) : '';
      },
    },

    smtpUnavailableMessage() {
      return this.$t(this.form.smtpSource === 'organization' ? 'campaigns.smtpOrganizationUnavailable' : 'campaigns.smtpPersonalUnavailable');
    },

    activeReplyMailboxes() {
      return this.replyMailboxes.filter((mailbox) => mailbox.status === 'active');
    },

    poolRoutingRows() {
      const rows = this.form.customerPools || this.form.customer_pools;
      return Array.isArray(rows) ? rows : [];
    },

    // Pool lists the audience selector currently holds. A new campaign has no
    // server-side routing rows yet, so the editor has to read the local
    // selection too: otherwise it would keep offering the campaign reply
    // mailbox after a pool list was picked.
    selectedPoolLists() {
      const poolTypes = ['pool', 'pool_segment', 'pool_allocation', 'org_pool_allocation'];
      const lists = Array.isArray(this.form.customer_lists) ? this.form.customer_lists : [];
      return lists.filter((list) => list && poolTypes.includes(list.type));
    },

    hasPoolAudience() {
      return this.poolRoutingRows.length > 0 || this.selectedPoolLists.length > 0;
    },

    hasPrivateAudience() {
      const poolTypes = ['pool', 'pool_segment', 'pool_allocation', 'org_pool_allocation'];
      return !this.hasPoolAudience || this.form.customer_lists.some((list) => !poolTypes.includes(list.type));
    },

    // Rows of the read-only routing notice: the resolved rows of a saved
    // campaign, or the locally selected pool lists before the first save. A
    // pending row states that the route is resolved on save/send instead of
    // claiming that the organization is unconfigured.
    poolNoticeRows() {
      if (this.poolRoutingRows.length) {
        return this.poolRoutingRows;
      }
      return this.selectedPoolLists.map((list) => ({
        poolId: list.id,
        name: list.name,
        pending: true,
        replyMailboxEmail: '',
      }));
    },

    hasUnresolvedPoolRoute() {
      return this.poolNoticeRows.some((pool) => (
        !pool.pending && !(pool.replyMailboxEmail || pool.reply_mailbox_email)
      ));
    },

    // Retain the campaign mailbox for private recipients in a mixed audience.
    // Pool recipients always use their independent per-customer route.
    campaignReplyMailboxID() {
      if (!this.hasPrivateAudience) {
        return null;
      }
      return this.form.replyMailboxId || null;
    },
  },

  beforeRouteLeave(to, from, next) {
    if (this.isUnsaved()) {
      this.$utils.confirm(this.$t('globals.messages.confirmDiscard'), () => next(true));
      return;
    }
    next(true);
  },

  watch: {
    'form.smtpSource': function onSMTPSourceChange() { this.form.smtpPoolId = null; this.loadPersonalSMTPStatus(); },
    'form.smtpPoolId': function onSMTPPoolChange() { if (this.form.smtpSource === 'organization') this.loadPersonalSMTPStatus(); },
    'data.id': function onCampaignIDChange() { this.loadPersonalSMTPStatus(); },
    'form.customer_lists': function onAudienceChange() { this.loadPersonalSMTPStatus(); },
    'form.poolScope': function onPoolScopeChange() { this.loadPersonalSMTPStatus(); },
    selectedLists() {
      // This computed value is only for preselecting lists on a new campaign.
      // An edited campaign receives its regular and pool audiences from the
      // API, and must not be reset when the global list store finishes loading.
      if (!this.isEditing) {
        this.form.customer_lists = this.selectedLists;
      }
    },

    // eslint-disable-next-line func-names
    'data.sendAt': function () {
      if (this.data.sendAt !== null) {
        this.form.sendLater = true;
        this.form.sendAtDate = dayjs(this.data.sendAt).toDate();
      } else {
        this.form.sendLater = false;
        this.form.sendAtDate = null;
      }
    },
  },

  mounted() {
    window.onbeforeunload = () => this.isUnsaved() || null;

    // Fill default form fields.
    this.form.fromEmail = this.serverConfig.from_email;
    this.loadPersonalSMTPStatus();
    this.loadReplyMailboxes();
    this.$api.getCustomFields().then((fields) => {
      this.customFields = (fields || []).filter((field) => field.active !== false);
    });

    // New campaign.
    const { id } = this.$route.params;
    if (id === 'new') {
      this.isNew = true;

      if (this.$route.query.customer_list_id) {
        // Multiple customer_list_id query params.
        let strIds = [];
        if (typeof this.$route.query.customer_list_id === 'object') {
          strIds = this.$route.query.customer_list_id;
        } else {
          strIds = [this.$route.query.customer_list_id];
        }

        this.selCustomerListIDs = strIds.map((v) => parseInt(v, 10));
      }
    } else {
      const intID = parseInt(id, 10);
      if (intID <= 0 || Number.isNaN(intID)) {
        this.$utils.toast(this.$t('campaigns.invalid'));
        return;
      }

      this.isEditing = true;
    }

    // Get templates customerList.
    this.$api.getTemplates().then((data) => {
      if (data.length > 0) {
        if (!this.form.templateId) {
          const tpl = data.find((i) => i.isDefault === true);
          if (tpl) {
            this.form.templateId = tpl.id;
          }
        }
      }
    });

    // Fetch campaign.
    if (this.isEditing) {
      this.getCampaign(id).then(() => {
        if (this.$route.hash !== '') {
          const tab = this.$route.hash.replace('#', '');
          const availableTabs = new Set(['campaign', 'content', 'attribs', 'analytics', 'archive']);
          this.activeTab = availableTabs.has(tab) ? tab : 'campaign';
        }
      });
    } else {
      this.form.messenger = 'email';
    }

    this.$nextTick(() => {
      this.$refs.focus.focus();
    });

    this.$events.$on('campaign.update', () => {
      this.onSubmit('update');
    });
  },

  beforeDestroy() {
    this.$events.$off('campaign.update');

    // The unload guard is assigned on mount; without clearing it here the
    // destroyed component kept prompting on every later navigation.
    window.onbeforeunload = null;
  },
});
</script>

<style scoped>
.campaign-toggle-field {
  margin-bottom: 1.25rem;
}

.campaign-toggle-field .label {
  margin-bottom: 0.5rem;
}

.campaign-toggle-field .help {
  margin-top: 0.5rem;
}

.campaign-send-later-row {
  align-items: flex-start;
}

.campaign-send-at-field {
  margin-top: 1.9rem;
}

.template-media {
  margin-bottom: 1.25rem;
}

.template-media-label {
  margin-bottom: 0.5rem;
}

.template-media-tags {
  margin-bottom: 0;
}
</style>
