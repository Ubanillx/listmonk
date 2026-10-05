const smtp = (name, email) => ({
  name, enabled: true, host: 'host.docker.internal', port: 6125,
  auth_protocol: 'none', username: '', password: '', from_email: email, daily_limit: 10,
  tls_type: 'none', tls_skip_verify: false,
});

describe('Organization marketing SMTP', () => {
  let organizationID;
  let otherOrganizationID;
  let smtpPoolID;
  let backupSMTPPoolID;
  let servers;
  let customerListID;

  it('keeps pools independent, saves sender choice and displays the sender overview', () => {
    cy.resetDB();
    cy.loginAndVisit('/admin/organizations');
    cy.request('/api/profile').then(({ body }) => {
      cy.request('POST', '/api/organizations', { name: 'Marketing SMTP team', manager_user_id: body.data.id })
        .then((response) => { organizationID = response.body.data.id; });
      cy.request('POST', '/api/organizations', { name: 'Other SMTP team', manager_user_id: body.data.id })
        .then((response) => { otherOrganizationID = response.body.data.id; });
    });
    cy.then(() => cy.request({
      method: 'POST', url: '/api/organizations/smtp-pools', headers: { 'X-Listmonk-Organization-ID': organizationID },
      body: { name: 'Marketing rotation' },
    })).then(({ body }) => {
      expect(body.data.id, JSON.stringify(body)).to.be.a('number');
      smtpPoolID = body.data.id;
    });
    cy.then(() => cy.request({
      method: 'POST', url: '/api/organizations/smtp-pools', headers: { 'X-Listmonk-Organization-ID': organizationID },
      body: { name: 'Backup rotation' },
    })).then(({ body }) => { backupSMTPPoolID = body.data.id; });
    cy.then(() => cy.request({
      url: '/api/organizations/smtp-pools', headers: { 'X-Listmonk-Organization-ID': organizationID },
    })).then(({ body }) => {
      smtpPoolID = body.data.find((pool) => pool.name === 'Marketing rotation').id;
      expect(body.data.map((pool) => pool.id)).to.include.members([smtpPoolID, backupSMTPPoolID]);
      expect(smtpPoolID).to.be.a('number');
    });
    cy.then(() => cy.request({
      method: 'PUT', url: `/api/organizations/smtp?pool_id=${smtpPoolID}`, headers: { 'X-Listmonk-Organization-ID': organizationID },
      body: { smtp: [smtp('org-one', 'org-one@example.com'), smtp('org-two', 'org-two@example.com')] },
    })).then(({ body }) => {
      servers = body.data.smtp;
      expect(servers).to.have.length(2);
      servers.forEach((server) => {
        expect(server.user_id).to.equal(null);
        expect(server.organization_id).to.equal(organizationID);
      });
    });
    cy.request('PUT', '/api/profile/smtp', { smtp: [smtp('personal', 'personal@example.com')] });
    cy.then(() => cy.request({
      method: 'POST', url: '/api/customer-lists', headers: { 'X-Listmonk-Organization-ID': organizationID },
      body: { name: 'SMTP recipient list', type: 'private', optin: 'single', visibility: 'private', tags: [] },
    })).then(({ body }) => { customerListID = body.data.id; });
    cy.then(() => cy.request({
      method: 'PUT', url: '/api/organizations/smtp', headers: { 'X-Listmonk-Organization-ID': otherOrganizationID },
      body: { smtp: [servers[0]] }, failOnStatusCode: false,
    })).its('status').should('eq', 404);
    cy.then(() => cy.request({
      method: 'DELETE', url: `/api/profile/smtp/${servers[0].id}`, failOnStatusCode: false,
    })).its('status').should('eq', 404);
    cy.visit('/admin/campaigns/new');
    cy.get('[data-cy=campaign-visibility]').should('have.value', 'private');
    cy.get('[data-cy=campaign-smtp-source]').should('have.value', 'personal');
    cy.get('[data-cy=campaign-smtp-rate-limit]').should('have.value', '20');
    cy.get('[data-cy=campaign-auto-track-links] input').should('be.checked');
    cy.window().then((win) => win.localStorage.setItem('listmonk.workspace.organizationId', String(organizationID)));
    cy.then(() => cy.setCookie('listmonk_workspace_organization_id', String(organizationID)));
    cy.visit('/admin/organizations/manage');
    cy.then(() => cy.get('.section-mini select').select(String(organizationID)));
    cy.get('.b-tabs nav a').contains('SMTP').click();
    cy.get('[data-cy=organization-smtp] .smtp-pool-option').should('have.length', 2);
    cy.get('[data-cy=organization-smtp] .smtp-pool-option').eq(1).click();
    cy.get('[data-cy=organization-smtp] .smtp-empty-state').should('contain', '尚未配置组织营销 SMTP');
    cy.get('[data-cy=organization-smtp] .smtp-pool-option').eq(0).click();
    cy.get('[data-cy=organization-smtp] .smtp-card').should('have.length', 2);
    cy.then(() => cy.visit(`/admin/campaigns/new?customer_list_id=${customerListID}`));
    cy.get('[data-cy=campaign-visibility]').should('have.value', 'organization');
    cy.get('[data-cy=campaign-smtp-source]').should('have.value', 'organization');
    cy.get('[data-cy=campaign-auto-track-links] input').should('be.checked');
    cy.get('[data-cy=campaign-smtp-rate-limit]').should('have.value', '100');
    cy.then(() => cy.get('[data-cy=campaign-smtp-pool]').select(String(backupSMTPPoolID)));
    cy.get('[data-cy=campaign-smtp-unavailable]').should('be.visible').and('contain', '所选组织发件池暂无启用的 SMTP')
      .and('not.contain', '个人');
    cy.get('[data-cy=campaign-smtp-overview]').should('contain', '所选组织发件池暂无启用的 SMTP');
    cy.get('[data-cy=campaign-smtp-source]').select('personal');
    cy.get('[data-cy=campaign-smtp-unavailable]').should('not.exist');
    cy.get('[data-cy=campaign-smtp-overview]').should('contain', 'personal@example.com').and('not.contain', 'org-one@example.com');
    cy.get('[data-cy=campaign-smtp-rate-limit]').should('have.value', '20');
    cy.get('[data-cy=campaign-smtp-source]').select('organization');
    cy.get('[data-cy=campaign-smtp-rate-limit]').should('have.value', '100').clear().type('30');
    cy.get('[data-cy=campaign-smtp-pool]').should('be.visible').find('option').should('have.length', 2)
      .first().invoke('val').then((value) => {
        expect(Number(value)).to.equal(smtpPoolID);
      });
    cy.get('[data-cy=campaign-smtp-overview]').should('contain', 'org-one@example.com').and('contain', 'org-two@example.com')
      .and('not.contain', 'personal@example.com');
    cy.get('input[data-cy=campaign-daily-resume-time]').should('have.value', '09:00').click();
    cy.get('.timepicker .dropdown-menu select:visible').should('have.length', 2).eq(0).select('0');
    cy.get('.timepicker .dropdown-menu select:visible').eq(1).select('5');
    cy.get('input[data-cy=campaign-daily-resume-time]').should('have.value', '00:05');
    cy.get('input[name=name]').type('Organization SMTP campaign');
    cy.get('input[name=subject]').type('Organization SMTP subject');
    cy.intercept('POST', '**/api/campaigns').as('createCampaign');
    cy.get('[data-cy=btn-continue]').click();
    cy.wait('@createCampaign').then(({ response }) => {
      expect(response.statusCode).to.equal(200);
      expect(response.body.data.smtp_source).to.equal('organization');
      expect(response.body.data.smtp_pool_id).to.equal(smtpPoolID);
      expect(response.body.data.smtp_rate_limit).to.equal(30);
      expect(response.body.data.visibility).to.equal('organization');
      expect(response.body.data.auto_track_links).to.equal(true);
      expect(response.body.data.daily_resume_time).to.equal('00:05');
      cy.visit(`/admin/campaigns/${response.body.data.id}`);
    });
    cy.get('[data-cy=campaign-smtp-source]').should('have.value', 'organization');
    cy.get('[data-cy=campaign-smtp-rate-limit]').should('have.value', '30');
    cy.get('input[data-cy=campaign-daily-resume-time]').should('have.value', '00:05');
    cy.then(() => cy.get('[data-cy=campaign-smtp-pool]').should('have.value', String(smtpPoolID)));
    cy.get('[data-cy=campaign-smtp-overview]').should('contain', 'org-one@example.com');
    cy.location('pathname').then((pathname) => {
      const campaignID = pathname.split('/').pop();
      cy.request('PUT', `/api/campaigns/${campaignID}`, {
        smtp_rate_limit: 45, customer_list_ids: [customerListID], visibility: 'private', smtp_source: 'personal', auto_track_links: false,
      })
        .its('body.data.smtp_rate_limit').should('equal', 45);
      cy.request('PUT', `/api/campaigns/${campaignID}`, { subject: 'Updated organization SMTP subject', customer_list_ids: [customerListID] })
        .its('body.data.smtp_rate_limit').should('equal', 45);
      cy.request({ method: 'PUT', url: `/api/campaigns/${campaignID}`, body: { smtp_rate_limit: -1 }, failOnStatusCode: false })
        .its('status').should('equal', 400);
      cy.reload();
      cy.get('[data-cy=campaign-smtp-rate-limit]').should('have.value', '45');
      cy.get('[data-cy=campaign-visibility]').should('have.value', 'private');
      cy.get('[data-cy=campaign-smtp-source]').should('have.value', 'personal');
      cy.get('[data-cy=campaign-auto-track-links] input').should('not.be.checked');
    });
    cy.then(() => cy.request({
      url: '/api/campaigns/smtp-overview?source=organization', headers: { 'X-Listmonk-Organization-ID': organizationID },
    })).then(({ body }) => {
      expect(body.data).to.have.length(2);
      body.data.forEach((row) => {
        expect(row).not.to.have.property('password');
        expect(row).not.to.have.property('username');
        expect(row).not.to.have.property('host');
      });
    });
  });

  it('allows members to see senders while protecting credentials and other organizations', () => {
    let memberID;
    cy.request('POST', '/api/roles/users', {
      name: 'SMTP campaign member', permissions: ['campaigns:get', 'campaigns:manage', 'workspaces:personal'],
    }).then(({ body }) => cy.request('POST', '/api/users', {
      username: 'smtp-member', name: 'SMTP member', email: 'smtp-member@example.com',
      type: 'user', status: 'enabled', password_login: true, password: 'smtp-member-test', user_role_id: body.data.id,
    })).then(({ body }) => {
      memberID = body.data.id;
      cy.request({
        method: 'POST', url: '/api/organizations/members', headers: { 'X-Listmonk-Organization-ID': organizationID },
        body: { user_id: memberID, role: 'member' },
      });
    });
    cy.clearCookies();
    cy.request('/admin/login').then(({ body }) => cy.request({
      method: 'POST', url: '/admin/login', form: true,
      body: { username: 'smtp-member', password: 'smtp-member-test', nonce: /name="nonce" value="([^"]+)"/.exec(body)[1], next: '/admin' },
    }));
    cy.then(() => cy.request({
      url: '/api/campaigns/smtp-overview?source=organization', headers: { 'X-Listmonk-Organization-ID': organizationID },
    })).its('body.data').should('have.length', 2);
    ['GET', 'PUT', 'POST'].forEach((method) => {
      cy.then(() => cy.request({
        method, url: method === 'POST' ? '/api/organizations/smtp/test' : '/api/organizations/smtp',
        headers: { 'X-Listmonk-Organization-ID': organizationID }, body: method === 'GET' ? undefined : { smtp: [] },
        failOnStatusCode: false,
      })).its('status').should('eq', 403);
    });
    cy.then(() => cy.request({
      url: '/api/campaigns/smtp-overview?source=organization', headers: { 'X-Listmonk-Organization-ID': otherOrganizationID },
      failOnStatusCode: false,
    })).its('status').should('be.oneOf', [403, 404]);
  });

  it('rotates actual deliveries through organization senders and honors the selected personal source', () => {
    const subject = `org-smtp-runtime-${Date.now()}`;
    const recipient = 'smtp-runtime-recipient@example.com';
    let campaignID;
    const workspaceHeaders = () => ({ 'X-Listmonk-Organization-ID': organizationID });
    cy.clearCookies();
    cy.loginAndVisit('/admin/settings');
    cy.request('PUT', '/api/settings/smtp_delivery', {
      max_conns: 2, max_msg_retries: 1, idle_timeout: '5s', wait_timeout: '5s',
      email_headers: [], send_delay_min: 0, send_delay_max: 0,
    });
    // Settings reload closes and recreates the application after 500 ms.
    cy.wait(2000);
    cy.then(() => cy.request({
      method: 'POST', url: '/api/customers', headers: workspaceHeaders(),
      body: { email: recipient, name: 'SMTP runtime recipient', customer_code: `SMTP-${Date.now()}`, status: 'enabled', customer_list_ids: [] },
    }));
    cy.then(() => cy.request({
      method: 'PUT', url: '/api/organizations/smtp', headers: workspaceHeaders(),
      body: { smtp: servers.map((server, index) => ({ ...server, daily_limit: index === 0 ? 1 : 10 })) },
    })).then(({ body }) => { servers = body.data.smtp; });
    cy.then(() => cy.request({
      method: 'POST', url: '/api/campaigns', headers: workspaceHeaders(),
      body: { name: 'SMTP runtime campaign', subject, messenger: 'email', smtp_source: 'organization',
        type: 'regular', content_type: 'plain', body: 'SMTP runtime test', daily_send_limit: 300,
        daily_resume_time: '09:00', customer_list_ids: [customerListID], headers: [], tags: [], attribs: {} },
    })).then(({ body }) => {
      campaignID = body.data.id;
      expect(body.data.smtp_rate_limit).to.equal(100);
    });
    const send = (source) => cy.then(() => cy.request({
      method: 'POST', url: `/api/campaigns/${campaignID}/test`, headers: workspaceHeaders(),
      body: { name: 'SMTP runtime campaign', subject, messenger: 'email', smtp_source: source,
        type: 'regular', content_type: 'plain', body: 'SMTP runtime test', daily_send_limit: 300,
        daily_resume_time: '09:00', customer_list_ids: [customerListID], headers: [], tags: [], customers: [recipient], media: [] },
    }));
    send('organization');
    send('organization');
    send('organization');
    send('personal');
    let messages = [];
    cy.waitUntil(() => cy.request('http://127.0.0.1:8265/api/v2/messages?limit=100').then(({ body }) => {
      messages = body.items.filter((message) => (message.Content.Headers.Subject || []).join('').includes(subject));
      return messages.length === 4;
    }), { timeout: 20000, interval: 500 });
    cy.then(() => {
      const from = messages.map((message) => message.Content.Headers.From.join(''));
      expect(from.filter((value) => value.includes('org-one@example.com'))).to.have.length(1);
      expect(from.filter((value) => value.includes('org-two@example.com'))).to.have.length(2);
      expect(from.filter((value) => value.includes('personal@example.com'))).to.have.length(1);
    });
    cy.then(() => cy.request({ url: '/api/organizations/smtp', headers: workspaceHeaders() })).then(({ body }) => {
      expect(body.data.smtp.map((server) => server.sent_today)).to.deep.equal([1, 2]);
    });
    cy.then(() => cy.request({
      method: 'PUT', url: '/api/organizations/smtp', headers: workspaceHeaders(),
      body: { smtp: servers.map((server) => ({ ...server, enabled: false })) },
    }));
    cy.then(() => cy.request({
      method: 'POST', url: `/api/campaigns/${campaignID}/test`, headers: workspaceHeaders(),
      body: { name: 'SMTP runtime campaign', subject, messenger: 'email', smtp_source: 'organization',
        type: 'regular', content_type: 'plain', body: 'SMTP runtime test', daily_send_limit: 300,
        daily_resume_time: '09:00', customer_list_ids: [customerListID], headers: [], tags: [], customers: [recipient] },
      failOnStatusCode: false,
    })).its('status').should('eq', 409);
    cy.request('/api/profile/smtp').its('body.data.smtp').should('have.length', 1);
  });
});
