describe('Reply mailbox processing confirmation', () => {
  beforeEach(() => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'zh-CN'; }));
    cy.loginAndVisit('/admin/user/profile');
    cy.contains('.b-tabs nav a', '个人邮件配置').click();
  });

  it('closes the bounce confirmation on cancel and leaves processing off', () => {
    cy.contains('.reply-mailboxes button', '新建').click();
    cy.get('[data-cy=reply-mailbox-scan]').click();
    cy.get('[data-cy=reply-mailbox-bounce]').click();
    cy.contains('.dialog .modal-card-foot button', '取消').should('be.visible').click();
    cy.get('.dialog').should('not.exist');
    cy.get('[data-cy=reply-mailbox-bounce] input').should('not.be.checked');
    cy.get('[data-cy=reply-mailbox-no-processing]').should('be.visible');
    cy.screenshot('reply-mailbox-cancelled', { capture: 'viewport' });
    cy.viewport(390, 844);
    cy.get('[data-cy=reply-mailbox-bounce]').click();
    cy.contains('.dialog .modal-card-foot button', '取消').click();
    cy.get('.dialog').should('not.exist');
    cy.get('[data-cy=reply-mailbox-bounce] input').should('not.be.checked');
    cy.get('[data-cy=reply-mailbox-bounce]').click();
    cy.get('body').type('{esc}');
    cy.get('.dialog').should('not.exist');
    cy.get('[data-cy=reply-mailbox-bounce] input').should('not.be.checked');
    // Cancellation must leave the control usable for a later deliberate opt-in.
    cy.get('[data-cy=reply-mailbox-bounce]').click();
    cy.contains('.dialog .modal-card-foot button', '确认').click();
    cy.get('.dialog').should('not.exist');
    cy.get('[data-cy=reply-mailbox-bounce] input').should('be.checked');
  });

  it('closes shared confirmations with a null or omitted cancel callback and preserves real callbacks', () => {
    const confirmed = cy.stub().as('confirmed');
    const cancelled = cy.stub().as('cancelled');
    // Exercise the actual shared helper used by other pages, including callers
    // that pass null to reach the optional presentation options argument.
    [null, undefined, cancelled].forEach((onCancel) => {
      cy.window().then((win) => {
        win.document.querySelector('#app').__vue__.$utils.confirm('取消操作验证', confirmed, onCancel, { type: 'is-warning' });
      });
      cy.contains('.dialog .modal-card-foot button', '取消').click();
      cy.get('.dialog').should('not.exist');
    });
    cy.get('@confirmed').should('not.have.been.called');
    cy.get('@cancelled').should('have.been.calledOnce').and('have.been.calledWith', 'button');
  });
});
