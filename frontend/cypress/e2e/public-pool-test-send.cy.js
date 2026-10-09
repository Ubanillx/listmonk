describe('Public-pool campaign test sending', () => {
  it('tests a global public pool through the active organization SMTP pool', () => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.loginAndVisit('/admin/campaigns');
    const subject = `public-pool-test-${Date.now()}`;
    const recipient = 'public-pool-test-recipient@example.com';
    let org;
    let pool;
    let smtpPool;
    let campaign;
    const headers = () => ({ 'X-Listmonk-Organization-ID': org });
    cy.request('/api/profile').then(({ body }) => cy.request('POST', '/api/organizations', {
      name: 'Public pool test team', manager_user_id: body.data.id,
    })).then(({ body }) => { org = body.data.id; });
    cy.request('POST', '/api/customer-lists', { name: 'Global public test pool', type: 'pool', optin: 'single' })
      .then(({ body }) => { pool = body.data.id; });
    cy.then(() => cy.request('POST', '/api/pools/allocations', {
      pool_id: pool, organization_id: org, name: 'Team public pool allocation',
    }));
    cy.then(() => cy.request({ method: 'POST', url: '/api/organizations/smtp-pools', headers: headers(),
      body: { name: 'Test organization senders' } })).then(({ body }) => { smtpPool = body.data.id; });
    cy.then(() => cy.request({ method: 'PUT', url: `/api/organizations/smtp?pool_id=${smtpPool}`, headers: headers(),
      body: { smtp: [{ name: 'Organization test sender', enabled: true, host: 'host.docker.internal', port: 6125,
        auth_protocol: 'none', tls_type: 'none', from_email: 'organization-test@example.com', daily_limit: 10 }] } }));
    cy.then(() => cy.request({ method: 'POST', url: '/api/profile/reply-mailboxes', headers: headers(),
      body: { email: 'team-reply@example.com', ai_enabled: false } })).then(({ body }) => cy.request({
      method: 'PUT', url: `/api/organizations/${org}/reply-mailbox`, headers: headers(),
      body: { reply_mailbox_id: body.data.id },
    }));
    // Test recipients remain ordinary customers owned in the active workspace.
    cy.then(() => cy.request({ method: 'POST', url: '/api/customers', headers: headers(),
      body: { email: recipient, name: 'Test recipient', customer_code: 'TEST-POOL', status: 'enabled', customer_list_ids: [] } }));
    cy.then(() => cy.request({ method: 'POST', url: '/api/campaigns', headers: headers(),
      body: { name: 'Public-pool test campaign', subject, messenger: 'email', smtp_source: 'organization', smtp_pool_id: smtpPool,
        pool_scope: 'organization', content_type: 'plain', body: 'Public-pool test content', customer_list_ids: [pool] } }))
      .then(({ body }) => { campaign = body.data.id; });
    cy.then(() => {
      cy.window().then((win) => win.localStorage.setItem('listmonk.workspace.organizationId', String(org)));
      cy.setCookie('listmonk_workspace_organization_id', String(org));
      cy.visit(`/admin/campaigns/${campaign}`);
    });
    cy.get('[data-cy=campaign-audience-trigger]').should('contain', 'Global public test pool');
    cy.get('[data-cy=campaign-smtp-source]').should('have.value', 'organization');
    cy.get('[data-cy=campaign-test-section] input').type(`${recipient}{enter}`);
    cy.intercept('POST', '/api/campaigns/*/test').as('testPublicPool');
    cy.get('[data-cy=campaign-test-section] button').contains('Send').click();
    cy.wait('@testPublicPool').then(({ request, response }) => {
      expect(request.body.customer_list_ids).to.deep.equal([pool]);
      expect(request.body.smtp_pool_id).to.eq(smtpPool);
      expect(response.statusCode, JSON.stringify(response.body)).to.eq(200);
    });
    let messages = [];
    cy.waitUntil(() => cy.request('http://127.0.0.1:8265/api/v2/messages?limit=100').then(({ body }) => {
      messages = body.items.filter((message) => (message.Content.Headers.Subject || []).join('') === subject);
      return messages.length === 1;
    }), { timeout: 20000, interval: 500 });
    cy.then(() => {
      expect(messages[0].Content.Headers.From.join('')).to.contain('organization-test@example.com');
      expect(messages[0].Content.Headers.To.join('')).to.contain(recipient);
    });
    cy.get('[data-cy=campaign-test-section]').screenshot('public-pool-test-sent');
  });
});
