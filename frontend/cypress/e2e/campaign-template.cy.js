describe('Campaign template selection', () => {
  const attachmentCleanupPaths = [];

  afterEach(() => {
    attachmentCleanupPaths.splice(0).reverse().forEach((path) => cy.request('DELETE', path));
  });

  it('shows the template while creating, previews it and keeps the selection', () => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.loginAndVisit('/admin/campaigns/new');
    let listID;
    cy.request('POST', '/api/customer-lists', { name: 'Template recipients', type: 'private', optin: 'single' })
      .then(({ body }) => { listID = body.data.id; });
    cy.visit('/admin/campaigns/new');
    cy.viewport(1440, 1000);

    // The creation form surfaces the wrapping template with a usable default.
    cy.get('[data-cy=campaign-template-field]').should('be.visible');
    cy.get('[data-cy=campaign-template]').should('contain', 'Default campaign template');

    // Preview renders the selected template before the campaign is saved.
    cy.intercept('POST', '/api/campaigns/preview').as('draftPreview');
    cy.get('[data-cy=btn-preview-draft]').click();
    cy.wait('@draftPreview').its('response.statusCode').should('eq', 200);
    cy.get('.modal iframe').should(($iframe) => {
      expect($iframe.attr('srcdoc') || '').to.contain('<!doctype html>');
    });
    cy.get('.modal-card-foot button').click();

    // Create the campaign and verify the selected template was persisted.
    cy.then(() => {
      cy.get('[data-cy=campaign-audience-trigger]').click();
      cy.get(`[data-cy=campaign-audience-${listID}]`).check();
      cy.get('[data-cy=campaign-audience-trigger]').click();
    });
    cy.get('input[name=name]').type('Template selection campaign');
    cy.get('input[name=subject]').type('Template selection subject');
    cy.intercept('POST', '/api/campaigns').as('createCampaign');
    cy.get('[data-cy=btn-continue]').click();
    cy.wait('@createCampaign').then(({ response }) => {
      expect(response.statusCode).to.eq(200);
      expect(response.body.data.template_id).to.be.a('number');
    });
  });

  it('keeps the attachment cover inside the template body without inheriting its table styles', () => {
    cy.loginAndVisit('/admin/campaigns');
    const suffix = Date.now().toString(36);
    let templateID;
    let listID;
    const files = [];
    cy.request('POST', '/api/templates', {
      name: `Letter layout ${suffix}`,
      type: 'campaign',
      body: `<!doctype html><html><head><style>
        body { margin: 0; }
        table, td { border-collapse: collapse !important; }
        .content table { width: 100%; margin: 20px 0; border: 1px solid #ddd; }
        .content td { padding: 8px; border: 1px solid #ddd; text-align: center; }
        </style></head><body><table width="100%"><tr><td class="content" id="letter-body">{{ template "content" . }}</td></tr></table>
        <footer id="letter-footer">Footer</footer></body></html>`,
    }).its('body.data.id').then((id) => {
      templateID = id;
      attachmentCleanupPaths.push(`/api/templates/${id}`);
    });
    cy.request('POST', '/api/customer-lists', { name: `Letter recipients ${suffix}`, type: 'private', optin: 'single' })
      .its('body.data.id').then((id) => {
        listID = id;
        attachmentCleanupPaths.push(`/api/customer-lists/${id}`);
      });
    cy.window().then(async (win) => {
      const canvas = win.document.createElement('canvas');
      canvas.width = 1600;
      canvas.height = 80;
      const wideImage = await new Promise((resolve) => canvas.toBlob(resolve, 'image/png'));
      for (const [filename, type, contents] of [
        [`catalogue-${suffix}.pdf`, 'application/pdf', '%PDF-1.4\nAttachment fixture'],
        [`images-${suffix}.zip`, 'application/zip', 'Attachment fixture'],
        [`wide-${suffix}.png`, 'image/png', wideImage],
      ]) {
        const form = new win.FormData();
        form.set('file', new win.File([contents], filename, { type }));
        const response = await win.fetch('/api/media', { method: 'POST', body: form });
        expect(response.ok).to.eq(true);
        const file = (await response.json()).data;
        files.push(file);
        attachmentCleanupPaths.push(`/api/media/${file.id}`);
      }
    });
    cy.then(() => cy.request('POST', '/api/campaigns', {
      name: `Letter attachments ${suffix}`, subject: 'Letter attachments', type: 'regular', messenger: 'email',
      content_type: 'html', body: '<p>Campaign letter</p><table id="authored-table"><tr><td>Body table</td></tr></table>',
      template_id: templateID, customer_list_ids: [listID], attachment_lang: 'de',
      from_email: 'Cover QA <cover@example.test>', media: [files[1].id, files[0].id, files[2].id],
    })).its('body.data.id').then((id) => {
      attachmentCleanupPaths.push(`/api/campaigns/${id}`);
      cy.visit(`/admin/campaigns/${id}#content`);
    });
    cy.get('[data-cy=btn-preview]').click();
    cy.get('#iframe').should('have.attr', 'srcdoc').and('include', 'listmonk-attachment-cover').then((html) => cy.document().then((doc) => {
      const frame = doc.createElement('iframe');
      frame.setAttribute('sandbox', 'allow-same-origin');
      frame.style.cssText = 'position:fixed;visibility:hidden;width:320px;height:600px;border:0';
      const loaded = new Cypress.Promise((resolve) => frame.addEventListener('load', resolve, { once: true }));
      frame.srcdoc = html;
      doc.body.append(frame);
      return loaded.then(() => {
        const rendered = frame.contentDocument;
        const css = (element) => frame.contentWindow.getComputedStyle(element);
        const letter = rendered.getElementById('letter-body');
        const authoredTable = rendered.getElementById('authored-table');
        const section = letter?.querySelector('.listmonk-attachments');
        const cover = section?.querySelector('.listmonk-attachment-cover');
        const row = section?.querySelector('.listmonk-attachment-file');
        expect(letter?.textContent).to.include('Campaign letter');
        expect(rendered.getElementById('letter-footer')?.textContent).to.eq('Footer');
        expect(rendered.querySelectorAll('.listmonk-attachment-cover')).to.have.length(1);
        const image = letter.querySelector(`img[alt="${files[2].filename}"]`);
        expect(image?.naturalWidth).to.eq(1600);
        const imageWidth = image.getBoundingClientRect().width;
        expect(imageWidth).to.be.within(1, rendered.documentElement.clientWidth);
        expect(authoredTable.nextElementSibling?.contains(image)).to.eq(true);
        expect(section?.previousElementSibling?.contains(image)).to.eq(true);
        expect(cover?.textContent).to.include(files[0].filename).and.to.include('Herunterladen');
        expect(row?.textContent).to.include(files[1].filename).and.not.to.include('Ansehen');
        const links = Array.from(row.querySelectorAll('a'), (link) => link.getAttribute('href'));
        expect(links).to.have.length(2);
        expect(links.every((href) => href.endsWith('download=1'))).to.eq(true);

        section.querySelectorAll('td').forEach((cell) => expect(css(cell).borderLeftWidth).to.eq('0px'));
        section.querySelectorAll('table').forEach((table) => expect(css(table).marginTop).to.eq('0px'));
        expect(css(cover).borderCollapse).to.eq('separate');
        expect(css(cover.rows[0].cells[0].querySelector('td')).borderTopWidth).to.eq('2px');
        expect(css(section.rows[0].cells[0]).textAlign).to.eq('left');
        expect(css(section.rows[0].cells[1]).textAlign).to.eq('right');
        expect(css(row.cells[0]).textAlign).to.eq('left');
        expect(css(row.cells[1]).textAlign).to.eq('right');
        expect(css(authoredTable.rows[0].cells[0]).borderLeftWidth).to.eq('1px');
        expect(css(authoredTable.rows[0].cells[0]).textAlign).to.eq('center');
        expect(rendered.documentElement.scrollWidth).to.be.at.most(rendered.documentElement.clientWidth);
      }).finally(() => frame.remove());
    }));
    cy.get('.modal-card-foot button').click();
  });
});
