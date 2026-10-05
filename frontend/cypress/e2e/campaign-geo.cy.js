/* eslint-env mocha */
/* global cy, expect */
import { mapCountries, countryView, locationPoints, cityTotals } from '../../src/utils/campaignGeo';

const locations = [
  { country_code: 'CN', country: 'China', region: 'Shanghai', city: 'Shanghai', longitude: 121.47, latitude: 31.23, count: 3 },
  { country_code: 'CN', country: 'China', region: 'Shanghai', city: 'Shanghai', longitude: 121.49, latitude: 31.24, count: 2 },
  { country_code: 'CN', country: 'China', region: 'Beijing', city: 'Beijing', longitude: 116.41, latitude: 39.9, count: 4 },
  { country_code: 'US', country: 'United States', region: 'Illinois', city: 'Springfield', longitude: -89.65, latitude: 39.78, count: 6 },
  { country_code: 'US', country: 'United States', region: 'Massachusetts', city: 'Springfield', longitude: -72.59, latitude: 42.1, count: 2 },
  { country_code: 'HK', country: 'Hong Kong', region: '', city: 'Hong Kong', longitude: 114.17, latitude: 22.3, count: 1 },
  { country_code: 'FR', country: 'France', region: 'Ile-de-France', city: 'Paris', longitude: 2.35, latitude: 48.85, count: 1 },
  { country_code: 'US', country: 'United States', region: 'California', city: '', longitude: -119, latitude: 35, count: 1 },
];
const report = { enabled: true, total_opens: 21, located_opens: 20, unknown_opens: 1, locations };

describe('City open maps', () => {
  it('keeps separate cities and handles countries crossing the dateline', () => {
    const points = locationPoints({ locations: locations.map((location) => ({ ...location, countryCode: location.country_code })) });
    const cities = cityTotals(points);
    expect(cities.find((city) => city.city === 'Shanghai').count).to.eq(5);
    expect(cities.filter((city) => city.city === 'Springfield')).to.have.length(2);
    expect(cities.find((city) => city.region === 'California').city).to.eq('');
    expect(locationPoints({ locations: [{ longitude: 0, latitude: 91, count: 1 }] })).to.have.length(0);
    ['CN', 'US', 'TR', 'KR', 'LC'].forEach((key) => expect(mapCountries.some((country) => country.key === key)).to.eq(true));
    mapCountries.forEach((country) => expect(() => countryView(country.key, [])).not.to.throw());
    const fiji = countryView('FJ', [{ longitude: 179, latitude: -17 }, { longitude: -179, latitude: -18 }]);
    expect(fiji.geoJSON.features).to.have.length(1);
    expect(fiji.bounds[1][0] - fiji.bounds[0][0]).to.be.lessThan(25);
  });

  it('switches country maps, focuses cities and preserves empty maps across refreshes', () => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    let nextReport = report;
    let fail = false;
    cy.intercept('GET', '/api/campaigns/report/geo*', (req) => {
      req.reply(fail ? { statusCode: 503, body: { message: 'Temporary fixture failure' } } : { body: { data: nextReport } });
    }).as('geo');
    cy.loginAndVisit('/admin/campaigns/analytics');
    cy.wait('@geo');
    cy.get('[data-cy=geo-scope]').should('contain', '6 cities').and('contain', '20 mapped opens');
    cy.get('.campaign-geo-map canvas').should('be.visible');
    cy.get('[data-cy=geo-country]').select('CN');
    cy.get('[data-cy=geo-scope]').should('contain', 'China').and('contain', '2 cities').and('contain', '9 mapped opens');
    cy.get('[data-cy=geo-cities]').should('contain', 'Shanghai').and('contain', 'Beijing').and('not.contain', 'Springfield');
    cy.contains('[data-cy=geo-cities] tr', 'Shanghai').should('contain', '5');
    cy.get('.campaign-geo-heatmap').screenshot('country-city-map-china');
    cy.contains('[data-cy=geo-cities] button', 'Shanghai').click();
    cy.get('[data-cy=geo-scope] .tag').should('contain', 'Shanghai');
    cy.get('[data-cy=geo-reset]').click();
    cy.get('[data-cy=geo-scope] .tag').should('not.exist');
    cy.get('[data-cy=geo-country]').select('US');
    cy.get('[data-cy=geo-scope]').should('contain', '2 cities').and('contain', '9 mapped opens');
    cy.get('[data-cy=geo-cities]').should('contain', 'Illinois').and('contain', 'Massachusetts').and('contain', 'City unavailable');
    cy.get('[data-cy=geo-country]').select('HK');
    cy.get('[data-cy=geo-scope]').should('contain', 'Hong Kong').and('contain', '1 cities');
    cy.get('.campaign-geo-map canvas').should('be.visible');
    cy.get('[data-cy=geo-country]').select('CN');
    cy.then(() => { nextReport = { enabled: true, total_opens: 0, located_opens: 0, unknown_opens: 0, locations: [] }; });
    cy.contains('button', 'Refresh report').click();
    cy.wait('@geo');
    cy.get('[data-cy=geo-country]').should('have.value', 'CN');
    cy.get('[data-cy=geo-scope]').should('contain', '0 cities');
    cy.get('[data-cy=geo-cities]').should('contain', 'No city locations').and('not.contain', 'Shanghai');
    cy.get('.campaign-geo-map canvas').should('be.visible');
    cy.then(() => { fail = true; });
    cy.contains('button', 'Refresh report').click();
    cy.wait('@geo');
    cy.get('.campaign-geo-heatmap').should('contain', 'Could not load open locations');
    cy.get('.campaign-geo-map canvas').should('be.visible');
    cy.viewport(390, 844);
    cy.get('[data-cy=geo-country]').should('be.visible');
    cy.document().then((doc) => { expect(doc.documentElement.scrollWidth).to.be.at.most(390); });
    cy.then(() => { fail = false; nextReport = { ...report, enabled: false, locations: [], located_opens: 0, unknown_opens: 21 }; });
    cy.contains('button', 'Refresh report').click();
    cy.wait('@geo');
    cy.get('.campaign-geo-heatmap').should('contain', 'Location collection is off');
    cy.get('.campaign-geo-map canvas').should('be.visible');
  });
});
