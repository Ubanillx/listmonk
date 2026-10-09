describe('Campaign audience labels', () => {
  it('shows private and pool audiences with distinct links and deduplicates pool allocations', () => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'zh-CN'; }));
    const campaign = (id, name, customerLists, customerPools) => ({
      id,
      name,
      subject: name,
      status: 'draft',
      type: 'regular',
      tags: [],
      send_at: null,
      created_at: '2026-10-09T00:00:00Z',
      owner_username: 'admin',
      visibility: 'private',
      customer_lists: customerLists,
      customer_pools: customerPools,
      views: 0,
      clicks: 0,
      sent: 0,
      to_send: 0,
      bounces: 0,
    });
    cy.intercept({ method: 'GET', pathname: '/api/campaigns' }, {
      data: {
        results: [
          campaign(1, '私有活动', [{ id: 11, name: '私有客户' }], []),
          campaign(2, '公海活动', [], [
            { pool_id: 11, allocation_id: 21, name: '公海客户' },
            { pool_id: 11, allocation_id: 22, name: '公海客户' },
            { pool_id: 12, name: '' },
          ]),
          campaign(3, '混合活动', [{ id: 11, name: '私有客户' }], [{ pool_id: 11, name: '公海客户' }]),
          campaign(4, '历史活动', [{ id: null, name: '已删除的列表' }], null),
          campaign(5, '空受众活动', null, null),
        ],
        total: 5,
        per_page: 20,
      },
    }).as('campaigns');
    cy.loginAndVisit('/admin/campaigns');
    cy.wait('@campaigns');
    cy.get('th').should('contain', '客户列表').and('not.contain', '私域客户列表');
    cy.contains('tbody tr', '私有活动').within(() => {
      cy.get('.customer_lists .tag').should('contain', '私有');
      cy.get('.customer_lists a').should('have.attr', 'href', '/admin/customers/customer-lists/11');
    });
    cy.contains('tbody tr', '公海活动').within(() => {
      cy.get('.customer_lists li').should('have.length', 2);
      cy.get('.customer_lists .tag').each(($tag) => expect($tag.text().trim()).to.eq('公海'));
      cy.contains('.customer_lists a', '公海客户').should('have.attr', 'href', '/admin/pool-lists/11/contacts');
      cy.contains('.customer_lists a', '一级公海 #12').should('have.attr', 'href', '/admin/pool-lists/12/contacts');
    });
    cy.contains('tbody tr', '混合活动').within(() => {
      cy.get('.customer_lists li').should('have.length', 2);
      cy.get('.customer_lists').should('contain', '私有').and('contain', '公海');
    });
    cy.contains('tbody tr', '历史活动').within(() => {
      cy.get('.customer_lists').should('contain', '已删除的列表');
      cy.get('.customer_lists a').should('not.exist');
    });
    cy.contains('tbody tr', '空受众活动').find('.customer_lists li').should('not.exist');
    cy.get('.campaigns').screenshot('campaign-audience-labels-desktop');
    cy.viewport(390, 844);
    cy.contains('tbody tr', '混合活动').find('.customer_lists .tag').should('be.visible');
    cy.document().then((doc) => expect(doc.documentElement.scrollWidth).to.be.at.most(390));
    cy.screenshot('campaign-audience-labels-mobile');
  });
});
