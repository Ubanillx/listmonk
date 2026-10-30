describe('Grouped campaign statistics', () => {
  it('shows three recipient states, text error signals, details and responsive groups', () => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'zh-CN'; }));
    const campaign = (id, name, status, errors) => ({
      id, name, subject: 'Grouped delivery', status, send_errors: errors,
      type: 'regular', tags: [], customer_lists: [{ id: 1, name: 'Default', type: 'private' }], customer_pools: [],
      created_at: '2026-10-29T01:00:00Z', updated_at: '2026-10-30T01:00:00Z', started_at: '2026-10-29T02:00:00Z',
      owner_username: 'admin', visibility: 'private', views: 241, clicks: 44,
      sent: 1534, to_send: 2122, unsent_count: 175, unsubscribe_activity: 24,
      delivery_total: 2122, delivery_sent: 1534, delivery_pending: 175, delivery_unavailable: 413,
    });
    let stopped = false;
    cy.intercept({ method: 'GET', pathname: '/api/campaigns' }, (req) => req.reply({ data: {
      results: [campaign(901, 'B 分组统计活动', stopped ? 'paused' : 'running', 2821), campaign(902, '零错误活动', 'finished', 0)],
      total: 2, per_page: 20,
    } })).as('campaigns');
    cy.intercept('GET', '/api/campaigns/running/stats', (req) => req.reply({ data: [
      ...(stopped ? [] : [{ id: 901, status: 'running', send_errors: 2822, delivery_total: 2122, delivery_sent: 1535, delivery_pending: 174,
        delivery_unavailable: 413, unsubscribe_activity: 24 }]),
      // Another campaign remains running when the first stops.
      { id: 999, status: 'running', sent: 1, to_send: 2, send_errors: 0 },
    ] })).as('live');
    cy.intercept('GET', '/api/campaigns/901/send-errors*', { data: {
      results: [], total: 0, total_attempts: 2822, missing_attempts: 0, categories: [], can_export: false,
    } }).as('errors');
    cy.intercept('GET', '/api/campaigns/901/unsubscribes*', { data: {
      results: [], total: 0, page: 1, per_page: 20,
    } }).as('unsubscribes');
    cy.viewport(1440, 1000);
    cy.loginAndVisit('/admin/campaigns');
    cy.wait('@campaigns');
    cy.document().then((doc) => {
      expect(doc.documentElement.scrollWidth).to.be.at.most(1440);
      expect(doc.body.scrollWidth).to.be.at.most(1440);
    });
    cy.get('.campaigns .table-wrapper').then(($wrapper) => {
      const dimensions = { client: $wrapper[0].clientWidth, scroll: $wrapper[0].scrollWidth,
        table: $wrapper[0].querySelector('table').getBoundingClientRect().width, viewport: window.innerWidth };
      expect(dimensions.scroll, JSON.stringify(dimensions)).to.be.at.most(dimensions.client);
    });
    ['发送情况', '客户互动', '发送错误', '退订相关人数'].forEach((label) => cy.contains('th', label).should('be.visible'));
    cy.contains('tbody tr', 'B 分组统计活动').within(() => {
      cy.get('[data-cy=delivery-unavailable]').should('have.text', '413');
      cy.get('.campaign-progress-track').should('be.visible').and('have.attr', 'role', 'meter')
        .find('.campaign-progress-fill').should('have.css', 'width').and('not.equal', '0px');
      cy.get('.campaign-send-errors').should('contain', '有发送错误').and('not.contain', '2821');
      cy.get('.campaign-unsubscribes a').should('have.text', '24');
    });
    cy.contains('tbody tr', '零错误活动').find('.campaign-send-errors').should('have.text', '无发送错误').find('a').should('not.exist');
    cy.contains('tbody tr', '零错误活动').find('.campaign-delivery-note')
      .should('contain', '未发送').and('contain', '2026年10月30日').and('contain', '结束');
    cy.wait('@live');
    cy.contains('tbody tr', 'B 分组统计活动').find('[data-cy=delivery-sent]').should('have.text', '1,535');
    cy.contains('tbody tr', 'B 分组统计活动').find('[data-cy=delivery-pending]').should('have.text', '174');
    cy.contains('tbody tr', 'B 分组统计活动').find('.campaign-send-errors a').click();
    cy.wait('@errors');
    cy.get('.campaign-send-error-report').should('be.visible');
    cy.get('.send-error-close').click();
    cy.contains('tbody tr', 'B 分组统计活动').find('.campaign-unsubscribes a').click();
    cy.wait('@unsubscribes');
    cy.get('[data-cy=unsubscribe-report]').should('be.visible');
    cy.contains('[data-cy=unsubscribe-report] button', '关闭').click();
    cy.screenshot('campaign-stats-B-desktop', { capture: 'viewport' });
    cy.then(() => { stopped = true; });
    cy.wait('@live');
    cy.wait('@campaigns');
    cy.contains('tbody tr', 'B 分组统计活动').should('contain', '暂停').find('[data-cy=delivery-sent]').should('have.text', '1,534');
    cy.viewport(390, 844);
    cy.contains('tbody tr', 'B 分组统计活动').within(() => {
      ['.campaign-delivery', '.campaign-interaction', '.campaign-send-errors', '.campaign-unsubscribes'].forEach((selector) => {
        cy.get(selector).scrollIntoView().should('be.visible').then(($el) => {
          const bounds = $el[0].getBoundingClientRect();
          expect(bounds.left).to.be.at.least(0); expect(bounds.right).to.be.at.most(390);
        });
      });
    });
    cy.document().then((doc) => expect(doc.documentElement.scrollWidth).to.be.at.most(390));
    cy.contains('tbody tr', 'B 分组统计活动').scrollIntoView();
    cy.screenshot('campaign-stats-B-mobile', { capture: 'viewport' });
  });
});
