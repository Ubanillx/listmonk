/* eslint-env mocha */
/* global cy, expect */

// Every report response is stubbed: the console has no sample mode, and the
// isolated test database has no provider account.
const config = {
  username: 'report-account', configured: true, has_password: true, verified: true, updated_at: 'v1',
};
const account = {
  customer: {
    id: 'A-1', username: 'report_account', company: '示例企业', contact: '邮件运营团队', email: 'ops@example.com', phone: '0755-00000000',
  },
  service: {
    id: 'A-1', service_status: 'enabled', expiration_date: '2027-01-01', qty_available: '728640', qty_buytotal: '1000000', service_type: 'all', send_type: 'remote',
  },
};
const statistics = [
  {
    task_id: '83001', task_date: '2026-10-15', task_ident: '20261015-17463-smtp', total: '166', success: '162', failed: '15', invalid: '0', actual: '177',
  },
  {
    task_id: '83002', task_date: '2026-10-16', task_ident: '20261016-17463-smtp', total: '271', success: '265', failed: '12', invalid: '0', actual: '277',
  },
];
// Addresses are unique per batch so the local search filter and the export
// request below assert real filtering rather than a canned page.
const failures = [1, 2, 3].flatMap((batch) => Array.from({ length: 12 }, (_, i) => ({
  email: `batch${batch}-recipient${String(i + 1).padStart(2, '0')}@example.com`,
  error_count: 1,
  errors: [{ error_type: String((i % 10) + 1), error_type_dec: `错误说明 ${(i % 10) + 1}`, error_count: 1 }],
})));

const JSON_OK = { statusCode: 200 };
const ok = (body) => ({ ...JSON_OK, body });
const url = (path) => new RegExp(`/api/delivery-provider/${path}(\\?|$)`);

// stubProvider registers the console's read endpoints. Report paths are matched
// with anchored regexes so the export request cannot be captured by the
// failures handler.
function stubProvider() {
  cy.intercept('GET', url('config'), ok({ data: config }));
  cy.intercept('GET', url('account'), ok({ data: { results: account, source: 'live', warning: '' } }));
  cy.intercept('GET', url('statistics'), ok({ data: { results: statistics, source: 'live', fetched_at: '2026-10-20T10:00:00Z' } }));
  cy.intercept('GET', url('failures'), ok({ data: { results: failures, source: 'live', fetched_at: '2026-10-20T10:00:00Z' } }));
  cy.intercept('GET', url('logs'), ok({ data: { results: [], source: 'live', fetched_at: '2026-10-20T10:00:00Z' } }));
  cy.intercept('GET', '/api/delivery-provider/failures/export*', ok(''));
}

function visitConsole() {
  stubProvider();
  cy.loginAndVisit('/admin/delivery-provider');
  cy.get('.provider-console').should('be.visible');
  cy.get('[data-cy=provider-total]').should('not.have.text', '—');
}

describe('Delivery service permission gates', () => {
  before(() => { cy.resetDB(); });
  const visitAs = (permissions, roleId = 77, configured = true) => {
    stubProvider();
    if (!configured) cy.intercept('GET', url('config'), ok({ data: { configured: false } }));
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'zh-CN'; }));
    cy.intercept('GET', '/api/profile', {
      data: {
        id: 77,
        username: 'provider-reader',
        name: '报表查看人员',
        user_role: { id: roleId, permissions },
        customer_list_roles: [],
      },
    });
    cy.clearLocalStorage();
    cy.loginAndVisit('/admin/delivery-provider');
  };

  it('hides the menu and rejects a direct URL without an explicit provider grant', () => {
    let providerRequests = 0;
    visitAs(['settings:get', 'settings:manage', 'mailboxes:manage', 'organizations:platform_manage', 'workspaces:personal']);
    cy.intercept('/api/delivery-provider/**', () => { providerRequests += 1; });
    cy.location('pathname').should('eq', '/admin/403');
    cy.get('[data-cy=delivery-provider]').should('not.exist');
    cy.get('.provider-console').should('not.exist');
    cy.then(() => { expect(providerRequests).to.equal(0); });
  });

  it('lets a delegated reader open reports, export, view logs and account details but not configure credentials', () => {
    visitAs(['delivery_provider:get', 'workspaces:personal']);
    cy.get('[data-cy=delivery-provider]').should('be.visible');
    cy.get('[data-cy=provider-tab-statistics]').click();
    cy.get('.provider-failure-table tbody tr').should('have.length', 10);
    cy.intercept('GET', '/api/delivery-provider/failures/export*').as('readerExport');
    cy.contains('.provider-console button', '导出 Excel').click();
    cy.wait('@readerExport').its('request.query.group_by').should('eq', 'email');
    cy.get('[data-cy=provider-tab-logs]').click();
    cy.get('input[type=email]').should('be.visible');
    cy.get('[data-cy=provider-tab-account]').should('contain.text', '账号资料').click();
    cy.contains('.provider-console', '示例企业').should('be.visible');
    cy.get('[data-cy=provider-configuration]').should('not.exist');
    cy.get('[data-cy=provider-connect]').should('not.exist');
    cy.reload();
    cy.get('.provider-console').should('be.visible');
    cy.get('[data-cy=provider-configuration]').should('not.exist');
  });

  it('explains an unconfigured account without offering configuration to readers', () => {
    visitAs(['delivery_provider:get', 'workspaces:personal'], 77, false);
    cy.contains('请联系平台管理员配置发件服务账号').should('be.visible');
    cy.get('.provider-empty button').should('not.exist');
  });

  it('preserves the platform administrator configuration access without an explicit grant', () => {
    visitAs([], 1);
    cy.get('[data-cy=delivery-provider]').should('be.visible');
    cy.get('[data-cy=provider-tab-account]').click();
    cy.get('[data-cy=provider-configuration]').should('be.visible');
    cy.get('[data-cy=provider-connect]').should('be.visible');
  });
});

// The provider answers "no rows" with a successful, empty report. Stub that
// shape directly so the empty state can be asserted without the live API.
function visitEmptyConsole() {
  const empty = ok({ data: { results: [], source: 'live', fetched_at: '2026-10-20T10:00:00Z' } });
  cy.intercept('GET', url('config'), ok({ data: config }));
  cy.intercept('GET', url('account'), ok({ data: { results: account, source: 'live', warning: '' } }));
  cy.intercept('GET', url('statistics'), empty);
  cy.intercept('GET', url('failures'), empty);
  cy.intercept('GET', url('logs'), empty);
  cy.loginAndVisit('/admin/delivery-provider');
  cy.get('.provider-console').should('be.visible');
}

describe('BestEDM delivery provider console', () => {
  before(() => { cy.resetDB(); });
  beforeEach(() => {
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'zh-CN'; }));
  });

  it('renders provider reports and keeps tab drafts across reloads', () => {
    visitConsole();
    cy.get('.provider-console').within(() => {
      cy.contains('示例企业').should('be.visible');
      cy.contains('728,640').should('be.visible');
      cy.get('[data-cy=provider-tab-logs]').click();
      cy.get('input[type=email]').type('draft@example.com');
      cy.get('[data-cy=provider-tab-account]').click();
      cy.get('[data-cy=provider-tab-logs]').click();
      cy.get('input[type=email]').should('have.value', 'draft@example.com');
    });
    cy.location('search').should('include', 'tab=logs');
    cy.reload();
    // The route key ignores local query state, so the draft survives a reload.
    cy.get('[data-cy=provider-tab-logs]').should('have.attr', 'aria-current', 'page');
    cy.contains('输入收件邮箱').should('be.visible');
    cy.get('[data-cy=provider-tab-overview]').click();
    cy.screenshot('bestedm-overview-desktop', { capture: 'viewport' });
  });

  it('links batches to failures, filters by address and exports the same scope', () => {
    visitConsole();
    cy.intercept('GET', url('failures')).as('failures');
    cy.get('[data-cy=provider-tab-statistics]').click();
    cy.get('.provider-failure-table tbody tr').should('have.length', 10);
    cy.wait('@failures');
    cy.contains('.provider-batch-group .provider-batch', '20261015-17463-smtp').click();
    cy.wait('@failures').then(({ request }) => {
      expect(request.query.task_ident).to.equal('20261015-17463-smtp');
      expect(request.query.group_by).to.equal('email');
    });
    cy.get('[data-cy=provider-selection]').should('contain', '20261015-17463-smtp');
    cy.get('.is-selected-batch').should('have.length', 1);
    cy.get('[data-cy=provider-failure-search]').type('BATCH1-RECIPIENT01');
    cy.get('.provider-failure-table tbody tr').should('have.length', 1)
      .and('contain', 'batch1-recipient01@example.com');
    cy.intercept('GET', '/api/delivery-provider/failures/export*').as('export');
    cy.contains('.provider-console button', '导出 Excel').click();
    cy.wait('@export').its('request.query').then((query) => {
      expect(query.search).to.equal('BATCH1-RECIPIENT01');
      expect(query.task_ident).to.equal('20261015-17463-smtp');
      expect(query.group_by).to.equal('email');
    });
    cy.get('[data-cy=provider-failures] [data-cy=page-size]').select('20');
    cy.get('.provider-failure-table tbody tr').should('have.length', 1);
  });

  it('allows task-scoped error types to be selected before choosing a batch', () => {
    visitConsole();
    cy.intercept('GET', url('failures')).as('failures');
    cy.get('[data-cy=provider-tab-statistics]').click();
    cy.wait('@failures');

    ['1', '2', '3'].forEach((type) => {
      cy.get(`[data-cy=provider-error-type] option[value="${type}"]`).should('not.be.disabled');
    });
    cy.get('[data-cy=provider-error-type]').select('1');
    cy.get('[role=alert]').should('be.visible').and('contain', '批次');
    cy.get('[data-cy=provider-error-type]').should('have.value', '1');

    cy.get('[data-cy=provider-batch-select]').select('20261015-17463-smtp');
    cy.wait('@failures').its('request.query').should((query) => {
      expect(query.type).to.equal('1');
      expect(query.task_ident).to.equal('20261015-17463-smtp');
    });
    cy.get('[role=alert]').should('not.exist');
  });

  it('matches failed addresses and blocklists them through a confirmation dialog', () => {
    visitConsole();
    const matched = {
      email: 'known@example.com',
      error_count: 2,
      errors: [{ error_type: '1', error_type_dec: '邮箱不存在', error_count: 2 }],
      matches: [
        {
          kind: 'private', id: 11, customer_code: 'C1', name: '李雷', status: 'enabled', blocked: false, can_block: true,
        },
        {
          kind: 'pool', id: 22, pool_id: 3, pool_name: '公海A', customer_code: 'P1', name: '韩梅梅', status: 'active', blocked: false, can_block: true,
        },
      ],
    };
    const unmatched = {
      email: 'nomatch@example.com',
      error_count: 1,
      errors: [{ error_type: '8', error_type_dec: '无效地址', error_count: 1 }],
      matches: [],
    };
    cy.intercept('GET', url('failures'), ok({ data: { results: [matched, unmatched], source: 'live', fetched_at: '2026-10-20T10:00:00Z' } })).as('matchedFailures');
    cy.intercept('PUT', '/api/delivery-provider/failures/blocklist', ok({ data: { applied: matched.matches, skipped: [], unmatched: [] } })).as('blocklist');
    cy.get('[data-cy=provider-tab-statistics]').click();
    cy.wait('@matchedFailures');

    cy.contains('.provider-failure-table tbody tr', 'nomatch@example.com')
      .find('[data-cy=provider-failure-blocklist]').should('be.disabled');
    cy.contains('.provider-failure-table tbody tr', 'known@example.com')
      .find('[data-cy=provider-failure-blocklist]')
      .should('have.attr', 'aria-label', '加入黑名单')
      .click();

    cy.get('[data-cy=provider-blocklist-dialog]').should('be.visible').within(() => {
      cy.contains('known@example.com').should('be.visible');
      cy.get('.provider-blocklist-targets li').should('have.length', 2)
        .first().should('contain', '私域')
        .and('contain', '李雷');
      cy.contains('.provider-blocklist-targets li', '公海').should('contain', '韩梅梅').and('contain', '公海A');
      cy.get('.provider-blocklist-targets li').eq(1).should('contain', '将加入黑名单');
      cy.contains('公海黑名单是共享主数据').should('be.visible');
      cy.get('[data-cy=provider-blocklist-confirm]').should('contain', '确认加入（2）').click();
    });

    cy.wait('@blocklist').its('request.body').then((body) => {
      expect(body.emails).to.deep.equal(['known@example.com']);
      expect(body.action).to.equal('block');
      expect(body.scope).to.equal('all');
    });
    cy.contains('.provider-failure-table tbody tr', 'known@example.com')
      .find('[data-cy=provider-failure-blocklist]')
      .should('have.attr', 'aria-label', '移出黑名单');
  });
  it('selects several failures and blocklists only the chosen scope', () => {
    visitConsole();
    const rows = [
      {
        email: 'private@example.com',
        error_count: 1,
        errors: [{ error_type: '1', error_type_dec: '邮箱不存在', error_count: 1 }],
        matches: [{
          kind: 'private', id: 1, customer_code: 'C1', name: '甲', status: 'enabled', blocked: false, can_block: true,
        }],
      },
      {
        email: 'pool@example.com',
        error_count: 1,
        errors: [{ error_type: '8', error_type_dec: '无效地址', error_count: 1 }],
        matches: [{
          kind: 'pool', id: 2, pool_id: 3, pool_name: '公海A', customer_code: 'P1', name: '乙', status: 'active', blocked: false, can_block: true,
        }],
      },
    ];
    cy.intercept('GET', url('failures'), ok({ data: { results: rows, source: 'live', fetched_at: '2026-10-20T10:00:00Z' } })).as('batchFailures');
    cy.intercept('PUT', '/api/delivery-provider/failures/blocklist', ok({ data: { applied: [], skipped: [], unmatched: [] } })).as('batchBlocklist');
    cy.get('[data-cy=provider-tab-statistics]').click();
    cy.wait('@batchFailures');

    cy.get('[data-cy=provider-batch-bar]').should('not.exist');
    cy.get('[data-cy=provider-select-address]').eq(0).check();
    cy.get('[data-cy=provider-select-address]').eq(1).check();
    cy.get('[data-cy=provider-batch-bar]').should('contain', '已选 2 个地址');
    cy.get('[data-cy=provider-batch-block]').click();

    cy.get('[data-cy=provider-blocklist-dialog]').within(() => {
      cy.contains('批量加入黑名单').should('be.visible');
      cy.get('.provider-scope-option').should('have.length', 3);
      cy.get('[data-cy=provider-scope-private]').check();
      cy.get('[data-cy=provider-blocklist-confirm]').should('contain', '确认加入（1）').click();
    });

    cy.wait('@batchBlocklist').its('request.body').then((body) => {
      expect(body.emails).to.have.members(['private@example.com', 'pool@example.com']);
      expect(body.action).to.equal('block');
      expect(body.scope).to.equal('private');
    });
    cy.get('[data-cy=provider-batch-bar]').should('not.exist');
  });

  it('surfaces provider failures instead of zero totals', () => {
    visitConsole();
    cy.intercept('GET', url('statistics'), { statusCode: 502, body: { message: 'Provider XML is invalid' } }).as('broken');
    cy.get('form.provider-filter').submit();
    cy.wait('@broken');
    cy.contains('.provider-console [role=alert]', 'Provider XML is invalid').should('be.visible');
    cy.get('[data-cy=provider-total]').should('have.text', '—');
  });

  it('groups tasks newest first, sorts records and paginates whole batches', () => {
    cy.viewport(1783, 1244);
    visitConsole();
    const tasks = Array.from({ length: 12 }, (_, i) => ({
      task_id: String(i + 1),
      task_ident: `batch-${String(i + 1).padStart(2, '0')}`,
      task_date: `2026-10-${String(i + 1).padStart(2, '0')}`,
      total: '10',
      success: '9',
      failed: '1',
    }));
    tasks.push({ ...tasks[11], task_id: '101', task_date: '2026-10-10' }, { ...tasks[11], task_id: '102', task_date: '2026-10-11' });
    cy.intercept('GET', url('statistics'), ok({ data: { results: tasks, source: 'live' } })).as('tasks');
    cy.get('form.provider-filter').submit();
    cy.wait('@tasks');
    cy.get('[data-cy=provider-tab-statistics]').click();
    cy.get('.provider-batches .provider-count').should('have.text', '12');
    cy.get('.provider-batch-group').should('have.length', 10).first().should('have.attr', 'data-batch', 'batch-12');
    cy.get('[data-batch=batch-12] .provider-batch-record time').then(($dates) => {
      expect([...$dates].map((date) => date.textContent)).to.deep.equal(['2026-10-12', '2026-10-11', '2026-10-10']);
    });
    cy.get('.provider-batches .pagination-link').contains('2').click();
    cy.get('.provider-batch-group').should('have.length', 2).first().should('have.attr', 'data-batch', 'batch-02');
    cy.get('.provider-batches [data-cy=page-size]').select('20');
    cy.get('.provider-batch-group').should('have.length', 12).first().should('have.attr', 'data-batch', 'batch-12');
    cy.get('[data-batch=batch-12] .provider-batch-record').should('have.length', 3);
  });

  it('keeps panes and pagers aligned and removes the annotated decorations', () => {
    cy.viewport(1783, 1244);
    visitConsole();
    cy.get('.provider-scope').should('not.exist');
    cy.get('[data-cy=provider-tab-statistics]').click();
    cy.get('.provider-failure-table tbody tr').should('have.length', 10);
    cy.get('.provider-selection').should('have.css', 'border-left-width', '0px');
    cy.get('.provider-analysis').should(($panel) => {
      const [left, right] = ['.provider-batches', '.provider-failures'].map((selector) => $panel[0].querySelector(selector));
      expect(Math.abs(left.getBoundingClientRect().height - right.getBoundingClientRect().height)).to.be.lessThan(1);
      const bottom = (pane) => pane.querySelector('.app-pagination').getBoundingClientRect().bottom;
      expect(Math.abs(bottom(left) - bottom(right))).to.be.lessThan(1);
    });
    cy.get('[data-cy=provider-tab-logs]').click();
    cy.get('.provider-notice').should('not.exist');
    cy.get('[role=alert]').should('not.exist');
    cy.intercept('GET', url('logs'), { statusCode: 502, body: { message: '日志查询服务暂不可用' } }).as('logError');
    cy.get('input[type=email]').type('address@example.com');
    cy.get('form.provider-filter').submit();
    cy.wait('@logError');
    cy.contains('.provider-console [role=alert]', '日志查询服务暂不可用').should('be.visible');
  });

  it('shows all error classifications and counts for each unique email', () => {
    visitConsole();
    cy.intercept('GET', url('failures'), ok({
      data: {
        results: [{
          email: 'repeat@example.com',
          error_count: 3,
          errors: [
            { error_type: '1', error_type_dec: 'Missing', error_count: 2 },
            { error_type: '10', error_type_dec: 'Timeout', error_count: 1 },
          ],
        }],
        source: 'live',
      },
    }));
    cy.get('[data-cy=provider-tab-statistics]').click();
    cy.get('.provider-failure-table tbody tr').should('have.length', 1);
    cy.get('.provider-error-count').should('have.text', '3');
    cy.get('.provider-failure-cause').should('have.length', 2);
    cy.contains('.provider-failure-cause', '邮箱不存在 × 2').should('be.visible');
    cy.contains('.provider-failure-cause', '连接异常').should('be.visible').and('not.contain.text', '× 1');
    cy.get('.provider-failure-table [data-cy=provider-failure-logs]').click();
    cy.get('input[type=email]').should('have.value', 'repeat@example.com');
  });

  it('filters from any batch row and ignores late responses from earlier selections', () => {
    visitConsole();
    cy.get('[data-cy=provider-tab-statistics]').click();
    cy.get('.provider-failure-table tbody tr').should('have.length', 10);
    cy.intercept('GET', url('failures'), (req) => {
      const first = req.query.task_ident === statistics[0].task_ident;
      req.alias = first ? 'slowBatch' : 'fastBatch';
      req.reply({
        delay: first ? 600 : 0,
        body: {
          data: {
            results: [{ email: first ? 'old@example.com' : 'latest@example.com', error_count: 1, errors: [] }], source: 'live',
          },
        },
      });
    });
    cy.get(`[data-batch="${statistics[0].task_ident}"] .provider-batch-record td`).last().click();
    cy.get(`[data-batch="${statistics[1].task_ident}"] .provider-batch-group-heading`).click();
    cy.wait('@fastBatch');
    cy.wait('@slowBatch');
    cy.get('.provider-failure-table tbody').should('contain', 'latest@example.com').and('not.contain', 'old@example.com');
    cy.get('.provider-selection').should('contain', statistics[1].task_ident);
    cy.get('[data-cy=provider-batch-select]').should('have.value', statistics[1].task_ident);
    cy.get('.is-selected-batch').should('have.attr', 'data-batch', statistics[1].task_ident);
  });

  it('queries logs once using the selected batch earliest date and keeps manual date queries available', () => {
    visitConsole();
    cy.intercept('GET', url('statistics'), ok({
      data: {
        results: [
          { ...statistics[1], task_date: '2026-10-20' },
          { ...statistics[0], task_date: '2026-10-14' },
          { ...statistics[1], task_date: '2026-10-16' },
          { ...statistics[1], task_date: '' },
        ],
        source: 'live',
      },
    }));
    cy.get('form.provider-filter').submit();
    cy.get('[data-cy=provider-tab-statistics]').click();
    cy.contains('.provider-batch-group .provider-batch', statistics[1].task_ident).click();
    cy.get('[data-cy=provider-selection]').should('contain', statistics[1].task_ident);
    // A draft filter must not replace the dates of the loaded batch records.
    cy.get('[data-cy=provider-start]').clear().type('2026-10-01');
    cy.intercept('GET', url('logs')).as('autoLogs');
    cy.get('.provider-failure-table [data-cy=provider-failure-logs]').first().click();
    cy.wait('@autoLogs').its('request.query').should('deep.equal', {
      date: '2026-10-16', mail_to: 'batch1-recipient01@example.com', task_ident: statistics[1].task_ident,
    });
    cy.get('[data-cy=provider-log-date]').should('have.value', '2026-10-16');
    cy.get('[data-cy=provider-log-batch]').should('have.value', statistics[1].task_ident);
    cy.contains('.provider-console', '该收件邮箱在所选日期没有投递记录').should('be.visible');
    cy.get('[data-cy=provider-tab-overview]').click();
    cy.get('[data-cy=provider-tab-logs]').click();
    cy.get('@autoLogs.all').should('have.length', 1);
    cy.get('[data-cy=provider-log-date]').clear().type('2026-10-17');
    cy.get('[data-cy=provider-log-date]').blur();
    cy.get('form.provider-filter').submit();
    cy.wait('@autoLogs').its('request.query.date').should('eq', '2026-10-17');
    cy.get('@autoLogs.all').should('have.length', 2);
  });

  it('uses the earliest date across all loaded statistics when no batch is selected', () => {
    visitConsole();
    cy.intercept('GET', url('statistics'), ok({
      data: {
        results: [
          { ...statistics[1], task_date: '2026-10-20' },
          { ...statistics[0], task_date: '2026-10-15' },
        ],
        source: 'live',
      },
    }));
    cy.get('form.provider-filter').submit();
    cy.get('[data-cy=provider-tab-statistics]').click();
    cy.get('[data-cy=provider-batch-select]').should('have.value', '');
    cy.intercept('GET', url('logs')).as('dateLogs');
    cy.get('.provider-failure-table [data-cy=provider-failure-logs]').first().click();
    cy.wait('@dateLogs').its('request.query').should('deep.equal', {
      date: '2026-10-15', mail_to: 'batch1-recipient01@example.com',
    });
    cy.get('[data-cy=provider-log-date]').should('have.value', '2026-10-15');
    cy.get('[data-cy=provider-log-batch]').should('have.value', '');
  });

  it('falls back to the applied range start when batch records have no dates', () => {
    visitConsole();
    cy.intercept('GET', url('statistics'), ok({
      data: {
        results: [
          { ...statistics[0], task_date: '' }, { ...statistics[0], task_date: 'unknown' },
        ],
        source: 'live',
      },
    }));
    cy.get('[data-cy=provider-start]').clear().type('2026-10-10');
    cy.get('[data-cy=provider-end]').clear().type('2026-10-20');
    cy.get('form.provider-filter').submit();
    cy.get('[data-cy=provider-tab-statistics]').click();
    cy.contains('.provider-batch-group .provider-batch', statistics[0].task_ident).click();
    cy.get('[data-cy=provider-selection]').should('contain', statistics[0].task_ident);
    cy.get('[data-cy=provider-start]').clear().type('2026-10-01');
    cy.intercept('GET', url('logs')).as('fallbackLogs');
    cy.get('.provider-failure-table [data-cy=provider-failure-logs]').first().click();
    cy.wait('@fallbackLogs').its('request.query.date').should('eq', '2026-10-10');
  });

  it('keeps controls within a mobile viewport', () => {
    visitConsole();
    cy.get('[data-cy=provider-tab-statistics]').click();
    cy.viewport(390, 844);
    cy.get('.provider-console').then(($page) => {
      const rect = $page[0].getBoundingClientRect();
      expect(rect.left).to.be.at.least(0);
      expect(rect.right).to.be.at.most(391);
    });
    cy.get('.provider-console input, .provider-console select, .provider-console form button').each(($control) => {
      const rect = $control[0].getBoundingClientRect();
      expect(rect.right).to.be.at.most(391);
      expect(rect.left).to.be.at.least(0);
    });
    cy.screenshot('bestedm-analysis-mobile', { capture: 'viewport' });
  });

  it('uses the report design system for its header, cards, chart and tables', () => {
    cy.viewport(1440, 1000);
    visitConsole();
    cy.get('.provider-console h1').should('have.length', 1).and('have.class', 'title');
    cy.get('.provider-metric').should('have.length', 4).each(($card) => {
      const style = getComputedStyle($card[0]);
      // Each metric keeps its own tint so the four counters stay distinguishable.
      expect(style.backgroundColor).to.not.equal('rgba(0, 0, 0, 0)');
      expect(style.borderRadius).to.equal('6px');
      expect($card[0].getBoundingClientRect().height).to.be.at.least(100);
    });
    cy.get('.provider-table thead th').first().should('have.css', 'font-weight', '700');
    cy.get('.provider-chart canvas').should('be.visible').then(($canvas) => {
      expect($canvas[0].width).to.be.greaterThan(0);
      expect($canvas[0].height).to.be.greaterThan(0);
    });
    cy.get('.provider-table tbody').should('contain', '166').and('contain', '271');
    cy.screenshot('bestedm-report-design-desktop', { capture: 'viewport' });
  });

  it('keeps every tab, form and pagination control inside the mobile workspace', () => {
    visitConsole();
    cy.viewport(390, 844);
    ['overview', 'statistics', 'logs', 'account'].forEach((tab) => {
      cy.get(`[data-cy=provider-tab-${tab}]`).click();
      cy.get(`[data-cy=provider-tab-${tab}]`).should('have.attr', 'aria-current', 'page');
      cy.get('.provider-console').should(($page) => {
        expect($page[0].scrollWidth).to.equal($page[0].clientWidth);
      });
      cy.get('.provider-console').then(($page) => {
        const page = $page[0].getBoundingClientRect();
        cy.get('.provider-console input, .provider-console select, .provider-console button, .provider-tabs a')
          .filter(':visible').each(($control) => {
            if ($control.closest('.provider-table-scroll').length) return;
            const rect = $control[0].getBoundingClientRect();
            expect(rect.right, `${tab}: ${$control[0].outerHTML}`).to.be.at.most(page.right + 1);
            expect(rect.left).to.be.at.least(page.left - 1);
          });
      });
    });
    cy.get('[data-cy=provider-tab-overview]').click();
    cy.get('[data-cy=provider-start]').should('have.attr', 'aria-label', '开始日期');
    cy.screenshot('bestedm-report-design-mobile', { capture: 'viewport' });
  });

  it('persists credentials without returning them and rejects stale and generic writes', () => {
    cy.request('/api/delivery-provider/config').then(({ body }) => {
      cy.request('PUT', '/api/delivery-provider/config', {
        username: 'provider-e2e-only', password: 'fake-password-for-isolated-test', expected_updated_at: body.data.updated_at,
      }).then(({ body: saved }) => {
        expect(saved.data.has_password).to.equal(true);
        expect(saved.data).not.to.have.property('password');
        cy.request('PUT', '/api/delivery-provider/config', {
          username: 'provider-e2e-only', password: '', expected_updated_at: saved.data.updated_at,
        }).its('body.data.has_password').should('equal', true);
        cy.request({
          method: 'PUT',
          url: '/api/delivery-provider/config',
          failOnStatusCode: false,
          body: { username: 'other-account', password: '', expected_updated_at: saved.data.updated_at },
        }).its('status').should('equal', 409);
      });
    });
    cy.request('/api/settings').then(({ body }) => {
      expect(body.data).not.to.have.property('integration.bestedm');
      expect(JSON.stringify(body)).not.to.contain('fake-password-for-isolated-test');
    });
    cy.request({
      method: 'PUT', url: '/api/settings/integration.bestedm', body: {}, failOnStatusCode: false,
    }).its('status').should('equal', 403);
  });

  // The provider reports "no rows" as a 200 response with an empty-result marker.
  // That must render an empty table, not an error banner.
  it('renders empty provider results as an empty state, not an error', () => {
    cy.intercept('GET', url('statistics'), ok({ data: { results: [], source: 'live', fetched_at: '2026-10-20T10:00:00Z' } }));
    cy.intercept('GET', url('failures'), ok({ data: { results: [], source: 'live', fetched_at: '2026-10-20T10:00:00Z' } }));
    cy.intercept('GET', url('logs'), ok({ data: { results: [], source: 'live', fetched_at: '2026-10-20T10:00:00Z' } }));
    visitEmptyConsole();

    cy.get('[data-cy=provider-tab-statistics]').click();
    cy.contains('.provider-console', '无记录').should('be.visible');
    cy.get('.provider-console [role=alert]').should('not.exist');
    cy.get('[data-cy=provider-total]').should('have.text', '0');

    cy.get('[data-cy=provider-tab-logs]').click();
    cy.get('[data-cy=provider-log-date]').clear().type('2026-10-14');
    cy.get('input[type=email]').type('mari.kekkonen@hongkongimport.fi');
    cy.get('form.provider-filter').submit();
    cy.contains('.provider-console', '该收件邮箱在所选日期没有投递记录').should('be.visible');
    cy.get('.provider-console [role=alert]').should('not.exist');
  });

  // The console has one data source. A sample-mode request must be rejected so
  // no synthetic report can be rendered.
  it('rejects sample-mode report queries', () => {
    cy.request({
      url: '/api/delivery-provider/statistics?mode=demo&date=2026-10-20', failOnStatusCode: false,
    }).its('status').should('equal', 400);
    cy.request({
      url: '/api/delivery-provider/account?mode=demo', failOnStatusCode: false,
    }).its('status').should('equal', 400);
  });
});
