describe('Campaign send errors', () => {
  it('groups reasons and customers, pages the report and exports the applied filter', () => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'zh-CN'; }));
    cy.intercept({ method: 'GET', pathname: '/api/campaigns' }, { data: {
      results: [{
        id: 901, name: '错误原因统计活动', subject: 'Error report', status: 'paused', type: 'regular', tags: [],
        customer_lists: [], customer_pools: [], send_errors: 25, owner_username: 'admin', visibility: 'private',
        views: 0, clicks: 0, sent: 3, to_send: 10, unsent_count: 7, bounces: 0,
      }],
      total: 1, per_page: 20,
    } }).as('campaigns');
    const rows = Array.from({ length: 21 }, (_, index) => ({
      recipient_type: 'private', recipient_id: index + 1, customer_code: `C-${index}`, name: `客户 ${index}`,
      email: `customer${index}@example.test`, stage: 'send', category: index === 0 ? 'smtp_rejected' : 'network',
      smtp_code: index === 0 ? 550 : 0, error: index === 0 ? '550 5.1.1 Recipient unknown' : 'dial tcp: connection refused',
      count: index === 0 ? 2 : 1, first_at: '2026-10-09T01:00:00Z', last_at: '2026-10-09T02:00:00Z',
    }));
    cy.intercept({ method: 'GET', pathname: '/api/campaigns/901/send-errors' }, (req) => {
      const selected = rows.filter((row) => (!req.query.category || row.category === req.query.category)
        && (!req.query.search || row.customer_code === req.query.search));
      const page = Number(req.query.page || 1);
      const perPage = Number(req.query.per_page || 20);
      const reasons = {};
      selected.forEach((row) => { reasons[row.category] = (reasons[row.category] || 0) + row.count; });
      req.reply({ data: {
        results: selected.slice((page - 1) * perPage, page * perPage), total: selected.length, page, per_page: perPage,
        recorded_errors: selected.reduce((total, row) => total + row.count, 0), historical_errors: 3,
        reasons: Object.entries(reasons).map(([category, count]) => ({ category, count })), can_export: true,
      } });
    }).as('errors');
    cy.intercept({ method: 'GET', pathname: '/api/campaigns/901/send-errors/export' }, (req) => {
      expect(req.query.category).to.eq('smtp_rejected');
      expect(req.query.search).to.eq('');
      expect(req.query.page).to.eq(undefined);
      expect(req.query.lang).to.eq('zh-CN');
      // The real exporter and workbook cells are checked by PostgreSQL/Go integration tests.
      req.reply({ body: 'Excel download flow verified', headers: { 'content-type': 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' } });
    }).as('exportErrors');
    cy.loginAndVisit('/admin/campaigns');
    cy.wait('@campaigns');
    cy.get('.campaign-send-errors a').click();
    cy.wait('@errors');
    cy.get('.campaign-send-error-report').within(() => {
      cy.contains('共 22 次错误，汇总为 21 条记录').should('be.visible');
      cy.get('[data-cy=send-error-attempts]').should('have.text', '22');
      cy.get('[data-cy=send-error-records]').should('have.text', '21');
      cy.contains('有 3 次历史错误').should('be.visible');
      cy.contains('.tag', 'SMTP 拒绝发送: 2').should('be.visible');
      cy.contains('tbody tr', 'C-0').should('contain', '550 5.1.1 Recipient unknown').and('contain', '客户 0');
      cy.get('.pagination-list').contains('2').click();
    });
    cy.wait('@errors');
    cy.get('.campaign-send-error-report').within(() => {
      cy.contains('tbody tr', 'C-20').should('be.visible');
      cy.get('select[data-cy=page-size]').select('10');
    });
    cy.wait('@errors').its('request.query').should('include', { page: '1', per_page: '10' });
    cy.get('.campaign-send-error-report').within(() => {
      cy.get('#send-error-category').select('smtp_rejected');
    });
    cy.wait('@errors');
    cy.get('.campaign-send-error-report').within(() => {
      cy.contains('共 2 次错误，汇总为 1 条记录').should('be.visible');
      // Unsubmitted search input must not change the export's applied filter.
      cy.get('#send-error-search').type('C-20');
      cy.get('[data-cy=export-send-errors]').click();
    });
    cy.wait('@exportErrors');
    cy.readFile('cypress/downloads/campaign-901-send-errors.xlsx').should('eq', 'Excel download flow verified');
    cy.get('.campaign-send-error-report').screenshot('campaign-send-error-details-desktop');
    cy.viewport(390, 844);
    cy.get('.campaign-send-error-report').within(() => {
      cy.contains('550 5.1.1 Recipient unknown').scrollIntoView().should('be.visible');
      cy.contains('button', '关闭').scrollIntoView().should('be.visible');
    });
    cy.screenshot('campaign-send-error-details-mobile');
  });

  it('renders the overview, empty and failed states without clipping controls', () => {
    cy.resetDB();
    cy.viewport(1487, 1058);
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'zh-CN'; }));
    cy.intercept({ method: 'GET', pathname: '/api/campaigns' }, { data: {
      results: [{
        id: 901, name: 'tracking-e2e-37564', subject: 'Error report', status: 'paused', type: 'regular', tags: [],
        customer_lists: [], customer_pools: [], send_errors: 44, owner_username: 'admin', visibility: 'private',
        views: 0, clicks: 0, sent: 3, to_send: 10, unsent_count: 7, bounces: 0,
      }], total: 1, per_page: 20,
    } }).as('campaigns');
    let mode = 'rows';
    const rows = [
      ['张伟', 'smtp_auth', 535, '535 Authentication failed: Invalid credentials', 16],
      ['李娜', 'smtp_rejected', 550, '550 Mailbox unavailable: User unknown', 12],
      ['王强', 'smtp_temporary', 451, '451 Try again later: Temporary server error', 8],
      ['陈敏', 'smtp_auth', 535, '535 Authentication failed: Username and password not accepted', 5],
      ['刘洋', 'smtp_rejected', 550, '550 Recipient address rejected: User unknown in local recipient table', 3],
    ].map(([name, category, smtpCode, error, count], index) => ({
      recipient_type: 'private', recipient_id: index + 1, customer_code: `C000${index + 1}`, name,
      email: 'li***@example.com', stage: 'send', category, smtp_code: smtpCode, error, count,
      first_at: '2026-10-19T00:12:00Z', last_at: '2026-10-19T06:32:00Z',
    }));
    cy.intercept({ method: 'GET', pathname: '/api/campaigns/901/send-errors' }, (req) => {
      if (mode === 'failed') { req.reply({ statusCode: 503, body: { message: 'Report unavailable' } }); return; }
      req.reply({ data: {
        results: mode === 'empty' ? [] : rows, total: mode === 'empty' ? 0 : rows.length,
        recorded_errors: mode === 'empty' ? 0 : 44, historical_errors: 0, reasons: [], can_export: mode !== 'restricted',
      } });
    }).as('errors');
    cy.loginAndVisit('/admin/campaigns');
    cy.wait('@campaigns');
    cy.get('.campaign-send-errors a').click();
    cy.wait('@errors');
    cy.get('.campaign-send-error-report').within(() => {
      cy.get('thead th').should('have.length', 6);
      cy.get('[data-cy=send-error-attempts]').should('have.text', '44');
      cy.get('[data-cy=send-error-records]').should('have.text', '5');
      cy.get('.send-error-reason-danger').should('have.length', 2);
      cy.get('.send-error-reason-warning').should('have.length', 2);
      cy.get('[data-cy=export-send-errors]').should('be.enabled');
    });
    cy.screenshot('send-error-overview-desktop', { capture: 'viewport' });
    cy.viewport(1280, 900);
    cy.get('.campaign-send-error-report').then(($modal) => {
      const rect = $modal[0].getBoundingClientRect();
      expect(rect.left).to.be.at.least(0);
      expect(rect.right).to.be.at.most(1280);
    });
    cy.viewport(1487, 1058);
    cy.then(() => { mode = 'empty'; });
    cy.get('.send-error-refresh button').click();
    cy.wait('@errors');
    cy.get('.send-error-empty').should('be.visible');
    cy.get('.send-error-table [data-cy=pagination]').should('not.exist');
    cy.get('[data-cy=send-error-attempts]').should('have.text', '0');
    cy.get('[data-cy=export-send-errors]').should('be.disabled');
    cy.screenshot('send-error-overview-empty', { capture: 'viewport' });
    cy.then(() => { mode = 'failed'; });
    cy.get('.send-error-refresh button').click();
    cy.wait('@errors');
    cy.get('.campaign-send-error-report .notification.is-danger').should('be.visible');
    cy.get('[data-cy=send-error-attempts]').should('have.text', '—');
    cy.get('.send-error-empty').should('not.exist');
    cy.get('[data-cy=export-send-errors]').should('be.disabled');
    cy.then(() => { mode = 'restricted'; });
    cy.get('.campaign-send-error-report .notification.is-danger button').click();
    cy.wait('@errors');
    cy.get('[data-cy=send-error-attempts]').should('have.text', '44');
    cy.get('[data-cy=export-send-errors]').should('be.disabled');
    cy.then(() => { mode = 'rows'; });
    cy.get('.send-error-refresh button').click();
    cy.wait('@errors');
    cy.viewport(390, 844);
    cy.get('.campaign-send-error-report').within(() => {
      cy.get('[data-cy=export-send-errors]').scrollIntoView().should('be.visible').then(($button) => {
        const rect = $button[0].getBoundingClientRect();
        expect(rect.left).to.be.at.least(0);
        expect(rect.right).to.be.at.most(390);
      });
      cy.get('.send-error-customer').first().scrollIntoView().should('contain', '张伟');
      cy.contains('button', '关闭').scrollIntoView().should('be.visible');
    });
    cy.screenshot('send-error-overview-mobile', { capture: 'viewport' });
    cy.get('.send-error-close').click();
    cy.get('.campaign-send-error-report').should('not.exist');
  });

  it('shows durable totals, updates running totals and retains them after a pause', () => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'zh-CN'; }));
    const campaign = (id, name, status, sendErrors) => ({
      id, name, subject: name, status, send_errors: sendErrors,
      type: 'regular', tags: [], customer_lists: [], customer_pools: [],
      created_at: '2026-10-09T00:00:00Z', owner_username: 'admin', visibility: 'private',
      views: 0, clicks: 0, sent: 3, to_send: 10, unsent_count: 7, bounces: 0,
      next_resume_at: status === 'deferred' ? '2026-10-10T09:00:00Z' : null,
    });
    let paused = false;
    cy.intercept({ method: 'GET', pathname: '/api/campaigns' }, (req) => req.reply({ data: {
      results: [
        campaign(1, '发送中的活动', paused ? 'paused' : 'running', paused ? 8 : 4),
        campaign(2, '延期中的活动', 'deferred', 5),
        campaign(3, '没有错误的活动', 'finished', 0),
      ],
      total: 3, per_page: 20,
    } })).as('campaigns');
    cy.intercept('GET', '/api/campaigns/running/stats', { data: [
      campaign(1, '发送中的活动', 'running', 7),
    ] }).as('liveStats');
    cy.loginAndVisit('/admin/campaigns');
    cy.wait('@campaigns');
    cy.contains('tbody tr', '延期中的活动').find('.campaign-send-errors')
      .should('contain', '发送错误次数').and('contain', '5').and('have.class', 'has-text-danger')
      .and('have.attr', 'title').and('contain', '同一收件人重试失败会累计');
    cy.contains('tbody tr', '没有错误的活动').find('.campaign-send-errors')
      .should('contain', '0').and('not.have.class', 'has-text-danger');
    cy.wait('@liveStats');
    cy.contains('tbody tr', '发送中的活动').find('.campaign-send-errors').should('contain', '7');
    paused = true;
    cy.intercept('GET', '/api/campaigns/running/stats', { data: [] }).as('stoppedStats');
    cy.wait('@stoppedStats');
    cy.wait('@campaigns');
    cy.contains('tbody tr', '发送中的活动').should('contain', '暂停')
      .find('.campaign-send-errors').should('contain', '8');
    cy.reload();
    cy.wait('@campaigns');
    cy.contains('tbody tr', '发送中的活动').find('.campaign-send-errors').should('contain', '8');
    cy.viewport(390, 844);
    cy.contains('tbody tr', '延期中的活动').find('.campaign-send-errors').should('be.visible').and('contain', '5');
  });
});
