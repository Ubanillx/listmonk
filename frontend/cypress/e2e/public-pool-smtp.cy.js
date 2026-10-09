const smtp = (name, email, enabled = true) => ({
  name, from_email: email, enabled, host: 'host.docker.internal', port: 6125,
  auth_protocol: 'none', tls_type: 'none', daily_limit: 10,
});

describe('Public-pool SMTP selection', () => {
  it('keeps organization and member SMTP separate, clears hidden pools and refreshes saved readiness', () => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.loginAndVisit('/admin/campaigns/new');
    const orgs = [];
    const pools = [];
    let firstSMTPPool;
    let privateList;
    let campaign;
    cy.request('/api/profile').then(({ body }) => {
      ['First SMTP organization', 'Second SMTP organization'].forEach((name) => {
        cy.request('POST', '/api/organizations', { name, manager_user_id: body.data.id })
          .then((response) => { orgs.push(response.body.data.id); });
      });
    });
    ['First SMTP public pool', 'Second SMTP public pool'].forEach((name) => {
      cy.request('POST', '/api/customer-lists', { name, type: 'pool', optin: 'single' })
        .then(({ body }) => { pools.push(body.data.id); });
    });
    cy.then(() => {
      orgs.forEach((org, index) => {
        const headers = { 'X-Listmonk-Organization-ID': org };
        cy.request({ method: 'POST', url: '/api/organizations/smtp-pools', headers,
          body: { name: 'Organization sender pool' } }).then(({ body }) => {
          if (index === 0) firstSMTPPool = body.data.id;
          cy.request({ method: 'PUT', url: `/api/organizations/smtp?pool_id=${body.data.id}`, headers,
            body: { smtp: [smtp(`organization-${index}`, `organization-${index}@example.com`)] } });
        });
        cy.request({ method: 'POST', url: '/api/profile/reply-mailboxes', headers,
          body: { email: `reply-${index}@example.com`, ai_enabled: false } }).then(({ body }) => {
          cy.request({ method: 'PUT', url: `/api/organizations/${org}/reply-mailbox`, headers,
            body: { reply_mailbox_id: body.data.id } });
        });
        cy.request('POST', '/api/pools/allocations', { pool_id: pools[index], organization_id: org, name: `Allocation ${index}` });
      });
      // The shared organization must appear once in the readiness response.
      cy.request('POST', '/api/pools/allocations', { pool_id: pools[1], organization_id: orgs[0], name: 'Shared allocation' });
      cy.request({ method: 'POST', url: '/api/customer-lists', headers: { 'X-Listmonk-Organization-ID': orgs[0] },
        body: { name: 'Private SMTP audience', type: 'private', optin: 'single' } })
        .then(({ body }) => { privateList = body.data.id; });
    });
    cy.request('PUT', '/api/profile/smtp', { smtp: [smtp('member-personal', 'member-personal@example.com', false)] });
    cy.then(() => {
      cy.window().then((win) => win.localStorage.setItem('listmonk.workspace.organizationId', String(orgs[0])));
      cy.setCookie('listmonk_workspace_organization_id', String(orgs[0]));
      cy.visit(`/admin/campaigns/new?customer_list_id=${pools[0]}`);
    });
    cy.get('[data-cy=campaign-pool-scope]').should('have.value', 'organization');
    cy.then(() => cy.get('[data-cy=campaign-smtp-pool]').should('have.value', String(firstSMTPPool)));
    cy.get('[data-cy=pool-scope-all-notice]').should('not.exist');
    cy.get('[data-cy=campaign-pool-scope]').select('all_organizations');
    cy.get('[data-cy=campaign-smtp-pool]').should('not.exist');
    cy.get('[data-cy=campaign-audience-trigger]').click();
    cy.then(() => cy.get(`[data-cy=campaign-audience-${privateList}]`).should('be.disabled'));
    cy.then(() => cy.get(`[data-cy=campaign-audience-${pools[1]}]`).check());
    cy.get('[data-cy=campaign-audience-trigger]').click();
    cy.get('[data-cy=campaign-smtp-overview]').should('contain', 'organization-0@example.com')
      .and('contain', 'organization-1@example.com').and('not.contain', 'member-personal@example.com');
    cy.get('[data-cy=campaign-pool-smtp-help]').should('contain', 'Member personal SMTP accounts are excluded');
    // Returning to single-organization scope restores an actual pool selection.
    cy.get('[data-cy=campaign-pool-scope]').select('organization');
    cy.then(() => cy.get('[data-cy=campaign-smtp-pool]').should('have.value', String(firstSMTPPool)));
    cy.get('[data-cy=campaign-pool-scope]').select('all_organizations');
    cy.get('input[name=name]').type('Public-pool SMTP regression');
    cy.get('input[name=subject]').type('Public-pool SMTP subject');
    cy.intercept('POST', '/api/campaigns').as('createPublicPool');
    cy.get('[data-cy=btn-continue]').click();
    cy.wait('@createPublicPool').then(({ request, response }) => {
      expect(request.body.smtp_pool_id).to.eq(null);
      expect(request.body.pool_scope).to.eq('all_organizations');
      expect(response.statusCode, JSON.stringify(response.body)).to.eq(200);
      campaign = response.body.data.id;
      cy.visit(`/admin/campaigns/${campaign}`);
    });
    cy.get('[data-cy=pool-send-status-ready]').should('contain', 'Ready to send');
    cy.then(() => cy.request(`/api/campaigns/${campaign}/pool-send-status`)).then(({ body }) => {
      expect(body.data.ready).to.eq(true);
      expect(body.data.organizations.map((org) => org.id).sort()).to.deep.eq([...orgs].sort());
    });
    cy.get('[data-cy=campaign-smtp-source]').select('personal');
    cy.get('[data-cy=pool-send-status-ready]').should('not.exist');
    cy.get('[data-cy=pool-send-status-save]').should('be.visible');
    cy.get('[data-cy=campaign-smtp-overview]').should('not.contain', 'organization-0@example.com');
    cy.intercept('PUT', '/api/campaigns/*').as('savePublicPool');
    cy.get('[data-cy=btn-save]').first().click();
    cy.wait('@savePublicPool').its('response.statusCode').should('eq', 200);
    cy.get('[data-cy=pool-send-status-issues]').should('contain', 'selected sender source');
    cy.get('[data-cy=btn-start]').should('not.exist');
    cy.get('[data-cy=campaign-smtp-source]').select('organization');
    cy.get('[data-cy=pool-send-status-ready]').should('not.exist');
    cy.get('[data-cy=btn-save]').first().click();
    cy.wait('@savePublicPool').then(({ request, response }) => {
      expect(request.body.smtp_pool_id).to.eq(null);
      expect(response.statusCode).to.eq(200);
    });
    cy.get('[data-cy=pool-send-status-ready]').should('contain', 'Ready to send');
    // Editing the selected pools changes the overview immediately, without
    // retaining the other saved pool's organization senders.
    cy.get('[data-cy=campaign-audience-trigger]').click();
    cy.then(() => cy.get(`[data-cy=campaign-audience-${pools[1]}]`).uncheck());
    cy.get('[data-cy=campaign-audience-trigger]').click();
    cy.get('[data-cy=campaign-smtp-overview]').should('contain', 'organization-0@example.com')
      .and('not.contain', 'organization-1@example.com');
    cy.get('[data-cy=pool-send-status-save]').should('be.visible');
    cy.get('[data-cy=btn-save]').first().click();
    cy.wait('@savePublicPool').its('response.statusCode').should('eq', 200);
    cy.get('[data-cy=pool-send-status-ready]').should('contain', 'Ready to send');
    cy.get('[data-cy=campaign-sender-section]').screenshot('public-pool-organization-smtp');
    cy.viewport(390, 844);
    cy.document().then((doc) => expect(doc.documentElement.scrollWidth).to.be.at.most(390));
    cy.get('[data-cy=campaign-sender-section]').screenshot('public-pool-organization-smtp-mobile');
  });
});
