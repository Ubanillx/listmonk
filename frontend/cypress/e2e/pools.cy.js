/* eslint-env mocha */
/* global cy, Cypress */

// Requires dev/pools_e2e_seed.sql and the Docker development stack. The spec
// is opt-in so the normal fresh-database suite stays deterministic.
describe('Public pools', function poolSuite() { // eslint-disable-line prefer-arrow-callback
  const password = Cypress.env('POOL_QA_PASSWORD') || 'Test@1234';
  const superPassword = Cypress.env('POOL_QA_SUPER_PASSWORD') || password;
  let poolID = Number(Cypress.env('POOL_QA_POOL_ID')) || 0;
  let guidedOrganizationID = Number(Cypress.env('POOL_QA_GUIDED_ORGANIZATION_ID')) || 0;
  let temporarySegmentListID = 0;

  before(function requirePoolFixture() {
    const enabled = Cypress.env('POOL_E2E');
    if (!(enabled === true || String(enabled).toLowerCase() === 'true')) this.skip();

    if (!poolID) {
      cy.request('/admin/login').then((response) => {
        const nonce = /name="nonce" value="([^"]+)"/.exec(response.body)[1];
        return cy.request({
          method: 'POST',
          url: '/admin/login',
          form: true,
          body: {
            username: Cypress.env('POOL_QA_USER') || 'wsqa_pool_manager',
            password,
            nonce,
            next: '/admin',
          },
        });
      }).then(() => cy.request({
        url: '/api/customer-lists?minimal=true&status=active',
        headers: { 'X-Listmonk-Organization-ID': '1' },
      })).then((response) => {
        const lists = response.body.data.results || [];
        const pool = lists.find((list) => list.name === 'wsqa-pool-primary' && list.type === 'pool');
        expect(pool, 'public-pool fixture').to.exist;
        poolID = Number(pool.id);
      });
    }

    if (guidedOrganizationID) return;
    return loginAs(Cypress.env('POOL_QA_SUPER_USER') || 'root', undefined, superPassword)
      .then(() => cy.request('/api/organizations')).then((response) => {
      const name = Cypress.env('POOL_QA_GUIDED_ORGANIZATION_NAME') || 'wsqa-pool-guided-org';
      const organization = (response.body.data || []).find((item) => item.name === name);
      expect(organization, 'guided secondary-list organization').to.exist;
      guidedOrganizationID = Number(organization.id);
      });
  });

  function loginAs(username, workspaceID, loginPassword = password) {
    cy.clearCookies();
    cy.clearLocalStorage();
    cy.visit('/admin/login?next=/admin');
    cy.get('input[name=username]').clear().type(username);
    cy.get('input[name=password]').clear().type(loginPassword);
    cy.get('button').click();
    const targetWorkspaceID = workspaceID || 0;
    let selectedOnServer = false;
    cy.get('body').then(($body) => {
      const selector = `.space-option[data-org="${targetWorkspaceID}"]`;
      if (!$body.find(selector).length) return null;
      selectedOnServer = true;
      return cy.get(selector).click();
    }).then(() => {
      if (!workspaceID || selectedOnServer) return null;
      // Vite serves the SPA for /admin/select-workspace. Let that first boot
      // finish before replacing its persisted selection, otherwise it can
      // race this test and restore the personal workspace.
      return cy.get('.workspace-label').should('be.visible').then(() => cy.window()).then((win) => {
        win.localStorage.setItem('listmonk.workspace.organizationId', String(workspaceID));
      }).then(() => cy.setCookie('listmonk_workspace_organization_id', String(workspaceID))).then(() => cy.reload());
    });

    // The workspace selector may preserve /admin/404 while the SPA bootstraps
    // in a dev proxy; navigate through the menu rather than asserting the
    // intermediate URL.
    cy.get('[data-cy=customerLists]').filter(':visible').first().click();
    cy.get('[data-cy=all-customer_lists]').filter(':visible').first().click();
    return cy.url().should('include', '/admin/customer-lists');
  }

  afterEach(() => {
    if (!temporarySegmentListID) return;
    cy.request({
      method: 'DELETE',
      url: `/api/customer-lists/${temporarySegmentListID}`,
      headers: { 'X-Listmonk-Organization-ID': String(guidedOrganizationID) },
    }).its('status').should('eq', 200);
    temporarySegmentListID = 0;
  });

  it('lets an organization admin split the pool inside the current organization', () => {
    loginAs(Cypress.env('POOL_QA_USER') || 'wsqa_pool_manager');
    cy.contains('a', 'wsqa-pool-primary').closest('tr').find('[data-cy=btn-manage-pool]').click();
    // The target organization is fixed to the caller's own workspace for an
    // organization admin: the cross-organization selector is replaced by a
    // read-only label showing that organization.
    cy.get('[data-cy=pool-target-organization]').should('not.exist');
    cy.get('[data-cy=pool-current-organization]').should('be.visible');
    cy.get('[data-cy=pool-secondary-list-panel]').scrollIntoView().should('be.visible');
    // Resolving an arbitrary organization's target stays highest-admin only.
    cy.request({
      url: `/api/pools/${poolID}/management-target?organization_id=1`,
      failOnStatusCode: false,
    }).its('status').should('eq', 403);
  });

  it('guides the highest administrator from organization selection to a ready-to-use secondary list', () => {
    loginAs(Cypress.env('POOL_QA_SUPER_USER') || 'root', undefined, superPassword);
    cy.contains('a', 'wsqa-pool-primary').closest('tr').find('[data-cy=btn-manage-pool]').click();
    cy.get('[data-cy=pool-secondary-list-empty]').should('be.visible');
    cy.get('[data-cy=pool-contact-allocation]').should('not.exist');
    cy.get('[data-cy=pool-target-organization]').select('1');
    cy.get('[data-cy=pool-secondary-list-panel]').should('be.visible');
    cy.get('.pool-manager').should('be.visible');
    cy.get('[data-cy=pool-segment-summary]').contains('wsqa-pool-segment');
    cy.get('[data-cy=pool-contact-allocation]').should('not.exist');
  });

  it('shows a single split workflow without merge controls', () => {
    loginAs(Cypress.env('POOL_QA_SUPER_USER') || 'root', undefined, superPassword);
    cy.contains('a', 'wsqa-pool-primary').closest('tr').find('[data-cy=btn-manage-pool]').click();
    cy.get('[data-cy=pool-target-organization]').select(String(guidedOrganizationID));
    cy.window().its('localStorage').invoke('getItem', 'listmonk.workspace.organizationId').should('be.null');
    cy.get('[data-cy=pool-segment-create]').scrollIntoView().should('be.visible');
    cy.get('[data-cy=create-pool-segment]').scrollIntoView().should('be.visible');
    cy.get('[data-cy=pool-segment-merge]').should('not.exist');
    cy.get('.pool-manager').should('not.contain', '合并已有二级列表');
  });

  it('creates and binds a secondary list from the guided workflow', () => {
    const segmentName = `wsqa-ui-segment-${Date.now()}`;
    const superUsername = Cypress.env('POOL_QA_SUPER_USER') || 'root';
    let superMembershipCount = 0;
    loginAs(Cypress.env('POOL_QA_SUPER_USER') || 'root', undefined, superPassword);
    cy.request(`/api/organizations/${guidedOrganizationID}/members`).then((response) => {
      superMembershipCount = (response.body.data || []).filter((member) => member.username === superUsername).length;
    });
    cy.contains('a', 'wsqa-pool-primary').closest('tr').find('[data-cy=btn-manage-pool]').click();
    cy.get('[data-cy=pool-target-organization]').select(String(guidedOrganizationID));
    cy.intercept('POST', '/api/pool-segments').as('createSegment');
    cy.get('[data-cy=pool-segment-name]').scrollIntoView().type(segmentName);
    cy.get('[data-cy=create-pool-segment]').click();
    cy.wait('@createSegment').then(({ request, response }) => {
      expect(response.statusCode).to.eq(200);
      expect(request.body.organization_id).to.eq(guidedOrganizationID);
      temporarySegmentListID = Number(response.body.data.list_id);
    });
    cy.get('.pool-manager').contains(segmentName).should('be.visible');
    cy.request(`/api/organizations/${guidedOrganizationID}/members`).then((response) => {
      const currentCount = (response.body.data || []).filter((member) => member.username === superUsername).length;
      expect(currentCount).to.eq(superMembershipCount);
    });
  });

  it('hides a pool from an organization without delivery access', () => {
    loginAs('wsqa_pool_unassigned');
    cy.contains('wsqa-pool-primary').should('not.exist');
    cy.get('[data-cy=btn-manage-pool]').should('not.exist');
    cy.request({ url: `/api/customer-lists/${poolID}`, failOnStatusCode: false })
      .its('status').should('eq', 403);
  });
});
