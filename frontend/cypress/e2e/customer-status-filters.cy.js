describe('Customer status columns and filters', () => {
  it('shows private blocklist and unsubscribe states with persistent server filters', () => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'zh-CN'; }));
    cy.loginAndVisit('/admin/customers');
    cy.request('POST', '/api/customers', { customer_code: 'STATE-BLOCK', name: '黑名单客户',
      email: 'state-block@example.test', status: 'blocklisted', customer_list_ids: [1] });
    cy.request('POST', '/api/customers', { customer_code: 'STATE-UNSUB', name: '退订客户',
      email: 'state-unsub@example.test', status: 'enabled', customer_list_ids: [1] }).then(({ body: recipient }) => {
      cy.request('POST', '/api/campaigns', { name: 'State opt-out fixture', subject: 'Opt-out', type: 'regular',
        content_type: 'plain', body: 'Fixture', customer_list_ids: [1], smtp_source: 'personal', template_id: 1 }).then(({ body }) => {
        cy.request({ method: 'POST', url: `/subscription/${body.data.uuid}/${recipient.data.uuid}`, form: true,
          body: { 'List-Unsubscribe': 'One-Click' } });
      });
    });
    cy.visit('/admin/customers');
    cy.get('thead .cy-customer-status').should('contain', '状态');
    cy.contains('tbody tr', 'STATE-BLOCK').find('[data-cy=contact-state-blocklisted]').should('contain', '黑名单');
    cy.contains('tbody tr', 'STATE-UNSUB').find('[data-cy=contact-state-unsubscribed]').should('contain', '已退订');
    cy.intercept({ method: 'GET', pathname: '/api/customers' }).as('customers');
    cy.get('[data-cy=customer-status-filter]').select('blocklisted');
    cy.wait('@customers').its('request.query').should('include', { contact_status: 'blocklisted', page: '1' });
    cy.get('tbody tr').should('have.length', 1).and('contain', 'STATE-BLOCK');
    cy.reload();
    cy.get('[data-cy=customer-status-filter]').should('have.value', 'blocklisted');
    cy.get('tbody tr').should('have.length', 1).and('contain', 'STATE-BLOCK');
    cy.get('[data-cy=customer-status-filter]').select('unsubscribed');
    cy.contains('tbody tr', 'STATE-UNSUB').should('be.visible');
    cy.get('tbody tr').should('not.contain', 'STATE-BLOCK');
    cy.get('[data-cy=search]').type('STATE-UNSUB{enter}');
    cy.contains('tbody tr', 'STATE-UNSUB').should('be.visible');
    cy.reload();
    cy.get('[data-cy=search]').should('have.value', 'state-unsub');
    cy.get('[data-cy=customer-status-filter]').should('have.value', 'unsubscribed');
    cy.screenshot('private-customer-status-desktop', { capture: 'viewport' });
    cy.viewport(390, 844);
    cy.contains('tbody tr', 'STATE-UNSUB').find('[data-cy=contact-state-unsubscribed]').scrollIntoView().should('be.visible');
    cy.screenshot('private-customer-status-mobile', { capture: 'viewport' });
  });

  it('filters actual pool blocklists in the aggregate and single-pool pages', () => {
    cy.resetDB();
    cy.viewport(1400, 950);
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'zh-CN'; }));
    cy.loginAndVisit('/admin/pool');
    cy.request('POST', '/api/customer-lists', { name: 'State filter pool', type: 'pool', optin: 'single' }).then(({ body }) => {
      const poolID = body.data.id;
      cy.request('POST', `/api/customer-lists/${poolID}/pool-contacts`, {
        customer_code: 'POOL-BLOCK', name: '公海黑名单', email: 'pool-state-block@example.test',
      }).then(({ body: contact }) => cy.request('PUT', '/api/pools/contacts/blocklist', { contact_ids: [contact.data.id], blocked: true }));
      cy.request('POST', `/api/customer-lists/${poolID}/pool-contacts`, {
        customer_code: 'POOL-NORMAL', name: '正常公海客户', email: 'pool-state-normal@example.test',
      });
      cy.visit('/admin/pool');
      cy.get('[data-cy=pool-status-filter]').should('have.value', 'all');
      cy.contains('tbody tr', 'POOL-BLOCK').find('[data-cy=contact-state-blocklisted]').should('contain', '黑名单');
      cy.intercept({ method: 'GET', pathname: '/api/pools/contacts' }).as('poolContacts');
      cy.get('[data-cy=pool-status-filter]').select('blocklisted');
      cy.wait('@poolContacts').its('request.query').should('include', { status: 'blocklisted', page: '1' });
      cy.get('[data-cy=pool-contacts-table] tbody tr').should('have.length', 1).and('contain', 'POOL-BLOCK');
      cy.reload();
      cy.get('[data-cy=pool-status-filter]').should('have.value', 'blocklisted');
      cy.contains('tbody tr', 'POOL-BLOCK').find('a').contains('State filter pool').click();
      cy.location('pathname').should('eq', `/admin/pool-lists/${poolID}/contacts`);
      cy.get('[data-cy=pool-status-filter]').select('blocklisted');
      cy.get('[data-cy=pool-contacts-table] tbody tr').should('have.length', 1).and('contain', 'POOL-BLOCK');
      cy.screenshot('pool-customer-status-desktop', { capture: 'viewport' });
      cy.get('[data-cy=pool-status-filter]').select('active');
      cy.get('[data-cy=pool-contacts-table] tbody tr').should('have.length', 1).and('contain', 'POOL-NORMAL');
      cy.get('[data-cy=pool-status-filter]').select('unsubscribed');
      cy.get('[data-cy=pool-contacts-table] tbody tr').should('not.contain', 'POOL-NORMAL');
    });
  });
});
