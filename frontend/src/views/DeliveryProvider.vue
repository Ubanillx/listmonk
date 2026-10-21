<template>
  <section class="provider-console">
    <header class="page-header provider-header">
      <div class="provider-heading">
        <h1 class="title is-4">{{ t('title') }} <b-tag type="is-light">BestEDM / U-MAIL</b-tag></h1>
      </div>
      <div class="provider-actions">
        <b-tag v-if="config.configured" :type="config.verified ? 'is-success is-light' : 'is-light'">
          {{ t(config.verified ? 'lastVerified' : 'notVerified') }}
        </b-tag>
      </div>
    </header>

    <nav class="tabs provider-tabs" :aria-label="t('title')">
      <ul>
        <li v-for="item in tabs" :key="item.key" :class="{ 'is-active': tab === item.key }">
          <a :href="tabHref(item.key)"
            :aria-current="tab === item.key ? 'page' : null" :data-cy="`provider-tab-${item.key}`" @click.prevent="selectTab(item.key)">
            <b-icon :icon="item.icon" size="is-small" />{{ t(item.key === 'account' && !canConfigure ? 'accountInfo' : item.label) }}
          </a>
        </li>
      </ul>
    </nav>

    <div v-if="configError" class="notification is-danger" role="alert">
      {{ configError }}
      <b-button size="is-small" @click="loadConfig">{{ t('retry') }}</b-button>
    </div>

    <div v-if="!config.configured && tab !== 'account' && !configLoading" class="provider-empty provider-panel">
      <b-icon icon="email-plus-outline" size="is-large" />
      <h2>{{ t('connectTitle') }}</h2>
      <p>{{ t(canConfigure ? 'connectHelp' : 'contactAdmin') }}</p>
      <b-button v-if="canConfigure" type="is-primary" @click="selectTab('account')">{{ t('connect') }}</b-button>
    </div>

    <template v-else-if="tab === 'overview' || tab === 'statistics'">
      <form key="provider-report-filter" class="provider-filter" @submit.prevent="run('statistics')">
        <b-field :label="t('startDate')" label-position="on-border" class="provider-date">
          <b-datepicker v-model="startDate" required expanded editable open-on-focus icon-right="calendar-month-outline"
            icon-right-clickable :date-formatter="formatISODate" :date-parser="parseISODate"
            :locale="$i18n.locale" :first-day-of-week="1" :mobile-native="false" :years-range="[-100, 3]"
            data-cy="provider-start" />
        </b-field>
        <b-field :label="t('endDate')" label-position="on-border" class="provider-date">
          <b-datepicker v-model="endDate" required expanded editable open-on-focus icon-right="calendar-month-outline"
            icon-right-clickable :date-formatter="formatISODate" :date-parser="parseISODate" :min-date="startDate"
            :locale="$i18n.locale" :first-day-of-week="1" :mobile-native="false" :years-range="[-100, 3]"
            data-cy="provider-end" />
        </b-field>
        <b-field v-if="tab === 'statistics'" :label="t('batch')" label-position="on-border" class="provider-batch-field">
          <b-select :value="selectedBatch" expanded :disabled="busy.statistics || !loaded.statistics"
            data-cy="provider-batch-select" @input="selectBatch">
            <option value="">{{ t('allBatches') }}</option>
            <option v-for="batch in batchOptions" :key="batch" :value="batch">{{ batch }}</option>
          </b-select>
        </b-field>
        <div class="provider-filter-actions">
          <b-button native-type="submit" type="is-primary" icon-left="magnify" :loading="busy.statistics">{{ t('query') }}</b-button>
          <b-button type="is-light" @click="recentWeek">{{ t('last7') }}</b-button>
          <b-button v-if="tab === 'statistics'" class="provider-export" icon-left="download"
            :disabled="!loaded.failures || busy.failures || busy.statistics" :loading="exporting" @click="exportFailures">
            {{ t('export') }}
          </b-button>
        </div>
      </form>

      <div v-if="errors.statistics" class="notification is-danger" role="alert">{{ errors.statistics }}</div>
      <div v-if="errors.account && tab === 'overview'" class="notification is-warning" role="alert">{{ errors.account }}</div>

      <p class="provider-report-scope provider-muted">{{ t(selectedBatch && tab === 'statistics' ? 'currentBatch' : 'dateScope') }} {{ metricScope }}</p>
      <div class="provider-metrics" :class="{ 'provider-analysis-metrics': tab === 'statistics' }" :aria-busy="busy.statistics">
        <article v-if="tab === 'overview'" class="box provider-metric quota">
          <p class="heading">{{ t('quota') }}</p>
          <strong class="title is-5">{{ number(service.qty_available) }}</strong>
        </article>
        <article class="box provider-metric sent">
          <p class="heading">{{ t('sent') }}</p>
          <strong class="title is-5" data-cy="provider-total">{{ number(total('total')) }}</strong>
        </article>
        <article class="box provider-metric success">
          <p class="heading">{{ t('delivered') }}</p>
          <strong class="title is-5">{{ number(total('success')) }}</strong>
        </article>
        <article class="box provider-metric danger">
          <p class="heading">{{ t('failed') }}</p>
          <strong class="title is-5">{{ number(total('failed')) }}</strong>
        </article>
        <article v-if="tab === 'statistics'" class="box provider-metric invalid">
          <p class="heading">{{ t('invalidAddresses') }}</p>
          <strong class="title is-5">{{ number(total('invalid')) }}</strong>
        </article>
      </div>

      <div v-if="tab === 'overview'" class="provider-overview-grid">
        <article class="provider-panel provider-chart-panel">
          <div class="provider-panel-heading">
            <h2>{{ t('trend') }}</h2>
            <span class="provider-muted">{{ t('latestDates') }}</span>
          </div>
          <div v-if="daily.length" class="provider-chart" role="img" :aria-label="t('trend')">
            <chart type="line" :data="trendData" />
          </div>
          <div v-else class="provider-inline-empty">{{ busy.statistics ? t('loading') : t('noData') }}</div>
          <p class="provider-footnote">{{ t('metricHelp') }}</p>
        </article>

        <article class="provider-panel provider-account-summary">
          <div class="provider-panel-heading">
            <h2>{{ t('accountStatus') }}</h2>
            <b-tag :type="service.service_status === 'enabled' ? 'is-success is-light' : 'is-light'">{{ serviceStatus }}</b-tag>
          </div>
          <h3>{{ customer.company || customer.username || 'BestEDM' }}</h3>
          <p class="provider-muted">{{ customer.username || '—' }}</p>
          <dl>
            <dt>{{ t('expiry') }}</dt>
            <dd>{{ service.expiration_date || '—' }}</dd>
            <dt>{{ t('serviceType') }}</dt>
            <dd>{{ serviceType }}</dd>
            <dt>{{ t('sendType') }}</dt>
            <dd>{{ sendType }}</dd>
            <dt>{{ t('contact') }}</dt>
            <dd>{{ customer.contact || '—' }}</dd>
          </dl>
          <button type="button" class="provider-batch" @click="selectTab('account')">{{ t('viewAccount') }} →</button>
        </article>
      </div>

      <article v-if="tab === 'overview'" class="provider-panel">
        <div class="provider-panel-heading">
          <h2>{{ t('statistics') }} <span class="provider-count">{{ statistics.length }}</span></h2>
          <span class="provider-muted">{{ timestamps.statistics ? `${t('updated')} ${time(timestamps.statistics)}` : t('notQueried') }}</span>
        </div>
        <div class="provider-table-scroll">
          <table class="table is-fullwidth provider-table">
            <thead>
              <tr>
                <th>{{ t('date') }}</th>
                <th>{{ t('batch') }}</th>
                <th class="is-numeric">{{ t('sent') }}</th>
                <th class="is-numeric">{{ t('delivered') }}</th>
                <th class="is-numeric">{{ t('failed') }}</th>
                <th class="is-numeric">{{ t('invalid') }}</th>
                <th class="is-numeric">{{ t('points') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(row, index) in statisticsPage" :key="`${row.task_id}-${row.task_date}-${index}`">
                <td>{{ row.task_date }}</td>
                <td>
                  <button type="button" class="provider-batch" :title="row.task_ident" @click="useBatch(row)">{{ row.task_ident || '—' }}</button>
                </td>
                <td class="is-numeric provider-sent">{{ number(row.total) }}</td>
                <td class="is-numeric provider-success">{{ number(row.success) }}</td>
                <td class="is-numeric provider-danger">{{ number(row.failed) }}</td>
                <td class="is-numeric provider-invalid">{{ number(row.invalid) }}</td>
                <td class="is-numeric provider-points">{{ number(row.actual) }}</td>
              </tr>
              <tr v-if="!statistics.length">
                <td colspan="7" class="provider-inline-empty">{{ busy.statistics ? t('loading') : t('noData') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <app-pagination v-if="statistics.length" :total="statistics.length" :current="pages.statistics" :per-page="sizes.statistics"
          @change="pages.statistics = $event" @per-page-change="changeSize('statistics', $event)" />
      </article>

      <article v-else class="provider-panel provider-analysis" data-cy="provider-analysis">
        <section class="provider-batches" :aria-label="t('batchStatistics')">
          <div class="provider-panel-heading">
            <h2>{{ t('batchStatistics') }} <span class="provider-count">{{ batchGroups.length }}</span></h2>
            <span class="provider-muted">{{ t('selectBatchHelp') }}</span>
          </div>
          <div class="provider-table-scroll">
            <table class="table is-fullwidth provider-table provider-batch-table">
              <thead>
                <tr>
                  <th>{{ t('date') }}</th>
                  <th class="is-numeric">{{ t('sent') }}</th>
                  <th class="is-numeric">{{ t('delivered') }}</th>
                  <th class="is-numeric">{{ t('failed') }}</th>
                </tr>
              </thead>
              <tbody v-for="group in batchGroupsPage" :key="group.key" class="provider-batch-group"
                :class="{ 'is-selected-batch': selectedBatch && selectedBatch === group.task_ident }" :data-batch="group.task_ident">
                <tr class="provider-batch-group-heading" :class="{ 'is-clickable-batch': group.task_ident }" @click="useBatch(group)">
                  <th colspan="4" scope="rowgroup">
                    <div class="provider-batch-group-title">
                      <button type="button" class="provider-batch" :disabled="!group.task_ident"
                        :aria-pressed="Boolean(selectedBatch && selectedBatch === group.task_ident)" @click.stop="useBatch(group)">
                        <b-icon icon="folder-outline" size="is-small" />{{ group.task_ident || '—' }}
                      </button>
                      <span class="provider-muted">{{ group.rows.length }} {{ t('batchRecords') }}</span>
                    </div>
                  </th>
                </tr>
                <tr v-for="(row, index) in group.rows" :key="`${row.task_id}-${row.task_date}-${index}`" class="provider-batch-record"
                  :class="{ 'is-clickable-batch': group.task_ident }" @click="useBatch(group)">
                  <td>
                    <time>{{ row.task_date || '—' }}</time>
                  </td>
                  <td class="is-numeric provider-sent">{{ number(row.total) }}</td>
                  <td class="is-numeric provider-success">{{ number(row.success) }}</td>
                  <td class="is-numeric provider-danger">{{ number(row.failed) }}</td>
                </tr>
              </tbody>
              <tbody v-if="!batchGroups.length">
                <tr>
                  <td colspan="4" class="provider-inline-empty">{{ busy.statistics ? t('loading') : t('noData') }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <app-pagination v-if="batchGroups.length" :total="batchGroups.length" :current="pages.batches"
            :per-page="sizes.batches" @change="pages.batches = $event"
            @per-page-change="changeSize('batches', $event)" />
        </section>

        <section class="provider-failures" :aria-label="t('failureDetails')" :aria-busy="busy.failures" data-cy="provider-failures">
          <div class="provider-panel-heading">
            <h2>{{ t('failureDetails') }} <span class="provider-count">{{ filteredFailures.length }}</span></h2>
            <span class="provider-muted">{{ timestamps.failures ? `${t('updated')} ${time(timestamps.failures)}` : '' }}</span>
          </div>
          <div class="provider-selection" data-cy="provider-selection">
            <b-icon icon="information-outline" size="is-small" />
            <span>{{ t(selectedBatch ? 'currentBatch' : 'dateScope') }} <strong>{{ selectedBatch || appliedLabel }}</strong></span>
            <button v-if="selectedBatch" type="button" class="provider-batch" @click="selectBatch('')">{{ t('clearBatch') }}</button>
          </div>
          <div class="provider-failure-toolbar">
            <b-field :label="t('errorType')" class="provider-error-type">
              <b-select v-model="failureFilters.type" :disabled="busy.statistics || !loaded.statistics"
                data-cy="provider-error-type" @input="refreshFailures">
                <option value="-1">{{ t('allErrors') }}</option>
                <option v-for="i in 10" :key="i" :value="String(i)">{{ t(`error${i}`) }}</option>
              </b-select>
            </b-field>
            <b-field :label="t('searchEmail')" class="provider-email-search">
              <b-input v-model="failureSearch" icon="magnify" :placeholder="t('searchEmail')" maxlength="320"
                data-cy="provider-failure-search" @input="pages.failures = 1" />
            </b-field>
          </div>
          <div v-if="errors.failures" class="notification is-danger" role="alert">
            {{ errors.failures }}
            <b-button size="is-small" @click="refreshFailures">{{ t('retry') }}</b-button>
          </div>
          <div v-if="selectedFailures.length" class="provider-batch-bar" data-cy="provider-batch-bar">
            <span class="provider-batch-count">{{ $t('deliveryProvider.batchSelected', { num: selectedFailures.length }) }}</span>
            <button type="button" class="provider-batch" :disabled="!canBatchBlock"
              data-cy="provider-batch-block" @click="openBatchBlocklist('block')">
              {{ t('batchBlocklist') }}
            </button>
            <button type="button" class="provider-batch" :disabled="!canBatchUnblock"
              data-cy="provider-batch-unblock" @click="openBatchBlocklist('unblock')">
              {{ t('batchUnblocklist') }}
            </button>
            <button type="button" class="provider-batch" data-cy="provider-batch-clear" @click="clearFailureSelection">
              {{ t('clearSelection') }}
            </button>
          </div>
          <div class="provider-table-scroll">
            <table class="table is-fullwidth provider-table provider-failure-table">
              <thead>
                <tr>
                  <th class="provider-select-cell">
                    <input type="checkbox" :checked="allFilteredSelected" :indeterminate.prop="someFilteredSelected"
                      :aria-label="$t('deliveryProvider.selectAllFiltered')" data-cy="provider-select-all"
                      @change="toggleAllFiltered($event.target.checked)">
                  </th>
                  <th>{{ t('email') }}</th>
                  <th>{{ t('errorType') }}</th>
                  <th class="is-numeric provider-error-count-cell">{{ t('errorCount') }}</th>
                  <th>{{ t('actions') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(row, index) in failuresPage" :key="index">
                  <td class="provider-select-cell">
                    <input type="checkbox" :checked="isFailureSelected(row)"
                      :aria-label="$t('deliveryProvider.selectAddress', { email: row.email })" data-cy="provider-select-address"
                      @change="toggleFailure(row)">
                  </td>
                  <td>{{ row.email }}</td>
                  <td>
                    <div v-for="(cause, causeIndex) in row.errors" :key="causeIndex" class="provider-failure-cause">
                      <b-tag :class="failureColor(cause.error_type)">
                        {{ failureType(cause.error_type) }}<template v-if="Number(cause.error_count) > 1"> × {{ number(cause.error_count) }}</template>
                      </b-tag>
                      <small v-if="cause.error_type_dec && cause.error_type_dec !== failureType(cause.error_type)"
                        class="provider-record-id">{{ cause.error_type_dec }}</small>
                    </div>
                  </td>
                  <td class="is-numeric provider-error-count provider-error-count-cell">{{ number(row.error_count) }}</td>
                  <td class="provider-failure-actions">
                    <icon-tip :label="t('viewLogs')" icon="text-box-search-outline"
                      data-cy="provider-failure-logs" @click="prepareLog(row)" />
                    <icon-tip :label="blocklistLabel(row)"
                      :icon="allBlocked(row) ? 'lock-open-outline' : 'account-cancel-outline'"
                      :blocked="allBlocked(row)" :disabled="!canToggleBlocklist(row)"
                      data-cy="provider-failure-blocklist" @click="openBlocklist(row)" />
                  </td>
                </tr>
                <tr v-if="!filteredFailures.length">
                  <td colspan="5" class="provider-inline-empty">{{ busy.failures ? t('loading') : loaded.failures ? t('noData') : t('queryFirst') }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <app-pagination v-if="filteredFailures.length" :total="filteredFailures.length" :current="pages.failures"
            :per-page="sizes.failures" @change="pages.failures = $event"
            @per-page-change="changeSize('failures', $event)" />
        </section>
        <p class="provider-footnote provider-analysis-note">
          {{ t('failureCountHelp') }}<br />{{ t('smtpDateHelp') }}
          <button type="button" class="provider-batch" :disabled="busy.statistics || !loaded.statistics" @click="selectBatch('')">
            {{ t('viewByDate') }} →
          </button>
        </p>
      </article>
    </template>

    <template v-else-if="tab === 'logs'">
      <form key="provider-log-filter" class="provider-filter" @submit.prevent="run('logs')">
        <b-field :label="t('date')" label-position="on-border" class="provider-date">
          <b-datepicker v-model="logDate" required expanded editable open-on-focus icon-right="calendar-month-outline"
            icon-right-clickable :date-formatter="formatISODate" :date-parser="parseISODate"
            :locale="$i18n.locale" :first-day-of-week="1" :mobile-native="false" :years-range="[-100, 3]"
            data-cy="provider-log-date" />
        </b-field>
        <b-field :label="t('recipient')" label-position="on-border" class="provider-recipient">
          <b-input v-model="logFilters.mail_to" type="email" required placeholder="recipient@example.com" maxlength="320" />
        </b-field>
        <b-field :label="t('batch')" label-position="on-border" class="provider-batch-field">
          <b-select v-model="logFilters.task_ident" data-cy="provider-log-batch">
            <option value="">{{ t('allBatches') }}</option>
            <option v-for="batch in batchOptions" :key="batch" :value="batch">{{ batch }}</option>
          </b-select>
        </b-field>
        <div class="provider-filter-actions">
          <b-button native-type="submit" type="is-primary" :loading="busy.logs" icon-left="magnify">{{ t('queryLogs') }}</b-button>
        </div>
      </form>
      <div v-if="errors.logs" class="notification is-danger" role="alert">{{ errors.logs }}</div>
      <article v-if="logs.length" class="provider-panel">
        <div class="provider-panel-heading">
          <h2>{{ t('logResults') }}</h2>
          <span class="provider-muted">{{ t('providerTime') }}</span>
        </div>
        <div v-for="(row, index) in logsPage" :key="index" class="provider-log">
          <div class="provider-log-head">
            <b-tag type="is-info">{{ row.code == null ? '—' : row.code }}</b-tag>
            <strong>{{ row.mail_to }}</strong>
            <time>{{ row.send_time }}</time>
          </div>
          <dl>
            <dt>{{ t('sender') }}</dt>
            <dd>{{ row.mail_from || '—' }}</dd>
            <dt>{{ t('batch') }}</dt>
            <dd>{{ row.task_ident || '—' }}</dd>
          </dl>
          <pre>{{ row.detail || '—' }}</pre>
          <p>{{ row['reason/suggestion'] || row.reason || row.suggestion || '—' }}</p>
        </div>
        <app-pagination :total="logs.length" :current="pages.logs" :per-page="sizes.logs"
          @change="pages.logs = $event" @per-page-change="changeSize('logs', $event)" />
      </article>
      <div v-else class="provider-panel provider-inline-empty">
        <span v-if="busy.logs">{{ t('loading') }}</span>
        <span v-else-if="loaded.logs">{{ t('logNoRows') }}<br /><small class="provider-muted">{{ t('logNoRowsHelp') }}</small></span>
        <span v-else>{{ t('logEmpty') }}</span>
      </div>
    </template>

    <template v-else-if="tab === 'account'">
      <div class="provider-account-grid" :class="{ 'is-read-only': !canConfigure }">
        <article v-if="canConfigure" class="provider-panel" data-cy="provider-configuration">
          <div class="provider-panel-heading">
            <h2>{{ t('connection') }}</h2>
            <b-tag :type="config.verified ? 'is-success is-light' : 'is-light'">{{ t(config.verified ? 'lastVerified' : 'notVerified') }}</b-tag>
          </div>
          <p class="provider-muted">{{ t('credentialHelp') }}</p>
          <form class="provider-credentials" @submit.prevent="saveAndConnect">
            <b-field :label="t('username')">
              <b-input v-model="credentials.username" required maxlength="320" autocomplete="off" data-cy="provider-username" />
            </b-field>
            <b-field :label="t('password')" :message="config.has_password ? t('keepPassword') : ''">
              <b-input v-model="credentials.password" type="password" :required="!config.has_password" maxlength="2048"
                autocomplete="new-password" data-cy="provider-password" />
            </b-field>
            <div class="provider-actions">
              <b-button type="is-primary" native-type="submit" :loading="saving || testing" :disabled="configLoading"
                data-cy="provider-connect">
{{ t('saveConnect') }}
</b-button>
              <b-button :disabled="!config.configured || saving || testing" :loading="testing" @click="testConnection">
                {{ t('testSaved') }}
              </b-button>
            </div>
          </form>
          <div v-if="connectionMessage || config.test_error" class="provider-test-result"
            :class="{ failure: connectionError || config.test_error }" role="status" data-cy="provider-connection-result">
            {{ connectionMessage || config.test_error }}
          </div>
          <p v-if="config.tested_at" class="provider-muted">{{ t('lastCheck') }} {{ time(config.tested_at) }}</p>
          <p class="provider-footnote">{{ t('smtpConfigHelp') }}</p>
        </article>
        <article class="provider-panel">
          <div class="provider-panel-heading">
            <h2>{{ t('accountInfo') }}</h2>
            <b-button size="is-small" :loading="busy.account" :disabled="!config.configured" @click="loadAccount">{{ t('refresh') }}</b-button>
          </div>
          <div v-if="errors.account" class="notification is-warning">{{ errors.account }}</div>
          <h3>{{ customer.company || 'BestEDM' }}</h3>
          <dl>
            <dt>{{ t('username') }}</dt>
            <dd>{{ customer.username || '—' }}</dd>
            <dt>{{ t('userId') }}</dt>
            <dd>{{ customer.id || '—' }}</dd>
            <dt>{{ t('contact') }}</dt>
            <dd>{{ customer.contact || '—' }}</dd>
            <dt>{{ t('email') }}</dt>
            <dd>{{ customer.email || '—' }}</dd>
            <dt>{{ t('phone') }}</dt>
            <dd>{{ customer.phone || customer.mobile || '—' }}</dd>
            <dt>{{ t('accountStatus') }}</dt>
            <dd>{{ serviceStatus }}</dd>
            <dt>{{ t('quota') }}</dt>
            <dd>{{ number(service.qty_available) }}</dd>
            <dt>{{ t('purchased') }}</dt>
            <dd>{{ number(service.qty_buytotal) }}</dd>
            <dt>{{ t('expiry') }}</dt>
            <dd>{{ service.expiration_date || '—' }}</dd>
            <dt>{{ t('serviceType') }}</dt>
            <dd>{{ serviceType }}</dd>
            <dt>{{ t('sendType') }}</dt>
            <dd>{{ sendType }}</dd>
          </dl>
        </article>
      </div>
    </template>

    <b-modal scroll="keep" :aria-modal="true" :active.sync="blocklistDialog.open" :width="520" has-modal-card
      custom-content-class="provider-blocklist-modal" :can-cancel="!blocklistDialog.busy" @close="resetBlocklistDialog">
      <div class="modal-card" role="dialog" aria-labelledby="provider-blocklist-title" data-cy="provider-blocklist-dialog">
        <header class="modal-card-head">
          <h2 id="provider-blocklist-title" class="modal-card-title">
            {{ t(dialogTitleKey) }}
          </h2>
        </header>
        <section class="modal-card-body">
          <p v-if="blocklistDialog.rows.length === 1" class="provider-blocklist-email">{{ blocklistDialog.rows[0].email }}</p>
          <p v-else class="provider-blocklist-email">{{ $t('deliveryProvider.batchAddresses', { num: blocklistDialog.rows.length }) }}</p>

          <div class="provider-scope" role="radiogroup" :aria-label="$t('deliveryProvider.blocklistScope')">
            <label v-for="option in scopeOptions" :key="option.value" class="provider-scope-option"
              :class="{ 'is-active': blocklistDialog.scope === option.value, 'is-empty': !option.count }">
              <input type="radio" :value="option.value" v-model="blocklistDialog.scope" :disabled="!option.count"
                :data-cy="`provider-scope-${option.value}`">
              <span>{{ option.label }}</span>
              <small>{{ option.count }}</small>
            </label>
          </div>

          <ul v-if="blocklistDialog.rows.length === 1" class="provider-blocklist-targets">
            <li v-for="(match, index) in dialogScopeTargets" :key="index"
              :class="{ 'is-skipped': targetState(match).skip }">
              <b-tag type="is-light">{{ t(match.kind === 'pool' ? 'matchPool' : 'matchPrivate') }}</b-tag>
              <span class="provider-blocklist-name">{{ match.name || match.customer_code || '—' }}</span>
              <small v-if="match.pool_name" class="provider-muted">{{ match.pool_name }}</small>
              <small class="provider-blocklist-state">{{ targetState(match).label }}</small>
            </li>
          </ul>
          <ul v-else class="provider-blocklist-targets provider-blocklist-addresses">
            <li v-for="row in blocklistDialog.rows" :key="row.email">
              <span class="provider-blocklist-address">{{ row.email }}</span>
              <small class="provider-blocklist-state">{{ addressState(row) }}</small>
            </li>
          </ul>

          <p v-if="blocklistDialog.action === 'block' && hasPoolTargets" class="provider-notice provider-blocklist-warning">
            {{ t('blocklistPoolWarning') }}
          </p>
          <p v-if="blocklistDialog.error" class="notification is-danger" role="alert">{{ blocklistDialog.error }}</p>
        </section>
        <footer class="modal-card-foot">
          <div class="buttons mb-0">
            <b-button :disabled="blocklistDialog.busy" @click="resetBlocklistDialog">{{ $t('globals.buttons.cancel') }}</b-button>
            <b-button type="is-primary" :loading="blocklistDialog.busy" :disabled="!blocklistApplicable"
              data-cy="provider-blocklist-confirm" @click="confirmBlocklist">
              {{ $t(blocklistDialog.action === 'unblock' ? 'deliveryProvider.unblocklistConfirm' : 'deliveryProvider.blocklistConfirm', { num: blocklistApplicable }) }}
            </b-button>
          </div>
        </footer>
      </div>
    </b-modal>
  </section>
</template>

<script>
import { now } from '../time';
import Chart from '../components/Chart.vue';
import IconTip from '../components/IconTip.vue';
import { colors } from '../constants';
import providerBatches from '../utils/providerBatches';

const tabs = [
  { key: 'overview', icon: 'view-dashboard-outline', label: 'overview' },
  { key: 'statistics', icon: 'chart-box-outline', label: 'analysis' },
  { key: 'logs', icon: 'text-box-search-outline', label: 'logs' },
  { key: 'account', icon: 'account-cog-outline', label: 'account' },
];
const errorMessage = (error) => error?.response?.data?.message || '请求失败，请稍后重试';
const numeric = (value) => {
  if (value == null || value === '' || !Number.isFinite(Number(value)) || Number(value) < 0) return null;
  return Number(value);
};
const dateFromISO = (value) => {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value || '');
  if (!match) return null;
  const [, year, month, day] = match;
  const date = new Date(Number(year), Number(month) - 1, Number(day));
  return date.getFullYear() === Number(year) && date.getMonth() === Number(month) - 1 && date.getDate() === Number(day)
    ? date : null;
};
const isoFromDate = (date) => (date instanceof Date && !Number.isNaN(date.getTime())
  ? `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
  : '');

export default {
  components: { Chart, IconTip },
  data() {
    const date = now().format('YYYY-MM-DD');
    const start = now().subtract(6, 'day').format('YYYY-MM-DD');
    return {
      tabs,
      config: {},
      configError: '',
      configLoading: false,
      credentials: { username: '', password: '' },
      customer: {},
      service: {},
      statistics: [],
      failures: [],
      logs: [],
      saving: false,
      testing: false,
      exporting: false,
      connectionMessage: '',
      connectionError: false,
      generation: 0,
      sequence: {
        account: 0, statistics: 0, failures: 0, logs: 0,
      },
      busy: {
        account: false, statistics: false, failures: false, logs: false,
      },
      errors: {
        account: '', statistics: '', failures: '', logs: '',
      },
      loaded: { statistics: false, failures: false, logs: false },
      timestamps: {},
      applied: {},
      pages: {
        statistics: 1, batches: 1, failures: 1, logs: 1,
      },
      sizes: {
        statistics: 10, batches: 10, failures: 10, logs: 10,
      },
      filters: { date: start, date_end: date },
      selectedBatch: '',
      failureSearch: '',
      failureFilters: {
        date: start, date_end: date, type: '-1', task_ident: '', group_by: 'email',
      },
      logFilters: { date, mail_to: '', task_ident: '' },
      selectedFailures: [],
      blocklistDialog: {
        open: false, rows: [], action: 'block', scope: 'all', busy: false, error: '',
      },
    };
  },
  computed: {
    canConfigure() { return Number(this.$store.state.profile?.userRole?.id) === 1; },
    startDate: {
      get() { return dateFromISO(this.filters.date); },
      set(value) {
        this.filters.date = isoFromDate(value);
        if (this.filters.date && this.filters.date_end && this.filters.date_end < this.filters.date) {
          this.filters.date_end = this.filters.date;
        }
      },
    },
    endDate: {
      get() { return dateFromISO(this.filters.date_end); },
      set(value) { this.filters.date_end = isoFromDate(value); },
    },
    logDate: {
      get() { return dateFromISO(this.logFilters.date); },
      set(value) { this.logFilters.date = isoFromDate(value); },
    },
    tab() { if (this.$route.query.tab === 'failures') return 'statistics'; return tabs.some((item) => item.key === this.$route.query.tab) ? this.$route.query.tab : 'overview'; },
    statisticsPage() { return this.slice('statistics'); },
    batchGroups() { return providerBatches(this.statistics); },
    batchGroupsPage() { const start = (this.pages.batches - 1) * this.sizes.batches; return this.batchGroups.slice(start, start + this.sizes.batches); },
    failuresPage() { const start = (this.pages.failures - 1) * this.sizes.failures; return this.filteredFailures.slice(start, start + this.sizes.failures); },
    filteredFailures() { const search = this.failureSearch.trim().toLowerCase(); return this.failures.filter((row) => (row.email || '').toLowerCase().includes(search)); },
    selectedFailureRows() {
      return this.failures.filter((row) => this.isFailureSelected(row));
    },
    allFilteredSelected() {
      return this.filteredFailures.length > 0 && this.filteredFailures.every((row) => this.isFailureSelected(row));
    },
    someFilteredSelected() {
      const count = this.filteredFailures.filter((row) => this.isFailureSelected(row)).length;
      return count > 0 && count < this.filteredFailures.length;
    },
    selectedTargets() {
      return this.selectedFailureRows.flatMap((row) => (row.matches || []));
    },
    canBatchBlock() { return this.selectedTargets.some((match) => match.can_block && !match.blocked); },
    canBatchUnblock() { return this.selectedTargets.some((match) => match.can_block && match.blocked); },
    dialogTargets() {
      return this.blocklistDialog.rows.flatMap((row) => (row.matches || []).map((match) => ({ ...match, email: row.email })));
    },
    dialogScopeTargets() {
      const { scope } = this.blocklistDialog;
      return this.dialogTargets.filter((match) => scope === 'all' || match.kind === scope);
    },
    scopeOptions() {
      const targets = this.dialogTargets;
      return [
        { value: 'all', label: this.$t('deliveryProvider.scopeAll'), count: targets.length },
        { value: 'private', label: this.$t('deliveryProvider.matchPrivate'), count: targets.filter((match) => match.kind === 'private').length },
        { value: 'pool', label: this.$t('deliveryProvider.matchPool'), count: targets.filter((match) => match.kind === 'pool').length },
      ];
    },
    blocklistApplicable() {
      return this.dialogScopeTargets.filter((match) => !this.targetState(match).skip).length;
    },
    hasPoolTargets() { return this.dialogScopeTargets.some((match) => match.kind === 'pool'); },
    dialogTitleKey() {
      const batch = this.blocklistDialog.rows.length > 1;
      if (this.blocklistDialog.action === 'unblock') return batch ? 'batchUnblocklistTitle' : 'unblocklistTitle';
      return batch ? 'batchBlocklistTitle' : 'blocklistTitle';
    },
    batchOptions() { return this.batchGroups.map((group) => group.task_ident).filter(Boolean); },
    scopedStatistics() { return this.tab === 'statistics' && this.selectedBatch ? this.statistics.filter((row) => row.task_ident === this.selectedBatch) : this.statistics; },
    logsPage() { return this.slice('logs'); },
    serviceStatus() { return this.t(({ enabled: 'enabled', disabled: 'disabled' })[this.service.service_status] || 'unknown'); },
    serviceType() { return this.t(({ all: 'totalQuota', day: 'dailyQuota' })[this.service.service_type] || 'unknown'); },
    sendType() { return this.t(({ remote: 'remote', local: 'local' })[this.service.send_type] || 'unknown'); },
    appliedLabel() {
      const params = this.applied.statistics; if (!params) return this.t('notQueried');
      return params.date === params.date_end ? params.date : `${params.date} — ${params.date_end}`;
    },
    // The headline multiplier under "sent": the batch when one is selected,
    // otherwise the applied date range.
    metricScope() {
      if (this.tab === 'statistics' && this.selectedBatch) return this.selectedBatch;
      return this.appliedLabel;
    },
    daily() {
      const grouped = {};
      this.statistics.forEach((row) => { const value = numeric(row.total); if (value != null && row.task_date) grouped[row.task_date] = (grouped[row.task_date] || 0) + value; });
      return Object.keys(grouped).sort().slice(-30).map((date) => ({ date, value: grouped[date] }));
    },
    trendData() {
      return {
        labels: this.daily.map((day) => day.date),
        datasets: [{
          label: this.t('sent'),
          data: this.daily.map((day) => day.value),
          borderColor: colors.primary,
          backgroundColor: colors.primarySoft,
          borderWidth: 2,
          pointRadius: 3,
          fill: true,
          tension: 0,
        }],
      };
    },
  },
  watch: {
    tab(value) { if (value === 'statistics' && this.loaded.statistics && !this.loaded.failures && !this.busy.failures) this.refreshFailures(); },
  },
  mounted() { this.initialize(); },
  beforeDestroy() { this.generation += 1; },
  methods: {
    t(key) { return this.$t(`deliveryProvider.${key}`); },
    formatISODate(date) { return isoFromDate(date); },
    parseISODate(value) { return dateFromISO(value); },
    time(value) { return this.$utils.niceDate(value, true); },
    number(value) { const n = numeric(value); return n == null ? '—' : this.$utils.formatNumber(n); },
    total(key) {
      if (!this.loaded.statistics) return null;
      // An empty result set means nothing was sent: that is a zero, not an
      // unknown. Otherwise sum the records that carry the field, so one batch
      // omitting a counter cannot blank out the whole headline.
      if (!this.scopedStatistics.length) return 0;
      const values = this.scopedStatistics.map((row) => numeric(row[key])).filter((value) => value != null);
      return values.length ? values.reduce((sum, value) => sum + value, 0) : null;
    },
    slice(kind) { const start = (this.pages[kind] - 1) * this.sizes[kind]; return this[kind].slice(start, start + this.sizes[kind]); },
    changeSize(kind, size) { this.sizes[kind] = size; this.pages[kind] = 1; },
    selectTab(tab) { if (this.tab === tab) return; this.$router.replace({ query: { ...this.$route.query, tab } }); },
    tabHref(tab) { return this.$router.resolve({ query: { ...this.$route.query, tab } }).href; },
    resetReports() {
      this.generation += 1; this.customer = {}; this.service = {}; this.statistics = []; this.failures = []; this.logs = [];
      this.selectedBatch = ''; this.failureSearch = ''; this.logFilters.task_ident = '';
      this.applied = {}; this.timestamps = {}; this.connectionMessage = ''; this.connectionError = false;
      Object.keys(this.busy).forEach((key) => { this.busy[key] = false; this.errors[key] = ''; });
      Object.keys(this.loaded).forEach((key) => { this.loaded[key] = false; });
      Object.keys(this.pages).forEach((key) => { this.pages[key] = 1; });
    },
    async loadConfig() {
      this.configLoading = true; this.configError = '';
      try {
        this.config = await this.$api.getDeliveryProviderConfig();
        this.credentials.username = this.config.username || '';
      } catch (error) { this.configError = errorMessage(error); } finally { this.configLoading = false; }
    },
    async initialize() {
      const { generation } = this; await this.loadConfig(); if (generation !== this.generation) return;
      if (this.config.configured) { this.loadAccount(); this.run('statistics'); }
    },
    async loadAccount() {
      const { generation } = this; this.sequence.account += 1; const sequence = this.sequence.account;
      this.busy.account = true; this.errors.account = ''; this.customer = {}; this.service = {};
      try {
        const data = await this.$api.getDeliveryProviderAccount();
        if (generation !== this.generation || sequence !== this.sequence.account) return;
        this.customer = data.results.customer || {}; this.service = data.results.service || {}; this.errors.account = data.warning || '';
      } catch (error) {
        if (generation === this.generation && sequence === this.sequence.account) this.errors.account = errorMessage(error);
      } finally {
        if (generation === this.generation && sequence === this.sequence.account) this.busy.account = false;
      }
    },
    params(kind) {
      let params;
      if (kind === 'statistics') params = { date: this.filters.date, date_end: this.filters.date_end };
      else params = { ...(kind === 'failures' ? this.failureFilters : this.logFilters) };
      Object.keys(params).forEach((key) => { if (params[key] === '') delete params[key]; });
      return params;
    },
    async run(kind) {
      if (kind === 'logs' && this.busy.logs) return;
      if (kind === 'statistics') { this.invalidateFailures(); this.pages.batches = 1; }
      const { generation } = this; const params = this.params(kind); this.sequence[kind] += 1; const sequence = this.sequence[kind];
      this.busy[kind] = true; this.errors[kind] = ''; this[kind] = []; this.loaded[kind] = false; this.pages[kind] = 1;
      this.$delete(this.timestamps, kind);
      try {
        const data = await this.$api.queryDeliveryProvider(kind, params);
        if (generation !== this.generation || sequence !== this.sequence[kind]) return;
        this[kind] = data.results; this.loaded[kind] = true; this.$set(this.applied, kind, params); this.$set(this.timestamps, kind, data.fetched_at);
        if (kind === 'failures') this.clearFailureSelection();
        if (kind === 'statistics') {
          if (!this.batchOptions.includes(this.selectedBatch)) this.selectedBatch = '';
          if (!this.batchOptions.includes(this.logFilters.task_ident)) this.logFilters.task_ident = '';
          if (this.tab === 'statistics') this.refreshFailures();
        }
      } catch (error) {
        if (generation === this.generation && sequence === this.sequence[kind]) this.errors[kind] = errorMessage(error);
      } finally {
        if (generation === this.generation && sequence === this.sequence[kind]) this.busy[kind] = false;
      }
    },
    recentWeek() { this.filters.date = now().subtract(6, 'day').format('YYYY-MM-DD'); this.filters.date_end = now().format('YYYY-MM-DD'); this.run('statistics'); },
    invalidateFailures() {
      this.sequence.failures += 1; this.failures = []; this.loaded.failures = false; this.busy.failures = false;
      this.clearFailureSelection();
      this.errors.failures = ''; this.$delete(this.timestamps, 'failures'); this.$delete(this.applied, 'failures'); this.pages.failures = 1;
    },
    selectBatch(batch) {
      if (this.selectedBatch === batch && (this.busy.failures || this.loaded.failures)) return;
      this.selectedBatch = batch; this.refreshFailures();
    },
    refreshFailures() {
      const scope = this.applied.statistics; if (!this.loaded.statistics || !scope) return;
      if (!this.selectedBatch && ['1', '2', '3'].includes(this.failureFilters.type)) {
        this.errors.failures = this.t('errorTypeNeedsBatch');
        this.sequence.failures += 1;
        this.failures = [];
        this.loaded.failures = false;
        this.busy.failures = false;
        this.applied.failures = null;
        this.clearFailureSelection();
        this.pages.failures = 1;
        return;
      }
      this.failureFilters = {
        date: scope.date, date_end: scope.date_end, task_ident: this.selectedBatch, type: this.failureFilters.type, group_by: 'email',
      };
      this.run('failures');
    },
    useBatch(row) {
      if (!row.task_ident) return;
      if (this.tab === 'statistics') this.selectBatch(row.task_ident);
      else { this.selectedBatch = row.task_ident; this.invalidateFailures(); this.selectTab('statistics'); }
    },
    failureColor(type) { return ['2', '4', '10'].includes(String(type)) ? 'provider-tag-warning' : 'provider-tag-danger'; },
    failureType(type) { return Number(type) >= 1 && Number(type) <= 10 ? this.t(`error${type}`) : type || '—'; },
    rowMatches(row) { return row.matches || []; },
    allBlocked(row) {
      const matches = this.rowMatches(row);
      return matches.length > 0 && matches.every((match) => match.blocked);
    },
    canToggleBlocklist(row) { return this.rowMatches(row).some((match) => match.can_block); },
    blocklistLabel(row) {
      const matches = this.rowMatches(row);
      if (!matches.length) return this.$t('deliveryProvider.blocklistUnmatched');
      if (!this.canToggleBlocklist(row)) return this.$t('deliveryProvider.blocklistNoPermission');
      return this.$t(this.allBlocked(row) ? 'deliveryProvider.unblocklist' : 'deliveryProvider.blocklist');
    },
    targetState(match) {
      const blocking = this.blocklistDialog.action === 'block';
      if (!match.can_block) return { skip: true, label: this.$t('deliveryProvider.blocklistNoPermission') };
      if (blocking && match.blocked) return { skip: true, label: this.$t('deliveryProvider.blocklistAlready') };
      if (!blocking && !match.blocked) return { skip: true, label: this.$t('deliveryProvider.blocklistNotBlocked') };
      return { skip: false, label: this.$t(blocking ? 'deliveryProvider.blocklistWillAdd' : 'deliveryProvider.blocklistWillRemove') };
    },
    isFailureSelected(row) { return this.selectedFailures.includes((row.email || '').toLowerCase()); },
    toggleFailure(row) {
      const email = (row.email || '').toLowerCase();
      this.selectedFailures = this.isFailureSelected(row)
        ? this.selectedFailures.filter((item) => item !== email)
        : [...this.selectedFailures, email];
    },
    toggleAllFiltered(checked) {
      this.selectedFailures = checked ? this.filteredFailures.map((row) => (row.email || '').toLowerCase()) : [];
    },
    clearFailureSelection() { this.selectedFailures = []; },
    addressState(row) {
      const { scope } = this.blocklistDialog;
      const targets = (row.matches || []).filter((match) => scope === 'all' || match.kind === scope);
      if (!targets.length) return this.$t('deliveryProvider.blocklistSkipNone');
      const applicable = targets.filter((match) => !this.targetState(match).skip).length;
      return applicable
        ? this.$t('deliveryProvider.blocklistWillApply', { num: applicable })
        : this.$t('deliveryProvider.blocklistSkipAll');
    },
    // The dialog is the second step: pick private, pool or both, then confirm
    // the exact targets the server will change.
    openBlocklistDialog(rows, action) {
      const targets = rows.flatMap((row) => row.matches || []);
      if (!targets.some((match) => match.can_block)) return;
      let scope = 'all';
      if (!targets.some((match) => match.kind === 'private')) scope = 'pool';
      else if (!targets.some((match) => match.kind === 'pool')) scope = 'private';
      this.blocklistDialog = {
        open: true,
        rows: rows.map((row) => ({ email: row.email, matches: (row.matches || []).map((match) => ({ ...match })) })),
        action,
        scope,
        busy: false,
        error: '',
      };
    },
    openBlocklist(row) {
      if (!this.canToggleBlocklist(row)) return;
      this.openBlocklistDialog([row], this.allBlocked(row) ? 'unblock' : 'block');
    },
    openBatchBlocklist(action) {
      const rows = this.selectedFailureRows.filter((row) => (row.matches || []).some((match) => match.can_block));
      if (!rows.length) return;
      this.openBlocklistDialog(rows, action);
    },
    resetBlocklistDialog() {
      this.blocklistDialog = {
        open: false, rows: [], action: 'block', scope: 'all', busy: false, error: '',
      };
    },
    async confirmBlocklist() {
      const dialog = this.blocklistDialog;
      if (dialog.busy || !this.blocklistApplicable) return;
      dialog.busy = true; dialog.error = '';
      try {
        const emails = dialog.rows.map((row) => row.email);
        const result = await this.$api.blocklistDeliveryProviderFailure({
          emails, action: dialog.action, scope: dialog.scope,
        });
        const blocked = dialog.action === 'block';
        const selected = new Set(emails.map((email) => (email || '').toLowerCase()));
        this.failures = this.failures.map((row) => {
          if (!selected.has((row.email || '').toLowerCase())) return row;
          return {
            ...row,
            matches: (row.matches || []).map((match) => {
              const inScope = dialog.scope === 'all' || match.kind === dialog.scope;
              return inScope && match.can_block ? { ...match, blocked } : match;
            }),
          };
        });
        const applied = (result && result.applied) || [];
        this.$utils.toast(this.$t('deliveryProvider.blocklistApplied', { num: applied.length }));
        this.selectedFailures = this.selectedFailures.filter((email) => !selected.has(email));
        this.resetBlocklistDialog();
      } catch (error) {
        dialog.error = errorMessage(error);
      } finally {
        dialog.busy = false;
      }
    },
    prepareLog(row) {
      // Failure rows have no send date. Use the earliest statistic in the selected batch, not the unsaved date filter.
      const dates = this.scopedStatistics.map((record) => record.task_date).filter((date) => /^\d{4}-\d{2}-\d{2}$/.test(date || '')).sort();
      const date = dates[0] || this.applied.failures?.date || this.applied.statistics?.date || now().format('YYYY-MM-DD');
      if (this.busy.logs && this.logFilters.mail_to === row.email && this.logFilters.task_ident === this.selectedBatch && this.logFilters.date === date) {
        this.selectTab('logs'); return;
      }
      this.logFilters.mail_to = row.email; this.logFilters.task_ident = this.selectedBatch;
      this.logFilters.date = date;
      this.sequence.logs += 1; this.logs = []; this.loaded.logs = false; this.busy.logs = false; this.errors.logs = '';
      this.selectTab('logs');
      this.run('logs');
    },
    async saveAndConnect() {
      if (!this.canConfigure || this.saving || this.testing || this.configLoading) return;
      this.saving = true; this.connectionMessage = ''; this.connectionError = false;
      try {
        this.config = await this.$api.saveDeliveryProviderConfig({ ...this.credentials, expected_updated_at: this.config.updated_at || '' });
        this.credentials.password = ''; this.resetReports(); await this.testConnection();
      } catch (error) { this.connectionError = true; this.connectionMessage = errorMessage(error); } finally { this.saving = false; }
    },
    async testConnection() {
      if (!this.canConfigure || this.testing) return;
      this.testing = true; this.connectionMessage = ''; this.connectionError = false;
      try {
        this.config = await this.$api.testDeliveryProvider(); this.connectionError = !this.config.verified;
        this.connectionMessage = this.config.verified ? this.t('connected') : this.config.test_error;
        if (this.config.verified) { this.loadAccount(); this.run('statistics'); }
      } catch (error) { this.connectionError = true; this.connectionMessage = errorMessage(error); } finally { this.testing = false; }
    },
    async exportFailures() {
      if (!this.loaded.failures || this.exporting || this.busy.failures) return;
      const { generation } = this; const sequence = this.sequence.failures; this.exporting = true; this.errors.failures = '';
      try {
        const blob = await this.$api.exportDeliveryProviderFailures({ ...this.applied.failures, search: this.failureSearch.trim(), lang: this.$i18n.locale });
        if (generation !== this.generation || sequence !== this.sequence.failures) return;
        const url = window.URL.createObjectURL(blob); const link = document.createElement('a'); link.href = url;
        link.download = 'bestedm-failures.xlsx'; document.body.appendChild(link); link.click(); link.remove(); window.URL.revokeObjectURL(url);
      } catch (error) { if (generation === this.generation && sequence === this.sequence.failures) this.errors.failures = errorMessage(error); } finally { this.exporting = false; }
    },
  },
};
</script>

<style lang="scss" scoped src="../assets/styles/views/_delivery-provider.scss"></style>
