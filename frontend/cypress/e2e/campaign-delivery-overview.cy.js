/* eslint-env mocha */
/* global cy, expect */

describe('Campaign delivery overview', () => {
  before(() => cy.resetDB());

  it('filters organizations, preserves applied filters for export, and retries only selected rows', () => {
    cy.viewport(1280, 720);
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.loginAndVisit('/admin/campaigns');
    cy.request('POST', '/api/campaigns', {
      name: 'Delivery overview E2E',
      subject: 'Delivery overview',
      type: 'regular',
      content_type: 'plain',
      body: 'Test campaign',
      customer_list_ids: [1],
      smtp_source: 'personal',
      smtp_rate_limit: 20,
      template_id: 1,
    }).then(({ body }) => {
      const { id } = body.data;
      cy.intercept('GET', `/api/campaigns/${id}/delivery-overview`, {
        data: {
          campaign_id: id,
          status: 'paused',
          source: 'pool',
          planned: 3,
          sent: 1,
          unsent: 2,
          waiting: 0,
          failed: 2,
          progress: 33,
          send_errors: 4,
          organizations: [
            {
              organization_id: 11, name: 'North', planned: 2, sent: 1, unsent: 1, failed: 1,
            },
            {
              organization_id: 12, name: 'South', planned: 1, sent: 0, unsent: 1, failed: 1,
            },
          ],
        },
      }).as('overview');

      const row = (orgId, recipientId, name) => ({
        recipient_type: 'pool',
        recipient_id: recipientId,
        organization_id: orgId,
        customer_code: `P-${recipientId}`,
        name,
        email: 'masked@example.test',
        stage: 'send',
        category: 'smtp_rejected',
        smtp_code: 550,
        error: '',
        count: 2,
        last_at: '2026-10-20T08:00:00Z',
        status: 'pending',
        retryable: true,
      });
      const rows = [row(11, 101, 'Alex'), row(12, 102, 'Bailey')];
      cy.intercept({ method: 'GET', pathname: `/api/campaigns/${id}/failed-recipients` }, (req) => {
        const filtered = rows.filter((item) => !req.query.organization_id
          || String(item.organization_id) === req.query.organization_id);
        req.reply({
          data: {
            results: filtered,
            total: filtered.length,
            page: Number(req.query.page || 1),
            per_page: 20,
            can_export: true,
            can_retry: true,
          },
        });
      }).as('failed');
      cy.intercept({ method: 'GET', pathname: `/api/campaigns/${id}/failed-recipients/export` }, (req) => {
        expect(req.query.organization_id).to.eq('12');
        expect(req.query.search).to.eq('');
        req.reply({ body: 'export fixture', headers: { 'content-type': 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' } });
      }).as('export');
      let rejectRetry = true;
      cy.intercept('POST', `/api/campaigns/${id}/failed-recipients/retry`, (req) => {
        expect(req.body.recipients).to.deep.eq([{ type: 'pool', id: 102 }]);
        if (rejectRetry) { req.reply({ statusCode: 409, body: { message: 'Selection changed' } }); return; }
        req.reply({ data: { selected: 1 } });
      }).as('retry');

      cy.visit(`/admin/campaigns/${id}#overview`);
      cy.wait('@overview');
      cy.wait('@failed');
      cy.get('.loading-overlay.is-active').should('not.be.visible');
      cy.window().then((win) => expect(win.document.documentElement.scrollWidth).to.be.at.most(win.innerWidth));
      cy.get('[data-cy=delivery-planned]').should('contain', '3');
      cy.contains('North').should('be.visible');
      cy.get('[data-cy=failed-org-filter]').select('12');
      cy.wait('@failed').its('request.query').should('include', { organization_id: '12' });
      cy.contains('tbody tr', 'Bailey').should('be.visible');
      cy.contains('tbody tr', 'Alex').should('not.exist');
      cy.get('[data-cy=failed-search]').type('unsubmitted');
      cy.get('[data-cy=failed-export]').click();
      cy.wait('@export');
      cy.readFile(`cypress/downloads/campaign-${id}-failed-recipients.xlsx`).should('eq', 'export fixture');
      cy.get('[data-cy=failed-search]').clear();
      cy.get('tbody .b-checkbox.checkbox').click();
      cy.get('[data-cy=failed-retry]').click();
      cy.get('.dialog .modal-card-foot button.is-primary').click();
      cy.wait('@retry').its('response.statusCode').should('eq', 409);
      cy.wait('@failed');
      cy.get('.delivery-panel .help.is-danger').should('be.visible');
      cy.get('[data-cy=failed-retry]').should('be.disabled');
      cy.then(() => { rejectRetry = false; });
      cy.get('tbody .b-checkbox.checkbox').click();
      cy.get('[data-cy=failed-retry]').click();
      cy.get('.dialog .modal-card-foot button.is-primary').click();
      cy.wait('@retry').its('response.statusCode').should('eq', 200);
      cy.get('.loading-overlay.is-active').should('not.be.visible');
      cy.reload();
      cy.wait('@overview');
      cy.wait('@failed');
      cy.get('[data-cy=campaign-delivery-overview]').should('be.visible');
      cy.viewport(390, 844);
      cy.get('.loading-overlay.is-active').should('not.be.visible');
    });
  });

  it('renders private progress, pages and resets selection, and handles empty, read-only and failed responses', () => {
    cy.viewport(1280, 720);
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'zh-CN'; }));
    cy.loginAndVisit('/admin/campaigns');
    cy.request('POST', '/api/campaigns', {
      name: 'Private delivery overview E2E',
      subject: 'Private delivery',
      type: 'regular',
      content_type: 'plain',
      body: 'Test campaign',
      customer_list_ids: [1],
      smtp_source: 'personal',
      smtp_rate_limit: 20,
      template_id: 1,
    }).then(({ body }) => {
      const { id } = body.data;
      let mode = 'normal';
      const rows = Array.from({ length: 21 }, (_, index) => ({
        recipient_type: 'private',
        recipient_id: index + 1,
        organization_id: 0,
        customer_code: `C-${index + 1}`,
        name: `Customer ${index + 1}`,
        email: 'redacted@example.test',
        category: 'network',
        smtp_code: 0,
        status: index === 1 ? 'deferred' : 'pending',
        retryable: index !== 1,
      }));
      cy.intercept('GET', `/api/campaigns/${id}/delivery-overview`, (req) => {
        if (mode === 'overview-error') { req.reply({ statusCode: 503, body: { message: 'Unavailable' } }); return; }
        req.reply({
          data: {
            campaign_id: id,
            status: 'paused',
            source: 'private',
            planned: 100,
            sent: 70,
            unsent: 30,
            waiting: 9,
            failed: 21,
            progress: 70,
            send_errors: 44,
            organizations: [],
          },
        });
      }).as('privateOverview');
      cy.intercept({ method: 'GET', pathname: `/api/campaigns/${id}/failed-recipients` }, (req) => {
        if (mode === 'list-error') { req.reply({ statusCode: 503, body: { message: 'Unavailable' } }); return; }
        const page = Number(req.query.page || 1);
        const size = Number(req.query.per_page || 20);
        req.reply({
          data: {
            results: mode === 'empty' ? [] : rows.slice((page - 1) * size, page * size),
            total: mode === 'empty' ? 0 : rows.length,
            page,
            per_page: size,
            can_export: mode !== 'readonly',
            can_retry: mode !== 'readonly',
          },
        });
      }).as('privateFailed');
      const refresh = () => {
        cy.get('.delivery-panel-heading button').first().click();
        cy.wait('@privateOverview');
        cy.wait('@privateFailed');
      };
      cy.visit(`/admin/campaigns/${id}#overview`);
      cy.wait('@privateOverview');
      cy.wait('@privateFailed');
      cy.get('.loading-overlay.is-active').should('not.be.visible');
      cy.window().then((win) => expect(win.document.documentElement.scrollWidth).to.be.at.most(win.innerWidth));
      cy.get('.delivery-private-grid').should('be.visible');
      cy.get('[data-cy=delivery-failed]').should('have.text', '21');
      cy.get('tbody .b-checkbox.checkbox').first().click();
      cy.get('tbody input[type=checkbox]').eq(1).should('be.disabled');
      cy.get('[data-cy=failed-retry]').should('be.enabled');
      cy.get('[data-cy=failed-select-page]').click();
      cy.get('tbody input[type=checkbox]:checked').should('have.length', 19);
      cy.get('tbody input[type=checkbox]').eq(1).should('not.be.checked');
      cy.get('[data-cy=failed-select-page]').click();
      cy.get('tbody input[type=checkbox]:checked').should('have.length', 0);
      cy.get('[data-cy=failed-retry]').should('be.disabled');
      cy.get('tbody .b-checkbox.checkbox').first().click();
      cy.get('.pagination-list').contains('2').click();
      cy.wait('@privateFailed').its('request.query').should('include', { page: '2' });
      cy.contains('tbody tr', 'C-21').should('be.visible');
      cy.get('[data-cy=failed-retry]').should('be.disabled');
      cy.get('select[data-cy=page-size]').select('10');
      cy.wait('@privateFailed').its('request.query').should('include', { page: '1', per_page: '10' });
      cy.get('.loading-overlay.is-active').should('not.be.visible');
      cy.viewport(390, 844);
      cy.get('[data-cy=failed-export]').scrollIntoView().should('be.visible').then(($button) => {
        expect($button[0].getBoundingClientRect().right).to.be.at.most(390);
      });
      cy.viewport(1280, 720);
      cy.then(() => { mode = 'readonly'; });
      refresh();
      cy.get('[data-cy=failed-export]').should('be.disabled');
      cy.get('tbody input[type=checkbox]').first().should('be.disabled');
      cy.then(() => { mode = 'empty'; });
      refresh();
      cy.get('.delivery-empty').should('be.visible');
      cy.get('[data-cy=failed-export]').should('be.disabled');
      cy.then(() => { mode = 'list-error'; });
      refresh();
      cy.get('.delivery-panel .notification.is-danger').should('be.visible');
      cy.get('[data-cy=failed-select-page] input').should('be.disabled');
      cy.get('.delivery-empty').should('not.exist');
      cy.then(() => { mode = 'overview-error'; });
      refresh();
      cy.get('[data-cy=campaign-delivery-overview] > .notification.is-danger').should('be.visible');
      cy.get('.delivery-metrics').should('not.exist');
      cy.then(() => { mode = 'normal'; });
      cy.get('[data-cy=campaign-delivery-overview] > .notification button').click();
      cy.wait('@privateOverview');
      cy.get('.delivery-private-grid').should('be.visible');
    });
  });
});
