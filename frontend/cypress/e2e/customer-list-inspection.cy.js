describe('Platform private-list inspection', () => {
  it('opens a foreign-workspace list from its customer count and on direct reload', () => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'zh-CN'; }));
    cy.loginAndVisit('/admin/customer-lists');
    cy.request('/api/profile').then(({ body: profile }) => cy.request('POST', '/api/organizations', {
      name: 'Inspection organization', manager_user_id: profile.data.id,
    })).then(({ body: organization }) => {
      const headers = { 'X-Listmonk-Organization-ID': String(organization.data.id) };
      cy.request({ method: 'POST', url: '/api/customer-lists', headers,
        body: { name: 'Cross-workspace inspection audience', type: 'private', optin: 'single' } }).then(({ body: list }) => {
        const listID = list.data.id;
        cy.request({ method: 'POST', url: '/api/customers', headers, body: {
          customer_code: 'INSPECTION-E2E', name: '跨工作区查看客户', email: 'inspection-e2e@example.test',
          status: 'enabled', customer_list_ids: [listID],
        } });
        // Keep the browser in personal space while the target list is organizational.
        cy.visit('/admin/customer-lists');
        cy.contains('tbody tr', 'Cross-workspace inspection audience').find('a.customer-count').click();
        cy.location('pathname').should('eq', `/admin/customers/customer-lists/${listID}`);
        cy.contains('tbody tr', 'INSPECTION-E2E').should('contain', '跨工作区查看客户');
        cy.reload();
        cy.contains('tbody tr', 'INSPECTION-E2E').should('contain', 'inspection-e2e@example.test');
        cy.get('.notification.is-danger').should('not.exist');
        cy.request('/api/customer-lists?minimal=true&type_group=private&status=active').then(({ body }) => {
          const rows = body.data.results || [];
          expect(rows.map((item) => item.id)).not.to.include(listID);
        });
        cy.screenshot('platform-private-list-inspection', { capture: 'viewport' });
      });
    });
  });
});
