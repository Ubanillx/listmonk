// Uses the isolated Cypress database. Only the external mailbox connection is
// stubbed; saves, password preservation, validation and settings are real APIs.
describe('Reply mailbox configuration', () => {
  const input = (name) => `[data-cy=reply-mailbox-${name}] input, input[data-cy=reply-mailbox-${name}]`;

  beforeEach(() => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'zh-CN'; }));
    cy.loginAndVisit('/admin/user/profile');
    cy.contains('.b-tabs nav a', '个人邮件配置').click();
    // Startup configuration is stable in this suite. Keep it available while
    // a real settings save restarts the backend and its health endpoint.
    cy.request('/api/config').then(({ body }) => {
      cy.intercept('GET', '/api/config', { body: { data: { ...body.data, lang: 'zh-CN' } } });
    });
  });

  it('saves a reply address without receiving credentials, then opts into AI', () => {
    cy.contains('.reply-mailboxes button', '新建').click();
    cy.get('[data-cy=reply-mailbox-receiving]').should('not.exist');
    cy.get('[data-cy=reply-mailbox-test]').should('not.exist');
    cy.get(input('email')).type('replies@example.com');
    cy.intercept('POST', '/api/profile/reply-mailboxes', (req) => {
      expect(req.body).to.deep.equal({ email: 'replies@example.com', name: '', is_default: false, ai_enabled: false });
      req.continue();
    }).as('saveAddress');
    cy.get('[data-cy=reply-mailbox-save]').click();
    cy.wait('@saveAddress').then(({ response }) => {
      expect(response.statusCode).to.equal(200);
      expect(response.body.data.status).to.equal('active');
      expect(response.body.data.has_password).to.equal(false);
      expect(response.body.data).not.to.have.property('password');
    });
    cy.contains('.reply-mailbox-card .tag', '回信地址可用').should('be.visible');
    cy.get('[data-cy=reply-mailbox-ai]').click();
    cy.get('[data-cy=reply-mailbox-receiving]').should('be.visible');
    cy.get('[data-cy=reply-mailbox-test]').should('be.disabled');
    cy.get(input('password')).type('test-only-mailbox-password');
    cy.get(input('host')).clear().type('imap.example.com');
    cy.intercept('PUT', '/api/profile/reply-mailboxes/*').as('saveAI');
    cy.get('[data-cy=reply-mailbox-save]').click();
    cy.wait('@saveAI').then(({ response }) => {
      expect(response.body.data.status).to.equal('pending');
      expect(response.body.data.has_password).to.equal(true);
      expect(response.body.data).not.to.have.property('password');
    });
    cy.get(input('password')).should('have.value', '');
    cy.get('[data-cy=reply-mailbox-test]').should('not.be.disabled');
    // Refresh also reuses server-side credentials, without a secret in the UI.
    cy.reload();
    cy.contains('.b-tabs nav a', '个人邮件配置').click();
    cy.get(input('password')).should('have.value', '').and('have.attr', 'placeholder', '已保存，留空表示不修改');
    cy.intercept('POST', '/api/profile/reply-mailboxes/test', (req) => {
      expect(Object.keys(req.body)).to.deep.equal(['id']);
      req.reply({ body: { data: true } });
    }).as('testConnection');
    cy.get('[data-cy=reply-mailbox-test]').click();
    cy.wait('@testConnection');
    cy.contains('.reply-mailbox-card .tag', '已验证').should('be.visible');
    // Unsaved edits cannot accidentally test or verify stale configuration.
    cy.get(input('name')).type('Customer replies');
    cy.get('[data-cy=reply-mailbox-test]').should('be.disabled');
    cy.get('[data-cy=reply-mailbox-save]').click();
    cy.wait('@saveAI').then(({ request, response }) => {
      expect(request.body).not.to.have.property('password');
      expect(response.body.data.has_password).to.equal(true);
    });
    cy.get('[data-cy=reply-mailbox-test]').should('not.be.disabled');
    cy.get('.reply-mailbox-card').screenshot('reply-mailbox-ai-saved');
    cy.viewport(390, 1244);
    cy.get('.reply-mailbox-card').screenshot('reply-mailbox-ai-mobile');
    cy.document().then((doc) => expect(doc.documentElement.scrollWidth).to.be.at.most(390));
    // Turning AI off hides the connection and keeps credentials for future use.
    cy.get('[data-cy=reply-mailbox-ai]').click();
    cy.get('[data-cy=reply-mailbox-receiving]').should('not.exist');
    cy.get('[data-cy=reply-mailbox-save]').click();
    cy.wait('@saveAI').then(({ request, response }) => {
      expect(request.body).not.to.have.property('imap_host');
      expect(response.body.data.has_password).to.equal(true);
      expect(response.body.data.status).to.equal('active');
    });
    cy.viewport(1400, 950);
    cy.get('.reply-mailbox-card').screenshot('reply-mailbox-address-only');
  });

  it('keeps saved credentials after a connection failure and allows a retry', () => {
    cy.contains('.reply-mailboxes button', '新建').click();
    cy.get(input('email')).type('retry@example.com');
    cy.get('[data-cy=reply-mailbox-ai]').click();
    cy.get(input('password')).type('test-only-password');
    cy.get('[data-cy=reply-mailbox-save]').click();
    cy.intercept('POST', '/api/profile/reply-mailboxes/test', {
      statusCode: 502, body: { message: 'Test mailbox unavailable' },
    }).as('failedTest');
    cy.get('[data-cy=reply-mailbox-test]').should('not.be.disabled').click();
    cy.wait('@failedTest');
    cy.contains('.reply-mailbox-card .tag', '待验证').should('be.visible');
    cy.get(input('password')).should('have.value', '');
    cy.intercept('POST', '/api/profile/reply-mailboxes/test', (req) => {
      expect(Object.keys(req.body)).to.deep.equal(['id']);
      req.reply({ data: true });
    }).as('retryTest');
    cy.get('[data-cy=reply-mailbox-test]').click();
    cy.wait('@retryTest');
    cy.contains('.reply-mailbox-card .tag', '已验证').should('be.visible');
  });

  it('keeps a disabled mailbox disabled after testing its saved connection', () => {
    cy.request('POST', '/api/profile/reply-mailboxes', {
      email: 'disabled@example.com', ai_enabled: true, password: 'test-only-password',
    }).then(({ body }) => cy.request('DELETE', `/api/profile/reply-mailboxes/${body.data.id}`));
    cy.reload();
    cy.contains('.b-tabs nav a', '个人邮件配置').click();
    cy.contains('.reply-mailbox-card .tag', '已停用').should('be.visible');
    cy.intercept('POST', '/api/profile/reply-mailboxes/test', { body: { data: true } }).as('testDisabled');
    cy.get('[data-cy=reply-mailbox-test]').should('not.be.disabled').click();
    cy.wait('@testDisabled');
    cy.contains('.reply-mailbox-card .tag', '已停用').should('be.visible');
    cy.contains('.reply-card-header button', '启用').should('be.visible');
    cy.request('/api/profile/reply-mailboxes').then(({ body }) => {
      expect(body.data[0].status).to.equal('disabled');
      expect(body.data[0].ai_enabled).to.equal(true);
      expect(body.data[0].has_password).to.equal(true);
    });
    cy.reload();
    cy.contains('.b-tabs nav a', '个人邮件配置').click();
    cy.contains('.reply-mailbox-card .tag', '已停用').should('be.visible');
  });

  it('saves and reloads the interval and rejects invalid intervals while AI is off', () => {
    cy.visit('/admin/settings');
    cy.contains('.b-tabs nav a', '回信 AI').click();
    cy.get('[data-cy=reply-scan-interval]').should('have.value', '60s').clear().type('5m');
    cy.intercept('PUT', '/api/settings').as('saveInterval');
    cy.get('[data-cy=btn-save]').click();
    cy.wait('@saveInterval').its('response.statusCode').should('equal', 200);
    cy.get('[data-cy=btn-save]', { timeout: 60000 }).should('be.disabled');
    cy.visit('/admin/settings');
    cy.contains('.b-tabs nav a', '回信 AI').click();
    cy.get('[data-cy=reply-scan-interval]').should('have.value', '5m0s');
    cy.screenshot('reply-mailbox-check-interval', { capture: 'viewport' });
    cy.request('/api/settings').then(({ body }) => {
      expect(body.data.reply_ai.enabled).to.equal(false);
      body.data.reply_ai.scan_interval = '0s';
      cy.request({ method: 'PUT', url: '/api/settings', body: body.data, failOnStatusCode: false })
        .its('status').should('equal', 400);
      cy.request({ method: 'PUT', url: '/api/settings/reply_ai', body: body.data.reply_ai, failOnStatusCode: false })
        .its('status').should('equal', 400);
    });
  });
});
