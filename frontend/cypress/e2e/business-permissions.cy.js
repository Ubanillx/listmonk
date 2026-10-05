// All API calls are intercepted. This suite never resets or mutates a database.
describe('Business permission groups', () => {
  const role = { id: 8, name: '部分授权测试', permissions: ['customers:get'] };

  beforeEach(() => {
    cy.readFile('../permissions.json').then((permissions) => {
      cy.readFile('../i18n/zh-CN.json').then((lang) => {
        cy.intercept('/api/**', { data: [] });
        cy.intercept('GET', '/api/config', { data: { lang: 'zh-CN', permissions, site_name: '权限验证', messengers: [] } });
        cy.intercept('GET', '/api/lang/zh-CN', { data: lang });
        cy.intercept('GET', '/api/profile', { data: {
          id: 77, username: 'permission-test', name: '权限测试',
          user_role: { id: 77, permissions: ['roles:get', 'roles:manage', 'workspaces:personal'] },
          customer_list_roles: [],
        } });
        cy.intercept('GET', '/api/workspace', { data: { personal: true, organization_id: 0 } });
        cy.intercept('GET', '/api/roles/users', { data: [role] });
        cy.intercept('GET', '/api/customer-lists*', { data: { results: [], total: 0 } });
        cy.visit('/admin/users/roles/users', {
          onBeforeLoad(win) { win.localStorage.clear(); },
        });
      });
    });
  });

  it('shows clear business names and preserves partial grants on save', () => {
    cy.contains('a', role.name).click();
    cy.contains('查看私域客户与列表').should('be.visible');
    cy.contains('部分已授权').should('be.visible');
    cy.contains('维护公海主数据').should('be.visible');
    cy.contains('配置发信与回信邮箱').scrollIntoView().should('be.visible');
    cy.intercept('PUT', '/api/roles/users/8', (req) => {
      expect(req.body.permissions).to.deep.equal(['customers:get']);
      req.reply({ data: role });
    }).as('savePartial');
    cy.get('[data-cy="permission-privateRead"]').scrollIntoView();
    cy.screenshot('business-permission-groups', { capture: 'viewport' });
    cy.get('[data-cy="btn-save"]').click();
    cy.wait('@savePartial');
  });

  it('changes only the selected business bundle', () => {
    cy.contains('a', role.name).click();
    cy.get('[data-cy="permission-privateMaintain"]').click();
    cy.intercept('PUT', '/api/roles/users/8', (req) => {
      expect(req.body.permissions).to.have.members([
        'customers:get', 'customers:manage', 'customers:import', 'customers:blocklist',
        'customers:membership_manage', 'customer_lists:manage_all',
      ]);
      expect(req.body.permissions).not.to.include('customers:delete');
      expect(req.body.permissions).not.to.include('mailboxes:manage');
      req.reply({ data: { ...role, permissions: req.body.permissions } });
    }).as('saveBundle');
    cy.get('[data-cy="btn-save"]').click();
    cy.wait('@saveBundle');
  });

  it('keeps new high impact permissions unchecked by default', () => {
    cy.get('[data-cy="btn-new"]').click();
    ['poolMaster', 'poolDelivery', 'campaignPublicPool', 'mailboxManage', 'assetShare'].forEach((id) => {
      cy.get(`[data-cy="permission-${id}"] input`).should('not.be.checked');
    });
  });
});

describe('Delegated pool master controls', () => {
  const pool = {
    id: 6, uuid: 'pool-6', name: '委派公海', type: 'pool', optin: 'single', status: 'active',
    visibility: 'global', organization_id: null, owner_user_id: 1, tags: [], customer_count: 0,
  };
  const visitPools = (grants) => {
    cy.readFile('../permissions.json').then((permissions) => {
      cy.readFile('../i18n/zh-CN.json').then((lang) => {
        cy.intercept('/api/**', { data: [] });
        cy.intercept('GET', '/api/config', { data: { lang: 'zh-CN', permissions, messengers: [] } });
        cy.intercept('GET', '/api/lang/zh-CN', { data: lang });
        cy.intercept('GET', '/api/profile', { data: {
          id: 77, username: 'delegate', name: '委派维护人员',
          user_role: { id: 77, permissions: ['workspaces:personal', 'pools:get', ...grants] },
          customer_list_roles: [],
        } });
        cy.intercept('GET', '/api/workspace', { data: { personal: true, organization_id: 0 } });
        cy.intercept('GET', '/api/customer-lists*', { data: { results: [pool], total: 1, page: 1, per_page: 20 } });
        cy.visit('/admin/pool-lists', { onBeforeLoad(win) { win.localStorage.clear(); } });
      });
    });
  };

  it('allows a master maintainer to create and edit a pool without platform identity', () => {
    visitPools(['pools:master_manage']);
    cy.get('[data-cy="btn-edit"]').click();
    cy.get('.modal.is-active select[name="type"]').should('have.value', 'pool');
    cy.get('.modal.is-active [data-cy="btn-save"]').should('be.visible');
    cy.get('.modal.is-active button').contains('取消').click();
    cy.get('[data-cy="btn-new"]').click();
    cy.get('.modal.is-active select[name="type"]').should('have.value', 'pool');
    cy.get('.modal.is-active [data-cy="btn-save"]').should('be.visible');
  });

  it('hides master creation, editing and deletion from allocation-only users', () => {
    visitPools(['pools:manage']);
    cy.contains('委派公海').should('be.visible');
    cy.get('[data-cy="btn-new"]').should('not.exist');
    cy.get('[data-cy="btn-edit"]').should('not.exist');
    cy.get('[data-cy="btn-delete"]').should('not.exist');
  });
});
