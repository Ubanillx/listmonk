describe('Campaign unsubscribe recipients', () => {
  it('records confirmed opt-outs once and shows durable list/detail counts', () => {
    cy.resetDB();
    cy.task('enableIndividualTracking', null, { timeout: 120000 });
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'zh-CN'; }));
    cy.loginAndVisit('/admin/campaigns');
    cy.request('POST', '/api/customers', {
      email: 'unsubscribe-e2e@example.test', name: '退订测试客户', customer_code: 'UNSUB-E2E', status: 'enabled', customer_list_ids: [1],
    }).then(({ body: customerBody }) => {
      const customer = customerBody.data;
      cy.request('POST', '/api/campaigns', {
        name: '退订人数回归', subject: 'Unsubscribe E2E', type: 'regular', content_type: 'richtext', auto_track_links: true,
        body: '<p><a href="{{ UnsubscribeURL }}">Unsubscribe</a></p>', customer_list_ids: [1], smtp_source: 'personal', template_id: 1,
      }).then(({ body }) => {
        const campaign = body.data;
        const url = `/subscription/${campaign.uuid}/${customer.uuid}`;
        cy.request(url).its('status').should('eq', 200);
        cy.request(`/api/campaigns/${campaign.id}`).its('body.data.unsubscribes').should('eq', 0);
        cy.request(`/campaign/${campaign.uuid}/${customer.uuid}`).then(({ body: html }) => {
          const document = new DOMParser().parseFromString(html, 'text/html');
          const anchor = Array.from(document.querySelectorAll('a')).find((el) => el.textContent.trim() === 'Unsubscribe');
          expect(Boolean(anchor), `rendered unsubscribe link in ${html}`).to.eq(true);
          const trackingPath = new URL(anchor.getAttribute('href')).pathname;
          expect(trackingPath).to.match(/^\/link\//);
          cy.request({ url: trackingPath, followRedirect: false }).its('status').should('eq', 307);
          cy.request({ url: trackingPath, followRedirect: false }).its('status').should('eq', 307);
        });
        cy.request(`/api/campaigns/${campaign.id}`).then(({ body: stats }) => {
          expect(stats.data.unsubscribe_clicks).to.eq(1);
          expect(stats.data.unsubscribes).to.eq(0);
        });
        cy.visit('/admin/campaigns');
        cy.contains('tbody tr', '退订人数回归').find('.campaign-unsubscribe-clicks a')
          .should(($link) => expect($link.text().trim()).to.eq('1')).click();
        cy.get('[data-cy=unsubscribe-report]').within(() => {
          cy.contains('tbody tr', '退订测试客户').should('contain', '已点击，未确认');
          cy.get('#unsubscribe-status').select('confirmed');
          cy.contains('暂无已记录的退订。').should('be.visible');
        });
        cy.contains('[data-cy=unsubscribe-report] button', '关闭').click();
        cy.request('POST', url, { 'List-Unsubscribe': 'One-Click' }).its('status').should('eq', 200);
        cy.request('POST', url, { 'List-Unsubscribe': 'One-Click' }).its('status').should('eq', 200);
        cy.request(`/api/campaigns/${campaign.id}`).its('body.data.unsubscribes').should('eq', 1);
        cy.request(`/api/campaigns/${campaign.id}/unsubscribes?per_page=1`).then(({ body: report }) => {
          expect(report.data.total).to.eq(1);
          expect(report.data.results[0].name).to.eq('退订测试客户');
          expect(report.data.results[0].status).to.eq('confirmed');
          expect(report.data.results[0].click_count).to.eq(2);
          expect(report.data.results[0]).not.to.have.property('uuid');
        });
        cy.visit('/admin/campaigns');
        cy.contains('tbody tr', '退订人数回归').within(() => {
          cy.get('.campaign-unsubscribes').should('contain', '退订人数').and('contain', '1');
          cy.get('.campaign-unsubscribes a').click();
        });
        cy.get('[data-cy=unsubscribe-report]').within(() => {
          cy.contains('tbody tr', '退订测试客户').should('contain', customer.email).and('contain', '已确认退订');
          cy.get('#unsubscribe-search').type('no-such-recipient{enter}');
          cy.contains('暂无已记录的退订。').should('be.visible');
          cy.get('#unsubscribe-search').clear().type('退订测试客户{enter}');
          cy.contains('tbody tr', '退订测试客户').should('be.visible');
        });
        cy.get('[data-cy=unsubscribe-report]').screenshot('campaign-unsubscribe-details-desktop');
        cy.viewport(390, 844);
        cy.get('[data-cy=unsubscribe-report]').within(() => {
          cy.contains('退订测试客户').scrollIntoView().should('be.visible');
          cy.contains('#unsubscribe-title', '退订记录').should('be.visible');
          cy.contains('button', '关闭').should('be.visible');
        });
        cy.screenshot('campaign-unsubscribe-details-mobile', { capture: 'viewport' });
        cy.contains('[data-cy=unsubscribe-report] button', '关闭').click();
        cy.reload();
        cy.contains('tbody tr', '退订人数回归').find('.campaign-unsubscribes').should('contain', '1');
      });
    });
  });

  it('pages the report and recovers from an unavailable detail API', () => {
    cy.resetDB();
    cy.viewport(1400, 950);
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'zh-CN'; }));
    cy.intercept({ method: 'GET', pathname: '/api/campaigns' }, { data: {
      results: [{ id: 901, name: '退订分页', subject: 'Opt-out', status: 'paused', type: 'regular', tags: [],
        customer_lists: [], customer_pools: [], owner_username: 'admin', visibility: 'private',
        views: 0, clicks: 0, sent: 30, to_send: 30, unsubscribes: 21, send_errors: 0 }],
      total: 1, per_page: 20,
    } });
    let failed = true;
    const rows = Array.from({ length: 21 }, (_, i) => ({
      recipient_type: 'private', recipient_id: i + 1, customer_code: `OPT-${i}`,
      name: `退订客户 ${i}`, email: 'a***@example.test', created_at: '2026-10-30T01:00:00Z',
      confirmed_at: '2026-10-30T01:00:00Z', status: 'confirmed',
    }));
    cy.intercept({ method: 'GET', pathname: '/api/campaigns/901/unsubscribes' }, (req) => {
      if (failed) { req.reply({ statusCode: 503, body: { message: 'Unavailable' } }); return; }
      const page = Number(req.query.page || 1);
      const size = Number(req.query.per_page || 20);
      req.reply({ data: { results: rows.slice((page - 1) * size, page * size), total: rows.length, page, per_page: size } });
    }).as('unsubscribes');
    cy.loginAndVisit('/admin/campaigns');
    cy.get('.campaign-unsubscribes a').click();
    cy.wait('@unsubscribes');
    cy.get('[data-cy=unsubscribe-report] .notification.is-danger').should('be.visible');
    cy.then(() => { failed = false; });
    cy.get('[data-cy=unsubscribe-report] .notification.is-danger button').click();
    cy.wait('@unsubscribes');
    cy.get('[data-cy=unsubscribe-report]').within(() => {
      cy.contains('tbody tr', 'OPT-0').should('be.visible');
      cy.get('.pagination-list').contains('2').click();
    });
    cy.wait('@unsubscribes').its('request.query').should('include', { page: '2', per_page: '20' });
    cy.get('[data-cy=unsubscribe-report]').within(() => {
      cy.contains('tbody tr', 'OPT-20').should('be.visible');
      cy.get('select[data-cy=page-size]').select('10');
    });
    cy.wait('@unsubscribes').its('request.query').should('include', { page: '1', per_page: '10' });
  });
});
