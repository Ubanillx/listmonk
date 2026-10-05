/* eslint-env mocha */
/* global cy, expect */

describe('Organization directory recovery', () => {
  let joinedID;
  let otherID;
  let poolID;
  let managerID;
  const directoryURL = '/api/organizations?include_archived=false';

  before(() => {
    cy.resetDB();
    cy.loginAndVisit('/admin/campaigns/new');
    cy.request('POST', '/api/roles/users', {
      name: 'Directory manager', permissions: ['campaigns:get', 'campaigns:manage'],
    }).then(({ body }) => cy.request('POST', '/api/users', {
      username: 'directory-manager', name: 'Directory manager', email: 'directory-manager@example.com',
      type: 'user', status: 'enabled', password_login: true, password: 'directory-manager-test', user_role_id: body.data.id,
    })).then(({ body }) => { managerID = body.data.id; });
    cy.request('/api/profile').then(({ body }) => cy.request('POST', '/api/organizations', {
      name: 'Joined directory team', manager_user_id: body.data.id,
    })).then(({ body }) => { joinedID = body.data.id; });
    cy.then(() => cy.request('POST', '/api/organizations', {
      name: 'Other directory team', manager_user_id: managerID,
    })).then(({ body }) => { otherID = body.data.id; });
    cy.request('POST', '/api/customer-lists', { name: 'Directory pool', type: 'pool', optin: 'single' })
      .then(({ body }) => { poolID = body.data.id; });
  });

  beforeEach(() => {
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.window().then((win) => win.localStorage.setItem('listmonk.workspace.organizationId', String(otherID)));
    cy.setCookie('listmonk_workspace_organization_id', String(otherID));
  });

  const assertAdminDirectory = () => {
    cy.then(() => cy.get(`[data-cy=workspace-organization-${joinedID}]`).should('contain', 'Joined directory team'));
    cy.then(() => cy.get(`[data-cy=workspace-organization-${otherID}]`).should('contain', 'Other directory team'));
  };

  const assertSelectedOrganization = () => {
    cy.get('[data-cy=workspace-switcher] .workspace-label').should('contain', 'Other directory team');
    cy.window().then((win) => expect(win.localStorage.getItem('listmonk.workspace.organizationId')).to.eq(String(otherID)));
  };

  it('keeps all accessible organizations after membership and management pages refresh', () => {
    cy.request('/api/organizations/me').then(({ body }) => {
      expect(body.data.map((row) => row.id)).to.include(joinedID).and.not.include(otherID);
    });
    cy.visit('/admin/organizations/manage');
    cy.get('.section-mini select option').should('have.length', 2);
    assertAdminDirectory();
    cy.get('[data-cy=btn-refresh]').click();
    assertAdminDirectory();
    cy.visit('/admin/organizations/mine');
    cy.get('.org-table').should('contain', 'Joined directory team').and('not.contain', 'Other directory team');
    assertAdminDirectory();
    cy.get('[data-cy=workspace-switcher]').click();
    cy.then(() => cy.get(`[data-cy=workspace-organization-${joinedID}]`).click());
    cy.get('[data-cy=workspace-switcher] .workspace-label').should('contain', 'Joined directory team');
    cy.get('[data-cy=workspace-switcher]').click();
    cy.then(() => cy.get(`[data-cy=workspace-organization-${otherID}]`).click());
    assertSelectedOrganization();
    cy.reload();
    assertSelectedOrganization();
    assertAdminDirectory();

    // SPA navigation must pass the complete directory to dependent components.
    cy.clickMenu('customers', 'import');
    cy.get('[data-cy=import-tab-pool]').click();
    cy.get('.customer_list-selector input').type('Directory pool');
    cy.contains('.customer_list-selector .autocomplete a', 'Directory pool').click();
    cy.get('[data-cy=pool-organization-option]').should('have.length', 2);
    cy.contains('[data-cy=pool-organization-option]', 'Other directory team').click();
    cy.get('[data-cy=pool-current-organization]').should('have.text', 'Other directory team');
    cy.then(() => cy.request(`/api/customer-lists/${poolID}`)).its('status').should('eq', 200);
  });

  it('retries a failed startup directory without mounting forms from partial state', () => {
    cy.intercept({ method: 'GET', url: directoryURL, times: 1 },
      { statusCode: 503, body: { message: 'Temporary directory outage' } });
    cy.visit('/admin/campaigns/new');
    cy.get('[data-cy=workspace-initialization-error]').should('be.visible');
    cy.get('[data-cy=campaign-smtp-source]').should('not.exist');
    cy.window().then((win) => expect(win.localStorage.getItem('listmonk.workspace.organizationId')).to.eq(String(otherID)));
    cy.get('[data-cy=workspace-initialization-retry]').click();
    cy.get('[data-cy=campaign-smtp-source]').should('have.value', 'organization');
    cy.get('[data-cy=campaign-visibility]').should('have.value', 'organization');
    assertSelectedOrganization();
    assertAdminDirectory();
  });

  it('retains the selected workspace when its validation temporarily fails', () => {
    cy.intercept({ method: 'GET', url: '/api/workspace', times: 1 },
      { statusCode: 503, body: { message: 'Temporary workspace outage' } });
    cy.visit('/admin/campaigns/new');
    cy.get('[data-cy=workspace-initialization-error]').should('be.visible');
    cy.get('[data-cy=campaign-smtp-source]').should('not.exist');
    cy.window().then((win) => expect(win.localStorage.getItem('listmonk.workspace.organizationId')).to.eq(String(otherID)));
    cy.get('[data-cy=workspace-initialization-retry]').click();
    assertSelectedOrganization();
    cy.get('[data-cy=campaign-smtp-rate-limit]').should('have.value', '100');
  });

  it('shares concurrent refreshes and retains both lists after a partial failure', () => {
    cy.intercept('GET', directoryURL).as('initialDirectory');
    cy.visit('/admin/organizations/mine');
    cy.wait('@initialDirectory');
    cy.wait('@initialDirectory');
    let requests = 0;
    cy.intercept('GET', directoryURL, (req) => {
      requests += 1;
      req.continue((res) => res.setDelay(700));
    }).as('sharedDirectory');
    cy.get('[data-cy=btn-refresh]').click().click();
    cy.wait('@sharedDirectory');
    cy.then(() => expect(requests).to.eq(1));
    assertAdminDirectory();

    cy.intercept({ method: 'GET', url: '/api/organizations/me', times: 1 }, { body: { data: [] } });
    cy.intercept({ method: 'GET', url: directoryURL, times: 1 },
      { statusCode: 503, body: { message: 'Temporary partial directory outage' } }).as('failedDirectory');
    cy.get('[data-cy=btn-refresh]').click();
    cy.wait('@failedDirectory');
    cy.get('.org-table').should('contain', 'Joined directory team');
    assertSelectedOrganization();
    assertAdminDirectory();
    cy.get('[data-cy=workspace-switcher]').click();
    cy.get('[data-cy=workspace-directory-retry]').click();
    cy.get('[data-cy=workspace-directory-retry]').should('not.exist');
    assertSelectedOrganization();
    assertAdminDirectory();
  });

  it('limits ordinary managers to their memberships on direct load and reload', () => {
    cy.clearCookies();
    cy.request('/admin/login').then(({ body }) => cy.request({
      method: 'POST', url: '/admin/login', form: true,
      body: { username: 'directory-manager', password: 'directory-manager-test',
        nonce: /name="nonce" value="([^"]+)"/.exec(body)[1], next: '/admin' },
    }));
    cy.visit('/admin/organizations/manage');
    cy.location('pathname').should('eq', '/admin/organizations/manage');
    cy.get('.section-mini select option').should('have.length', 1).and('contain', 'Other directory team');
    cy.then(() => cy.get(`[data-cy=workspace-organization-${joinedID}]`).should('not.exist'));
    cy.then(() => cy.get(`[data-cy=workspace-organization-${otherID}]`).should('exist'));
    cy.reload();
    cy.get('.section-mini select option').should('have.length', 1);
    assertSelectedOrganization();
  });
});
