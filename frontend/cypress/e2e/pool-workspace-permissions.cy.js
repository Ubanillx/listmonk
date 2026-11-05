/* eslint-env mocha */
/* global cy, expect */

import * as XLSX from 'xlsx';

describe('Public pool workspace permissions', () => {
  it('shows only current department allocations and scopes metadata, contacts and exports', () => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.loginAndVisit('/admin/pool-lists');
    cy.get('[data-cy=pool-list-filters]').should('be.visible');
    const orgs = {};
    const pools = {};
    const allocations = {};
    cy.request('/api/profile').then(({ body }) => {
      ['North', 'South'].forEach((name) => {
        cy.request('POST', '/api/organizations', { name, manager_user_id: body.data.id })
          .then((res) => { orgs[name] = res.body.data.id; });
      });
    });
    ['Shared source', 'South source', 'Unallocated source'].forEach((name) => {
      cy.request('POST', '/api/customer-lists', { name, type: 'pool', optin: 'single' })
        .then(({ body }) => { pools[name] = body.data.id; });
    });
    cy.then(() => {
      ['North', 'South'].forEach((name) => {
        cy.request('POST', '/api/org-pool-allocations', {
          pool_id: pools['Shared source'], organization_id: orgs[name], name: `${name} shared allocation`,
        }).then(({ body }) => { allocations[name] = body.data.list_id; });
        cy.request('POST', `/api/customer-lists/${pools['Shared source']}/pool-contacts`, {
          customer_code: `${name}-only`, name: `${name} customer`, email: `${name.toLowerCase()}@example.com`, allocation_department: name,
        });
      });
      cy.request('POST', '/api/org-pool-allocations', {
        pool_id: pools['South source'], organization_id: orgs.South, name: 'South exclusive allocation',
      });
    });
    cy.visit('/admin/pool-lists');
    cy.get('[data-cy=pool-list-organization-filter]').should('be.visible');
    cy.contains('tbody tr', 'Unallocated source').should('exist');

    // Use the real switcher so stale list state cannot survive a department change.
    cy.viewport(1200, 680);
    cy.get('[data-cy=workspace-switcher] .workspace-label').click();
    cy.then(() => cy.get(`[data-cy=workspace-organization-${orgs.North}]`).click());
    cy.get('[data-cy=pool-list-organization-filter]').should('not.exist');
    cy.get('tbody tr').should('have.length', 2);
    cy.contains('tbody tr', 'Shared source').find('.customer-count strong').should('have.text', '1');
    cy.contains('tbody tr', 'Shared source').find('.pool-tree-label').then(($label) => {
      expect($label[0].getBoundingClientRect().width).to.be.greaterThan(120);
    });
    cy.contains('tbody tr', 'North shared allocation').should('exist');
    cy.get('tbody').should('not.contain', 'South').and('not.contain', 'Unallocated source');
    cy.screenshot('pool-workspace-north-desktop', { capture: 'viewport' });

    cy.then(() => {
      const headers = { 'X-Listmonk-Organization-ID': orgs.North };
      [pools['South source'], pools['Unallocated source'], allocations.South].forEach((id) => {
        ['', '/pool-contacts', '/pool-contacts/export', '/org-pool-allocations'].forEach((suffix) => {
          cy.request({ url: `/api/customer-lists/${id}${suffix}`, headers, failOnStatusCode: false }).its('status').should('eq', 403);
        });
      });
      cy.request({ url: '/api/customer-lists?minimal=true&per_page=all', headers }).then(({ body }) => {
        expect(body.data.results.map((row) => row.id)).to.have.members([pools['Shared source'], allocations.North]);
      });
      cy.request({ url: '/api/pools/contacts', headers }).then(({ body }) => {
        expect(body.data.total).to.eq(1);
        expect(body.data.results[0].customer_code).to.eq('North-only');
      });
      cy.request({ url: `/api/customer-lists/${pools['Shared source']}/org-pool-allocations`, headers }).then(({ body }) => {
        expect(body.data.map((row) => row.organization_id)).to.deep.eq([orgs.North]);
      });
      ['/api/pools/contacts/export', `/api/customer-lists/${pools['Shared source']}/pool-contacts/export`].forEach((url) => {
        cy.request({ url, headers, encoding: 'binary' }).then(({ body }) => {
          const book = XLSX.read(body, { type: 'binary' });
          const rows = XLSX.utils.sheet_to_json(book.Sheets[book.SheetNames[0]], { header: 1 });
          expect(rows).to.have.length(2);
          expect(rows.flat()).to.include('North-only').and.not.include('South-only');
        });
      });
    });
    cy.viewport(390, 844);
    cy.get('[data-cy=pool-list-type-filter]').should('be.visible');
    cy.contains('tbody tr', 'North shared allocation').should('be.visible');
    cy.document().then((doc) => { expect(doc.documentElement.scrollWidth).to.be.at.most(390); });
    cy.screenshot('pool-workspace-north-mobile', { capture: 'viewport' });
    cy.viewport(1200, 680);
    cy.get('[data-cy=workspace-switcher] .workspace-label').click();
    cy.then(() => cy.get(`[data-cy=workspace-organization-${orgs.South}]`).click());
    cy.get('tbody tr').should('have.length', 4);
    cy.contains('tbody tr', 'South shared allocation').should('exist');
    cy.contains('tbody tr', 'South exclusive allocation').should('exist');
    cy.get('tbody').should('not.contain', 'North shared allocation').and('not.contain', 'Unallocated source');
    cy.get('[data-cy=workspace-switcher] .workspace-label').click();
    cy.contains('.navbar-dropdown a', 'Personal space').click();
    cy.get('[data-cy=pool-list-organization-filter]').should('be.visible');
    cy.contains('tbody tr', 'North shared allocation').should('exist');
    cy.contains('tbody tr', 'Unallocated source').should('exist');
  });
});
